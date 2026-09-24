package main

import (
	"crypto/tls"
	"fmt"
	"os"
	"sync"
	"time"
)

// servetls.go gives `taskr serve` its own TLS, for the deployments where the
// transport is not already private, without a reverse proxy in front.
// Tailscale users do not need it
// (the tunnel is encrypted); a VPS or a LAN without it does, since the bearer
// token and every task otherwise cross the network in the clear.
//
// The certificate is read through a reloader rather than once at start: the
// two sources a self-hoster actually uses — `tailscale cert` and Let's Encrypt
// — both renew every few weeks by rewriting the files in place, and a server
// that read them once would serve the expired pair until someone noticed and
// restarted it.

// certReloader serves the key pair at certFile/keyFile, re-reading it when
// either file's modification time moves. A reload that fails — a renewal
// caught halfway through writing the pair — keeps serving the last good pair
// and tries again on the next handshake, rather than failing every handshake
// until the files settle.
type certReloader struct {
	certFile, keyFile string

	mu      sync.Mutex
	cert    *tls.Certificate
	certMod time.Time
	keyMod  time.Time
	// lastErr is the most recent reload failure, kept so it is logged once
	// per distinct failure rather than once per handshake.
	lastErr string
	logf    func(format string, args ...any)
}

// newCertReloader loads the pair once, synchronously, so a wrong path or a
// mismatched key is a startup error naming the file — not a handshake failure
// the first client reports as "connection reset".
func newCertReloader(certFile, keyFile string, logf func(string, ...any)) (*certReloader, error) {
	r := &certReloader{certFile: certFile, keyFile: keyFile, logf: logf}
	certMod, keyMod, err := r.modTimes()
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS key pair (%s, %s): %w", certFile, keyFile, err)
	}
	r.cert, r.certMod, r.keyMod = &cert, certMod, keyMod
	return r, nil
}

func (r *certReloader) modTimes() (certMod, keyMod time.Time, err error) {
	ci, err := os.Stat(r.certFile)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("TLS certificate: %w", err)
	}
	ki, err := os.Stat(r.keyFile)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("TLS key: %w", err)
	}
	return ci.ModTime(), ki.ModTime(), nil
}

// getCertificate is the tls.Config hook. A stat of two files per handshake is
// cheap next to the handshake itself, and sync clients reuse connections, so
// handshakes are rare.
func (r *certReloader) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	certMod, keyMod, err := r.modTimes()
	if err == nil && (!certMod.Equal(r.certMod) || !keyMod.Equal(r.keyMod)) {
		cert, lerr := tls.LoadX509KeyPair(r.certFile, r.keyFile)
		if lerr == nil {
			r.cert, r.certMod, r.keyMod, r.lastErr = &cert, certMod, keyMod, ""
			r.logf("taskr serve: reloaded the TLS certificate from %s", r.certFile)
			return r.cert, nil
		}
		err = lerr
	}
	if err != nil && err.Error() != r.lastErr {
		r.lastErr = err.Error()
		r.logf("taskr serve: TLS certificate reload failed, still serving the previous one: %v", err)
	}
	return r.cert, nil
}

// serveTLSConfig is the server's TLS configuration: the reloading pair, and
// nothing older than TLS 1.2 — every taskr client is a Go binary, so there is
// no legacy peer to keep a weaker version around for. Go's server default is
// already 1.2; saying it here keeps it true under a GODEBUG that lowers it.
func serveTLSConfig(r *certReloader) *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: r.getCertificate,
	}
}
