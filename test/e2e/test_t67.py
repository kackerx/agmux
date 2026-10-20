"""T67 / T72 / T73 the sessions come from Claude Code's own register, not from ccmux: <config dir>/sessions/<pid>.json ($CLAUDE_CONFIG_DIR, else ~/.claude) -- a process that is there (kill(pid, 0)) and whose
procStart is the one ps says (a reused pid is no session); `spare: true`, a dead pid, a file that does not parse, `<pid>.<hash>.key` are not sessions (one bad file drops only itself); status busy -> working, waiting -> waiting,
idle / shell / anything else -> idle (waitingFor "permission prompt" -> permission, else question); a background session (kind bg) is its jobId and its state / name / transcript are in jobs/<jobId>/state.json; the
name is the register's name given by the user or a peer, else the transcript's custom-title, else the summary in the pane's title (tmux list-panes' 5th column; the ✳ / braille spinner in front and `Claude Code` are
not a summary), else the name Claude made up (nameSource derived), else the project (the directory's repo, else its name); the tmux session shown is the first word of the register's `tmux` field before ` # `, and what
tmux says of the pane when it says; the transcript is the statusline's transcript_path, else projects/<cwd with every other character a "-">/<session id>.jsonl; branch / pending tool / plan to approve are read off its tail;
the project of a worktree is its main repo. A session is done when this board saw it work (or its status time moved since it first saw it) and it is idle and the done is not marked seen; a board that starts finds
every session idle that is idle then; the same id under another process is a new session. T72: the agent role is `$XDG_STATE_HOME/claude-agent/<session id>` first (`-` or none: the transcript's). T73: the footer says
`t pin` and `m read` after `a avatar`, with the keys the config gives. Spec: specs/m3-config/tasks.md「第十四批」, specs/m5-own-discovery/design.md. The register here is written by hand (Env.reg); `agmux show` and the board are the black box."""
import json, os, re, subprocess, time, unittest
from lib import *
from test_t3 import C_FOCUS, border_colours, last_text_row
from test_t60 import patch_tmux


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); patch_tmux(self.env); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def cli(self, *args, extra=None, text=None):
        if text is not None: (self.cfg / "agmux" / "config.toml").write_text(text)
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8", **(extra or {})}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=30, stdin=subprocess.DEVNULL)

    def show(self, **kw):
        out = self.cli("show", **kw); self.assertEqual(out.returncode, 0, out.stderr); return {l.split("\t")[0]: l.split("\t") for l in out.stdout.splitlines() if l}

    def name_of(self, sid): return self.show()[sid][1]

    def log(self, sid, lines, cwd=None):
        """the transcript of session sid where Claude Code keeps it"""
        slug = re.sub(r"[^A-Za-z0-9]", "-", cwd or str(self.env.home / "work" / sid)); f = self.env.claude / "projects" / slug / f"{sid}.jsonl"; f.parent.mkdir(parents=True, exist_ok=True)
        f.write_text("".join((l if isinstance(l, str) else json.dumps(l, separators=(",", ":"))) + "\n" for l in lines)); return f  # (compact: the reader looks for `"type":"assistant"` as a substring first)

    def panes(self, *lines): (self.env.fake / "tmux.panes").write_text("".join(l + "\n" for l in lines))

    def board(self, w=200, h=50, text=None, n=None):
        if text is not None: (self.cfg / "agmux" / "config.toml").write_text(text)
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        if n is not None: self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) >= n, 10), t.text()); time.sleep(0.4)
        return t

    def card(self, t, name):
        m = [c for c in find_cards(t.snap()) if name in c.line(1)]; self.assertEqual(len(m), 1, f"{name}: {len(m)} cards\n{t.text()}"); return m[0]

    def word(self, t, name):
        l3 = self.card(t, name).line(3); m = re.search(r"(⏵ working|⏸ idle|● done|⏳ waiting)", l3); return m.group(1) if m else l3


