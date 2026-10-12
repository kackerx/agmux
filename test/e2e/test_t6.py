"""T6 `agmux statusline` + `agmux setup`. Spec: specs/m1-mvp/design.md「数据源 B」(+细则), specs/m1-mvp/tasks.md T6.

Everything runs against a temporary HOME / XDG_STATE_HOME. The real ~/.claude/settings.json is never written; setUpModule
records its hash and tearDownModule proves it is unchanged.
"""
import hashlib, json, os, pathlib, subprocess, tempfile, threading, time, unittest
from lib import BIN

REAL_SETTINGS = pathlib.Path(os.path.expanduser("~/.claude/settings.json"))
_real_hash = None


def _h(p):
    try:
        return hashlib.sha256(p.read_bytes()).hexdigest()
    except OSError:
        return None


def setUpModule():
    global _real_hash
    _real_hash = _h(REAL_SETTINGS)


def tearDownModule():
    assert _h(REAL_SETTINGS) == _real_hash, "the REAL ~/.claude/settings.json changed during the tests"


class Sandbox:
    def __init__(self):
        self.tmp = pathlib.Path(tempfile.mkdtemp(prefix="agmux-t6-"))
        self.home, self.state, self.bin = self.tmp / "home", self.tmp / "state", self.tmp / "bin"
        for d in (self.home, self.state, self.bin):
            d.mkdir()
        os.symlink(BIN, self.bin / "agmux")  # settings.json commands say plain `agmux`
        self.settings = self.home / ".claude" / "settings.json"
        assert not str(self.home).startswith(os.path.expanduser("~") + "/.claude")

    def env(self, **kw):
        e = {"PATH": f"{self.bin}:/usr/bin:/bin", "HOME": str(self.home), "XDG_STATE_HOME": str(self.state), "LANG": "en_US.UTF-8"}
        e.update(kw)
        return e

    def run(self, *args, stdin=b"", env=None, timeout=20):
        return subprocess.run([BIN, *args], input=stdin, capture_output=True, env=env or self.env(), timeout=timeout)

    def hud(self, sid):
        return self.state / "agmux" / f"{sid}.json"

    def write_settings(self, obj_or_text):
        self.settings.parent.mkdir(exist_ok=True)
        self.settings.write_text(obj_or_text if isinstance(obj_or_text, str) else json.dumps(obj_or_text, indent=2, ensure_ascii=False))
        return self.settings.read_bytes()


def payload(sid="sess-1", pct=41.6, **extra):
    d = {"session_id": sid, "model": {"display_name": "Opus 5.5"}, "context_window": {"used_percentage": pct, "remaining_percentage": 100 - pct},
         "rate_limits": {"five_hour": {"used_percentage": 7}}}
    d.update(extra)
    return json.dumps(d, ensure_ascii=False).encode()


class Base(unittest.TestCase):
    def setUp(self):
        self.sb = Sandbox()
        self.addCleanup(self.cleanup)

    def cleanup(self):  # some tests chmod directories read-only
        import shutil
        subprocess.run(["chmod", "-R", "u+rwx", str(self.sb.tmp)])
        shutil.rmtree(self.sb.tmp, ignore_errors=True)


