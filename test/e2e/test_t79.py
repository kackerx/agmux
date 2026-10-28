"""T79 the input box, shared by the board's `/` and the recall screen (T83, tester side; written from the spec before the code: specs/m3-config/tasks.md 第十六批 T79, specs/m6-recall/design.md §2「输入框」).
A REAL cursor (the terminal's own: `#{cursor_x}` of the tmux pane that stands for the terminal; not a drawn block); left / right, home / ctrl+a, end / ctrl+e, alt+backspace / ctrl+w (a word back), characters go in at the
cursor; the cursor column follows the DISPLAY width (a Chinese character is 2 cells). kitty sends option+backspace as ESC DEL (bytes 0x1b 0x7f): sent here as the raw bytes through the pane, once, so the program must tell
it from an ESC followed by a backspace. The same cases run on both hosts (mixin `Edits`).
Skipped until T79–T82 are in the binary (lib_recall.t83_ready()). Things only the code decides carry `GUESS:` (where exactly the cursor lands after a word is deleted: before or after the blank)."""
import time, unittest
from lib import wait, disp_width, Term, find_cards, build_fixture
from lib_recall import *

needs = unittest.skipUnless(t83_ready(), "T79-T82 are not in this binary yet: written from the spec")
ESC_DEL = "\x1b\x7f"


class Edits:
    """each host gives `start()` -> a terminal whose input box is open and empty, `results_follow()`"""

    def at(self, t, text, pos, msg=""):
        """the text is on the screen in one piece and the real cursor stands `pos` characters into it (columns by display width)"""
        want = {}

        def ok():
            f = row_of(t, text)
            if not f: return False
            r, c = f; x, y, vis = cursor(t); want.update(row=r, col=c, cx=x, cy=y, vis=vis)
            return vis and y == r and x == c + disp_width(text[:pos])
        self.assertTrue(wait(ok, 4), f"{msg}: want {text!r} with the cursor after {text[:pos]!r}; found {want}\n{t.text()}")

    def test_the_cursor_is_the_terminals_own_and_follows_what_is_typed(self):
        t = self.start(); t.lit("abc"); self.at(t, "abc", 3)

    def test_left_and_right_move_the_cursor_and_characters_go_in_at_it(self):
        t = self.start(); t.lit("abcd"); t.keys("Left", "Left"); self.at(t, "abcd", 2); t.lit("X"); self.at(t, "abXcd", 3); t.keys("Right"); self.at(t, "abXcd", 4)
        t.keys(*["Right"] * 5); self.at(t, "abXcd", 5, "clamped at the end"); t.keys(*["Left"] * 9); self.at(t, "abXcd", 0, "clamped at the start"); t.lit("Y"); self.at(t, "YabXcd", 1)

    def test_home_and_ctrl_a_go_to_the_start_end_and_ctrl_e_to_the_end(self):
        t = self.start(); t.lit("hello world"); t.keys("C-a"); self.at(t, "hello world", 0, "ctrl+a"); t.lit("["); self.at(t, "[hello world", 1); t.keys("C-e"); self.at(t, "[hello world", 12, "ctrl+e"); t.lit("]"); self.at(t, "[hello world]", 13)
        t.keys("Home"); self.at(t, "[hello world]", 0, "home"); t.keys("End"); self.at(t, "[hello world]", 13, "end")

    def test_backspace_and_ctrl_h_delete_the_character_before_the_cursor(self):
        t = self.start(); t.lit("abcd"); t.keys("Left", "Left", "BSpace"); self.at(t, "acd", 1); t.keys("C-h"); self.at(t, "cd", 0)

    def word_back(self, key):
        """`key` deletes the word before the cursor: at the end, and in the middle; the cursor lands where the word was (GUESS: before or after the blank that followed it: both are fine)"""
        t = self.start(); t.lit("foo bar baz"); key(t)
        self.assertTrue(wait(lambda: row_of(t, "foo bar") and not row_of(t, "baz"), 4), "the last word is gone, the others stay:\n" + t.text()); f = row_of(t, "foo bar"); x, y, _ = cursor(t); self.assertIn(x - f[1], (7, 8), "the cursor is right after what is left")
        key(t); self.assertTrue(wait(lambda: row_of(t, "foo") and not row_of(t, "bar"), 4), "a second one takes `bar`"); f = row_of(t, "foo"); self.assertIn(cursor(t)[0] - f[1], (3, 4))
        t.close(); t = self.start(); t.lit("foo bar baz"); t.keys(*["Left"] * 4); key(t)  # the cursor is right after `bar`
        self.assertTrue(wait(lambda: row_of(t, "foo") and row_of(t, "baz") and not row_of(t, "bar"), 4), "the word before the cursor goes, what follows it stays:\n" + t.text()); f = row_of(t, "foo"); self.assertIn(cursor(t)[0] - f[1], (3, 4), "the cursor stays where the word was")

    def test_alt_backspace_as_kitty_sends_it_esc_del_deletes_a_word(self):
        self.word_back(lambda t: t.lit(ESC_DEL))

    def test_alt_backspace_as_a_tmux_key_does_too(self):
        self.word_back(lambda t: t.keys("M-BSpace"))

    def test_ctrl_w_deletes_a_word(self):
        self.word_back(lambda t: t.keys("C-w"))

    def test_a_lone_esc_is_still_an_esc_after_esc_del_has_been_taken_for_alt_backspace(self):
        t = self.start(); t.lit("one two"); t.lit(ESC_DEL); self.assertTrue(wait(lambda: row_of(t, "one") and not row_of(t, "two"), 4)); time.sleep(0.5); self.assertTrue(t.alive(), "ESC DEL is not an esc, not a quit")

    def test_the_cursor_column_follows_the_display_width_of_chinese(self):
        t = self.start(); t.lit("ab找"); self.at(t, "ab找", 3, "2 + 2 cells"); t.lit("回"); self.at(t, "ab找回", 4); t.keys("Left"); self.at(t, "ab找回", 3); t.keys("Left", "Left", "Left"); self.at(t, "ab找回", 0)
        t.keys("Right", "Right", "Right"); self.at(t, "ab找回", 3, "left and right move by character, the column by cells")

    def test_chinese_goes_in_in_the_middle_and_a_chinese_word_is_deleted(self):
        t = self.start(); t.lit("找回"); t.keys("Left"); t.lit("会话"); self.at(t, "找会话回", 3, "6 cells in"); t.keys("C-e"); self.at(t, "找会话回", 4)
        t.close(); t = self.start(); t.lit("找回 会话"); t.lit(ESC_DEL)
        self.assertTrue(wait(lambda: row_of(t, "找回") and not row_of(t, "会话"), 4), t.text()); f = row_of(t, "找回"); self.assertIn(cursor(t)[0] - f[1], (4, 5), "the blank stays or goes, the cursor is after `找回`")

    def test_a_text_edited_in_the_middle_is_what_filters(self):
        self.results_follow()