class Discovery(Base):
    def test_live_sessions_are_listed_by_session_id_with_their_name_and_directory(self):
        self.env.reg("sid-one", name="one", nameSource="user"); self.env.reg("sid-two", name="two", nameSource="user", cwd=str(self.env.home / "elsewhere")); rows = self.show()
        self.assertEqual(sorted(rows), ["sid-one", "sid-two"]); self.assertEqual((rows["sid-one"][1], rows["sid-two"][1]), ("one", "two")); self.assertEqual(rows["sid-two"][2], str(self.env.home / "elsewhere")); self.assertEqual(rows["sid-one"][7], "")

    def test_a_spare_process_a_dead_pid_and_a_reused_pid_are_not_sessions(self):
        self.env.reg("real", name="real", nameSource="user"); self.env.reg("spare", name="spare", spare=True); self.env.reg("dead", name="dead", pid=self.env.dead_pid(), procstart="Mon Jan  1 00:00:00 2024")
        self.env.reg("reused", name="reused", procstart="Mon Jan  1 00:00:00 2001")  # a live process that is not the one that registered
        self.assertEqual(sorted(self.show()), ["real"])

    def test_a_register_without_procstart_is_kept_and_the_ps_form_is_matched_loosely(self):
        self.env.reg("nops", name="nops", procstart=""); f = self.env.reg("sp", name="spaced"); d = json.loads(f.read_text()); d["procStart"] = d["procStart"].replace(" ", "  ", 1); f.write_text(json.dumps(d))
        self.assertEqual(sorted(self.show()), ["nops", "sp"])

    def test_only_json_files_and_a_bad_one_drops_only_itself(self):
        d = self.env.claude / "sessions"; self.env.reg("good", name="good", nameSource="user"); key = self.env.reg("keyed", name="keyed", file="55555.0123456789abcdef.key"); self.assertTrue(key.exists())
        (d / "11111.json").write_text("{not json"); (d / "22222.json").write_text(""); (d / "33333.json").write_text('{"pid": 33333, "sessionId": "trunc'); (d / "44444.json").write_text("[1, 2]"); (d / "notes.txt").write_text("hi")
        (d / "0.json").write_text(json.dumps(dict(pid=0, sessionId="zero"))); (d / "-5.json").write_text(json.dumps(dict(pid=-5, sessionId="neg")))
        self.assertEqual(sorted(self.show()), ["good"])

    def test_claude_config_dir_moves_the_register(self):
        self.env.reg("home-one", name="home-one"); other = self.env.tmp / "other-claude"; (other / "sessions").mkdir(parents=True); pid, ps = self.env.proc("elsewhere")
        (other / "sessions" / f"{pid}.json").write_text(json.dumps(dict(pid=pid, sessionId="there", cwd="/tmp/x", procStart=ps, kind="interactive", status="idle", name="there", nameSource="user", statusUpdatedAt=1, updatedAt=1)))
        self.assertEqual(sorted(self.show(extra={"CLAUDE_CONFIG_DIR": str(other)})), ["there"]); self.assertEqual(sorted(self.show()), ["home-one"])

    def test_a_process_that_ends_is_gone_at_the_next_poll_and_a_register_that_is_deleted_too(self):
        self.env.reg("a", name="a", nameSource="user"); self.env.reg("b", name="b", nameSource="user"); t = self.board(w=100, n=2); self.assertEqual(sorted(c.line(1)[8:12].strip() for c in find_cards(t.snap())), ["a", "b"])
        self.env._procs["a"].kill(); self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) == 1, 6), t.text()); self.assertIn(" b ", self.card(t, "b").line(1) + " ")
        (self.env.claude / "sessions" / f"{self.env._procs['b'].pid}.json").unlink(); self.assertTrue(wait(lambda: "0 sessions" in t.text(), 6), t.text())

    def test_no_register_directory_at_all_is_an_empty_board_not_an_error(self):
        import shutil; shutil.rmtree(self.env.claude); r = self.cli("show"); self.assertEqual((r.returncode, r.stdout, r.stderr), (0, "", ""))


