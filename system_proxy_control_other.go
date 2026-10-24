//go:build !windows && !linux && !darwin

package main

import (
	"context"
	"fmt"
)

func capturePlatformSystemProxy(context.Context) (SystemProxySnapshot, error) {
	return SystemProxySnapshot{}, fmt.Errorf("当前平台暂不支持自动切换系统代理")
}

func applyPlatformSystemProxy(context.Context, string, int) error {
	return fmt.Errorf("当前平台暂不支持自动切换系统代理")
}

func restorePlatformSystemProxy(context.Context, SystemProxySnapshot) error {
	return fmt.Errorf("当前平台暂不支持自动恢复系统代理")
}

func platformSystemProxyMatches(context.Context, string, int) bool {
	return false
}
