package httpapi

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// CanonicalOrigin uses the same host and port normalization for the configured address and browser requests.
func CanonicalOrigin(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil ||
		(u.Scheme != "https" && u.Scheme != "http") ||
		(u.EscapedPath() != "" && u.EscapedPath() != "/") ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", errors.New("须为 HTTP 或 HTTPS 地址，只包含主机和可选端口")
	}

	host := u.Hostname()
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil && ip.Zone() == "" {
		host = ip.String()
	} else {
		host, err = idna.Lookup.ToASCII(host)
		if err != nil || host == "" || strings.Contains(host, ":") {
			return "", errors.New("主机须为有效域名或 IP 地址")
		}
		host = strings.ToLower(host)
	}

	var port uint64
	if value := u.Port(); value != "" {
		port, err = strconv.ParseUint(value, 10, 16)
		if err != nil || port == 0 {
			return "", errors.New("端口须在 1 至 65535 之间")
		}
	}
	if port != 0 && (u.Scheme == "https" && port != 443 || u.Scheme == "http" && port != 80) {
		host = net.JoinHostPort(host, strconv.FormatUint(port, 10))
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host, nil
}