class Status(Base):
    def test_the_register_statuses(self):
        for i, st in enumerate(("busy", "waiting", "idle", "shell", "frobnicating")): self.env.reg(f"s{i}", status=st, name=f"n{i}", nameSource="user")
        rows = self.show(); self.assertEqual([rows[f"s{i}"][3] for i in range(5)], ["working", "waiting", "idle", "idle", "idle"], "busy / waiting / idle; shell (a background shell runs, the session does not) and what a later version adds are idle")

    def test_waiting_with_a_permission_prompt_and_with_input_needed_both_show_as_waiting_cards(self):
        self.env.reg("p", status="waiting", waitingFor="permission prompt", name="perm", nameSource="user"); self.env.reg("q", status="waiting", waitingFor="input needed", name="ask", nameSource="user")
        self.env.reg("r", status="waiting", name="plain", nameSource="user"); t = self.board(w=200, n=3)
        for n in ("perm", "ask", "plain"): self.assertEqual(self.word(t, n), "⏳ waiting", n)

    def test_a_plan_waiting_for_approval_is_a_waiting_card(self):
        self.env.reg("pl", status="waiting", waitingFor="input needed", name="planner", nameSource="user")
        self.log("pl", [{"type": "assistant", "message": {"content": [{"type": "tool_use", "id": "x1", "name": "ExitPlanMode", "input": {}}]}}]); t = self.board(n=1); self.assertEqual(self.word(t, "planner"), "⏳ waiting")


