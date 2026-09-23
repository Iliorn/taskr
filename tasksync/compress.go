package tasksync

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Compression of the /v1/sync bodies.
//
// A sync sends the whole task set each way, and task JSON is repetitive
// enough that gzip takes about 87% off it (2,000 tasks: ~2 MB raw, ~250 KB
// compressed) — on a timer, on every CLI mutation, often over a phone's
// hotspot. The two directions are negotiated differently, because they fail
// differently against an older peer:
//
//   - Responses. Go's http.Transport already sends "Accept-Encoding: gzip"
//     and decompresses transparently, so every taskr client that ever shipped
//     takes a gzipped response without knowing it. The server compresses
//     whenever it is asked; nothing to negotiate.
//   - Requests. An older server decodes the body as JSON straight away, so a
//     gzipped request is a 400 there. The server therefore says it accepts
//     one — "Accept-Encoding: gzip" on its responses, which is what RFC 7694
//     defines the response header for — and a client compresses only toward a
//     URL that has said so. A URL that stops accepting (a server rolled back
//     to an older build) answers the compressed request with 400 or 415; the
//     client forgets the capability and sends that one request again plain.
//
// The events stream is never compressed: a gzip writer buffers, and a nudge
// that sits in a buffer is a nudge that does not arrive.

// maxSyncBody caps a sync request body *after* decompression. Capping only
// the bytes on the wire would let a few kilobytes of gzip expand into
// gigabytes of JSON for the decoder to hold.
const maxSyncBody = 64 << 20

// errBodyTooLarge is what the decompressing reader returns past maxSyncBody.
var errBodyTooLarge = errors.New("request body too large")

// acceptsGzip reports whether the request's Accept-Encoding admits gzip. A
// "gzip;q=0" is a refusal, not an acceptance.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		q := strings.ReplaceAll(strings.ToLower(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

// syncRequestBody returns a reader over the decoded request body, capped at
// maxSyncBody whichever encoding it arrived in. ok is false, with the status
// already written, for an encoding this server does not speak.
func syncRequestBody(w http.ResponseWriter, r *http.Request) (body io.Reader, closeFn func(), ok bool) {
	wire := http.MaxBytesReader(w, r.Body, maxSyncBody)
	switch enc := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); enc {
	case "", "identity":
		return wire, func() {}, true
	case "gzip":
		zr, err := gzip.NewReader(wire)
		if err != nil {
			http.Error(w, "bad request: gzip: "+err.Error(), http.StatusBadRequest)
			return nil, nil, false
		}
		return &cappedReader{r: zr, left: maxSyncBody}, func() { zr.Close() }, true
	default:
		http.Error(w, "unsupported Content-Encoding "+enc+" (this server accepts gzip)", http.StatusUnsupportedMediaType)
		return nil, nil, false
	}
}

// cappedReader fails with errBodyTooLarge rather than stopping quietly at the
// limit: a silent EOF would hand the decoder a truncated document and make a
// bomb look like malformed JSON.
type cappedReader struct {
	r    io.Reader
	left int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		// One byte past the cap is proof the body is over it; exactly at the
		// cap and then EOF is a body that fits.
		var one [1]byte
		if n, _ := c.r.Read(one[:]); n > 0 {
			return 0, errBodyTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

// gzipAcceptingURLs remembers, per sync endpoint, that the server there said
// it takes a gzipped request. Per process: a long-running TUI learns it on
// its first sync and compresses every one after, while a one-shot CLI sync
// sends plain — the same bytes it always sent, never a failed first attempt.
var gzipAcceptingURLs sync.Map // endpoint string → struct{}

// noteRequestEncodings records what the server at endpoint says it accepts.
// Only a response that carries the header changes anything: a response with
// none (an error from a proxy in front, say) is not evidence either way.
func noteRequestEncodings(endpoint string, resp *http.Response) {
	accepts := resp.Header.Values("Accept-Encoding")
	if len(accepts) == 0 {
		return
	}
	for _, v := range accepts {
		for _, part := range strings.Split(v, ",") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), ";")
			if strings.EqualFold(strings.TrimSpace(name), "gzip") {
				gzipAcceptingURLs.Store(endpoint, struct{}{})
				return
			}
		}
	}
	gzipAcceptingURLs.Delete(endpoint)
}

func serverAcceptsGzip(endpoint string) bool {
	_, ok := gzipAcceptingURLs.Load(endpoint)
	return ok
}

// gzipBytes compresses b at the default level. Sync bodies are at most a few
// megabytes, where the difference between levels is milliseconds.
func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
