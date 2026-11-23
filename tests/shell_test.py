#!/usr/bin/env python3
"""Drive real interactive shells through a pseudo-terminal and check slash's
shell integration: what runs, what's shown, what lands in history.

The integration script comes from the real binary (`slash init <shell>`), but
`slash resolve` is a fake with fixed answers, so no package command can ever
run. Usage: SLASH_BIN=/path/to/slash python3 -I tests/shell_test.py
"""
import os
import pty
import re
import select
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest

SLASH_BIN = os.environ.get("SLASH_BIN", "")

FAKE_SLASH = r"""#!/bin/sh
# Fake `slash resolve` with fixed answers.
[ "$1" = resolve ] || exit 9
mode=$2; shift 2
entry=""
if [ "$1" = --history ]; then entry=$2; shift 2; fi
line="$*"
case "$line" in
  update)
    echo "echo REAL-UPDATE  #slash: update"
    # --words --history ENTRY: emulate slash's rewrite for the cases tested
    [ "$mode" = --words ] && [ "$entry" = "update && echo after" ] && echo "echo REAL-UPDATE && echo after  #slash: update"
    exit 0 ;;
  "install obsidian") echo "echo REAL-FLATPAK  #slash: install obsidian (not in apt, found in flatpak)"; exit 3 ;;
  "update firefox") [ "$mode" = --words ] && echo "slash: I can update everything" >&2; exit 2 ;;
esac
exit 1
"""


class Shell:
    """An interactive shell in a pty."""

    def __init__(self, argv, env):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execvpe(argv[0], argv, env)
        self.out = b""

    def send(self, text):
        os.write(self.fd, text.encode())

    def read_for(self, seconds):
        end = time.time() + seconds
        while time.time() < end:
            r, _, _ = select.select([self.fd], [], [], 0.05)
            if r:
                try:
                    data = os.read(self.fd, 65536)
                except OSError:
                    break
                self.out += data
                # Answer terminal queries like a real terminal (fish 4 waits for them).
                if b"\x1b[0c" in data or b"\x1b[c" in data:
                    os.write(self.fd, b"\x1b[?62;22c")
                if b"\x1b[6n" in data:
                    os.write(self.fd, b"\x1b[1;1R")

    def expect(self, pattern, timeout=5):
        end = time.time() + timeout
        while time.time() < end:
            if re.search(pattern, self.text()):
                return
            self.read_for(0.1)
        raise AssertionError(f"did not see {pattern!r} in:\n{self.text()}")

    def text(self):
        s = self.out.decode(errors="replace")
        s = re.sub(r"\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1bP[^\x1b]*\x1b\\", "", s)  # OSC, DCS
        s = re.sub(r"\x1b\[[0-9;?<>=]*[ -/]*[@-~]|\x1b[()][0-9A-Za-z]|\x1b[=>]|\r", "", s)  # CSI, misc
        return s.replace("\u23ce", "")  # fish's "no newline" mark

    def mark(self):
        """Forget output so far; later expects only see new output."""
        self.read_for(0.3)
        self.out = b""

    def close(self):
        try:
            os.kill(self.pid, signal.SIGKILL)
            os.waitpid(self.pid, 0)
        except OSError:
            pass
        os.close(self.fd)


# How each shell is started, configured and asked for its history.
SHELLS = {
    "bash": {
        "argv": lambda d: ["bash", "--noprofile", "--rcfile", d + "/.bashrc", "-i"],
        "rc": ".bashrc",
        "base": "PS1='PROMPT> '\nHISTFILE={d}/hist\n",
        "history": "history 6",
        "alias": "alias update='echo MY-ALIAS'",
        "function": "update() { echo MY-FUNC; }",
        "ignorespace": "HISTCONTROL=ignorespace",
        "vi": "set -o vi",
        "script": "bash -c 'type update || echo NOT-IN-SCRIPT'",
    },
    "zsh": {
        "argv": lambda d: ["zsh", "-i"],
        "rc": ".zshrc",
        "base": "PS1='PROMPT> '\nHISTFILE={d}/hist\nHISTSIZE=100\nSAVEHIST=0\n",
        "history": "fc -ln -6",
        "alias": "alias update='echo MY-ALIAS'",
        "function": "update() { echo MY-FUNC; }",
        "ignorespace": "setopt hist_ignore_space",
        "vi": "bindkey -v",
        "script": "zsh -c 'whence update || echo NOT-IN-SCRIPT'",
    },
    "fish": {
        "argv": lambda d: ["fish", "-i"],
        "rc": ".config/fish/config.fish",
        "base": "set -g fish_greeting\nfunction fish_prompt; echo -n 'PROMPT> '; end\n",
        "history": "history search --max 6 | cat",
        "alias": "alias update 'echo MY-ALIAS'",
        "function": "function update; echo MY-FUNC; end",
        "ignorespace": "",  # fish's default
        "vi": "fish_vi_key_bindings",
        "script": "fish -c 'type -q update; or echo NOT-IN-SCRIPT'",
    },
}


