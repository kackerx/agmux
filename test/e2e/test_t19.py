"""T19 `/` fuzzy filter. Spec: specs/m3-config/design.md「`/` 模糊搜索过滤」, tasks.md T19.

Each card is matched as `name + " " + branch + " " + cwd` (fzf's FuzzyMatchV2); the matched characters are drawn in the focus colour (#ff8ebe).
Fixture strings: a1 `alpha-main feature/qa-… ~/work/alpha`, a2 `pj-fallback main ~/work/alpha` (no summary: named after its project), b1 `bravo-29 main ~/work/bravo`, e1 `echo-long main /zzz/agmux-e2e/…`.
"""
import json, unittest
import test_t12 as _t12
from lib import *
from test_t3 import ready, last_text_row, border_colours, C_FOCUS, NAMES, by_name
from test_t11 import PreviewBase, CAT_CMD, MARK_CMD, PV_X

F = C_FOCUS
ALL = ["alpha-main", "pj-fallback", "告警会话-超长中文名字-用来测试宽", "bravo-29", "bravo-bg", "charlie-30", "charlie-59", "delta-60", "echo-long"]  # NOT layout order; use sorted()


def card_name(c):
    return c.line(1)[8:-6].strip()  # after "│ ▄▀▀▀▀▄  ", before the relative time and the border


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.t = Term(self.env, w=200, h=50); self.addCleanup(self.t.close)
        self.assertTrue(ready(self.t), self.t.text())

    def search(self, q, enter=False):
        self.t.keys("/"); time.sleep(0.15); self.t.lit(q); time.sleep(0.45)
        if enter:
            self.t.keys("Enter"); time.sleep(0.4)

    def names(self, t=None):
        return [card_name(c) for c in find_cards((t or self.t).snap())]

    def top(self, t=None):
        return (t or self.t).snap()[0].text.rstrip()

    def count(self):
        m = re.search(r"(\d+)/(\d+)\s*$", self.top()); return (int(m.group(1)), int(m.group(2))) if m else None

    def focused(self, t=None):
        return [card_name(c) for c in find_cards((t or self.t).snap()) if border_colours(c) == {F}]

    def titles(self):
        return [re.sub(r"\s{2,}.*", "", r.text.strip()) for r in self.t.snap() if r.text.lstrip().startswith(("▾", "▸"))]

    @staticmethod
    def lit_chars(row, c0=0, c1=None):
        """the characters of a row drawn in the focus colour, borders and the thick-border glyphs excluded"""
        return "".join(c[0] for c in row.cells[c0:c1] if c[1] == F and c[0].strip() and c[0] not in "┃┏┗┓┛━│╭╮╰╯")


class Input(Base):
    def test_slash_turns_the_top_row_into_an_input_with_a_counter(self):
        self.t.keys("/"); time.sleep(0.4)
        top = self.top(); self.assertTrue(top.lstrip().startswith("/ ▏"), top); self.assertTrue(top.endswith("9/9"), top)
        self.assertIn("enter keep the filter · esc clear it", last_text_row(self.t.snap()))
        self.t.lit("brav"); time.sleep(0.5)
        top = self.top(); self.assertIn("/ brav▏", top); self.assertEqual(self.count(), (2, 9))
        self.assertEqual(sorted(self.names()), ["bravo-29", "bravo-bg"], "only the matching cards are drawn")
        self.assertEqual(self.titles(), ["▾ ~/work/bravo (2)"], "groups without a match disappear; the count is of the visible cards")

    def test_the_default_footer_lists_the_key(self):
        self.assertIn("/ search", last_text_row(self.t.snap()))

    def test_typing_filters_live_and_ctrl_h_is_backspace(self):
        self.t.keys("/"); self.t.lit("alpx"); time.sleep(0.4); self.assertEqual(self.names(), [], "alpx matches nothing")
        self.t.keys("C-h"); time.sleep(0.4)
        self.assertIn("/ alp▏", self.top()); self.assertEqual(sorted(self.names()), sorted(["alpha-main", "pj-fallback", "告警会话-超长中文名字-用来测试宽"]))
        self.t.keys("BSpace"); time.sleep(0.3); self.assertIn("/ al▏", self.top())
        self.assertTrue(self.t.alive(), "ctrl+h is backspace, not a command")

    def test_no_match(self):
        self.search("qqqq")
        self.assertEqual(self.count(), (0, 9)); self.assertEqual(self.names(), [])
        self.assertTrue([r for r in self.t.snap() if "no match" in r.text], self.t.text())
        self.t.keys("Enter"); time.sleep(0.4); self.assertTrue(self.t.alive())
        self.assertEqual(self.env.actions(), [], "nothing to jump to")

    def test_the_counter_total_is_all_sessions(self):
        self.search("'echo-l"); self.assertEqual(self.count(), (1, 9))


