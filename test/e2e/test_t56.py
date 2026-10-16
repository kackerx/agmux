"""T56 (as T61 left it) the preview shows every pane line cut to the box's inner width -- no wrapping: a Claude Code table / input box keeps its shape, a long line has no second row. A wide character at the
edge is dropped whole, a tab is expanded to 8 columns first, the rows shown are the last N lines of the pane, the wheel moves 3 lines. What T56 fixed on the way stays: the pane's colours carry over the pane's own lines
(tmux `capture-pane -e` does not repeat an SGR that did not change, a line break included) even when the colour was opened in the part of a line that is cut off on the right or in lines cut off at the top; a line whose
background is not reset ends at its text: the padding up to the border has no background. Spec: specs/m3-config/tasks.md T56 / T61. Real tmux panes (private server), the screen read with `capture-pane -e`.

Geometry (read off the screen, not assumed): at 232 columns the box is 91 wide at column 141, 89 cells between the borders: one blank cell each side + 87 for the text (CW)."""
import unittest
from lib import *
from test_t11 import PreviewBase, PV_X, pane_text, CAT_CMD


def sh(script): return f"sh -c '{script}; sleep 600'"


def digits(n): return ("".join(f"{i:03d}." for i in range(n // 4 + 1)))[:n]  # 000.001.002. ...: every position is told by the text itself


def box(snap, w):
    """(x of the box's left border, top row index of the box's first inner row, inner rows as Row objects) of the preview, or None. The box is the one whose top-right corner is the terminal's last column."""
    for r in range(1, len(snap) - 3):
        row = snap[r + 1]
        if len(row.cells) == w and row.cells[-1][0] == "╮":
            x = max(i for i, c in enumerate(row.cells) if c[0] == "╭"); rows = []
            for rr in range(r + 2, len(snap)):
                cells = snap[rr].cells
                if len(cells) == w and cells[x][0] == "│" and cells[-1][0] == "│": rows.append(Row(cells[x + 1:-1]))
                else: break
            return x, r + 2, rows
    return None


CW = 87  # the text cells of the preview at 232 columns


class Cut(PreviewBase):
    W, H = 232, 50
    PANES = (("a1", sh("printf abc")), ("a2", sh("printf abc")), ("a3", sh("printf abc")))

    def pane(self, script, width=150, sid="a1"):
        tm("resize-window", "-t", f"pane-{sid}", "-x", width); tm("respawn-pane", "-k", "-t", self.panes[sid], sh(script)); time.sleep(0.9)

    def show(self, t=None):
        t = t or self.t; t.keys("p"); self.assertTrue(wait(lambda: box(t.snap(), t.w) is not None, 4), t.text()); time.sleep(1.0)
        return box(t.snap(), t.w)

    def content(self, rows):
        """what the box says, row by row: the text between the blank pad cells, right-trimmed"""
        return [r.width_text(1, len(r.cells) - 1).rstrip() for r in rows]


class Geometry(Cut):
    def test_a_line_wider_than_the_box_is_cut_and_has_no_second_row(self):
        line = digits(150); self.pane(f"printf \"%s\\nnext\\n\" {line}"); x, top, rows = self.show(); self.assertEqual(len(rows[0].cells) - 2, CW, "89 cells between the borders at 232 columns")
        out = self.content(rows); i = out.index(line[:CW]); self.assertEqual(out[i + 1], "next", f"the row after the long line is the next line of the pane: {out[i:i + 3]}"); self.assertNotIn(line[CW:CW + 8], "\n".join(out), "what was cut is nowhere")
        for r in rows: self.assertEqual((r.cells[0][0], r.cells[-1][0]), (" ", " "), "the pad cells stay blank: the text never touches the border")

    def test_the_rows_shown_are_the_last_lines_of_the_pane_one_row_each(self):
        n = 120; self.pane("i=1; while [ $i -le %d ]; do printf \"%%03d %%s\\n\" $i \"$(printf %%0100d 0)\"; i=$((i+1)); done" % n); x, top, rows = self.show(); out = self.content(rows); want = [(f"{i:03d} " + "0" * 100)[:CW] for i in range(1, n + 1)]
        self.assertEqual(len(rows), 44); tail = [c for c in out if c]; self.assertEqual(tail, want[len(want) - len(tail):], "the end of the pane, one row per line"); self.assertEqual(out[-2], want[-1]); self.assertEqual(out[-1], "", "the cursor's row")

    def test_a_wide_character_that_does_not_fit_is_dropped_whole(self):
        # `ab` + wide characters: 2 + 2 * 42 = 86 cells fit, the 43rd would end at cell 88: it goes, the cell stays blank
        tm("resize-window", "-t", "pane-a1", "-x", 200); tm("respawn-pane", "-k", "-t", self.panes["a1"], "env LANG=en_US.UTF-8 sh -c 'printf ab; i=0; while [ $i -lt 60 ]; do printf 你; i=$((i+1)); done; printf \"\\nnext\\n\"; sleep 600'"); time.sleep(0.9)
        x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("ab你")); self.assertEqual(out[i], "ab" + "你" * 42); self.assertEqual(sum(cw(ch) for ch in out[i]), 86)
        self.assertEqual(out[i + 1], "next", "no second row"); self.assertEqual(rows[i].cells[-1][0], " ", "no half character reaches the pad cell"); self.assertNotIn("\ufffd", "".join(out))

    def test_a_tab_is_expanded_to_eight_columns_before_the_cut(self):
        self.pane("printf \"ab\\t%s\\n\" " + digits(100)); x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("ab")); self.assertTrue(out[i].startswith("ab      000.001."), repr(out[i])); self.assertEqual(len(out[i]), CW)

    def test_short_lines_and_blank_lines_are_not_touched(self):
        self.pane("printf \"one\\n\\ntwo\\n\""); x, top, rows = self.show(); out = self.content(rows); i = out.index("one"); self.assertEqual(out[i:i + 3], ["one", "", "two"])

    def test_a_line_of_exactly_the_inner_width_and_one_more(self):
        a, b = digits(CW), digits(CW + 1); self.pane(f"printf \"%s\\n%s\\nnext\\n\" {a} {b}"); x, top, rows = self.show(); out = self.content(rows); i = out.index(a); self.assertEqual(out[i + 1], b[:CW], "one column over: the last character is cut"); self.assertEqual(out[i + 2], "next")

    def test_a_box_drawing_table_wider_than_the_preview_keeps_its_left_side_row_by_row(self):
        # the shape Claude Code's tables have: the same left part on every row, nothing shifted by a wrap
        W = 150; lines = ["┌" + "─" * 30 + "┬" + "─" * 60 + "┬" + "─" * 55 + "┐", "│ 名称 (wide)" + " " * 18 + "│ " + "description " * 4 + " " * 12 + "│ " + "x" * 53 + " │", "├" + "─" * 30 + "┼" + "─" * 60 + "┼" + "─" * 55 + "┤",
                                       "│ row two" + " " * 22 + "│ " + "y" * 58 + "│ " + "z" * 53 + " │", "└" + "─" * 30 + "┴" + "─" * 60 + "┴" + "─" * 55 + "┘"]
        lines = [l + " " * (W - sum(cw(ch) for ch in l)) if sum(cw(ch) for ch in l) < W else l for l in lines]
        tm("resize-window", "-t", "pane-a1", "-x", 200); tm("respawn-pane", "-k", "-t", self.panes["a1"], "env LANG=en_US.UTF-8 sh -c 'cat <<EOT\n" + "\n".join(lines) + "\nEOT\nsleep 600'"); time.sleep(0.9)
        x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("┌"))
        def cut(l, n):
            o, w = "", 0
            for ch in l:
                if w + cw(ch) > n: break
                o += ch; w += cw(ch)
            return o
        self.assertEqual(out[i:i + 5], [cut(l, CW).rstrip() for l in lines], "each row is the left 87 cells of the original, one row each")