def shell_env(home, bindir):
    return {"HOME": home, "PATH": bindir + ":/usr/bin:/bin", "TERM": "xterm", "LC_ALL": "C.UTF-8",
            "XDG_CONFIG_HOME": home + "/.config", "XDG_DATA_HOME": home + "/.local/share",
            "XDG_STATE_HOME": home + "/state", "XDG_CACHE_HOME": home + "/cache"}


class ShellCases:
    """The same behaviour, checked in every shell. Subclasses set NAME."""
    NAME = ""

    @property
    def cfg(self):
        return SHELLS[self.NAME]

    def start(self, before="", after="", with_slash=True):
        self.tmp = tempfile.mkdtemp()
        bindir = os.path.join(self.tmp, "bin")
        os.mkdir(bindir)
        if with_slash:
            fake = os.path.join(bindir, "slash")
            with open(fake, "w") as f:
                f.write(FAKE_SLASH)
            os.chmod(fake, 0o755)
        init = subprocess.run([SLASH_BIN, "init", self.NAME], capture_output=True, text=True, check=True).stdout
        rc = os.path.join(self.tmp, self.cfg["rc"])
        os.makedirs(os.path.dirname(rc), exist_ok=True)
        with open(rc, "w") as f:
            f.write(self.cfg["base"].format(d=self.tmp) + "%s\n%s\n%s\n" % (before, init, after))
        self.sh = Shell(self.cfg["argv"](self.tmp), shell_env(self.tmp, bindir))
        self.sh.expect("PROMPT> ")
        self.sh.mark()

    def tearDown(self):
        if hasattr(self, "sh"):
            self.sh.close()
        if hasattr(self, "tmp"):
            shutil.rmtree(self.tmp, ignore_errors=True)

    def run_line(self, line, wait_for):
        self.sh.send(line + "\r")
        self.sh.expect(wait_for)

    def history(self):
        self.sh.mark()
        cmd = self.cfg["history"]
        self.sh.send(cmd + "\r")
        self.sh.expect(re.escape(cmd) + r"\n(.|\n)*PROMPT> ")
        return self.sh.text().split(cmd, 1)[1]

    def test_layer1_rewrites_and_records_real_command(self):
        self.start()
        self.run_line("update", "REAL-UPDATE\n")
        h = self.history()
        self.assertIn("echo REAL-UPDATE  #slash: update", h)
        self.assertNotRegex(h, r"(?m)^\s*(\d+\*?\s+)?update$")

    def test_layer1_confirm_waits_for_second_enter(self):
        self.start()
        self.sh.send("install obsidian\r")
        self.sh.expect(r"echo REAL-FLATPAK  #slash: install obsidian \(not in apt, found in flatpak\)")
        self.sh.read_for(0.5)
        self.assertNotIn("REAL-FLATPAK\n", self.sh.text(), "ran without confirmation")
        self.run_line("", "REAL-FLATPAK\n")
        self.assertIn("echo REAL-FLATPAK  #slash: install obsidian", self.history())

    def test_layer1_confirm_can_be_cancelled(self):
        self.start()
        self.sh.send("install obsidian\r")
        self.sh.expect("#slash: install obsidian")
        self.sh.send("\x03")  # Ctrl-C
        self.sh.mark()
        self.run_line("echo next", "next\n")
        self.assertNotIn("REAL-FLATPAK\n", self.sh.text())

    def test_layer2_compound_line(self):
        self.start()
        self.sh.send("update && echo after\r")
        self.sh.expect("→ echo REAL-UPDATE\n")
        self.sh.expect("REAL-UPDATE\n")
        self.sh.expect("after\n")
        if self.records_layer2():
            self.assertIn("echo REAL-UPDATE && echo after  #slash: update", self.history())

    def records_layer2(self):
        return True

    def test_layer2_ignorespace_not_recorded(self):
        self.start(before=self.cfg["ignorespace"])
        self.run_line("echo first", "first\n")
        self.run_line(" update && echo hidden", "hidden\n")
        h = self.history()
        self.assertIn("echo first", h)
        self.assertNotIn("hidden", h)

    def test_layer2_explains_errors(self):
        self.start()
        self.run_line("update firefox", "slash: I can update everything")

    def test_not_handled_runs_real_program(self):
        self.start()
        self.run_line("install -m 755 /nonexistent/a /tmp/b", r"install: cannot stat")

    def test_users_alias_wins(self):
        self.start(before=self.cfg["alias"])
        self.run_line("update", "MY-ALIAS\n")
        self.assertNotIn("REAL-UPDATE", self.sh.text())

    def test_users_function_defined_later_wins(self):
        self.start(after=self.cfg["function"])
        self.run_line("update", "MY-FUNC\n")
        self.assertNotIn("REAL-UPDATE", self.sh.text())

    def test_fail_open_without_slash(self):
        self.start(with_slash=False)
        self.run_line("update", r"(?i)command.*not found|not found|unknown command")
        self.run_line("echo still-fine", "still-fine\n")
        self.assertNotIn("timeout", self.sh.text(), "fail-open must be silent")

    def test_other_commands_untouched(self):
        self.start()
        self.run_line("echo plain && echo line", "plain\nline\n")
        self.assertIn("echo plain && echo line", self.history())

    def test_vi_mode(self):
        self.start(after=self.cfg["vi"])
        self.run_line("update", "REAL-UPDATE\n")
        self.assertIn("echo REAL-UPDATE  #slash: update", self.history())

    def test_scripts_unaffected(self):
        self.start()
        self.run_line(self.cfg["script"], "NOT-IN-SCRIPT\n")


