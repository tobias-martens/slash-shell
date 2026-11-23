# slash

**simple and natural commands for the Linux shell.**

Type what you mean in plain English. slash runs the right command for your
system — and always shows you what it really runs, so you learn as you go.

```
$ update
→ sudo apt update && sudo apt upgrade; flatpak update; sudo snap refresh

$ install firefox as flatpak
→ flatpak install firefox

$ show disk space
→ df -h -x tmpfs -x devtmpfs

$ unpack photos.tar.zst
→ tar -xf photos.tar.zst
```

Your shell history keeps the real command, with the slash phrase as a comment:

```
sudo apt update && sudo apt upgrade  #slash: update
```

> **Status:** early development — the design is settled, the code is being
> written. Nothing here is usable yet.

## What you can say (planned for v1)

| | |
|---|---|
| `update` | update everything: system packages, flatpak, snap |
| `install vlc` · `install firefox as flatpak` | install programs |
| `remove spotify` | uninstall a program, wherever it came from |
| `about firefox` | details on a package, from every source |
| `search holiday` | find files and packages (offline — nothing leaves your machine) |
| `show disk space` · `show memory` · `show ports` · `show programs` · … | look at your system |
| `start` · `stop` · `restart nginx` | control services and programs |
| `unpack photos.zip` · `pack Photos as zip` | archives, without remembering `tar` flags |
| `delete old-notes.txt` | move a file to the trash (never `rm`) |
| `clean up` | free space from unneeded packages and caches |
| `slash` | a short intro and examples for your system |

## Principles

- **Simple and natural.** Plain words, no special syntax to learn.
- **Transparent.** The real command is always shown and kept in your history.
- **Non-intrusive.** Scripts, pipes and anything out of scope are left alone.
  If you install another program with the same name as a slash command, that
  program wins.
- **Minimal privilege.** `sudo` only where it's really needed.
- **Predictable.** When something is ambiguous, slash asks instead of guessing.

## Supported systems (planned)

Debian, Ubuntu, Fedora, Arch, SteamOS, openSUSE, Alpine and their
derivatives — with flatpak and snap — in bash, zsh and fish.

## License

[GPL-3.0](LICENSE)
