package main

import "sync"

// ── Layout calculation ────────────────────────────────────────────────────────

// nameColWidth is the width of the leading "name" column shared by the list
// tabs (task title, project name, tag, learning text). Keeping it a single
// proportional-with-clamp rule makes the gap before the next column consistent
// across tabs and makes them all reflow the same way as the terminal resizes.
func nameColWidth(termWidth int) int {
	w := termWidth * nameColWidthPct / 100
	if w < nameColMinWidth {
		w = nameColMinWidth
	}
	if w > nameColMaxWidth {
		w = nameColMaxWidth
	}
	return w
}

// contentFitWidth sizes a leading list column to its widest entry (contentMax
// runes) plus a small gap, so short titles get a tight column and long ones
// expand — but never below floor (the header label must still fit) nor above
// the shared responsive cap for the current terminal width.
func contentFitWidth(termWidth, contentMax, gap, floor int) int {
	w := contentMax + gap
	if w < floor {
		w = floor
	}
	if maxW := nameColWidth(termWidth); w > maxW {
		w = maxW
	}
	return w
}

type layout struct {
	headerH  int
	listH    int
	detailH  int
	footerH  int
	contentW int
}

type layoutInput struct {
	termW       int
	termH       int
	mode        appMode
	tab         tab
	detailLines int
}

func computeLayout(in layoutInput) layout {
	l := layout{
		contentW: in.termW - 4,
	}

	// Header is a fixed height: the tab bar plus one always-present status
	// line (filter/history chips + sync glyph). Filters and toasts render
	// into that single line instead of stacking their own rows, so the list
	// never reflows as they come and go.
	l.headerH = minHeaderLines

	l.footerH = footerHeight

	if in.mode == modeNormal {
		l.detailH = in.detailLines
		maxDetail := in.termH * detailMaxHeightPct / 100
		if l.detailH > maxDetail {
			l.detailH = maxDetail
		}
	}

	l.listH = in.termH - l.headerH - l.footerH - l.detailH
	if l.listH < minListHeight {
		l.listH = minListHeight
	}

	return l
}

// ── Detail render cache ───────────────────────────────────────────────────────

type detailRenderCache struct {
	taskID        string
	field         detailField
	tagCursor     int
	depCursor     int
	learnCursor   int
	subtaskCursor int
	commentCursor int
	termW         int
	pane          pane
	rendered      string
	valid         bool
}

func (m *model) invalidateDetailCache() {
	m.detailRC.valid = false
}

func (m *model) getCachedDetailContent() string {
	t := m.currentTodo()
	if t == nil {
		return ""
	}

	rc := &m.detailRC
	if rc.valid &&
		rc.taskID == t.ID &&
		rc.field == m.detail.field &&
		rc.tagCursor == m.detail.tagCursor &&
		rc.depCursor == m.detail.depCursor &&
		rc.learnCursor == m.detail.learningCursor &&
		rc.subtaskCursor == m.detail.subtaskCursor &&
		rc.commentCursor == m.detail.commentCursor &&
		rc.termW == m.termWidth &&
		rc.pane == m.pane {
		return rc.rendered
	}

	content := m.buildDetailContent()
	rc.taskID = t.ID
	rc.field = m.detail.field
	rc.tagCursor = m.detail.tagCursor
	rc.depCursor = m.detail.depCursor
	rc.learnCursor = m.detail.learningCursor
	rc.subtaskCursor = m.detail.subtaskCursor
	rc.commentCursor = m.detail.commentCursor
	rc.termW = m.termWidth
	rc.pane = m.pane
	rc.rendered = content
	rc.valid = true

	return content
}

// ── Gantt buffer pool ─────────────────────────────────────────────────────────

type ganttBuffers struct {
	bar   []rune
	color []int
}

var ganttBufPool = sync.Pool{
	New: func() interface{} {
		return &ganttBuffers{
			bar:   make([]rune, 256),
			color: make([]int, 256),
		}
	},
}

func getGanttBuffers(size int) *ganttBuffers {
	bufs := ganttBufPool.Get().(*ganttBuffers)
	if len(bufs.bar) < size {
		bufs.bar = make([]rune, size)
		bufs.color = make([]int, size)
	}
	return bufs
}

func putGanttBuffers(bufs *ganttBuffers) {
	ganttBufPool.Put(bufs)
}
