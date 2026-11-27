//go:build windows

package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	winsetup "github.com/smart-parking/parking-backend/deploy/device-agent/windows"
)

// shouldSelfInstall: a setup .exe double-clicked from Downloads installs
// itself; the installed copy (started by the scheduled task with -config)
// never gets here, and running it by hand from Program Files just runs it.
func shouldSelfInstall(exe string) bool {
	dst := filepath.Join(os.Getenv("ProgramFiles"), "ParkingAgent")
	return !strings.EqualFold(filepath.Clean(filepath.Dir(exe)), filepath.Clean(dst))
}

const uninstallCmd = "@echo off\r\nrem Right-click -> \"Run as administrator\"\r\n" +
	"powershell -NoProfile -ExecutionPolicy Bypass -File \"%~dp0uninstall.ps1\"\r\npause\r\n"

// selfInstall unpacks the agent, its settings and install.ps1 into a temp
// folder and runs install.ps1 as Administrator (Windows asks "Yes / No").
// The result is shown in a message box: on a touch screen nobody reads the
// console.
func selfInstall(exe, env string) int {
	if env == "" {
		// "parking-agent.exe install" without embedded settings: use agent.env
		// next to the exe, the same as install.cmd would.
		b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "agent.env"))
		if err != nil {
			fmt.Println("Sozlama topilmadi: admin paneldan 'Windows o'rnatuvchi' faylini yuklab oling yoki .exe yoniga agent.env qo'ying.")
			return 1
		}
		env = string(b)
	}

	dir, err := os.MkdirTemp("", "parking-agent-setup-")
	if err != nil {
		fmt.Println("Vaqtinchalik papka ochilmadi:", err)
		return 1
	}
	defer os.RemoveAll(dir)

	files := map[string][]byte{
		"agent.env":     []byte(env),
		"install.ps1":   winsetup.InstallPS1,
		"uninstall.ps1": winsetup.UninstallPS1,
		"uninstall.cmd": []byte(uninstallCmd),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			fmt.Println("Fayl yozilmadi:", err)
			return 1
		}
	}
	if err := copyFile(exe, filepath.Join(dir, "parking-agent.exe")); err != nil {
		fmt.Println("Agent nusxalanmadi:", err)
		return 1
	}

	fmt.Println("Parking Agent o'rnatilmoqda... Windows so'rasa \"Ha\" (Yes) ni bosing.")
	ps1 := strings.ReplaceAll(filepath.Join(dir, "install.ps1"), "'", "''")
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$f = '%s'
try {
    $p = Start-Process -FilePath powershell.exe -Verb RunAs -Wait -PassThru -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', ('"' + $f + '"'), '-Gui')
    exit $p.ExitCode
} catch {
    Add-Type -AssemblyName System.Windows.Forms
    [System.Windows.Forms.MessageBox]::Show("O'rnatish bekor qilindi: administrator ruxsati berilmadi.", 'Parking Agent', 'OK', 'Warning') | Out-Null
    exit 1
}`, ps1)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodePS(script))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Println("PowerShell ishga tushmadi:", err)
		return 1
	}
	return 0
}

// encodePS: -EncodedCommand takes UTF-16LE base64, so no quoting problems
// with spaces or apostrophes in the temp path (C:\Users\O'Brien\...).
func encodePS(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
