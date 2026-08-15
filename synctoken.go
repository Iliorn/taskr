package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"unicode"
)

// synctoken.go is about the one secret taskr holds: the sync bearer token.
//
// Everything downstream of it is careful — constant-time comparison on the
// server (tasksync/server.go), 0600 on sync.json, never logged, never in
// `taskr doctor` output. None of that matters if the token is "hunter2",
// because the endpoint is one guess away and every other protection is
// downstream of a secret the user invented under time pressure.
//
// So taskr mints one on request and says so when the configured one is short.
// It warns rather than refuses: an existing setup that works must keep working,
// and the person best placed to judge a token behind a Tailscale-only listener
// is the person who put it there.

// syncTokenBytes is the entropy in a generated token. 32 bytes is the width of
// the SHA-256 the server compares against and far past anything brute-forcible
// over a network.
const syncTokenBytes = 32

// syncTokenMinLen is the length below which a token gets a warning. It is a
// proxy for entropy, not a measure of it — a 24-character passphrase from a
// dictionary is weaker than it looks — but length is the one property that can
// be checked without pretending to score a password, and it catches the case
// that actually happens: a short word typed into a prompt.
const syncTokenMinLen = 24

// newSyncToken mints a token from the system CSPRNG. RawURLEncoding so the
// result survives a shell, a URL, a YAML file and a systemd unit without
// quoting — a token that needs escaping gets mangled and then re-typed shorter.
func newSyncToken() (string, error) {
	buf := make([]byte, syncTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("could not read random bytes for a token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// weakSyncToken reports why a token is worth replacing, or "" when it is fine.
// Callers render it as a warning; nothing refuses to run on the strength of it.
func weakSyncToken(token string) string {
	if token == "" {
		// An absent token is a different condition with its own message —
		// the server refuses to start at all — so it is not this one's to
		// report.
		return ""
	}
	if n := len([]rune(token)); n < syncTokenMinLen {
		unit := "characters"
		if n == 1 {
			unit = "character"
		}
		return fmt.Sprintf("only %d %s — anyone who can reach the endpoint can guess it; `taskr serve --new-token` mints a strong one", n, unit)
	}
	if isSingleClass(token) {
		return "one character class throughout, so it is likely a word or a phrase rather than random; `taskr serve --new-token` mints a strong one"
	}
	return ""
}

// isSingleClass reports whether a string is drawn from one character class
// (all lower, all upper, or all digits). A long token that never changes class
// is a sentence, not a secret — the length check alone would pass it.
func isSingleClass(s string) bool {
	var lower, upper, digit, other bool
	for _, r := range s {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			other = true
		}
	}
	classes := 0
	for _, present := range []bool{lower, upper, digit, other} {
		if present {
			classes++
		}
	}
	return classes <= 1
}
