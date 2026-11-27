package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/whg517/rustdesk-hub/internal/httpapi"
	"github.com/whg517/rustdesk-hub/internal/hub"
)

const (
	origin      = "https://hub.example.test"
	password    = "initial-test-password"
	newPassword = "different-test-password"
)

type client struct {
	handler    http.Handler
	token      string
	cookie     *http.Cookie
	csrf       string
	remoteAddr string
	forwarded  string
}

func (c client) send(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, origin+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if strings.HasPrefix(path, "/api/v1/") {
		if method != http.MethodGet {
			req.Header.Set("Origin", origin)
		}
		if c.csrf != "" {
			req.Header.Set("X-CSRF-Token", c.csrf)
		}
	}
	if c.remoteAddr != "" {
		req.RemoteAddr = c.remoteAddr
	}
	if c.forwarded != "" {
		req.Header.Set("X-Forwarded-For", c.forwarded)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)
	return rec
}

func (c client) call(t *testing.T, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	rec := c.send(method, path, body)
	assertStatus(t, rec, status, method+" "+path)
	return rec
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, status int, label string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("%s returned %d, want %d: %s", label, rec.Code, status, rec.Body)
	}
}

func empty(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Body.Len() != 0 {
		t.Fatalf("expected empty response, got %s", response.Body)
	}
}

func decode[T any](t *testing.T, response *httptest.ResponseRecorder) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func (c client) nativeLogin(t *testing.T, secret string) client {
	t.Helper()
	return c.nativeLoginAs(t, "owner", secret)
}

func (c client) nativeLoginAs(t *testing.T, name, secret string) client {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"username": name, "password": secret, "id": "123", "uuid": "test-client",
		"autoLogin": true, "type": "account", "deviceInfo": struct{}{},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := c.call(t, "POST", "/api/login", string(body), 200)
	result := decode[struct {
		Type  string `json:"type"`
		Token string `json:"access_token"`
		User  struct {
			Name    string `json:"name"`
			Status  int    `json:"status"`
			IsAdmin bool   `json:"is_admin"`
		} `json:"user"`
	}](t, rec)
	if result.Type != "access_token" || result.Token == "" || result.User.Name != name || result.User.Status != 1 || result.User.IsAdmin {
		t.Fatal("native login contract mismatch")
	}
	return client{handler: c.handler, token: result.Token}
}

func (c client) webLogin(t *testing.T, secret string) client {
	t.Helper()
	return c.webLoginAs(t, "owner", secret)
}

func (c client) webLoginAs(t *testing.T, name, secret string) client {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": name, "password": secret})
	if err != nil {
		t.Fatal(err)
	}
	rec := c.call(t, "POST", "/api/v1/session", string(body), 200)
	session := decode[struct {
		CSRFToken string `json:"csrfToken"`
		APIURL    string `json:"apiUrl"`
	}](t, rec)
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode ||
		session.CSRFToken == "" || session.APIURL != origin {
		t.Fatal("browser session security or connection settings mismatch")
	}
	return client{handler: c.handler, cookie: cookies[0], csrf: session.CSRFToken}
}

type peer struct {
	ID    string   `json:"id"`
	Alias string   `json:"alias"`
	Note  string   `json:"note"`
	Tags  []string `json:"tags"`
}

type page struct {
	Total int    `json:"total"`
	Data  []peer `json:"data"`
}

