package hub_test

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whg517/rustdesk-hub/internal/hub"
)

func legacyDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "legacy.sqlite3")
	store, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return path, db
}

func execute(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradePreservesPeerAndTag(t *testing.T) {
	path, legacy := legacyDatabase(t)
	execute(t, legacy, "INSERT INTO peers (id, book_id, device_id, alias, note) VALUES (1, 1, '000 123', '书房', '备注')")
	execute(t, legacy, "INSERT INTO tags (id, book_id, name, color) VALUES (1, 1, '家庭', 4283215696)")
	execute(t, legacy, "INSERT INTO peer_tags (peer_id, tag_id) VALUES (1, 1)")

	upgraded, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()

	var id, alias, note, tag string
	err = legacy.QueryRow(`SELECT p.device_id, p.alias, p.note, t.name FROM peers p
		JOIN peer_tags pt ON pt.peer_id = p.id JOIN tags t ON t.id = pt.tag_id`).Scan(&id, &alias, &note, &tag)
	if err != nil {
		t.Fatal(err)
	}
	if id != "000123" || alias != "书房" || note != "备注" || tag != "家庭" {
		t.Fatalf("unexpected upgraded record: %q %q %q %q", id, alias, note, tag)
	}
}

func TestUpgradeStopsOnCollidingIDsWithoutChangingRecords(t *testing.T) {
	path, legacy := legacyDatabase(t)
	execute(t, legacy, "DROP TABLE diagnostics")
	execute(t, legacy, `INSERT INTO peers (book_id, device_id, alias, note) VALUES
		(1, '01 23', '别名一', ''),
		(1, '0123', '别名二', ''),
		(1, '000 567', '其他设备', '')`)

	upgraded, err := hub.Open(path)
	if err == nil {
		upgraded.Close()
		t.Fatal("expected an upgrade conflict")
	}
	if !strings.Contains(err.Error(), `"0123"`) {
		t.Fatalf("expected the affected ID in the upgrade error, got %v", err)
	}
	var count int
	err = legacy.QueryRow("SELECT count(*) FROM peers WHERE device_id IN ('01 23', '0123', '000 567')").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("expected all three legacy records to remain, got %d", count)
	}
	if err := legacy.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = 'diagnostics'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("upgrade conflict must also preserve the pre-upgrade schema")
	}
}

func TestExistingReservedTagCanBeRepaired(t *testing.T) {
	path, legacy := legacyDatabase(t)
	store, err := hub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := t.Context()
	if err := store.InitOwner(ctx, "owner", "legacy-test-password"); err != nil {
		t.Fatal(err)
	}
	login, err := store.Login(ctx, "owner", "legacy-test-password", hub.Web)
	if err != nil {
		t.Fatal(err)
	}
	scope := hub.BookScope{Credential: hub.Credential{Token: login.Token, Source: hub.Web}}
	execute(t, legacy, "INSERT INTO peers (id, book_id, device_id, alias, note) VALUES (1, 1, '001', '', '')")
	execute(t, legacy, "INSERT INTO tags (id, book_id, name, color) VALUES (1, 1, 'Untagged', 4283215696)")
	execute(t, legacy, "INSERT INTO peer_tags (peer_id, tag_id) VALUES (1, 1)")

	names := []string{"Untagged"}
	if err := store.UpdatePeer(ctx, scope, "001", hub.PeerChanges{Tags: &names}); err != nil {
		t.Fatalf("existing association should remain repairable: %v", err)
	}
	created, err := store.AddPeer(ctx, scope, hub.Peer{ID: "002", Tags: names})
	if err == nil || created {
		t.Fatal("new association using the reserved tag should be rejected")
	}
	newName := "重新整理"
	if err := store.UpdateTag(ctx, scope, hub.TagRef{ID: 1}, &newName, nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListPeers(ctx, scope, hub.PeerQuery{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Peers[0].Tags) != 1 || page.Peers[0].Tags[0] != newName {
		t.Fatalf("expected the repaired legacy record and no new peer: %#v", page)
	}
}
