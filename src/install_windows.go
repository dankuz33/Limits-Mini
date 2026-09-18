package main

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

//go:embed assets/app.ico
var installIcon []byte

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func startupEnabled() bool {
	var key uintptr
	r, _, _ := advapi32.NewProc("RegOpenKeyExW").Call(0x80000001, ptr(runKey), 0, 0x20019, uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return false
	}
	defer advapi32.NewProc("RegCloseKey").Call(key)
	var n uint32
	r, _, _ = advapi32.NewProc("RegQueryValueExW").Call(key, ptr("LimitsMini"), 0, 0, 0, uintptr(unsafe.Pointer(&n)))
	return r == 0 && n > 0
}
func setStartup(enable bool) error {
	var key uintptr
	r, _, _ := advapi32.NewProc("RegCreateKeyExW").Call(0x80000001, ptr(runKey), 0, 0, 0, 0x20006, 0, uintptr(unsafe.Pointer(&key)), 0)
	if r != 0 {
		return fmt.Errorf("registry error %d", r)
	}
	defer advapi32.NewProc("RegCloseKey").Call(key)
	if !enable {
		r, _, _ = advapi32.NewProc("RegDeleteValueW").Call(key, ptr("LimitsMini"))
		if r == 0 || r == 2 {
			return nil
		}
		return fmt.Errorf("registry error %d", r)
	}
	path, e := os.Executable()
	if e != nil {
		return e
	}
	val, _ := syscall.UTF16FromString(`"` + path + `" --startup`)
	r, _, _ = advapi32.NewProc("RegSetValueExW").Call(key, ptr("LimitsMini"), 0, 1, uintptr(unsafe.Pointer(&val[0])), uintptr(len(val)*2))
	if r != 0 {
		return fmt.Errorf("registry error %d", r)
	}
	return nil
}
func installDirectory() string    { return filepath.Join(os.Getenv("LOCALAPPDATA"), "LimitsMini") }
func installedExecutable() string { return filepath.Join(installDirectory(), "LimitsMini.exe") }
func copyFile(source, dest string) error {
	in, e := os.Open(source)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0700)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	ce := out.Close()
	if e != nil {
		return e
	}
	return ce
}
func makeShortcuts() error {
	// PowerShell is used once, only for the Windows-provided shortcut COM object.
	// No ExecutionPolicy changes, no downloads, no scripts fetched from a server.
	script := `$ErrorActionPreference='Stop'; ` +
		`$d=Join-Path $env:LOCALAPPDATA 'LimitsMini'; ` +
		`$w=New-Object -ComObject WScript.Shell; ` +
		`$p=Join-Path ([Environment]::GetFolderPath('Programs')) 'Limits Mini'; ` +
		`New-Item -ItemType Directory -Force -Path $p | Out-Null; ` +
		`foreach($f in @((Join-Path ([Environment]::GetFolderPath('Desktop')) 'Limits Mini.lnk'),(Join-Path $p 'Limits Mini.lnk'))) { ` +
		`$s=$w.CreateShortcut($f); $s.TargetPath=Join-Path $d 'LimitsMini.exe'; $s.WorkingDirectory=$d; $s.IconLocation=(Join-Path $d 'LimitsMini.ico'); $s.Save() }; ` +
		`$s=$w.CreateShortcut((Join-Path $p 'Uninstall Limits Mini.lnk')); $s.TargetPath=Join-Path $d 'LimitsMini.exe'; $s.Arguments='--uninstall'; $s.WorkingDirectory=$d; $s.Save()`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return hiddenCommand(ctx, systemExe(`WindowsPowerShell\v1.0\powershell.exe`), "-NoProfile", "-NonInteractive", "-Command", script).Run()
}
func beginInstallFromWidget() {
	source, e := os.Executable()
	if e != nil {
		return
	}
	if strings.EqualFold(filepath.Clean(source), filepath.Clean(installedExecutable())) {
		installApp()
		return
	}
	user32.NewProc("DestroyWindow").Call(app.main)
	cmd := exec.Command(source, "--install")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}
