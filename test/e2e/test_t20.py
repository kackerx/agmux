"""T20 `s` global jump (flash). Spec: specs/m3-config/design.md「`s` 全局定位（flash）」, tasks.md T20.

`s` puts a one-letter label on the top border of every visible card (and on folded group titles), ordered by distance from the focused card
(the focused card is `a`); the other content is dimmed; pressing a label only MOVES THE FOCUS (no jump to the session); esc cancels.
"""
import json, unittest
import test_t12 as _t12
from lib import *
from test_t3 import ready, last_text_row, border_colours, C_FOCUS, NAMES, by_name
from test_t11 import PreviewBase, CAT_CMD

ALPHABET = "asdfghjklqwertyuiopzxcvbnm"
LABELS = list(ALPHABET) + list(ALPHABET.upper())
INK, MUTED = (40, 42, 54), (98, 114, 164)  # label text #282a36 on the focus colour; the dimmed content is `muted` #6272a4


def label_cells(snap):
    """(row, col, char) of every label: a cell on a card's top border (or a folded title) drawn dark-on-focus-colour"""
    out = []
    for r, row in enumerate(snap):
        for c, cell in enumerate(row.cells):
            if cell[0].strip() and cell[2] == C_FOCUS and cell[1] == ("rgb", *INK) and r > 0 and r < len(snap) - 1:
                out.append((r, c, cell[0]))
    return out


def near(a, b, d=3):
    return a[0] == "rgb" and all(abs(x - y) <= d for x, y in zip(a[1:], b))


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        self.t = self.board()

    def config(self, text):
        (self.cfg / "agmux" / "config.toml").write_text(text)

    def board(self, w=200, h=50, n=9):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(ready(t, n), t.text()); return t

    def bar(self):
        return last_text_row(self.t.snap())

    def focused(self):
        return [c.line(1)[8:-6].strip() for c in find_cards(self.t.snap()) if border_colours(c) == {C_FOCUS}]

    def flash(self):
        self.t.keys("s"); self.assertTrue(wait(lambda: "jump: press a label" in self.bar(), 3), self.t.text())
        time.sleep(0.2)

    def labels_by_name(self):
        snap = self.t.snap(); out = {}
        for c in find_cards(snap):
            out[c.line(1)[8:-6].strip()] = snap[c.r].cells[c.c + 1][0]
        return out

    def expected_order(self, focus_name):
        """card names in label order: by distance from the focused card (rows first, then columns, then reading order)"""
        cs = find_cards(self.t.snap()); f = next(c for c in cs if c.line(1)[8:-6].strip() == focus_name)
        return [c.line(1)[8:-6].strip() for c in sorted(cs, key=lambda c: (abs(c.r - f.r), abs(c.c - f.c), c.r, c.c)) if True]


