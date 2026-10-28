"""T77 the board's `?` entry (action `recall`, default `?`): the recall screen of T76 inside the board's own process. Spec: specs/m6-recall/design.md §2 (看板里 enter 关了的会话: 普通 pane → exec 原地; popup / --stay → 在会话 cwd 开
tmux 新窗口跑 resume 命令再跳过去; 在跑的 → jump), specs/m3-config/tasks.md T77: `?` switches the board to the recall Model, esc or `?` comes back; enter on a closed session: a plain pane execs, a popup / `--stay`
runs `new-window -c <cwd> -n <name> -- <resume argv>` and then switches to it; enter on a running one jumps (as on the board).

STATE: live since 4d91819 (dev branch merged); skipped only on a binary without T77 (board_has_recall()). Fakes: tmux (the Env's: every call is logged, `Env.tmux_calls()`; `new-window -P` answers with the pane id in
fake/tmux.newwindow), claude / pi (lib_recall: cwd + argv recorded). Where the board runs is decided by the environment (internal/board/recall_embed.go floating()): ordinary pane = $TMUX and $TMUX_PANE, popup = $TMUX without
$TMUX_PANE, `--stay` = $TMUX. `GUESS:` marks what is still not pinned down by the code."""
import os, re, time, unittest
from lib import wait, SRC
from lib_recall import *
from test_t76 import CART, AUDIT, GONE, PI_AUDIT, NAMES

def board_has_recall():
    """T77 delivered? The `recall` action is in the config since T76; T77 is the board handling it: look for it in the board package of the source tree under test (AGMUX_T77=1 forces it on)"""
    if os.environ.get("AGMUX_T77"): return True
    return any('"recall"' in p.read_text() for p in (SRC / "internal" / "board").glob("*.go") if not p.name.endswith("_test.go") and not p.name.startswith("recall"))


needs_ui = unittest.skipUnless(ui_ready() and board_has_recall(), "T77 (the board's ?) is not delivered yet: skeleton")


class Base(unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); self.c = corpus(self.env); self.cart_dir = str(self.env.home / "work" / "shopcart"); self.api_dir = str(self.env.home / "work" / "ledger.worktrees" / "feature" / "audit" / "api")
        self.env.reg("live1", name="live-one", nameSource="user", tmux="sess:@1.%51", cwd=str(self.env.home / "work" / "live"))  # a running session: the board has a card to start from

    # where the board runs (Model.floating: $TMUX set and (--stay or no $TMUX_PANE)): an ordinary pane of a tmux session has both TMUX and TMUX_PANE, a popup has TMUX but no TMUX_PANE, `--stay` has TMUX
    PLAIN, POPUP = dict(TMUX="/tmp/fake-tmux,1,0", TMUX_PANE="%1"), dict(TMUX="/tmp/fake-tmux,1,0")

    def board(self, args="board", w=140, h=40, **kw):
        (self.env.fake / "tmux.newwindow").write_text("%88\n")  # the pane the fake tmux says `new-window -P` made
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

    def test_zh_follows_into_the_recall_screen(self):
        t = self.board(lang="zh"); t.keys("?"); self.assertTrue(wait(lambda: t.visible(NAMES), 10)); self.assertRegex(t.text(), r"[一-鿿]")

    def test_a_typed_query_is_dropped_on_the_way_out_and_the_board_filter_is_not_touched(self):
        t = self.board(); self.recall(t); self.assertEqual(t.visible(NAMES), {"cart"})
        for _ in range(3):  # esc is T76's three levels (panel, query, leave): one to three presses get back to the board
            if "live-one" in t.text(): break
            t.esc()
        self.assertIn("live-one", t.text()); self.assertTrue(t.alive()); t.keys("?"); self.assertTrue(wait(lambda: len(t.visible(NAMES)) == 5, 6), "coming back starts a fresh query:\n" + t.text())


