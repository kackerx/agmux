"""T84 mouse on the recall screen (T83, tester side; written from the spec before the code: specs/m6-recall/design.md §2「鼠标」, specs/m3-config/tasks.md T84). Real SGR mouse bytes (`ESC [ < 0 ; x ; y M` press, `m` release,
wheel 64 up / 65 down) are written into the pane's pty (lib.Term.click / wheel), in `agmux -r` and in the board's `?`:
- a click on a list row selects it, in either state, and changes no state (typing stays typing, locked stays locked);
- a second click on THE SAME row within 400 ms confirms (= enter in the locked state: a closed session is resumed, a running one is jumped to on the board / left alone in `-r`, a vanished directory does nothing);
  two clicks on different rows, or 400 ms apart, are no double click;
- the wheel over the list moves the selection one row, no wrap; over the preview it scrolls 3 rows (like ctrl+n / ctrl+p);
- clicks and the wheel on the input box, the frames and the footer do nothing; with the filter panel open the mouse is ignored altogether;
- inside the board, the mouse still works after an ExecProcess (ctrl+o) came back.
Coordinates are found on the screen, not computed: the row of a session is the row its name is on in the left 60% of the width, the preview is the right part. Skipped until T79-T82 are in the binary
(lib_recall.t83_ready()); `GUESS:` marks what the spec leaves open and the layout decides (T84 itself may not be in that binary yet: if t84 is red on every click, check that first)."""
import os, time, unittest
from lib import wait
from lib_recall import *
from test_t81 import first_line
from test_t82 import boxes

needs = unittest.skipUnless(t83_ready(), "T79-T82 are not in this binary yet: written from the spec")
PLAIN = dict(TMUX="/tmp/fake-tmux,1,0", TMUX_PANE="%1")
POPUP = dict(TMUX="/tmp/fake-tmux,1,0")


def list_row(t, name):
    """(row, column of the name) of the session `name` in the left 60% of the screen (the list), None when it is not shown"""
    snap = t.snap(); x1 = int(t.w * 0.6)
    for r, row in enumerate(snap):
        c = row.find(name)
        if 0 <= c < x1: return r, c
    return None


