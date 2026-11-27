package httpapi_test

import (
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/whg517/rustdesk-hub/internal/httpapi"
	"github.com/whg517/rustdesk-hub/internal/hub"
)

func TestWebAddressBookOnlineStatus(t *testing.T) {
	store, err := hub.Open(filepath.Join(t.TempDir(), "hub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.InitOwner(t.Context(), "owner", password); err != nil {
		t.Fatal(err)
	}

	var queriedIDs []string
	var queriedAddress string
	var queryErr error
	config := httpapi.Config{
		PublicURL: origin,
		QueryOnline: func(_ context.Context, address string, ids []string) ([]bool, error) {
			queriedAddress = address
			queriedIDs = ids
			return []bool{true, false}, queryErr
		},
	}
	anon := client{handler: httpapi.New(store, config)}
	const statusPath = "/api/v1/address-book/online-status"
	anon.call(t, "GET", statusPath+"?id=012345678", "", 401)
	if len(queriedIDs) != 0 {
		t.Fatal("an unauthenticated request reached the status server")
	}

	web := anon.webLogin(t, password)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	web.call(t, "PUT", rustDeskSettingsPath, `{"idServer":"id.example.test","publicKey":"`+key+`","onlineEnabled":true}`, 200)
	web.call(t, "GET", statusPath+"?"+strings.Repeat("id=012345678&", 101), "", 400)
	web.call(t, "POST", "/api/v1/address-book/peers", `{"id":"012345678","alias":"Test workstation"}`, 201)
	web.call(t, "POST", "/api/v1/address-book/peers", `{"id":"234567890","alias":"Test laptop"}`, 201)
	web.call(t, "POST", "/api/v1/address-book/peers", `{"id":"192.0.2.10:21118"}`, 201)

	type statusResult struct {
		Configured bool
		CheckedAt  *time.Time
		Statuses   map[string]string
	}
	path := statusPath + "?id=234567890&id=01+2345678&id=999999999&id=192.0.2.10%3A21118"
	result := decode[statusResult](t, web.call(t, "GET", path, "", 200))
	if queriedAddress != "id.example.test:21116" {
		t.Fatalf("query did not follow the client's ID server: %s", queriedAddress)
	}
	if !slices.Equal(queriedIDs, []string{"012345678", "234567890"}) {
		t.Fatalf("queried IDs = %v, want only saved devices", queriedIDs)
	}
	if !result.Configured || result.CheckedAt == nil ||
		!maps.Equal(result.Statuses, map[string]string{"012345678": "online", "234567890": "offline", "192.0.2.10:21118": "unknown"}) {
		t.Fatalf("unexpected status response: %+v", result)
	}

	queryErr = errors.New("status server unavailable")
	web.call(t, "PUT", rustDeskSettingsPath, `{"onlineEnabled":true,"onlineServer":"hbbs.internal:22116"}`, 200)
	unknown := map[string]string{"012345678": "unknown", "234567890": "unknown", "192.0.2.10:21118": "unknown"}
	result = decode[statusResult](t, web.call(t, "GET", path, "", 200))
	if queriedAddress != "hbbs.internal:22116" {
		t.Fatalf("query did not use the updated override: %s", queriedAddress)
	}
	if !result.Configured || result.CheckedAt == nil || !maps.Equal(result.Statuses, unknown) {
		t.Fatalf("unexpected response for unavailable server: %+v", result)
	}
	if book := decode[page](t, web.call(t, "GET", "/api/v1/address-book", "", 200)); book.Total != 3 {
		t.Fatalf("address book after status failure contains %d entries", book.Total)
	}

	web.call(t, "PUT", rustDeskSettingsPath, `{"onlineServer":"hbbs.internal:22116"}`, 200)
	result = decode[statusResult](t, web.call(t, "GET", path, "", 200))
	if result.Configured || result.CheckedAt != nil || !maps.Equal(result.Statuses, unknown) {
		t.Fatalf("unexpected response for unconfigured server: %+v", result)
	}
}