@needs_ui
class EnterClosed(Base):
    def test_on_a_plain_pane_a_closed_claude_session_is_resumed_in_place_by_exec(self):
        t = self.board(**self.PLAIN); self.recall(t); t.keys("Enter"); self.assertTrue(wait(lambda: self.env.ran("claude"), 6), t.text()); r = self.env.ran("claude")[0]
        self.assertEqual((r["argv"], os.path.realpath(r["cwd"])), (["--resume", CART], os.path.realpath(self.cart_dir))); self.assertEqual(self.calls("new-window"), [], "no new window on a plain pane"); self.assertEqual(t.exit_status(5), 0, "the board is gone")

    def test_on_a_plain_pane_a_pi_session_is_resumed_with_its_file(self):
        t = self.board(**self.PLAIN); self.recall(t, "'inner join"); t.keys("Enter"); self.assertTrue(wait(lambda: self.env.ran("pi"), 6), t.text()); r = self.env.ran("pi")[0]
        self.assertEqual((r["argv"], os.path.realpath(r["cwd"])), (["--session", str(self.c["pi_audit"][0])], os.path.realpath(self.api_dir)))

    def test_with_stay_it_opens_a_tmux_window_in_the_sessions_cwd_runs_the_resume_command_there_and_switches_to_it(self):
        t = self.board("board --stay", **self.POPUP); self.recall(t); t.keys("Enter"); self.assertTrue(wait(lambda: self.calls("new-window"), 6), t.text()); nw = self.calls("new-window")[0]
        self.assertEqual(self.env.ran("claude"), [], "nothing is exec'd: the board stays"); self.assertTrue(t.alive())
        self.assertEqual(nw[:5], ["new-window", "-d", "-P", "-F", "#{pane_id}"], "made without taking the focus, answering with its pane"); self.assertEqual(nw[nw.index("-t") + 1], "=main:", "in the session of the user's client (the fake list-clients says main)")
        self.assertIn("-c", nw); self.assertEqual(os.path.realpath(nw[nw.index("-c") + 1]), os.path.realpath(self.cart_dir)); self.assertEqual(nw[-4:], ["--", "claude", "--resume", CART]); self.assertEqual(nw[nw.index("-n") + 1], "cart-fix", "named after the session")
        self.assertTrue(wait(lambda: any("%88" in c and c[0] in ("switch-client", "select-window", "select-pane") for c in self.env.tmux_calls()), 4), "and the client is taken to the new pane: " + str(self.env.tmux_calls()))

    def test_in_a_popup_it_is_the_same_as_with_stay(self):
        t = self.board(**self.POPUP); self.recall(t); t.keys("Enter"); self.assertTrue(wait(lambda: self.calls("new-window"), 6), t.text()); self.assertEqual(self.env.ran("claude"), [], "a popup is no terminal to exec in: no $TMUX_PANE"); self.assertEqual(self.calls("new-window")[0][-3:], ["claude", "--resume", CART])

    def test_the_window_for_a_pi_session_runs_pi_session_file(self):
        t = self.board("board --stay", **self.POPUP); self.recall(t, "'inner join"); t.keys("Enter"); self.assertTrue(wait(lambda: self.calls("new-window"), 6)); nw = self.calls("new-window")[0]
        self.assertEqual(nw[-3:], ["pi", "--session", str(self.c["pi_audit"][0])]); self.assertEqual(os.path.realpath(nw[nw.index("-c") + 1]), os.path.realpath(self.api_dir))

    def test_a_vanished_directory_opens_nothing_and_the_footer_says_so(self):
        for args, env in (("board", self.PLAIN), ("board --stay", self.POPUP)):
            t = self.board(args, **env); self.recall(t, "'vanished helper"); t.keys("Enter"); time.sleep(1.0); self.assertEqual(self.env.ran("claude"), []); self.assertEqual(self.calls("new-window"), [], args); self.assertTrue(t.alive())
            self.assertRegex(t.text(), r"(?i)no longer exists|does not exist|gone|missing"); t.close()


@needs_ui
class EnterRunning(Base):
    def test_a_running_session_is_jumped_to_as_on_the_board_and_nothing_is_started(self):
        self.env.reg(CART, name="cart-fix", nameSource="user", tmux="sess:@1.%52", cwd=self.cart_dir); t = self.board("board --stay", **self.POPUP); self.recall(t); t.keys("Enter"); time.sleep(1.0)
        self.assertEqual((self.env.ran("claude"), self.calls("new-window")), ([], []), "it runs: no second copy"); self.assertTrue(wait(lambda: any("%52" in " ".join(c) for c in self.env.tmux_calls() if c[0] in ("switch-client", "select-pane", "select-window")), 4), str(self.env.tmux_calls()))


@needs_ui
class Survives(Base):
    def test_the_board_comes_back_as_it_was_filter_and_all(self):
        t = self.board(**self.PLAIN); t.keys("/"); time.sleep(0.3); t.lit("live"); time.sleep(0.4); t.keys("Enter"); time.sleep(0.6); self.assertIn("esc clear", t.text().splitlines()[-1], "a kept filter")
        t.keys("?"); self.assertTrue(wait(lambda: t.visible(NAMES), 10)); t.esc(); self.assertTrue(wait(lambda: "live-one" in t.text() and not t.visible(NAMES), 6), t.text())
        self.assertIn("esc clear", t.text().splitlines()[-1], "the filter is still there"); self.assertTrue(t.alive())

    def test_a_tmux_that_does_not_say_which_pane_it_made_is_a_red_line_and_the_recall_screen_stays(self):
        t = self.board(**self.POPUP); self.recall(t); (self.env.fake / "tmux.newwindow").unlink(); t.keys("Enter")
        self.assertTrue(wait(lambda: "did not say which pane" in t.text(), 6), t.text()); self.assertTrue(t.alive()); self.assertTrue(t.visible(NAMES), "still on the recall screen")


if __name__ == "__main__":
    unittest.main()
