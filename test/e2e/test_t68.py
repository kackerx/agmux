"""T68 pi on the board: the extension agmux installs (internal/pi/agmux.js, `agmux setup --write` puts it in <pi dir>/extensions/agmux.js; the pi dir is $PI_CODING_AGENT_DIR, else ~/.pi/agent) writes one file per running pi
with a UI into <pi dir>/agmux-sessions/<pid>.json ({pid, sessionId, cwd, transcript, name, status idle|working|waiting, pendingTool, statusAt, updatedAt}); the board lists those whose process is there and was started
no later than the file was last written (a reused pid: the file is of one that is gone: dropped and removed; so is the file of a dead pid; a file that does not parse is only left out); the pane is found by climbing the
process's parents to a tmux pane's pane_pid (list-panes' 6th column); a pi has no context / model; a name is the file's. Hints: with a process called pi and no pi session known the footer says `pi: run agmux setup --write`
(extension not installed) or `pi: restart the running pi sessions to show them` (installed). `agmux setup` also installs the extension: a dry run says what it would do, --write does it (atomically, once: the same file is
`already installed`, an edited one is left alone), no pi dir: skipped. r on a pi is the process ended and `pi --session <transcript>` typed into its pane; x ends it. Spec: specs/m3-config/tasks.md「第十四批」T68, design §9.
The files here are written by hand (Env.proc gives a live process); the harness' fake `ps` hides the machine's own pi processes and adds the rows of $fake/ps.extra."""
import json, os, re, subprocess, time, unittest
from lib import *
from test_t3 import last_text_row
from test_t60 import patch_tmux

EVENTS = ["session_start", "session_shutdown", "session_info_changed", "agent_start", "agent_end", "tool_execution_start", "tool_execution_end", "ui_prompt_start", "ui_prompt_end"]


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); patch_tmux(self.env); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        self.pidir = self.env.home / ".pi" / "agent"; self.sessions = self.pidir / "agmux-sessions"

    def pi(self, sid, status="idle", pid=None, **kw):
        self.sessions.mkdir(parents=True, exist_ok=True); pid = pid or self.env.proc(sid)[0]; now = int(time.time() * 1000)
        e = dict(pid=pid, sessionId=sid, cwd=str(self.env.home / "work" / sid), transcript=str(self.env.tmp / f"{sid}.jsonl"), name=sid, status=status, statusAt=now, updatedAt=now); e.update(kw); f = self.sessions / f"{pid}.json"; f.write_text(json.dumps(e)); return f

    def cli(self, *args, extra=None):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8", **(extra or {})}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=30, stdin=subprocess.DEVNULL)

    def show(self):
        out = self.cli("show"); self.assertEqual(out.returncode, 0, out.stderr); return {l.split("\t")[0]: l.split("\t") for l in out.stdout.splitlines() if l}

    def board(self, w=100, h=50, n=1):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) >= n, 10), t.text()); time.sleep(0.4); return t

    def msg(self, t): return "\n".join(r.text for r in t.snap()[-3:])