class Names(Base):
    TITLE = {"type": "custom-title", "customTitle": "from-the-title"}

    def pane(self, sid, title, session="sess"):
        self.env.reg(sid, tmux=f"{session}:@1.%7{ord(sid[-1])}", **self.kw); self.panes(f"%7{ord(sid[-1])}\t{session}\t0\tclaude\t{title}\t1")

    def test_the_order_user_name_title_pane_summary_derived_name_project(self):
        home = self.env.home
        # 1 the name a user gave beats everything; 2 a title beats the pane; 3 the pane's summary beats the name Claude made up; 4 that beats the project; 5 the project is the directory's name
        self.env.reg("c1", name="given", nameSource="user", tmux="s:@1.%71", cwd=str(home / "d1")); self.log("c1", [self.TITLE], str(home / "d1"))
        self.env.reg("c2", name="peer-given", nameSource="peer", cwd=str(home / "d2")); self.log("c2", [self.TITLE], str(home / "d2"))
        self.env.reg("c3", name="slug-name", nameSource="derived", tmux="s:@1.%73", cwd=str(home / "d3")); self.log("c3", [self.TITLE], str(home / "d3"))
        self.env.reg("c4", name="slug-four", nameSource="derived", tmux="s:@1.%74", cwd=str(home / "d4"))
        self.env.reg("c5", name="slug-five", nameSource="derived", cwd=str(home / "d5")); self.env.reg("c6", cwd=str(home / "projname"))
        self.panes("%71\ts\t0\tclaude\t✳ pane one\t1", "%73\ts\t0\tclaude\t✳ pane three\t1", "%74\ts\t0\tclaude\t⠋ Pane four summary\t1")
        rows = self.show(); self.assertEqual({k: v[1] for k, v in rows.items()}, {"c1": "given", "c2": "peer-given", "c3": "from-the-title", "c4": "Pane four summary", "c5": "slug-five", "c6": "projname"})

    def test_the_marks_in_front_of_the_pane_title_and_claude_code_are_not_a_summary(self):
        cases = [("✳ idle summary", "idle summary"), ("⠂ working summary", "working summary"), ("⠐ ⠂ two marks", "two marks"), ("   padded   ", "padded"), ("Claude Code", "slug"), ("✳ Claude Code", "slug"), ("", "slug"), ("✳", "slug"), ("no mark", "no mark"), ("名字 title", "名字 title")]
        for i, (title, want) in enumerate(cases):
            with self.subTest(title=title):
                self.env.reg(f"t{i}", name="slug", nameSource="derived", tmux=f"s{i}:@1.%8{i}", cwd=str(self.env.home / f"dd{i}")); self.panes(f"%8{i}\ts{i}\t0\tclaude\t{title}\t1"); self.assertEqual(self.show()[f"t{i}"][1], want); (self.env.claude / "sessions" / f"{self.env.proc(f't{i}')[0]}.json").unlink()

    def test_a_pane_that_is_not_listed_or_a_session_without_a_pane_has_no_summary(self):
        self.env.reg("x1", name="made-up", nameSource="derived", tmux="s:@1.%90", cwd=str(self.env.home / "x1")); self.panes("%91\ts\t0\tclaude\t✳ somebody else's pane\t1"); self.env.reg("x2", name="no-pane-up", nameSource="derived", cwd=str(self.env.home / "x2"))
        rows = self.show(); self.assertEqual((rows["x1"][1], rows["x2"][1]), ("made-up", "no-pane-up"))

    def test_the_title_is_the_last_custom_title_of_the_transcript(self):
        self.env.reg("tt", name="slug", nameSource="derived"); self.log("tt", [{"type": "custom-title", "customTitle": "first"}, {"type": "user"}, {"type": "custom-title", "customTitle": "second"}, {"type": "user"}]); self.assertEqual(self.name_of("tt"), "second")

    def test_a_rename_by_the_user_shows_at_the_next_poll(self):
        self.env.reg("rn", name="before", nameSource="user", cwd=str(self.env.home / "rn")); t = self.board(w=100, n=1); self.assertIn("before", self.card(t, "before").line(1))
        self.env.reg("rn", name="after", nameSource="user", cwd=str(self.env.home / "rn")); self.assertTrue(wait(lambda: any("after" in c.line(1) for c in find_cards(t.snap())), 6), t.text())

    def test_the_project_is_the_main_repo_of_a_worktree_and_the_directory_name_outside_git(self):
        main = self.env.home / "work" / "mainrepo"; self.env.git_repo(main); wt = self.env.home / "work" / "mainrepo.wt"; self.env._git("worktree", "add", "-q", "--detach", str(wt), cwd=str(main)); plain = self.env.home / "work" / "plainthing"; plain.mkdir(parents=True)
        self.env.reg("m1", cwd=str(main)); self.env.reg("m2", cwd=str(wt)); self.env.reg("m3", cwd=str(plain)); self.assertEqual({k: v[1] for k, v in self.show().items()}, {"m1": "mainrepo", "m2": "mainrepo", "m3": "plainthing"})
        t = self.board(w=100, text='[layout]\nmax_columns = 1\ngroup_by = "project"\n', n=3); titles = [re.match(r"▾ (.+?) \((\d+)\)", r.text.strip()).groups() for r in t.snap() if re.match(r"▾ (.+?) \((\d+)\)", r.text.strip())]
        self.assertEqual(sorted(titles), [("mainrepo", "2"), ("plainthing", "1")], "the worktree is in its main repo's project")


