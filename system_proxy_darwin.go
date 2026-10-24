//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type darwinProxySettings struct {
	Enabled       bool   `json:"enabled"`
	Server        string `json:"server"`
	Port          int    `json:"port"`
	Authenticated bool   `json:"authenticated"`
}

type darwinAutoProxySettings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

type darwinServiceProxySnapshot struct {
	Name   string                  `json:"name"`
	Web    darwinProxySettings     `json:"web"`
	Secure darwinProxySettings     `json:"secure"`
	Auto   darwinAutoProxySettings `json:"auto"`
}

func platformProxyForRequest(request *http.Request) (*url.URL, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "scutil", "--proxy").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	values := parseDarwinSCUtilProxy(string(output))
	prefix := "HTTP"
	if request.URL.Scheme == "https" {
		prefix = "HTTPS"
	}
	if values[prefix+"Enable"] == "1" {
		host := values[prefix+"Proxy"]
		port, _ := strconv.Atoi(values[prefix+"Port"])
		if host != "" && port > 0 {
			return validateProxyURL("http://" + net.JoinHostPort(host, strconv.Itoa(port)))
		}
	}
	if values["SOCKSEnable"] == "1" {
		host := values["SOCKSProxy"]
		port, _ := strconv.Atoi(values["SOCKSPort"])
		if host != "" && port > 0 {
			return validateProxyURL("socks5://" + net.JoinHostPort(host, strconv.Itoa(port)))
		}
	}
	if values["ProxyAutoConfigEnable"] == "1" {
		return nil, fmt.Errorf("macOS 自动代理配置（PAC）暂不支持作为上游，请改用手动代理出口")
	}
	return nil, nil
}

func capturePlatformSystemProxy(ctx context.Context) (SystemProxySnapshot, error) {
	services, err := listDarwinNetworkServices(ctx)
	if err != nil {
		return SystemProxySnapshot{}, err
	}
	if len(services) == 0 {
		return SystemProxySnapshot{}, fmt.Errorf("未找到可配置的 macOS 网络服务")
	}
	snapshots := make([]darwinServiceProxySnapshot, 0, len(services))
	for _, service := range services {
		web, readErr := readDarwinProxySettings(ctx, "-getwebproxy", service)
		if readErr != nil {
			return SystemProxySnapshot{}, readErr
		}
		secure, readErr := readDarwinProxySettings(ctx, "-getsecurewebproxy", service)
		if readErr != nil {
			return SystemProxySnapshot{}, readErr
		}
		if (web.Enabled && web.Authenticated) || (secure.Enabled && secure.Authenticated) {
			return SystemProxySnapshot{}, fmt.Errorf("网络服务 %s 使用认证系统代理，无法安全保存密码，因此未修改任何设置", service)
		}
		auto, readErr := readDarwinAutoProxySettings(ctx, service)
		if readErr != nil {
			return SystemProxySnapshot{}, readErr
		}
		snapshots = append(snapshots, darwinServiceProxySnapshot{Name: service, Web: web, Secure: secure, Auto: auto})
	}
	data, err := json.Marshal(snapshots)
	if err != nil {
		return SystemProxySnapshot{}, err
	}
	return SystemProxySnapshot{Platform: "darwin", Values: map[string]string{"services": string(data)}}, nil
}

func applyPlatformSystemProxy(ctx context.Context, host string, port int) error {
	if net.ParseIP(host) == nil || port < 1 || port > 65535 {
		return fmt.Errorf("本地网关地址无效")
	}
	services, err := listDarwinNetworkServices(ctx)
	if err != nil {
		return err
	}
	for _, service := range services {
		if err = runNetworkSetup(ctx, "-setwebproxy", service, host, strconv.Itoa(port)); err != nil {
			return err
		}
		if err = runNetworkSetup(ctx, "-setsecurewebproxy", service, host, strconv.Itoa(port)); err != nil {
			return err
		}
		if err = runNetworkSetup(ctx, "-setautoproxystate", service, "off"); err != nil {
			return err
		}
	}
	return nil
}

