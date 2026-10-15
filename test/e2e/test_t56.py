"""T56 the preview wraps long pane lines: every line inside the box is hard-wrapped at the box's inner width (a wide character is never cut in half, a tab is expanded to 8 columns first), the rows shown are the last N
rows after wrapping; the wheel scrolls by wrapped rows; the cursor (block / underline) lands on the wrapped row and column; the pane's colours carry over the wrapped rows and over the pane's own lines (tmux capture -e does
not repeat an SGR that did not change, a line break included; a colour opened in lines that are cut off at the top still holds at the first row shown); a line whose background is not reset ends at its text: the
padding up to the border has no background. Spec: specs/m3-config/tasks.md T56. Real tmux panes (private server), the screen read with `capture-pane -e`.

Geometry (read off the screen, not assumed): at 232 columns the box is 91 wide at column 141, 89 cells between the borders: one blank cell each side + 87 for the text (CW). At 100 columns (cards 40, box from 41) it is 55."""
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


class Wrap(PreviewBase):
    W, H = 232, 50
    PANES = (("a1", sh("printf abc")), ("a2", sh("printf abc")), ("a3", sh("printf abc")))

    def pane(self, script, width=150, sid="a1"):
        tm("resize-window", "-t", f"pane-{sid}", "-x", width); tm("respawn-pane", "-k", "-t", self.panes[sid], sh(script)); time.sleep(0.9)

    def show(self, t=None, rows_wanted=None):
        t = t or self.t; t.keys("p"); self.assertTrue(wait(lambda: box(t.snap(), t.w) is not None, 4), t.text()); time.sleep(1.0)
        return box(t.snap(), t.w)

    def content(self, rows):
        """what the box says, row by row: the text between the blank pad cells, right-trimmed"""
        return [r.width_text(1, len(r.cells) - 1).rstrip() for r in rows]


class Geometry(Wrap):
    def test_a_line_wider_than_the_box_is_wrapped_and_its_tail_is_there(self):
        line = digits(150); self.pane(f"printf \"%s\\n\" {line}"); x, top, rows = self.show(); inner = len(rows[0].cells) - 2; self.assertEqual(inner, 87, "89 cells between the borders at 232 columns")
        out = [c for c in self.content(rows) if c]; self.assertEqual(len(out), 2, f"150 columns at 87 a row = 2 rows: {out}"); self.assertEqual("".join(out), line, "nothing lost, nothing doubled")
        self.assertEqual(len(out[0]), inner, "the first row is full"); self.assertTrue(out[1].endswith(line[-6:]), "the tail of the line is visible")
        for r in rows: self.assertEqual((r.cells[0][0], r.cells[-1][0]), (" ", " "), "the pad cells stay blank: the text never touches the border")

    def test_pane_150_and_preview_55_at_100_columns(self):
        line = digits(150); self.pane(f"printf \"%s\\n\" {line}"); t = self.board(100, 50); t.keys("p"); self.assertTrue(wait(lambda: box(t.snap(), 100) is not None, 4), t.text()); time.sleep(1.0)
        x, top, rows = box(t.snap(), 100); inner = len(rows[0].cells) - 2; self.assertEqual((x, inner), (41, 55), "the geometry the 100-column board gives the preview")
        out = [c for c in self.content(rows) if c]; self.assertEqual(len(out), 3, f"ceil(150 / 55) = 3 rows: {out}"); self.assertEqual("".join(out), line); self.assertEqual([len(c) for c in out], [55, 55, 40])
        self.assertTrue(out[-1].endswith(line[-6:]))

    def test_the_rows_shown_are_the_last_n_after_wrapping(self):
        n = 120; self.pane("i=1; while [ $i -le %d ]; do printf \"%%03d %%s\\n\" $i \"$(printf %%0100d 0)\"; i=$((i+1)); done" % n); x, top, rows = self.show(); out = self.content(rows)
        self.assertEqual(len(rows), 44, "the box height as without wrapping"); self.assertTrue(out[-1] == "" and out[-2].startswith("0"), out[-3:])
        # every logical line is 2 rows (104 columns at 87): the first row shown is a row, of line n - 21 or n - 22, and the rows are in order
        want = []
        for i in range(1, n + 1): want += [f"{i:03d} " + "0" * 83, "0" * 17]  # 104 columns: 87 + 17
        tail = [c for c in out if c]; self.assertEqual(tail, want[len(want) - len(tail):], "the box is the end of the wrapped text, row for row"); self.assertEqual(out[-2:-1], want[-1:], "ending with the last row of the last line")

    def test_a_wide_character_is_never_cut_in_half(self):
        # `a`, then 90 two-cell characters = 181 cells: row 1 takes `a` + 43 of them (87 cells), row 2 takes 43 (86 cells: the 44th would end at cell 88), row 3 the last 4
        self.pane("printf a; i=0; while [ $i -lt 90 ]; do printf 你; i=$((i+1)); done; printf \"\\n\"", width=200); time.sleep(0.3)
        tm("respawn-pane", "-k", "-t", self.panes["a1"], "env LANG=en_US.UTF-8 sh -c 'printf a; i=0; while [ $i -lt 90 ]; do printf 你; i=$((i+1)); done; printf \"\\n\"; sleep 600'"); time.sleep(0.9)
        x, top, rows = self.show(); out = [c for c in self.content(rows) if c]; self.assertEqual(len(out), 3, out)
        self.assertEqual("".join(out), "a" + "你" * 90, "every character whole, none doubled"); self.assertEqual([sum(cw(ch) for ch in c) for c in out], [87, 86, 8], "a 2-cell character that would cross the edge goes to the next row")
        for r in rows[:3]: self.assertEqual(r.cells[-1][0], " ", "no half character reaches the pad cell")

    def test_a_tab_is_expanded_to_eight_columns_before_the_wrap(self):
        self.pane("printf \"ab\\t%s\\n\" " + digits(100)); x, top, rows = self.show(); out = [c for c in self.content(rows) if c]
        self.assertTrue(out[0].startswith("ab      000.001."), f"ab + 6 blanks to column 8: {out[0]!r}"); self.assertEqual(len(out[0]), 87); self.assertEqual("".join(out)[8:], digits(100)); self.assertEqual(len(out), 2)

    def test_short_lines_and_blank_lines_are_not_touched(self):
        self.pane("printf \"one\\n\\ntwo\\n\""); x, top, rows = self.show(); out = self.content(rows); i = out.index("one"); self.assertEqual(out[i:i + 3], ["one", "", "two"])

    def test_a_line_of_exactly_the_inner_width_is_one_row(self):
        line = digits(87); self.pane(f"printf \"%s\\nnext\\n\" {line}"); x, top, rows = self.show(); out = self.content(rows); i = out.index(line); self.assertEqual(out[i + 1], "next", "no empty row after a full row")

    def test_a_line_one_over_the_width_wraps_its_last_character(self):
        line = digits(88); self.pane(f"printf \"%s\\nnext\\n\" {line}"); x, top, rows = self.show(); out = self.content(rows); i = out.index(line[:87]); self.assertEqual(out[i + 1:i + 3], [line[87:], "next"])


