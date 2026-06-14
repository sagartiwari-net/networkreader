//go:build windows

package main

import "os/exec"

func execCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = nil
	return cmd.Output()
}
