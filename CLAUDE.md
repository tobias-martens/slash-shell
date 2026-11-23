# slash

**simple and natural commands for the Linux shell.** A thin abstraction layer:
plain-English commands (`update`, `install firefox as flatpak`) that translate
to the right real command for the current system. Not a new shell or
language — it never changes anything fundamental.

Scope: everyday shell tasks a newcomer would phrase in plain words — not only
package management. v1 covers packages, search, system info (`show`),
services/programs, archives, safe deletion and cleanup; more areas follow
under the same goals.

## Main goals (apply to every design decision)

1. **Simplicity / near-natural language.** Users type plain English words.
   Never require tool-specific syntax: no prefixes (`sp install`), no sigils
   (`flatpak:firefox`), no slash-only flags (`--flatpak`). If a feature needs
   special syntax, redesign it in plain words or drop it. Accept very common
   misspellings (e.g. "flatpack") but don't build big typo tables.
2. **Transparency.** The real command is always shown before it runs, and
   history stores the real command first: `sudo apt update  #slash: update`.
3. **Non-intrusive.** Only act on input clearly in scope. Complex commands,
   pipes, scripts and non-interactive shells are left alone.
4. **Minimal privilege.** `sudo` only where strictly necessary; never for
   flatpak or brew; skip when already root.
5. **Predictable over clever.** A small, fixed grammar — not real NL parsing.
   When ambiguous, ask; never guess silently. Never add flags the user didn't
   ask for (no `-y`).
6. **Few reserved names.** Every bare word slash claims is a cost; add verbs
   and synonyms sparingly.
7. **Reach.** Support all major distros and shells, not just one family.

## Agreed design