class Mouse:
    """host: start(cwd=None, scroll=False) -> a terminal on the recall screen in the typing state, wrap-1 .. wrap-4 listed (wrap-1 first); self.ids / self.dirs; self.resumed() -> what the host did to resume"""

    def sel(self, t): return selected_id(t, self.ids)

    def until_sel(self, t, i, msg=""):
        self.assertTrue(wait(lambda: self.sel(t) == self.ids[i], 4), f"{msg}: want wrap-{i + 1} selected, the preview shows {self.sel(t)}\n{t.text()}")

    def at(self, t, name):
        f = None
        self.assertTrue(wait(lambda: list_row(t, name), 4), f"{name} is in the list:\n{t.text()}"); f = list_row(t, name); return f[1] + 2, f[0]  # (column, row), 0-based: a cell inside the row

    def click(self, t, name, n=1):
        x, y = self.at(t, name); t.click(x, y, n=n)

    def lock(self, t):
        t.lit("'wrap test"); time.sleep(0.5); t.keys("Enter"); time.sleep(0.6)

    def not_text(self, t):
        t.lit("x"); time.sleep(0.5); self.assertIsNone(row_of(t, "'wrap testx"), "locked: a letter is no text")

    # ---- single click
    def test_a_click_selects_the_row_in_the_typing_state_and_stays_in_it(self):
        t = self.start(); self.until_sel(t, 0); self.click(t, "wrap-3"); self.until_sel(t, 2, "click"); self.assertEqual(self.resumed(), [], "a click does not resume")
        t.lit("zz"); self.assertTrue(wait(lambda: row_of(t, "zz") is not None, 4), "still typing: letters are text")

    def test_a_click_selects_the_row_in_the_locked_state_and_stays_locked(self):
        t = self.start(); self.lock(t); self.click(t, "wrap-4"); self.until_sel(t, 3, "click"); self.assertEqual(self.resumed(), []); self.not_text(t); self.click(t, "wrap-2"); self.until_sel(t, 1, "another click")

    # ---- double click
    def test_two_clicks_on_one_row_within_400ms_confirm_in_the_locked_state(self):
        t = self.start(); self.lock(t); x, y = self.at(t, "wrap-3"); t.click(x, y, n=2); self.assertTrue(wait(lambda: self.resumed(), 6), t.text()); self.assertEqual(self.resumed_ids(), [self.ids[2]], "the clicked row, not the one that was selected"); self.assertEqual(os.path.realpath(self.resumed()[0]["cwd"]), os.path.realpath(self.dirs[2]))

    def test_two_clicks_on_one_row_confirm_in_the_typing_state_as_well(self):
        t = self.start(); x, y = self.at(t, "wrap-2"); t.click(x, y, n=2); self.assertTrue(wait(lambda: self.resumed(), 6), t.text()); self.assertEqual(self.resumed_ids(), [self.ids[1]], "GUESS: the double click is a confirm in both states (the spec: `不改打字 / 锁定态` is about the single click)")

    def test_two_clicks_on_different_rows_are_no_double_click(self):
        t = self.start(); self.lock(t); x2, y2 = self.at(t, "wrap-2"); x3, y3 = self.at(t, "wrap-3"); t.click(x2, y2); time.sleep(0.1); t.click(x3, y3); time.sleep(0.8); self.until_sel(t, 2, "the second click selected"); self.assertEqual(self.resumed(), [], "no resume")
        self.assertTrue(t.alive())

    def test_two_clicks_on_one_row_more_than_400ms_apart_are_no_double_click(self):
        t = self.start(); self.lock(t); x, y = self.at(t, "wrap-2"); t.click(x, y); time.sleep(0.7); t.click(x, y); time.sleep(0.8); self.until_sel(t, 1); self.assertEqual(self.resumed(), []); self.assertTrue(t.alive())

    def test_a_third_click_after_a_double_click_that_did_nothing_is_not_a_third_of_it(self):
        t = self.start(); self.lock(t); x, y = self.at(t, "wrap-2"); t.click(x, y); time.sleep(0.7); t.click(x, y); time.sleep(0.15); self.assertEqual(self.resumed(), [], "the second click is a first click again: 0.7 s after the first"); self.assertTrue(t.alive())

    def test_a_vanished_directory_is_not_started_by_a_double_click(self):
        import shutil; shutil.rmtree(self.dirs[3]); t = self.start(); self.lock(t); x, y = self.at(t, "wrap-4"); t.click(x, y, n=2); time.sleep(1.2); self.assertEqual(self.resumed(), []); self.assertTrue(t.alive())
        self.assertRegex(t.text(), r"(?i)no longer exists|does not exist|gone|missing", "the footer says why (as for enter)")

    # ---- the wheel
    def test_the_wheel_over_the_list_moves_the_selection_one_row_a_notch_without_wrapping(self):
        t = self.start(); self.until_sel(t, 0); x, y = self.at(t, "wrap-2")
        for want in (1, 2, 3): t.wheel(x, y, up=False); self.until_sel(t, want, "wheel down")
        t.wheel(x, y, up=False); time.sleep(0.6); self.until_sel(t, 3, "no wrap at the bottom")
        for want in (2, 1, 0): t.wheel(x, y, up=True); self.until_sel(t, want, "wheel up")
        t.wheel(x, y, up=True); time.sleep(0.6); self.until_sel(t, 0, "no wrap at the top"); self.assertEqual(self.resumed(), [])

    def test_the_wheel_works_in_the_locked_state_too(self):
        t = self.start(); self.lock(t); x, y = self.at(t, "wrap-1"); t.wheel(x, y, up=False); self.until_sel(t, 1); self.not_text(t)

    def test_the_wheel_over_the_preview_scrolls_it_three_rows_a_notch(self):
        t = self.start(scroll=True); t.lit("'scrollme"); self.assertTrue(wait(lambda: first_line(t) == 1, 6), t.text()); time.sleep(0.4); x, y = int(t.w * 0.8), t.h // 2
        t.wheel(x, y, up=False); self.assertTrue(wait(lambda: first_line(t) == 4, 4), f"3 rows: {first_line(t)}"); t.wheel(x, y, up=False, n=2); self.assertTrue(wait(lambda: first_line(t) == 10, 4), f"two more notches: {first_line(t)}")
        t.wheel(x, y, up=True, n=3); self.assertTrue(wait(lambda: first_line(t) == 1, 4)); t.wheel(x, y, up=True); time.sleep(0.5); self.assertEqual(first_line(t), 1, "stops at the top")

    # ---- the parts that do nothing
    def test_clicks_and_the_wheel_on_the_input_box_the_frames_and_the_footer_do_nothing(self):
        t = self.start(); self.click(t, "wrap-3"); self.until_sel(t, 2); bs = boxes(t); inp = sorted(bs, key=lambda b: (b["c"], b["r"]))[0]; lst = sorted(bs, key=lambda b: (b["c"], b["r"]))[1]
        spots = [("input row", 3, inp["r"] + 1), ("input top border", 3, inp["r"]), ("list top border", 3, lst["r"]), ("list bottom border", 3, lst["r2"]), ("footer", 3, t.h - 1), ("footer, twice", 3, t.h - 1)]
        for name, x, y in spots: t.click(x, y, n=2); t.wheel(x, y, up=False); time.sleep(0.4)
        time.sleep(0.6); self.until_sel(t, 2, "nothing moved the selection"); self.assertEqual(self.resumed(), [], "and a double click on the input box or the footer is not a confirm"); self.assertTrue(t.alive())

    def test_with_the_filter_panel_open_the_mouse_is_ignored(self):
        t = self.start(); self.until_sel(t, 0); t.keys("C-f"); time.sleep(0.7); x, y = int(t.w * 0.2), t.h // 2
        t.click(x, y, n=2); t.wheel(x, y, up=False, n=3); time.sleep(0.6); self.assertEqual(self.resumed(), [], "no resume through the panel"); t.keys("Escape"); time.sleep(0.6); self.until_sel(t, 0, "the selection was not moved behind the panel"); self.assertTrue(t.alive())
        self.assertEqual(len([n for n in ("wrap-1", "wrap-2", "wrap-3", "wrap-4") if n in t.text()]), 4, "and the panel's own clicks did not change the filter")

    def test_the_mouse_is_still_on_after_the_editor_came_back(self):
        t = self.start(); self.until_sel(t, 0); t.keys("C-o"); self.assertTrue(wait(lambda: self.env.ran("fakeedit"), 4)); time.sleep(1.0); self.click(t, "wrap-3"); self.until_sel(t, 2, "a click after ctrl+o")
        x, y = self.at(t, "wrap-1"); t.wheel(x, y, up=False); self.until_sel(t, 3, "and the wheel (one row down from wrap-3)")


