"""T80 the recall screen is two-stage (T83, tester side; written from the spec before the code: specs/m6-recall/design.md §2「两段式」, specs/m3-config/tasks.md 第十六批 T80), like the board's `/`:
TYPING: the query is edited as you type, the first row is pre-selected, up / down move the selection, enter KEEPS the query and goes to SELECTING (no resume), esc with an empty query leaves the screen (an open panel closes first).
SELECTING: letters are no text; `down` / `up` (j / k, arrows) move the selection and do NOT wrap (n / N do nothing there since 802fb83), enter resumes the selected session, `search` (/) goes back to the input with the query
kept, esc clears the query and goes back to typing. `agmux -r <query>` starts SELECTING. ctrl+y / ctrl+o / ctrl+f / ctrl+t work in both. Inside the board (`?`) the same.
Which row is selected is read from the preview: it shows the selected session's id and no other id does (lib_recall.selected_id). Four closed sessions wrap-1 .. wrap-4 (wrap-1 the newest), all saying `wrap test sentence <i>`.
Skipped until T79-T82 are in the binary (lib_recall.t83_ready()). `GUESS:` marks what the spec leaves open."""
import os, shlex, time, unittest
from lib import wait
from lib_recall import *

needs = unittest.skipUnless(t83_ready(), "T79-T82 are not in this binary yet: written from the spec")
PLAIN = dict(TMUX="/tmp/fake-tmux,1,0", TMUX_PANE="%1")  # a board in an ordinary pane of tmux: enter execs in place (T77)
Q = "'wrap test"