class Sessions(Base):
    def test_live_pi_sessions_are_listed_by_session_id_with_the_files_name_and_status(self):
        for i, st in enumerate(("idle", "working", "waiting", "frobnicating")): self.pi(f"p{i}", st, name=f"pi-{i}")
        rows = self.show(); self.assertEqual(sorted(rows), ["p0", "p1", "p2", "p3"]); self.assertEqual([rows[f"p{i}"][1] for i in range(4)], [f"pi-{i}" for i in range(4)]); self.assertEqual([rows[f"p{i}"][3] for i in range(4)], ["idle", "working", "waiting", "idle"])
        self.assertEqual(rows["p0"][4], ""); self.assertEqual(rows["p0"][5], "--", "no model, no context: a pi has no statusline")

    def test_a_pi_and_a_claude_session_are_on_one_board(self):
        self.pi("pip", "working", name="the-pi"); self.env.reg("clau", name="the-claude", nameSource="user", status="busy"); self.assertEqual(sorted(self.show()), ["clau", "pip"]); t = self.board(w=200, n=2)
        self.assertTrue(all(any(n in c.line(1) for c in find_cards(t.snap())) for n in ("the-pi", "the-claude")))

    def test_a_dead_pid_and_a_reused_pid_are_not_sessions_and_their_files_are_removed(self):
        self.pi("live", name="live"); d = self.pi("dead", pid=self.env.dead_pid(), name="dead"); old = self.pi("reused", name="reused", updatedAt=int(time.time() * 1000) - 3600 * 1000)  # the process started after the file was written
        self.assertEqual(sorted(self.show()), ["live"]); self.assertFalse(d.exists(), "the file of a pi that is gone is the extension's own: removed"); self.assertFalse(old.exists(), "and so is the one of a reused pid")
        self.assertTrue((self.sessions / f"{self.env.proc('live')[0]}.json").exists())

    def test_files_that_do_not_parse_or_say_nothing_are_left_out_one_by_one(self):
        self.pi("good", name="good"); (self.sessions / "1.json").write_text("{not json"); (self.sessions / "2.json").write_text(""); (self.sessions / "3.json").write_text(json.dumps(dict(pid=3))); (self.sessions / "4.json").write_text(json.dumps(dict(pid=0, sessionId="zero")))
        (self.sessions / "notes.txt").write_text("x"); self.assertEqual(sorted(self.show()), ["good"])

    def test_the_half_written_file_of_a_dead_pid_is_swept_and_a_live_ones_is_left(self):
        self.pi("good", name="good"); dead = self.env.dead_pid(); a = self.sessions / f"{dead}.json.tmp.abc"; a.write_text("{"); b = self.sessions / f"{self.env.proc('good')[0]}.json.tmp.def"; b.write_text("{")
        self.show(); self.assertFalse(a.exists()); self.assertTrue(b.exists())

    def test_no_pi_directory_is_an_empty_pi_not_an_error(self):
        r = self.cli("show"); self.assertEqual((r.returncode, r.stdout, r.stderr), (0, "", ""))

    def test_pi_agent_dir_moves_the_directory(self):
        other = self.env.tmp / "otherpi"; (other / "agmux-sessions").mkdir(parents=True); pid, ps = self.env.proc("elsewhere"); now = int(time.time() * 1000)
        (other / "agmux-sessions" / f"{pid}.json").write_text(json.dumps(dict(pid=pid, sessionId="there", cwd="/tmp/x", transcript="", name="there", status="idle", statusAt=now, updatedAt=now)))
        self.pi("home-one"); out = self.cli("show", extra={"PI_CODING_AGENT_DIR": str(other)}); self.assertEqual([l.split("\t")[0] for l in out.stdout.splitlines()], ["there"])

    def test_the_pending_tool_is_the_files_and_a_working_pi_shows_it(self):
        self.pi("pt", "working", name="tooling", pendingTool="bash"); t = self.board(); self.assertIn("working · bash", self.card(t, "tooling").line(3))

    def card(self, t, name): m = [c for c in find_cards(t.snap()) if name in c.line(1)]; self.assertEqual(len(m), 1, t.text()); return m[0]


