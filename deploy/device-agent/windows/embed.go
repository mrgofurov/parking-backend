// Package winsetup carries the Windows install scripts inside the agent
// executable, so a configured parking-agent-setup.exe installs itself
// without a zip to unpack.
package winsetup

import _ "embed"

//go:embed install.ps1
var InstallPS1 []byte

//go:embed uninstall.ps1
var UninstallPS1 []byte
