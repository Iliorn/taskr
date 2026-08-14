# Security policy

## Reporting a vulnerability

Please report security issues **privately**, not as a public issue.

Use GitHub's private vulnerability reporting:
[**Report a vulnerability**](https://github.com/Iliorn/taskr/security/advisories/new).
It opens a private thread visible only to the maintainers.

Please include what `taskr doctor` prints (it never includes your sync token),
what you did, what happened, and what you expected. A proof of concept helps
but is not required to file.

taskr is a single-maintainer hobby project, so there is no paid bounty and no
guaranteed response time. Expect a first reply within about a week. If a
report is valid you will be credited in the release notes unless you would
rather not be.

## What is in scope

taskr is a local terminal application, so most of it has no attacker to defend
against: whoever can run `taskr` can already read `~/.taskr`. The parts where
that is not true:

- **The sync server** (`taskr serve`, or the in-process server enabled in
  Settings). It listens on a network socket and is the one component that
  accepts input from another machine. Authentication bypass, reading or
  writing another user's tasks, crashes triggered by a request, or anything
  reachable before the bearer-token check are all in scope.
- **The sync client**, including how it handles a hostile or compromised
  server's response.
- **The self-update path** on Linux and Windows, which downloads and replaces
  the running binary.
- **Token and file handling** — anything that writes a sync token somewhere
  it should not be, or that widens the permissions of a file under `~/.taskr`.
- **Parsing of files taskr reads**: the SQLite database, `settings.json`,
  `sync.json`, an imported export file, and the legacy `tasks.json`.

## What is not in scope

- **Plain HTTP between client and server.** taskr does not terminate TLS. The
  bearer token travels in the clear unless you put it behind a tunnel, and it
  warns you about that in Settings and in `taskr doctor`. Run the sync server
  over Tailscale, a VPN, or a reverse proxy that terminates TLS. A report that
  the token is readable on an unencrypted link is documented behaviour, not a
  vulnerability.
- **A single shared token with full access.** The sync server is single-owner
  by design: one token, no multi-tenancy, no per-device revocation. Anyone
  holding the token can read and write every task.
- **Local access.** Another process running as your user can read `~/.taskr`
  directly; taskr does not try to prevent that.
- **Denial of service against your own server** by a client holding a valid
  token.

## Handling of secrets

The only secret taskr stores is the sync bearer token, in `~/.taskr/sync.json`
with mode `0600`. It is never written to `sync.log`, never included in
`taskr doctor` output, and never logged. If you believe you have found a path
where it is exposed, that is in scope and worth reporting.

## Supported versions

Fixes land on `main` and go out in the next release. Only the latest release
is supported — there are no backports to older tags.
