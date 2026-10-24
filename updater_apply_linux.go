//go:build linux

package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

//go:embed updater/scripts/install-linux.sh
var linuxUpdateScript []byte

func applyStagedUpdate(stagedPath string) error {
	currentExecutable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取当前程序路径失败：%w", err)
	}
	script, err := os.CreateTemp("", "atnetool-update-*.sh")
	if err != nil {
		return fmt.Errorf("创建更新脚本失败：%w", err)
	}
	scriptPath := script.Name()
	if _, err = script.Write(linuxUpdateScript); err == nil {
		err = script.Chmod(0o700)
	}
	if closeErr := script.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("写入更新脚本失败：%w", err)
	}
	command := exec.Command(scriptPath, strconv.Itoa(os.Getpid()), stagedPath, currentExecutable)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = command.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return fmt.Errorf("启动更新脚本失败：%w", err)
	}
	return nil
}