func restorePlatformSystemProxy(ctx context.Context, snapshot SystemProxySnapshot) error {
	if snapshot.Platform != "darwin" {
		return fmt.Errorf("系统代理快照与当前平台不匹配")
	}
	var services []darwinServiceProxySnapshot
	if err := json.Unmarshal([]byte(snapshot.Values["services"]), &services); err != nil || len(services) == 0 {
		return fmt.Errorf("macOS 系统代理快照不完整")
	}
	for _, service := range services {
		if err := restoreDarwinProxySettings(ctx, service.Name, "web", service.Web); err != nil {
			return err
		}
		if err := restoreDarwinProxySettings(ctx, service.Name, "secure", service.Secure); err != nil {
			return err
		}
		if service.Auto.URL != "" {
			if err := runNetworkSetup(ctx, "-setautoproxyurl", service.Name, service.Auto.URL); err != nil {
				return err
			}
		}
		state := "off"
		if service.Auto.Enabled {
			state = "on"
		}
		if err := runNetworkSetup(ctx, "-setautoproxystate", service.Name, state); err != nil {
			return err
		}
	}
	return nil
}

func platformSystemProxyMatches(ctx context.Context, host string, port int) bool {
	services, err := listDarwinNetworkServices(ctx)
	if err != nil || len(services) == 0 {
		return false
	}
	for _, service := range services {
		web, webErr := readDarwinProxySettings(ctx, "-getwebproxy", service)
		secure, secureErr := readDarwinProxySettings(ctx, "-getsecurewebproxy", service)
		if webErr != nil || secureErr != nil || !web.Enabled || !secure.Enabled || web.Server != host || secure.Server != host || web.Port != port || secure.Port != port {
			return false
		}
	}
	return true
}

func listDarwinNetworkServices(ctx context.Context) ([]string, error) {
	output, err := exec.CommandContext(ctx, "networksetup", "-listallnetworkservices").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("读取 macOS 网络服务失败：%s", strings.TrimSpace(string(output)))
	}
	var services []string
	for index, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if index == 0 || line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

func readDarwinProxySettings(ctx context.Context, command, service string) (darwinProxySettings, error) {
	output, err := exec.CommandContext(ctx, "networksetup", command, service).CombinedOutput()
	if err != nil {
		return darwinProxySettings{}, fmt.Errorf("读取网络服务 %s 代理设置失败：%s", service, strings.TrimSpace(string(output)))
	}
	values := parseDarwinNetworkSetup(string(output))
	port, _ := strconv.Atoi(values["Port"])
	return darwinProxySettings{
		Enabled:       strings.EqualFold(values["Enabled"], "Yes"),
		Server:        values["Server"],
		Port:          port,
		Authenticated: values["Authenticated Proxy Enabled"] == "1" || strings.EqualFold(values["Authenticated Proxy Enabled"], "Yes"),
	}, nil
}

func readDarwinAutoProxySettings(ctx context.Context, service string) (darwinAutoProxySettings, error) {
	output, err := exec.CommandContext(ctx, "networksetup", "-getautoproxyurl", service).CombinedOutput()
	if err != nil {
		return darwinAutoProxySettings{}, fmt.Errorf("读取网络服务 %s PAC 设置失败：%s", service, strings.TrimSpace(string(output)))
	}
	values := parseDarwinNetworkSetup(string(output))
	return darwinAutoProxySettings{Enabled: strings.EqualFold(values["Enabled"], "Yes"), URL: values["URL"]}, nil
}

func restoreDarwinProxySettings(ctx context.Context, service, kind string, settings darwinProxySettings) error {
	setCommand, stateCommand := "-setwebproxy", "-setwebproxystate"
	if kind == "secure" {
		setCommand, stateCommand = "-setsecurewebproxy", "-setsecurewebproxystate"
	}
	host := settings.Server
	port := settings.Port
	if host == "" || port < 1 {
		host, port = "127.0.0.1", 1
	}
	if err := runNetworkSetup(ctx, setCommand, service, host, strconv.Itoa(port)); err != nil {
		return err
	}
	state := "off"
	if settings.Enabled {
		state = "on"
	}
	return runNetworkSetup(ctx, stateCommand, service, state)
}

func runNetworkSetup(ctx context.Context, arguments ...string) error {
	output, err := exec.CommandContext(ctx, "networksetup", arguments...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("networksetup %s: %s", strings.Join(arguments, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func parseDarwinNetworkSetup(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(line, ":")
		if found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

func parseDarwinSCUtilProxy(output string) map[string]string {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && !strings.Contains(key, " ") && !strings.HasPrefix(value, "<") {
			values[key] = value
		}
	}
	return values
}
