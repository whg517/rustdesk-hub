package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestLoginSourceHonorsOnlyTheAdjacentTrustedProxy(t *testing.T) {
	request := httptest.NewRequest("POST", "https://hub.example.test/api/login", nil)
	request.RemoteAddr = "[::ffff:192.0.2.1]:43123"
	request.Header.Set("X-Forwarded-For", "198.51.100.1, 198.51.100.2")
	if got := loginSource(request, true); got != "198.51.100.2" {
		t.Fatalf("trusted source = %q", got)
	}
	if got := loginSource(request, false); got != "192.0.2.1" {
		t.Fatalf("direct client source = %q", got)
	}
}