- Verbs (+ synonyms): `update` (`upgrade`), `install`, `remove` (`uninstall`),
  `about` (`describe`), `search`, `show`, `start`, `stop`, `restart`,
  `unpack`, `pack`, `delete`, `clean`.
  - `about <name>`: details for one package across all sources (compact
    summary built from script-stable commands: `dpkg-query`, `rpm -q`,
    `flatpak --columns`, …).
  - `search <words>`: lists matches, by default both **files** and
    **packages**, in separate sections, a few results each. Narrow with the
    normal grammar: `search report in files`, `search editor in packages`,
    `search editor in flatpak`. Files: `plocate`/`locate` if available,
    otherwise `find` in the home directory (skipping hidden dirs/caches).
    Packages: each source's local catalog (see "Offline lookups"). Matching
    is case-insensitive substring for both files (`plocate -i`,
    `find -iname '*x*'`) and packages. No fuzzy/typo matching in v1.
    Searching file *contents* is a later addition.
  - `show <thing> [of|in|for <target>]` — fixed list of things; anything
    else prints "I can show: …". v1: `disk space` (`df -h -x tmpfs -x
    devtmpfs`), `size of <dir>` (`du -sh`), `biggest files in <dir>`
    (`du -ah | sort -rh | head`), `memory` (`free -h`), `ip` (`ip -br addr`),
    `ports` (`ss -tulpn`), `what uses port <n>` (`ss -ltnp 'sport = :<n>'`),
    `programs`/`processes` (the user's running programs, `ps -u $USER …
    --sort=-%cpu`), `services` (`systemctl list-units --type=service
    --state=running`, plus `--user`), `logs of <service>` (`journalctl -u … -e`),
    `errors` (`journalctl -p err -b`), `drives` (`lsblk -o
    NAME,SIZE,MOUNTPOINT,LABEL`), `wifi` (`nmcli dev wifi list`), `battery`
    (`upower -i …`), `system` (`hostnamectl`). `public ip` needs an external
    service — only on explicit request, clearly labelled.
  - `start` / `stop` / `restart <name>`: services via `systemctl` (sudo for
    system services, `--user` for user services). `stop <program>`: `pkill`
    (TERM); if still running after a few seconds, ask "force it? [y/N]" →
    `pkill -KILL`. If a service and a program share the name, ask.
    Bare `stop` / `restart` (no name) list what can be stopped/restarted
    (running programs + services) instead of erroring. Tab completion for
    `stop`/`start`/`restart` offers running programs and service names.
  - `unpack <archive>`: picks the tool by extension (`tar -xf`, `unzip`,
    `7z x`, `unrar x`, …). `pack <dir> [as zip|tar.gz|…]`: `zip -r` /
    `tar -czf`. (`compress`/`extract` avoided: taken by ncompress/libextractor.)
  - `delete <file>`: moves to the trash (`gio trash`), never `rm`.
  - `clean up`: removes unneeded packages and caches on every source
    (`sudo apt autoremove && sudo apt clean; flatpak uninstall --unused; …`).
  - Cross-verb hints: `remove <existing file>` asks "that's a file — move it
    to the trash?"; `delete <x>` where x is no file but a package suggests
    `remove`.
- `slash` (no arguments): short intro (one or two lines on what slash is)
  plus sample commands, adapted to the system (only verbs/sources that work
  here, e.g. no snap examples without snap). Ends with a hint that every real
  command is shown before it runs. Fits on one screen. Other subcommands
  (`slash <verb> …` fallback, `slash doctor`, `slash resolve …`) are
  secondary.
- Project name `slash` (renamed from "splash" on 2026-11-23: `splash` is an
  astrophysics tool in Debian/Ubuntu, `splain` ships with Perl everywhere).
  `slash` verified free as command and package name on Debian, Ubuntu,
  Fedora, openSUSE, Arch, Alpine, AUR and Homebrew. GitHub repo:
  `tobias-martens/slash-shell` (public, not yet created) — "slash" alone is
  crowded on GitHub and close to slash-lang ("a new language for the web"),
  so never describe slash as a "language". `tools/check-names.sh` re-runs the
  name check against distro file indexes.
- Later (v1.x): `open` (`xdg-open`), `download` (`curl -LO`), `connect to
  wifi …` / `disconnect wifi` (`nmcli`), `check internet`. Maybe later:
  `where is …`, copy/move/create folder, permissions, users.
- Names taken by core tools (never use): find, free, kill, ip, make, mount,
  eject, test, size, info, which, go, uncompress, rename, edit, view.
- Name collisions: verified 2026-11-22 against Debian sid, Ubuntu 26.04, Fedora
  rawhide, openSUSE Tumbleweed, Arch core+extra, Alpine edge, snap store,
  Homebrew. Only `install` collides with a core tool (coreutils, everywhere).
  Taken by rarely installed packages (fine under "newest install wins"):
  `search` (Go dev packages), `show` (nmh), `pack` (allegro dev tools),
  `start`/`stop`/`restart` (some Alpine package, unverified). Never usable:
  `old` (openSUSE base package `aaa_base`).
- Mechanism: two layers (decided 2026-11-23). Layer 1 — **line rewriting on
  Enter** (main path). When the
  user presses Enter and the whole line is a slash phrase, the shell hook
  replaces the line with the real command plus a comment, e.g.
  `sudo apt update && sudo apt upgrade  #slash: update`, and the shell runs
  and records it natively — no history manipulation, no separate "→" output.
  - fish: `abbr --function` (fish ≥ 3.6). zsh: wrapper widget around
    `accept-line`. bash (≥ 4): Enter bound to "rewrite line, then
    accept-line" (emacs and vi keymaps; ble.sh needs its own hook).
  - When slash had to choose or assume something (e.g. package not in the
    system PM but found in flatpak), it writes the command onto the line
    *without running it*, with the reason in the comment; Enter runs it,
    Ctrl-C cancels, the user may edit first.
  - Verbs with own output or follow-ups (`search`, `about`, `stop <program>`)
    rewrite to `slash <verb> …  #slash: …`; slash then prints the real
    commands it runs.
  - Only whole lines are rewritten; compound lines are handled by layer 2.
  - Layer 2 — fallback functions: `init` also defines one shell function per
    verb, used only when layer 1 didn't rewrite the line (compound lines like
    `update && reboot`, bash without readline / ble.sh / Emacs shell, other
    tools owning Enter). The function asks the same resolver (same takeover
    rules, coreutils `install` rule, fail open), prints `→ real command`,
    runs it, and appends `real command  #slash: phrase` to history
    (bash `history -s`, zsh `print -s`; fish: no reliable way, skipped).
    Functions exist only in interactive sessions: scripts are untouched,
    `command <name>` bypasses them, uninstall stays clean.
  - ble.sh: own hook later, so it gets layer 1 too.
  - Fail open: if the slash binary is missing, errors or is slow, the line is
    accepted unchanged. A broken slash must never block the user's Enter key.
