package main

import (
	"encoding/base64"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/whg517/rustdesk-hub/internal/hub"
	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

func TestConfigDefaultsFileAndFlags(t *testing.T) {
	t.Chdir(t.TempDir())
	original, err := parseOptions(flag.NewFlagSet("hub", flag.ContinueOnError), nil)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte(`db: data/other.sqlite3
hbbs: rustdesk.example.test:21116
behind-proxy: true
web-dir: retired/dist
`)
	if err := os.WriteFile(defaultConfigPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	flags := flag.NewFlagSet("hub", flag.ContinueOnError)
	got, err := parseOptions(flags, []string{"-hbbs=", "-behind-proxy=false", "init-owner", "owner"})
	if err != nil {
		t.Fatal(err)
	}
	want := original
	want.Database = "data/other.sqlite3"
	if got != want {
		t.Fatalf("merged configuration = %+v, want %+v", got, want)
	}
	if !slices.Equal(flags.Args(), []string{"init-owner", "owner"}) {
		t.Fatalf("command arguments = %v", flags.Args())
	}
	if flags.Lookup("web-dir") != nil {
		t.Fatal("web-dir must not be a runtime flag")
	}

	fileOnly, err := parseOptions(flag.NewFlagSet("hub", flag.ContinueOnError), nil)
	if err != nil {
		t.Fatal(err)
	}
	if fileOnly.LegacyHBBS != "rustdesk.example.test:21116" || !fileOnly.BehindProxy {
		t.Fatalf("unexpected file settings: %+v", fileOnly)
	}
}

func TestExplicitConfigErrors(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		name   string
		source string
		want   string
	}{
		{"missing", "", "读取配置文件失败"},
		{"unknown-field", "hbbss: local.example.test\n", "hbbss"},
		{"invalid-boolean", "behind-proxy: [false]\n", "into bool"},
		{"multiple-documents", "hbbs: one.example.test\n---\nhbbs: two.example.test\n", "配置文件"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.name+".yaml")
			if tt.source != "" {
				if err := os.WriteFile(path, []byte(tt.source), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := parseOptions(flag.NewFlagSet("hub", flag.ContinueOnError), []string{"-config", path})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected config error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestLegacyRustDeskSettingsImportOnce(t *testing.T) {
	dir := t.TempDir()
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	configPath := filepath.Join(dir, "config.yaml")
	config := "hbbs: old.example.test:21116\nrustdesk:\n  id-server: old.example.test\n  relay-server: relay.example.test\n  public-key: " + key + "\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := parseOptions(flag.NewFlagSet("hub", flag.ContinueOnError), []string{"-config", configPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	dbPath := filepath.Join(dir, "hub.sqlite3")
	store, err := hub.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := opts.importRustDeskSettings(ctx, store); err != nil {
		t.Fatal(err)
	}
	settings, err := store.RustDeskSettings(ctx)
	if err != nil || settings.OnlineAddress() != "old.example.test:21116" || settings.OnlineServer != "" ||
		settings.PublicKey != key || settings.RelayServer != "relay.example.test:21117" {
		t.Fatalf("unexpected legacy settings: %+v, %v", settings, err)
	}

	const password = "legacy-test-password"
	if err := store.InitOwner(ctx, "owner", password); err != nil {
		t.Fatal(err)
	}
	login, err := store.Login(ctx, "owner", password, hub.Web)
	if err != nil {
		t.Fatal(err)
	}
	credential := hub.Credential{Token: login.Token, Source: hub.Web}
	settings.IDServer = "new.example.test"
	settings.OnlineEnabled = false
	want, err := store.SaveRustDeskSettings(ctx, credential, settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := hub.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := opts.importRustDeskSettings(ctx, reopened); err != nil {
		t.Fatal(err)
	}
	got, err := reopened.AdminRustDeskSettings(ctx, credential)
	if err != nil || got != want || got.OnlineAddress() != "" || got.IDServer != "new.example.test:21116" {
		t.Fatalf("saved settings did not survive restart: %+v, %v", got, err)
	}
}

func TestLegacyRustDeskImportKeepsTheValidGroup(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, test := range []struct {
		name       string
		key        string
		legacyHBBS string
		client     string
		query      string
	}{
		{"invalid-client", "invalid", "id.example.test:21116", "", "id.example.test:21116"},
		{"invalid-query", key, "https://id.example.test", "id.example.test:21116", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := hub.Open(filepath.Join(t.TempDir(), "hub.sqlite3"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			opts := options{
				LegacyHBBS:  test.legacyHBBS,
				LegacySetup: &rustdesk.ServerSettings{IDServer: "id.example.test", PublicKey: test.key},
			}
			if err := opts.importRustDeskSettings(t.Context(), store); err != nil {
				t.Fatal(err)
			}
			settings, err := store.RustDeskSettings(t.Context())
			if err != nil || settings.IDServer != test.client || settings.OnlineAddress() != test.query {
				t.Fatalf("expected the valid group to be imported, got %+v, %v", settings, err)
			}
		})
	}
}