class Base:
    def resumed_ids(self):
        return [x["argv"][-1] for x in self.resumed()]


@needs
class RecallScreen(Mouse, Base, unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); w = wrap_corpus(self.env); self.ids, self.dirs = [x[0] for x in w], [x[1] for x in w]

    def start(self, cwd=None, scroll=False):
        if scroll: scroll_corpus(self.env)
        t = RTerm(self.env, args="-r", w=140, h=40, cwd=cwd); self.addCleanup(t.close); self.assertTrue(wait(lambda: t.visible({"a": "wrap-1", "b": "wrap-2"}), 10), t.text()); time.sleep(0.5); return t

    def resumed(self): return self.env.ran("claude")

    def test_a_double_click_on_a_running_session_leaves_it_alone_and_the_footer_says_it_runs(self):
        self.env.reg(self.ids[1], name="wrap-2", nameSource="user", cwd=self.dirs[1]); t = self.start(); x, y = self.at(t, "wrap-2"); t.click(x, y, n=2); time.sleep(1.2); self.assertEqual(self.resumed(), []); self.assertTrue(t.alive())
        self.assertRegex(t.text(), r"(?i)running|live|already", "as for enter")


@needs
class InsideTheBoard(Mouse, Base, unittest.TestCase):
    """the board's `?` in an ordinary pane: a double click on a closed session execs in place (T77); the board's own mouse keeps working around it"""

    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); w = wrap_corpus(self.env); self.ids, self.dirs = [x[0] for x in w], [x[1] for x in w]
        self.env.reg("live1", name="live-one", nameSource="user", tmux="sess:@1.%51", cwd=str(self.env.home / "work" / "live"))

    def start(self, cwd=None, scroll=False, args="board", env=PLAIN):
        if scroll: scroll_corpus(self.env)
        (self.env.fake / "tmux.newwindow").write_text("%88\n"); t = RTerm(self.env, args=args, w=140, h=40, cwd=cwd, tmux=True, **env); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: "live-one" in t.text(), 10), t.text()); time.sleep(0.5); t.keys("?"); self.assertTrue(wait(lambda: t.visible({"a": "wrap-1", "b": "wrap-2"}), 10), t.text()); time.sleep(0.5); return t

    def resumed(self): return self.env.ran("claude")

    def test_a_double_click_on_a_running_session_jumps_to_its_pane(self):
        self.env.reg(self.ids[1], name="wrap-2", nameSource="user", tmux="sess:@1.%52", cwd=self.dirs[1]); t = self.start(args="board --stay", env=POPUP); x, y = self.at(t, "wrap-2"); t.click(x, y, n=2)
        self.assertTrue(wait(lambda: any("%52" in " ".join(c) for c in self.env.tmux_calls() if c and c[0] in ("switch-client", "select-pane", "select-window")), 5), str(self.env.tmux_calls())); self.assertEqual(self.resumed(), [])

    def test_in_a_popup_a_double_click_makes_a_window_and_the_mouse_still_works_after_it(self):
        t = self.start(args="board --stay", env=POPUP); self.lock(t); x, y = self.at(t, "wrap-3"); t.click(x, y, n=2)
        self.assertTrue(wait(lambda: [c for c in self.env.tmux_calls() if c and c[0] == "new-window"], 6), str(self.env.tmux_calls())); self.assertEqual(self.resumed(), [], "nothing is exec'd: a window was made"); time.sleep(1.0)
        self.assertTrue(t.alive()); self.click(t, "wrap-1"); self.until_sel(t, 0, "the mouse still works after the window was made")

    def test_a_click_and_the_wheel_on_the_board_itself_are_not_swallowed_after_leaving_recall(self):
        t = self.start(); t.esc(); self.assertTrue(wait(lambda: "live-one" in t.text() and not t.visible({"a": "wrap-1"}), 5), t.text()); f = row_of(t, "live-one"); self.assertIsNotNone(f); t.click(f[1] + 2, f[0]); time.sleep(0.6); self.assertTrue(t.alive())


if __name__ == "__main__":
    unittest.main()
