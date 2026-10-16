"""T24 the pane's cursor drawn in the preview. Spec: specs/m3-config/design.md「光标 (T24)」, tasks.md T24.

The preview asks tmux, in the same call as the capture, for `#{cursor_x} #{cursor_y} #{cursor_flag} #{cursor_shape} #{pane_height}` and draws: flag 0 -> nothing; block / default -> that cell reversed
(a blank cell too); bar / underline -> that cell underlined in the focus colour; nothing while the preview is scrolled, nothing when the column is outside the preview. Panes are real tmux panes
(private server); the screen is read with `capture-pane -e` and the attributes are parsed from the raw escape sequences (lib.parse_attr).
"""
import unittest
from lib import *
from test_t11 import PreviewBase, PV_X, pane_text, CAT_CMD

FOCUS = ("rgb", 255, 142, 190)


def sh(script):
    return f"sh -c '{script}; sleep 600'"


class CursorBase(PreviewBase):
    W, H = 232, 50
    PANES = (("a1", sh("printf abc")), ("a2", sh("printf abc")), ("a3", sh("printf abc")))

    def preview_cells(self):
        """[(screen row, [cells...])] of the preview's inner rows, with attributes; cells are indexed by screen column"""
        p = self.pv(); self.assertIsNotNone(p, self.t.text())
        _, _, top, x, _ = p
        attr = parse_attr(self.t.raw())
        return [(r, attr[r]) for r in range(top + 1, len(attr)) if len(attr[r]) > x and attr[r][x][0] == "│"], x

    def marked(self, kind):
        """screen (row, col) of every cell inside the preview that is `rev` or `ul`"""
        rows, x = self.preview_cells(); out = []
        for r, cells in rows:
            for c in range(x + 1, min(len(cells), self.W - 1)):
                if (kind == "rev" and cells[c][1]) or (kind == "ul" and cells[c][2]):
                    out.append((r, c))
        return out

    def text_col(self, needle):
        """(row, col of the first character of needle) in the preview"""
        rows, x = self.preview_cells()
        for r, cells in rows:
            s = "".join(c[0] for c in cells[x + 1:])
            i = s.find(needle)
            if i >= 0:
                # walk the characters: a wide one takes two columns
                pos = x + 1
                for k, ch in enumerate(s[:i]):
                    pos += cw(ch)
                return r, pos
        return None

    def open_preview_on(self, who="a1"):
        self.press("p"); self.assertTrue(wait(lambda: self.pv() is not None, 4), self.t.text())
        time.sleep(0.9)


class Shapes(CursorBase):
    def test_a_block_cursor_reverses_the_cell_after_the_text(self):
        self.open_preview_on()
        r, c = self.text_col("abc")
        self.assertEqual(self.marked("rev"), [(r, c + 3)], "exactly one reversed cell: the 4th cell of the first line, right after abc")
        self.assertEqual(self.marked("ul"), [])

    def test_a_hidden_cursor_is_not_drawn(self):
        self.press("j"); self.panes["a2"]  # a2's pane is a plain `printf abc`; replace it with one that hides the cursor
        tm("respawn-pane", "-k", "-t", self.panes["a2"], sh("tput civis; printf abc")); time.sleep(0.8)
        self.open_preview_on()
        self.assertIsNotNone(self.text_col("abc")); self.assertEqual(self.marked("rev"), []); self.assertEqual(self.marked("ul"), [])

    def test_bar_and_underline_shapes_are_an_underline_in_the_focus_colour(self):
        for code, kind in (("6", "bar steady"), ("5", "bar blinking"), ("4", "underline steady"), ("3", "underline blinking")):
            with self.subTest(shape=kind):
                tm("respawn-pane", "-k", "-t", self.panes["a1"], sh(f'printf "\\033[{code} q"; printf abc')); time.sleep(0.8)
                if self.pv() is None:
                    self.open_preview_on()
                time.sleep(0.8)
                r, c = self.text_col("abc")
                self.assertEqual(self.marked("ul"), [(r, c + 3)], kind); self.assertEqual(self.marked("rev"), [], f"{kind}: not a block")
                cell = parse_attr(self.t.raw())[r][c + 3]; self.assertEqual(cell[3], FOCUS, "the underline is in the focus colour")

    def test_block_shapes_are_reversed(self):
        for code in ("0", "1", "2"):
            with self.subTest(code=code):
                tm("respawn-pane", "-k", "-t", self.panes["a1"], sh(f'printf "\\033[{code} q"; printf abc')); time.sleep(0.8)
                if self.pv() is None:
                    self.open_preview_on()
                time.sleep(0.8)
                r, c = self.text_col("abc"); self.assertEqual(self.marked("rev"), [(r, c + 3)]); self.assertEqual(self.marked("ul"), [])

    def test_a_blank_cell_is_drawn_too(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], sh("printf \"abc   \"")); time.sleep(0.8)
        self.open_preview_on(); r, c = self.text_col("abc")
        self.assertEqual(self.marked("rev"), [(r, c + 6)], "the cursor sits on a blank cell after trailing spaces: still drawn as a block")

    def real(self):
        o = tm("display-message", "-p", "-t", self.t.name, "#{cursor_x} #{cursor_y} #{cursor_flag}").stdout.split(); return tuple(int(v) for v in o)

    def test_the_cursor_in_the_passthrough_is_the_real_one_and_follows_the_typing(self):
        # T57: with the keys on the pane the board's own terminal cursor sits on that cell (and is shown); the cell is not drawn as a block any more (test_t57 has the rest)
        tm("respawn-pane", "-k", "-t", self.panes["a1"], CAT_CMD); time.sleep(0.8)
        self.open_preview_on(); self.assertEqual(len(self.marked("rev")), 1, "not in the pass-through: the block"); self.press("Tab"); time.sleep(0.5)
        self.t.lit("xyz"); time.sleep(0.9)
        r, c = self.text_col("xyz"); self.assertEqual(self.marked("rev"), [], "the real cursor is there instead"); self.assertEqual(self.real(), (c + 3, r, 1), "right after what was typed")
        self.t.lit("!"); time.sleep(0.9)
        r, c = self.text_col("xyz!"); self.assertEqual(self.marked("rev"), []); self.assertEqual(self.real(), (c + 4, r, 1), "and it moved with the next character (refreshed with every capture)")

    def test_nothing_is_drawn_without_a_preview(self):
        self.assertIsNone(self.pv()); self.assertNotIn("\x1b[7m \x1b[0m", self.t.raw().split("\n")[0])


