package httpapi_test

import (
	"path/filepath"
	"testing"

	"github.com/whg517/rustdesk-hub/internal/httpapi"
	"github.com/whg517/rustdesk-hub/internal/hub"
)

const (
	usersPath      = "/api/v1/users"
	webSessionPath = "/api/v1/session"
	webBookPath    = "/api/v1/address-book"
	memberPassword = "member-test-password"
	memberRequest  = `{"username":"成员","password":"` + memberPassword + `"}`
	nativeMember   = `{"username":"成员","password":"` + memberPassword + `","type":"account"}`
)

type listedUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"isAdmin"`
	Disabled bool   `json:"disabled"`
}

func multiUserClients(t *testing.T) (client, client) {
	t.Helper()
	store, err := hub.Open(filepath.Join(t.TempDir(), "hub.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.InitOwner(t.Context(), "owner", password); err != nil {
		t.Fatal(err)
	}
	anonymous := client{handler: httpapi.New(store, httpapi.Config{PublicURL: origin})}
	return anonymous, anonymous.webLogin(t, password)
}

func TestMultipleUsersHaveIndependentBooks(t *testing.T) {
	anonymous, admin := multiUserClients(t)
	anonymous.call(t, "GET", usersPath, "", 401)
	member := decode[listedUser](t, admin.call(t, "POST", usersPath, memberRequest, 201))
	if member.ID == "" || member.ID == "1" || member.Username != "成员" || member.IsAdmin || member.Disabled {
		t.Fatalf("unexpected new user: %+v", member)
	}
	admin.call(t, "POST", usersPath, memberRequest, 409)
	users := decode[[]listedUser](t, admin.call(t, "GET", usersPath, "", 200))
	if len(users) != 2 || users[0].Username != "owner" || !users[0].IsAdmin || users[1] != member {
		t.Fatalf("unexpected user list: %+v", users)
	}

	ownerClient := anonymous.nativeLogin(t, password)
	memberClient := anonymous.nativeLoginAs(t, member.Username, memberPassword)
	memberWeb := anonymous.webLoginAs(t, member.Username, memberPassword)
	who := decode[listedUser](t, memberWeb.call(t, "GET", webSessionPath, "", 200))
	if who.Username != member.Username || who.IsAdmin {
		t.Fatalf("unexpected member session: %+v", who)
	}
	ownerGUID := decode[map[string]string](t, ownerClient.call(t, "POST", "/api/ab/personal", "", 200))["guid"]
	memberGUID := decode[map[string]string](t, memberClient.call(t, "POST", "/api/ab/personal", "", 200))["guid"]
	if ownerGUID == "" || memberGUID == "" || ownerGUID == memberGUID {
		t.Fatal("each account should have its own nonempty GUID")
	}
	admin.call(t, "POST", webBookPath+"/tags", `{"name":"我的标签","color":4283215696}`, 201)
	admin.call(t, "POST", webBookPath+"/peers", `{"id":"00123","alias":"管理员设备","tags":["我的标签"]}`, 201)
	initial := decode[page](t, memberWeb.call(t, "GET", webBookPath, "", 200))
	if initial.Total != 0 {
		t.Fatal("a new account should have an empty address book")
	}
	memberWeb.call(t, "POST", webBookPath+"/tags", `{"name":"我的标签","color":4283215696}`, 201)
	memberClient.call(t, "POST", "/api/ab/peer/add/"+memberGUID,
		`{"id":"00123","alias":"成员设备","tags":["我的标签"]}`, 200)
	memberWeb.call(t, "POST", webBookPath+"/peers", `{"id":"00456"}`, 201)
	ownerBook := decode[page](t, admin.call(t, "GET", webBookPath, "", 200))
	memberBook := decode[page](t, memberWeb.call(t, "GET", webBookPath, "", 200))
	if ownerBook.Total != 1 || ownerBook.Data[0].Alias != "管理员设备" ||
		memberBook.Total != 2 || memberBook.Data[0].Alias != "成员设备" || len(memberBook.Data[0].Tags) != 1 {
		t.Fatalf("books should be independent: owner=%+v member=%+v", ownerBook, memberBook)
	}
	memberClient.call(t, "POST", "/api/ab/peers?ab="+ownerGUID, "", 404)
	memberClient.call(t, "PUT", "/api/ab/peer/update/"+ownerGUID, `{"id":"00123","alias":"改名"}`, 404)
	ownerClient.call(t, "POST", "/api/ab/tags/"+memberGUID, "", 404)
	statuses := decode[struct{ Statuses map[string]string }](t, admin.call(t, "GET", webBookPath+"/online-status?id=00456", "", 200))
	if len(statuses.Statuses) != 0 {
		t.Fatal("status query should only consider the caller's saved devices")
	}
	memberWeb.call(t, "GET", usersPath, "", 403)
	memberWeb.call(t, "GET", "/api/v1/diagnostics", "", 403)
	memberWeb.call(t, "POST", usersPath, `{"username":"new","password":"member-test-password"}`, 403)
	memberWeb.call(t, "PATCH", usersPath+"/"+member.ID, `{"disabled":true}`, 403)
	memberWeb.call(t, "PUT", usersPath+"/"+member.ID+"/password", `{"password":"another-test-password"}`, 403)
	admin.call(t, "GET", "/api/v1/diagnostics", "", 200)
}

func TestUserAccessChangesRevokeBothKindsOfSession(t *testing.T) {
	anonymous, admin := multiUserClients(t)
	member := decode[listedUser](t, admin.call(t, "POST", usersPath, memberRequest, 201))
	userPath := usersPath + "/" + member.ID
	memberWeb := anonymous.webLoginAs(t, member.Username, memberPassword)
	memberNative := anonymous.nativeLoginAs(t, member.Username, memberPassword)
	memberWeb.call(t, "POST", webBookPath+"/peers", `{"id":"00456"}`, 201)

	noCSRF := admin
	noCSRF.csrf = ""
	noCSRF.call(t, "PATCH", userPath, `{"disabled":true}`, 403)
	admin.call(t, "PATCH", usersPath+"/1", `{"disabled":true}`, 400)
	admin.call(t, "PUT", usersPath+"/1/password", `{"password":"another-test-password"}`, 400)
	admin.call(t, "PATCH", userPath, `{"disabled":true}`, 204)
	memberWeb.call(t, "GET", webSessionPath, "", 401)
	memberNative.call(t, "POST", "/api/currentUser", "", 401)
	denied := decode[map[string]string](t, anonymous.call(t, "POST", "/api/login", nativeMember, 401))
	unknown := decode[map[string]string](t, anonymous.call(t, "POST", "/api/login",
		`{"username":"missing","password":"member-test-password","type":"account"}`, 401))
	if denied["error"] == "" || denied["error"] != unknown["error"] {
		t.Fatal("disabled and unknown accounts should have the same login error")
	}

	admin.call(t, "PATCH", userPath, `{"disabled":false}`, 204)
	memberWeb.call(t, "GET", webBookPath, "", 401)
	memberNative.call(t, "POST", "/api/ab/personal", "", 401)
	memberNative = anonymous.nativeLoginAs(t, member.Username, memberPassword)
	admin.call(t, "PUT", userPath+"/password", `{"password":"`+newPassword+`"}`, 204)
	memberNative.call(t, "POST", "/api/currentUser", "", 401)
	anonymous.call(t, "POST", "/api/login", nativeMember, 401)
	afterReset := anonymous.webLoginAs(t, member.Username, newPassword)
	data := decode[page](t, afterReset.call(t, "GET", webBookPath, "", 200))
	if data.Total != 1 || data.Data[0].ID != "00456" {
		t.Fatalf("account access changes should preserve the book: %+v", data)
	}
	admin.call(t, "GET", webSessionPath, "", 200)
}