class Syntax(Base):
    """fzf extended syntax: space = AND, 'exact, ^prefix, suffix$, !exclude; smartcase."""

    def got(self, q):
        self.t.keys("/")
        for _ in range(20):
            self.t.keys("C-h")
        time.sleep(0.2); self.t.lit(q); time.sleep(0.5)
        return sorted(self.names())

    def test_fuzzy_by_default_and_exact_with_a_quote(self):
        self.assertEqual(self.got("brvo"), ["bravo-29", "bravo-bg"], "b..r..v..o in order")
        self.assertEqual(self.got("'brvo"), [], "'exact needs the substring")
        self.assertEqual(self.got("'ravo"), ["bravo-29", "bravo-bg"])

    def test_prefix_and_suffix(self):
        self.assertEqual(self.got("^alpha"), ["alpha-main"], "^ anchors at the start of name + branch + cwd")
        self.assertEqual(self.got("^lpha"), [])
        self.assertEqual(self.got("alpha$"), sorted(["alpha-main", "pj-fallback", "告警会话-超长中文名字-用来测试宽"]), "$ anchors at the end (the cwd)")

    def test_exclude(self):
        self.assertEqual(self.got("!bravo"), sorted(set(ALL) - {"bravo-29", "bravo-bg"}))
        self.assertEqual(self.got("!qqqq"), sorted(ALL), "excluding something nobody has keeps everything")
        self.assertEqual(self.got("^alpha !main"), [], "alpha-main is excluded by !main")

    def test_space_is_and(self):
        self.assertEqual(self.got("echo zzz"), ["echo-long"], "name has echo, cwd has zzz")
        self.assertEqual(self.got("echo bravo"), [])
        self.assertEqual(self.got("bravo 29"), ["bravo-29"])

    def test_smartcase(self):
        self.assertEqual(self.got("bravo"), ["bravo-29", "bravo-bg"])
        self.assertEqual(self.got("BRAVO"), [], "an upper-case letter in the pattern makes it case-sensitive")
        self.assertEqual(self.got("MAIN"), [])
        self.assertEqual(self.count(), (0, 9))

    def test_all_three_fields_match(self):
        self.assertEqual(self.got("qa-acc"), ["alpha-main"], "branch")
        self.assertEqual(self.got("work/delta"), ["delta-60"], "directory")
        self.assertEqual(self.got("charlie-5"), ["charlie-59"], "name")


class Highlight(Base):
    def test_matched_characters_are_in_the_focus_colour_and_the_rest_are_not(self):
        self.search("brav")
        cs = {card_name(c): c for c in find_cards(self.t.snap())}
        row = cs["bravo-bg"].rows[1]  # the focused card (bravo-29) draws everything in the focus colour; judge the other one
        self.assertEqual(self.lit_chars(row), "brav")
        i = row.find("bravo-bg"); self.assertEqual([row.cells[i + k][1] == F for k in range(8)], [True] * 4 + [False] * 4, "b r a v lit, o - b g not")
        self.assertEqual(self.lit_chars(cs["bravo-bg"].rows[2]), "", "branch / bar rows have no match")

    def test_directory_match_highlights_the_group_title(self):
        self.search("work/alpha")
        self.assertEqual(sorted(self.names()), sorted(["alpha-main", "pj-fallback", "告警会话-超长中文名字-用来测试宽"]))
        title = next(r for r in self.t.snap() if "▾ ~/work/alpha" in r.text)
        self.assertEqual(self.lit_chars(title), "work/alpha", "the path in the group title is highlighted")
        cs = {card_name(c): c for c in find_cards(self.t.snap())}
        self.assertEqual(self.lit_chars(cs["pj-fallback"].rows[1]), "", "the name was not what matched")

    def test_branch_match_is_highlighted_on_the_branch_row(self):
        self.search("qa-acc"); c = find_cards(self.t.snap())[0]
        self.assertEqual(self.lit_chars(c.rows[1]), "", "the name row has no match")
        self.assertIn("qa-acc", c.rows[2].text)

    def test_cjk_names_highlight_by_character_and_the_layout_holds(self):
        self.search("告警")
        cs = find_cards(self.t.snap()); self.assertEqual(len(cs), 1); self.assertTrue(cs[0].ok, cs[0].why)
        self.assertEqual(self.lit_chars(cs[0].rows[1]), "告警")
        self.t.keys("C-h"); time.sleep(0.2); self.t.keys("C-h"); time.sleep(0.2); self.t.lit("会话"); time.sleep(0.5)
        cs = find_cards(self.t.snap()); self.assertTrue(cs and all(c.ok for c in cs), self.t.text())
        self.assertEqual(self.lit_chars(cs[0].rows[1]), "会话")
        self.assertEqual(disp_width(cs[0].rows[1].text), cs[0].w, "wide characters keep the card border aligned")


