package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/whg517/rustdesk-hub/internal/hbbs"
	"github.com/whg517/rustdesk-hub/internal/hub"
	"github.com/whg517/rustdesk-hub/internal/rustdesk"
)

const defaultConfigPath = "config.yaml"

type options struct {
	Database    string                   `yaml:"db"`
	Listen      string                   `yaml:"listen"`
	PublicURL   string                   `yaml:"public-url"`
	LegacyHBBS  string                   `yaml:"hbbs"`
	LegacySetup *rustdesk.ServerSettings `yaml:"rustdesk"`
	BehindProxy bool                     `yaml:"behind-proxy"`
	TLSCert     string                   `yaml:"tls-cert"`
	TLSKey      string                   `yaml:"tls-key"`
}

func parseOptions(flags *flag.FlagSet, args []string) (options, error) {
	var opts options
	configPath := flags.String("config", defaultConfigPath, "YAML 配置文件路径，留空时跳过文件")
	flags.StringVar(&opts.Database, "db", "data/hub.sqlite3", "SQLite 文件路径")
	flags.StringVar(&opts.Listen, "listen", "127.0.0.1:8080", "监听地址")
	flags.StringVar(&opts.PublicURL, "public-url", "http://127.0.0.1:8080", "客户端和浏览器使用的服务地址")
	flags.StringVar(&opts.LegacyHBBS, "hbbs", "", "旧版在线查询地址，仅用于首次导入到系统设置")
	flags.BoolVar(&opts.BehindProxy, "behind-proxy", false, "允许通过受保护的网络为 HTTPS 反向代理提供 HTTP 上游")
	flags.StringVar(&opts.TLSCert, "tls-cert", "", "HTTPS 证书链文件")
	flags.StringVar(&opts.TLSKey, "tls-key", "", "HTTPS 私钥文件")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "用法: rustdesk-hub [选项] [serve | init-owner 账号名]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}

	overrides := make(map[string]string)
	flags.Visit(func(f *flag.Flag) {
		overrides[f.Name] = f.Value.String()
	})
	_, required := overrides["config"]
	delete(overrides, "config")
	if err := loadOptions(*configPath, required, &opts); err != nil {
		return options{}, err
	}
	for name, value := range overrides {
		if err := flags.Set(name, value); err != nil {
			return options{}, err
		}
	}
	return opts, nil
}

func (o options) importRustDeskSettings(ctx context.Context, store *hub.Store) error {
	onlineServer := strings.TrimSpace(o.LegacyHBBS)
	input := hub.RustDeskSettings{OnlineEnabled: onlineServer != "", OnlineServer: onlineServer}
	if o.LegacySetup != nil {
		input.ServerSettings = *o.LegacySetup
	}
	inserted, err := store.SeedRustDeskSettings(ctx, reuseIDServer(input))
	var invalid hub.InputError
	if errors.As(err, &invalid) {
		if input.ServerSettings != (rustdesk.ServerSettings{}) {
			if _, err := input.ServerSettings.Normalize(); err != nil {
				slog.Warn("旧客户端参数需要在系统设置中重新填写", "reason", err)
				input.ServerSettings = rustdesk.ServerSettings{}
			}
		}
		if input.OnlineEnabled {
			if _, err := hbbs.New(onlineServer); err != nil {
				slog.Warn("旧在线查询地址需要在系统设置中重新填写", "reason", err)
				input.OnlineEnabled, input.OnlineServer = false, ""
			}
		}
		inserted, err = store.SeedRustDeskSettings(ctx, reuseIDServer(input))
	}
	if err != nil {
		return fmt.Errorf("初始化 RustDesk 系统设置。%w", err)
	}
	if inserted && input != (hub.RustDeskSettings{}) {
		slog.Info("RustDesk 配置已导入系统设置")
	}
	return nil
}

func reuseIDServer(input hub.RustDeskSettings) hub.RustDeskSettings {
	clientHost, clientPort, clientErr := rustdesk.ParseServerAddress(input.IDServer, rustdesk.IDPort)
	queryHost, queryPort, queryErr := rustdesk.ParseServerAddress(input.OnlineServer, rustdesk.IDPort)
	if clientErr == nil && queryErr == nil && clientPort == queryPort && strings.EqualFold(clientHost, queryHost) {
		input.OnlineServer = ""
	}
	return input
}

func loadOptions(path string, required bool, opts *options) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if !required && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取配置文件失败。%w", err)
	}
	file := struct {
		Options      *options `yaml:",inline"`
		LegacyWebDir *string  `yaml:"web-dir"`
	}{Options: opts}
	if err := yaml.Load(data, &file, yaml.WithKnownFields()); err != nil {
		return fmt.Errorf("配置文件 %q 无效。%w", path, err)
	}
	if file.LegacyWebDir != nil {
		slog.Warn("web-dir 已弃用，管理端资源随可执行文件发布")
	}
	return nil
}
