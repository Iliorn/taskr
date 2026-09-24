# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Architecture

The architecture notes live in **[ARCHITECTURE.md](ARCHITECTURE.md)** — what
this app is, the commands, the release process, the cache-invalidation rules,
the keymap registry, the rendering width contracts. They used to live here,
which meant a human contributor met the project's own design document under a
filename that reads like private notes to a tool, and skipped it. One copy,
two audiences.

@ARCHITECTURE.md

## House rules (from global CLAUDE.md)

- Match the existing style of the file you edit; **no blanket reformatting**, and keep any formatting-only change in its own commit.
- **Comments describe the code as it is now, not how it got here.** No "it used to…", "previously…", "the old X…", or eulogies for removed features — that history goes in the commit message. Say *why* the current code is the way it is, briefly; a comment that only makes sense to someone who saw the last version is noise to everyone else.
- TokyoNight-style palette is the visual baseline.
- Share the approach and get buy-in before large multi-file or expensive changes rather than spiraling.
- After meaningful changes, remember this repo is public under GitHub user `Iliorn` (capital I — `git remote` is `https://github.com/Iliorn/taskr.git`).
