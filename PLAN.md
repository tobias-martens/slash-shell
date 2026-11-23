# slash — implementation plan

Design decisions live in [CLAUDE.md](CLAUDE.md). This file is the build order.
Every stage ends usable and tested on its own.

## Language and structure (decided 2026-11-23)

Go, one static binary, following the pattern of zoxide, starship, fzf and
direnv: the user adds `eval "$(slash init bash)"` (or `zsh`/`fish`) to their
rc file; the binary prints the matching shell glue, so glue and resolver
always have the same version.

## Stage 0 — skeleton
- Layout: `cmd/slash/` (main), `internal/…` (grammar, sources, resolver),
  `shell/` (glue per shell, embedded), `data/sources/*.tsv` (command
  mappings, embedded), `tests/`, `tools/`.
- Test harness: `go test` for the resolver; bats tests for the bash glue,
  equivalent scripted tests for zsh and fish.
- CI: containers per distro (Debian, Ubuntu, Fedora, Arch, openSUSE, Alpine),
  dry-run mode, **networking disabled** for search/about tests.
- `tools/check-names.sh` in CI to catch new name collisions.

## Stage 1 — core + packages
- Source detection: apt, dnf, pacman, zypper, apk, flatpak, snap; immutable
  systems (SteamOS, Fedora Atomic) → flatpak first.
- Grammar: `<verb> <things> [as|from|via|in <source>]`, `and`, commas, `only`.
- Verbs: `update`/`upgrade`, `install`, `remove`/`uninstall`, incl. the
  coreutils `install` pass-through rule and "ask before using another source".
- sudo rules (per verb × source; never for flatpak; skip when root).
- Shell glue for bash, zsh, fish: Enter hook rewrites a slash phrase into
  `real command  #slash: phrase`; the shell runs and records it natively.
  Confirm-before-run when slash had to choose; fail open on any error.
  Layer 2 fallback functions per verb for compound lines and setups without
  a usable Enter hook: print `→ real command`, run, append to history.
- Installer/uninstaller (per user, one guarded block in the rc file): record
  existing collisions, report them, "newest install wins" at rewrite time.
  Uninstall restores everything exactly (CI-tested: rc file byte-identical,
  taken-over commands work as before).
- `slash` intro screen; `slash doctor`; `slash resolve` (dry run).

## Stage 2 — search and about (offline only)
- `search`: files (`plocate`/`locate`, else `find` in home) + packages from
  local catalogs only; case-insensitive substring; `in files|packages|<source>`.
- `about`: compact per-source summary from script-stable commands.
- No local data → "run `update` first", never fetch.

## Stage 3 — show, start, stop, restart
- `show` with its fixed list of things; unknown thing → "I can show: …".
- Services vs programs, ask on ambiguity; graceful stop, ask before force.
- Bare `stop`/`restart` lists candidates; tab completion in all three shells.

## Stage 4 — files and cleanup
- `unpack`/`pack` (tool by extension), `delete` (trash), `clean up`.
- Cross-verb hints (`remove <file>`, `delete <package>`).

## Stage 5 — v1 release
- Full CI matrix green, README examples verified, install docs.
- Create public GitHub repo `tobias-martens/slash-shell`, tag v1.0.

## After v1
- v1.x verbs: `open`, `download`, `connect to wifi`/`disconnect wifi`,
  `check internet`.
- Typo correction (Damerau-Levenshtein, confirm first).
- Homebrew; distro packages (.deb, .rpm, AUR); fish history rewrite.
