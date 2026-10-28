"""T81 ctrl+n / ctrl+p scroll the preview (T83, tester side; written from the spec before the code: specs/m6-recall/design.md §2「预览滚动」, specs/m3-config/tasks.md 第十六批 T81).
Defaults: `preview_down = ["pgdown", "ctrl+n"]`, `preview_up = ["pgup", "ctrl+p"]`; they scroll in BOTH states of the recall screen (typing and locked: ctrl+n is no editing key there) and on the board's own preview
(real panes: test_t11.PreviewBase, 3 rows a key). A key list the user writes REPLACES the default one. Where the preview of the recall screen is: its right part (>= 62% of the width); what scrolls is read from the first
`scrollme line NNN` of that part.
Skipped until T79-T82 are in the binary (lib_recall.t83_ready())."""
import re, subprocess, tempfile, time, unittest
from lib import wait, plain_config
from test_t82 import boxes
from lib_recall import *
from test_t11 import PreviewBase
from test_t74 import PreviewKeys

needs = unittest.skipUnless(t83_ready(), "T79-T82 are not in this binary yet: written from the spec")
PLAIN = dict(TMUX="/tmp/fake-tmux,1,0", TMUX_PANE="%1")


def first_line(t):
    """1 + how many rows the preview has been scrolled since the first time this was asked of this terminal (the first call is made with the preview at the top): None when no preview shows. The preview starts with a
    header (directory, agent, id, a blank row) before the first hit, so the first hit stays in view for the first rows of scrolling: what is followed is the row of `scrollme line 010`, which has 9 hits above it."""
    snap = t.snap(); bs = boxes(t); x0 = max(b["c"] for b in bs) if len(bs) == 3 else int(t.w * 0.62)
    row = next((r for r, rw in enumerate(snap) if "scrollme line 010" in rw.width_text(x0, len(rw.cells))), None)
    if row is None: return None
    if getattr(t, "_scroll_base", None) is None: t._scroll_base = row
    return 1 + t._scroll_base - row


@needs
class Template(unittest.TestCase):
    def test_the_defaults_in_agmux_config(self):
        with tempfile.TemporaryDirectory() as d:
            out = plain_config(subprocess.run([BIN, "config"], capture_output=True, text=True, env={"PATH": "/usr/bin:/bin", "HOME": d, "XDG_CONFIG_HOME": d}).stdout)
        self.assertIn('preview_up = ["pgup", "ctrl+p"]', out); self.assertIn('preview_down = ["pgdown", "ctrl+n"]', out)


class Scroll:
    """host: start() -> a terminal on the recall screen (typing state, empty query) with the scroll session in the list"""

    def show(self, t):
        t.lit("'scrollme"); self.assertTrue(wait(lambda: first_line(t) is not None, 6), "the preview lists the hits:\n" + t.text()); time.sleep(0.4); self.assertEqual(first_line(t), 1, "from the top")

    def test_ctrl_n_scrolls_the_preview_down_and_ctrl_p_back_in_the_typing_state(self):
        t = self.start(); self.show(t); t.keys("C-n"); self.assertTrue(wait(lambda: (first_line(t) or 0) > 1, 4), "ctrl+n scrolled"); n = first_line(t); t.keys("C-n"); self.assertTrue(wait(lambda: (first_line(t) or 0) > n, 4))
        t.keys("C-p", "C-p"); self.assertTrue(wait(lambda: first_line(t) == 1, 4), "ctrl+p twice is back at the top"); t.keys("C-p"); time.sleep(0.5); self.assertEqual(first_line(t), 1, "and stops there")

    def test_the_same_in_the_locked_state(self):
        t = self.start(); self.show(t); t.keys("Enter"); time.sleep(0.6); self.assertEqual(self.env.ran("claude"), [], "locked, not resumed"); t.keys("C-n"); self.assertTrue(wait(lambda: (first_line(t) or 0) > 1, 4)); t.keys("C-p"); self.assertTrue(wait(lambda: first_line(t) == 1, 4))

    def test_pgdown_and_pgup_still_scroll_and_do_the_same(self):
        t = self.start(); self.show(t); t.keys("NPage"); self.assertTrue(wait(lambda: (first_line(t) or 0) > 1, 4)); a = first_line(t); t.keys("PPage"); self.assertTrue(wait(lambda: first_line(t) == 1, 4)); t.keys("C-n"); self.assertTrue(wait(lambda: first_line(t) == a, 4), "ctrl+n moves as far as pgdown")

    def test_scrolling_does_not_move_the_selection_or_touch_the_query(self):
        t = self.start(); self.show(t); t.keys("C-n"); time.sleep(0.5); self.assertIsNotNone(row_of(t, "'scrollme"), "the query is still the query")
        self.assertTrue(t.alive())