class Keeping(Base):
    def test_enter_keeps_the_filter_and_focuses_the_first_match(self):
        self.search("bravo", enter=True)
        top = self.top(); self.assertIn("/ bravo", top); self.assertNotIn("▏", top, "the caret is gone"); self.assertEqual(self.count(), (2, 9))
        self.assertEqual(sorted(self.names()), ["bravo-29", "bravo-bg"], "still filtered")
        self.assertEqual(self.focused(), ["bravo-29"], "focus on the first matching card")
        self.assertIn("j/k h/l move", last_text_row(self.t.snap()), "the normal footer is back")
        self.t.keys("j"); time.sleep(0.3); self.assertEqual(self.focused(), ["bravo-bg"], "navigation works inside the filtered layout")
        self.t.keys("k"); time.sleep(0.3); self.t.keys("Enter")
        self.assertTrue(wait(lambda: self.env.actions(), 3)); self.assertEqual(self.env.actions(), [["switch", "b1"]])

    def test_esc_clears_the_filter_and_returns_to_the_card_that_had_focus(self):
        self.t.keys("l"); self.t.keys("l"); self.t.keys("j"); time.sleep(0.3)
        self.assertEqual(self.focused(), ["charlie-59"])
        self.search("alpha"); self.assertEqual(self.focused(), ["alpha-main"], "while typing, the focus is on the first match")
        self.t.esc(); time.sleep(0.4)
        self.assertEqual(len(self.names()), 9); self.assertTrue(self.top().lstrip().startswith("agmux"), self.top())
        self.assertEqual(self.focused(), ["charlie-59"], "back on the card that had it when / was pressed")
        self.assertTrue(self.t.alive(), "esc while typing does not quit")

    def test_ctrl_c_clears_like_esc_and_does_not_quit(self):
        self.t.keys("l"); self.t.keys("j"); time.sleep(0.3); before = self.focused()
        self.search("alpha"); self.t.keys("C-c"); time.sleep(0.4)
        self.assertTrue(self.t.alive(), "ctrl+c in the input is a cancel"); self.assertEqual(len(self.names()), 9); self.assertEqual(self.focused(), before)

    def test_slash_again_edits_the_kept_query(self):
        self.search("alpha", enter=True); self.assertEqual(len(self.names()), 3)
        self.t.keys("/"); time.sleep(0.3); self.assertIn("/ alpha▏", self.top(), "the query is back in the input")
        self.t.lit(" main"); time.sleep(0.5); self.assertIn("/ alpha main▏", self.top())
        self.assertEqual(self.count(), (3, 9), "alpha main: a1 name+…, a2 and a3 have main in the branch")

    def test_filter_empty_means_everything_in_layout_order(self):
        base = self.names(); self.t.keys("/"); self.t.lit("a"); time.sleep(0.3)
        self.t.keys("C-h"); time.sleep(0.4); self.assertEqual(self.names(), base, "an empty pattern is no filter and the order does not change")
        self.assertEqual(self.count(), (9, 9))


class Mouse(Base):
    def test_a_click_hits_the_card_in_the_filtered_layout(self):
        self.search("charlie", enter=True)
        cs = {card_name(c): c for c in find_cards(self.t.snap())}; self.assertEqual(sorted(cs), ["charlie-30", "charlie-59"])
        c = cs["charlie-59"]; self.assertEqual(c.c, 0, "filtered: the charlie group moved to the first column")
        self.t.click(c.c + c.w // 2, c.r + 2); time.sleep(0.4)
        self.assertEqual(self.focused(), ["charlie-59"]); self.assertEqual(self.env.actions(), [], "first click only selects")
        self.t.click(c.c + c.w // 2, c.r + 2)
        self.assertTrue(wait(lambda: self.env.actions(), 3)); self.assertEqual(self.env.actions(), [["switch", "c2"]], "the second click jumps to the card that was clicked")


class WithPanes(PreviewBase):
    """real panes: a1 / a3 = raw `cat -v`, a2 = a pane that prints a marker"""
    PANES = (("a1", CAT_CMD), ("a2", MARK_CMD), ("a3", CAT_CMD))
    cat, enter_passthrough_on_a3, preview_focused = _t12.PassthroughReview.cat, _t12.PassthroughReview.enter_passthrough_on_a3, _t12.PassthroughReview.preview_focused

    def search(self, q, enter=True):
        self.t.keys("/"); time.sleep(0.15); self.t.lit(q); time.sleep(0.4)
        if enter:
            self.t.keys("Enter"); time.sleep(0.4)

    def test_the_preview_follows_the_visible_focused_card(self):
        self.search("pj-fallback")
        self.press("p"); self.assertTrue(wait(lambda: self.pv() is not None, 4), self.t.text())
        self.assertIn("pj-fallback", self.pv()[0], "the preview is of the one visible card, not of a1 (the first card of the full layout)")
        self.assertTrue(wait(lambda: any("second-pane-marker" in x for x in self.pv_text()), 4), "and it shows a2's pane:\n" + "\n".join(self.pv_text()))

    def test_slash_in_the_passthrough_belongs_to_the_pane(self):
        self.enter_passthrough_on_a3()
        self.t.lit("/"); time.sleep(0.5)
        self.assertEqual(self.cat("a3"), "/", "the pane got the slash")
        self.assertNotIn("▏", self.t.snap()[0].text, "no filter input opened"); self.assertTrue(self.preview_focused())


if __name__ == "__main__":
    unittest.main()
