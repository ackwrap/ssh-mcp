package config

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func openTestStore(t *testing.T, name string) (*Store, *OpenStore) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, name), filepath.Join(dir, name+".key"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := store.SaveEncrypted(NewAppConfig(), "master123", nil)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open(store, key)
	if err != nil {
		t.Fatal(err)
	}
	return store, opened
}

func passwordServer(name, password string) map[string]any {
	return map[string]any{"name": name, "host": "host.example", "username": "root", "auth": map[string]any{"type": "password", "password": password}}
}

func TestEncryptedCRUDMaskAndConflict(t *testing.T) {
	store, opened := openTestStore(t, "config.json")
	if _, err := opened.AddServer(passwordServer("web1", "secret")); err != nil {
		t.Fatal(err)
	}
	raw, err := store.LoadRaw()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(raw)
	if strings.Contains(string(encoded), "secret") {
		t.Fatal("plaintext credential reached disk")
	}
	if _, err := opened.UpdateServer("web1", map[string]any{"host": "host2", "auth": map[string]any{"type": "password", "password": Mask}}); err != nil {
		t.Fatal(err)
	}
	server, _ := opened.GetServer("web1")
	if server.Host != "host2" || String(server.Auth.Password) != "secret" {
		t.Fatalf("mask update lost data: %#v", server)
	}

	stale, err := Open(store, opened.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := opened.AddServer(passwordServer("web2", "secret2")); err != nil {
		t.Fatal(err)
	}
	_, err = stale.AddServer(passwordServer("stale", "secret3"))
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestProxyMaskRequiresOldPassword(t *testing.T) {
	_, opened := openTestStore(t, "config.json")
	if _, err := opened.AddServer(passwordServer("web", "secret")); err != nil {
		t.Fatal(err)
	}
	_, err := opened.UpdateServer("web", map[string]any{"proxy": map[string]any{"type": "http", "host": "proxy", "port": 8080, "password": Mask}})
	if err == nil || !strings.Contains(err.Error(), "代理密码") {
		t.Fatalf("expected proxy mask error, got %v", err)
	}
}

func TestExportImportFull(t *testing.T) {
	sourceStore, source := openTestStore(t, "source.json")
	if _, err := source.AddServer(passwordServer("web", "secret")); err != nil {
		t.Fatal(err)
	}
	raw, err := sourceStore.LoadRaw()
	if err != nil {
		t.Fatal(err)
	}
	rawData, _ := json.Marshal(raw)
	var dump map[string]any
	_ = json.Unmarshal(rawData, &dump)
	_, target := openTestStore(t, "target.json")
	if _, err := target.ImportFull(dump, "wrong"); err == nil {
		t.Fatal("wrong import password succeeded")
	}
	cfg, err := target.ImportFull(dump, "master123")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 || String(cfg.Servers[0].Auth.Password) != "secret" {
		t.Fatalf("imported config = %#v", cfg)
	}
}
