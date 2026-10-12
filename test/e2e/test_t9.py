"""T9 spatial hjkl navigation. Spec: specs/m1-mvp/design.md「交互」, specs/m1-mvp/tasks.md T9.

j/k: the card with the largest HORIZONTAL overlap below / above, crossing groups and bands, no fallback (stop at the edge).
h/l: the card with the largest VERTICAL overlap on the left / right; when nothing overlaps in that direction, fall back to the nearest column
there and its vertically nearest card. A folded group's title takes part as a one-row card.
The focused card is the one with the #ff8ebe (thick) border, so the tests read focus from the screen and never need to press enter.

Fixture layout at 120 columns (2 columns of 59), groups sorted by cwd:
  band 1: G1 a1 a2 a3 | G2 b1 b2     band 2: G3 c1 c2 | G4 d1     band 3: G5 e1
At 200 columns (4 columns of 48): band 1: G1 G2 G3 G4, band 2: G5.
"""
import unittest
from lib import *
from test_t3 import ready, by_name, group_cards, NAMES, C_FOCUS
from test_t7 import sid_of, structure


def focus_sid(term):
    cs = [c for c in find_cards(term.snap()) if c.ok]
    f = [c for c in cs if c.rows[0].cells[0][1] == C_FOCUS]
    assert len(f) == 1, f"{len(f)} cards with the focus border:\n{term.text()}"
    return sid_of(f[0])


class Base(unittest.TestCase):
    W = 120

    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.t = Term(self.env, w=self.W, h=100); self.addCleanup(self.t.close)
        self.assertTrue(ready(self.t), self.t.text())

    def press(self, *keys):
        for k in keys:
            self.t.keys(k); time.sleep(0.22)

    def walk(self, steps, start="a1"):
        self.assertEqual(focus_sid(self.t), start, "initial focus")
        for k, want in steps:
            self.press(k)
            self.assertEqual(focus_sid(self.t), want, f"after {k!r} (expected {want})")


class TwoColumns(Base):
    W = 120

    def test_layout_is_two_columns_three_bands(self):
        n, colW = layout(self.W)
        self.assertEqual((n, colW), (2, 59))
        titles, cards = structure(self.t)
        self.assertEqual([g[2] for g in titles], [["a1", "a2", "a3"], ["b1", "b2"], ["c1", "c2"], ["d1"], ["e1"]])
        self.assertEqual(sorted({c[1] for c in cards}), [0, 61])

    def test_j_walks_the_whole_left_column_across_bands_and_k_walks_back(self):
        self.walk([("j", "a2"), ("j", "a3"), ("j", "c1"), ("j", "c2"), ("j", "e1"), ("j", "e1"),
                   ("k", "c2"), ("k", "c1"), ("k", "a3"), ("k", "a2"), ("k", "a1"), ("k", "a1")])

    def test_j_stays_in_the_right_column_it_never_jumps_to_the_left_one(self):
        self.walk([("l", "b1"), ("j", "b2"), ("j", "d1"), ("j", "d1"), ("k", "b2"), ("k", "b1"), ("k", "b1")])

    def test_l_and_h_pick_the_largest_vertical_overlap(self):
        self.walk([("l", "b1"), ("h", "a1"), ("j", "a2"), ("l", "b2"), ("h", "a2")])

    def test_l_falls_back_to_the_vertically_nearest_card_when_nothing_overlaps(self):
        self.walk([("j", "a2"), ("j", "a3"), ("l", "b2"),  # a3 has no neighbour at its height: nearest card of the right column is b2
                   ("h", "a2")])  # asymmetric on purpose: from b2 the left card with the largest overlap is a2, not a3

    def test_band_two_and_three(self):
        self.walk([("j", "a2"), ("j", "a3"), ("j", "c1"), ("l", "d1"), ("h", "c1"), ("j", "c2"), ("l", "d1"), ("h", "c1"),
                   ("j", "c2"), ("j", "e1"), ("l", "d1"), ("h", "c1")])

    def test_edges_stop(self):
        self.walk([("h", "a1"), ("k", "a1"), ("l", "b1"), ("l", "b1"), ("k", "b1")])

    def test_arrow_keys_are_the_same(self):
        self.walk([("Down", "a2"), ("Right", "b2"), ("Down", "d1"), ("Left", "c1"), ("Up", "a3"), ("Left", "a3")])

    def test_enter_after_a_walk_switches_to_the_focused_card(self):
        self.press("j", "j", "j", "l")  # c1 -> d1
        self.t.keys("Enter"); wait(lambda: self.env.calls(), 3)
        self.assertEqual(self.env.calls(), [["switch", "d1"]])

    def test_focus_survives_polls_and_resort(self):
        self.press("j", "j", "j")  # c1
        n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.3)
        self.assertEqual(focus_sid(self.t), "c1")