- Takeover policy, "newest install wins" (same rules in both layers; slash
  never modifies or replaces any installed program):
  - Names that already exist when slash is installed: slash takes them over;
    `command <name> …` runs the original (never rewritten, and it bypasses
    the layer-2 functions). Recorded in a state file, reported once at install.
  - Commands installed *after* slash win: if a real command with the verb's
    name exists that isn't in the recorded list, the line is left alone and a
    one-time notice says slash's verb is now reachable as `slash <verb>`. If
    that program is removed again, the verb comes back by itself.
  - The user's own aliases/functions with a verb's name always win.
  - Reinstalling slash is again the newest intent: re-records and takes over.
  - Coreutils `install`: lines it could execute successfully (options, or an
    operand that is an existing file/path) are never rewritten.
- Clean uninstall: removing slash must leave everything exactly as before.
  - The installer only adds one marked block to the rc file(s), guarded so a
    missing binary is harmless:
    `command -v slash >/dev/null && eval "$(slash init bash)"`.
  - The uninstaller removes exactly that block, the binary and slash's state
    dir (`~/.local/state/slash`) — nothing else.
  - Since slash never touches other programs, any command it took over works
    as before immediately; already-open shells keep working thanks to
    fail-open. History entries stay valid, as they are real commands.
  - Distro packages can't edit users' rc files — the guard above covers a
    package removal.
  - CI test: install → use → uninstall → the rc file is byte-identical and
    previously taken-over commands behave as before.
- Grammar: `<verb> <thing>[, <thing>] [and <thing>] [as|from|via|in <source>]`,
  plus `update <source>` / `update only <source>`. For `search`, `files` and
  `packages` also count as sources. Connector words only count in
  their grammatical position (`install as` installs the package `as`).
- Sources: system package manager (apt, dnf, pacman, zypper, apk) + flatpak +
  snap, detected by presence of the binary. `update` means "update everything":
  refresh + upgrade on every source (e.g. `sudo apt update && sudo apt upgrade;
  flatpak update; sudo snap refresh`), keeping each tool's own confirmation.
  `install` tries the system PM first; if the package isn't there, it checks the
  other sources and asks before using one. `remove` targets wherever the package
  is installed.
- No standalone "refresh index" verb anywhere (on Arch `pacman -Sy` without
  `-u` causes partial upgrades). Arch: `update` = `pacman -Syu`, `install` =
  `pacman -S`, never `-Sy`.
- Immutable systems (SteamOS, Fedora Atomic/Bazzite, …): read-only base, so
  flatpak becomes the default install source; system updates via the OS's own
  updater.
- Offline lookups: `search` and `about` never touch the network — no query
  (which may be a private file name like `XXX.mp4`) ever leaves the machine.
  They only read local data; if a source has no local data yet, say so
  ("package lists not downloaded yet — run `update`") instead of fetching.
  Verified 2026-11-23 on Ubuntu with strace/ss: `apt-cache search` is offline;
  `flatpak search` downloads fresh appstream data when stale (→ read
  `/var/lib/flatpak/appstream/<remote>/<arch>/active/appstream.xml.gz`
  directly); `snap find`/`snap info` always query the store (→ substring match
  on `/var/cache/snapd/names`, names only; installed snaps via `snap list`).
  From docs, to verify in network-less containers: `dnf` refreshes expired
  metadata (→ `--cacheonly`), `zypper` auto-refreshes (→ `--no-refresh`),
  `pacman -Ss`/`-Si` and `apk search` read local databases. CI must run all
  search/about tests with networking disabled.
- Typo correction (later version, for verbs, `about` and `search` alike;
  v1 only accepts a few very common spellings like "flatpack"):
  Damerau-Levenshtein with keyboard-adjacency weighting, only towards slash verbs/source names, skipped if close to an
  installed command, confirm with `[Y/n]` (Enter = yes), never non-interactive.
  Chains to the distro's existing command-not-found handler.
- Architecture: one static Go binary `slash`. The resolver (`slash resolve …`)
  only prints the command. The shell glue (Enter hook, fallback functions,
  completion) is
  embedded in the binary and loaded the zoxide/starship way:
  `eval "$(slash init bash)"` (also `zsh`, `fish`) in the rc file. Command
  mappings are data files (embedded at build time), not code.
- License: GPL-3.0. No user configuration planned. Homebrew and distro
  packages (.deb/.rpm/AUR) are planned for later.