class Transcript(Base):
    def test_branch_pending_tool_and_model_are_read_off_the_transcript_at_the_default_path(self):
        self.env.reg("tr", name="tr", nameSource="user", status="busy")
        self.log("tr", [{"type": "assistant", "message": {"model": "claude-opus-5-5", "content": []}}, {"type": "user", "gitBranch": "feature/one", "message": {"content": "x"}}, {"type": "user", "gitBranch": "feature/two", "message": {"content": "x"}},
                        {"type": "assistant", "message": {"model": "claude-opus-5-5", "content": [{"type": "tool_use", "id": "t1", "name": "Edit", "input": {}}]}}])
        t = self.board(w=100, n=1); c = self.card(t, "tr"); self.assertIn("feature/two", c.line(2), "the last branch of the tail"); self.assertIn("working · Edit", c.line(3)); self.assertIn("Opus", c.line(3))

    def test_a_tool_that_has_its_result_is_not_pending_and_an_idle_session_has_none(self):
        self.env.reg("pd", name="paired", nameSource="user", status="busy"); self.env.reg("id", name="idler", nameSource="user", status="idle")
        self.log("pd", [{"type": "assistant", "message": {"content": [{"type": "tool_use", "id": "t1", "name": "Bash", "input": {}}]}}, {"type": "user", "message": {"content": [{"type": "tool_result", "tool_use_id": "t1", "content": "ok"}]}}])
        self.log("id", [{"type": "assistant", "message": {"content": [{"type": "tool_use", "id": "t2", "name": "Bash", "input": {}}]}}]); t = self.board(w=100, n=2)
        self.assertNotIn("Bash", self.card(t, "paired").line(3)); self.assertNotIn("Bash", self.card(t, "idler").line(3))

    def test_the_statusline_payload_names_the_transcript_before_the_default_path(self):
        self.env.reg("sl", name="sl", nameSource="user", status="busy"); other = self.env.tmp / "elsewhere.jsonl"
        other.write_text(json.dumps({"type": "user", "gitBranch": "from-the-hud"}) + "\n"); self.log("sl", [{"type": "user", "gitBranch": "from-the-default"}])
        self.env.hud("sl", json.dumps({"transcript_path": str(other), "model": {"display_name": "Hud Model 1"}, "context_window": {"used_percentage": 12}})); t = self.board(w=100, n=1); c = self.card(t, "sl")
        self.assertIn("from-the-hud", c.line(2)); self.assertIn("Hud Model 1", c.line(3))

    def test_a_statusline_path_that_is_not_there_falls_back_to_the_default(self):
        self.env.reg("sf", name="sf", nameSource="user", status="busy"); self.log("sf", [{"type": "user", "gitBranch": "from-the-default"}]); self.env.hud("sf", json.dumps({"transcript_path": str(self.env.tmp / "missing.jsonl")})); t = self.board(w=100, n=1); self.assertIn("from-the-default", self.card(t, "sf").line(2))

    def test_head_is_no_branch_and_git_is_asked(self):
        repo = self.env.home / "work" / "gitrepo"; self.env.git_repo(repo); self.env._git("checkout", "-q", "-b", "from-git", cwd=str(repo)); self.env.reg("hd", name="hd", nameSource="user", cwd=str(repo))
        self.log("hd", [{"type": "user", "gitBranch": "HEAD"}], str(repo)); t = self.board(w=100, n=1); self.assertIn("from-git", self.card(t, "hd").line(2))


class Background(Base):
    def job(self, jid, **st): d = self.env.claude / "jobs" / jid; d.mkdir(parents=True, exist_ok=True); (d / "state.json").write_text(json.dumps(st))

    def test_a_background_session_is_its_job_id_and_says_bg(self):
        self.env.reg("sess-uuid-1", kind="bg", jobId="job12345", name="regname", nameSource="peer"); rows = self.show(); self.assertEqual(list(rows), ["job12345"]); self.assertEqual((rows["job12345"][1], rows["job12345"][7]), ("regname", "bg"))

    def test_the_job_states(self):
        cases = [("a", dict(state="running", tempo="active"), "working"), ("b", dict(state="running", tempo="blocked", questions=[{"q": 1}]), "waiting"), ("c", dict(state="running", tempo="blocked", needs=["perm"]), "waiting"), ("d", dict(state="done", tempo="idle"), "idle"),
                 ("e", dict(state="stopped"), "idle"), ("f", dict(state="failed"), "idle"), ("g", dict(state="whatever", tempo="whatever"), "working"), ("h", dict(), "working")]
        for jid, st, want in cases:
            self.env.reg(f"u-{jid}", kind="bg", jobId=jid, status="idle", name=f"n-{jid}"); self.job(jid, **st)
        rows = self.show(); self.assertEqual({k: rows[k][3] for k, _, _ in cases}, {k: w for k, _, w in cases})

    def test_a_job_without_a_state_file_has_the_status_of_the_register(self):
        self.env.reg("u-1", kind="bg", jobId="j1", status="busy", name="bgone"); self.env.reg("u-2", kind="bg", jobId="j2", status="idle", name="bgtwo"); rows = self.show(); self.assertEqual((rows["j1"][3], rows["j2"][3]), ("working", "idle"))

    def test_the_job_name_beats_the_registers_and_its_transcript_is_the_link_scan_path(self):
        self.env.reg("u-n", kind="bg", jobId="jn", name="registers-name", nameSource="peer"); tr = self.env.tmp / "job.jsonl"; tr.write_text(json.dumps({"type": "custom-title", "customTitle": "ignored-title"}) + "\n" + json.dumps({"type": "user", "gitBranch": "job-branch"}) + "\n")
        self.job("jn", state="running", name="job-name", linkScanPath=str(tr)); t = self.board(w=100, n=1); c = self.card(t, "job-name"); self.assertIn("job-branch", c.line(2), "the transcript of the job"); self.assertEqual(self.show()["jn"][1], "job-name")

    def test_a_background_session_has_no_pane_until_it_is_attached(self):
        self.env.reg("u-p", kind="bg", jobId="jp", name="bgpane"); t = self.board(w=100, n=1); self.assertIn("background session · enter or i attaches it", "\n".join(r.text for r in t.snap()[-3:]))


