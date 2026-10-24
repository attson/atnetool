//go:build windows

package main

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var windowsManagedProxyStringValues = []string{"ProxyServer", "ProxyOverride", "AutoConfigURL"}
var windowsManagedProxyIntegerValues = []string{"AutoDetect", "ProxyEnable"}

func capturePlatformSystemProxy(context.Context) (SystemProxySnapshot, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettings, registry.QUERY_VALUE)
	if err != nil {
		return SystemProxySnapshot{}, fmt.Errorf("读取系统代理设置失败：%w", err)
	}
	defer key.Close()
	snapshot := SystemProxySnapshot{
		Platform: "windows",
		Values:   make(map[string]string),
		Integers: make(map[string]uint64),
		Present:  make(map[string]bool),
	}
	for _, name := range windowsManagedProxyIntegerValues {
		if value, _, readErr := key.GetIntegerValue(name); readErr == nil {
			snapshot.Integers[name] = value
			snapshot.Present[name] = true
		}
	}
	for _, name := range windowsManagedProxyStringValues {
		if value, _, readErr := key.GetStringValue(name); readErr == nil {
			snapshot.Values[name] = value
			snapshot.Present[name] = true
		}
	}
	return snapshot, nil
}

func applyPlatformSystemProxy(_ context.Context, host string, port int) error {
	if net.ParseIP(host) == nil || port < 1 || port > 65535 {
		return fmt.Errorf("本地网关地址无效")
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsInternetSettings, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开系统代理设置失败：%w", err)
	}
	defer key.Close()
	if err = key.SetStringValue("ProxyServer", net.JoinHostPort(host, strconv.Itoa(port))); err != nil {
		return err
	}
	if err = key.DeleteValue("AutoConfigURL"); err != nil && err != registry.ErrNotExist {
		return err
	}
	if err = key.SetDWordValue("AutoDetect", 0); err != nil {
		return err
	}
	if err = key.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	notifyWindowsProxyChanged()
	return nil
}

func restorePlatformSystemProxy(_ context.Context, snapshot SystemProxySnapshot) error {
	if snapshot.Platform != "windows" {
		return fmt.Errorf("系统代理快照与当前平台不匹配")
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsInternetSettings, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开系统代理设置失败：%w", err)
	}
	defer key.Close()
	for _, name := range windowsManagedProxyStringValues {
		if snapshot.Present[name] {
			if err = key.SetStringValue(name, snapshot.Values[name]); err != nil {
				return err
			}
		} else if err = key.DeleteValue(name); err != nil && err != registry.ErrNotExist {
			return err
		}
	}
	if snapshot.Present["AutoDetect"] {
		if err = key.SetDWordValue("AutoDetect", uint32(snapshot.Integers["AutoDetect"])); err != nil {
			return err
		}
	} else if err = key.DeleteValue("AutoDetect"); err != nil && err != registry.ErrNotExist {
		return err
	}
	if snapshot.Present["ProxyEnable"] {
		err = key.SetDWordValue("ProxyEnable", uint32(snapshot.Integers["ProxyEnable"]))
	} else {
		err = key.DeleteValue("ProxyEnable")
		if err == registry.ErrNotExist {
			err = nil
		}
	}
	if err != nil {
		return err
	}
	notifyWindowsProxyChanged()
	return nil
}

func platformSystemProxyMatches(_ context.Context, host string, port int) bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettings, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	enabled, _, enabledErr := key.GetIntegerValue("ProxyEnable")
	server, _, serverErr := key.GetStringValue("ProxyServer")
	return enabledErr == nil && enabled != 0 && serverErr == nil && server == net.JoinHostPort(host, strconv.Itoa(port))
}

func notifyWindowsProxyChanged() {
	wininet := windows.NewLazySystemDLL("wininet.dll")
	setOption := wininet.NewProc("InternetSetOptionW")
	const (
		internetOptionSettingsChanged = 39
		internetOptionRefresh         = 37
	)
	_, _, _ = setOption.Call(0, internetOptionSettingsChanged, 0, 0)
	_, _, _ = setOption.Call(0, internetOptionRefresh, 0, 0)
}
