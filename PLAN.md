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

## Stage 1 — core + packages (done 2026-11-24)
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
- Status: all of the above built; Go unit tests plus 46 pty tests (bash,
  zsh, fish, end to end) pass on Ubuntu. dnf/pacman/zypper/apk commands are
  unit-tested only until the distro container tests (stage 5).

## Stage 2 — search and about (offline only) (done 2026-11-24)
- `search`: files (`plocate`/`locate`, else `find` in home) + packages from
  local catalogs only; case-insensitive substring; `in files|packages|<source>`.
- `about`: compact per-source summary from script-stable commands.
- No local data → "run `update` first", never fetch.
- Status: built and tested on Ubuntu (apt, flatpak, snap; find for files).
  The dnf, pacman, zypper and apk parsers and plocate/locate are written
  from their documented output formats, unverified until the distro
  container tests (stage 5).

## Stage 3 — show, start, stop, restart (done 2026-11-24)
- `show` with its fixed list of things; unknown thing → "I can show: …".
- Services vs programs, ask on ambiguity; graceful stop, ask before force.
- Bare `stop`/`restart` lists candidates; tab completion in all three shells.
- Status: built and tested on Ubuntu with systemd (55 pty tests). Not yet:
  OpenRC (Alpine) services, systems without per-app cgroups (falls back to
  a plain process list), sudo for programs of other users.

## Stage 4 — files and cleanup (done 2026-11-24)
- `unpack`/`pack` (tool by extension), `delete` (trash), `clean up`.
- Cross-verb hints (`remove <file>`, `delete <package>`).
- Status: built and unit-tested; tried for real on Ubuntu (tar, zip, gio).
  7z/unrar/zstd and trash-put untested here. Cleaning snap (old revisions)
  and apk not supported yet.

## Stage 5 — v1 release
- Full CI matrix green, README examples verified, install docs.
- Create public GitHub repo `tobias-martens/slash-shell`, tag v1.0.
- Status: distro tests pass in LXD (2026-11-24) on Debian 13, Ubuntu 24.04,
  Fedora 43, Arch, openSUSE Tumbleweed and Alpine edge
  (`tests/distro/lxd.sh`): real install/remove round trip, `clean up`, then
  network off for search, about, files, show, pack/unpack, services and
  the bash/zsh/fish pty tests. This verifies the stage 2 parsers for dnf,
  pacman, zypper and apk. The Docker CI job runs the same checks
  (untested until the repo is on GitHub).
- Install script (`install.sh`, built 2026-11-24): per user, no sudo;
  downloads `slash-linux-<arch>` + `SHA256SUMS` from the latest GitHub
  release (`.github/workflows/release.yml` builds them on a `v*` tag),
  installs to `~/.local/bin`, runs `slash enable`; `--uninstall` runs
  `slash disable` and removes the binary and the folders the install
  created. `tests/install_test.sh`: install twice, a new bash/zsh/fish finds
  slash, uninstall, home folder byte-identical; passes locally and on all
  six distros. The download path itself is untested until the first release.
- README rewritten (2026-11-24): every example's command taken from real
  `slash resolve` output on Ubuntu; describes line rewriting, asking,
  offline search, takeover rules, install/uninstall, supported systems.
- `pack` defaults to `.zip` (2026-11-24), made with whatever is installed
  (zip, 7z, bsdtar, python3), else `.tar.gz`; `unpack`, `show` and `delete`
  also fall back to installed tools (slash's own trash without gio).
- No-guessing sweep (2026-11-24): `install`/`remove` say "not found / not
  installed" instead of passing the name to the package manager; several
  flatpak apps, programs or services with one name → listed and asked.
  Distro tests pass on Debian, Ubuntu, Fedora, Arch and Alpine; openSUSE
  not re-run yet (its mirror stalled the test setup).
  Still to do: create the public repo (needs the user's OK), first CI run,
  tag v1.0 (release workflow builds the binaries the installer downloads).

## After v1
- **Vocabulary study (thorough, before adding verbs or synonyms):** collect
  what everyday tasks newcomers actually look for (forum questions, "how do
  I … in Linux" searches, beginner guides, AskUbuntu/Reddit titles) and
  which words they use for each ("get rid of", "uninstall", "delete",
  "kill", "close", "unzip", "extract", "free up space", …). Map each to
  slash's verbs and check the fit is extremely good: the words people use
  must lead to the right verb or a helpful hint, never to a wrong action
  (e.g. "delete firefox" must not uninstall anything, "kill" stays the
  core tool). Result: a table of use case → user words → slash phrase, and
  a list of gaps to fix or new verbs to consider (goal 6: few names).
- v1.x verbs: `open`, `download`, `connect to wifi`/`disconnect wifi`,
  `check internet`.
- Typo correction (Damerau-Levenshtein, confirm first).
- Homebrew; distro packages (.deb, .rpm, AUR); fish history rewrite.