class Done(Base):
    def go(self, sid, status, ms=None, **kw):
        """rewrite the register of an existing session (same process): its status and, when asked, the time of it"""
        d = json.loads((self.env.claude / "sessions" / f"{self.env.proc(sid)[0]}.json").read_text()); d.update(status=status, statusUpdatedAt=ms or int(time.time() * 1000), updatedAt=int(time.time() * 1000), **kw)
        f = self.env.claude / "sessions" / f"{d['pid']}.json"; tmp = f.with_suffix(".tmp"); tmp.write_text(json.dumps(d)); os.replace(tmp, f)

    def start(self, **sessions):
        for sid, st in sessions.items(): self.env.reg(sid, status=st, name=sid, nameSource="user", tmux=f"sess:@1.%6{ord(sid[-1])}", cwd=str(self.env.home / "work" / sid))
        return self.board(w=100, n=len(sessions))

    def test_a_session_that_is_idle_when_the_board_starts_is_not_done(self):
        t = self.start(aaa="idle", bbb="busy"); time.sleep(2.5); self.assertEqual((self.word(t, "aaa"), self.word(t, "bbb")), ("⏸ idle", "⏵ working"))

    def test_working_then_idle_is_done_and_m_marks_it_and_the_next_working_then_idle_is_done_again(self):
        t = self.start(aaa="busy"); self.assertEqual(self.word(t, "aaa"), "⏵ working"); self.go("aaa", "idle"); self.assertTrue(wait(lambda: self.word(t, "aaa") == "● done", 6), self.word(t, "aaa"))
        t.keys("m"); self.assertTrue(wait(lambda: self.word(t, "aaa") == "⏸ idle", 3)); time.sleep(2.5); self.assertEqual(self.word(t, "aaa"), "⏸ idle", "marked: it stays idle")
        seen = json.loads((self.env.state / "agmux" / "seen.json").read_text()); self.assertEqual(list(seen), ["aaa"])
        self.go("aaa", "busy"); self.assertTrue(wait(lambda: self.word(t, "aaa") == "⏵ working", 6)); time.sleep(0.2); self.go("aaa", "idle"); self.assertTrue(wait(lambda: self.word(t, "aaa") == "● done", 6), "a new answer is a new done")

    def test_waiting_then_idle_is_done_too(self):
        t = self.start(aaa="waiting"); self.go("aaa", "idle"); self.assertTrue(wait(lambda: self.word(t, "aaa") == "● done", 6), self.word(t, "aaa"))

    def test_a_status_time_that_moves_while_idle_is_a_done_a_short_answer_between_two_polls(self):
        t = self.start(aaa="idle"); time.sleep(2.5); self.assertEqual(self.word(t, "aaa"), "⏸ idle"); self.go("aaa", "idle"); self.assertTrue(wait(lambda: self.word(t, "aaa") == "● done", 6), "its status changed since the board first saw it")

    def test_a_restarted_board_has_every_idle_session_idle_and_the_seen_stay_seen(self):
        t = self.start(aaa="busy"); self.go("aaa", "idle"); self.assertTrue(wait(lambda: self.word(t, "aaa") == "● done", 6)); t.close()
        t2 = self.board(w=100, n=1); time.sleep(2.5); self.assertEqual(self.word(t2, "aaa"), "⏸ idle", "the old done is idle for a board that never saw it work")

    def test_a_session_that_appears_idle_is_not_done_and_one_that_appears_busy_and_ends_is(self):
        t = self.start(aaa="busy"); self.env.reg("late", status="idle", name="newone", nameSource="user", tmux="sess:@1.%699", cwd=str(self.env.home / "work" / "late")); self.env.reg("lateb", status="busy", name="other", nameSource="user", tmux="sess:@1.%698", cwd=str(self.env.home / "work" / "lateb"))
        self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) == 3, 6)); time.sleep(0.5); self.assertEqual(self.word(t, "newone"), "⏸ idle"); self.go("lateb", "idle"); self.assertTrue(wait(lambda: self.word(t, "other") == "● done", 6))

    def test_the_same_id_under_another_process_is_a_new_session_idle_and_not_done(self):
        t = self.start(aaa="busy"); self.go("aaa", "idle"); self.assertTrue(wait(lambda: self.word(t, "aaa") == "● done", 6))
        old = self.env._procs.pop("aaa"); (self.env.claude / "sessions" / f"{old.pid}.json").unlink(); old.kill(); old.wait(); self.env._written.pop("aaa", None)
        self.env.reg("aaa", status="idle", name="aaa", nameSource="user", tmux="sess:@1.%6a", cwd=str(self.env.home / "work" / "aaa")); time.sleep(0.5); self.assertTrue(wait(lambda: self.word(t, "aaa") == "⏸ idle", 6), self.word(t, "aaa"))

    def test_a_background_session_is_done_when_its_job_finishes(self):
        self.env.reg("u-1", kind="bg", jobId="j1", name="bgjob"); d = self.env.claude / "jobs" / "j1"; d.mkdir(parents=True); (d / "state.json").write_text(json.dumps(dict(state="running", tempo="active"))); t = self.board(w=100, n=1)
        self.assertEqual(self.word(t, "bgjob"), "⏵ working"); self.go("u-1", "idle"); (d / "state.json").write_text(json.dumps(dict(state="done", tempo="idle"))); self.assertTrue(wait(lambda: self.word(t, "bgjob") == "● done", 6), self.word(t, "bgjob"))


