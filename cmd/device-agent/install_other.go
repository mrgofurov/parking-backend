//go:build !windows

package main

import "log"

// Self-install is Windows only; on Linux use deploy/device-agent (systemd).
func shouldSelfInstall(string) bool { return false }

func selfInstall(string, string) int {
	log.Println("[agent] 'install' faqat Windows uchun. Linux: deploy/device-agent/README.md (systemd)")
	return 1
}
