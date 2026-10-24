//go:build windows

package main

import (
	"archive/zip"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

//go:embed updater/scripts/install-windows.bat
var windowsUpdateScript []byte

func applyStagedUpdate(stagedPath string) error {
	currentExecutable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("读取当前程序路径失败：%w", err)
	}
	stageDirectory, err := os.MkdirTemp("", "atnetool-update-*")
	if err != nil {
		return fmt.Errorf("创建更新目录失败：%w", err)
	}
	newExecutable := filepath.Join(stageDirectory, "ATNetool.exe")
	if err = extractWindowsUpdateExecutable(stagedPath, newExecutable); err != nil {
		_ = os.RemoveAll(stageDirectory)
		return err
	}
	script, err := os.CreateTemp("", "atnetool-update-*.bat")
	if err != nil {
		_ = os.RemoveAll(stageDirectory)
		return fmt.Errorf("创建更新脚本失败：%w", err)
	}
	scriptPath := script.Name()
	if _, err = script.Write(windowsUpdateScript); err == nil {
		err = script.Close()
	} else {
		_ = script.Close()
	}
	if err != nil {
		_ = os.Remove(scriptPath)
		_ = os.RemoveAll(stageDirectory)
		return fmt.Errorf("写入更新脚本失败：%w", err)
	}
	command := exec.Command("cmd", "/C", "start", "", scriptPath, strconv.Itoa(os.Getpid()), newExecutable, currentExecutable, stagedPath, stageDirectory)
	if err = command.Start(); err != nil {
		_ = os.Remove(scriptPath)
		_ = os.RemoveAll(stageDirectory)
		return fmt.Errorf("启动更新脚本失败：%w", err)
	}
	return nil
}

func extractWindowsUpdateExecutable(archivePath, destination string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开 Windows 更新包失败：%w", err)
	}
	defer archive.Close()
	for _, item := range archive.File {
		if !strings.EqualFold(filepath.Base(filepath.FromSlash(item.Name)), "ATNetool.exe") || item.FileInfo().IsDir() {
			continue
		}
		reader, openErr := item.Open()
		if openErr != nil {
			return openErr
		}
		file, createErr := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
		if createErr != nil {
			reader.Close()
			return createErr
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		reader.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return fmt.Errorf("Windows 更新包中缺少 ATNetool.exe")
}
