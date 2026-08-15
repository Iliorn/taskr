package main

import (
	"net/http"
	"strings"
	"testing"
)

// synctoken_test.go and the update-host cases below cover the two places where
// taskr's security posture depends on something other than careful downstream
// code: the strength of a secret the user chose, and the origin of a binary it
// is about to run.

func TestGeneratedTokensAreStrongAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		token, err := newSyncToken()
		if err != nil {
			t.Fatalf("newSyncToken: %v", err)
		}
		if seen[token] {
			t.Fatalf("newSyncToken returned a duplicate after %d draws: %q", i, token)
		}
		seen[token] = true
		if why := weakSyncToken(token); why != "" {
			t.Errorf("a generated token was judged weak: %s", why)
		}
		// It has to survive a shell, a URL and a YAML file without quoting,
		// or it gets mangled and re-typed shorter by hand.
		if strings.ContainsAny(token, " \t\n\"'`$&|<>;\\/+=") {
			t.Errorf("generated token needs quoting somewhere: %q", token)
		}
	}
}

func TestWeakTokenJudgement(t *testing.T) {
	cases := []struct {
		name  string
		token string
		weak  bool
	}{
		{"empty is somebody else's error", "", false},
		{"a word", "hunter2", true},
		{"short random", "aB3$xQ", true},
		{"long but one class", "correcthorsebatterystaplecorrect", true},
		{"long and mixed", "correct-Horse-Battery-Staple-42", false},
	}
	for _, c := range cases {
		if got := weakSyncToken(c.token) != ""; got != c.weak {
			t.Errorf("%s: weak = %v, want %v (%q)", c.name, got, c.weak, weakSyncToken(c.token))
		}
	}
}

// The warning names the problem without ever repeating the secret — the same
// rule `taskr doctor` output follows, since it is meant to be pasteable.
func TestWeakTokenWarningNeverQuotesTheToken(t *testing.T) {
	const token = "hunter2"
	if why := weakSyncToken(token); strings.Contains(why, token) {
		t.Errorf("the warning quotes the token: %q", why)
	}
}

// ── Where an update may come from ─────────────────────────────────────────────

// The asset URL arrives inside a JSON body, so it is data, not a constant.
// Following one that points off GitHub would hand the update path to whoever
// wrote that body.
func TestUpdateRefusesAssetsFromElsewhere(t *testing.T) {
	allowed := []string{
		"https://github.com/iliorn/taskr/releases/download/v1/taskr",
		"https://objects.githubusercontent.com/foo",
		"https://release-assets.githubusercontent.com/bar", // the host GitHub moved to
	}
	for _, raw := range allowed {
		if err := checkAssetURL(raw); err != nil {
			t.Errorf("%s should be allowed: %v", raw, err)
		}
	}

	refused := []string{
		"http://github.com/iliorn/taskr/releases/download/v1/taskr", // no TLS
		"https://github.com.evil.test/taskr",                        // suffix that only looks right
		"https://evil.test/taskr",
		"https://githubXcom/taskr",
		"ftp://github.com/taskr",
		"://",
	}
	for _, raw := range refused {
		if err := checkAssetURL(raw); err == nil {
			t.Errorf("%s should be refused", raw)
		}
	}
}

// A pre-flight check on the URL is not enough on its own: the default client
// follows redirects, so a 302 off GitHub would walk straight past it.
func TestUpdateRefusesARedirectOffGitHub(t *testing.T) {
	srv := releaseServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.test/taskr", http.StatusFound)
	})

	_, err := downloadReleaseAsset(releaseAsset{Name: "taskr", URL: srv.URL + "/taskr"}, t.TempDir()+"/taskr")
	if err == nil {
		t.Fatal("a redirect to a non-GitHub host was followed")
	}
	if !strings.Contains(err.Error(), "evil.test") {
		t.Errorf("error should name the host it refused, got %v", err)
	}
}
