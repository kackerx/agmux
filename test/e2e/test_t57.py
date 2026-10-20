"""T57 the real cursor in the pass-through: while the keys go to the pane (tab / i) and the pane's cursor is visible, the board's own terminal cursor is shown and sits on the cell where the preview draws the pane's
cursor -- `tmux display-message -p -t <board pane> '#{cursor_x} #{cursor_y} #{cursor_flag}'` = that cell, flag 1 -- and that cell is no longer drawn as a reversed block (without the pass-through it still is). It follows
typing, wide characters, line breaks and the T56 wrapping. Back on the cards, the preview closed, the board narrower than 100 columns, the pane gone ("session is gone"), the pane's cursor hidden, the preview scrolled back:
flag 0 and the block is back. Leaving the board leaves no cursor positioning on the terminal. `d` (ccmux review) still gets the terminal as its stdout. Spec: specs/m3-config/tasks.md T57 (the input-method
candidate box is the user's to check: not tested). Real tmux panes (private server), the board in a pane of it."""
import json, re, unittest
from lib import *
from test_t11 import PreviewBase, PV_X, pane_text
from test_t24 import CursorBase, sh

CAT = "env LANG=en_US.UTF-8 sh -c 'stty raw -echo; exec cat'"  # echoes the bytes it gets as they are: no wrapping of its own, wide characters whole


class Real(CursorBase):
    PANES = (("a1", sh("printf abc")), ("a2", sh("printf abc")), ("a3", CAT))

    def cur(self, t=None):
        t = t or self.t; o = tm("display-message", "-p", "-t", t.name, "#{cursor_x} #{cursor_y} #{cursor_flag}").stdout.split(); return tuple(int(v) for v in o)

    def to_cat(self):
        """focus a3 (cat), open the preview on it"""
        self.press("j", "j"); self.open_preview_on(); time.sleep(0.3)

    def enter(self):
        self.press("Tab"); self.assertTrue(wait(lambda: "keys go to the pane" in "\n".join(r.text for r in self.t.snap()[-3:]), 3), self.t.text()); time.sleep(0.5)

    def at(self, cell):
        """the board pane's cursor is the screen cell (row, col)"""
        r, c = cell; return self.assertEqual(self.cur(), (c, r, 1), f"cursor_x cursor_y cursor_flag = (col {c}, row {r}, shown)\n{self.t.text()}")

    def settle(self, cell, secs=4):
        r, c = cell; self.assertTrue(wait(lambda: self.cur() == (c, r, 1), secs), f"want (x {c}, y {r}, flag 1), got {self.cur()}\n{self.t.text()}")


class Follows(Real):
    def test_the_board_cursor_is_on_the_cell_after_what_was_typed_and_the_block_is_gone(self):
        self.to_cat(); self.assertEqual(self.cur()[2], 0, "preview open, keys on the cards: no real cursor"); self.assertEqual(len(self.marked("rev")), 1, "the block is drawn as before")
        self.enter(); self.t.lit("xyz"); time.sleep(0.9); r, c = self.text_col("xyz"); self.settle((r, c + 3)); self.assertEqual(self.marked("rev"), [], "the cell under the real cursor is not drawn as a block")
        self.assertEqual(self.marked("ul"), [])
        self.t.lit("!"); time.sleep(0.9); r, c = self.text_col("xyz!"); self.settle((r, c + 4)); self.assertEqual(self.marked("rev"), [])

    def test_it_is_on_the_cell_from_the_moment_the_pass_through_starts(self):
        self.to_cat(); r, c = self.marked("rev")[0]; self.enter(); self.settle((r, c)); self.assertEqual(self.marked("rev"), [])

    def test_wide_characters_count_two_cells(self):
        self.to_cat(); self.enter(); self.t.lit("你好"); time.sleep(0.9); r, c = self.text_col("你好"); self.settle((r, c + 4)); self.assertEqual(self.marked("rev"), [])
        self.t.lit("a"); time.sleep(0.9); self.settle((r, c + 5))

    def test_a_line_break_moves_it_to_the_next_row(self):
        self.to_cat(); self.enter(); self.t.lit("ab"); time.sleep(0.5); self.t.keys("Enter"); time.sleep(0.9); r, c = self.text_col("ab"); self.settle((r, c)); self.t.keys("C-j"); time.sleep(0.9); self.settle((r + 1, c - 0)); self.t.lit("cd"); time.sleep(0.9); self.settle((r + 1, c + 2))

    def test_a_cursor_past_the_last_cell_of_the_box_is_not_placed_and_not_drawn(self):
        # T61: the preview is cut, not wrapped: the box has 87 text cells; the pane's cursor at column 87 or more is not in it
        tm("resize-window", "-t", "pane-a3", "-x", 150); self.to_cat(); self.enter(); self.t.lit("x" * 85); time.sleep(1.0); r, c = self.text_col("x" * 10); self.settle((r, c + 85)); self.assertEqual(self.marked("rev"), [])
        self.t.lit("x"); time.sleep(0.9); self.settle((r, c + 86), 3)  # cursor_x = 86: the 87th and last cell
        self.t.lit("x"); self.assertTrue(wait(lambda: self.cur()[2] == 0, 4), f"cursor_x = 87: past the box: the real cursor is hidden ({self.cur()})\n{self.t.text()}"); self.assertEqual(self.marked("rev"), [], "and no block either")
        self.t.lit("xxxxx"); time.sleep(0.9); self.assertEqual(self.cur()[2], 0); self.assertEqual(self.marked("rev"), []); self.assertEqual(len([r for r in self.t.snap() if "x" * 20 in r.text]), 1, "one row: nothing wrapped")
        self.t.keys("Enter"); self.t.keys("C-j"); time.sleep(0.9); self.t.lit("ab"); time.sleep(0.9); r2, c2 = self.text_col("ab"); self.settle((r2, c2 + 2), 4)  # a new line: the cursor is back in the box