class Panes(Base):
    def test_the_pane_is_found_by_climbing_the_parents_to_a_panes_pid(self):
        # the harness' process is a child of this python: a pane whose pane_pid is this process is its pane
        self.pi("pc", name="climber"); (self.env.fake / "tmux.panes").write_text(f"%66\tsess # w\t0\tpi\tπ - x\t{os.getpid()}\n"); t = self.board()
        self.assertEqual(self.msg(t).count("no tmux pane"), 0, "it has a pane"); (self.env.fake / "tmux.panes").write_text("%66\tsess # w\t0\tpi\tπ - x\t999999\n")  # a pane_pid nobody climbs to
        self.assertTrue(wait(lambda: "no tmux pane: can't jump or send" in self.msg(t), 6), self.msg(t))

    def test_a_pi_card_with_a_pane_can_be_jumped_to(self):
        self.pi("pj", name="jumper"); (self.env.fake / "tmux.panes").write_text(f"%67\tsess # w\t0\tpi\tπ - x\t{os.getpid()}\n"); t = self.board(); t.keys("Enter"); self.assertEqual(t.exit_status(6), 0, t.text()); self.assertIn("%67", [c[-1] for c in self.env.tmux_calls() if c[0] == "switch-client"])

    def test_a_pane_title_is_not_a_pis_name(self):
        self.pi("pt", name="own-name"); (self.env.fake / "tmux.panes").write_text(f"%68\tsess # w\t0\tpi\tπ - some title\t{os.getpid()}\n"); self.assertEqual(self.show()["pt"][1], "own-name")


class Hints(Base):
    def test_a_pi_process_and_no_pi_session_say_how_to_get_it_shown(self):
        (self.env.fake / "ps.extra").write_text("99999 1 pi\n"); self.env.reg("c1", name="claude-one", nameSource="user"); t = self.board(); self.assertTrue(wait(lambda: "pi: run agmux setup --write" in self.msg(t), 5), self.msg(t))

    def test_with_the_extension_installed_the_hint_says_to_restart_the_running_pis(self):
        (self.env.fake / "ps.extra").write_text("99999 1 /opt/homebrew/bin/pi\n"); (self.pidir / "extensions").mkdir(parents=True); (self.pidir / "extensions" / "agmux.js").write_text("// x"); self.env.reg("c1", name="claude-one", nameSource="user")
        t = self.board(); self.assertTrue(wait(lambda: "pi: restart the running pi sessions to show them" in self.msg(t), 5), self.msg(t)); self.assertNotIn("setup --write", self.msg(t))

    def test_no_hint_when_a_pi_session_is_known_or_there_is_no_pi_process(self):
        (self.env.fake / "ps.extra").write_text("99999 1 pi\n"); self.pi("known", name="known-pi"); self.env.reg("c1", name="claude-one", nameSource="user"); t = self.board(w=200, n=2); time.sleep(3.0); self.assertNotIn("pi:", self.msg(t))
        (self.env.fake / "ps.extra").unlink(); t2 = Term(self.env, w=100, h=50, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t2.close); time.sleep(3.5); self.assertNotIn("pi:", self.msg(t2))