class Labels(Base):
    def test_default_footer_lists_s(self):
        self.assertIn("s flash", self.bar())

    def test_every_card_gets_a_label_nearest_first_the_focused_one_is_a(self):
        before = border_colours(find_cards(self.t.snap())[0]); self.flash()
        got = self.labels_by_name()
        self.assertEqual(got["alpha-main"], "a", "the focused card is a")
        self.assertEqual(got, dict(zip(self.expected_order("alpha-main"), "asdfghjkl")), "9 cards, 9 labels, in the order of distance")
        self.assertEqual(got["bravo-29"] + got["charlie-30"] + got["delta-60"], "sdf", "the first row left to right")
        self.assertIn("jump: press a label · esc cancel", self.bar())
        self.assertEqual(sorted(l[2] for l in label_cells(self.t.snap())), sorted("asdfghjkl"), "exactly 9 label cells on screen")

    def test_labels_follow_the_distance_from_the_focus_not_the_screen_order(self):
        self.t.keys("l"); self.t.keys("l"); self.t.keys("j"); time.sleep(0.3)
        self.assertEqual(self.focused(), ["charlie-59"]); self.flash()
        got = self.labels_by_name(); order = self.expected_order("charlie-59")
        self.assertEqual(got["charlie-59"], "a"); self.assertEqual(order[0], "charlie-59")
        self.assertEqual(got, dict(zip(order, "asdfghjkl")))
        self.assertEqual([got[n] for n in ("charlie-59", "bravo-bg", "pj-fallback", "charlie-30")], list("asdf"),
                         "same row first (nearest left, then the next one), then the row above: distance = |dy| * width + |dx|")
        self.assertEqual(got["bravo-29"] + got["delta-60"], "gh", "equal distance (one column left / right): reading order, left first")

    def test_label_style_and_the_rest_is_dimmed(self):
        normal = {c.line(1)[8:-6].strip(): [x[1] for x in c.rows[1].cells if x[0].strip() and x[0] not in "│┃"] for c in find_cards(self.t.snap())}
        self.flash(); snap = self.t.snap(); cs = find_cards(snap)
        for c in cs:
            lab = snap[c.r].cells[c.c + 1]
            self.assertEqual((lab[1], lab[2]), (("rgb", *INK), C_FOCUS), "dark text on the focus colour, one cell right of the corner")
            self.assertTrue(snap[c.r].cells[c.c][0] in "╭┏", "the corner itself is still there")
            inside = {x[1] for r in c.rows[1:4] for x in r.cells[1:-1] if x[0].strip()}
            self.assertEqual(len(inside), 1, f"{c.line(1)}: all card content in one colour")
            self.assertTrue(near(next(iter(inside)), MUTED), f"{c.line(1)}: content is `muted`: {inside}")
        self.assertNotEqual(normal["alpha-main"], [("rgb", *MUTED)] * len(normal["alpha-main"]))

    def test_folded_group_titles_get_labels_too(self):
        self.t.keys("f"); time.sleep(0.4); self.flash()
        title = self.t.snap()[1]; self.assertIn("▸", title.text); self.assertEqual(title.cells[1][2], C_FOCUS, "the label sits right of the ▸ and is drawn like the card labels")
        lab = title.cells[1][0]; self.assertIn(lab, "asdfghjkl")
        self.t.keys(lab); time.sleep(0.4)
        self.assertTrue(self.t.alive()); self.assertTrue("jump:" not in self.bar())
        self.t.keys("Enter"); time.sleep(0.5)
        self.assertIn("▾ ~/work/alpha (3)", self.t.text(), "the focus is on the folded title: enter expands it, it does not jump")
        self.assertEqual(self.env.actions(), [])

    def test_thirty_cards_second_round_is_upper_case(self):
        S = json.loads((self.env.fake / "show.json").read_text()); base = S[0]; many = []
        for i in range(30):
            many.append(dict(base, id=f"s{i:02d}", summary=f"sess-{i:02d}", project=f"p{i % 4}", cwd=f"{self.env.home}/work/p{i % 4}", mainRepoRoot=f"{self.env.home}/work/p{i % 4}",
                             tmuxPane=f"%{200 + i}", status="idle", subagents=[], pendingTool=None, nativeSessionId=f"n-s{i}", logPath=None))
        self.env.write_show(many); self.t.close()
        t = self.t = self.board(200, 60, 30)
        self.flash(); got = self.labels_by_name(); order = self.expected_order("sess-00")
        self.assertEqual(len(got), 30)
        self.assertEqual([got[n] for n in order], LABELS[:30], "26 lower-case, then A S D F for the 27th..30th card")
        self.assertEqual(got[order[26]], "A")
        t.keys("A"); time.sleep(0.3); self.assertEqual(self.focused(), [order[26]], "an upper-case label moves the focus too")


class Behaviour(Base):
    def test_a_label_moves_the_focus_and_nothing_else(self):
        self.flash(); self.t.keys("d"); time.sleep(0.4)
        self.assertEqual(self.focused(), ["charlie-30"], "d = the third nearest card: charlie-30 (c1)")
        self.assertEqual(self.env.actions(), [], "no ccmux / tmux call: the session is not jumped to"); self.assertEqual(self.env.tmux_calls(), [])
        self.assertTrue(self.t.alive()); self.assertNotIn("jump:", self.bar()); self.assertIn("j/k h/l move", self.bar())
        self.assertEqual(label_cells(self.t.snap()), [], "labels are gone"); self.assertEqual(self.env.tmux_calls(), [])
        self.t.keys("Enter"); self.assertTrue(wait(lambda: self.env.actions(), 3)); self.assertEqual(self.env.actions(), [["switch", "c1"]], "enter then jumps from the new focus")

    def test_esc_cancels_and_the_focus_stays(self):
        self.t.keys("l"); time.sleep(0.3); before = self.focused(); self.flash()
        self.t.esc(); time.sleep(0.4)
        self.assertEqual(self.focused(), before); self.assertTrue(self.t.alive()); self.assertEqual(label_cells(self.t.snap()), []); self.assertIn("j/k h/l move", self.bar())
        self.assertEqual(self.env.actions(), [])

    def test_other_characters_are_ignored_and_do_not_quit(self):
        self.flash()
        for k in ("q", "z", "1", "x", "r", "Enter", "Tab"):  # 9 cards: the labels are a..l, so none of these is one
            self.t.keys(k); time.sleep(0.2)
        self.assertTrue(self.t.alive(), "q is not a label with 9 cards and must not quit")
        self.assertIn("jump: press a label", self.bar(), "still waiting for a label")
        self.assertEqual(self.env.actions(), [], "x / r / enter did not fire")
        self.t.esc(); time.sleep(0.4); self.assertEqual(self.focused(), ["alpha-main"], "none of them moved the focus")

    def test_a_card_that_vanishes_while_labels_are_up(self):
        self.flash(); lab = self.labels_by_name()["bravo-29"]
        S = json.loads((self.env.fake / "show.json").read_text())
        self.env.write_show([s for s in S if s["id"] != "b1"])
        n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.4)
        self.t.keys(lab); time.sleep(0.5)
        self.assertTrue(self.t.alive(), "no crash on the label of a card that is gone"); self.assertEqual(self.env.actions(), [])
        self.assertEqual(len(find_cards(self.t.snap())), 8)
        if "jump:" in self.bar():
            self.t.esc(); time.sleep(0.4)
        self.t.keys("j"); time.sleep(0.3); self.assertTrue(self.t.alive(), "the board still works"); self.t.keys("q"); self.assertEqual(self.t.exit_status(), 0)

    def test_with_a_filter_only_the_visible_cards_are_labelled(self):
        self.t.keys("/"); self.t.lit("brav"); self.t.keys("Enter"); time.sleep(0.5)
        self.assertEqual(len(find_cards(self.t.snap())), 2)
        self.flash(); got = self.labels_by_name()
        self.assertEqual(got, {"bravo-29": "a", "bravo-bg": "s"}); self.assertEqual(len(label_cells(self.t.snap())), 2)
        self.t.keys("s"); time.sleep(0.4); self.assertEqual(self.focused(), ["bravo-bg"])


