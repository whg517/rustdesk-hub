package httpapi_test

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whg517/rustdesk-hub/internal/httpapi"
	"github.com/whg517/rustdesk-hub/internal/hub"
	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

const (
	rustDeskSettingsPath = "/api/v1/system-settings/rustdesk"
	clientSetupPath      = "/api/v1/client-setup"
)

func TestWebClientSetupAndConnection(t *testing.T) {
	store, err := hub.Open(filepath.Join(t.TempDir(), "hub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InitOwner(t.Context(), "owner", password); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	setup, err := rustdesk.NewSetup(rustdesk.ServerSettings{IDServer: "id.example.test", PublicKey: key}, origin)
	if err != nil {
		t.Fatal(err)
	}
	anon := client{handler: httpapi.New(store, httpapi.Config{PublicURL: origin})}
	const bookPath = "/api/v1/address-book"
	const sessionPath = "/api/v1/session"
	anon.call(t, "GET", sessionPath, "", 401)
	anon.call(t, "GET", clientSetupPath, "", 401)
	anon.call(t, "GET", bookPath, "", 401)
	web := anon.webLogin(t, password)

	type sessionResult struct {
		RustDesk *rustdesk.Setup
	}
	if got := decode[sessionResult](t, web.call(t, "GET", clientSetupPath, "", 200)); got.RustDesk != nil {
		t.Fatal("unconfigured server reported a client profile")
	}
	web.call(t, "PUT", rustDeskSettingsPath, `{"idServer":"id.example.test","publicKey":"`+key+`"}`, 200)
	session := decode[sessionResult](t, web.call(t, "GET", sessionPath, "", 200))
	if session.RustDesk == nil || session.RustDesk.ImportConfig != setup.ImportConfig {
		t.Fatalf("missing configuration in the authorized session: %+v", session)
	}
	web.call(t, "POST", bookPath+"/peers", `{"id":"012 345 678","alias":"Fixture"}`, 201)
	web.call(t, "POST", bookPath+"/peers", `{"id":"192.0.2.10:21118"}`, 201)

	type bookResult struct {
		Data []struct {
			ID         string
			Connection *rustdesk.Connection
		}
	}
	book := decode[bookResult](t, web.call(t, "GET", bookPath, "", 200))
	if len(book.Data) != 2 || book.Data[0].ID != "012345678" || book.Data[0].Connection == nil ||
		*book.Data[0].Connection != *setup.ConnectionFor("012345678") || book.Data[1].Connection != nil {
		t.Fatalf("unexpected connection targets: %+v", book)
	}

	native := anon.nativeLogin(t, password)
	guid := decode[map[string]string](t, native.call(t, "POST", "/api/ab/personal", "", 200))["guid"]
	nativeBook := decode[struct {
		Data []map[string]json.RawMessage
	}](t, native.call(t, "POST", "/api/ab/peers?ab="+guid, "", 200))
	if len(nativeBook.Data) != 2 {
		t.Fatalf("native address book has %d entries", len(nativeBook.Data))
	}
	for _, entry := range nativeBook.Data {
		if _, exists := entry["connection"]; exists {
			t.Fatal("browser connection details must not change native address book responses")
		}
	}

	web.call(t, "PUT", rustDeskSettingsPath, `{"idServer":"next.example.test","publicKey":"`+key+`"}`, 200)
	web.call(t, "PUT", rustDeskSettingsPath, `{"idServer":"https://wrong.example.test","publicKey":"invalid"}`, 400)
	updated := decode[sessionResult](t, web.call(t, "GET", clientSetupPath, "", 200))
	if updated.RustDesk == nil || updated.RustDesk.IDServer != "next.example.test:21116" || updated.RustDesk.ImportConfig == setup.ImportConfig {
		t.Fatalf("expected updated client setup in existing session, got %+v", updated)
	}
	book = decode[bookResult](t, web.call(t, "GET", bookPath, "", 200))
	if len(book.Data) != 2 || book.Data[0].Connection == nil || !strings.Contains(book.Data[0].Connection.Target, "@next.example.test:21116") {
		t.Fatalf("address book did not use changed settings: %+v", book)
	}

	web.call(t, "PUT", rustDeskSettingsPath, `{}`, 200)
	if got := decode[sessionResult](t, web.call(t, "GET", sessionPath, "", 200)); got.RustDesk != nil {
		t.Fatal("unconfigured server reported a client profile")
	}
	book = decode[bookResult](t, web.call(t, "GET", bookPath, "", 200))
	if len(book.Data) != 2 || book.Data[0].Connection != nil {
		t.Fatalf("unconfigured server returned unexpected entries: %+v", book)
	}
}

func TestRustDeskSettingsRequireAdminAndCSRF(t *testing.T) {
	anon, admin := multiUserClients(t)
	anon.call(t, "GET", rustDeskSettingsPath, "", 401)
	anon.call(t, "PUT", rustDeskSettingsPath, `{}`, 401)
	admin.call(t, "POST", usersPath, memberRequest, 201)
	member := anon.webLoginAs(t, "成员", memberPassword)
	member.call(t, "GET", rustDeskSettingsPath, "", 403)
	member.call(t, "PUT", rustDeskSettingsPath, `{}`, 403)

	noCSRF := admin
	noCSRF.csrf = ""
	noCSRF.call(t, "PUT", rustDeskSettingsPath, `{}`, 403)
	admin.call(t, "PUT", rustDeskSettingsPath, `{"onlineEnabled":true}`, 400)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	admin.call(t, "PUT", rustDeskSettingsPath,
		`{"idServer":"id.example.test","publicKey":"`+key+`","onlineEnabled":true,"onlineServer":"hbbs.internal:22116"}`, 200)
	saved := decode[hub.RustDeskSettings](t, admin.call(t, "GET", rustDeskSettingsPath, "", 200))
	if saved.OnlineAddress() != "hbbs.internal:22116" || saved.IDServer != "id.example.test:21116" {
		t.Fatalf("unexpected persisted settings: %+v", saved)
	}
	setup := member.call(t, "GET", clientSetupPath, "", 200).Body.String()
	if !strings.Contains(setup, "id.example.test:21116") || strings.Contains(setup, "hbbs.internal") {
		t.Fatal("client setup should contain only the client-facing settings")
	}
}