class Statusline(Base):
    def test_passthrough_and_file(self):
        p = payload()
        r = self.sb.run("statusline", "--", "cat", stdin=p)
        self.assertEqual((r.returncode, r.stdout), (0, p), r.stderr)
        self.assertEqual(self.sb.hud("sess-1").read_bytes(), p, "file holds the exact payload")
        self.assertEqual([x.name for x in (self.sb.state / "agmux").iterdir()], ["sess-1.json"], "no tmp file left behind")

    def test_exit_code_and_stdout_passthrough(self):
        r = self.sb.run("statusline", "--", "echo '状态栏'; echo oops >&2; exit 3", stdin=payload())
        self.assertEqual((r.returncode, r.stdout), (3, "状态栏\n".encode()))
        self.assertTrue(self.sb.hud("sess-1").exists(), "file is written even when the original fails")

    def test_command_is_a_shell_string(self):
        r = self.sb.run("statusline", "--", "cat | tr a-z A-Z; echo $HOME", stdin=b'{"session_id":"s","x":"abc"}')
        self.assertEqual(r.stdout.decode(), '{"SESSION_ID":"S","X":"ABC"}' + f"{self.sb.home}\n", "pipes and $HOME are handled by the sh -c layer")
        r = self.sb.run("statusline", "--", "echo", "a", "b", stdin=payload("s2"))  # several args are joined with spaces, then sh -c
        self.assertEqual(r.stdout, b"a b\n")

    def test_no_original_command(self):
        r = self.sb.run("statusline", stdin=payload())
        self.assertEqual((r.returncode, r.stdout), (0, b""), r.stderr)
        self.assertTrue(self.sb.hud("sess-1").exists())
        r = self.sb.run("statusline", "--", stdin=payload("s9"))  # bare `--`
        self.assertEqual(r.returncode, 0, r.stderr)

    def test_not_json_or_no_session_id_writes_nothing(self):
        for stdin in (b"not json at all", b"", b'{"model":{"display_name":"x"}}', b'{"session_id":""}', b'{"session_id":5}', b"[1,2]"):
            with self.subTest(stdin=stdin):
                r = self.sb.run("statusline", "--", "cat; exit 5", stdin=stdin)
                self.assertEqual((r.returncode, r.stdout), (5, stdin), "passthrough + the original's exit code")
                r = self.sb.run("statusline", stdin=stdin)
                self.assertEqual((r.returncode, r.stdout), (0, b""))
                d = self.sb.state / "agmux"
                self.assertEqual(list(d.iterdir()) if d.exists() else [], [], "nothing written")

    def test_unknown_command_exits_127_file_still_written(self):
        r = self.sb.run("statusline", "--", "/nonexistent/statusline-cmd", stdin=payload())
        self.assertEqual(r.returncode, 127, r.stderr); self.assertTrue(self.sb.hud("sess-1").exists())
        # the shell itself cannot start (no sh on PATH): agmux reports one line and exits 127
        r = self.sb.run("statusline", "--", "echo hi", stdin=payload("s3"), env=self.sb.env(PATH="/nonexistent"))
        self.assertEqual(r.returncode, 127)
        self.assertRegex(r.stderr.decode().strip(), r"^agmux statusline: .+")
        self.assertEqual(len(r.stderr.decode().strip().splitlines()), 1, r.stderr)
        self.assertTrue(self.sb.hud("s3").exists(), "file written before the original is started")

    def test_unwritable_state_dir_does_not_break_the_status_line(self):
        blocker = self.sb.tmp / "blocker"; blocker.write_text("a file, not a dir")
        r = self.sb.run("statusline", "--", "echo STATUS; exit 2", stdin=payload(), env=self.sb.env(XDG_STATE_HOME=str(blocker)))
        self.assertEqual((r.returncode, r.stdout), (2, b"STATUS\n"), r.stderr)
        ro = self.sb.tmp / "ro"; ro.mkdir(); ro.chmod(0o555)
        r = self.sb.run("statusline", "--", "cat", stdin=payload(), env=self.sb.env(XDG_STATE_HOME=str(ro)))
        self.assertEqual((r.returncode, r.stdout), (0, payload()), r.stderr)
        r = self.sb.run("statusline", stdin=payload(), env=self.sb.env(XDG_STATE_HOME=str(ro)))
        self.assertEqual(r.returncode, 0, "no original command and the write fails: still exit 0")

    def test_session_id_cannot_escape_the_state_dir(self):
        for sid in ("../escape", "../../escape", "a/b", "/tmp/agmux-t6-abs"):
            with self.subTest(sid=sid):
                self.sb.run("statusline", "--", "cat", stdin=payload(sid))
        found = [str(p) for p in self.sb.tmp.rglob("*.json") if (self.sb.state / "agmux") not in p.parents]
        self.assertEqual(found, [], "a session_id with path separators must not write outside $XDG_STATE_HOME/agmux")
        self.assertFalse(pathlib.Path("/tmp/agmux-t6-abs.json").exists())

    def test_session_id_charset(self):  # spec 2e2bca5: only [A-Za-z0-9_-]+ is accepted, anything else -> no file, still passthrough
        for sid in ("a.b", "a b", ".", "..", "中文", "a%2Fb", "a\nb", "x" * 0):
            with self.subTest(bad=sid):
                r = self.sb.run("statusline", "--", "cat", stdin=payload(sid))
                self.assertEqual((r.returncode, r.stdout), (0, payload(sid)))
        d = self.sb.state / "agmux"
        self.assertEqual(list(d.iterdir()) if d.exists() else [], [], "no file for a session_id outside [A-Za-z0-9_-]+")
        for sid in ("abc-DEF_123", "0", "e44efbbf-befc-489b-ab0d-9f30e9217f7e"):
            with self.subTest(good=sid):
                self.sb.run("statusline", stdin=payload(sid)); self.assertTrue(self.sb.hud(sid).exists())

    def test_big_payload_and_child_that_ignores_stdin(self):
        big = payload(pad="x" * (2 << 20))
        r = self.sb.run("statusline", "--", "cat", stdin=big)
        self.assertEqual((r.returncode, len(r.stdout)), (0, len(big))); self.assertEqual(self.sb.hud("sess-1").read_bytes(), big)
        t0 = time.time(); r = self.sb.run("statusline", "--", "echo hi", stdin=big)  # never reads stdin: EPIPE must not fail or hang
        self.assertEqual((r.returncode, r.stdout), (0, b"hi\n"), r.stderr); self.assertLess(time.time() - t0, 5)

    def test_fast(self):
        t0 = time.time(); [self.sb.run("statusline", "--", "cat", stdin=payload()) for _ in range(10)]
        self.assertLess((time.time() - t0) / 10, 0.5, "the status line runs on every refresh: wrapper overhead must stay small")

    def test_atomic_write_readers_never_see_a_partial_file(self):
        stop, bad, reads = threading.Event(), [], [0]
        path = self.sb.hud("sess-1")
        sizes = [payload(pad="y" * n) for n in (10, 200_000, 50, 400_000)]

        def reader():
            while not stop.is_set():
                try:
                    data = path.read_bytes()
                except FileNotFoundError:
                    continue
                reads[0] += 1
                try:
                    json.loads(data)
                except ValueError:
                    bad.append(len(data))

        th = threading.Thread(target=reader); th.start()
        try:
            for i in range(40):
                self.sb.run("statusline", stdin=sizes[i % 4])
        finally:
            stop.set(); th.join()
        self.assertGreater(reads[0], 10); self.assertEqual(bad, [], "partial / torn file observed (write is not atomic)")

    def test_concurrent_sessions(self):
        ths = [threading.Thread(target=lambda i=i: self.sb.run("statusline", "--", "cat", stdin=payload(f"c{i}", pct=i))) for i in range(12)]
        [t.start() for t in ths]; [t.join() for t in ths]
        for i in range(12):
            self.assertEqual(json.loads(self.sb.hud(f"c{i}").read_bytes())["context_window"]["used_percentage"], i)


