"""T69 r / x / d without ccmux. r: a foreground session's process gets SIGTERM (SIGKILL when it is still there after 5 s) and once it has gone `claude --resume <session id>` is typed into its pane (tmux send-keys -l, then
Enter); a background Claude session is `claude respawn <job id>`; a pi is `pi --session <transcript>` (test_t68); a session that is not running (r pressed twice: the second would type into the session the first started) or
has no pane or no session id is an error line and nothing is signalled or typed. x: a foreground process SIGTERM (SIGKILL after 2 s), a background session `claude stop <job id>` ("No job matching" in its output: the job is gone
already: not an error). d: `hunk diff --watch` at the root of the git repo of the card's cwd, with the terminal; without hunk `git diff` through `less -R`; no repo: a line saying so. Spec: specs/m3-config/tasks.md「第十四批」T69.
Fake tmux (the harness' one: its calls are the typing), fake `claude` / `hunk` on PATH (they log into calls.log), the sessions' processes are the harness' `sleep`s (or a shell that ignores SIGTERM)."""
import json, os, re, signal, subprocess, threading, time, unittest
from lib import *
from test_t3 import last_text_row


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def board(self, w=100, h=50, n=1):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) >= n, 10), t.text()); time.sleep(0.4); return t

    def msg(self, t): return "\n".join(r.text for r in t.snap()[-3:])

    def sent(self): return [c for c in self.env.tmux_calls() if c[0] == "send-keys"]

    def fg(self, sid, **kw): return self.env.reg(sid, name=sid, nameSource="user", tmux=f"sess:@1.%5{ord(sid[0])}", cwd=str(self.env.home / "work" / sid), **kw)

    def alive(self, sid): return self.env._procs[sid].poll() is None

    def spawn(self, script):
        """a process of its own (not a bare sleep), reaped when it dies: a zombie answers kill(pid, 0); returns (process, the start time ps says)"""
        p = subprocess.Popen(["sh", "-c", script], start_new_session=True); threading.Thread(target=p.wait, daemon=True).start(); self.addCleanup(lambda: p.kill())
        time.sleep(0.3); return p, " ".join(subprocess.run(["ps", "-o", "lstart=", "-p", str(p.pid)], capture_output=True, text=True, env={**os.environ, "LC_ALL": "C", "TZ": "UTC"}).stdout.split())


