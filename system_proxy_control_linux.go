//go:build linux

package main

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

var linuxManagedProxyKeys = []struct {
	schema string
	key    string
}{
	{gnomeHTTPProxySchema, "host"},
	{gnomeHTTPProxySchema, "port"},
	{gnomeHTTPSProxySchema, "host"},
	{gnomeHTTPSProxySchema, "port"},
	{gnomeProxySchema, "use-same-proxy"},
	{gnomeProxySchema, "mode"},
}

func capturePlatformSystemProxy(ctx context.Context) (SystemProxySnapshot, error) {
	if _, err := exec.LookPath("gsettings"); err != nil {
		return SystemProxySnapshot{}, fmt.Errorf("当前桌面环境不支持自动切换系统代理：未找到 gsettings")
	}
	snapshot := SystemProxySnapshot{Platform: "linux", Values: make(map[string]string), Present: make(map[string]bool)}
	for _, item := range linuxManagedProxyKeys {
		value, err := readGSettingsContext(ctx, item.schema, item.key)
		if err != nil {
			return SystemProxySnapshot{}, fmt.Errorf("读取系统代理设置失败：%w", err)
		}
		id := linuxProxyKeyID(item.schema, item.key)
		snapshot.Values[id] = value
		snapshot.Present[id] = true
	}
	return snapshot, nil
}

func applyPlatformSystemProxy(ctx context.Context, host string, port int) error {
	return applyLinuxSystemProxy(ctx, host, port, writeGSettingsContext)
}

func applyLinuxSystemProxy(ctx context.Context, host string, port int, write func(context.Context, string, string, string) error) error {
	if net.ParseIP(host) == nil || port < 1 || port > 65535 {
		return fmt.Errorf("本地网关地址无效")
	}
	settings := []struct {
		schema string
		key    string
		value  string
	}{
		{gnomeHTTPProxySchema, "host", quoteGVariantString(host)},
		{gnomeHTTPProxySchema, "port", strconv.Itoa(port)},
		{gnomeHTTPSProxySchema, "host", quoteGVariantString(host)},
		{gnomeHTTPSProxySchema, "port", strconv.Itoa(port)},
		{gnomeProxySchema, "use-same-proxy", "true"},
		{gnomeProxySchema, "mode", "'manual'"},
	}
	for _, item := range settings {
		if err := write(ctx, item.schema, item.key, item.value); err != nil {
			return fmt.Errorf("写入系统代理设置失败：%w", err)
		}
	}
	return nil
}

func restorePlatformSystemProxy(ctx context.Context, snapshot SystemProxySnapshot) error {
	return restoreLinuxSystemProxy(ctx, snapshot, writeGSettingsContext)
}

func restoreLinuxSystemProxy(ctx context.Context, snapshot SystemProxySnapshot, write func(context.Context, string, string, string) error) error {
	if snapshot.Platform != "linux" {
		return fmt.Errorf("系统代理快照与当前平台不匹配")
	}
	for _, item := range linuxManagedProxyKeys {
		id := linuxProxyKeyID(item.schema, item.key)
		value, exists := snapshot.Values[id]
		if !exists {
			return fmt.Errorf("系统代理快照不完整：缺少 %s", id)
		}
		if err := write(ctx, item.schema, item.key, value); err != nil {
			return fmt.Errorf("恢复系统代理设置失败：%w", err)
		}
	}
	return nil
}

func platformSystemProxyMatches(ctx context.Context, host string, port int) bool {
	mode, err := readGSettingsContext(ctx, gnomeProxySchema, "mode")
	if err != nil || parseGVariantString(mode) != "manual" {
		return false
	}
	configuredHost, configuredPort := readGnomeProxyAddress(func(schema, key string) (string, error) {
		return readGSettingsContext(ctx, schema, key)
	}, gnomeHTTPProxySchema)
	return configuredHost == host && configuredPort == port
}

func readGSettingsContext(ctx context.Context, schema, key string) (string, error) {
	output, err := exec.CommandContext(ctx, "gsettings", "get", schema, key).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gsettings get %s %s: %s", schema, key, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func writeGSettingsContext(ctx context.Context, schema, key, value string) error {
	output, err := exec.CommandContext(ctx, "gsettings", "set", schema, key, value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gsettings set %s %s: %s", schema, key, strings.TrimSpace(string(output)))
	}
	return nil
}

func linuxProxyKeyID(schema, key string) string {
	return schema + ":" + key
}

func quoteGVariantString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `'`, `\'`)
	return "'" + value + "'"
}