class Leaves(Real):
    def test_tab_back_to_the_cards_hides_it_and_the_block_returns(self):
        self.to_cat(); self.enter(); self.t.lit("xyz"); time.sleep(0.9); r, c = self.text_col("xyz"); self.settle((r, c + 3))
        self.press("Tab"); self.assertTrue(wait(lambda: self.cur()[2] == 0, 3), f"{self.cur()}\n{self.t.text()}"); time.sleep(0.9); self.assertEqual(self.marked("rev"), [(r, c + 3)], "the block again")
        self.enter(); self.settle((r, c + 3))

    def test_closing_the_preview_hides_it(self):
        self.to_cat(); self.enter(); self.press("Tab"); self.assertTrue(wait(lambda: self.cur()[2] == 0, 3)); self.enter(); self.assertEqual(self.cur()[2], 1)
        self.press("Tab"); self.press("p"); self.assertTrue(wait(lambda: self.pv() is None, 4), "preview closed"); self.assertTrue(wait(lambda: self.cur()[2] == 0, 3), self.cur())

    def test_a_board_narrower_than_100_columns_closes_the_preview_and_the_cursor(self):
        self.to_cat(); self.enter(); self.assertEqual(self.cur()[2], 1); self.t.resize(99, 50); self.assertTrue(wait(lambda: self.cur()[2] == 0, 5), f"{self.cur()}\n{self.t.text()}")
        self.assertTrue(self.t.alive()); self.assertIsNone(self.pv()); self.t.resize(232, 50); time.sleep(1.0); self.assertEqual(self.cur()[2], 0, "and it does not come back by itself")

    def test_the_session_going_away_hides_it(self):
        self.to_cat(); self.enter(); self.t.lit("x"); time.sleep(0.6); self.assertEqual(self.cur()[2], 1)
        S = json.loads((self.env.fake / "show.json").read_text()); self.env.write_show([s for s in S if s["id"] != "a3"])
        self.assertTrue(wait(lambda: "session is gone" in self.t.text(), 6), self.t.text()); self.assertTrue(wait(lambda: self.cur()[2] == 0, 3), f"{self.cur()}\n{self.t.text()}")
        self.assertEqual(len(self.marked("rev")), 1, "back to the status quo: the block, on the last frame the preview had")

    def test_a_pane_that_hides_its_cursor_hides_ours_and_a_shown_one_brings_it_back(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], sh("printf abc; sleep 3; printf \"\\033[?25l\"; sleep 3; printf \"\\033[?25h\"")); time.sleep(0.5)
        self.open_preview_on(); self.enter(); r, c = self.text_col("abc"); self.settle((r, c + 3), 3)
        self.assertTrue(wait(lambda: self.cur()[2] == 0, 5), "the pane hid its cursor: so do we"); self.assertEqual(self.marked("rev"), []); self.assertEqual(self.marked("ul"), [])
        self.assertTrue(wait(lambda: self.cur() == (c + 3, r, 1), 6), f"the pane showed it again: {self.cur()}")

    def test_scrolling_back_hides_it_and_the_bottom_brings_it_back(self):
        tm("respawn-pane", "-k", "-t", self.panes["a3"], f"env LANG=en_US.UTF-8 sh -c 'seq 1 200; stty raw -echo; exec cat'"); time.sleep(1.2)
        self.press("j", "j"); self.open_preview_on(); self.enter(); self.assertTrue(wait(lambda: self.cur()[2] == 1, 4), self.t.text()); where = self.cur()
        self.t.wheel(PV_X + 20, 15, up=True, n=3); self.assertTrue(wait(lambda: self.cur()[2] == 0, 4), f"scrolled back: {self.cur()}")
        self.t.wheel(PV_X + 20, 15, up=False, n=10); self.assertTrue(wait(lambda: self.cur() == where, 4), f"back at the bottom: {self.cur()} (was {where})")

    def test_the_block_is_drawn_again_when_the_pass_through_ends_even_with_the_cursor_hidden_by_wheel(self):
        self.to_cat(); self.enter(); self.press("Tab"); self.assertEqual(self.cur()[2], 0); self.assertEqual(len(self.marked("rev")), 1)


