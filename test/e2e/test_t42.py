"""T42 the heavy rule `━━━` after a group title is drawn in the title's own colour: `colors.dir` (default ANSI 11), or the focus colour (#ff8ebe) on a focused folded title -- it was muted grey.
Spec: specs/m3-config/tasks.md「第八批」T42; design.md T40 sentence (specs/m1-mvp/design.md follows)."""
import unittest
from lib import *
from test_t3 import C_FOCUS
from test_t40 import Base as _B40, titles

DIR_DEFAULT = ("idx", 11)
MUTED = (98, 114, 164)


def near(a, b, d=3): return a is not None and a[0] == "rgb" and all(abs(x - y) <= d for x, y in zip(a[1:], b))


def runs(row, c):
    """(mark colour, text colour, rule colours, rule width) of the title that starts at cell c: mark = ▾/▸, text = its first path char, the ━ run after the text"""
    cells = row.cells; j = c
    while j < len(cells) and cells[j][0] != "━": j += 1
    k = j
    while k < len(cells) and cells[k][0] == "━": k += 1
    return cells[c][1], cells[c + 2][1], {cells[x][1] for x in range(j, k)}, k - j


class Rule(_B40):
    def test_default_rule_is_the_dir_colour_like_the_title(self):
        t = self.board(many=False, w=200, h=50); seen = 0
        for r, c, row in titles(t.snap()):
            mark, text, rule, n = runs(row, c)
            if not n: continue  # the one that fills its column
            self.assertEqual(rule, {mark}, f"one colour for the whole rule, the same as the title mark: {row.text[c:c + 40]!r}"); self.assertEqual(text, mark, "path text and mark share it")
            self.assertEqual(rgb(mark), rgb(DIR_DEFAULT), "colors.dir default (ANSI 11)"); self.assertNotEqual(rgb(mark), MUTED); seen += 1
        self.assertGreaterEqual(seen, 3)

    def test_colors_dir_moves_the_rule_with_the_title(self):
        self.config('[colors]\ndir = "#ff8800"\n'); t = self.board(many=False, w=200, h=50); seen = 0
        for r, c, row in titles(t.snap()):
            mark, text, rule, n = runs(row, c)
            if not n: continue
            self.assertTrue(rule == {mark} and near(mark, (255, 136, 0)), f"{mark} {rule}"); seen += 1
        self.assertGreaterEqual(seen, 3)

    def test_the_count_stays_muted_between_title_and_rule(self):
        t = self.board(many=False, w=200, h=50); r, c, row = next(x for x in titles(t.snap()) if "━" in x[2].text); cells = row.cells
        i = next(k for k in range(c, len(cells)) if cells[k][0] == "("); self.assertTrue(near(cells[i][1], MUTED), f"the (n) count is still muted: {cells[i][1]}")

    def test_a_folded_focused_title_has_a_focus_coloured_rule_and_an_unfocused_folded_one_is_dir(self):
        t = self.board(many=False, w=200, h=50); t.keys("f"); time.sleep(0.4); t.keys("l"); time.sleep(0.4); t.keys("f"); time.sleep(0.5); folded = []  # fold the first group, move to the next column, fold that one
        for r, c, row in titles(t.snap()):
            if row.cells[c][0] == "▸": folded.append((row.text[c:c + 14],) + runs(row, c))
        self.assertEqual(len(folded), 2, f"two folded groups: {folded}"); focused = [f for f in folded if f[1] == C_FOCUS]; other = [f for f in folded if f[1] != C_FOCUS]
        self.assertEqual(len(focused), 1, folded); self.assertEqual((focused[0][2], focused[0][3]), (C_FOCUS, {C_FOCUS}), "the focused folded title: mark, text and rule in the focus colour")
        self.assertEqual((other[0][3], rgb(other[0][1])), ({other[0][1]}, rgb(DIR_DEFAULT)), "the other folded title keeps dir, rule included"); self.assertGreater(other[0][4], 0); self.assertGreater(focused[0][4], 0)


if __name__ == "__main__":
    unittest.main()