class TwoStage:
    """the cases, for a host that gives start(cwd=None) -> a terminal on the recall screen in the typing state with the four sessions listed"""

    def sel(self, t): return selected_id(t, self.ids)

    def until_sel(self, t, i, msg=""):
        self.assertTrue(wait(lambda: self.sel(t) == self.ids[i], 4), f"{msg}: want wrap-{i + 1} selected, preview shows {self.sel(t)}\n{t.text()}")

    def lock(self, t, i=0):
        t.lit(Q); time.sleep(0.5); self.until_sel(t, 0, "typing a query pre-selects the first row"); t.keys("Enter"); time.sleep(0.6)

    def not_text(self, t):
        """in the selecting state a letter is no text: the query on the screen does not change"""
        t.lit("x"); time.sleep(0.5); self.assertIsNotNone(row_of(t, Q), "the query is still there"); self.assertIsNone(row_of(t, Q + "x"), "`x` was not typed into it")

    def test_typing_preselects_the_first_row_and_up_down_move_the_selection(self):
        t = self.start(); self.until_sel(t, 0, "empty query: the newest"); t.lit(Q); time.sleep(0.4); self.until_sel(t, 0)
        t.keys("Down"); self.until_sel(t, 1); t.keys("Down"); self.until_sel(t, 2); t.keys("Up"); self.until_sel(t, 1)

    def test_enter_in_the_typing_state_keeps_the_query_and_does_not_resume(self):
        t = self.start(); t.lit(Q); time.sleep(0.4); t.keys("Down"); self.until_sel(t, 1); t.keys("Enter"); time.sleep(1.0)
        self.assertEqual(self.env.ran("claude"), [], "enter is not resume any more"); self.assertTrue(t.alive()); self.until_sel(t, 1, "the selection is where it was"); self.not_text(t)

    def test_n_and_N_do_nothing_in_the_selecting_state_they_are_not_text_either(self):
        # (802fb83: the user dropped n / N from the recall screen)
        t = self.start(); self.lock(t); t.keys("j"); self.until_sel(t, 1)
        for k in ("n", "N", "n", "n", "N"): t.lit(k); time.sleep(0.3)
        time.sleep(0.5); self.until_sel(t, 1, "the selection did not move"); self.assertIsNotNone(row_of(t, Q), "the query is as it was"); self.assertIsNone(row_of(t, Q + "n"), "n was not typed"); self.assertTrue(t.alive()); self.assertEqual(self.env.ran("claude"), [])

    def test_selecting_j_and_k_move_and_do_not_wrap(self):
        t = self.start(); self.lock(t)
        for want in (1, 2, 3): t.keys("j"); self.until_sel(t, want, "j")
        t.keys("j"); time.sleep(0.5); self.until_sel(t, 3, "j at the last row stays"); t.keys("k"); self.until_sel(t, 2)
        t.keys("k", "k"); self.until_sel(t, 0); t.keys("k"); time.sleep(0.5); self.until_sel(t, 0, "k at the first row stays")

    def test_selecting_up_and_down_arrows_move_like_j_and_k(self):
        t = self.start(); self.lock(t); t.keys("Down"); self.until_sel(t, 1); t.keys("Up"); self.until_sel(t, 0); t.keys("Up"); time.sleep(0.4); self.until_sel(t, 0, "no wrap")

    def test_enter_in_the_selecting_state_resumes_the_selected_session_in_its_directory(self):
        t = self.start(); self.lock(t); t.keys("j"); self.until_sel(t, 1); t.record(); t.keys("Enter"); self.assertTrue(wait(lambda: self.env.ran("claude"), 6), t.text()); r = self.env.ran("claude")
        self.assertEqual([x["argv"] for x in r], [["--resume", self.ids[1]]]); self.assertEqual(os.path.realpath(r[0]["cwd"]), os.path.realpath(self.dirs[1])); self.assertEqual(t.exit_status(5), 0, "the screen is gone")

    def test_a_letter_is_no_text_in_the_selecting_state(self):
        t = self.start(); self.lock(t); self.not_text(t)

    def test_search_key_goes_back_to_the_input_with_the_query_kept_and_enter_locks_again(self):
        t = self.start(); self.lock(t); t.lit("/"); time.sleep(0.5); self.assertIsNotNone(row_of(t, Q), "the query is kept"); x, y, vis = cursor(t); f = row_of(t, Q); self.assertEqual((y, x), (f[0], f[1] + len(Q)), "the cursor is at its end")
        t.lit(" 3"); self.until_sel(t, 2, "typing is text again: `wrap test 3` is the third session only"); self.assertEqual(self.env.ran("claude"), []); t.keys("Enter"); time.sleep(0.6); self.assertEqual(self.env.ran("claude"), [], "enter locks again, no resume")
        t.keys("Enter"); self.assertTrue(wait(lambda: self.env.ran("claude"), 6)); self.assertEqual(self.env.ran("claude")[0]["argv"], ["--resume", self.ids[2]])

    def test_esc_in_the_selecting_state_clears_the_query_and_goes_back_to_typing(self):
        t = self.start(); self.lock(t); t.esc(); self.assertTrue(wait(lambda: row_of(t, Q) is None, 4), "the query is cleared:\n" + t.text()); self.assertTrue(t.alive()); self.until_sel(t, 0, "everything is listed again")
        t.lit("'wrap"); self.assertTrue(wait(lambda: row_of(t, "'wrap") is not None, 4), "typing state: letters are text again")

    def test_esc_with_an_empty_query_in_the_typing_state_leaves_and_resumes_nothing(self):
        t = self.start(); t.esc(); self.assertEqual(t.exit_status(5), 0); self.assertEqual(self.env.ran("claude"), [])

    def test_esc_in_the_typing_state_with_a_query_does_not_leave(self):
        t = self.start(); t.lit(Q); time.sleep(0.4); t.esc(); time.sleep(0.5); self.assertTrue(t.alive(), "the query goes first (GUESS: it is cleared; the spec says the screen is left only with an empty query)"); self.assertIsNone(row_of(t, Q))

    def test_the_panel_closes_before_the_state_changes_or_the_screen_is_left(self):
        t = self.start(); t.keys("C-f"); time.sleep(0.6); t.esc(); time.sleep(0.5); self.assertTrue(t.alive()); self.until_sel(t, 0, "the list is back, the screen is not left")
        self.lock(t); t.keys("C-f"); time.sleep(0.6); t.esc(); time.sleep(0.5); self.assertIsNotNone(row_of(t, Q), "the panel closed, the query is still there"); self.until_sel(t, 0)

    def both(self, fn, cwd=None):
        for state in ("typing", "selecting"):
            t = self.start(cwd=cwd)
            if state == "selecting": self.lock(t)
            else: self.until_sel(t, 0)
            with self.subTest(state=state): fn(t)
            t.close()

    def test_ctrl_y_copies_the_resume_command_in_both_states(self):
        def go(t):
            n = len(self.env.ran("pbcopy")); t.keys("C-y"); self.assertTrue(wait(lambda: len(self.env.ran("pbcopy")) > n, 4), t.text()); self.assertEqual(self.env.ran("pbcopy")[-1]["stdin"].strip(), f"cd '{self.dirs[0]}' && claude --resume {self.ids[0]}"); self.assertTrue(t.alive())
        self.both(go)

    def test_ctrl_o_opens_the_transcript_in_both_states(self):
        def go(t):
            n = len(self.env.ran("fakeedit")); t.keys("C-o"); self.assertTrue(wait(lambda: len(self.env.ran("fakeedit")) > n, 4), t.text()); self.assertTrue(self.env.ran("fakeedit")[-1]["argv"][0].endswith(self.ids[0] + ".jsonl")); time.sleep(0.8); self.assertTrue(t.alive())
        self.both(go)

    def test_ctrl_f_opens_the_panel_in_both_states(self):
        def go(t): t.keys("C-f"); time.sleep(0.7); self.assertIn("claude", t.text().lower().split("\n", 3)[-1], "the panel lists the sources"); t.esc()
        self.both(go)

    def test_ctrl_t_switches_the_scope_in_both_states(self):
        def go(t):
            t.keys("C-t"); self.assertTrue(wait(lambda: t.visible({"w1": "wrap-1"}) and not t.visible({"w2": "wrap-2"}), 4), "this project only:\n" + t.text()); t.keys("C-t"); self.assertTrue(wait(lambda: t.visible({"w2": "wrap-2"}), 4))
        self.both(go, cwd=self.dirs[0])

    def test_the_footer_lists_other_keys_in_the_two_states(self):
        t = self.start(); typing = t.text().rstrip().splitlines()[-1]; self.lock(t); selecting = t.text().rstrip().splitlines()[-1]
        self.assertNotEqual(typing, selecting, "each state lists its own keys"); self.assertIn("enter", typing); self.assertIn("enter", selecting); self.assertIn("/", selecting, "GUESS: the selecting state names `/` (search again)")


