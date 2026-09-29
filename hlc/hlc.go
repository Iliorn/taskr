// Package hlc is a hybrid logical clock: the stamps a sync merge orders edits
// by.
//
// A stamp is a wall-clock millisecond, a counter and the node that issued it,
// encoded so that comparing two stamps as strings compares them as moments.
// The clock never issues a stamp at or below one it has issued or seen
// (Observe), so an edit made after this device received another device's edit
// always carries the later stamp, whatever either device's wall clock says.
// Wall time keeps stamps close to real time where the clocks agree; the
// counter orders what happens within one millisecond, or while the wall clock
// lags a stamp already seen; the node breaks the tie between two devices that
// stamp the same millisecond and count.
package hlc

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Stamp is one issued moment: "WWWWWWWWWWWWW.CCCC.NODE", the Unix
// milliseconds in 13 digits, the counter in 4 hex digits and the node. The
// fixed widths are what make string order the moment order.
type Stamp string

// maxCount is the counter's ceiling within one millisecond. Reaching it moves
// the stamp to the next millisecond rather than overflowing the field.
const maxCount = 0xffff

// Clock issues stamps for one node. The zero Clock is not usable; build one
// with New.
type Clock struct {
	node  string
	wall  int64 // milliseconds of the latest stamp issued or seen
	count uint32
}

// New is a clock for node that resumes after last, the latest stamp the node
// issued or saw before (empty for none), so a restart never reissues a stamp.
func New(node string, last Stamp) *Clock {
	c := &Clock{node: node}
	c.Observe(last)
	return c
}

// NewNode is a random node name for a device that has none.
func NewNode() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail on the platforms tjek runs on; the time is
		// a node name that is still unlikely to collide.
		return fmt.Sprintf("%08x", uint32(time.Now().UnixNano()))
	}
	return hex.EncodeToString(b)
}

// Now issues a stamp later than every stamp the clock has issued or seen, at
// the wall time now when that is later still.
func (c *Clock) Now(now time.Time) Stamp {
	ms := now.UnixMilli()
	switch {
	case ms > c.wall:
		c.wall, c.count = ms, 0
	case c.count < maxCount:
		c.count++
	default:
		c.wall, c.count = c.wall+1, 0
	}
	return c.Last()
}

// Observe moves the clock past s, a stamp from another node or from the
// store, so the next stamp it issues is later. A malformed stamp is ignored.
func (c *Clock) Observe(s Stamp) {
	ms, count, _, ok := s.parts()
	if !ok {
		return
	}
	if ms > c.wall || (ms == c.wall && count > c.count) {
		c.wall, c.count = ms, count
	}
}

// Last is the latest stamp the clock has issued or seen, carrying this node:
// what New takes to resume.
func (c *Clock) Last() Stamp { return encode(c.wall, c.count, c.node) }

// Node is the name the clock stamps with.
func (c *Clock) Node() string { return c.node }

// At is the stamp for a moment recorded before stamps existed: the wall time
// t, counter zero and no node. It sorts before every stamp a node issues in
// the same millisecond, so a real edit beats a reconstructed one.
func At(t time.Time) Stamp {
	if t.IsZero() {
		return ""
	}
	return encode(t.UnixMilli(), 0, "")
}

// Time is the wall-clock part of s, zero for a malformed stamp.
func (s Stamp) Time() time.Time {
	ms, _, _, ok := s.parts()
	if !ok {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// After reports whether s is a later moment than t. The empty stamp is
// earlier than every other.
func (s Stamp) After(t Stamp) bool { return s > t }

// Max is the later of s and t.
func Max(s, t Stamp) Stamp {
	if t > s {
		return t
	}
	return s
}

func encode(ms int64, count uint32, node string) Stamp {
	if ms < 0 {
		ms = 0
	}
	return Stamp(fmt.Sprintf("%013d.%04x.%s", ms, count, node))
}

func (s Stamp) parts() (ms int64, count uint32, node string, ok bool) {
	f := strings.SplitN(string(s), ".", 3)
	if len(f) != 3 || len(f[0]) != 13 || len(f[1]) != 4 {
		return 0, 0, "", false
	}
	ms, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return 0, 0, "", false
	}
	c, err := strconv.ParseUint(f[1], 16, 32)
	if err != nil {
		return 0, 0, "", false
	}
	return ms, uint32(c), f[2], true
}