class Exit(Real):
    def test_leaving_the_board_from_the_pass_through_leaves_no_cursor_position_behind(self):
        out = self.env.tmp / "board.out"; tm("pipe-pane", "-o", "-t", self.t.name, f"cat >> {out}")  # the bytes the board writes, before tmux reads them
        self.to_cat(); self.enter(); self.t.lit("xyz"); time.sleep(0.9); self.assertEqual(self.cur()[2], 1); self.press("Tab"); self.enter(); self.press("Tab")
        self.t.keys("q"); self.assertEqual(self.t.exit_status(5), 0, self.t.text()); time.sleep(0.3); data = out.read_bytes().decode("utf-8", "replace")
        self.assertIn("\x1b[?1049l", data, "the alt screen was left"); tail = data[data.rindex("\x1b[?1049l"):]
        self.assertEqual(re.findall(r"\x1b\[\d+;\d+[Hf]", tail), [], f"nothing positions the cursor after the alt screen is left: {tail!r}")
        self.assertIn("\x1b[?25l", data, "it was hidden again at least once (leaving the pass-through)"); self.assertIn("\x1b[?25h", data)
        self.assertTrue(tail.endswith("\x1b[?25h"), f"the last word is 'show the cursor': {tail[-40:]!r}")

    def test_quitting_straight_from_the_pass_through_state_too(self):
        out = self.env.tmp / "board2.out"; tm("pipe-pane", "-o", "-t", self.t.name, f"cat >> {out}")
        self.to_cat(); self.enter(); self.press("Tab"); self.t.keys("Q"); self.assertEqual(self.t.exit_status(5), 0, self.t.text()); time.sleep(0.3)
        data = out.read_bytes().decode("utf-8", "replace"); tail = data[data.rindex("\x1b[?1049l"):]; self.assertEqual(re.findall(r"\x1b\[\d+;\d+[Hf]", tail), [], repr(tail))


class Review(Real):
    """`d` runs `hunk diff --watch` (T69; ccmux review before) with the board's terminal as its stdin / stdout (stderr is captured on purpose: the board shows its last line when the review fails) (T57 broke that once: bubbletea gave the child a pipe); the board's cursor logic survives the round trip"""
    def hook(self):
        out = self.env.fake / "review.tty"; self.env.git_repo(self.env.home / "work" / "alpha")  # (d runs hunk at the root of the repo of the card's cwd)
        f = self.env.bin_real / "hunk"; f.write_text(f'#!/bin/sh\ni=no; o=no; [ -t 0 ] && i=tty; [ -t 1 ] && o=tty; printf "in=$i out=$o" > {out}; sleep 0.5; exit 0\n'); f.chmod(0o755); return out

    def test_the_review_child_has_a_terminal_for_stdin_and_stdout(self):
        out = self.hook(); self.press("d"); self.assertTrue(wait(out.exists, 5), "hunk was run"); time.sleep(0.8); self.assertEqual(out.read_text(), "in=tty out=tty", "T57: [ -t 1 ] is true")
        self.assertTrue(self.t.alive()); self.assertTrue(wait(lambda: self.pv() is None and len(find_cards(self.t.snap())) >= 5, 5), self.t.text())

    def test_after_a_review_the_pass_through_cursor_still_works(self):
        out = self.hook(); self.to_cat(); self.enter(); self.t.lit("xyz"); time.sleep(0.9); r, c = self.text_col("xyz"); self.settle((r, c + 3)); self.press("Tab")
        self.assertTrue(wait(lambda: self.cur()[2] == 0, 3)); self.press("d"); self.assertTrue(wait(out.exists, 5)); time.sleep(1.5); self.assertEqual(out.read_text(), "in=tty out=tty")
        self.assertTrue(wait(lambda: len(find_cards(self.t.snap())) >= 5, 5), self.t.text()); self.assertEqual(self.cur()[2], 0, "back on the cards: no real cursor")
        self.to_cat() if self.pv() is None else None; self.enter(); r, c = self.text_col("xyz"); self.settle((r, c + 3), 5)


if __name__ == "__main__":
    unittest.main()
