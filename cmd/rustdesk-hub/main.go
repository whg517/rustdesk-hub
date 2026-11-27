package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/whg517/rustdesk-hub/internal/httpapi"
	"github.com/whg517/rustdesk-hub/internal/hub"
	"github.com/whg517/rustdesk-hub/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("rustdesk-hub", "error", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.CommandLine
	opts, err := parseOptions(flags, os.Args[1:])
	if err != nil {
		return err
	}

	args := flags.Args()
	initOwner := len(args) == 2 && args[0] == "init-owner"
	if !initOwner && len(args) != 0 && (len(args) != 1 || args[0] != "serve") {
		flags.Usage()
		return errors.New("无效的命令")
	}
	var config httpapi.Config
	if !initOwner {
		config, err = serverConfig(&opts)
		if err != nil {
			return err
		}
	}

	store, err := hub.Open(opts.Database)
	if err != nil {
		return err
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hasOwner, err := store.HasOwner(ctx)
	if err != nil {
		return err
	}
	if initOwner {
		if hasOwner {
			return hub.ErrOwnerExists
		}
		password, err := readPassword()
		if err != nil {
			return err
		}
		if err := store.InitOwner(ctx, args[1], password); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "管理员账号和个人通讯录已初始化")
		return nil
	}
	if !hasOwner {
		return errors.New("请先用 init-owner 初始化账号")
	}
	if err := opts.importRustDeskSettings(ctx, store); err != nil {
		return err
	}

	return serve(ctx, store, opts, config)
}

func loopback(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func (o *options) validate() error {
	canonical, err := httpapi.CanonicalOrigin(o.PublicURL)
	if err != nil {
		return fmt.Errorf("public-url 无效。%w", err)
	}
	u, err := url.Parse(canonical)
	if err != nil {
		return err
	}
	if u.Scheme == "https" && u.Port() == "21114" {
		return errors.New("RustDesk 1.5.3 默认会移除 HTTPS 端口 21114，请使用 443 或其他端口")
	}
	listenHost, _, err := net.SplitHostPort(o.Listen)
	if err != nil {
		return errors.New("listen 须包含主机地址和端口")
	}
	if u.Scheme == "http" && (!loopback(u.Hostname()) || !loopback(listenHost)) {
		return errors.New("HTTP 预览仅支持回环地址，部署时请使用 HTTPS")
	}
	if (o.TLSCert == "") != (o.TLSKey == "") || (o.TLSCert != "" && u.Scheme != "https") {
		return errors.New("直接提供 HTTPS 时须成对设置 tls-cert 和 tls-key")
	}
	if o.BehindProxy && (u.Scheme != "https" || o.TLSCert != "") {
		return errors.New("behind-proxy 要求 HTTPS public-url 并由反向代理终止 TLS")
	}
	if u.Scheme == "https" && !loopback(listenHost) && o.TLSCert == "" && !o.BehindProxy {
		return errors.New("非回环监听须提供 TLS 证书，或通过 behind-proxy 显式配置受保护的反向代理上游")
	}
	o.PublicURL = canonical
	return nil
}

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "初始密码（至少 12 个字符） ")
		password, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(password), err
	}
	value, err := io.ReadAll(io.LimitReader(os.Stdin, 259))
	if err != nil {
		return "", err
	}
	if len(value) >= 259 {
		return "", errors.New("密码最多 256 字节")
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(value), "\n"), "\r"), nil
}

func serverConfig(opts *options) (httpapi.Config, error) {
	if err := opts.validate(); err != nil {
		return httpapi.Config{}, err
	}
	dist, err := web.Dist()
	if err != nil {
		return httpapi.Config{}, err
	}
	if err := httpapi.ValidateWebBuild(dist); err != nil {
		return httpapi.Config{}, fmt.Errorf("内嵌管理端资源不可用，请先运行 pnpm -C web build，再重新编译 Hub。%w", err)
	}
	return httpapi.Config{
		PublicURL: opts.PublicURL, WebFS: dist,
		BehindProxy: strings.HasPrefix(opts.PublicURL, "https://") && opts.TLSCert == "",
	}, nil
}

func serve(ctx context.Context, store *hub.Store, opts options, config httpapi.Config) error {
	listener, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           httpapi.New(store, config),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       time.Minute,
	}

	done := make(chan error, 1)
	go func() {
		slog.Info("RustDesk Hub listening", "listen", listener.Addr(), "publicURL", opts.PublicURL)
		if opts.TLSCert != "" {
			done <- server.ServeTLS(listener, opts.TLSCert, opts.TLSKey)
			return
		}
		done <- server.Serve(listener)
	}()

	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