class Restart(Base):
    def test_r_ends_the_process_and_then_types_the_resume_command_and_enter(self):
        self.fg("aaa"); t = self.board(); t0 = time.time(); t.keys("r"); self.assertTrue(wait(lambda: ["send-keys", "-t", "%597", "Enter"] in self.sent(), 8), self.sent())
        self.assertEqual(self.sent(), [["send-keys", "-t", "%597", "-l", "--", "claude --resume aaa"], ["send-keys", "-t", "%597", "Enter"]]); self.assertFalse(self.alive("aaa")); self.assertEqual(self.env.calls(), [], "no ccmux, no claude command")
        self.assertLess(time.time() - t0, 5, "SIGTERM is enough for a process that exits")

    def test_the_command_is_typed_after_the_process_has_gone_not_before(self):
        # the process ends only at its own pace (0.8 s after SIGTERM): the typing has to wait for that
        p, ps = self.spawn("trap 'sleep 0.8; exit 0' TERM; while :; do sleep 0.1; done")
        self.env.reg("slow", pid=p.pid, procstart=ps, name="slow", nameSource="user", tmux="sess:@1.%5s", cwd=str(self.env.home / "work" / "slow")); t = self.board(); t.keys("r"); self.assertTrue(wait(lambda: self.sent(), 8))
        self.assertIsNotNone(p.poll(), "gone by the time the first key is typed"); self.assertEqual(self.sent()[0][-1], "claude --resume slow")

    def test_a_process_that_ignores_sigterm_is_killed_after_five_seconds(self):
        p, ps = self.spawn("trap '' TERM; while :; do sleep 0.2; done")
        self.env.reg("stub", pid=p.pid, procstart=ps, name="stub", nameSource="user", tmux="sess:@1.%5s", cwd=str(self.env.home / "work" / "stub")); t = self.board(); t0 = time.time(); t.keys("r")
        self.assertTrue(wait(lambda: self.sent(), 12), self.msg(t)); dt = time.time() - t0; self.assertTrue(4.5 < dt < 9.5, f"SIGTERM, 5 s, SIGKILL, then the typing: {dt:.1f} s"); self.assertIsNotNone(p.poll())

    def test_r_pressed_twice_types_once_and_says_the_second_found_nothing_running(self):
        self.fg("aaa"); t = self.board(); t.keys("r"); t.keys("r"); self.assertTrue(wait(lambda: ["send-keys", "-t", "%597", "Enter"] in self.sent(), 8)); time.sleep(1.0)
        self.assertEqual(len([c for c in self.sent() if "-l" in c]), 1, "the command is typed once"); self.assertEqual(len([c for c in self.sent() if c[-1] == "Enter"]), 1)

    def test_a_session_without_a_pane_is_an_error_and_is_not_signalled(self):
        self.env.reg("nop", name="nopane", nameSource="user", cwd=str(self.env.home / "work" / "nop")); t = self.board(); t.keys("r"); self.assertTrue(wait(lambda: "has no tmux pane to start it again in" in self.msg(t), 4), self.msg(t)); self.assertTrue(self.alive("nop")); self.assertEqual(self.sent(), [])

    def test_a_failing_tmux_is_the_first_line_of_what_it_said_and_enter_is_not_sent(self):
        self.fg("aaa"); (self.env.fake / "tmux.fail").write_text("send-keys"); t = self.board(); t.keys("r"); self.assertTrue(wait(lambda: "boom from fake tmux" in self.msg(t), 8), self.msg(t)); self.assertNotIn("second tmux line", t.text())
        self.assertEqual(len(self.sent()), 1, "the typing failed: no Enter after it"); self.assertTrue(t.alive())

    def test_a_background_session_is_restarted_by_the_daemon(self):
        self.env.reg("u-1", kind="bg", jobId="job77", name="bgjob", cwd=str(self.env.home / "work" / "bg")); t = self.board(); t.keys("r"); self.assertTrue(wait(lambda: self.env.calls() == [["respawn", "job77"]], 5), self.env.calls()); self.assertTrue(self.alive("u-1")); self.assertEqual(self.sent(), [])

    def test_a_failing_respawn_shows_the_first_line(self):
        self.env.reg("u-1", kind="bg", jobId="job77", name="bgjob", cwd=str(self.env.home / "work" / "bg")); (self.env.fake / "fail").write_text(""); t = self.board(); t.keys("r"); self.assertTrue(wait(lambda: "boom from fake claude" in self.msg(t), 5), self.msg(t)); self.assertNotIn("second stderr line", t.text())


class Kill(Base):
    def kill(self, t): t.keys("x"); time.sleep(0.3); t.keys("y")

    def test_x_ends_the_process_and_touches_nothing_else_and_the_card_goes(self):
        self.fg("aaa"); self.fg("bbb"); t = self.board(w=200, n=2); self.kill(t); self.assertTrue(wait(lambda: not self.alive("aaa"), 5)); self.assertTrue(self.alive("bbb")); self.assertEqual((self.env.calls(), self.sent()), ([], []))
        self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) == 1, 6), t.text())

    def test_a_process_that_ignores_sigterm_is_killed_after_two_seconds(self):
        p, ps = self.spawn("trap '' TERM; while :; do sleep 0.2; done")
        self.env.reg("stub", pid=p.pid, procstart=ps, name="stub", nameSource="user", tmux="sess:@1.%5s", cwd=str(self.env.home / "work" / "stub")); t = self.board(); t0 = time.time(); self.kill(t); self.assertTrue(wait(lambda: p.poll() is not None, 8)); dt = time.time() - t0
        self.assertTrue(1.5 < dt < 6, f"SIGTERM, 2 s, SIGKILL: {dt:.1f} s")

    def test_x_on_a_background_session_is_claude_stop_with_the_job_id(self):
        self.env.reg("u-1", kind="bg", jobId="job77", name="bgjob", cwd=str(self.env.home / "work" / "bg")); t = self.board(); self.kill(t); self.assertTrue(wait(lambda: self.env.calls() == [["stop", "job77"]], 5), self.env.calls()); self.assertTrue(self.alive("u-1"), "the daemon ends it, not a signal")

    def test_no_job_matching_is_a_job_that_is_gone_already_not_an_error(self):
        self.env.reg("u-1", kind="bg", jobId="job77", name="bgjob", cwd=str(self.env.home / "work" / "bg")); (self.env.fake / "nojob").write_text(""); t = self.board(); self.kill(t); self.assertTrue(wait(lambda: self.env.calls() == [["stop", "job77"]], 5)); time.sleep(0.8)
        self.assertNotIn("No job matching", self.msg(t)); self.assertNotIn("boom", self.msg(t))

    def test_a_failing_stop_shows_the_first_line(self):
        self.env.reg("u-1", kind="bg", jobId="job77", name="bgjob", cwd=str(self.env.home / "work" / "bg")); (self.env.fake / "fail").write_text(""); t = self.board(); self.kill(t); self.assertTrue(wait(lambda: "boom from fake claude" in self.msg(t), 5), self.msg(t))

    def test_x_asks_first_and_only_y_confirms(self):
        self.fg("aaa"); t = self.board(); t.keys("x"); self.assertTrue(wait(lambda: "y to confirm" in self.msg(t), 3)); t.keys("n"); time.sleep(0.8); self.assertTrue(self.alive("aaa"))