class Agent(Base):
    def role(self, sid, text):
        d = self.env.state / "claude-agent"; d.mkdir(exist_ok=True); (d / sid).write_text(text)

    def test_the_role_file_comes_first_dash_or_missing_falls_back_to_the_transcripts_setting(self):
        for sid in ("r1", "r2", "r3", "r4", "r5"): self.env.reg(sid, name=sid, nameSource="user"); self.log(sid, [{"type": "agent-setting", "agentSetting": "from-transcript"}])
        self.role("r1", "from-the-file"); self.role("r2", "-"); self.role("r4", "  padded-role \n"); self.role("r5", "")
        rows = self.show(); self.assertEqual([rows[s][6] for s in ("r1", "r2", "r3", "r4", "r5")], ["from-the-file", "from-transcript", "from-transcript", "padded-role", "from-transcript"])

    def test_the_role_without_any_transcript_and_the_state_dir_follows_xdg(self):
        self.env.reg("nr", name="nr", nameSource="user"); self.role("nr", "storefront-qa"); self.assertEqual(self.show()["nr"][6], "storefront-qa")
        other = self.env.tmp / "otherstate"; (other / "claude-agent").mkdir(parents=True); (other / "claude-agent" / "nr").write_text("elsewhere-role\n")
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(other), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        out = subprocess.run([BIN, "show"], env=env, capture_output=True, text=True, timeout=20).stdout; self.assertEqual(out.split("\t")[6], "elsewhere-role")

    def test_a_role_on_a_card_shows_at_the_right_edge(self):
        self.env.reg("cr", name="cr", nameSource="user"); self.role("cr", "reviewer"); t = self.board(w=100, n=1); self.assertRegex(self.card(t, "cr").line(2), r"reviewer[│┃]$")


