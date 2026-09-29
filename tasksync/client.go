package tasksync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Iliorn/tjek/hlc"
	"github.com/Iliorn/tjek/todo"
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
//
// clientVersion is this build's tjek version, used only to describe a
// version gap in an error — pass "" from anywhere that has no version stamp
// and the comparison is skipped rather than guessed at.
// board is this device's column list when it shares one, nil when it does not
// — see board.go.
func PostSync(serverURL, token, clientVersion string, tasks []todo.Todo, board *Board, timeout time.Duration) (Response, error) {
	body, err := json.Marshal(Request{Tasks: tasks, Board: board, Protocol: ProtocolVersion})
	if err != nil {
		return Response{}, err
	}
	endpoint := strings.TrimRight(serverURL, "/") + "/v1/sync"
	client := &http.Client{Timeout: timeout, Transport: syncTransport}
	compressed := serverAcceptsGzip(endpoint)
	resp, err := postSyncBody(client, endpoint, token, body, compressed)
	if err != nil {
		return Response{}, err
	}
	noteRequestEncodings(endpoint, resp)
	// The server behind this URL said it takes gzip and now refuses it —
	// rolled back to an older build, or a proxy swapped in front. Forget the
	// capability and send the same request plain, once; the answer to that
	// one is the answer.
	if compressed && (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnsupportedMediaType) {
		resp.Body.Close()
		gzipAcceptingURLs.Delete(endpoint)
		resp, err = postSyncBody(client, endpoint, token, body, false)
		if err != nil {
			return Response{}, err
		}
		noteRequestEncodings(endpoint, resp)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		// A version mismatch already carries a complete, actionable
		// sentence from the server; wrapping it in "server returned 409
		// Conflict" only buries the part the user has to act on.
		if resp.StatusCode == http.StatusConflict {
			return Response{}, fmt.Errorf("%s", strings.TrimSpace(string(msg)))
		}
		return Response{}, serverError(resp, clientVersion, string(msg))
	}
	var out Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Response{}, err
	}
	out.ServerVersion = resp.Header.Get(VersionHeader)
	// The server accepted our version, but it may still be newer than we
	// are: it answers in its own wire, and decoding a v2 body as v1 is the
	// silent misread this whole mechanism exists to prevent. Checking the
	// response too closes the direction the request check cannot.
	if err := negotiate(out.Protocol); err != nil {
		return Response{}, err
	}
	return out, nil
}

// postSyncBody sends one /v1/sync request, gzipping the body when compress is
// set. Accept-Encoding is left to the transport, which asks for gzip itself
// and undoes it transparently — setting it here would switch that off.
func postSyncBody(client *http.Client, endpoint, token string, body []byte, compress bool) (*http.Response, error) {
	if compress {
		z, err := gzipBytes(body)
		if err != nil {
			return nil, err
		}
		body = z
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if compress {
		req.Header.Set("Content-Encoding", "gzip")
	}
	return client.Do(req)
}

// serverError turns a non-OK sync response into the sentence the user reads.
//
// Two rules, both learned from one outage. The server's own words come first,
// because every surface that shows this eventually shortens it — a status
// footer, a toast, a log line — and "server returned 500 Internal Server
// Error" is the half that carries no information. And when the two ends are
// running different builds, that goes ahead of everything: a server left on an
// old build after the store was migrated answers every sync with a 500 whose
// body names an internal table, and the only sentence that helps names the
// versions and says which end to restart.
func serverError(resp *http.Response, clientVersion, body string) error {
	body = strings.TrimSpace(body)
	if peer := resp.Header.Get(VersionHeader); peer != "" && clientVersion != "" && peer != clientVersion {
		return fmt.Errorf("sync server runs tjek %s, this device runs %s; restart the sync server (it answered %s: %s)",
			peer, clientVersion, resp.Status, body)
	}
	return fmt.Errorf("%s (server returned %s)", body, resp.Status)
}

// VersionGapWarning returns a human warning when a *successful* sync came back
// from a server running a different tjek build. Succeeding is what makes it
// worth saying: a schema migration that only adds a column lets an older
// server keep answering 200 while dropping the new field on every round trip,
// so the mismatch never surfaces as an error — it surfaces as data quietly
// not arriving. Equal versions, or either side unknown, return "".
func VersionGapWarning(serverVersion, clientVersion string) string {
	if serverVersion == "" || clientVersion == "" || serverVersion == clientVersion {
		return ""
	}
	return fmt.Sprintf("sync server runs tjek %s, this device runs %s; restart the sync server after upgrading it, or the two can drift apart",
		serverVersion, clientVersion)
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
	return fmt.Sprintf("warning: this device's clock is about %s off from the sync server's, so edits made here can silently lose (or wrongly win) against other devices until the clock is fixed",
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
	return fmt.Sprintf("warning: %s is plain http to a public host: the sync token and your tasks travel unencrypted; prefer a Tailscale IP, or https (tjek serve --tls-cert)", rawURL)
}

// DroppedEdit is an edit made on this device that a merge did not keep: the
// local version of the task, and the units (todo.Fields keys, set member
// keys, or "deleted" for an edit to a task deleted elsewhere) whose local
// value lost.
type DroppedEdit struct {
	Local todo.Todo
	Units []string
}

// DroppedLocalEdits returns the edits made here since the last successful
// sync (`since`) that the merge did not keep: a unit set here after `since`
// that came out with another device's value, stamped later or equal (an equal
// stamp is two versions from before stamps, and the local one lost the tie),
// and an edit to a task another device deleted after it. A unit changed on one side only is never
// among them; it survives the merge. Without the baseline every remote edit
// arriving via a pull would read as a conflict, the local copy merely stale;
// a zero `since` (no sync recorded yet) lists everything that differs: when
// unsure, over-log, it is a recovery net.
func DroppedLocalEdits(local, merged []todo.Todo, since time.Time) []DroppedEdit {
	mergedByID := make(map[string]todo.Todo, len(merged))
	for _, t := range merged {
		mergedByID[t.ID] = t
	}
	editedHere := func(s hlc.Stamp) bool { return s.Time().After(since) }
	var dropped []DroppedEdit
	for _, l := range local {
		m, ok := mergedByID[l.ID]
		if !ok || l.Deleted {
			continue
		}
		if m.Deleted {
			// A deletion propagating to us is not a conflict; an edit made
			// here after it is, since the deletion still stands.
			if last := l.LatestStamp(); editedHere(last) && last.After(m.Stamp("deleted")) {
				dropped = append(dropped, DroppedEdit{Local: l, Units: []string{"deleted"}})
			}
			continue
		}
		var units []string
		for _, f := range todo.Fields {
			ls := l.Stamp(f.Key)
			if editedHere(ls) && !f.Same(&l, &m) && !ls.After(m.Stamp(f.Key)) {
				units = append(units, f.Key)
			}
		}
		for _, k := range l.SetKeys() {
			ls := l.Stamp(k)
			if editedHere(ls) && l.HasMember(k) != m.HasMember(k) && !ls.After(m.Stamp(k)) {
				units = append(units, k)
			}
		}
		if len(units) > 0 {
			dropped = append(dropped, DroppedEdit{Local: l, Units: units})
		}
	}
	return dropped
}