class Colour(Cut):
    def test_a_coloured_long_line_is_one_colour_to_the_edge_and_has_no_continuation(self):
        line = digits(150); self.pane("printf \"\\033[38;2;255;100;0m%s\\033[0m\\nplain\\n\" " + line); x, top, rows = self.show(); out = self.content(rows); i = out.index(line[:CW])
        cs = [c for c in rows[i].cells[1:-1] if c[0].strip()]; self.assertEqual("".join(c[0] for c in cs), line[:CW]); self.assertEqual({c[1] for c in cs}, {("rgb", 255, 100, 0)}); self.assertEqual(out[i + 1], "plain")
        nxt = [c for c in rows[i + 1].cells if c[0].strip()]; self.assertNotEqual({c[1] for c in nxt}, {("rgb", 255, 100, 0)}, "reset on the long line's end")

    def test_a_colour_change_inside_the_visible_part_lands_on_the_right_character(self):
        line = digits(150); self.pane("printf \"\\033[38;2;255;0;0m%s\\033[38;2;0;255;0m%s\\033[0m\\n\" " + f"{line[:50]} {line[50:]}"); x, top, rows = self.show(); out = self.content(rows); i = out.index(line[:CW]); cs = [c for c in rows[i].cells[1:-1] if c[0].strip()]
        self.assertEqual([c[1] for c in cs[:50]], [("rgb", 255, 0, 0)] * 50); self.assertEqual([c[1] for c in cs[50:]], [("rgb", 0, 255, 0)] * (CW - 50))

    def test_a_colour_opened_in_the_cut_off_part_of_a_line_carries_to_the_next_line(self):
        # line 1: red up to column 100, green from column 100 (outside the box) and never reset; line 2: `second` -- tmux writes no SGR for it, it is green
        line = digits(150); self.pane("printf \"\\033[38;2;255;0;0m%s\\033[38;2;0;255;0m%s\\nsecond\\n\\033[0mthird\\n\" " + f"{line[:100]} {line[100:]}"); x, top, rows = self.show(); out = self.content(rows); i = out.index(line[:CW]); GREEN, RED = ("rgb", 0, 255, 0), ("rgb", 255, 0, 0)
        self.assertEqual({c[1] for c in rows[i].cells[1:-1] if c[0].strip()}, {RED}, "the visible part of line 1 is red"); self.assertEqual(out[i + 1], "second"); self.assertEqual({c[1] for c in rows[i + 1].cells[1:-1] if c[0].strip()}, {GREEN}, "the green opened where it was cut off")
        self.assertEqual(out[i + 2], "third"); self.assertNotIn(GREEN, {c[1] for c in rows[i + 2].cells[1:-1] if c[0].strip()}, "reset")

    def test_the_colour_does_not_reach_the_border_nor_the_pad(self):
        self.pane("printf \"\\033[38;2;255;100;0m%s\\033[0m\\n\" " + digits(150)); x, top, rows = self.show(); snap = self.t.snap()
        for r in range(top, top + 3): self.assertNotEqual(snap[r].cells[x][1], ("rgb", 255, 100, 0), "left border keeps its own colour"); self.assertNotEqual(snap[r].cells[-1][1], ("rgb", 255, 100, 0), "right border")

    def test_a_background_that_is_never_reset_ends_at_the_text_and_carries_to_the_next_lines(self):
        line = digits(150); self.pane("printf \"\\033[41mshort\\n%s\\nred3\\n\\033[0mplain\\n\" " + line); x, top, rows = self.show(); RED = rgb(("idx", 1)); red = lambda c: c[2] is not None and rgb(c[2]) == RED
        out = self.content(rows); i = out.index("short")
        for k, want in ((0, "short"), (1, line[:CW]), (2, "red3")):
            n = len(out[i + k]); self.assertEqual(out[i + k], want); self.assertTrue(all(red(c) for c in rows[i + k].cells[1:1 + n]), f"{want!r}: the text is on red"); self.assertFalse(any(red(c) for c in rows[i + k].cells[1 + n:]), f"{want!r}: after the text no background up to the border")
        self.assertEqual(out[i + 3], "plain"); self.assertFalse(any(red(c) for c in rows[i + 3].cells)); snap = self.t.snap(); self.assertFalse(any(red(snap[top + i + k].cells[-1]) for k in range(4)), "the border is not red")

    def test_a_colour_opened_above_the_rows_that_are_shown_still_holds_at_the_first_shown_row(self):
        self.pane("printf \"\\033[44m\"; seq 1 200"); x, top, rows = self.show(); BLUE = rgb(("idx", 4)); out = self.content(rows)
        self.assertEqual(out[-2], "200"); self.assertTrue(next(i for i, c in enumerate(out) if c) < 3, out[:3])
        for i, r in enumerate(rows):
            n = len(out[i])
            if n: self.assertTrue(all(c[2] is not None and rgb(c[2]) == BLUE for c in r.cells[1:1 + n]), f"row {i} ({out[i]!r}) is on blue")

    def test_a_reset_in_the_middle_clears_it_for_the_lines_after(self):
        self.pane("printf \"\\033[44mblue1\\nblue2\\033[0m\\nafter\\n\""); x, top, rows = self.show(); BLUE = rgb(("idx", 4)); out = self.content(rows); i = out.index("blue1"); isb = lambda c: c[2] is not None and rgb(c[2]) == BLUE
        self.assertTrue(all(isb(c) for c in rows[i].cells[1:6])); self.assertTrue(all(isb(c) for c in rows[i + 1].cells[1:6])); self.assertEqual(out[i + 2], "after"); self.assertFalse(any(isb(c) for c in rows[i + 2].cells))