@needs
class RecallScreen(TwoStage, unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); w = wrap_corpus(self.env); self.ids, self.dirs = [x[0] for x in w], [x[1] for x in w]

    def start(self, cwd=None, query=None):
        t = RTerm(self.env, args="-r" + (f" {shlex.quote(query)}" if query else ""), w=140, h=40, cwd=cwd); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: t.visible({"a": "wrap-1", "b": "wrap-2"}), 10), t.text()); time.sleep(0.5); return t

    def test_a_query_on_the_command_line_starts_in_the_selecting_state(self):
        t = self.start(query=Q); self.until_sel(t, 0); self.not_text(t); t.keys("j"); self.until_sel(t, 1, "j moves, it is not typed"); self.assertEqual(self.env.ran("claude"), [], "starting with a query does not resume by itself")
        t.keys("Enter"); self.assertTrue(wait(lambda: self.env.ran("claude"), 6)); self.assertEqual(self.env.ran("claude")[0]["argv"], ["--resume", self.ids[1]])

    def test_without_a_query_argument_it_starts_in_the_typing_state(self):
        t = self.start(); t.lit("'wrap"); self.assertTrue(wait(lambda: row_of(t, "'wrap") is not None, 4))


@needs
class InsideTheBoard(TwoStage, unittest.TestCase):
    """the board's `?`: the same two stages; enter on a closed session in an ordinary pane execs in place (T77)"""

    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); w = wrap_corpus(self.env); self.ids, self.dirs = [x[0] for x in w], [x[1] for x in w]
        self.env.reg("live1", name="live-one", nameSource="user", tmux="sess:@1.%51", cwd=str(self.env.home / "work" / "live"))

    def start(self, cwd=None):
        (self.env.fake / "tmux.newwindow").write_text("%88\n"); t = RTerm(self.env, args="board", w=140, h=40, cwd=cwd, tmux=True, **PLAIN); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: "live-one" in t.text(), 10), t.text()); time.sleep(0.5); t.keys("?"); self.assertTrue(wait(lambda: t.visible({"a": "wrap-1", "b": "wrap-2"}), 10), t.text()); time.sleep(0.5); return t

    def test_esc_with_an_empty_query_in_the_typing_state_leaves_and_resumes_nothing(self):  # in the board that means: back to the cards, not out of agmux
        t = self.start(); t.esc(); self.assertTrue(wait(lambda: "live-one" in t.text() and not t.visible({"a": "wrap-1"}), 5), "back on the board:\n" + t.text()); self.assertEqual(self.env.ran("claude"), []); self.assertTrue(t.alive())


if __name__ == "__main__":
    unittest.main()
