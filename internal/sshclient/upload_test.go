package sshclient

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ackwrap/ssh-mcp/internal/config"
	"github.com/pkg/sftp"
)

func newUploadTestManager(t testing.TB, handlers ...sftp.Handlers) (*Manager, *config.Server) {
	t.Helper()
	port, stop := startTestSSHServer(t, handlers...)
	t.Cleanup(stop)
	password := "test-password"
	server := &config.Server{Endpoint: config.Endpoint{
		ID: "upload", Name: "upload", Host: "127.0.0.1", Port: port, Username: "tester",
		Auth: config.AuthSpec{Type: "password", Password: &password},
	}}
	manager := NewManager(&config.OpenStore{Config: config.NewAppConfig()})
	t.Cleanup(manager.CloseAll)
	return manager, server
}

func TestUploadSizesAndDownload(t *testing.T) {
	manager, server := newUploadTestManager(t)
	for _, size := range []int{
		0, 123, 32 * 1024, 32*1024 + 17,
		UploadBufferSize - 1, UploadBufferSize, UploadBufferSize + 17,
		8*1024*1024 + 123,
	} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			dir := t.TempDir()
			local, remote := filepath.Join(dir, "source.bin"), filepath.Join(dir, "remote.bin")
			data := make([]byte, size)
			for i := range data {
				data[i] = byte((i*31 + i/32768) % 251)
			}
			if err := os.WriteFile(local, data, 0o600); err != nil {
				t.Fatal(err)
			}
			// Replacing a longer file must not leave its old tail behind.
			if err := os.WriteFile(remote, bytes.Repeat([]byte{0xff}, size+100), 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := manager.Upload(server, local, remote)
			if err != nil {
				t.Fatal(err)
			}
			if result["ok"] != true || result["size"] != int64(size) {
				t.Fatalf("upload result = %#v", result)
			}
			uploaded, err := os.ReadFile(remote)
			if err != nil {
				t.Fatal(err)
			}
			if sha256.Sum256(uploaded) != sha256.Sum256(data) {
				t.Fatal("uploaded content differs from source")
			}
			download := filepath.Join(dir, "download.bin")
			if _, err := manager.Download(server, remote, download); err != nil {
				t.Fatal(err)
			}
			downloaded, err := os.ReadFile(download)
			if err != nil {
				t.Fatal(err)
			}
			if sha256.Sum256(downloaded) != sha256.Sum256(data) {
				t.Fatal("downloaded content differs from source")
			}
		})
	}
}

func TestUploadConcurrentFiles(t *testing.T) {
	manager, server := newUploadTestManager(t)
	connection, err := manager.GetClient(server)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			local, remote := filepath.Join(dir, "source.bin"), filepath.Join(dir, "remote.bin")
			data := bytes.Repeat([]byte{byte(i), 0x00, 0xff, 0x42}, 1024*1024+17)
			if err := os.WriteFile(local, data, 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := manager.Upload(server, local, remote)
			if err != nil || result["size"] != int64(len(data)) {
				t.Fatalf("upload result=%#v err=%v", result, err)
			}
			uploaded, err := os.ReadFile(remote)
			if err != nil || !bytes.Equal(uploaded, data) {
				t.Fatalf("concurrent upload content mismatch: %v", err)
			}
			reused, err := manager.GetClient(server)
			if err != nil || reused != connection {
				t.Fatalf("SSH connection was not reused: %v", err)
			}
		})
	}
}