class Cursor(Cut):
    PANES = (("a1", sh("printf abc")), ("a2", sh("printf abc")), ("a3", CAT_CMD))

    def marked(self, kind="rev"):
        x, top, rows = box(self.t.snap(), self.t.w); a = parse_attr(self.t.raw()); out = []
        for r in range(top, top + len(rows)):
            for c in range(x + 1, self.t.w - 1):
                if c < len(a[r]) and ((kind == "rev" and a[r][c][1]) or (kind == "ul" and a[r][c][2])): out.append((r, c))
        return out

    def test_a_cursor_inside_the_box_is_drawn_on_the_cut_line(self):
        self.pane("printf %s " + digits(60)); x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("000.")); self.assertEqual(self.marked(), [(top + i, x + 2 + 60)]); self.assertEqual(self.marked("ul"), [])

    def test_the_last_cell_of_the_box_still_takes_the_cursor_and_the_one_after_it_does_not(self):
        self.pane("printf %s " + digits(CW - 1)); x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("000.")); self.assertEqual(self.marked(), [(top + i, x + 2 + CW - 1)], "cursor_x = 86: the 87th cell")
        self.t.keys("p"); time.sleep(0.4); self.pane("printf %s " + digits(CW)); x, top, rows = self.show(); self.assertEqual(self.marked(), [], "cursor_x = 87: past the last cell: nothing is drawn"); self.assertEqual(self.marked("ul"), [])

    def test_a_cursor_past_the_box_width_is_not_drawn_not_wrapped(self):
        self.pane("printf %s " + digits(100)); x, top, rows = self.show(); self.assertEqual(self.marked(), []); self.assertEqual(self.marked("ul"), []); out = self.content(rows); self.assertEqual(len([c for c in out if c.startswith("000.")]), 1)

    def test_an_underline_shape_cursor_inside_the_box_is_an_underline(self):
        self.pane("printf \"\\033[4 q\"; printf %s " + digits(60)); x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("000.")); self.assertEqual(self.marked("ul"), [(top + i, x + 2 + 60)]); self.assertEqual(self.marked("rev"), [])

    def test_the_cursor_hidden_pane_draws_nothing(self):
        self.pane("printf \"\\033[?25l\"; printf %s " + digits(60)); x, top, rows = self.show(); self.assertEqual(self.marked(), []); self.assertEqual(self.marked("ul"), [])


