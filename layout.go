package main

import "sync"

// ── Layout calculation ────────────────────────────────────────────────────────

type layout struct {
    headerH  int
    listH    int
    detailH  int
    footerH  int
    contentW int
}

type layoutInput struct {
    termW             int
    termH             int
    hasErr            bool
    hasSearch         bool
    hasFocus          bool
    hasTagSearch      bool
    hasLearningSearch bool
    mode              appMode
    tab               tab
    detailLines       int
}

func computeLayout(in layoutInput) layout {
    l := layout{
        contentW: in.termW - 4,
    }

    l.headerH = minHeaderLines
    if in.hasErr {
        l.headerH++
    }
    if in.hasFocus {
        l.headerH++
    }
    if in.hasSearch {
        l.headerH++
    }
    if in.hasTagSearch {
        l.headerH++
    }
    if in.hasLearningSearch {
        l.headerH++
    }

    l.footerH = footerHeight

    if in.mode == modeNormal && in.tab != tabStats {
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

// ── View metrics cache ────────────────────────────────────────────────────────

type viewMetrics struct {
    termW       int
    termH       int
    taskID      string
    page        int
    listVisible int
}

func (m *model) getCachedListVisible() int {
    id := m.currentTaskID()
    if m.metrics.termW == m.termWidth &&
        m.metrics.termH == m.termHeight &&
        m.metrics.taskID == id &&
        m.metrics.page == m.detail.page {
        return m.metrics.listVisible
    }
    m.metrics.listVisible = m.listVisible()
    m.metrics.termW = m.termWidth
    m.metrics.termH = m.termHeight
    m.metrics.taskID = id
    m.metrics.page = m.detail.page
    return m.metrics.listVisible
}

// ── Detail render cache ───────────────────────────────────────────────────────

type detailRenderCache struct {
    taskID        string
    page          int
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
        rc.page == m.detail.page &&
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
    rc.page = m.detail.page
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
