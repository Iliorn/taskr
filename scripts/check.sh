#!/bin/sh
# check.sh runs, locally, the checks CI fails a push on: formatting, vet,
# golangci-lint and the shuffled test suite. It exists because two lint
# findings once reached main and turned CI red for every later push — each
# of them a one-line fix that a local run would have shown before the push.
#
#   scripts/check.sh           the fast set (what the pre-push hook runs)
#   scripts/check.sh --race    also the race-detector run CI does
#
# Enable it as a pre-push hook once per clone:
#
#   git config core.hooksPath .githooks
#
# golangci-lint is required rather than skipped when missing: a gate that
# quietly drops the check that failed is not a gate. Install it from
# https://golangci-lint.run/welcome/install/, or set TASKR_CHECK_NO_LINT=1
# to skip it on purpose.
set -eu

cd "$(dirname "$0")/.."

step() { printf '==> %s\n' "$*"; }

step gofmt
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "gofmt: these files need formatting (run gofmt -w on them):" >&2
	echo "$unformatted" >&2
	exit 1
fi

step "go vet"
go vet ./...

if [ "${TASKR_CHECK_NO_LINT:-}" = "1" ]; then
	step "golangci-lint (skipped: TASKR_CHECK_NO_LINT=1)"
elif command -v golangci-lint >/dev/null 2>&1; then
	step golangci-lint
	golangci-lint run ./...
else
	echo "golangci-lint is not installed; CI runs it on every push." >&2
	echo "Install it (https://golangci-lint.run/welcome/install/) or set TASKR_CHECK_NO_LINT=1." >&2
	exit 1
fi

step "go test (shuffled, as CI runs it)"
go test -shuffle=on -timeout 5m ./...

if [ "${1:-}" = "--race" ]; then
	step "go test -race"
	go test -race -timeout 10m ./...
fi

step "all checks passed"
