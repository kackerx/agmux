"""T4 interaction + store polling. Spec: specs/m1-mvp/design.md「交互」, specs/m1-mvp/tasks.md T4 (x: y runs / any other key cancels; the old `s` prompt is gone since T14, see test_t10).

The fake `ccmux` on PATH records argv of every non-`show` call, so each key is checked end-to-end as the exact command line.
Focus order since T9 is spatial (see test_t9); at 200 columns: band 1 = G1 a1,a2,a3 | G2 b1,b2(background, no pane) | G3 c1,c2 | G4 d1, band 2 = G5 e1.
"""
import fcntl, json, os, re, pty, struct, subprocess, termios, unittest
from lib import *
from test_t3 import ready, by_name, NAMES, last_text_row


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.t = Term(self.env, w=200, h=50); self.addCleanup(self.t.close)
        self.cards = ready(self.t); self.assertTrue(self.cards, self.t.text())

    def calls(self, n=None, timeout=3):
        """Wait for n recorded ccmux calls (or return what is there after a quiet 0.8s when n is None)."""
        if n is None:
            time.sleep(0.8); return self.env.actions()
        wait(lambda: len(self.env.actions()) >= n, timeout)
        return self.env.actions()

    def footer(self):
        return last_text_row(self.t.snap())

    def focus_probe(self):
        """Which session has focus = what `enter` would switch to. Destructive (exits the TUI)."""
        self.t.keys("Enter"); c = self.calls(1); return c[0] if c else None


class Quit(Base):
    def test_q(self):
        self.t.keys("q"); self.assertEqual(self.t.exit_status(), 0)

    def test_esc(self):
        self.t.esc(); self.assertEqual(self.t.exit_status(), 0)


