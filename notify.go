package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

// Desktop notifications go through the tool each platform already ships, so
// taskr needs no notification library and no cgo: notify-send on Linux and
// the BSDs, osascript on macOS, and Windows PowerShell's toast API on Windows.

// sendDesktopNotification is a variable so tests can capture a notification
// instead of showing one.
var sendDesktopNotification = desktopNotify

const notifyTimeout = 10 * time.Second

func desktopNotify(title, body string) error {
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

// notifyCommand builds the platform's notification command. The texts travel
// as arguments or environment variables, never spliced into a script, so a
// task title cannot break out of the quoting.
func notifyCommand(ctx context.Context, goos, title, body string) *exec.Cmd {
	switch goos {
	case "windows":
		cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(windowsToastScript))
		cmd.Env = append(os.Environ(), "TASKR_NOTIFY_TITLE="+title, "TASKR_NOTIFY_BODY="+body)
		return cmd
	case "darwin":
		return exec.CommandContext(ctx, "osascript",
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			title, body)
	default:
		return exec.CommandContext(ctx, "notify-send", "--app-name=taskr", title, body)
	}
}

// encodePowerShell is the -EncodedCommand form of a script: base64 over
// UTF-16LE. It spares the script Windows' command-line quoting rules, which
// powershell.exe and Go's argument escaping read differently.
func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(b[2*i:], u)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// notifyFailureReason turns a failed notifier's output into one line. The
// tools answer in their own formats — Windows PowerShell writes its errors as
// a CLIXML document — and a reminder is no place for a page of markup. The one
// cause worth naming is a PowerShell an organisation has put in Constrained
// Language Mode, which blocks the WinRT calls a toast needs; anything else is
// the first readable line, clipped.
func notifyFailureReason(out []byte, err error) string {
	text := string(out)
	if strings.Contains(text, "language mode") {
		return "Windows PowerShell is in Constrained Language Mode (set by your organisation), which blocks toast notifications"
	}
	text = strings.Replace(text, "#< CLIXML", "", 1)
	text = clixmlTags.ReplaceAllString(text, "\n")
	for _, line := range strings.Split(text, "\n") {
		line = strings.Join(strings.Fields(strings.ReplaceAll(line, "_x000D__x000A_", " ")), " ")
		if line != "" {
			return truncate(line, 160)
		}
	}
	return err.Error()
}

// clixmlTags matches the markup of a PowerShell CLIXML error document.
var clixmlTags = regexp.MustCompile(`<[^>]*>`)

func notifyInstallHint(goos string) string {
	switch goos {
	case "windows", "darwin":
		return ""
	}
	return " — install libnotify (Debian/Ubuntu: apt install libnotify-bin)"
}

// windowsToastScript shows a toast under Windows PowerShell's own app id,
// which every Windows 10 and 11 install has registered; an unregistered id is
// silently dropped. It needs powershell.exe (5.1) rather than pwsh, which
// cannot load the WinRT types.
const windowsToastScript = `
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] > $null
$title = [System.Security.SecurityElement]::Escape($env:TASKR_NOTIFY_TITLE)
$body = [System.Security.SecurityElement]::Escape($env:TASKR_NOTIFY_BODY)
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml("<toast><visual><binding template=""ToastGeneric""><text>$title</text><text>$body</text></binding></visual></toast>")
$app = '{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app).Show([Windows.UI.Notifications.ToastNotification]::new($xml))
`