class Colour(Wrap):
    def test_a_coloured_long_line_is_one_colour_in_every_row(self):
        line = digits(150); self.pane("printf \"\\033[38;2;255;100;0m%s\\033[0m\\nplain\\n\" " + line); x, top, rows = self.show()
        cs = [c for r in rows[:2] for c in r.cells[1:-1] if c[0].strip()]; self.assertEqual("".join(c[0] for c in cs), line)
        self.assertEqual({c[1] for c in cs}, {("rgb", 255, 100, 0)}, "the second half of the line has the colour of the first")
        plain = [c for r in rows for c in r.cells[1:-1] if c[0] != " "]; nxt = [c for c in rows[2].cells if c[0].strip()]; self.assertEqual("".join(c[0] for c in nxt), "plain"); self.assertNotEqual({c[1] for c in nxt}, {("rgb", 255, 100, 0)})

    def test_a_colour_change_inside_the_line_lands_on_the_right_character(self):
        line = digits(150); pre, mid = line[:100], line[100:]
        self.pane("printf \"\\033[38;2;255;0;0m%s\\033[38;2;0;255;0m%s\\033[0m\\n\" " + f"{pre} {mid}"); x, top, rows = self.show(); cs = [c for r in rows[:2] for c in r.cells[1:-1] if c[0].strip()]
        self.assertEqual("".join(c[0] for c in cs), line)
        self.assertEqual([c[1] for c in cs[:100]], [("rgb", 255, 0, 0)] * 100, "red up to character 100 (the row break is at 87)"); self.assertEqual([c[1] for c in cs[100:]], [("rgb", 0, 255, 0)] * 50)

    def test_the_colour_does_not_reach_the_border_nor_the_pad(self):
        self.pane("printf \"\\033[38;2;255;100;0m%s\\033[0m\\n\" " + digits(150)); x, top, rows = self.show(); snap = self.t.snap()
        for r in range(top, top + 3): self.assertNotEqual(snap[r].cells[x][1], ("rgb", 255, 100, 0), "left border keeps its own colour"); self.assertNotEqual(snap[r].cells[-1][1], ("rgb", 255, 100, 0), "right border")

    def test_a_background_that_is_never_reset_ends_at_the_text_and_carries_to_the_next_line_and_wraps(self):
        # line 1: 150 columns on red, no reset; line 2: `red2` (tmux repeats nothing for it); line 3: after a reset
        line = digits(150); self.pane("printf \"\\033[41m%s\\nred2\\n\\033[0mplain\\n\" " + line); x, top, rows = self.show(); RED = rgb(("idx", 1))
        red = lambda c: c[2] is not None and rgb(c[2]) == RED
        r1, r2, r3, r4 = rows[0], rows[1], rows[2], rows[3]; text = lambda r: r.width_text(1, len(r.cells) - 1).rstrip()
        self.assertEqual(text(r1) + text(r2), line, f"{text(r1)!r}"); self.assertEqual(text(r3), "red2")
        for r in (r1, r2):
            n = len(text(r)); self.assertTrue(all(red(c) for c in r.cells[1:1 + n]), "the text is on red (the wrapped half too)"); self.assertFalse(any(red(c) for c in r.cells[1 + n:]), "after the text: pad cells without the background, up to the border")
        n = len("red2"); self.assertTrue(all(red(c) for c in r3.cells[1:1 + n]), "the next line carries the background (tmux did not repeat it)"); self.assertFalse(any(red(c) for c in r3.cells[1 + n:]), "and stops at its text")
        self.assertEqual(text(r4), "plain"); self.assertFalse(any(red(c) for c in r4.cells), "the reset line has none")
        snap = self.t.snap(); self.assertTrue(all(snap[top + k].cells[-1][2] is None or not red(snap[top + k].cells[-1]) for k in range(4)), "the border is not red")

    def test_a_colour_opened_above_the_rows_that_are_shown_still_holds_at_the_first_shown_row(self):
        # 200 lines, all on blue and never reset: the first captured line carries the SGR, the next 199 have none; only the last 43 are shown
        self.pane("printf \"\\033[44m\"; seq 1 200"); x, top, rows = self.show(); BLUE = rgb(("idx", 4)); out = self.content(rows)
        self.assertEqual(out[-2], "200"); first = next(i for i, c in enumerate(out) if c); self.assertTrue(first < 3, out[:3])
        for i, r in enumerate(rows):
            n = len(out[i])
            if n: self.assertTrue(all(c[2] is not None and rgb(c[2]) == BLUE for c in r.cells[1:1 + n]), f"row {i} ({out[i]!r}) is on blue")

    def test_a_reset_in_the_middle_clears_it_for_the_lines_after(self):
        self.pane("printf \"\\033[44mblue1\\nblue2\\033[0m\\nafter\\n\""); x, top, rows = self.show(); BLUE = rgb(("idx", 4)); out = self.content(rows); i = out.index("blue1"); isb = lambda c: c[2] is not None and rgb(c[2]) == BLUE
        self.assertTrue(all(isb(c) for c in rows[i].cells[1:6])); self.assertTrue(all(isb(c) for c in rows[i + 1].cells[1:6])); self.assertEqual(out[i + 2], "after"); self.assertFalse(any(isb(c) for c in rows[i + 2].cells))


