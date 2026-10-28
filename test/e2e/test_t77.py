"""T77 the board's `?` entry (action `recall`, default `?`): the recall screen of T76 inside the board's own process. Spec: specs/m6-recall/design.md §2 (看板里 enter 关了的会话: 普通 pane → exec 原地; popup / --stay → 在会话 cwd 开
tmux 新窗口跑 resume 命令再跳过去; 在跑的 → jump), specs/m3-config/tasks.md T77: `?` switches the board to the recall Model, esc or `?` comes back; enter on a closed session: a plain pane execs, a popup / `--stay`
runs `new-window -c <cwd> -n <name> -- <resume argv>` and then switches to it; enter on a running one jumps (as on the board).

STATE: SKELETON, skipped until T76 exists (lib_recall.ui_ready()). Fakes: tmux (the Env's: every call is logged, `Env.tmux_calls()`), claude / pi (lib_recall: cwd + argv recorded). `GUESS:` marks what only T77 decides (the exact tmux argv,
what counts as "popup" in a test: `board --stay`, or AGMUX_CLIENT set by the launcher as in test_t10 / test_t47). Fix the test, not the product, when the design leaves it open."""
import os, re, time, unittest
from lib import wait
from lib_recall import *
from test_t76 import CART, AUDIT, GONE, PI_AUDIT, NAMES

needs_ui = unittest.skipUnless(ui_ready(), "T76 / T77 (agmux -r, the board's ?) is not delivered yet: skeleton")


class Base(unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); self.c = corpus(self.env); self.cart_dir = str(self.env.home / "work" / "shopcart"); self.api_dir = str(self.env.home / "work" / "ledger.worktrees" / "feature" / "audit" / "api")
        self.env.reg("live1", name="live-one", nameSource="user", tmux="sess:@1.%51", cwd=str(self.env.home / "work" / "live"))  # a running session: the board has a card to start from

    def board(self, args="board", w=140, h=40, **kw):
        t = RTerm(self.env, args=args, w=w, h=h, tmux=True, **kw); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: "live-one" in t.text(), 10), t.text()); time.sleep(0.5); return t

    def recall(self, t, query="'coupon"):
        t.keys("?"); self.assertTrue(wait(lambda: t.visible(NAMES), 10), "the recall screen:\n" + t.text()); time.sleep(0.4); t.lit(query); time.sleep(0.6)

    def calls(self, name): return [c for c in self.env.tmux_calls() if c and c[0] == name]


@needs_ui
class Entry(Base):
    def test_question_mark_opens_the_recall_screen_and_esc_or_question_mark_goes_back_to_the_cards(self):
        t = self.board(); t.keys("?"); self.assertTrue(wait(lambda: t.visible(NAMES), 10), t.text()); self.assertNotIn("live-one", t.text(), "the board's cards are not on this screen (GUESS: the list is of closed sessions + the running ones merged: relax if live-one shows)")
        t.esc(); self.assertTrue(wait(lambda: "live-one" in t.text() and not t.visible(NAMES), 4), "esc: back on the board\n" + t.text()); t.keys("?"); self.assertTrue(wait(lambda: t.visible(NAMES), 4)); t.keys("?")
        self.assertTrue(wait(lambda: "live-one" in t.text(), 4), "`?` toggles back (design: esc / ? returns)"); self.assertTrue(t.alive())

    def test_the_key_is_the_recall_action_of_the_keys_table(self):
        self.env.cfg.joinpath("agmux", "config.toml").write_text('[keys]\nrecall = ["ctrl+r"]\n'); t = self.board(); t.keys("?"); time.sleep(0.8); self.assertFalse(t.visible(NAMES), "? is nothing now")
        t.keys("C-r"); self.assertTrue(wait(lambda: t.visible(NAMES), 6), t.text())

    def test_the_board_footer_lists_the_recall_key(self):
        t = self.board(w=232); self.assertRegex(t.text().splitlines()[-1], r"\? recall", "GUESS: `? recall`, zh `? 找回`")

    def test_zh_follows_into_the_recall_screen(self):
        t = self.board(lang="zh"); t.keys("?"); self.assertTrue(wait(lambda: t.visible(NAMES), 10)); self.assertRegex(t.text(), r"[一-鿿]")

    def test_a_typed_query_is_dropped_on_the_way_out_and_the_board_filter_is_not_touched(self):
        t = self.board(); self.recall(t); self.assertEqual(t.visible(NAMES), {"cart"})
        for _ in range(3):  # GUESS: esc is T76's three levels (panel, query, leave): one to three presses get back to the board
            if "live-one" in t.text(): break
            t.esc()
        self.assertIn("live-one", t.text()); self.assertTrue(t.alive()); t.keys("?"); self.assertTrue(wait(lambda: len(t.visible(NAMES)) == 5, 6), "coming back starts a fresh query:\n" + t.text())


