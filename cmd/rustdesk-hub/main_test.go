package main

import (
	"strings"
	"testing"
)

func TestListenAndPublicURL(t *testing.T) {
	cases := []struct {
		name      string
		opts      options
		wantURL   string
		wantError string
	}{
		{
			name:    "loopback preview",
			opts:    options{Listen: "127.0.0.1:8080", PublicURL: "http://127.0.0.1:8080"},
			wantURL: "http://127.0.0.1:8080",
		},
		{
			name:    "same host HTTPS proxy",
			opts:    options{Listen: "127.0.0.1:8080", PublicURL: "https://hub.example.test:0443/"},
			wantURL: "https://hub.example.test",
		},
		{
			name:      "implicit plain HTTP on non-loopback",
			opts:      options{Listen: "192.0.2.10:8080", PublicURL: "https://hub.example.test"},
			wantError: "非回环监听",
		},
		{
			name:    "explicit private proxy upstream",
			opts:    options{Listen: "192.0.2.10:8080", PublicURL: "https://hub.example.test", BehindProxy: true},
			wantURL: "https://hub.example.test",
		},
		{
			name:    "direct TLS",
			opts:    options{Listen: ":443", PublicURL: "https://hub.example.test", TLSCert: "test.pem", TLSKey: "test.key"},
			wantURL: "https://hub.example.test",
		},
		{
			name:    "IPv6 origin",
			opts:    options{Listen: "[::1]:8080", PublicURL: "http://[0:0:0:0:0:0:0:1]:8080"},
			wantURL: "http://[::1]:8080",
		},
		{
			name:    "IDN origin",
			opts:    options{Listen: "127.0.0.1:8080", PublicURL: "https://bücher.example.test"},
			wantURL: "https://xn--bcher-kva.example.test",
		},
		{
			name:      "port stripped by the client",
			opts:      options{Listen: "127.0.0.1:8080", PublicURL: "https://hub.example.test:021114/"},
			wantError: "21114",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.validate()
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("want error containing %q, got %v", tt.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.opts.PublicURL != tt.wantURL {
				t.Fatalf("public URL = %q, want %q", tt.opts.PublicURL, tt.wantURL)
			}
		})
	}
}
