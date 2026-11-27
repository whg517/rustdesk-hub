package httpapi_test

import "testing"

func TestWebReadsStayBoundToThePageSession(t *testing.T) {
	anon, admin := multiUserClients(t)
	admin.call(t, "POST", usersPath, memberRequest, 201)
	member := anon.webLoginAs(t, "成员", memberPassword)
	member.call(t, "POST", webBookPath+"/peers", `{"id":"0123","alias":"成员设备"}`, 201)

	// The browser switched cookies, while this page still holds its earlier session.
	stale := admin
	stale.cookie = member.cookie
	for _, path := range []string{webBookPath, webBookPath + "/online-status?id=0123", clientSetupPath, usersPath, rustDeskSettingsPath, "/api/v1/diagnostics"} {
		row := decode[map[string]string](t, stale.call(t, "GET", path, "", 401))
		if row["code"] != "Session.Changed" {
			t.Fatalf("expected a recoverable session change for %s, got %v", path, row)
		}
	}
	stale.call(t, "POST", webBookPath+"/peers", `{"id":"0456"}`, 403)

	initial := member
	initial.csrf = ""
	initial.call(t, "GET", webBookPath, "", 409)
	who := decode[listedUser](t, initial.call(t, "GET", webSessionPath, "", 200))
	if who.Username != "成员" {
		t.Fatal("session refresh did not pick up the active account")
	}
	book := decode[page](t, member.call(t, "GET", webBookPath, "", 200))
	if book.Total != 1 || len(book.Data) != 1 || book.Data[0].ID != "0123" {
		t.Fatalf("unexpected member address book after the switch: %+v", book)
	}
}

func TestLoginBurstIsScopedAndDoesNotBlockPasswordChange(t *testing.T) {
	anon, admin := multiUserClients(t)
	limited := anon
	limited.remoteAddr = "192.0.2.10:6500"
	const failed = `{"username":"owner","password":"","type":"account"}`
	for i := 0; i < 8; i++ {
		limited.call(t, "POST", "/api/login", failed, 401)
	}
	// A direct client cannot claim a different source using forwarding headers.
	limited.forwarded = "192.0.2.11"
	limited.remoteAddr = "192.0.2.10:6501"
	limited.call(t, "POST", webSessionPath, failed, 429)

	other := anon
	other.remoteAddr = "192.0.2.11:6500"
	other.call(t, "POST", webSessionPath, failed, 401)
	admin.remoteAddr = limited.remoteAddr
	admin.call(t, "PUT", "/api/v1/account/password",
		`{"currentPassword":"`+password+`","newPassword":"`+newPassword+`"}`, 204)
}