@needs_ui
class EnterClosed(Base):
    def test_on_a_plain_pane_a_closed_claude_session_is_resumed_in_place_by_exec(self):
        t = self.board(); self.recall(t); t.keys("Enter"); self.assertTrue(wait(lambda: self.env.ran("claude"), 6), t.text()); r = self.env.ran("claude")[0]
        self.assertEqual((r["argv"], os.path.realpath(r["cwd"])), (["--resume", CART], os.path.realpath(self.cart_dir))); self.assertEqual(self.calls("new-window"), [], "no new window on a plain pane"); self.assertEqual(t.exit_status(5), 0, "the board is gone")

    def test_on_a_plain_pane_a_pi_session_is_resumed_with_its_file(self):
        t = self.board(); self.recall(t, "'inner join"); t.keys("Enter"); self.assertTrue(wait(lambda: self.env.ran("pi"), 6), t.text()); r = self.env.ran("pi")[0]
        self.assertEqual((r["argv"], os.path.realpath(r["cwd"])), (["--session", str(self.c["pi_audit"][0])], os.path.realpath(self.api_dir)))

    def test_with_stay_it_opens_a_tmux_window_in_the_sessions_cwd_runs_the_resume_command_there_and_switches_to_it(self):
        t = self.board("board --stay"); self.recall(t); t.keys("Enter"); self.assertTrue(wait(lambda: self.calls("new-window"), 6), t.text()); nw = self.calls("new-window")[0]
        self.assertEqual(self.env.ran("claude"), [], "nothing is exec'd: the board stays"); self.assertTrue(t.alive())
        self.assertIn("-c", nw); self.assertEqual(os.path.realpath(nw[nw.index("-c") + 1]), os.path.realpath(self.cart_dir)); self.assertEqual(nw[-3:], ["claude", "--resume", CART], "GUESS: the resume argv ends the call (design: `-- <resume argv>`)")
        self.assertIn("-n", nw); self.assertTrue(wait(lambda: self.calls("switch-client") or self.calls("select-window"), 4), "and the client is taken there: " + str(self.env.tmux_calls()))

    def test_in_a_popup_it_is_the_same_as_with_stay(self):
        t = self.board(AGMUX_CLIENT="c-test"); self.recall(t); t.keys("Enter"); self.assertTrue(wait(lambda: self.calls("new-window"), 6), t.text()); self.assertEqual(self.env.ran("claude"), [])  # GUESS: AGMUX_CLIENT is what a popup launcher sets

    def test_the_window_for_a_pi_session_runs_pi_session_file(self):
        t = self.board("board --stay"); self.recall(t, "'inner join"); t.keys("Enter"); self.assertTrue(wait(lambda: self.calls("new-window"), 6)); nw = self.calls("new-window")[0]
        self.assertEqual(nw[-3:], ["pi", "--session", str(self.c["pi_audit"][0])]); self.assertEqual(os.path.realpath(nw[nw.index("-c") + 1]), os.path.realpath(self.api_dir))

    def test_a_vanished_directory_opens_nothing_and_the_footer_says_so(self):
        for args in ("board", "board --stay"):
            t = self.board(args); self.recall(t, "'vanished helper"); t.keys("Enter"); time.sleep(1.0); self.assertEqual(self.env.ran("claude"), []); self.assertEqual(self.calls("new-window"), [], args); self.assertTrue(t.alive())
            self.assertRegex(t.text(), r"(?i)no longer|gone|missing|not found|does not exist", "GUESS: wording"); t.close()


@needs_ui
class EnterRunning(Base):
    def test_a_running_session_is_jumped_to_as_on_the_board_and_nothing_is_started(self):
        self.env.reg(CART, name="cart-fix", nameSource="user", tmux="sess:@1.%52", cwd=self.cart_dir); t = self.board("board --stay"); self.recall(t); t.keys("Enter"); time.sleep(1.0)
        self.assertEqual((self.env.ran("claude"), self.calls("new-window")), ([], []), "it runs: no second copy"); self.assertTrue(wait(lambda: any("%52" in " ".join(c) for c in self.env.tmux_calls() if c[0] in ("switch-client", "select-pane", "select-window")), 4), str(self.env.tmux_calls()))


if __name__ == "__main__":
    unittest.main()
