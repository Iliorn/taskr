//go:build windows

package main

import "testing"

// Everything a reminder toast needs short of showing it: the XmlDocument and
// its IXmlDocumentIO, Windows' own parse of toastXML, the ToastNotification
// factory and the notifier for an AppUserModelID. Each step names an
// interface by its ID and calls a method by its slot, so a wrong one fails
// here rather than silently on a desktop. Show is left out: it needs an
// interactive session, and a test run should not put toasts on the screen.
func TestWindowsToastUpToShow(t *testing.T) {
	err := withWinRT(func() error {
		toast, err := newToastNotification(toastXML("tjek: 1 due today", "• Pay <rent> & \"bills\"\n• Second"))
		if err != nil {
			return err
		}
		defer comRelease(toast)
		if toast == nil {
			t.Error("CreateToastNotification returned no notification")
		}
		notifier, err := newToastNotifier(toastAppID)
		if err != nil {
			return err
		}
		defer comRelease(notifier)
		if notifier == nil {
			t.Error("CreateToastNotifierWithId returned no notifier")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