class FourColumns(Base):
    W = 200

    def test_l_walks_the_first_band_and_stops(self):
        self.walk([("l", "b1"), ("l", "c1"), ("l", "d1"), ("l", "d1"), ("h", "c1"), ("h", "b1"), ("h", "a1"), ("h", "a1")])

    def test_j_does_not_retreat_into_another_column(self):
        self.walk([("l", "b1"), ("j", "b2"), ("j", "b2"), ("k", "b1")])  # nothing under b2 in column 2: stay
        self.walk_from(["l", "l", "l"], "d1", [("j", "d1")])

    def walk_from(self, pre, expect, steps):
        self.press(*pre); self.assertEqual(focus_sid(self.t), expect)
        for k, want in steps:
            self.press(k); self.assertEqual(focus_sid(self.t), want, f"after {k!r}")

    def test_second_band_to_the_right_falls_back_and_k_returns(self):
        self.walk([("j", "a2"), ("j", "a3"), ("j", "e1"), ("l", "b2"),  # e1 (band 2, left) has nothing at its height on the right: b2 is the vertically nearest
                   ("h", "a2")])
        self.walk_from(["j", "j"], "e1", [("k", "a3")])  # from a2 (focus is a2 now): j j -> a3 -> e1, then k back to a3


class Folded(Base):
    W = 120

    def test_a_folded_title_is_a_one_row_card_in_the_walk(self):
        self.press("j", "j", "j")  # c1
        self.t.keys("f"); time.sleep(0.4)  # fold G3: c1 / c2 disappear, a title row remains
        self.assertTrue(wait(lambda: [g[0][0] for g in structure(self.t)[0]][2] == "▸", 3))
        self.press("k")
        self.assertEqual(focus_sid(self.t), "a3", "k from the folded title goes up to a3")
        self.press("j")
        # the folded title has no border to read: it is focused when enter unfolds it instead of switching
        self.t.keys("Enter")
        self.assertTrue(wait(lambda: [g[0][0] for g in structure(self.t)[0]][2] == "▾", 3), "j from a3 lands on the folded title; enter unfolds it")
        self.assertEqual(self.env.calls(), [], "no ccmux call")

    def test_j_passes_through_a_folded_title_to_the_next_card(self):
        self.press("j", "j", "j"); self.t.keys("f"); time.sleep(0.4)  # focus on the folded G3 title
        self.press("j")
        self.assertEqual(focus_sid(self.t), "e1", "j from the folded title goes down to e1")
        self.press("k", "k")
        self.assertEqual(focus_sid(self.t), "a3", "k twice: title, then a3")

    def test_l_from_a_folded_title_does_not_get_stuck(self):
        self.press("j", "j", "j"); self.t.keys("f"); time.sleep(0.4)
        self.press("l")
        self.assertEqual(focus_sid(self.t), "d1", "the title row has no vertical overlap with d1: the fallback picks the nearest card of the right column")
        self.press("h"); time.sleep(0.2)
        self.t.keys("Enter")  # back on the folded title? enter unfolds it, it must not call ccmux
        self.assertTrue(wait(lambda: [g[0][0] for g in structure(self.t)[0]][2] == "▾", 3), "h from d1 returns to the folded title")
        self.assertEqual(self.env.calls(), [])


if __name__ == "__main__":
    unittest.main()
