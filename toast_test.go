package main

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The flash helpers set text and kind together so they can't drift.
func TestFlashHelpersSetKind(t *testing.T) {
	var m model
	m.flashError("boom")
	if m.err != "boom" || m.errKind != toastError {
		t.Errorf("flashError: got (%q, %d), want (boom, error)", m.err, m.errKind)
	}
	m.flashSuccess("done")
	if m.err != "done" || m.errKind != toastSuccess {
		t.Errorf("flashSuccess: got (%q, %d), want (done, success)", m.err, m.errKind)
	}
	m.flashInfo("note")
	if m.err != "note" || m.errKind != toastInfo {
		t.Errorf("flashInfo: got (%q, %d), want (note, info)", m.err, m.errKind)
	}
}

// Clearing the toast resets the kind so the next error can't inherit a prior
// success/info colour.
func TestClearErrResetsKind(t *testing.T) {
	m := modelWithTasks(t)
	m.flashSuccess("undone")
	next, _ := m.Update(clearErrMsg{})
	m = next.(model)
	if m.err != "" || m.errKind != toastError {
		t.Errorf("after clear: got (%q, %d), want (\"\", error)", m.err, m.errKind)
	}
}

// Each kind renders the status line with a distinct style (colour), given a
// colour-capable profile.
func TestToastKindsRenderDistinctly(t *testing.T) {
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(before)
	applyTheme(themes[0]) // rebuild styles under the forced profile

	m := modelWithTasks(t)
	m.err = "identical text"

	m.errKind = toastError
	e := m.renderStatusLine()
	m.errKind = toastSuccess
	s := m.renderStatusLine()
	m.errKind = toastInfo
	i := m.renderStatusLine()

	if e == s || s == i || e == i {
		t.Errorf("toast kinds should style differently:\n error=%q\n success=%q\n info=%q", e, s, i)
	}
}