@needs
class RecallInput(Edits, unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); self.c = corpus(self.env)

    def start(self):
        t = RTerm(self.env, args="-r", w=140, h=40); self.addCleanup(t.close); self.assertTrue(wait(lambda: t.visible({"a": "cart-fix"}), 10), t.text()); time.sleep(0.5); return t

    def results_follow(self):
        t = self.start(); t.lit("'couon"); t.keys("Left", "Left"); t.lit("p")  # `'couon` is no hit; with the p put in the middle it is `'coupon`
        self.assertTrue(wait(lambda: t.visible(NAMES_ALL) == {"cart"}, 5), t.text()); self.at(t, "'coupon", 5)


NAMES_ALL = {"cart": "cart-fix", "audit": "Audit log", "gone": "vanished helper", "pi_audit": "audit-owner", "pi_plain": "checkout flow"}


@needs
class BoardSearchInput(Edits, unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); build_fixture(self.env)

    def start(self):
        t = Term(self.env, w=200, h=40); self.addCleanup(t.close); self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) >= 1, 10), t.text()); time.sleep(0.5); t.keys("/"); time.sleep(0.4); return t

    def results_follow(self):
        t = self.start(); t.lit("chrlie"); t.keys(*["Left"] * 4); t.lit("a")  # `chrlie` has no hit; `charlie` is the two charlie cards
        names = lambda: [c.line(1) for c in find_cards(t.snap()) if c.ok]
        self.assertTrue(wait(lambda: len(names()) == 2 and all("charlie" in x for x in names()), 5), t.text()); self.at(t, "charlie", 3)


if __name__ == "__main__":
    unittest.main()
