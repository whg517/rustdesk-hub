package hbbs

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"slices"
	"testing"
	"time"
)

func TestStatusPort(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{"rustdesk.example.test", "rustdesk.example.test:21115"},
		{"rustdesk.example.test:22116", "rustdesk.example.test:22115"},
		{"2001:db8::2", "[2001:db8::2]:21115"},
		{"[2001:db8::2]:22116", "[2001:db8::2]:22115"},
		{"https://rustdesk.example.test", ""},
		{"rustdesk.example.test:1", ""},
	} {
		t.Run(test.input, func(t *testing.T) {
			client, err := New(test.input)
			if test.want == "" {
				if err == nil {
					t.Fatal("invalid ID server accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if client.address != test.want {
				t.Fatalf("query address = %q, want %q", client.address, test.want)
			}
		})
	}
}

func TestQueryMatchesOSSFrame(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}

	ids := []string{
		"000000001", "000000002", "000000003",
		"000000004", "000000005", "000000006",
		"000000007", "000000008", "000000009",
	}
	// Two-byte header for a 102-byte message, then OnlineRequest with 99 bytes of peers.
	expected := []byte{0x99, 0x01, 0xba, 0x01, 0x63}
	for _, id := range ids {
		expected = append(expected, 0x12, 0x09)
		expected = append(expected, id...)
	}

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- serveStatusFixture(listener, expected)
	}()

	client := Client{address: listener.Addr().String()}
	states, err := client.Query(t.Context(), ids)
	if fixtureErr := <-serveResult; fixtureErr != nil {
		t.Fatal(fixtureErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, false, false, false, false, false, true, true}
	if !slices.Equal(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
}

func serveStatusFixture(listener net.Listener, expected []byte) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return err
	}
	request := make([]byte, len(expected))
	if _, err := io.ReadFull(conn, request); err != nil {
		return err
	}
	if !bytes.Equal(request, expected) {
		return fmt.Errorf("unexpected request frame %x", request)
	}
	// OnlineResponse marks peers at indexes 0, 7, and 8.
	_, err = conn.Write([]byte{0x1c, 0xc2, 0x01, 0x04, 0x0a, 0x02, 0x81, 0x80})
	return err
}

func TestQueryRejectsShortBitmap(t *testing.T) {
	response := []byte{0xc2, 0x01, 0x03, 0x0a, 0x01, 0x80}
	if _, err := decodeOnline(response, 9); err == nil {
		t.Fatal("a partial bitmap must not be treated as offline peers")
	}
}