def have(shell):
    return bool(SLASH_BIN and shutil.which(shell))


@unittest.skipUnless(have("bash"), "needs SLASH_BIN and bash")
class BashTest(ShellCases, unittest.TestCase):
    NAME = "bash"

    def test_layer2_history_off(self):
        self.start()
        self.run_line("echo before", "before\n")
        self.sh.send("set +o history\r")
        self.run_line("update && echo after", "after\n")
        self.sh.send("set -o history\r")
        h = self.history()
        self.assertNotIn("REAL-UPDATE", h)
        self.assertIn("echo before", h)


@unittest.skipUnless(have("zsh"), "needs SLASH_BIN and zsh")
class ZshTest(ShellCases, unittest.TestCase):
    NAME = "zsh"

    def start(self, *a, **kw):
        os.environ.pop("ZDOTDIR", None)
        super().start(*a, **kw)

    def test_comment_option_restored(self):
        """'#' is only a comment for slash's own line; the user's setting stays."""
        self.start()
        self.run_line("update", "REAL-UPDATE\n")
        self.run_line("echo a #b", "a #b\n")
        self.run_line("[[ -o interactive_comments ]] || echo OFF", "OFF\n")

    def test_recalled_history_line_runs(self):
        self.start()
        self.run_line("update", "REAL-UPDATE\n")
        self.sh.mark()
        self.sh.send("\x1b[A\r")  # up arrow, Enter
        self.sh.expect("REAL-UPDATE\n")
        self.assertNotIn("#slash", self.sh.text().split("\n", 1)[1])


@unittest.skipUnless(have("fish"), "needs SLASH_BIN and fish")
class FishTest(ShellCases, unittest.TestCase):
    NAME = "fish"

    def records_layer2(self):
        """history append and { … } blocks need fish 4.1."""
        out = subprocess.run(["fish", "--version"], capture_output=True, text=True).stdout
        major, minor = map(int, re.search(r"(\d+)\.(\d+)", out).groups())
        return (major, minor) >= (4, 1)

    def test_users_abbreviation_wins(self):
        self.start(before="abbr -a update echo MY-ABBR")
        self.run_line("update", "MY-ABBR\n")
        self.assertNotIn("REAL-UPDATE", self.sh.text())


FAKE_TOOL = """#!/bin/sh
echo "FAKE-$(basename "$0") $*"
"""