@needs
class RecallScroll(Scroll, unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); scroll_corpus(self.env); wrap_corpus(self.env, 2)

    def start(self):
        t = RTerm(self.env, args="-r", w=140, h=40); self.addCleanup(t.close); self.assertTrue(wait(lambda: t.visible({"a": "scroll-session"}), 10), t.text()); time.sleep(0.5); return t

    def test_the_keys_a_user_binds_replace_the_default_ones(self):
        self.env.cfg.joinpath("agmux", "config.toml").write_text('[keys]\npreview_down = ["ctrl+j"]\npreview_up = ["ctrl+k"]\n'); t = self.start(); self.show(t)
        t.keys("C-n"); time.sleep(0.6); self.assertEqual(first_line(t), 1, "ctrl+n is nothing now"); t.keys("C-j"); self.assertTrue(wait(lambda: (first_line(t) or 0) > 1, 4)); t.keys("C-p"); time.sleep(0.5); self.assertGreater(first_line(t) or 0, 1, "ctrl+p is nothing now")
        t.keys("C-k"); self.assertTrue(wait(lambda: first_line(t) == 1, 4))


@needs
class BoardRecallScroll(Scroll, unittest.TestCase):
    """the recall screen inside the board (`?`)"""

    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); scroll_corpus(self.env); wrap_corpus(self.env, 2)
        self.env.reg("live1", name="live-one", nameSource="user", tmux="sess:@1.%51", cwd=str(self.env.home / "work" / "live"))

    def start(self):
        t = RTerm(self.env, args="board", w=140, h=40, tmux=True, **PLAIN); self.addCleanup(t.close); self.assertTrue(wait(lambda: "live-one" in t.text(), 10), t.text()); time.sleep(0.5); t.keys("?")
        self.assertTrue(wait(lambda: t.visible({"a": "scroll-session"}), 10), t.text()); time.sleep(0.5); return t


@needs
class BoardPreviewDefault(PreviewKeys):
    """the board's own preview of a real pane (200 numbered lines): ctrl+p / ctrl+n are pgup / pgdown (3 rows)"""

    def test_ctrl_p_scrolls_back_three_rows_and_ctrl_n_forward(self):
        self.open_preview(); self.assertEqual(self.last(), 200); self.assertEqual(self.press_and_read("C-p"), 197); self.assertEqual(self.press_and_read("C-p"), 194); self.assertEqual(self.press_and_read("C-n"), 197); self.assertEqual(self.press_and_read("C-n"), 200)
        self.assertEqual(self.press_and_read("C-n"), 200, "stops at the bottom")

    def test_ctrl_n_and_ctrl_p_are_not_forwarded_to_the_pane_and_pgup_still_works(self):
        self.open_preview(); self.press_and_read("C-p"); self.assertEqual(self.press_and_read("PageUp"), 194, "both keys scroll, they add up")
        self.assertNotIn("\x10", tm("capture-pane", "-p", "-t", self.panes["a1"]).stdout)


@needs
class BoardPreviewRebound(PreviewKeys):
    CONF = '[keys]\npreview_up = ["["]\npreview_down = ["]"]\n'

    def test_a_list_of_the_users_replaces_the_default_so_ctrl_p_is_nothing(self):
        self.open_preview(); self.assertEqual(self.press_and_read("C-p"), 200, "ctrl+p is nothing now"); self.assertEqual(self.press_and_read("["), 197); self.assertEqual(self.press_and_read("C-n"), 197, "and ctrl+n does not scroll forward"); self.assertEqual(self.press_and_read("]"), 200)


if __name__ == "__main__":
    unittest.main()
