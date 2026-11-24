# slash

**simple and natural commands for the Linux shell.**

Type what you mean in plain English. slash turns it into the right command
for your system, and you always see that command, so you learn as you go.

You type:

```
$ update
```

When you press Enter, slash replaces the line with the real command, which
then runs as usual:

```
$ sudo apt update && sudo apt upgrade; flatpak update; sudo snap refresh  #slash: update
```

Your history keeps it this way: the real command, with your words as a
comment. On Fedora the same word becomes `sudo dnf upgrade`, and on Arch
`sudo pacman -Syu`.

> **Status:** v1.0 is being prepared and not released yet. The install
> command below works once it is.

## What you can say

| You type | slash runs (on Ubuntu, for example) |
|---|---|
| `update` | `sudo apt update && sudo apt upgrade; flatpak update; sudo snap refresh` |
| `install vlc` | `sudo apt install vlc` |
| `install firefox as flatpak` | `flatpak install org.mozilla.firefox` |
| `remove vlc` | `sudo apt remove vlc` |
| `search holiday` | finds files and packages, offline |
| `about firefox` | details from every source: version, installed or not |
| `show disk space` | `df -h -x tmpfs -x devtmpfs` |
| `show what uses port 8080` | `ss -tulpn 'sport = :8080'` |
| `restart bluetooth` | `sudo systemctl restart bluetooth` |
| `stop firefox` | stops the program, and asks before forcing it |
| `unpack photos.tar.zst into Old` | `mkdir -p Old && tar -xkf photos.tar.zst -C Old` |
| `pack Photos` | `zip -r Photos.zip Photos` (or `pack Photos as tar.gz`) |
| `delete old-notes.txt` | `gio trash old-notes.txt`: to the trash, never `rm` |
| `clean up` | `sudo apt autoremove && sudo apt clean; flatpak uninstall --unused` |
| `slash` | a short intro with examples for your system |

`show` also knows: `memory`, `ip`, `ports`, `programs`, `services`,
`size of <folder>`, `biggest files in <folder>`, `logs of <service>`,
`errors`, `drives`, `wifi`, `battery`, `system`, `public ip`.

Plain `stop` or `restart` lists what is running, and Tab completes program
and service names.

**When slash has to choose, it asks.** If a program isn't in your system's
packages but slash finds it elsewhere, it writes the command onto the line
with the reason, and waits:

```
$ flatpak install md.obsidian.Obsidian  #slash: install obsidian (obsidian: not in apt, found in flatpak)
```

Press Enter to run it, or Ctrl-C to cancel. You can also edit it first.

**`search` and `about` never use the internet.** They read only the package
lists already on your computer, so a search for a private file name never
leaves your machine. Each section shows which command it read from:

```
$ search editor in packages
apt  (apt-cache search --names-only -- editor)
  mu-editor                     simple editor for beginner Python programmers
  auto-editor                   Effort free video editing
  node-editor                   Launch $EDITOR in your program
  …
```

Installed packages are marked `[installed]`. Of the `show` commands, only
`show public ip` uses the internet, and it waits for a second Enter.

## Install

For your user only, no sudo:

```
curl -fsSL https://raw.githubusercontent.com/tobias-martens/slash-shell/main/install.sh | sh
```

This puts the `slash` program in `~/.local/bin` and adds one marked block to
your shell's start-up file (`~/.bashrc`, `~/.zshrc` or fish's `conf.d`).
Open a new terminal and type `slash` for a short intro. If you use more than
one shell, `slash enable zsh` (or `bash`, `fish`) turns it on there too.

To remove slash again and leave every file exactly as it was:

```
curl -fsSL https://raw.githubusercontent.com/tobias-martens/slash-shell/main/install.sh | sh -s -- --uninstall
```

## Your commands stay yours

- **Scripts are untouched.** slash only acts in interactive shells, on lines
  that are clearly one of its phrases. Pipes, scripts and anything else run
  as always.
- **Your aliases and functions win.** If you have your own `update`, slash
  leaves it alone.
- **Programs installed later win.** Some names already exist on some
  systems. Coreutils' `install`, for example, keeps working for files, as in
  `install -m 755 script ~/bin/`. When you turn slash on, it lists any it
  takes over, and `command <name>` always runs the original. If you later
  install a program with the same name as one of slash's words, that program
  wins, and slash's word stays reachable as `slash <word>`.
- **If slash breaks, nothing breaks.** If the program is missing, fails or is
  slow, your line runs unchanged.
- **sudo only where needed.** Never for flatpak, and not when you are already
  root. slash never adds `-y` or other flags you didn't ask for.

Some other things you can run:

- `slash doctor` shows what slash found on your system.
- `slash resolve --line "install vlc"` prints what a phrase would run,
  without running it.
- `slash disable` turns it off again.

## Supported systems

Tested on Debian 13, Ubuntu 24.04, Fedora 43, Arch, openSUSE Tumbleweed and
Alpine, with apt, dnf, pacman, zypper, apk, flatpak and snap, in bash (4+),
zsh (5.3+) and fish (3.1+). Derivatives of these (Mint, Pop!_OS, Manjaro,
EndeavourOS, …) work the same way. On systems with a read-only base, such as
Fedora Atomic or SteamOS, slash installs with flatpak.

Not yet supported:
- starting and stopping services without systemd (e.g. OpenRC on Alpine)
- `clean up` for snap and apk
- ble.sh, which needs its own hook (until then, slash works there through its
  fallback functions)

## License

[GPL-3.0](LICENSE)
