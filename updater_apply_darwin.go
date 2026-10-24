//go:build darwin

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

//go:embed updater/scripts/install-darwin.sh
var darwinUpdateScript []byte

func applyStagedUpdate(stagedPath string) error {
	bundlePath, err := currentDarwinBundlePath()
	if err != nil {
		return err
	}
	script, err := os.CreateTemp("", "atnetool-update-*.sh")
	if err != nil {
		return fmt.Errorf("创建更新脚本失败：%w", err)
	}
	scriptPath := script.Name()
	if _, err = script.Write(darwinUpdateScript); err == nil {
		err = script.Chmod(0o700)
	}
	if closeErr := script.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("写入更新脚本失败：%w", err)
	}
	command := exec.Command(scriptPath, strconv.Itoa(os.Getpid()), stagedPath, bundlePath)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = command.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("启动更新脚本失败：%w", err)
	}
	return nil
}

func currentDarwinBundlePath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("读取当前程序路径失败：%w", err)
	}
	for cursor := filepath.Dir(executable); cursor != "." && cursor != string(filepath.Separator); cursor = filepath.Dir(cursor) {
		if filepath.Ext(cursor) == ".app" {
			return cursor, nil
		}
	}
	return "", fmt.Errorf("当前程序不在 macOS .app 中，无法自动更新")
}