class Cursor(Wrap):
    PANES = (("a1", sh("printf abc")), ("a2", sh("printf abc")), ("a3", CAT_CMD))

    def attr_rows(self):
        a = parse_attr(self.t.raw()); return a

    def marked(self, kind="rev"):
        x, top, rows = box(self.t.snap(), self.t.w); a = self.attr_rows(); out = []
        for r in range(top, top + len(rows)):
            for c in range(x + 1, self.t.w - 1):
                if c < len(a[r]) and ((kind == "rev" and a[r][c][1]) or (kind == "ul" and a[r][c][2])): out.append((r, c))
        return out

    def test_the_cursor_on_the_second_segment_of_a_wrapped_line(self):
        # 100 characters, no newline: cursor_x = 100 in the pane; the box takes 87 a row: the text's 2nd row has 13, the cursor sits right after them
        self.pane("printf %s " + digits(100)); x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("000.")); self.assertEqual(out[i + 1], digits(100)[87:], out[i:i + 2])
        rev = self.marked(); self.assertEqual(rev, [(top + i + 1, x + 1 + 1 + 13)], f"the reversed cell: row 2 of the line, 13 characters in (+1 pad, +1 border): {rev}"); self.assertEqual(self.marked("ul"), [])

    def test_the_cursor_after_text_that_fills_the_row_is_on_the_row_below(self):
        self.pane("printf %s " + digits(87)); x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("000."))
        self.assertEqual(self.marked(), [(top + i + 1, x + 2)], "87 cells fill the row: the cursor is the first cell of the next row")

    def test_the_cursor_after_a_wide_character_that_was_pushed_to_the_next_row(self):
        tm("resize-window", "-t", "pane-a1", "-x", 200); tm("respawn-pane", "-k", "-t", self.panes["a1"], "env LANG=en_US.UTF-8 sh -c 'printf a; i=0; while [ $i -lt 44 ]; do printf 你; i=$((i+1)); done; sleep 600'"); time.sleep(0.9)
        x, top, rows = self.show(); out = [c for c in self.content(rows) if c]; self.assertEqual(len(out), 2, out)  # a + 43 wide = 87 cells on row 1... the 44th goes to row 2
        i = next(k for k, c in enumerate(self.content(rows)) if c.startswith("a你")); w2 = sum(cw(ch) for ch in out[1]); self.assertEqual(out[1], "你"); self.assertEqual(self.marked(), [(top + i + 1, x + 2 + 2)], "after the pushed character: 2 cells in")

    def test_an_underline_shape_cursor_is_an_underline_on_the_wrapped_row(self):
        self.pane("printf \"\\033[4 q\"; printf %s " + digits(100)); x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.startswith("000."))
        self.assertEqual(self.marked("ul"), [(top + i + 1, x + 2 + 13)]); self.assertEqual(self.marked("rev"), [])

    def test_a_cursor_past_the_box_width_is_drawn_on_the_wrapped_row(self):
        # (T24 once left it out: cursor_x 95 is outside the box) `%95s x` -> x is the 96th character; its row is the 2nd, the cursor right after it
        self.pane("printf \"%95s\" x"); x, top, rows = self.show(); out = self.content(rows); i = next(k for k, c in enumerate(out) if c.strip() == "x")
        self.assertEqual(self.marked(), [(top + i, x + 2 + 8)], f"x is column 95 of the line: row 2, cell 8; the cursor is the next one\n{out[:4]}")

    def test_the_cursor_hidden_pane_draws_nothing_on_a_wrapped_line_either(self):
        self.pane("printf \"\\033[?25l\"; printf %s " + digits(100)); x, top, rows = self.show(); self.assertEqual(self.marked(), []); self.assertEqual(self.marked("ul"), [])


