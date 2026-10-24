//go:build !windows && !linux && !darwin

package main

import "fmt"

func applyStagedUpdate(string) error {
	return fmt.Errorf("当前平台暂不支持自动更新")
}
