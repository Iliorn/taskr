package main

import (
	"testing"

	"taskr/todo"
)

func TestComputeLayout(t *testing.T) {
	tests := []struct {
		name        string
		input       layoutInput
		wantDetailH int
	}{
		{
			name: "basic layout",
			input: layoutInput{
				termW:       120,
				termH:       40,
				mode:        modeNormal,
				tab:         tabTasks,
				detailLines: 10,
			},
			wantDetailH: 10,
		},
		{
			name: "stats tab no detail",
			input: layoutInput{
				termW:       120,
				termH:       40,
				mode:        modeNormal,
				tab:         tabStats,
				detailLines: 10,
			},
			wantDetailH: 0,
		},
		{
			name: "input mode no detail",
			input: layoutInput{
				termW:       120,
				termH:       40,
				mode:        modeInput,
				tab:         tabTasks,
				detailLines: 10,
			},
			wantDetailH: 0,
		},
		{
			name: "very small terminal",
			input: layoutInput{
				termW:       40,
				termH:       10,
				mode:        modeNormal,
				tab:         tabTasks,
				detailLines: 20,
			},
		},
		{
			name: "detail capped at max pct",
			input: layoutInput{
				termW:       120,
				termH:       30,
				mode:        modeNormal,
				tab:         tabTasks,
				detailLines: 100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := computeLayout(tt.input)

			if l.contentW != tt.input.termW-4 {
				t.Errorf("contentW = %d, want %d", l.contentW, tt.input.termW-4)
			}
			if l.listH < minListHeight {
				t.Errorf("listH = %d, below minimum %d", l.listH, minListHeight)
			}
			if tt.wantDetailH > 0 && l.detailH != tt.wantDetailH {
				t.Errorf("detailH = %d, want %d", l.detailH, tt.wantDetailH)
			}

			total := l.headerH + l.listH + l.detailH + l.footerH
			if total > tt.input.termH+minListHeight {
				t.Errorf("total layout %d exceeds terminal height %d (with minList tolerance)",
					total, tt.input.termH)
			}
		})
	}
}

func TestComputeLayoutContentWidth(t *testing.T) {
	l := computeLayout(layoutInput{termW: 100, termH: 40, mode: modeNormal, tab: tabTasks})
	if l.contentW != 96 {
		t.Errorf("contentW = %d, want 96", l.contentW)
	}
}

// The header is now a fixed two rows (tab bar + one status line) — filters and
// toasts render into that single status line instead of stacking their own
// rows, so the header height never varies and the list never reflows.
func TestComputeLayoutHeaderFixed(t *testing.T) {
	l := computeLayout(layoutInput{termW: 100, termH: 40, mode: modeNormal, tab: tabTasks})
	if l.headerH != minHeaderLines {
		t.Errorf("headerH = %d, want fixed %d", l.headerH, minHeaderLines)
	}
}

// When the detail pane is hidden (pane != paneDetail on tabs that can hide
// it), estimateListHeight and listVisible must no longer reserve the 12-line
// detail block — otherwise the task list silently caps at termH-12 instead of
// filling the window. Backlog item bbd963df.
func TestListHeightFillsWhenDetailHidden(t *testing.T) {
	m := modelWithTasks(t, todo.New("a"), todo.New("b"))
	m.termHeight = 40

	m.pane = paneDetail
	withDetail := m.estimateListHeight()
	withDetailVisible := m.listVisible()

	m.pane = paneList
	withoutDetail := m.estimateListHeight()
	withoutDetailVisible := m.listVisible()

	if withoutDetail <= withDetail {
		t.Errorf("estimateListHeight: hiding the pane did not grow the list: with=%d without=%d",
			withDetail, withoutDetail)
	}
	if withoutDetailVisible <= withDetailVisible {
		t.Errorf("listVisible: hiding the pane did not grow the list: with=%d without=%d",
			withDetailVisible, withoutDetailVisible)
	}
}