class Keys(Base):
    def test_enter_switches_then_exits(self):
        self.t.keys("Enter")
        self.assertEqual(self.calls(1), [["switch", "a1"]])
        self.assertEqual(self.t.exit_status(), 0, "enter must exit the TUI after ccmux switch")

    def test_navigation(self):
        cases = [("j", "a2"), ("j j", "a3"), ("j j j j j", "e1"), ("k", "a1"), ("l", "b1"), ("l l", "c1"), ("l l l", "d1"), ("l l l l", "d1"),
                 ("l l l l l", "d1"), ("j j l l", "c2"), ("j j j l", "e1"), ("j l", "b2"), ("Down", "a2"), ("Right", "b1"), ("Right Left", "a1"),
                 ("j Up", "a1"), ("l h", "a1"), ("h", "a1"), ("l l l h h", "b1")]
        for keys, want in cases:
            with self.subTest(keys=keys):
                env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
                t = Term(env, w=200, h=50); self.addCleanup(t.close); self.assertTrue(ready(t))
                for k in keys.split():
                    t.keys(k); time.sleep(0.15)
                t.keys("Enter")
                wait(lambda: env.actions(), 3)
                if want == "b2":  # background session: switch is greyed
                    self.assertEqual(env.actions(), [], f"after keys {keys!r} (b2 is background)")
                else:
                    self.assertEqual(env.actions(), [["switch", want]], f"after keys {keys!r}")

    def test_burst_keys_not_dropped(self):
        """Several keys arriving in one read (bubbletea hands them over as one message)."""
        self.t.lit("ljd")  # l -> b1, j -> b2, d -> review b2
        self.assertEqual(self.calls(1), [["review", "b2"]])
        env2 = Env(); self.addCleanup(env2.cleanup); build_fixture(env2)
        t2 = Term(env2, w=200, h=50); self.addCleanup(t2.close); self.assertTrue(ready(t2))
        t2.lit("jjxy")  # a3, x, confirm
        wait(lambda: env2.actions(), 3); self.assertEqual(env2.actions(), [["kill", "a3"]])
        t2.lit("HHr"); wait(lambda: len(env2.actions()) >= 4, 3)
        self.assertEqual(sorted(map(tuple, env2.actions()[1:])), sorted([("send", "a3", "--", "/handoff")] * 2 + [("restart", "a3")]))

    def test_H_hjkl_letters(self):
        self.t.keys("l"); time.sleep(0.2)
        self.t.keys("H"); c = self.calls(1)
        self.assertEqual(c, [["send", "b1", "--", "/handoff"]], "H = ccmux send <focused> /handoff")
        self.assertTrue(self.t.alive(), "H must not exit the TUI"); self.assertTrue(find_cards(self.t.snap()))

    def test_r_d(self):
        self.t.keys("r"); self.assertEqual(self.calls(1), [["restart", "a1"]])
        self.t.keys("d"); self.assertEqual(self.calls(2)[1:], [["review", "a1"]])
        self.assertTrue(self.t.alive())

    def test_x_confirm(self):
        self.t.keys("x"); self.assertEqual(self.calls(), [], "x alone must not kill")
        self.assertTrue(wait(lambda: re.search(r"y to confirm", "\n".join(r.text for r in self.t.snap()[-3:-1])), 3),
                        "x should ask for confirmation on the line above the key hints:\n" + self.t.text())
        self.t.keys("y"); self.assertEqual(self.calls(1), [["kill", "a1"]])
        self.assertTrue(self.t.alive())

    def test_x_cancel(self):
        for k in ("n", "z"):
            self.t.keys("x"); time.sleep(0.2); self.t.keys(k)
        self.assertEqual(self.calls(), [], "any non-y key cancels")
        self.t.keys("r"); self.assertEqual(self.calls(1), [["restart", "a1"]], "TUI back to normal mode after cancel")

    def test_paste_is_not_commands(self):
        self.t.lit("\x1b[200~xyHr\x1b[201~")  # bracketed paste while browsing
        self.assertEqual(self.calls(), [], "pasted letters must not trigger x / y / H / r")
        self.assertTrue(self.t.alive()); self.assertEqual(len([c for c in find_cards(self.t.snap()) if c.ok]), 9)

    def test_review_failure_shows_last_stderr_line(self):
        self.env.flag("review.fail"); self.t.keys("d")
        self.assertTrue(wait(lambda: "ccmux review: review exploded: the last word" in "\n".join(r.text for r in self.t.snap()[-3:]), 5),
                        "footer should say `ccmux review: <last stderr line>`:\n" + self.t.text())
        self.assertNotIn("first stderr line", "\n".join(r.text for r in self.t.snap()[-3:]))
        self.assertTrue(self.t.alive()); self.assertEqual(len([c for c in find_cards(self.t.snap()) if c.ok]), 9)

    def test_mouse_works_again_after_review(self):
        mouse_on = lambda: tm("display", "-p", "-t", self.t.name, "#{mouse_any_flag}").stdout.strip()
        self.assertEqual(mouse_on(), "1", "board enables mouse reporting")
        self.env.flag("review.hold"); self.t.keys("d")
        self.assertTrue(wait(lambda: "agmux · 9 sessions" in self.t.text() and self.env.actions() and "REVIEW-RUNNING" not in self.t.text(), 8))
        self.assertTrue(wait(lambda: mouse_on() == "1", 3), "mouse reporting must be back on after review (bubbletea v1 leaves it off)")
        c = by_name(find_cards(self.t.snap()), NAMES["a2"]); self.t.click(c.c + c.w // 2, c.r + 2); time.sleep(0.4)
        self.t.keys("H"); self.assertEqual(self.calls(2)[1:], [["send", "a2", "--", "/handoff"]], "click after review selects a2")

    def test_d_hands_terminal_over_then_returns(self):
        self.env.flag("review.hold"); self.t.keys("d")
        self.assertTrue(wait(lambda: "REVIEW-RUNNING" in self.t.text(), 3), "review should own the terminal:\n" + self.t.text())
        self.assertNotIn("agmux · 9 sessions", self.t.text(), "board is suspended while review runs")
        self.assertTrue(wait(lambda: "agmux · 9 sessions" in self.t.text(), 6), "board must come back after review exits")
        self.assertEqual(self.env.actions(), [["review", "a1"]])
        self.assertEqual(len([c for c in find_cards(self.t.snap()) if c.ok]), 9)
        self.assertRegex("\n".join(r.text for r in self.t.snap()[-3:]), "review finished", "footer reports the finished review")
        self.t.keys("j"); time.sleep(0.2); self.t.keys("Enter")
        self.assertEqual(self.calls(2)[1:], [["switch", "a2"]], "board keeps working after review")

    def test_ccmux_stderr_in_footer_no_popup(self):
        self.env.flag("fail"); self.t.keys("r")
        self.assertTrue(wait(lambda: "boom from fake ccmux" in "\n".join(r.text for r in self.t.snap()[-3:]), 3),
                        "stderr should show on the bottom line:\n" + self.t.text())
        self.assertTrue(self.t.alive()); self.assertEqual(len([c for c in find_cards(self.t.snap()) if c.ok]), 9, "cards still rendered")

    def test_background_session_greyed(self):
        self.t.keys("l"); time.sleep(0.2); self.t.keys("j"); time.sleep(0.2)  # b2
        hint = "background session · enter or i attaches it"
        self.assertTrue(wait(lambda: hint in "\n".join(r.text for r in self.t.snap()[-3:]), 3), "footer hint when a background card is focused:\n" + self.t.text())
        for k in ("Enter", "H"):
            self.t.keys(k); time.sleep(0.4)
            self.assertEqual(self.env.actions(), [], f"{k} on background session must not call ccmux")
            self.assertTrue(self.t.alive(), f"{k}: TUI must stay open")
            self.assertEqual(self.t.text().count(hint), 1, f"after {k}: the hint is shown exactly once")
        self.t.keys("i"); time.sleep(0.4)
        self.assertEqual(self.t.text().count(hint), 1, "after s: the hint is shown exactly once")
        self.assertEqual(self.env.actions(), [], "s on background session must not open the prompt / send")
        self.t.lit("x"); self.t.keys("Enter"); time.sleep(0.3)  # (an `x` then lands on the kill confirm, Enter cancels it)
        self.assertIn(hint, "\n".join(r.text for r in self.t.snap()[-3:]))
        self.t.keys("h"); time.sleep(0.2)  # a2 (native) again: hint gone
        self.assertNotIn(hint, "\n".join(r.text for r in self.t.snap()[-3:]))
        self.t.keys("l"); time.sleep(0.2); self.t.keys("x"); time.sleep(0.2); self.t.keys("y")  # back on b2: x/r/d are not greyed
        self.assertEqual(self.calls(1), [["kill", "b2"]])


class Races(Base):
    """Session set changes while a prompt is open / focus is held (store polls every 2s)."""

    def poll_twice(self):
        n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.3)

    def test_focus_follows_session_across_polls(self):
        self.t.keys("l"); time.sleep(0.2)  # b1
        sess = json.loads((self.env.fake / "show.json").read_text()); self.env.write_show(sess[::-1] + [])  # reverse ccmux order
        self.poll_twice(); self.t.keys("Enter")
        self.assertEqual(self.calls(1), [["switch", "b1"]])

    def test_focused_session_disappears(self):
        self.t.keys("j"); time.sleep(0.2)  # a2
        self.env.write_show([x for x in json.loads((self.env.fake / "show.json").read_text()) if x["id"] != "a2"]); self.poll_twice()
        self.assertTrue(self.t.alive()); self.assertEqual(len([c for c in find_cards(self.t.snap()) if c.ok]), 8)
        self.t.keys("Enter"); self.assertEqual(len(self.calls(1)), 1)
        self.assertIn(self.env.actions()[0][0], ("switch",)); self.assertNotEqual(self.env.actions()[0][1], "a2", "must not act on the vanished session")

    def test_kill_confirm_target_vanishes(self):
        self.t.keys("x"); time.sleep(0.2)
        self.env.write_show([x for x in json.loads((self.env.fake / "show.json").read_text()) if x["id"] != "a1"]); self.poll_twice()
        self.t.keys("y"); time.sleep(0.8)
        self.assertEqual(self.env.actions(), [], "no kill of a session that is gone"); self.assertTrue(self.t.alive())


