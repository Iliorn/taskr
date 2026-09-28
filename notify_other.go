//go:build !windows

package main

import "errors"

// windowsToast exists off Windows only so desktopNotify compiles everywhere;
// it is called only when runtime.GOOS is "windows".
func windowsToast(title, body string) error {
	return errors.New("toast notifications are Windows-only")
}