class Scroll(Cut):
    def test_one_wheel_notch_moves_three_lines_of_the_pane(self):
        n = 100; self.pane("i=1; while [ $i -le %d ]; do printf \"%%03d %%s\\n\" $i \"$(printf %%0100d 0)\"; i=$((i+1)); done" % n); x, top, rows = self.show(); want = [(f"{i:03d} " + "0" * 100)[:CW] for i in range(1, n + 1)]
        H = len(rows); get = lambda: self.content(box(self.t.snap(), self.t.w)[2]); self.assertEqual(get(), want[len(want) - (H - 1):] + [""], "at the bottom: the end of the text and the cursor's row"); done = 0
        for k in (1, 2, 5):
            self.t.wheel(x + 20, top + 10, up=True, n=k - done); done = k; time.sleep(0.9); self.assertEqual(get(), want[len(want) - 3 * k - H:len(want) - 3 * k], f"{k} notch(es) back: {3 * k} lines")
        self.t.wheel(x + 20, top + 10, up=False, n=5); time.sleep(0.9); self.assertEqual(get(), want[len(want) - (H - 1):] + [""], "and back down to the bottom")

    def test_scrolling_stops_at_the_top_of_the_pane_history(self):
        self.pane("i=1; while [ $i -le 30 ]; do printf \"%03d %s\\n\" $i \"$(printf %0100d 0)\"; i=$((i+1)); done"); x, top, rows = self.show()
        self.t.wheel(x + 20, top + 10, up=True, n=200); time.sleep(1.2); out = self.content(box(self.t.snap(), self.t.w)[2]); first = [c for c in out if c][0]
        self.assertTrue(first.startswith("001 "), f"the top of the pane's history is the first row: {first!r}"); self.t.wheel(x + 20, top + 10, up=True, n=20); time.sleep(0.6); self.assertEqual(self.content(box(self.t.snap(), self.t.w)[2]), out, "no further"); self.assertTrue(self.t.alive())


if __name__ == "__main__":
    unittest.main()
