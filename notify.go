package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"git.sr.ht/~jackmordaunt/go-toast/v2/wintoast"
)

// Desktop notifications go through what each platform already has, so taskr
// needs no cgo: notify-send on Linux and the BSDs, osascript on macOS, and on
// Windows the toast API called directly over COM (wintoast). Windows does not
// go through PowerShell: an organisation can lock PowerShell into Constrained
// Language Mode, which blocks the WinRT calls a toast script makes, while a
// program calling the API itself is an ordinary desktop app.

// sendDesktopNotification is a variable so tests can capture a notification
// instead of showing one.
var sendDesktopNotification = desktopNotify

const notifyTimeout = 10 * time.Second

func desktopNotify(title, body string) error {
	if runtime.GOOS == "windows" {
		return windowsToast(title, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
	defer cancel()
	cmd := notifyCommand(ctx, runtime.GOOS, title, body)
	if errors.Is(cmd.Err, exec.ErrNotFound) {
		return fmt.Errorf("%s not found%s", cmd.Args[0], notifyInstallHint(runtime.GOOS))
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", cmd.Args[0], notifyFailureReason(out, err))
	}
	return nil
}

// notifyCommand builds the notification command on the platforms that have
// one. The texts travel as arguments, never spliced into a script, so a task
// title cannot break out of the quoting.
func notifyCommand(ctx context.Context, goos, title, body string) *exec.Cmd {
	if goos == "darwin" {
		return exec.CommandContext(ctx, "osascript",
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			title, body)
	}
	return exec.CommandContext(ctx, "notify-send", "--app-name=taskr", title, body)
}

// notifyFailureReason turns a failed notifier's output into one line: the
// first readable line, clipped, so a reminder never carries a page of tool
// output. A tool that failed silently is reported by its exit status.
func notifyFailureReason(out []byte, err error) string {
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			return truncate(line, 160)
		}
	}
	return err.Error()
}

func notifyInstallHint(goos string) string {
	if goos == "darwin" {
		return ""
	}
	return " — install libnotify (Debian/Ubuntu: apt install libnotify-bin)"
}

// Windows shows a toast only for an app it knows by an AppUserModelID.
// wintoast registers taskr's under the current user
// (HKCU\Software\Classes\AppUserModelId\taskr: a display name, and the class
// its click callback would use, fixed here so it is taskr's own). That is the
// per-user registration any desktop app makes and needs no administrator.
const (
	toastAppID = "taskr"
	toastGUID  = "{9A35CAD8-E3DC-4CBA-A96C-6BE92FDAC825}"
)

func windowsToast(title, body string) error {
	// The runtime is initialised once for the process; holding the thread for
	// the call keeps the registration and the push on the one it started on.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := wintoast.SetAppData(wintoast.AppData{AppID: toastAppID, GUID: toastGUID}); err != nil {
		return fmt.Errorf("registering taskr for notifications: %w", err)
	}
	if err := wintoast.Push(toastAppID, toastXML(title, body)); err != nil {
		return fmt.Errorf("toast: %w", err)
	}
	return nil
}

// toastXML is the toast's content: the heading and the body as the two text
// lines of the generic template, escaped, so a title with < or & in it is
// text rather than markup.
func toastXML(title, body string) string {
	var b bytes.Buffer
	b.WriteString(`<toast><visual><binding template="ToastGeneric"><text>`)
	_ = xml.EscapeText(&b, []byte(title))
	b.WriteString(`</text><text>`)
	_ = xml.EscapeText(&b, []byte(body))
	b.WriteString(`</text></binding></visual></toast>`)
	return b.String()
}
