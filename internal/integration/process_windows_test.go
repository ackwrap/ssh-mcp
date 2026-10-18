//go:build windows

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestRunCommandSupportsWindowsCmdShim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.cmd")
	if err := os.WriteFile(path, []byte("@echo off\r\necho shim-ok\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := runCommand(context.Background(), path, "--ignored")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "shim-ok") {
		t.Fatalf("output = %q", output)
	}
}

func TestResolveScoopShimUsesExistingAbsoluteTarget(t *testing.T) {
	for _, name := range []string{"default", "short-path"} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if name == "short-path" {
				path, err := windows.UTF16PtrFromString(directory)
				if err != nil {
					t.Fatal(err)
				}
				buffer := make([]uint16, 32768)
				length, err := windows.GetShortPathName(path, &buffer[0], uint32(len(buffer)))
				if err != nil {
					t.Skipf("8.3 short paths unavailable: %v", err)
				}
				if length == 0 || length >= uint32(len(buffer)) {
					t.Fatalf("invalid short path length: %d", length)
				}
				short := windows.UTF16ToString(buffer[:length])
				if strings.EqualFold(short, directory) {
					t.Skip("filesystem does not provide an 8.3 alias")
				}
				directory = short
			}
			shim := filepath.Join(directory, "shims", "tool.exe")
			target := filepath.Join(directory, "apps", "tool", "current", "tool.exe")
			if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("target"), 0o700); err != nil {
				t.Fatal(err)
			}
			metadata := "path = \"" + target + "\"\n"
			if err := os.WriteFile(strings.TrimSuffix(shim, ".exe")+".shim", []byte(metadata), 0o600); err != nil {
				t.Fatal(err)
			}
			resolved := resolveScoopShim(shim)
			resolvedInfo, err := os.Stat(resolved)
			if err != nil {
				t.Fatal(err)
			}
			targetInfo, err := os.Stat(target)
			if err != nil {
				t.Fatal(err)
			}
			// Windows may expand an 8.3 path (e.g. RUNNER~1) or normalize
			// casing, so compare file identity rather than path spelling.
			if !os.SameFile(resolvedInfo, targetInfo) {
				t.Fatalf("resolved = %q, want the same file as %q", resolved, target)
			}
		})
	}
}

func TestResolveScoopShimRejectsRelativeTarget(t *testing.T) {
	shim := filepath.Join(t.TempDir(), "shims", "tool.exe")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(strings.TrimSuffix(shim, ".exe")+".shim", []byte("path = \"relative.exe\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resolved := resolveScoopShim(shim); resolved != shim {
		t.Fatalf("resolved unsafe relative target: %q", resolved)
	}
}

func TestRunCommandBypassesScoopShimWrapper(t *testing.T) {
	directory := t.TempDir()
	shim := filepath.Join(directory, "shims", "tool.exe")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	source := os.Getenv("ComSpec")
	if source == "" {
		source = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}
	target := filepath.Join(directory, "apps", "tool", "current", "tool.exe")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, executable, 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := "path = \"" + target + "\"\n"
	if err := os.WriteFile(strings.TrimSuffix(shim, ".exe")+".shim", []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := runCommand(context.Background(), shim, "/d", "/c", "echo shim-target-ok")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "shim-target-ok") {
		t.Fatalf("output = %q", output)
	}
}

func TestResolveScoopShimRejectsTargetOutsideApps(t *testing.T) {
	directory := t.TempDir()
	shim := filepath.Join(directory, "shims", "tool.exe")
	target := filepath.Join(directory, "outside", "tool.exe")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := "path = \"" + target + "\"\n"
	if err := os.WriteFile(strings.TrimSuffix(shim, ".exe")+".shim", []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if resolved := resolveScoopShim(shim); resolved != shim {
		t.Fatalf("resolved target outside Scoop apps: %q", resolved)
	}
}

func TestResolveScoopShimRejectsDifferentPackage(t *testing.T) {
	directory := t.TempDir()
	shim := filepath.Join(directory, "shims", "tool.exe")
	target := filepath.Join(directory, "apps", "attacker-package", "current", "tool.exe")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("target"), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := "path = \"" + target + "\"\n"
	if err := os.WriteFile(strings.TrimSuffix(shim, ".exe")+".shim", []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if resolved := resolveScoopShim(shim); resolved != shim {
		t.Fatalf("resolved target from a different Scoop package: %q", resolved)
	}
}
