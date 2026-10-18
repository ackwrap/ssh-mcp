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
	input := passwordServer("web", "secret")
	input["group"], input["disabled"] = "生产环境", true
	if _, err := source.AddServer(input); err != nil {
		t.Fatal(err)
	}
	if err := source.SetSettings(Settings{AllowMCPAddresses: true}); err != nil {
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
	if len(cfg.Servers) != 1 || String(cfg.Servers[0].Auth.Password) != "secret" || cfg.Servers[0].Group != "生产环境" || !cfg.Servers[0].Disabled {
		t.Fatalf("imported config = %#v", cfg)
	}
	if cfg.Settings.AllowMCPAddresses {
		t.Fatal("import enabled address disclosure without a local opt-in")
	}
}

func TestAddressPrivacySettings(t *testing.T) {
	legacy, err := DecodeAppConfig([]byte(`{"version":1,"servers":[],"jumphosts":[]}`))
	if err != nil || legacy.Settings.AllowMCPAddresses || NewAppConfig().Settings.AllowMCPAddresses {
		t.Fatalf("addresses must be hidden by default: %v, %#v", err, legacy.Settings)
	}
	store, opened := openTestStore(t, "config.json")
	if _, err := opened.AddServer(passwordServer("web", "secret")); err != nil {
		t.Fatal(err)
	}
	stale, err := Open(store, opened.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, allow := range []bool{true, false} {
		if err := opened.SetSettings(Settings{AllowMCPAddresses: allow}); err != nil {
			t.Fatal(err)
		}
		opened, err = Open(store, opened.Key)
		if err != nil {
			t.Fatal(err)
		}
		if opened.Config.Settings.AllowMCPAddresses != allow || len(opened.Config.Servers) != 1 || opened.Config.Servers[0].Host != "host.example" || String(opened.Config.Servers[0].Auth.Password) != "secret" {
			t.Fatal("saving address privacy did not preserve settings and connection data")
		}
	}
	var conflict *ConflictError
	if err := stale.SetSettings(Settings{AllowMCPAddresses: true}); !errors.As(err, &conflict) {
		t.Fatalf("stale privacy setting did not conflict: %v", err)
	}
}

func TestServerGroupAndDisabledLifecycle(t *testing.T) {
	store, opened := openTestStore(t, "config.json")
	server, err := opened.AddServer(passwordServer("legacy", "secret"))
	if err != nil {
		t.Fatal(err)
	}
	if server.Group != "" || server.Disabled {
		t.Fatalf("legacy defaults = %#v", server)
	}
	if _, err := opened.UpdateServer("legacy", map[string]any{"group": "  生产环境  ", "disabled": true}); err != nil {
		t.Fatal(err)
	}
	opened, err = Open(store, opened.Key)
	if err != nil {
		t.Fatal(err)
	}
	server, err = opened.GetServer("legacy")
	if err != nil || server.Group != "生产环境" || !server.Disabled || server.Host != "host.example" || String(server.Auth.Password) != "secret" {
		t.Fatalf("persisted server = %#v, err = %v", server, err)
	}
	copy, err := opened.CopyServer("legacy", map[string]any{"name": "copy"})
	if err != nil || copy.Group != "生产环境" || !copy.Disabled {
		t.Fatalf("copied server = %#v, err = %v", copy, err)
	}
	template := opened.ExportTemplate()["servers"].([]map[string]any)[0]
	if template["group"] != "生产环境" || template["disabled"] != true {
		t.Fatalf("template = %#v", template)
	}
	server, err = opened.UpdateServer("legacy", map[string]any{"group": " ", "disabled": false})
	if err != nil || server.Group != "" || server.Disabled {
		t.Fatalf("re-enabled server = %#v, err = %v", server, err)
	}
	for _, changes := range []map[string]any{{"group": 123}, {"disabled": "false"}} {
		if _, err := opened.UpdateServer("legacy", changes); err == nil {
			t.Fatalf("invalid changes accepted: %#v", changes)
		}
	}
}

func TestPublicEndpointHidesAddressesAndCredentials(t *testing.T) {
	for _, host := range []string{"192.0.2.40", "2001:db8::40", "server.example"} {
		t.Run(host, func(t *testing.T) {
			data := passwordServer("web", "server-secret")
			data["host"] = host
			data["proxy"] = map[string]any{"type": "socks5", "host": "proxy.example", "port": 1080, "password": "proxy-secret"}
			before, _ := json.Marshal(data)
			public := PublicEndpoint(data, false)
			encoded, _ := json.Marshal(public)
			for _, hidden := range []string{host, "proxy.example", "server-secret", "proxy-secret"} {
				if strings.Contains(string(encoded), hidden) {
					t.Fatalf("public endpoint contains %q: %s", hidden, encoded)
				}
			}
			if public["host"] != Mask || public["proxy"].(map[string]any)["host"] != Mask {
				t.Fatalf("addresses not masked: %s", encoded)
			}
			after, _ := json.Marshal(data)
			if string(before) != string(after) {
				t.Fatal("masking changed the source configuration")
			}
			if MaskEndpoint(data)["host"] != host {
				t.Fatal("WebUI must retain the editable address")
			}
			visible := PublicEndpoint(data, true)
			if visible["host"] != host || visible["proxy"].(map[string]any)["host"] != "proxy.example" {
				t.Fatalf("opt-in did not expose addresses: %#v", visible)
			}
			encoded, _ = json.Marshal(visible)
			if strings.Contains(string(encoded), "server-secret") || strings.Contains(string(encoded), "proxy-secret") {
				t.Fatal("address opt-in exposed credentials")
			}
		})
	}
}
