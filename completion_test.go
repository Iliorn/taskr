package main

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	return buf.String()
}

var flagLine = regexp.MustCompile(`(?m)^\s+-([a-zA-Z][\w-]*)`)

// The completion table lists each command's flags by hand, which would rot the
// moment someone adds a flag. Every command builds a flag.FlagSet and prints it
// on --help, so ask the command itself what its flags are and compare.
func TestCompletionMatchesFlagSets(t *testing.T) {
	for _, spec := range cliCommandSpecs {
		switch spec.name {
		case "completion", "man", "help", "import":
			continue // no FlagSet of their own
		}
		t.Run(spec.name, func(t *testing.T) {
			usage := captureStderr(t, func() { dispatchCLI([]string{spec.name, "--help"}) })
			var got []string
			for _, m := range flagLine.FindAllStringSubmatch(usage, -1) {
				got = append(got, m[1])
			}
			// A command with no flags prints none; the table must agree rather
			// than claim flags the command never had.
			if len(got) == 0 && len(spec.flags) == 0 {
				return
			}
			want := append([]string(nil), spec.flags...)
			sort.Strings(got)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("flags out of sync for %q:\n  command: %v\n  table:   %v",
					spec.name, got, want)
			}
		})
	}
}

// Every command the CLI routes must be in the table, or it is missing from the
// completions and the man page.
func TestCompletionCoversEveryCommand(t *testing.T) {
	// The dispatch accepts a few aliases and top-level switches that are not
	// commands in their own right.
	skip := map[string]bool{"ls": true, "rm": true, "-h": true, "--help": true, "--version": true}
	routed := []string{
		"add", "list", "ls", "done", "top", "show", "edit", "delete", "rm", "undelete",
		"comment", "stats", "start", "stop", "log", "export", "import", "subtask",
		"search", "tags", "projects", "learnings", "serve", "sync", "undo", "doctor",
		"completion", "man", "help",
	}
	for _, name := range routed {
		if skip[name] {
			continue
		}
		if !isCLICommand(name) {
			t.Errorf("%q is in the routed list but isCLICommand says otherwise", name)
		}
		if _, ok := specFor(name); !ok {
			t.Errorf("%q is a real command but has no completion/man entry", name)
		}
	}
	for _, spec := range cliCommandSpecs {
		if !isCLICommand(spec.name) {
			t.Errorf("the table lists %q, which the CLI does not route", spec.name)
		}
	}
}

func TestCompletionScriptsMentionEveryCommand(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		var script string
		out := captureStdout(t, func() {
			if rc := cliCompletion([]string{shell}); rc != 0 {
				t.Errorf("taskr completion %s exited %d", shell, rc)
			}
		})
		script = out
		if script == "" {
			t.Fatalf("%s completion was empty", shell)
		}
		for _, spec := range cliCommandSpecs {
			if !strings.Contains(script, spec.name) {
				t.Errorf("%s completion is missing the %q command", shell, spec.name)
			}
		}
	}
	if rc := cliCompletion([]string{"tcsh"}); rc == 0 {
		t.Error("an unknown shell should be a usage error")
	}
	if rc := cliCompletion(nil); rc == 0 {
		t.Error("no shell argument should be a usage error")
	}
}

// The man page has to stay valid roff: a summary starting with a dot or holding
// a backslash would otherwise be read as a request.
func TestManPageStructure(t *testing.T) {
	page := manPage()
	for _, want := range []string{".TH TASKR 1", ".SH NAME", ".SH SYNOPSIS", ".SH DESCRIPTION",
		".SH COMMANDS", ".SH FILES", ".SH ENVIRONMENT", ".SH EXIT STATUS"} {
		if !strings.Contains(page, want) {
			t.Errorf("man page is missing %q", want)
		}
	}
	for _, spec := range cliCommandSpecs {
		if !strings.Contains(page, ".B taskr "+spec.name+"\n") {
			t.Errorf("man page is missing the %q command", spec.name)
		}
	}
	for i, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, ".") && !regexp.MustCompile(`^\.[A-Za-z]{1,3}\b`).MatchString(line) {
			t.Errorf("line %d looks like a broken roff request: %q", i+1, line)
		}
	}
	if rc := cliMan([]string{"extra"}); rc == 0 {
		t.Error("taskr man takes no arguments")
	}
}
