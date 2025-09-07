package system

import (
	"os"
	"os/exec"
)

// Check if the platform has systemd available
func HasSystemd() bool {
	if _, err := exec.LookPath("systemctl"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/systemd/system"); err == nil {
		return true
	}

	return false
}

type Systemd struct {
}

var _ ServiceHandler = (*Systemd)(nil)

// Stops service
func (s *Systemd) Stop(name string) (err error) {
	return exec.Command("/usr/bin/systemctl", "stop", name).Run()
}

// Starts the service
func (s *Systemd) Restart(name string) (err error) {
	return exec.Command("/usr/bin/systemctl", "restart", name).Run()
}

// Enables the service
func (s *Systemd) Enable(name string) (err error) {
	return exec.Command("/usr/bin/systemctl", "enable", name).Run()
}

// Disables the service
func (s *Systemd) Disable(name string) (err error) {
	return exec.Command("/usr/bin/systemctl", "disable", name).Run()
}

// Starts the service
func (s *Systemd) DaemonReload() (err error) {
	return exec.Command("/usr/bin/systemctl", "daemon-reload").Run()
}
