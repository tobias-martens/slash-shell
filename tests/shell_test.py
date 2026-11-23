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
                    self.out += os.read(self.fd, 65536)
                except OSError:
                    break

    def expect(self, pattern, timeout=5):
        end = time.time() + timeout
        while time.time() < end:
            if re.search(pattern, self.text()):
                return
            self.read_for(0.1)
        raise AssertionError(f"did not see {pattern!r} in:\n{self.text()}")

    def text(self):
        s = self.out.decode(errors="replace")
        return re.sub(r"\x1b\[[0-9;?]*[A-Za-z]|\r", "", s)

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


@unittest.skipUnless(SLASH_BIN and shutil.which("bash"), "needs SLASH_BIN and bash")
class BashTest(unittest.TestCase):
    def start(self, before="", after="", with_slash=True):
        self.tmp = tempfile.mkdtemp()
        bindir = os.path.join(self.tmp, "bin")
        os.mkdir(bindir)
        if with_slash:
            fake = os.path.join(bindir, "slash")
            with open(fake, "w") as f:
                f.write(FAKE_SLASH)
            os.chmod(fake, 0o755)
        init = subprocess.run([SLASH_BIN, "init", "bash"], capture_output=True, text=True, check=True).stdout
        rc = os.path.join(self.tmp, "bashrc")
        with open(rc, "w") as f:
            f.write("PS1='PROMPT> '\nHISTFILE=%s/hist\n%s\n%s\n%s\n" % (self.tmp, before, init, after))
        env = {"HOME": self.tmp, "PATH": bindir + ":/usr/bin:/bin", "TERM": "xterm", "LC_ALL": "C.UTF-8"}
        self.sh = Shell(["bash", "--noprofile", "--rcfile", rc, "-i"], env)
        self.sh.expect("PROMPT> ")
        self.sh.mark()

    def tearDown(self):
        if hasattr(self, "sh"):
            self.sh.close()
        if hasattr(self, "tmp"):
            shutil.rmtree(self.tmp, ignore_errors=True)

    def history(self):
        self.sh.mark()
        self.sh.send("history 6\r")
        self.sh.expect(r"history 6\n(.|\n)*PROMPT> ")
        return self.sh.text()

    def test_layer1_rewrites_and_records_real_command(self):
        self.start()
        self.sh.send("update\r")
        self.sh.expect("REAL-UPDATE\n")
        h = self.history()
        self.assertIn("echo REAL-UPDATE  #slash: update", h)
        self.assertNotRegex(h, r"\d+  update\n")

    def test_layer1_confirm_waits_for_second_enter(self):
        self.start()
        self.sh.send("install obsidian\r")
        self.sh.expect(r"echo REAL-FLATPAK  #slash: install obsidian \(not in apt, found in flatpak\)")
        self.sh.read_for(0.5)
        self.assertNotIn("REAL-FLATPAK\n", self.sh.text(), "ran without confirmation")
        self.sh.send("\r")
        self.sh.expect("REAL-FLATPAK\n")
        self.assertIn("echo REAL-FLATPAK  #slash: install obsidian", self.history())

    def test_layer1_confirm_can_be_cancelled(self):
        self.start()
        self.sh.send("install obsidian\r")
        self.sh.expect("#slash: install obsidian")
        self.sh.send("\x03")  # Ctrl-C
        self.sh.mark()
        self.sh.send("echo next\r")
        self.sh.expect("next\n")
        self.assertNotIn("REAL-FLATPAK\n", self.sh.text())

    def test_layer2_compound_line(self):
        self.start()
        self.sh.send("update && echo after\r")
        self.sh.expect("→ echo REAL-UPDATE\n")
        self.sh.expect("REAL-UPDATE\n")
        self.sh.expect("after\n")
        self.assertIn("echo REAL-UPDATE && echo after  #slash: update", self.history())

    def test_layer2_ignorespace_previous_entry_untouched(self):
        self.start(before="HISTCONTROL=ignorespace")
        self.sh.send("echo first\r")
        self.sh.expect("first\n")
        self.sh.send(" update && echo hidden\r")
        self.sh.expect("hidden\n")
        h = self.history()
        self.assertIn("echo first", h)
        self.assertNotIn("hidden", h)

    def test_layer2_history_off(self):
        self.start()
        self.sh.send("echo before\r")
        self.sh.send("set +o history\r")
        self.sh.send("update && echo after\r")
        self.sh.expect("after\n")
        self.sh.send("set -o history\r")
        h = self.history()
        self.assertNotIn("REAL-UPDATE", h)
        self.assertIn("echo before", h)

    def test_layer2_explains_errors(self):
        self.start()
        self.sh.send("update firefox\r")
        self.sh.expect("slash: I can update everything")

    def test_not_handled_runs_real_program(self):
        self.start()
        self.sh.send("install -m 755 /nonexistent/a /tmp/b\r")
        self.sh.expect(r"install: cannot stat")

    def test_users_alias_wins(self):
        self.start(before="alias update='echo MY-ALIAS'")
        self.sh.send("update\r")
        self.sh.expect("MY-ALIAS\n")
        self.assertNotIn("REAL-UPDATE", self.sh.text())

    def test_users_function_defined_later_wins(self):
        self.start(after="update() { echo MY-FUNC; }")
        self.sh.send("update\r")
        self.sh.expect("MY-FUNC\n")
        self.assertNotIn("REAL-UPDATE", self.sh.text())

    def test_fail_open_without_slash(self):
        self.start(with_slash=False)
        self.sh.send("update\r")
        self.sh.expect(r"(?i)command.*not found|not found")
        self.sh.send("echo still-fine\r")
        self.sh.expect("still-fine\n")
        self.assertNotIn("timeout", self.sh.text(), "fail-open must be silent")

    def test_other_commands_untouched(self):
        self.start()
        self.sh.send("echo plain && echo line\r")
        self.sh.expect("plain\nline\n")
        self.assertIn("echo plain && echo line", self.history())

    def test_vi_mode(self):
        self.start(after="set -o vi")
        self.sh.send("update\r")
        self.sh.expect("REAL-UPDATE\n")
        self.assertIn("echo REAL-UPDATE  #slash: update", self.history())

    def test_scripts_unaffected(self):
        self.start()
        self.sh.send("bash -c 'type update || echo NOT-IN-SCRIPT'\r")
        self.sh.expect("NOT-IN-SCRIPT\n")


