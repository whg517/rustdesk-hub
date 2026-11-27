package rustdesk

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

const (
	IDPort    = 21116
	RelayPort = 21117
)

// ParseServerAddress accepts the address forms used by RustDesk's network settings.
func ParseServerAddress(value string, defaultPort uint16) (string, uint16, error) {
	address := strings.TrimSpace(value)
	host, port := address, strconv.Itoa(int(defaultPort))
	if parsedHost, parsedPort, err := net.SplitHostPort(address); err == nil {
		host, port = parsedHost, parsedPort
	} else if strings.HasPrefix(address, "[") && strings.HasSuffix(address, "]") {
		host = address[1 : len(address)-1]
		if ip, err := netip.ParseAddr(host); err != nil || !ip.Is6() {
			return "", 0, errors.New("请填写有效的服务器地址")
		}
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		host = ip.String()
	} else {
		host, err = idna.Lookup.ToASCII(host)
		if err != nil || host == "" || strings.ContainsAny(host, ":/?#@[] \t\r\n") {
			return "", 0, errors.New("请填写服务器主机名或主机名加端口")
		}
	}
	n, err := strconv.ParseUint(port, 10, 16)
	if err != nil || n == 0 {
		return "", 0, errors.New("服务器端口须在 1 至 65535 之间")
	}
	return host, uint16(n), nil
}