func TestUploadPipelinesWrites(t *testing.T) {
	secondWrite := make(chan struct{})
	var once sync.Once
	handlers := uploadTestHandlers(func(writer io.WriterAt, p []byte, off int64) (int, error) {
		if off == 0 {
			// No first acknowledgement until another packet is in flight. A
			// sequential uploader will time out instead of filling the pipeline.
			select {
			case <-secondWrite:
			case <-time.After(5 * time.Second):
				return 0, fmt.Errorf("upload waited for each write acknowledgement")
			}
		} else {
			once.Do(func() { close(secondWrite) })
		}
		return writer.WriteAt(p, off)
	})
	manager, server := newUploadTestManager(t, handlers)
	local := filepath.Join(t.TempDir(), "source.bin")
	data := bytes.Repeat([]byte("pipeline upload\x00"), 16385)
	if err := os.WriteFile(local, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(server, local, "/upload.bin"); err != nil {
		t.Fatal(err)
	}
	content, err := manager.ReadFile(server, "/upload.bin", MaxFileBytes)
	if err != nil || content != string(data) {
		t.Fatalf("uploaded content mismatch: %v", err)
	}
}

func TestUploadKeepsCompatiblePackets(t *testing.T) {
	const maxPacketSize = 32 * 1024
	var mu sync.Mutex
	largestPacket := 0
	handlers := uploadTestHandlers(func(writer io.WriterAt, p []byte, off int64) (int, error) {
		if len(p) > maxPacketSize {
			return 0, fmt.Errorf("SFTP write is too large: %d bytes", len(p))
		}
		mu.Lock()
		largestPacket = max(largestPacket, len(p))
		mu.Unlock()
		return writer.WriteAt(p, off)
	})
	manager, server := newUploadTestManager(t, handlers)
	local := filepath.Join(t.TempDir(), "source.bin")
	data := bytes.Repeat([]byte("x"), 2*UploadBufferSize+17)
	if err := os.WriteFile(local, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Upload(server, local, "/upload.bin")
	if err != nil || result["size"] != int64(len(data)) {
		t.Fatalf("upload result=%#v err=%v", result, err)
	}
	mu.Lock()
	observed := largestPacket
	mu.Unlock()
	if observed != maxPacketSize {
		t.Fatalf("largest SFTP write = %d, want %d", observed, maxPacketSize)
	}
	content, err := manager.ReadFile(server, "/upload.bin", MaxFileBytes)
	if err != nil || content != string(data) {
		t.Fatalf("uploaded content mismatch across buffer boundaries: %v", err)
	}
}

func TestUploadWriteFailureTruncates(t *testing.T) {
	const packetSize = 32 * 1024
	laterWrite := make(chan struct{})
	var once sync.Once
	handlers := uploadTestHandlers(func(writer io.WriterAt, p []byte, off int64) (int, error) {
		if off == packetSize {
			select {
			case <-laterWrite:
			case <-time.After(5 * time.Second):
				return 0, fmt.Errorf("later write never arrived")
			}
			return 0, os.ErrPermission
		}
		n, err := writer.WriteAt(p, off)
		if off > packetSize && err == nil {
			once.Do(func() { close(laterWrite) })
		}
		return n, err
	})
	manager, server := newUploadTestManager(t, handlers)
	local := filepath.Join(t.TempDir(), "source.bin")
	data := bytes.Repeat([]byte("x"), 4*packetSize+123)
	if err := os.WriteFile(local, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Upload(server, local, "/upload.bin")
	if err == nil || !strings.Contains(err.Error(), "上传失败") || result != nil {
		t.Fatalf("failed upload result=%#v err=%v", result, err)
	}
	select {
	case <-laterWrite:
	default:
		t.Fatal("test did not write beyond the failed packet")
	}
	content, err := manager.ReadFile(server, "/upload.bin", MaxFileBytes)
	if err != nil || content != string(data[:packetSize]) {
		t.Fatalf("failed upload must retain only the successful prefix: size=%d err=%v", len(content), err)
	}
}

func TestUploadPathErrors(t *testing.T) {
	manager, server := newUploadTestManager(t)
	dir := t.TempDir()
	local, remote := filepath.Join(dir, "local.bin"), filepath.Join(dir, "remote.bin")
	if err := os.WriteFile(remote, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(server, local, remote); err == nil {
		t.Fatal("missing source must fail")
	}
	data, err := os.ReadFile(remote)
	if err != nil || string(data) != "keep" {
		t.Fatalf("missing source changed destination: data=%q err=%v", data, err)
	}
	if err := os.WriteFile(local, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Upload(server, local, filepath.Join(dir, "missing", "remote.bin")); err == nil {
		t.Fatal("missing destination directory must fail")
	}
}

// Inject server-side delays and failures without depending on an external host.
func uploadTestHandlers(writeAt func(io.WriterAt, []byte, int64) (int, error)) sftp.Handlers {
	handlers := sftp.InMemHandler()
	handlers.FilePut = &uploadTestFileWriter{FileWriter: handlers.FilePut, writeAt: writeAt}
	return handlers
}

type uploadTestFileWriter struct {
	sftp.FileWriter
	writeAt func(io.WriterAt, []byte, int64) (int, error)
}

func (h *uploadTestFileWriter) Filewrite(request *sftp.Request) (io.WriterAt, error) {
	writer, err := h.FileWriter.Filewrite(request)
	if err != nil {
		return nil, err
	}
	return &uploadTestWriter{WriterAt: writer, writeAt: h.writeAt}, nil
}

type uploadTestWriter struct {
	io.WriterAt
	writeAt func(io.WriterAt, []byte, int64) (int, error)
}

func (w *uploadTestWriter) WriteAt(p []byte, off int64) (int, error) {
	return w.writeAt(w.WriterAt, p, off)
}

func (w *uploadTestWriter) Close() error {
	if closer, ok := w.WriterAt.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func BenchmarkUploadLatency(b *testing.B) {
	for _, concurrent := range []bool{false, true} {
		name := "sequential"
		if concurrent {
			name = "pipelined"
		}
		b.Run(name, func(b *testing.B) {
			handlers := uploadTestHandlers(func(writer io.WriterAt, p []byte, off int64) (int, error) {
				time.Sleep(10 * time.Millisecond)
				return writer.WriteAt(p, off)
			})
			manager, server := newUploadTestManager(b, handlers)
			client, err := manager.sftp(server)
			if err != nil {
				b.Fatal(err)
			}
			defer client.Close()
			const size = 4 * 1024 * 1024
			local := filepath.Join(b.TempDir(), "source.bin")
			if err := os.WriteFile(local, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
				b.Fatal(err)
			}
			b.SetBytes(size)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if concurrent {
					if _, err := manager.Upload(server, local, "/upload.bin"); err != nil {
						b.Fatal(err)
					}
					continue
				}
				// Reproduce the original io.Copy path for comparison.
				source, err := os.Open(local)
				if err != nil {
					b.Fatal(err)
				}
				target, err := client.Create("/upload.bin")
				if err != nil {
					_ = source.Close()
					b.Fatal(err)
				}
				_, copyErr := io.Copy(target, source)
				closeErr := target.Close()
				_ = source.Close()
				if copyErr != nil || closeErr != nil {
					b.Fatalf("copy=%v close=%v", copyErr, closeErr)
				}
			}
			b.StopTimer()
		})
	}
}