FAKE_TOOL = """#!/bin/sh
echo "FAKE-$(basename "$0") $*"
"""


@unittest.skipUnless(SLASH_BIN and shutil.which("bash") and shutil.which("apt-cache"), "needs SLASH_BIN, bash, apt")
class EndToEndBash(unittest.TestCase):
    """The real slash: enable, use, disable. sudo/flatpak/snap are fakes that
    only print, so nothing is ever installed."""

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
        self.rc = os.path.join(self.tmp, ".bashrc")
        self.original = "PS1='PROMPT> '\nHISTFILE=%s/hist\n" % self.tmp
        with open(self.rc, "w") as f:
            f.write(self.original)
        self.env = {"HOME": self.tmp, "PATH": self.bindir + ":/usr/bin:/bin", "TERM": "xterm",
                    "LC_ALL": "C.UTF-8", "SHELL": "/bin/bash", "XDG_STATE_HOME": self.tmp + "/state",
                    "XDG_CACHE_HOME": self.tmp + "/cache"}

    def tearDown(self):
        if hasattr(self, "sh"):
            self.sh.close()
        shutil.rmtree(self.tmp, ignore_errors=True)

    def slash(self, *args):
        return subprocess.run([os.path.join(self.bindir, "slash"), *args], env=self.env,
                              capture_output=True, text=True)

    def shell(self):
        self.sh = Shell(["bash", "--noprofile", "-i"], self.env)
        self.sh.expect("PROMPT> ")
        self.sh.mark()

    def test_enable_use_disable(self):
        r = self.slash("enable")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("enabled for bash", r.stdout)

        self.shell()
        self.sh.send("update\r")
        self.sh.expect("FAKE-sudo apt update")
        self.sh.expect("FAKE-flatpak update")
        self.sh.expect("FAKE-sudo snap refresh")
        self.sh.mark()
        self.sh.send("history 3\r")
        self.sh.expect(r"sudo apt update && sudo apt upgrade; flatpak update; sudo snap refresh  #slash: update")
        self.sh.mark()
        self.sh.send("install -m 644 %s %s/copy\r" % (self.rc, self.tmp))  # coreutils install, untouched
        self.sh.expect("PROMPT> ")
        self.assertTrue(os.path.exists(os.path.join(self.tmp, "copy")))
        self.sh.close()
        del self.sh

        r = self.slash("disable")
        self.assertEqual(r.returncode, 0, r.stderr)
        with open(self.rc) as f:
            self.assertEqual(f.read(), self.original, "rc file must be byte-identical")
        self.assertFalse(os.path.exists(self.tmp + "/state/slash"))

        self.shell()
        self.sh.send("update\r")
        self.sh.expect(r"(?i)not found")
        self.assertNotIn("FAKE-", self.sh.text())


if __name__ == "__main__":
    unittest.main(verbosity=2)