func installApp() {
	if os.Getenv("LOCALAPPDATA") == "" {
		winError(appName, "Не найдена папка локальных данных пользователя.")
		return
	}
	source, e := os.Executable()
	if e != nil {
		winError(appName, "Не удалось определить путь программы.")
		return
	}
	dest := installedExecutable()
	if !strings.EqualFold(filepath.Clean(source), filepath.Clean(dest)) {
		// Seamless in-place update: ask the already running copy to exit, then wait
		// briefly for Windows to release the executable before replacing it.
		if h, _, _ := user32.NewProc("FindWindowW").Call(ptr(className), 0); h != 0 {
			pPostMessage.Call(h, wmAppExit, 0, 0)
			deadline := time.Now().Add(4 * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(120 * time.Millisecond)
				if h2, _, _ := user32.NewProc("FindWindowW").Call(ptr(className), 0); h2 == 0 {
					break
				}
			}
		}
		if e = os.MkdirAll(installDirectory(), 0700); e != nil {
			winError(appName, "Не удалось создать папку установки.")
			return
		}
		if e = copyFile(source, dest); e != nil {
			winError(appName, "Не удалось скопировать программу. Закройте ранее запущенную версию и повторите установку.")
			return
		}
	}
	_ = os.WriteFile(filepath.Join(installDirectory(), "LimitsMini.ico"), installIcon, 0600)
	_ = os.WriteFile(filepath.Join(installDirectory(), "Uninstall.cmd"), []byte("@echo off\r\nstart \"\" \"%~dp0LimitsMini.exe\" --uninstall\r\n"), 0600)
	note := "Установлено. Ярлык Limits Mini добавлен на рабочий стол и в меню «Пуск».\r\n\r\nАвтозапуск выключен. Включить его можно правой кнопкой по виджету."
	if e = makeShortcuts(); e != nil {
		note = "Программа установлена, но Windows не разрешила создать ярлыки.\r\n\r\nЗапускайте: " + dest
	}
	winInfo(0, appName, note)
	if !strings.EqualFold(filepath.Clean(source), filepath.Clean(dest)) {
		cmd := exec.Command(dest)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd.Start()
	}
}
func uninstallApp() {
	if !winConfirm(0, appName, "Удалить Limits Mini, настройки, автозапуск и созданные ярлыки?") {
		return
	}
	// Remove only this application's own startup value.
	_ = setStartup(false)
	if h, _, _ := user32.NewProc("FindWindowW").Call(ptr(className), 0); h != 0 {
		// Ask only our window to shut down its worker and remove its tray icons.
		pPostMessage.Call(h, wmAppExit, 0, 0)
	}
	script := `$ErrorActionPreference='SilentlyContinue'; ` +
		`Remove-Item -LiteralPath (Join-Path ([Environment]::GetFolderPath('Desktop')) 'Limits Mini.lnk'); ` +
		`$p=Join-Path ([Environment]::GetFolderPath('Programs')) 'Limits Mini'; ` +
		`Remove-Item -LiteralPath (Join-Path $p 'Limits Mini.lnk'); ` +
		`Remove-Item -LiteralPath (Join-Path $p 'Uninstall Limits Mini.lnk'); ` +
		`Remove-Item -LiteralPath $p`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = hiddenCommand(ctx, systemExe(`WindowsPowerShell\v1.0\powershell.exe`), "-NoProfile", "-NonInteractive", "-Command", script).Run()
	target := installDirectory()
	// Fixed, app-owned directory only. Never remove a parent or a caller-supplied path.
	if filepath.Base(target) != "LimitsMini" || os.Getenv("LOCALAPPDATA") == "" {
		winError(appName, "Неверный путь удаления.")
		return
	}
	f, e := os.CreateTemp("", "limitsmini-uninstall-*.cmd")
	if e != nil {
		winError(appName, "Не удалось создать команду удаления.")
		return
	}
	_, _ = f.WriteString("@echo off\r\nsetlocal DisableDelayedExpansion\r\ntimeout /t 3 /nobreak >nul 2>&1\r\nrmdir /s /q \"%LIMITSMINI_REMOVE_DIR%\"\r\ndel \"%~f0\"\r\n")
	_ = f.Close()
	cmd := exec.Command(systemExe("cmd.exe"), "/D", "/C", f.Name())
	cmd.Env = append(os.Environ(), "LIMITSMINI_REMOVE_DIR="+target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if e = cmd.Start(); e != nil {
		winError(appName, "Закройте программу и удалите папку: "+target)
	}
}