class Review(Base):
    def test_d_runs_hunk_at_the_root_of_the_repo_of_the_cards_directory(self):
        repo = self.env.git_repo(self.env.home / "work" / "repo"); sub = repo / "deep" / "er"; sub.mkdir(parents=True); self.env.reg("rv", name="reviewee", nameSource="user", tmux="sess:@1.%5r", cwd=str(sub)); t = self.board(); t.keys("d")
        self.assertTrue(wait(lambda: self.env.calls() == [["hunk", "diff", "--watch"]], 6), self.env.calls()); self.assertEqual(os.path.realpath((self.env.fake / "hunk.cwd").read_text().strip()), os.path.realpath(repo), "the root, not the card's own directory")
        self.assertTrue(wait(lambda: "review finished" in self.msg(t) or "REVIEW-DONE" in t.text(), 6), t.text())

    def test_hunk_gets_the_terminal_the_board_waits_and_comes_back(self):
        repo = self.env.git_repo(self.env.home / "work" / "repo"); self.env.reg("rv", name="reviewee", nameSource="user", tmux="sess:@1.%5r", cwd=str(repo)); (self.env.fake / "review.hold").write_text(""); t = self.board(); t.keys("d")
        self.assertTrue(wait(lambda: "REVIEW-RUNNING" in t.text(), 4), t.text()); self.assertNotIn(" sessions", t.text().split("\n")[0], "the board is suspended"); self.assertTrue(wait(lambda: " sessions" in t.text(), 6), t.text())

    def test_a_directory_that_is_not_in_a_repo_says_so_and_runs_nothing(self):
        d = self.env.home / "work" / "plain"; d.mkdir(parents=True); self.env.reg("pl", name="plain", nameSource="user", tmux="sess:@1.%5p", cwd=str(d)); t = self.board(); t.keys("d"); self.assertTrue(wait(lambda: "plain: " in self.msg(t) and "is not" in self.msg(t), 5), self.msg(t))  # (the line is cut: the temp path is long); self.assertEqual(self.env.calls(), [])

    def test_a_failing_hunk_shows_the_last_stderr_line(self):
        repo = self.env.git_repo(self.env.home / "work" / "repo"); self.env.reg("rv", name="reviewee", nameSource="user", tmux="sess:@1.%5r", cwd=str(repo)); (self.env.fake / "review.fail").write_text(""); t = self.board(); t.keys("d")
        self.assertTrue(wait(lambda: "review exploded: the last word" in self.msg(t), 6), self.msg(t)); self.assertNotIn("first stderr line", self.msg(t)); self.assertTrue(t.alive())

    def test_without_hunk_git_diff_goes_through_less_and_q_comes_back(self):
        repo = self.env.git_repo(self.env.home / "work" / "repo"); (repo / "changed.txt").write_text("one line\n"); self.env._git("add", "changed.txt", cwd=str(repo)); self.env._git("commit", "-q", "-m", "c", cwd=str(repo)); (repo / "changed.txt").write_text("one line\nthe NEW line\n")
        (self.env.bin / "hunk").unlink(); self.env.reg("rv", name="reviewee", nameSource="user", tmux="sess:@1.%5r", cwd=str(repo)); t = self.board(); t.keys("d")
        self.assertTrue(wait(lambda: "the NEW line" in t.text(), 6), t.text()); self.assertIn("+the NEW line", t.text()); self.assertNotIn(" sessions", t.text().split("\n")[0], "less has the terminal"); t.keys("q")
        self.assertTrue(wait(lambda: " sessions" in t.text(), 6), t.text()); self.assertEqual(self.env.calls(), [])

    def test_a_session_without_a_directory_has_nothing_to_review(self):
        self.env.reg("nd", name="nodir", nameSource="user", tmux="sess:@1.%5n", cwd=None); t = self.board(); t.keys("d"); time.sleep(1.0); self.assertEqual(self.env.calls(), []); self.assertTrue(t.alive())


if __name__ == "__main__":
    unittest.main()