class Setup(Base):
    def run_setup(self, *a, extra=None): return self.cli("setup", *a, extra={"PI_CODING_AGENT_DIR": str(self.pidir), **(extra or {})})

    def ext(self): return self.pidir / "extensions" / "agmux.js"

    def test_the_extension_source_names_the_events_it_listens_to(self):
        src = (SRC / "internal" / "pi" / "agmux.js").read_text()
        for e in EVENTS: self.assertIn(f'"{e}"', src, e)
        self.assertIn("agmux-sessions", src); self.assertIn("PI_CODING_AGENT_DIR", src)

    def test_a_dry_run_says_what_it_would_do_and_changes_nothing(self):
        self.pidir.mkdir(parents=True); r = self.run_setup(); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("pi: would install the extension", r.stdout); self.assertIn(str(self.ext()), r.stdout); self.assertIn("dry run: add --write to apply", r.stdout); self.assertFalse(self.ext().exists())
        self.assertFalse((self.pidir / "extensions").exists())

    def test_write_installs_the_embedded_file_atomically_and_a_second_run_changes_nothing(self):
        self.pidir.mkdir(parents=True); r = self.run_setup("--write"); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("pi: installed", r.stdout); self.assertEqual(self.ext().read_bytes(), (SRC / "internal" / "pi" / "agmux.js").read_bytes())
        self.assertEqual(os.stat(self.ext()).st_mode & 0o777, 0o644); self.assertEqual([p.name for p in (self.pidir / "extensions").iterdir()], ["agmux.js"], "no temporary file is left (and none is a *.js that pi would load)")
        m = self.ext().stat().st_mtime_ns; r2 = self.run_setup("--write"); self.assertIn("pi: extension already installed", r2.stdout); self.assertEqual(self.ext().stat().st_mtime_ns, m)
        self.assertIn("already installed", self.run_setup().stdout, "the dry run says so too")

    def test_an_edited_or_other_version_is_left_alone(self):
        self.ext().parent.mkdir(parents=True); self.ext().write_text("// my own version\n")
        for args in (("--write",), ()):
            r = self.run_setup(*args); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("differs from agmux's extension", r.stdout); self.assertIn("left alone", r.stdout)
        self.assertEqual(self.ext().read_text(), "// my own version\n")

    def test_no_pi_directory_is_skipped_with_a_line_and_exit_0(self):
        r = self.run_setup("--write"); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("not found, extension skipped", r.stdout); self.assertFalse(self.pidir.exists())

    def test_the_default_directory_is_under_home_and_the_claude_part_is_the_same_command(self):
        (self.env.home / ".pi" / "agent").mkdir(parents=True); (self.env.home / ".claude").mkdir(exist_ok=True); r = self.cli("setup", "--write"); self.assertEqual(r.returncode, 0, r.stderr); self.assertTrue((self.env.home / ".pi" / "agent" / "extensions" / "agmux.js").exists())
        self.assertIn("written", r.stdout); self.assertIn("statusLine", (self.env.home / ".claude" / "settings.json").read_text())

    def test_the_extension_file_is_valid_javascript_when_node_is_there(self):
        import shutil
        if not shutil.which("node"): self.skipTest("no node")
        r = subprocess.run(["node", "--check", str(SRC / "internal" / "pi" / "agmux.js")], capture_output=True, text=True)
        if r.returncode and "ES module" in r.stderr: self.skipTest("esm")
        self.assertEqual(r.returncode, 0, r.stderr)


class Control(Base):
    def test_r_on_a_pi_ends_the_process_and_types_pi_session_with_the_transcript(self):
        tr = self.env.tmp / "pi sessions" / "t 1.jsonl"; self.pi("pr", name="restarter", transcript=str(tr)); (self.env.fake / "tmux.panes").write_text(f"%69\tsess # w\t0\tpi\tπ - x\t{os.getpid()}\n"); t = self.board()
        t.keys("r"); self.assertTrue(wait(lambda: ["send-keys", "-t", "%69", "Enter"] in self.env.tmux_calls(), 8), self.env.tmux_calls() + [t.text()])
        sent = [c for c in self.env.tmux_calls() if c[0] == "send-keys" and "-l" in c]; self.assertEqual(sent, [["send-keys", "-t", "%69", "-l", "--", f"pi --session '{tr}'"]], "the transcript is quoted as one word"); self.assertEqual(self.env.killed(), ["pr"])

    def test_r_on_a_pi_that_did_not_say_its_session_file_is_an_error_and_nothing_is_signalled(self):
        self.pi("pn", name="nofile", transcript=""); (self.env.fake / "tmux.panes").write_text(f"%70\tsess # w\t0\tpi\tπ - x\t{os.getpid()}\n"); t = self.board(); t.keys("r"); self.assertTrue(wait(lambda: "has not said which session file" in self.msg(t), 4), self.msg(t))
        self.assertEqual(self.env.killed(), []); self.assertEqual([c for c in self.env.tmux_calls() if c[0] == "send-keys"], [])

    def test_x_ends_a_pi_and_it_leaves_the_board(self):
        self.pi("px", name="goner"); self.pi("py", name="stayer"); t = self.board(w=200, n=2)
        for k in ("x", "y"): t.keys(k); time.sleep(0.3)
        self.assertTrue(wait(lambda: self.env.killed() == ["px"] or self.env.killed() == ["py"], 5), self.env.killed()); self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) == 1, 6), t.text())


if __name__ == "__main__":
    unittest.main()
