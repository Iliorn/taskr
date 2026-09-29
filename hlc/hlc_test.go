package hlc

import (
	"math/rand"
	"sort"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

// Every stamp a clock issues is later than the one before, whatever the wall
// clock does between them: forward, still, or backwards.
func TestNowIsStrictlyIncreasing(t *testing.T) {
	c := New("aaaa", "")
	prev := Stamp("")
	for i, now := range []time.Time{t0, t0, t0.Add(-time.Hour), t0.Add(time.Millisecond), t0.Add(-time.Minute)} {
		s := c.Now(now)
		if !s.After(prev) {
			t.Fatalf("stamp %d %q is not after %q", i, s, prev)
		}
		prev = s
	}
}

// An edit made after seeing another device's edit is stamped later, even on a
// device whose clock is an hour slow.
func TestObserveOrdersAfterWhatWasSeen(t *testing.T) {
	fast, slow := New("fast", ""), New("slow", "")
	seen := fast.Now(t0)
	slow.Observe(seen)
	if s := slow.Now(t0.Add(-time.Hour)); !s.After(seen) {
		t.Errorf("slow clock stamped %q, not after the %q it had seen", s, seen)
	}
}

// A clock resumed from its last stamp does not reissue it after a restart,
// even if the wall clock went back meanwhile.
func TestNewResumesAfterLast(t *testing.T) {
	c := New("aaaa", "")
	last := c.Now(t0)
	resumed := New("aaaa", last)
	if s := resumed.Now(t0.Add(-time.Second)); !s.After(last) {
		t.Errorf("resumed clock stamped %q, not after its last %q", s, last)
	}
}

// Many stamps in one millisecond stay ordered past the counter's ceiling.
func TestCounterOverflowMovesToTheNextMillisecond(t *testing.T) {
	c := New("aaaa", "")
	prev := c.Now(t0)
	for i := 0; i < maxCount+10; i++ {
		s := c.Now(t0)
		if !s.After(prev) {
			t.Fatalf("stamp %d %q is not after %q", i, s, prev)
		}
		prev = s
	}
}

// String order is moment order: sorting stamps as strings sorts them by wall
// time, then counter, then node.
func TestStringOrderIsMomentOrder(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	type moment struct {
		ms    int64
		count uint32
		node  string
	}
	var ms []moment
	var stamps []Stamp
	for i := 0; i < 500; i++ {
		m := moment{t0.UnixMilli() + int64(r.Intn(5)), uint32(r.Intn(3)), []string{"", "aaaa", "bbbb"}[r.Intn(3)]}
		ms = append(ms, m)
		stamps = append(stamps, encode(m.ms, m.count, m.node))
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i] < stamps[j] })
	sort.Slice(ms, func(i, j int) bool {
		a, b := ms[i], ms[j]
		if a.ms != b.ms {
			return a.ms < b.ms
		}
		if a.count != b.count {
			return a.count < b.count
		}
		return a.node < b.node
	})
	for i := range ms {
		if want := encode(ms[i].ms, ms[i].count, ms[i].node); stamps[i] != want {
			t.Fatalf("position %d: %q, want %q", i, stamps[i], want)
		}
	}
}

// A reconstructed moment sorts before a real edit in the same millisecond,
// and after one in an earlier millisecond.
func TestAtSortsBeforeANodeInTheSameMillisecond(t *testing.T) {
	c := New("aaaa", "")
	real := c.Now(t0)
	if At(t0).After(real) || At(t0) == real {
		t.Errorf("At(t0) %q should sort before %q", At(t0), real)
	}
	if !At(t0.Add(time.Millisecond)).After(real) {
		t.Errorf("At a millisecond later should sort after %q", real)
	}
	if At(time.Time{}) != "" {
		t.Error("a zero time has no stamp")
	}
}

func TestTimeAndMalformedStamps(t *testing.T) {
	if got := New("aaaa", "").Now(t0).Time(); !got.Equal(t0) {
		t.Errorf("Time() = %v, want %v", got, t0)
	}
	c := New("aaaa", "")
	before := c.Last()
	for _, bad := range []Stamp{"", "junk", "123.0000.x", "0000000000001.zzzz.x"} {
		c.Observe(bad)
		if !bad.Time().IsZero() {
			t.Errorf("%q has a time", bad)
		}
	}
	if c.Last() != before {
		t.Error("observing malformed stamps moved the clock")
	}
	if Max("b", "a") != "b" || Max("a", "b") != "b" {
		t.Error("Max picks the later stamp")
	}
	if len(NewNode()) != 8 {
		t.Error("NewNode is 8 hex digits")
	}
}