class BackgroundWithError(Base):
    def test_error_and_hint_both_visible(self):
        self.t.keys("l"); time.sleep(0.2); self.t.keys("j"); time.sleep(0.2)  # b2: background
        self.env.flag("fail"); self.t.keys("r")  # restart is not greyed; fake ccmux prints 3 stderr lines and exits 1
        self.assertTrue(wait(lambda: "boom from fake ccmux" in "\n".join(r.text for r in self.t.snap()[-3:]), 4), self.t.text())
        tail = "\n".join(r.text for r in self.t.snap()[-3:])
        self.assertIn("background session · enter or i attaches it", tail, "hint must not be pushed out by the error line")
        self.assertNotIn("second stderr line", self.t.text())
        rows = self.t.text().split("\n")
        while rows and rows[-1] == "" and len(rows) > 50: rows.pop()
        self.assertEqual(len(rows), 50); self.assertIn("agmux · 9 sessions", rows[0])

    def test_poll_error_and_hint_both_visible(self):
        self.t.keys("l"); time.sleep(0.2); self.t.keys("j"); time.sleep(0.2)
        self.env.flag("showfail")
        self.assertTrue(wait(lambda: "show exploded" in self.t.text(), 8), self.t.text())
        tail = "\n".join(r.text for r in self.t.snap()[-3:])
        self.assertIn("background session · enter or i attaches it", tail)
        self.assertNotIn("second poll-error line", self.t.text())