class EndToEnd:
    """The real slash: enable, use, disable. sudo/flatpak/snap are fakes that
    only print, so nothing is ever installed. Subclasses set NAME."""
    NAME = ""

    def setUp(self):
        self.tmp = tempfile.mkdtemp()
        self.bindir = os.path.join(self.tmp, "bin")
        os.mkdir(self.bindir)
        shutil.copy(SLASH_BIN, os.path.join(self.bindir, "slash"))
        for tool in ["sudo", "flatpak", "snap"]:
            path = os.path.join(self.bindir, tool)
            with open(path, "w") as f:
                f.write(FAKE_TOOL)
            os.chmod(path, 0o755)
        cfg = SHELLS[self.NAME]
        self.rc = os.path.join(self.tmp, cfg["rc"])
        os.makedirs(os.path.dirname(self.rc), exist_ok=True)
        self.original = cfg["base"].format(d=self.tmp)
        with open(self.rc, "w") as f:
            f.write(self.original)
        self.env = shell_env(self.tmp, self.bindir)
        self.env["SHELL"] = "/bin/" + self.NAME
        # Start the shell once, so files it creates itself (fish_variables, …)
        # exist before slash is enabled.
        self.shell()
        self.sh.close()
        del self.sh
        self.files_before = self.snapshot()

    def snapshot(self):
        """Every file and directory under HOME except slash's own and history."""
        out = set()
        for root, dirs, files in os.walk(self.tmp):
            for n in dirs + files:
                rel = os.path.relpath(os.path.join(root, n), self.tmp)
                if not rel.startswith(("bin", "state", "cache", ".local", "hist", ".zcompdump")):
                    out.add(rel)
        return out

    def tearDown(self):
        if hasattr(self, "sh"):
            self.sh.close()
        shutil.rmtree(self.tmp, ignore_errors=True)

    def slash(self, *args):
        return subprocess.run([os.path.join(self.bindir, "slash"), *args], env=self.env,
                              capture_output=True, text=True)

    def shell(self):
        self.sh = Shell(SHELLS[self.NAME]["argv"](self.tmp), self.env)
        self.sh.expect("PROMPT> ")
        self.sh.mark()

    def test_enable_use_disable(self):
        r = self.slash("enable")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("enabled for " + self.NAME, r.stdout)

        self.shell()
        self.sh.send("update\r")
        self.sh.expect("FAKE-sudo apt update")
        self.sh.expect("FAKE-flatpak update")
        self.sh.expect("FAKE-sudo snap refresh")
        self.sh.mark()
        hist = SHELLS[self.NAME]["history"]
        self.sh.send(hist + "\r")
        self.sh.expect(r"sudo apt update && sudo apt upgrade; flatpak update; sudo snap refresh  #slash: update")
        self.sh.mark()
        self.sh.send("install -m 644 %s %s/copy\r" % (self.rc, self.tmp))  # coreutils install, untouched
        self.sh.expect("PROMPT> ")
        self.assertTrue(os.path.exists(os.path.join(self.tmp, "copy")))
        os.remove(os.path.join(self.tmp, "copy"))
        self.sh.close()
        del self.sh

        r = self.slash("disable")
        self.assertEqual(r.returncode, 0, r.stderr)
        with open(self.rc) as f:
            self.assertEqual(f.read(), self.original, "rc file must be byte-identical")
        self.assertEqual(self.snapshot(), self.files_before, "slash must leave no files behind")
        self.assertFalse(os.path.exists(self.tmp + "/state/slash"))

        self.shell()
        self.sh.send("update\r")
        self.sh.expect(r"(?i)not found|unknown command")
        self.assertNotIn("FAKE-", self.sh.text())


def have_e2e(shell):
    return have(shell) and bool(shutil.which("apt-cache"))


@unittest.skipUnless(have_e2e("bash"), "needs SLASH_BIN, bash, apt")
class EndToEndBash(EndToEnd, unittest.TestCase):
    NAME = "bash"


@unittest.skipUnless(have_e2e("zsh"), "needs SLASH_BIN, zsh, apt")
class EndToEndZsh(EndToEnd, unittest.TestCase):
    NAME = "zsh"


@unittest.skipUnless(have_e2e("fish"), "needs SLASH_BIN, fish, apt")
class EndToEndFish(EndToEnd, unittest.TestCase):
    NAME = "fish"


if __name__ == "__main__":
    unittest.main(verbosity=2)