class Scroll(Wrap):
    def test_one_wheel_notch_moves_three_wrapped_rows(self):
        # the notch is 3 (as it always was: board.go scroll +/- 3), counted in rows after the wrapping, not in the pane's lines (3 lines would be 6 rows here); scrolled back the box ends 3 rows above the last text row
        n = 40; self.pane("i=1; while [ $i -le %d ]; do printf \"%%03d %%s\\n\" $i \"$(printf %%0100d 0)\"; i=$((i+1)); done" % n); x, top, rows = self.show(); want = []
        for i in range(1, n + 1): want += [f"{i:03d} " + "0" * 83, "0" * 17]
        H = len(rows); get = lambda: self.content(box(self.t.snap(), self.t.w)[2]); self.assertEqual(get(), want[len(want) - (H - 1):] + [""], "at the bottom: the end of the text and the cursor's row")
        done = 0
        for k in (1, 2, 5):
            self.t.wheel(x + 20, top + 10, up=True, n=k - done); done = k; time.sleep(0.9)
            self.assertEqual(get(), want[len(want) - 3 * k - H:len(want) - 3 * k], f"{k} notch(es) back: {3 * k} rows, not {6 * k}")
        self.t.wheel(x + 20, top + 10, up=False, n=5); time.sleep(0.9); self.assertEqual(get(), want[len(want) - (H - 1):] + [""], "and back down to the bottom")

    def test_scrolling_stops_at_the_top_of_the_wrapped_history(self):
        self.pane("i=1; while [ $i -le 30 ]; do printf \"%03d %s\\n\" $i \"$(printf %0100d 0)\"; i=$((i+1)); done"); x, top, rows = self.show()
        self.t.wheel(x + 20, top + 10, up=True, n=200); time.sleep(1.2); out = self.content(box(self.t.snap(), self.t.w)[2]); self.assertTrue(out[0].startswith("001 ") or out[0].startswith("0"), out[:2]); first = [c for c in out if c][0]
        self.assertTrue(first.startswith("001 "), f"the top of the pane's history is the first row: {first!r}"); self.t.wheel(x + 20, top + 10, up=True, n=20); time.sleep(0.6); self.assertEqual(self.content(box(self.t.snap(), self.t.w)[2]), out, "no further")
        self.assertTrue(self.t.alive())


if __name__ == "__main__":
    unittest.main()
