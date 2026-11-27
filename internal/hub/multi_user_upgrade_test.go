package hub_test

import (
	"testing"

	"github.com/whg517/rustdesk-hub/internal/hub"
)

func TestUpgradeKeepsExistingAdministratorSessionAndBook(t *testing.T) {
	path, legacy := legacyDatabase(t)
	store, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	const password = "original-test-password"
	if err := store.InitOwner(ctx, "owner", password); err != nil {
		t.Fatal(err)
	}
	login, err := store.Login(ctx, "owner", password, hub.Web)
	if err != nil {
		t.Fatal(err)
	}
	credential := hub.Credential{Token: login.Token, Source: hub.Web}
	guid, err := store.PersonalGUID(ctx, credential)
	if err != nil {
		t.Fatal(err)
	}
	scope := hub.BookScope{Credential: credential, GUID: guid}
	if _, err := store.AddPeer(ctx, scope, hub.Peer{ID: "00123", Alias: "旧设备"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	execute(t, legacy, "DROP INDEX idx_accounts_name")
	execute(t, legacy, "ALTER TABLE accounts DROP COLUMN disabled")
	upgraded, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	user, err := upgraded.CurrentUser(ctx, credential)
	if err != nil || !user.IsAdmin || user.Name != "owner" || user.Disabled {
		t.Fatalf("original administrator session unavailable: %+v, %v", user, err)
	}
	page, err := upgraded.ListPeers(ctx, scope, hub.PeerQuery{Page: 1, PageSize: 10})
	if err != nil || page.Total != 1 || len(page.Peers) != 1 || page.Peers[0].Alias != "旧设备" {
		t.Fatalf("original address book changed: %+v, %v", page, err)
	}
}
