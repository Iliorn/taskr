# Installing taskr

The [README](../README.md#install) has the one-line install for each
platform. This page has the details.

## macOS (Homebrew)

```sh
brew install iliorn/tap/taskr
```

Homebrew builds the tagged source and installs the `taskr` command. Update
with `brew update && brew upgrade taskr`; taskr recognises a Homebrew install
and points you at that command rather than replacing Homebrew's files.

## Windows (Scoop)

```sh
scoop install https://github.com/Iliorn/taskr/releases/latest/download/taskr.json
```

That address always points at the newest release. Upgrade with
`scoop update taskr`.

Notes open in `EDITOR` if it is set (`setx EDITOR hx`), otherwise Notepad.

## A downloaded binary (Linux / Windows)

From the [Releases](https://github.com/iliorn/taskr/releases) page:

| File | Platform |
|------|----------|
| `taskr` | Linux x64 |
| `taskr-linux-arm64` | Linux arm64 (Raspberry Pi, ARM servers) |
| `taskr.exe` | Windows x64 |
| `SHA256SUMS` | checksums for the three binaries |
| `taskr.json` | Scoop manifest (Windows) |

Put it somewhere on your `PATH` (for example `~/.local/bin`). Later updates
are one step: Settings → "Update to latest release", or `taskr update`. The
download is checked against the release's `SHA256SUMS`, and nothing is
installed if it doesn't match.

## With Go

```sh
go install github.com/Iliorn/taskr@latest
```

Builds from source on any platform Go supports, including ones the release
page doesn't carry. Go checks every module against `sum.golang.org`, a public
log that cannot be rewritten afterwards, which makes this the install with the
strongest integrity guarantee.

## From source

```sh
git clone https://github.com/iliorn/taskr
cd taskr
go build -ldflags "-X main.appVersion=$(git describe --tags --abbrev=0)" -o taskr .
mv taskr ~/.local/bin/   # or anywhere on your PATH
```

## Checking a download

That a file matches what the release published:

```sh
curl -LO https://github.com/Iliorn/taskr/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
```

That it was built by this repository's release workflow. Every release
binary is signed through Sigstore and recorded in a public log:

```sh
gh attestation verify taskr --repo Iliorn/taskr
```

Release builds are reproducible: check out the tag, run the same `go build`
the [release workflow](../.github/workflows/release.yml) does, and the hashes
should match. [SECURITY.md](../SECURITY.md#the-update-path) explains what each
check does and does not prove.