class Footer(Base):
    def foot(self, t): return last_text_row(t.snap())

    def test_pin_and_read_follow_avatar_in_the_footer(self):
        self.env.reg("f1", name="f1", nameSource="user", tmux="sess:@1.%61", cwd=str(self.env.home / "f1")); t = self.board(w=240, n=1); bar = self.foot(t)
        self.assertIn("a avatar · t pin · m read", bar, bar); self.assertLess(bar.index("a avatar"), bar.index("t pin")); self.assertLess(bar.index("t pin"), bar.index("m read")); self.assertLess(bar.index("m read"), bar.index("q quit"))

    def test_the_keys_come_from_the_config_and_an_action_without_a_key_is_not_listed(self):
        self.env.reg("f2", name="f2", nameSource="user", tmux="sess:@1.%62", cwd=str(self.env.home / "f2")); t = self.board(w=240, n=1, text='[keys]\nmark_read = ["q"]\npin = ["T"]\nquit = ["esc"]\n'); bar = self.foot(t); self.assertIn("T pin", bar); self.assertIn("q read", bar); self.assertNotIn("t pin", bar); self.assertNotIn("m read", bar)

    def test_they_are_listed_for_a_card_without_a_pane_too_and_a_narrow_footer_is_cut_not_broken(self):
        self.env.reg("f3", name="f3", nameSource="user", cwd=str(self.env.home / "f3")); t = self.board(w=240, n=1); bar = self.foot(t); self.assertIn("t pin", bar); self.assertIn("m read", bar)
        t2 = self.board(w=100, n=1); self.assertTrue(t2.alive()); self.assertLessEqual(len(self.foot(t2)), 100); self.assertIn("j/k h/l move", self.foot(t2))


class Panes(Base):
    def test_the_tmux_session_is_the_first_word_before_the_hash_and_tmux_says_where_the_pane_is_now(self):
        home = self.env.home; self.env.reg("p1", name="p1", nameSource="user", tmux="alpha # main:@14.%37", cwd=str(home / "d1")); self.env.reg("p2", name="p2", nameSource="user", tmux="x:y # w:@3.%38", cwd=str(home / "d2"))
        self.env.reg("p3", name="p3", nameSource="user", tmux="olds # w:@3.%39", cwd=str(home / "d3")); self.env.reg("p4", name="p4", nameSource="user", cwd=str(home / "d4"))
        self.panes("%39\tmoved # elsewhere\t0\tclaude\tt\t1"); t = self.board(w=80, h=70, text='[layout]\nmax_columns = 1\ngroup_by = "session"\n', n=4)
        titles = sorted(re.match(r"▾ (.+?) \(\d+\)", r.text.strip()).group(1) for r in t.snap() if re.match(r"▾ (.+?) \(\d+\)", r.text.strip()))
        self.assertEqual(titles, sorted(["alpha", "x:y", "moved", "(no pane)"]), "alpha # main -> alpha; a session named x:y # w -> x:y; the register's `olds` is what the pane was in when the process started: tmux says `moved`; no tmux field -> no pane")

    def test_a_card_with_a_pane_can_be_jumped_to_and_one_without_says_so(self):
        self.env.reg("j1", name="j1", nameSource="user", tmux="alpha # main:@14.%37", cwd=str(self.env.home / "d1")); self.env.reg("j2", name="j2", nameSource="user", cwd=str(self.env.home / "d2")); t = self.board(w=100, n=2)
        t.keys("l"); time.sleep(0.4)
        self.assertTrue(wait(lambda: "no tmux pane: can't jump or send" in "\n".join(r.text for r in t.snap()[-3:]), 3), t.text()); t.keys("h"); time.sleep(0.4); t.keys("Enter"); self.assertEqual(t.exit_status(5), 0)
        self.assertIn("%37", [c[-1] for c in self.env.tmux_calls() if c[0] == "switch-client"])


if __name__ == "__main__":
    unittest.main()
