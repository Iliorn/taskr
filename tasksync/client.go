package tasksync

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"taskr/todo"
)

// syncTransport is shared by every sync round trip and the SSE listener. The
// short dial timeout is the point: connecting to an unreachable server (a
// Tailscale peer that's offline blackholes the SYN rather than refusing it)
// must fail in seconds, not eat the whole request timeout — otherwise every
// mutating CLI command stalls its full 10s on a laptop that's off the network.
// Sharing one transport also reuses keep-alive connections across the periodic
// syncs instead of re-dialing every tick.
var syncTransport = &http.Transport{
	DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
}

// PostSync pushes tasks to the server at serverURL and returns its response:
// the merged authoritative set plus the server's clock reading.
func PostSync(serverURL, token string, tasks []todo.Todo, timeout time.Duration) (Response, error) {
	body, err := json.Marshal(Request{Tasks: tasks})
	if err != nil {
		return Response{}, err
	}
	endpoint := strings.TrimRight(serverURL, "/") + "/v1/sync"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: timeout, Transport: syncTransport}).Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Response{}, fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Response{}, err
	}
	return out, nil
}

// ClockSkewWarning returns a human warning when this device's clock and the
// server's differ by more than maxClientClockSkew. The LWW merge orders edits
// by device wall clocks: a clock behind silently loses every conflict it
// touches, a clock ahead wrongly wins (until the server's clamp catches the
// worst of it) — and no error ever surfaces either way. A zero serverTime (a
// server from before the field existed) skips the check. The measurement
// includes the network round trip, which is noise at a five-minute threshold.
func ClockSkewWarning(serverTime, now time.Time) string {
	if serverTime.IsZero() {
		return ""
	}
	skew := now.Sub(serverTime)
	if skew < 0 {
		skew = -skew
	}
	if skew <= maxClientClockSkew {
		return ""
	}
	return fmt.Sprintf("warning: this device's clock is about %s off from the sync server's — edits made here can silently lose (or wrongly win) against other devices until the clock is fixed",
		skew.Round(time.Minute))
}

// InsecureURLWarning returns a human warning when rawURL sends the bearer
// token in cleartext somewhere it could actually be sniffed: plain http to a
// host that is not loopback, RFC1918/link-local private, Tailscale CGNAT
// (100.64/10), or a *.ts.net name. https and private transports return "".
// Empty/unparseable URLs return "" too — reachability errors surface later,
// on the sync itself; this is only about the token's exposure.
func InsecureURLWarning(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" {
		return ""
	}
	host := u.Hostname()
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".ts.net") {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return ""
		}
		// Tailscale's CGNAT range 100.64.0.0/10 — private in practice.
		if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1] >= 64 && ip4[1] < 128 {
			return ""
		}
	}
	return fmt.Sprintf("warning: %s is plain http to a public host — the sync token and your tasks travel unencrypted; prefer a Tailscale IP or an https reverse proxy", rawURL)
}

// DroppedLocalEdits returns the local versions of tasks whose scalar fields
// were overwritten by the merge — a local edit that lost last-writer-wins.
// Only tasks the client had live are considered, and only ones actually
// modified here since the last successful sync (`since`). Without that
// baseline, every remote edit arriving via a pull read as a "conflict" — the
// local copy differs from the merged result, but it's merely stale, nothing
// was lost. A zero `since` (no sync recorded yet) logs everything: when
// unsure, over-log — it's a recovery net.
func DroppedLocalEdits(local, merged []todo.Todo, since time.Time) []todo.Todo {
	mergedByID := make(map[string]todo.Todo, len(merged))
	for _, t := range merged {
		mergedByID[t.ID] = t
	}
	var dropped []todo.Todo
	for _, l := range local {
		if l.Deleted {
			continue
		}
		if !l.ModifiedAt.After(since) {
			// Untouched here since the last sync: an overwrite is inbound
			// propagation of another device's edit, not a lost local one.
			continue
		}
		m, ok := mergedByID[l.ID]
		if !ok {
			continue
		}
		if m.Deleted {
			// The authoritative version is a tombstone while we still had it
			// live: another device deleted it. That's only a genuine dropped
			// edit if our copy was modified *after* the deletion (an edit that
			// lost to a delete). A plain deletion propagating to us is not a
			// conflict — surfacing it as one nags on every remote delete.
			if l.ModifiedAt.After(m.DeletedAt) {
				dropped = append(dropped, l)
			}
			continue
		}
		if scalarHash(l) != scalarHash(m) {
			dropped = append(dropped, l)
		}
	}
	return dropped
}

// scalarHash hashes only the conflict-relevant scalar fields of a task (not
// children, tags or deps, which merge independently) so DroppedLocalEdits can
// tell whether the authoritative version replaced the local one.
func scalarHash(t todo.Todo) [32]byte {
	key := struct {
		Title      string
		Status     todo.Status
		Priority   todo.Priority
		Size       todo.Size
		Project    string
		Notes      string
		ParentID   string
		Recurrence string
		Due        time.Time
		Start      time.Time
		Completed  time.Time
		Deleted    bool
	}{t.Title, t.Status, t.Priority, t.Size, t.Project, t.Notes, t.ParentID,
		t.Recurrence, t.DueDate, t.StartDate, t.CompletedAt, t.Deleted}
	b, _ := json.Marshal(key)
	return sha256.Sum256(b)
}