class Geometry(CursorBase):
    def test_wide_characters_count_two_cells(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], f"env LANG=en_US.UTF-8 {sh(chr(112) + 'rintf 你好')}"); time.sleep(0.9)
        self.open_preview_on(); r, c = self.text_col("你好")
        self.assertEqual(self.marked("rev"), [(r, c + 4)], "two wide characters = 4 cells: the cursor is at column 4")

    def test_a_tab_expands_to_the_next_8_column_stop(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], sh("printf \"a\\tbc\"")); time.sleep(0.8)
        self.open_preview_on(); rows, x = self.preview_cells()
        r, c = self.text_col("a")
        line = "".join(ch[0] for ch in parse_attr(self.t.raw())[r][c:c + 12])
        self.assertEqual(line[:10], "a       bc", f"the tab is 7 spaces after `a` (the next stop is column 8): {line!r}")
        self.assertEqual(self.marked("rev"), [(r, c + 10)], "and the cursor (cursor_x = 10 in the pane) is right after `bc`")

    def test_the_cursor_on_the_last_line_when_the_content_fills_the_box(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], sh("seq 1 200")); time.sleep(1.2)
        self.open_preview_on()
        rows, x = self.preview_cells()
        rev = self.marked("rev"); self.assertEqual(len(rev), 1, f"one cursor cell: {rev}")
        last_row = rows[-1][0]
        self.assertEqual(rev[0][0], last_row, "the cursor is on an empty line below `200`: the bottom row of the box, even though that blank line was trimmed from the text")
        self.assertEqual(rev[0][1], x + 2, "in the first text column")
        above = "".join(c[0] for c in rows[-2][1][x + 1:]).split("│")[0].strip(); self.assertEqual(above, "200")

    def test_a_cursor_column_outside_the_preview_is_not_drawn(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], sh("printf \"%95s\" x")); time.sleep(0.8)  # the pane is 100 wide: cursor_x = 95; the preview's text is 87 cells (T61: cut, not wrapped: T56's wrapping is gone)
        self.open_preview_on(); self.assertEqual(self.marked("rev"), []); self.assertEqual(self.marked("ul"), [])

    def test_scrolling_back_hides_it_and_returning_to_the_bottom_brings_it_back(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], sh("seq 1 200")); time.sleep(1.2)
        self.open_preview_on(); self.assertEqual(len(self.marked("rev")), 1)
        self.t.wheel(PV_X + 20, 15, up=True, n=3); time.sleep(0.9)
        self.assertEqual(self.marked("rev"), [], "scrolled back: the cursor belongs to the live screen")
        self.t.wheel(PV_X + 20, 15, up=False, n=10); time.sleep(0.9)
        self.assertEqual(len(self.marked("rev")), 1, "back at the bottom")


class Following(CursorBase):
    def test_changing_the_focused_card_changes_the_cursor(self):
        tm("respawn-pane", "-k", "-t", self.panes["a2"], sh("printf abcdef")); time.sleep(0.8)
        self.open_preview_on(); r, c = self.text_col("abc"); self.assertEqual(self.marked("rev"), [(r, c + 3)])
        self.press("j"); time.sleep(1.0)
        r, c = self.text_col("abcdef"); self.assertEqual(self.marked("rev"), [(r, c + 6)])


if __name__ == "__main__":
    unittest.main()