class Mouse(Base):
    """SGR mouse bytes injected straight into the pane's pty (does NOT prove tmux popup forwards real mouse events: see PopupMouse)."""

    def centre(self, name):
        c = by_name(find_cards(self.t.snap()), NAMES[name]); return c.c + c.w // 2, c.r + 2

    def test_click_selects_then_second_click_switches(self):
        x, y = self.centre("a2"); self.t.click(x, y); time.sleep(0.4)
        self.assertEqual(self.env.actions(), [], "first click only selects"); self.assertTrue(self.t.alive())
        self.t.keys("H"); self.assertEqual(self.calls(1), [["send", "a2", "--", "/handoff"]], "click moved focus to a2")
        self.t.click(x, y)
        self.assertEqual(self.calls(2)[1:], [["switch", "a2"]]); self.assertEqual(self.t.exit_status(), 0)

    def test_double_click(self):
        x, y = self.centre("c2"); self.t.click(x, y, n=2)
        self.assertEqual(self.calls(1), [["switch", "c2"]]); self.assertEqual(self.t.exit_status(), 0)

    def test_click_empty_area_noop(self):
        self.t.click(150, 44); time.sleep(0.5)
        self.assertEqual(self.env.actions(), []); self.assertTrue(self.t.alive())

    def test_click_background_card_greyed(self):
        x, y = self.centre("b2"); self.t.click(x, y); time.sleep(0.3); self.t.click(x, y); time.sleep(0.5)
        self.assertEqual(self.env.actions(), [], "switch is greyed for tmuxPane == null"); self.assertTrue(self.t.alive())


