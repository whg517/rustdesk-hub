package rustdesk

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const dnsLabel = `[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?`

var (
	rendezvousID = regexp.MustCompile(`^[0-9]+$|^[a-zA-Z][a-zA-Z0-9_-]*$`)
	hostName     = regexp.MustCompile(`^` + dnsLabel + `(?:\.` + dnsLabel + `)*\.?$`)
)

type ServerSettings struct {
	IDServer    string `json:"idServer" yaml:"id-server"`
	RelayServer string `json:"relayServer" yaml:"relay-server"`
	PublicKey   string `json:"publicKey" yaml:"public-key"`
}

// Setup provides the server settings displayed to authenticated web users.
type Setup struct {
	IDServer     string `json:"idServer"`
	RelayServer  string `json:"relayServer"`
	PublicKey    string `json:"publicKey"`
	ImportConfig string `json:"importConfig"`
	ipv6         bool
}

type Connection struct {
	URL    string `json:"url,omitempty"`
	Target string `json:"target"`
}

// Normalize validates the settings and adds the default ports.
func (settings ServerSettings) Normalize() (ServerSettings, error) {
	address, err := clientServerAddress(settings.IDServer, IDPort)
	if err != nil {
		return ServerSettings{}, fmt.Errorf("ID 服务器地址无效。%w", err)
	}
	normalized := ServerSettings{IDServer: address}
	if strings.TrimSpace(settings.RelayServer) != "" {
		normalized.RelayServer, err = clientServerAddress(settings.RelayServer, RelayPort)
		if err != nil {
			return ServerSettings{}, fmt.Errorf("中继服务器地址无效。%w", err)
		}
	}

	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(settings.PublicKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return ServerSettings{}, errors.New("服务端公钥须填写 id_ed25519.pub 中的内容")
	}
	normalized.PublicKey = base64.StdEncoding.EncodeToString(key)
	return normalized, nil
}

// NewSetup prepares a client profile using the Hub's validated public URL.
func NewSetup(settings ServerSettings, publicURL string) (*Setup, error) {
	settings, err := settings.Normalize()
	if err != nil {
		return nil, err
	}
	setup := &Setup{
		IDServer:    settings.IDServer,
		RelayServer: settings.RelayServer,
		PublicKey:   settings.PublicKey,
		ipv6:        strings.HasPrefix(settings.IDServer, "["),
	}

	// RustDesk 1.5.3's ServerConfig.encode reverses the padded Base64URL string.
	profile := struct {
		Host  string `json:"host"`
		Relay string `json:"relay"`
		API   string `json:"api"`
		Key   string `json:"key"`
	}{setup.IDServer, setup.RelayServer, publicURL, setup.PublicKey}
	data, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	encoded := []byte(base64.URLEncoding.EncodeToString(data))
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	setup.ImportConfig = string(encoded)
	return setup, nil
}

func clientServerAddress(value string, defaultPort uint16) (string, error) {
	host, port, err := ParseServerAddress(value, defaultPort)
	if err != nil {
		return "", err
	}
	ip, err := netip.ParseAddr(host)
	if ip.Zone() != "" || (err != nil && (len(strings.TrimSuffix(host, ".")) > 253 || !hostName.MatchString(host))) {
		return "", errors.New("请填写客户端可访问的域名或 IP 地址")
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// IsRendezvousID identifies saved values that can be looked up on this ID server.
func IsRendezvousID(id string) bool { return rendezvousID.MatchString(id) }

// ConnectionFor accepts a saved, normalized device ID. Legacy values remain
// available to copy from the address book without becoming browser links.
func (s *Setup) ConnectionFor(id string) *Connection {
	if s == nil || !IsRendezvousID(id) {
		return nil
	}
	target := id + "@" + s.IDServer
	connection := &Connection{Target: target + "?key=" + s.PublicKey}
	// RustDesk 1.5.3 reads Dart's encoded URI path, so IPv6 brackets can
	// remain escaped when passed to the connection parser.
	if s.ipv6 {
		return connection
	}
	link := url.URL{
		Scheme:   "rustdesk",
		Host:     "connect",
		Path:     "/" + target,
		RawQuery: url.Values{"key": {s.PublicKey}}.Encode(),
	}
	connection.URL = link.String()
	return connection
}