class Esc(Base):
    def test_esc_clears_a_kept_filter_first_and_a_second_esc_quits(self):
        self.t.keys("/"); self.t.lit("alpha"); self.t.keys("Enter"); time.sleep(0.5); self.assertEqual(len(find_cards(self.t.snap())), 3)
        self.t.esc(); time.sleep(0.5)
        self.assertTrue(self.t.alive(), "the first esc only clears the filter"); self.assertEqual(len(find_cards(self.t.snap())), 9)
        self.assertTrue(self.t.snap()[0].text.lstrip().startswith("agmux"), self.t.snap()[0].text)
        self.t.esc(); self.assertEqual(self.t.exit_status(), 0, "the second esc quits")

    def test_esc_without_a_filter_still_quits(self):
        self.t.esc(); self.assertEqual(self.t.exit_status(), 0)

    def test_esc_in_flash_cancels_the_flash_and_does_not_clear_the_filter(self):
        self.t.keys("/"); self.t.lit("alpha"); self.t.keys("Enter"); time.sleep(0.5)
        self.flash(); self.t.esc(); time.sleep(0.4)
        self.assertTrue(self.t.alive()); self.assertEqual(len(find_cards(self.t.snap())), 3, "the filter is still on")
        self.assertIn("j/k h/l move", self.bar())


class Config(Base):
    def test_flash_key_is_configurable(self):
        self.config('[keys]\nflash = ["g"]\n'); self.t.close(); self.t = self.board()
        self.assertIn("g flash", self.bar()); self.assertNotIn("s flash", self.bar())
        self.t.keys("s"); time.sleep(0.4); self.assertNotIn("jump:", self.bar(), "s is not bound any more")
        self.t.keys("g"); self.assertTrue(wait(lambda: "jump: press a label" in self.bar(), 3), self.t.text())
        self.t.keys("s"); time.sleep(0.3); self.assertEqual(self.focused(), ["bravo-29"], "s is now simply a label")

    def test_dim_colour_follows_the_muted_setting(self):
        self.config('[colors]\nmuted = "#336699"\n'); self.t.close(); self.t = self.board(); self.flash()
        snap = self.t.snap(); c = find_cards(snap)[1]
        self.assertTrue({x[1] for r in c.rows[1:4] for x in r.cells[1:-1] if x[0].strip()} == {("rgb", 51, 102, 153)}, "dimmed content uses colors.muted")


class InPassthrough(PreviewBase):
    PANES = _t12.PassthroughReview.PANES
    cat, enter_passthrough_on_a3, preview_focused = _t12.PassthroughReview.cat, _t12.PassthroughReview.enter_passthrough_on_a3, _t12.PassthroughReview.preview_focused

    def test_s_is_forwarded_to_the_pane(self):
        self.enter_passthrough_on_a3(); self.t.lit("s"); time.sleep(0.5)
        self.assertEqual(self.cat("a3"), "s"); self.assertNotIn("jump: press a label", self.t.text()); self.assertTrue(self.preview_focused())
        self.t.keys("C-]"); time.sleep(0.4); self.t.keys("s"); time.sleep(0.4)
        self.assertIn("jump: press a label", self.t.text(), "back on the cards, s starts the jump")


if __name__ == "__main__":
    unittest.main()