func TestRustDesk153AndWebShareAddressBook(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.sqlite3")
	store, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	if err := store.InitOwner(t.Context(), "owner", password); err != nil {
		t.Fatal(err)
	}
	openClient := func() client {
		return client{handler: httpapi.New(store, httpapi.Config{PublicURL: origin})}
	}
	anon := openClient()

	if options := decode[[]string](t, anon.call(t, "GET", "/api/login-options", "", 200)); options == nil || len(options) != 0 {
		t.Fatal("login options must be an empty JSON array")
	}
	anon.call(t, "POST", "/api/login", `{"type":"account","username":"owner","password":"wrong"}`, 401)
	anon.call(t, "POST", "/api/ab/personal", "", 401)
	first, second := anon.nativeLogin(t, password), anon.nativeLogin(t, password)
	web := anon.webLogin(t, password)
	restoredWeb := web.call(t, "GET", "/api/v1/session", "", 200).Header().Get("X-Request-ID")
	if first.token == second.token {
		t.Fatal("each login must issue an independent token")
	}
	if user := decode[map[string]any](t, second.call(t, "POST", "/api/currentUser", `{"id":"123","uuid":"test-client"}`, 200)); user["name"] != "owner" || user["is_admin"] != false {
		t.Fatal("currentUser must return the user at the root")
	}

	guid := decode[map[string]string](t, first.call(t, "POST", "/api/ab/personal", "", 200))["guid"]
	if guid == "" || decode[map[string]string](t, second.call(t, "POST", "/api/ab/personal", "", 200))["guid"] != guid {
		t.Fatal("personal GUID must be stable across sessions")
	}
	peers := "/api/ab/peers?ab=" + guid + "&current=1&pageSize=100"
	peerAdd, peerUpdate, peerDelete := "/api/ab/peer/add/"+guid, "/api/ab/peer/update/"+guid, "/api/ab/peer/"+guid
	tagAdd, tags := "/api/ab/tag/add/"+guid, "/api/ab/tags/"+guid
	if list := decode[page](t, second.call(t, "POST", peers, "", 200)); list.Total != 0 || list.Data == nil {
		t.Fatal("empty address book must have an empty data array")
	}
	if list := decode[[]any](t, first.call(t, "POST", tags, "", 200)); list == nil || len(list) != 0 {
		t.Fatal("empty tag list must be an empty JSON array")
	}
	if settings := decode[map[string]int](t, first.call(t, "POST", "/api/ab/settings", "", 200)); settings["max_peer_one_ab"] != 0 {
		t.Fatal("unexpected address book settings")
	}
	first.call(t, "POST", "/api/ab/shared/profiles?current=1&pageSize=100", "", 200)
	first.call(t, "GET", "/api/device-group/accessible", "", 200)
	first.call(t, "POST", "/api/ab/peers?ab=other", "", 404)
	empty(t, anon.call(t, "POST", "/api/sysinfo", `{"hostname":"synthetic"}`, 200))
	anon.call(t, "POST", "/api/heartbeat", `{}`, 200)

	client{handler: anon.handler, token: web.cookie.Value}.call(t, "POST", "/api/ab/personal", "", 401)
	noCSRF := web
	noCSRF.csrf = ""
	noCSRF.call(t, "POST", "/api/v1/address-book/peers", `{"id":"0123"}`, 403)

	first.call(t, "POST", tagAdd, `{"name":"Untagged","color":4283215696}`, 400)
	web.call(t, "POST", "/api/v1/address-book/tags", `{"name":"Untagged","color":4283215696}`, 400)
	empty(t, first.call(t, "POST", tagAdd, `{"name":"家里","color":4283215696}`, 200))
	empty(t, first.call(t, "POST", tagAdd, `{"name":"办公室","color":4294940104}`, 200))
	empty(t, first.call(t, "POST", peerAdd, `{"id":"01 23","alias":"书房","tags":["家里"],"note":"备用","hash":"discard-test-value"}`, 200))
	empty(t, first.call(t, "POST", peerAdd, `{"id":"0123","alias":"重复条目"}`, 200))
	duplicate := web.call(t, "POST", "/api/v1/address-book/peers", `{"id":"01 23","alias":"网页重复条目"}`, 200)
	if decode[map[string]bool](t, duplicate)["created"] {
		t.Fatal("formatted and canonical IDs must refer to the same device")
	}
	visible := web.call(t, "GET", "/api/v1/address-book?q=01%2023&tag=%E5%AE%B6%E9%87%8C", "", 200)
	list := decode[page](t, visible)
	if list.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != "0123" || list.Data[0].Alias != "书房" || strings.Contains(visible.Body.String(), "discard-test-value") {
		t.Fatalf("web must see the same text ID, alias and tags: %s", visible.Body)
	}

	// Independent clients can change different fields without overwriting one another.
	var writes sync.WaitGroup
	writeAlias := make(chan *httptest.ResponseRecorder, 1)
	writeNote := make(chan *httptest.ResponseRecorder, 1)
	writes.Go(func() {
		writeAlias <- first.send("PUT", peerUpdate, `{"id":"01 23","alias":"工作站"}`)
	})
	writes.Go(func() {
		writeNote <- web.send("PATCH", "/api/v1/address-book/peers", `{"id":"0123","note":"新的设备备注"}`)
	})
	writes.Wait()
	aliasResponse, noteResponse := <-writeAlias, <-writeNote
	assertStatus(t, aliasResponse, 200, "native alias update")
	empty(t, aliasResponse)
	assertStatus(t, noteResponse, 204, "web note update")

	first.call(t, "PUT", "/api/ab/tag/rename/"+guid, `{"old":"家里","new":"Untagged"}`, 400)
	labels := decode[struct {
		Tags []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"tags"`
	}](t, visible)
	var familyTagID string
	for _, label := range labels.Tags {
		if label.Name == "家里" {
			familyTagID = label.ID
		}
	}
	if familyTagID == "" {
		t.Fatal("web tag must carry a stable ID")
	}
	web.call(t, "PATCH", "/api/v1/address-book/tags", `{"tagId":"`+familyTagID+`","newName":"Untagged"}`, 400)
	first.call(t, "PUT", "/api/ab/tag/rename/"+guid, `{"old":"家里","new":"办公室"}`, 409)
	empty(t, first.call(t, "PUT", "/api/ab/tag/rename/"+guid, `{"old":"家里","new":"家庭"}`, 200))
	list = decode[page](t, second.call(t, "POST", peers, "", 200))
	if len(list.Data) != 1 || list.Data[0].Alias != "工作站" || list.Data[0].Note != "新的设备备注" ||
		len(list.Data[0].Tags) != 1 || list.Data[0].Tags[0] != "家庭" {
		t.Fatalf("second native session did not see merged changes: %#v", list)
	}
	first.call(t, "PUT", peerUpdate, `{"id":"0123","hash":"test-only"}`, 422)
	empty(t, first.call(t, "PUT", peerUpdate, `{"id":"0123","hash":"","hostname":"ignored"}`, 200))

	// The old name can be reused without redirecting an open browser form.
	empty(t, first.call(t, "POST", tagAdd, `{"name":"家里","color":4294940104}`, 200))
	web.call(t, "PATCH", "/api/v1/address-book/tags", `{"name":"家里","newName":"旧页面","color":4282090230}`, 409)
	web.call(t, "PATCH", "/api/v1/address-book/tags",
		`{"tagId":"`+familyTagID+`","color":4282090230}`, 204)
	tagList := decode[[]struct {
		Name  string `json:"name"`
		Color uint32 `json:"color"`
	}](t, first.call(t, "POST", tags, "", 200))
	colors := make(map[string]uint32)
	for _, label := range tagList {
		colors[label.Name] = label.Color
	}
	if colors["家庭"] != 4282090230 || colors["家里"] != 4294940104 {
		t.Fatalf("the old browser form must retain the original tag across a rename: %v", colors)
	}
	filteredByID := decode[page](t, web.call(t, "GET", "/api/v1/address-book?tagId="+familyTagID, "", 200))
	if filteredByID.Total != 1 || len(filteredByID.Data) != 1 || filteredByID.Data[0].ID != "0123" {
		t.Fatalf("web filter should follow the original tag after its name is reused: %+v", filteredByID)
	}
	web.call(t, "PATCH", "/api/v1/address-book/tags", `{"tagId":"9223372036854775808","newName":"新标签"}`, 400)
	web.call(t, "GET", "/api/v1/address-book?tagId=9223372036854775808", "", 400)
	web.call(t, "DELETE", "/api/v1/address-book/tags", `["家里"]`, 409)
	web.call(t, "DELETE", "/api/v1/address-book/tags", `{"tagId":"`+familyTagID+`"}`, 204)
	list = decode[page](t, second.call(t, "POST", peers, "", 200))
	if len(list.Data) != 1 || list.Data[0].Tags == nil || len(list.Data[0].Tags) != 0 {
		t.Fatal("removing a label must also remove its associations")
	}
	empty(t, first.call(t, "DELETE", peerDelete, `["01 23"]`, 200))
	second.call(t, "PUT", peerUpdate, `{"id":"0123","alias":"stale"}`, 404)
	web.call(t, "POST", "/api/v1/address-book/peers", `{"id":"0123","alias":"已重新添加"}`, 201)
	empty(t, first.call(t, "POST", "/api/logout", `{}`, 200))
	empty(t, first.call(t, "POST", "/api/logout", `{}`, 200))
	first.call(t, "POST", peers, "", 401)
	second.call(t, "POST", peers, "", 200)

	chunkedLogout := httptest.NewRequest("POST", origin+"/api/logout", strings.NewReader(strings.Repeat("x", (1<<20)+1)))
	chunkedLogout.ContentLength = -1
	chunkedLogout.TransferEncoding = []string{"chunked"}
	chunkedLogout.Header.Set("Authorization", "Bearer "+second.token)
	streamResponse := httptest.NewRecorder()
	anon.handler.ServeHTTP(streamResponse, chunkedLogout)
	assertStatus(t, streamResponse, 413, "oversized chunked logout")
	second.call(t, "POST", "/api/currentUser", "{}", 200)

	wrongPassword := web.call(t, "PUT", "/api/v1/account/password",
		`{"currentPassword":"wrong","newPassword":"`+newPassword+`"}`, 401).Header().Get("X-Request-ID")
	tooLarge := web.call(t, "POST", "/api/v1/address-book/peers",
		`{"id":"9876","note":"`+strings.Repeat("a", 1<<20)+`"}`, 413)
	rejectedID := tooLarge.Header().Get("X-Request-ID")
	diagnostics := web.call(t, "GET", "/api/v1/diagnostics", "", 200)
	entries := decode[[]struct {
		Source    string `json:"source"`
		Operation string `json:"operation"`
		Status    int    `json:"status"`
		ErrorCode string `json:"errorCode"`
		RequestID string `json:"requestId"`
	}](t, diagnostics)
	got := make(map[string]struct {
		source, operation, code string
		status                  int
	})
	for _, entry := range entries {
		got[entry.RequestID] = struct {
			source, operation, code string
			status                  int
		}{entry.Source, entry.Operation, entry.ErrorCode, entry.Status}
	}
	if got[rejectedID].code != "Request.TooLarge" || got[rejectedID].operation != "addressBook.write" ||
		got[streamResponse.Header().Get("X-Request-ID")].code != "Request.TooLarge" ||
		got[restoredWeb].operation != "session.read" || got[restoredWeb].source != "web" ||
		got[wrongPassword].code != "Account.InvalidCredentials" {
		t.Fatal("expected diagnostic entries for request-size, session restoration and credential failure")
	}
	log := diagnostics.Body.String()
	if !strings.Contains(log, `"operation":"login","status":401`) || !strings.Contains(log, `"operation":"addressBook.write"`) ||
		strings.Contains(log, password) || strings.Contains(log, "discard-test-value") {
		t.Fatal("diagnostics did not record sanitized outcomes")
	}

	web.call(t, "PUT", "/api/v1/account/password", `{"currentPassword":"`+password+`","newPassword":"`+newPassword+`"}`, 204)
	second.call(t, "POST", "/api/currentUser", "", 401)
	web.call(t, "GET", "/api/v1/session", "", 401)
	anon.call(t, "POST", "/api/login", `{"type":"account","username":"owner","password":"`+password+`"}`, 401)
	fresh := anon.nativeLogin(t, newPassword)

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	fresh.handler = openClient().handler
	list = decode[page](t, fresh.call(t, "POST", peers, "", 200))
	if list.Total != 1 || len(list.Data) != 1 || list.Data[0].Alias != "已重新添加" {
		t.Fatalf("restart did not preserve the session and book: %#v", list)
	}
}