class Polling(unittest.TestCase):
    """T1 leftovers: 2s poll cadence, live update, ccmux failing mid-run."""

    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env)
        self.t = Term(self.env, w=200, h=50); self.addCleanup(self.t.close)
        self.assertTrue(ready(self.t), self.t.text())

    def test_cadence(self):
        n0 = self.env.show_calls(); time.sleep(6.5); n = self.env.show_calls() - n0
        self.assertIn(n, (3, 4), f"ccmux show --json calls in 6.5s (want ~every 2s): {n}")

    def test_live_update(self):
        S = self.fx["sessions"]; S[0]["summary"] = "alpha-RENAMED"; S2 = [s for s in S if s["id"] != "e1"]
        self.env.write_show(S2)
        self.assertTrue(wait(lambda: "alpha-RENAMED" in self.t.text() and "echo-long" not in self.t.text(), 6), "rename / removal not shown within 6s")
        self.assertTrue(wait(lambda: "agmux · 8 sessions" in self.t.text(), 2))

    def test_show_failure_keeps_board(self):
        self.env.flag("showfail"); time.sleep(5)
        self.assertTrue(self.t.alive()); self.assertEqual(len([c for c in find_cards(self.t.snap()) if c.ok]), 9, "stale cards kept")
        self.env.flag("showfail", False)


class PopupMouse(unittest.TestCase):
    """Real mouse path: a pty-attached tmux client with `mouse on`, `agmux board` inside a display-popup, bytes written to the client pty.
    Answers tasks.md T4's "does bubbletea receive mouse events in a tmux popup"."""

    def test_popup_click(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        # positions: same fixture / size in a plain pane
        t0 = Term(env, w=200, h=50); self.assertTrue(ready(t0))
        c = by_name(find_cards(t0.snap()), "pj-fallback"); x, y = c.c + c.w // 2, c.r + 2; t0.close()
        tm("start-server", ";", "set", "-g", "status", "off", ";", "set", "-g", "mouse", "on", ";", "new-session", "-d", "-s", "base", "-x", 200, "-y", 50, "sleep 120")
        pid, fd = pty.fork()
        if pid == 0:
            os.execvpe("tmux", ["tmux", "-L", SOCK, "attach", "-t", "base"], {"TERM": "xterm-256color", "PATH": os.environ["PATH"], "LANG": "en_US.UTF-8"})
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 50, 200, 0, 0))
        self.addCleanup(lambda: (os.close(fd), os.kill(pid, 9), os.waitpid(pid, 0)))
        import threading
        def drain():
            try:
                while os.read(fd, 65536): pass
            except OSError: pass
        threading.Thread(target=drain, daemon=True).start()
        cl = wait(lambda: tm("list-clients", "-F", "#{client_name}").stdout.split(), 5); self.assertTrue(cl, "client never attached")
        # display-popup -E blocks until the popup closes, so it must not run through the blocking tm() helper
        pop = subprocess.Popen(["tmux", "-L", SOCK, "display-popup", "-c", cl[0], "-B", "-x", "0", "-y", "0", "-w", "200", "-h", "50", "-E",
                                f"{env.prefix} {shlex.quote(BIN)} board"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(lambda: pop.poll() is None and pop.kill())
        self.assertTrue(wait(lambda: env.show_calls() >= 1, 5), "board did not start in the popup")
        time.sleep(1.0)
        sgr = lambda b: os.write(fd, f"\x1b[<0;{x + 1};{y + 1}{b}".encode())
        sgr("M"); sgr("m"); time.sleep(0.5)
        self.assertEqual(env.actions(), [], "first click only selects")
        os.write(fd, b"H"); self.assertTrue(wait(lambda: env.actions(), 3), "no ccmux call: click did not reach the app (or H did not)")
        self.assertEqual(env.actions(), [["send", "a2", "--", "/handoff"]], "click in popup selected a2")
        sgr("M"); sgr("m"); self.assertTrue(wait(lambda: len(env.actions()) >= 2, 3))
        self.assertEqual(env.actions()[1], ["switch", "a2"])
        self.assertTrue(wait(lambda: pop.poll() == 0, 3), "popup must close after switch (board exits 0)")


if __name__ == "__main__":
    unittest.main()
