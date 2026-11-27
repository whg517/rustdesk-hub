package rustdesk

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"maps"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestClientSetupAndLink(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xfb}, 32))
	const publicURL = "https://hub.example.test"
	setup, err := NewSetup(ServerSettings{
		IDServer: "id.example.test:22116", RelayServer: "relay.example.test", PublicKey: key + "\n",
	}, publicURL)
	if err != nil {
		t.Fatal(err)
	}
	reversed := []byte(setup.ImportConfig)
	slices.Reverse(reversed)
	data, err := base64.URLEncoding.DecodeString(string(reversed))
	if err != nil {
		t.Fatal(err)
	}
	var imported map[string]string
	if err := json.Unmarshal(data, &imported); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"host": "id.example.test:22116", "relay": "relay.example.test:21117", "api": publicURL, "key": key}
	if !maps.Equal(imported, want) {
		t.Fatalf("configuration = %v, want %v", imported, want)
	}

	for _, id := range []string{"012345678", "Laptop_01"} {
		connection := setup.ConnectionFor(id)
		if connection == nil {
			t.Fatalf("missing connection for %q", id)
		}
		link, err := url.Parse(connection.URL)
		if err != nil {
			t.Fatal(err)
		}
		target := id + "@id.example.test:22116"
		if link.Scheme != "rustdesk" || link.Host != "connect" || link.EscapedPath() != "/"+target ||
			link.Query().Get("key") != key || connection.Target != target+"?key="+key {
			t.Fatalf("connection = %+v", connection)
		}
	}
	for _, id := range []string{"", "01 234", "123@elsewhere?key=x", "other/123", "192.0.2.13:21118"} {
		if setup.ConnectionFor(id) != nil {
			t.Fatalf("legacy value %q must remain a copy-only entry", id)
		}
	}
}

func TestClientSetupIPv6(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	setup, err := NewSetup(ServerSettings{IDServer: "[2001:db8::2]:22116", PublicKey: key}, "https://hub.example.test")
	if err != nil {
		t.Fatal(err)
	}
	connection := setup.ConnectionFor("012345678")
	if connection == nil || connection.URL != "" ||
		connection.Target != "012345678@[2001:db8::2]:22116?key="+key || setup.RelayServer != "" {
		t.Fatalf("expected copyable IPv6 target and automatic relay, got %+v", connection)
	}
}

func TestClientSetupRejectsInvalidOptions(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, tt := range []struct {
		options ServerSettings
		field   string
	}{
		{ServerSettings{PublicKey: key}, "ID 服务器"},
		{ServerSettings{IDServer: "https://id.example.test", PublicKey: key}, "ID 服务器"},
		{ServerSettings{IDServer: "[fe80::1%en0]:21116", PublicKey: key}, "ID 服务器"},
		{ServerSettings{IDServer: "id.example.test", RelayServer: "relay.example.test:65536", PublicKey: key}, "中继服务器"},
		{ServerSettings{IDServer: "id.example.test", PublicKey: "private-or-invalid-key"}, "服务端公钥"},
	} {
		if _, err := NewSetup(tt.options, "https://hub.example.test"); err == nil || !strings.Contains(err.Error(), tt.field) {
			t.Fatalf("expected an error for %s, got %v", tt.field, err)
		}
	}
}