class Setup(Base):
    def lines(self, r):
        return r.stdout.decode().splitlines()

    def test_no_statusline_dry_run(self):
        raw = self.sb.write_settings({"theme": "dark"})
        r = self.sb.run("setup")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.lines(r), ["statusLine.command:", "  before: (none)", "  after:  agmux statusline", "dry run: add --write to apply"])
        self.assertEqual(self.sb.settings.read_bytes(), raw, "dry run changes nothing")
        self.assertFalse((self.sb.settings.parent / "settings.json.bak").exists())

    def test_wraps_existing_command(self):
        self.sb.write_settings({"statusLine": {"type": "command", "command": "~/.config/bin/claude-hud", "padding": 0}})
        r = self.sb.run("setup")
        self.assertEqual(self.lines(r), ["statusLine.command:", "  before: ~/.config/bin/claude-hud",
                                         "  after:  agmux statusline -- '~/.config/bin/claude-hud'", "dry run: add --write to apply"])

    def test_quote_escaping(self):
        self.sb.write_settings({"statusLine": {"type": "command", "command": "echo it's $HOME | cat"}})
        r = self.sb.run("setup")
        self.assertIn("  after:  agmux statusline -- 'echo it'\\''s $HOME | cat'", self.lines(r))

    def test_already_agmux(self):
        for cmd in ("agmux statusline", "agmux statusline -- 'foo --bar'"):
            with self.subTest(cmd=cmd):
                raw = self.sb.write_settings({"statusLine": {"type": "command", "command": cmd}})
                for args in (("setup",), ("setup", "--write")):
                    r = self.sb.run(*args)
                    self.assertEqual((r.returncode, self.lines(r)), (0, [f"already set: {cmd}"]), r.stderr)
                    self.assertEqual(self.sb.settings.read_bytes(), raw, "untouched")
                self.assertFalse((self.sb.settings.parent / "settings.json.bak").exists(), "nothing to back up when nothing changes")

    def test_write_preserves_everything_else_and_backs_up(self):
        orig = {"theme": "dark", "permissions": {"allow": ["Bash(ls:*)"], "deny": []}, "env": {"A": "1", "中文": "值"}, "n": 3, "f": 1.5, "t": True, "nil": None,
                "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "echo '<&>'"}]}]},
                "statusLine": {"type": "command", "command": "~/.config/bin/claude-hud", "padding": 2}}
        raw = self.sb.write_settings(orig)
        r = self.sb.run("setup", "--write")
        out = self.lines(r)
        self.assertEqual(r.returncode, 0, r.stderr)
        bak = self.sb.settings.parent / "settings.json.bak"
        self.assertEqual(out[:3], ["statusLine.command:", "  before: ~/.config/bin/claude-hud", "  after:  agmux statusline -- '~/.config/bin/claude-hud'"])
        self.assertEqual(out[3], f"written (backup: {bak})")
        self.assertEqual(bak.read_bytes(), raw, "backup is the byte-identical original")
        new = json.loads(self.sb.settings.read_text())
        want = json.loads(json.dumps(orig)); want["statusLine"]["command"] = "agmux statusline -- '~/.config/bin/claude-hud'"
        self.assertEqual(new, want, "only statusLine.command changes; type / padding and every other key survive")
        txt = self.sb.settings.read_text()
        self.assertIn("中文", txt, "non-ASCII must not be \\u-escaped"); self.assertIn("<&>", txt, "<, >, & must not be \\u003c-escaped")

    def test_write_adds_statusline_when_missing(self):
        self.sb.write_settings({"theme": "dark"})
        r = self.sb.run("setup", "--write"); self.assertEqual(r.returncode, 0, r.stderr)
        new = json.loads(self.sb.settings.read_text())
        self.assertEqual(new["theme"], "dark"); self.assertEqual(new["statusLine"]["command"], "agmux statusline")
        self.assertEqual(new["statusLine"].get("type"), "command", "Claude Code needs type=command")

    def test_write_creates_missing_file(self):
        self.assertFalse(self.sb.settings.exists())
        r = self.sb.run("setup", "--write"); self.assertEqual(r.returncode, 0, r.stderr + r.stdout)
        self.assertEqual(list(json.loads(self.sb.settings.read_text())), ["statusLine"], "new file holds only statusLine")
        self.assertEqual(self.sb.run("setup").stdout.decode().splitlines()[0], "already set: agmux statusline")

    def test_dry_run_without_a_file(self):
        r = self.sb.run("setup"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.lines(r)[:3], ["statusLine.command:", "  before: (none)", "  after:  agmux statusline"])
        self.assertFalse(self.sb.settings.exists(), "dry run must not create the file")

    def test_corrupt_json_untouched_exit_1(self):
        for text in ("{not json", "", "[1,2]", '"str"', '{"statusLine": 5}'):
            with self.subTest(text=text):
                raw = self.sb.write_settings(text)
                for args in (("setup",), ("setup", "--write")):
                    r = self.sb.run(*args)
                    self.assertEqual(r.returncode, 1, f"{args}: {r.stdout!r} {r.stderr!r}")
                    self.assertTrue(r.stderr.strip(), "error goes to stderr"); self.assertEqual(self.sb.settings.read_bytes(), raw, "original untouched")
                self.assertFalse((self.sb.settings.parent / "settings.json.bak").exists())

    def test_no_write_permission(self):
        raw = self.sb.write_settings({"theme": "dark"})
        self.sb.settings.chmod(0o444); self.sb.settings.parent.chmod(0o555)
        r = self.sb.run("setup", "--write")
        self.assertEqual(r.returncode, 1, f"{r.stdout!r} {r.stderr!r}"); self.assertTrue(r.stderr.strip())
        self.sb.settings.parent.chmod(0o755); self.sb.settings.chmod(0o644)
        self.assertEqual(self.sb.settings.read_bytes(), raw, "original untouched")
        self.assertEqual([p.name for p in self.sb.settings.parent.iterdir()], ["settings.json"], "no stray backup / tmp files")

    def test_second_write_keeps_the_original_backup(self):
        raw = self.sb.write_settings({"statusLine": {"type": "command", "command": "mine.sh"}})
        self.sb.run("setup", "--write"); self.sb.run("setup", "--write")
        self.assertEqual((self.sb.settings.parent / "settings.json.bak").read_bytes(), raw, ".bak must stay the user's ORIGINAL file")

    def test_symlinked_settings_survive(self):  # ~/.claude/settings.json is often a symlink into a dotfiles repo
        real = self.sb.tmp / "dotfiles" / "settings.json"; real.parent.mkdir()
        real.write_text(json.dumps({"theme": "dark", "statusLine": {"type": "command", "command": "hud.sh"}}))
        self.sb.settings.parent.mkdir(); os.symlink(real, self.sb.settings)
        r = self.sb.run("setup", "--write"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(self.sb.settings.is_symlink(), "settings.json must still be a symlink (dotfiles setups break otherwise)")
        self.assertEqual(os.readlink(self.sb.settings), str(real), "link target unchanged")
        self.assertEqual(json.loads(real.read_text())["statusLine"]["command"], "agmux statusline -- 'hud.sh'", "the link target is what gets updated")
        self.assertEqual(json.loads((real.parent / "settings.json.bak").read_text())["statusLine"]["command"], "hud.sh", ".bak sits next to the TARGET")
        self.assertFalse((self.sb.settings.parent / "settings.json.bak").exists(), "no .bak next to the link")
        self.assertEqual(sorted(p.name for p in real.parent.iterdir()), ["settings.json", "settings.json.bak"], "no tmp files left in the target dir")

    def test_bad_usage(self):
        for args in (("setup", "--bogus"), ("setup", "extra")):
            r = self.sb.run(*args); self.assertEqual(r.returncode, 2, f"{args}: {r.stderr!r}")
        self.assertFalse(self.sb.settings.exists())

    def test_no_home_errors(self):
        r = self.sb.run("setup", env={"PATH": "/usr/bin:/bin"}); self.assertEqual(r.returncode, 1); self.assertIn("HOME", r.stderr.decode())

    def test_end_to_end_claude_code_would_run_it(self):
        """setup --write, then run the resulting command the way Claude Code does (`sh -c <command>`, payload on stdin)."""
        script = self.sb.tmp / "my hud.sh"; script.write_text("#!/bin/sh\ncat >/dev/null\nprintf 'HUD-LINE'\nexit 4\n"); script.chmod(0o755)
        self.sb.write_settings({"statusLine": {"type": "command", "command": f"'{script}'"}})
        self.assertEqual(self.sb.run("setup", "--write").returncode, 0)
        cmd = json.loads(self.sb.settings.read_text())["statusLine"]["command"]
        p = payload("e2e", pct=29.6)
        r = subprocess.run(["sh", "-c", cmd], input=p, capture_output=True, env=self.sb.env())
        self.assertEqual((r.returncode, r.stdout), (4, b"HUD-LINE"), r.stderr)
        self.assertEqual(self.sb.hud("e2e").read_bytes(), p)


if __name__ == "__main__":
    unittest.main()
