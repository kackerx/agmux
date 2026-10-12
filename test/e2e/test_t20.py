"""T20 + T23 `s` flash, flash.nvim style. Spec: specs/m3-config/design.md「`s` 全局定位（flash）」, tasks.md T23.

`s` enters the JUMP state: nothing has a label yet. Every printable character narrows: each card on screen (and each group title) whose name / branch (dir: group titles only) contains the typed text
as a case-insensitive substring gets the match on the yellow `colors.match` background and ONE label in the cell right after the hit (focus background, dark text). Labels come from
`asdfghjklqwertyuiopzxcvbnm` then upper case, in order of distance from the focused card, and never use a character that follows any hit (typing on and pressing a label cannot collide).
A label moves the focus (it never jumps to the session) and leaves JUMP; esc leaves with the focus unchanged.
"""
import json, unittest
import test_t12 as _t12
from lib import *
from test_t3 import ready, last_text_row, border_colours, C_FOCUS, NAMES, by_name
from test_t11 import PreviewBase, CAT_CMD

ALPHABET = "asdfghjklqwertyuiopzxcvbnm"
LABELS = list(ALPHABET) + list(ALPHABET.upper())
MATCH = ("rgb", 242, 208, 107)
INK, MUTED = (40, 42, 54), (98, 114, 164)
PROMPT = "flash: type to narrow · label to jump · esc"


def name_of(c):
    return c.line(1)[8:-6].strip()


KNOWN = ["alpha-main", "pj-fallback", "告警会话-超长中文名字-用来测试宽字符截断-再多写一些字让它超过列宽", "bravo-29", "bravo-bg", "charlie-30", "charlie-59", "delta-60", "echo-long",
         "aaa-new", "bnew-card", "zed-one", "alpha", "alps", "alpine"] + [f"alp-{i:02d}" for i in range(20)] + [f"sess-{i:02d}" for i in range(30)]


def resolve(shown):
    """the card name behind what is on screen: in flash a label overwrites the character after the hit, so allow one wrong cell"""
    s = shown.rstrip("…").rstrip()
    for k in KNOWN:
        k2 = k[:len(s)]
        if len(k2) == len(s) and sum(a != b for a, b in zip(k2, s)) <= 1:
            return k
    return shown


def near(a, b, d=3):
    return a is not None and a[0] == "rgb" and all(abs(x - y) <= d for x, y in zip(a[1:], b))


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
        return [resolve(name_of(c)) for c in find_cards(self.t.snap()) if border_colours(c) == {C_FOCUS}]

    def flash(self, text=""):
        self.t.keys("s"); self.assertTrue(wait(lambda: PROMPT in self.bar(), 3), self.t.text())
        if text:
            self.t.lit(text)
        time.sleep(0.5)

    def in_flash(self):
        return PROMPT in self.bar()

    def message_row(self):
        return next((r.text.strip() for r in self.t.snap() if "▏" in r.text), None)

    def label_cells(self):
        """(row, col, char) of every label cell: dark text on the focus colour"""
        return [(r, c, cell[0]) for r, row in enumerate(self.t.snap()) for c, cell in enumerate(row.cells)
                if cell[0].strip() and cell[2] == C_FOCUS and cell[1] == ("rgb", *INK)]

    def card_labels(self):
        """{card name: label} for the cards on screen (the label sits on the name / branch row), plus {"title:<text>": label} for group titles"""
        snap = self.t.snap(); out = {}; cells = self.label_cells()
        for c in find_cards(snap):
            labs = [ch for r, col, ch in cells if c.r < r < c.r + len(c.rows) - 1 and c.c <= col < c.c + c.w]
            if labs:
                out[resolve(name_of(c))] = labs
        for r, ch_col, ch in cells:
            row = snap[r]
            if row.text.lstrip().startswith(("▾", "▸")):
                out.setdefault("title:" + re.search(r"[▾▸] \S+", row.text[row.text.find("▾") if "▾" in row.text else 0:]).group(0) if False else "titles", []).append(ch)
        return out

    def hits(self, row):
        return "".join(c[0] for c in row.cells if c[2] == MATCH and c[0].strip())


class Prompt(Base):
    def test_default_footer_lists_s(self):
        self.assertIn("s flash", self.bar())

    def test_s_alone_shows_the_prompt_and_the_empty_input_and_no_labels(self):
        self.flash()
        self.assertEqual(self.label_cells(), [], "no label until something is typed")
        self.assertEqual(self.message_row(), "s ▏")
        snap = self.t.snap()
        for c in find_cards(snap):
            inside = {x[1] for r in c.rows[1:4] for x in r.cells[1:-1] if x[0].strip()}
            self.assertEqual(len(inside), 1, f"{c.line(1)}: content in one colour"); self.assertTrue(near(next(iter(inside)), MUTED), f"dimmed to `muted`: {inside}")

    def test_the_message_row_shows_what_is_typed(self):
        self.flash("br"); self.assertEqual(self.message_row(), "s br▏")
        self.t.keys("a"); time.sleep(0.3); self.assertEqual(self.message_row(), "s bra▏")
        self.t.keys("BSpace"); time.sleep(0.3); self.assertEqual(self.message_row(), "s br▏")
        self.t.keys("C-h"); time.sleep(0.3); self.assertEqual(self.message_row(), "s b▏", "ctrl+h is a backspace too")

    def test_s_inside_flash_is_text_or_a_label_not_a_second_flash(self):
        self.flash("br"); labs = self.label_cells()
        self.assertTrue(self.in_flash())


class Matches(Base):
    def test_typing_marks_substring_hits_yellow_with_one_label_right_after_each(self):
        self.flash("br")
        cs = {resolve(name_of(c)): c for c in find_cards(self.t.snap())}
        for n in ("bravo-29", "bravo-bg"):
            self.assertEqual(self.hits(cs[n].rows[1]), "br", f"{n}: the hit is on the yellow background")
            row = cs[n].rows[1]; i = row.find("br")
            lab = row.cells[i + 2]; self.assertEqual((lab[2], lab[1]), (C_FOCUS, ("rgb", *INK)), f"{n}: the label is the cell right after `br`"); self.assertIn(lab[0], ALPHABET)
        for n in ("alpha-main", "pj-fallback", "charlie-30", "delta-60", "echo-long"):
            self.assertEqual(self.hits(cs[n].rows[1]), "", n); self.assertNotIn(n, self.card_labels())
        title = next(r for r in self.t.snap() if r.text.lstrip().startswith("▾") and "~/work/br" in r.text)
        self.assertEqual(self.hits(title), "br", "the group title is searched too (its directory text)")
        self.assertEqual(len(self.label_cells()), 3, "one label per hit: two cards and one group title")

    def test_the_search_is_a_case_insensitive_substring_not_fuzzy(self):
        self.flash("BR"); self.assertEqual(len(self.label_cells()), 3, "upper case finds the same hits")
        self.t.keys("BSpace"); self.t.keys("BSpace"); time.sleep(0.3)
        self.t.lit("bvo"); time.sleep(0.4)
        self.assertEqual(self.label_cells(), [], "b..v..o is a fuzzy match, not a substring: nothing")

    def test_the_directory_is_searched_in_group_titles_only(self):
        self.flash("work/br")
        self.assertEqual(len(self.label_cells()), 1, "cards do not show their directory: only the bravo group title matches")
        title = next(r for r in self.t.snap() if r.text.lstrip().startswith("▾") and "~/work/br" in r.text); self.assertEqual(self.hits(title), "work/br")
        for c in find_cards(self.t.snap()):
            self.assertEqual(self.hits(c.rows[1]) + self.hits(c.rows[2]), "")

    def test_name_and_branch_together_have_one_hit_and_one_label_per_card(self):
        S = json.loads((self.env.fake / "show.json").read_text()); base = S[0]
        two = dict(base, id="zz", summary="zed-one", project="zzp", cwd=f"{self.env.home}/work/zzp", mainRepoRoot=f"{self.env.home}/work/zzp", gitBranch="zed-two", tmuxPane="%190",
                   status="idle", subagents=[], pendingTool=None, nativeSessionId="n-zz", logPath=None)
        self.env.write_show(S + [two]); self.t.close(); self.t = self.board(200, 50, 10)
        self.flash("zed")
        self.assertEqual(len([1 for r, c, ch in self.label_cells()]), 1, "the name and the branch both contain `zed`: still one hit, one label")

    def test_continuing_to_type_narrows_and_reassigns_the_labels(self):
        self.flash("b"); n1 = len(self.label_cells()); self.t.lit("ravo-b"); time.sleep(0.5)
        self.assertEqual(len(self.label_cells()), 1, "only bravo-bg has `bravo-b`"); self.assertGreater(n1, 1)
        self.assertEqual([resolve(name_of(c)) for c in find_cards(self.t.snap()) if self.hits(c.rows[1])], ["bravo-bg"])

    def test_no_match_has_no_labels_and_one_can_keep_typing_or_erase(self):
        self.flash("qqq"); self.assertEqual(self.label_cells(), []); self.assertTrue(self.in_flash())
        for _ in range(3):
            self.t.keys("BSpace")
        self.t.lit("br"); time.sleep(0.4); self.assertEqual(len(self.label_cells()), 3, "erasing and typing again works after a dead end")

    def test_hits_use_the_match_colour_from_the_config(self):
        self.config('[colors]\nmatch = "#00ff00"\n'); self.t.close(); self.t = self.board(); self.flash("br")
        c = next(c for c in find_cards(self.t.snap()) if resolve(name_of(c)) == "bravo-bg")
        self.assertEqual("".join(x[0] for x in c.rows[1].cells if x[2] == ("rgb", 0, 255, 0) and x[0].strip()), "br")


class LabelLetters(Base):
    def sessions(self, names, project=None):
        base = json.loads((self.env.fake / "show.json").read_text())[0]
        proj = lambda i: project or f"p{i % 4}"  # four groups: a grid, not one endless column
        return [dict(base, id=f"x{i:02d}", summary=n, project=proj(i), cwd=f"{self.env.home}/work/{proj(i)}", mainRepoRoot=f"{self.env.home}/work/{proj(i)}", gitBranch="main",
                     tmuxPane=f"%{600 + i}", status="idle", subagents=[], pendingTool=None, nativeSessionId=f"n-x{i}", logPath=None) for i, n in enumerate(names)]

    def restart_with(self, sessions):
        self.env.write_show(sessions); self.env.pane_ids = {s["tmuxPane"]: s["id"] for s in sessions}
        self.t.close(); self.t = self.board(200, 50, len(sessions))

    def test_labels_never_use_the_character_after_a_hit(self):
        self.restart_with(self.sessions(["alpha", "alps", "alpine"], project="pp"))
        self.flash("al")
        labs = [ch for _, _, ch in self.label_cells()]
        self.assertEqual(len(labs), 3); self.assertNotIn("p", labs + [l.lower() for l in labs], "every hit is followed by `p`: no label may be p")
        self.assertEqual(sorted(labs), sorted("asd"), "the first free letters in distance order")

    def test_twenty_hits_followed_by_p_skip_p_in_the_alphabet(self):
        self.restart_with(self.sessions([f"alp-{i:02d}" for i in range(20)]))
        self.flash("al"); labs = [ch for _, _, ch in self.label_cells()]
        self.assertEqual(len(labs), 20); self.assertEqual(len(set(labs)), 20); self.assertNotIn("p", labs, "the 19th letter of the alphabet is the skipped one")
        self.assertEqual(sorted(labs), sorted((ALPHABET.replace("p", ""))[:20]))

    def test_the_skipped_characters_follow_the_text_typed_so_far(self):
        self.flash("br"); labs = [ch for _, _, ch in self.label_cells()]
        self.assertNotIn("a", labs, "`br` is followed by `a` in bravo")
        self.t.keys("a"); time.sleep(0.4); labs = [ch for _, _, ch in self.label_cells()]
        self.assertIn("a", labs, "after `bra` the next character is `v`: `a` is free again"); self.assertNotIn("v", labs)

    def test_labels_are_given_by_distance_from_the_focused_card(self):
        self.t.keys("l"); self.t.keys("j"); time.sleep(0.3); f = next(c for c in find_cards(self.t.snap()) if border_colours(c) == {C_FOCUS})  # focus bravo-bg: not the first card
        self.flash("o")  # `o` is in many places: group titles (work), bravo-*, echo-long, a branch
        cells = self.label_cells(); self.assertGreaterEqual(len(cells), 6)
        by_distance = sorted(cells, key=lambda x: (abs(x[0] - f.r) * 200 + abs(x[1] - f.c), x[0], x[1]))
        got = [ch for _, _, ch in by_distance]
        self.assertEqual(got, LABELS[:len(got)], f"the nearest hit (measured from the focused card) gets `a`, then s d f ...: {[(r, c, ch) for r, c, ch in by_distance]}")

    def test_thirty_cards_second_round_is_upper_case(self):
        self.env.write_show(self.sessions([f"sess-{i:02d}" for i in range(30)])); self.env.pane_ids = {}
        self.t.close(); self.t = self.board(200, 60, 30)
        self.flash("sess"); labs = [ch for _, _, ch in self.label_cells()]
        self.assertEqual(len(labs), 30)
        self.assertEqual(sorted(labs), sorted(LABELS[:30]), "26 lower case, then A S D F")
        self.assertIn("A", labs)


class Pressing(Base):
    def test_a_card_label_moves_the_focus_and_leaves_flash_without_jumping(self):
        self.flash("br")
        lab = next(ch for r, c, ch in self.label_cells() if r == next(cc.r for cc in find_cards(self.t.snap()) if resolve(name_of(cc)) == "bravo-bg") + 1)
        self.t.keys(lab); time.sleep(0.5)
        self.assertEqual(self.focused(), ["bravo-bg"]); self.assertFalse(self.in_flash()); self.assertIn("j/k h/l move", self.bar())
        self.assertEqual(self.env.actions(), [], "no ccmux / tmux call"); self.assertEqual(self.env.tmux_calls(), [])
        self.assertEqual(self.label_cells(), [], "labels and yellow are gone"); self.assertTrue(self.t.alive())

    def test_a_group_title_label_goes_to_the_first_card_of_an_open_group(self):
        self.flash("br")
        title_row = next(r for r, row in enumerate(self.t.snap()) if row.text.lstrip().startswith("▾") and "~/work/br" in row.text)
        lab = next(ch for r, c, ch in self.label_cells() if r == title_row)
        self.t.keys(lab); time.sleep(0.5)
        self.assertEqual(self.focused(), ["bravo-29"], "the first card of the group"); self.assertFalse(self.in_flash()); self.assertEqual(self.env.actions(), [])

    def test_a_folded_group_title_label_lands_on_the_title(self):
        self.t.keys("f"); time.sleep(0.4)  # alpha folded, focus on its title
        self.flash("alpha")
        labs = self.label_cells(); self.assertEqual(len(labs), 1, "only the folded title matches (its cards are hidden)")
        self.t.keys(labs[0][2]); time.sleep(0.5)
        self.assertFalse(self.in_flash()); self.t.keys("Enter"); time.sleep(0.5)
        self.assertEqual(self.env.actions(), [], "enter on the title expands it, it does not jump"); self.assertIn("▾ ~/work/alpha (3)", self.t.text())

    def test_esc_cancels_and_the_focus_stays(self):
        self.t.keys("l"); time.sleep(0.3); before = self.focused(); self.flash("br")
        self.t.esc(); time.sleep(0.4)
        self.assertEqual(self.focused(), before); self.assertTrue(self.t.alive()); self.assertEqual(self.label_cells(), []); self.assertIn("j/k h/l move", self.bar())

    def test_keys_that_are_neither_labels_nor_text_are_ignored(self):
        self.flash("br")
        for k in ("Tab", "Enter", "Up", "Down", "F5"):
            self.t.keys(k); time.sleep(0.2)
        self.assertTrue(self.t.alive()); self.assertTrue(self.in_flash()); self.assertEqual(self.message_row(), "s br▏"); self.assertEqual(self.env.actions(), [])
        self.t.esc(); time.sleep(0.3); self.assertEqual(self.focused(), ["alpha-main"], "none of them moved the focus")

    def test_a_label_is_not_a_command_and_text_keys_are_not_commands(self):
        self.flash("q")  # q / x / r are text here, never quit / kill / restart
        self.assertTrue(self.t.alive()); self.assertEqual(self.env.actions(), [])
        self.t.keys("x"); self.t.keys("r"); time.sleep(0.3); self.assertEqual(self.env.actions(), []); self.assertTrue(self.t.alive())

    def test_mouse_clicks_and_wheel_are_swallowed(self):
        self.flash("br"); c = next(c for c in find_cards(self.t.snap()) if resolve(name_of(c)) == "echo-long")
        self.t.click(c.c + c.w // 2, c.r + 2); self.t.wheel(c.c + 5, c.r + 2, up=False, n=3); time.sleep(0.5)
        self.assertTrue(self.in_flash(), "still in flash"); self.assertEqual(self.env.actions(), []); self.assertEqual(self.message_row(), "s br▏")
        self.t.esc(); time.sleep(0.3); self.assertEqual(self.focused(), ["alpha-main"], "the click did not select the card")

    def test_with_a_filter_only_the_visible_cards_are_searched(self):
        self.t.keys("/"); self.t.lit("brav"); self.t.keys("Enter"); time.sleep(0.5)
        self.assertEqual(len(find_cards(self.t.snap())), 2)
        self.flash("b")
        self.assertEqual(len(self.label_cells()), 3, "two visible cards + their group title; the hidden ones have no label")
        for n in ("alpha-main", "charlie-30"):
            self.assertNotIn(n, [resolve(name_of(c)) for c in find_cards(self.t.snap())])


class Polling(Base):
    def test_the_label_of_a_card_that_vanished_is_ignored_and_flash_goes_on(self):
        self.flash("br")
        row = next(c.r for c in find_cards(self.t.snap()) if resolve(name_of(c)) == "bravo-29") + 1
        lab = next(ch for r, c, ch in self.label_cells() if r == row)
        S = json.loads((self.env.fake / "show.json").read_text())
        self.env.write_show([s for s in S if s["id"] != "b1"])
        n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.5)
        self.t.keys(lab); time.sleep(0.5)
        self.assertTrue(self.t.alive()); self.assertTrue(self.in_flash(), "ignored: still waiting"); self.assertEqual(self.env.actions(), [])
        self.assertEqual(self.message_row(), "s br▏", "the dead label did not enter the typed text either (it is not text: it is a label that no longer exists)")
        self.assertEqual(len(find_cards(self.t.snap())), 8)
        self.t.esc(); time.sleep(0.4); self.assertEqual(self.focused(), ["alpha-main"])

    def test_a_new_card_shifting_the_layout_moves_hits_and_labels_with_their_cards(self):
        self.flash("br")
        labels_before = {resolve(name_of(c)): [ch for r, col, ch in self.label_cells() if c.r < r < c.r + 4 and c.c <= col < c.c + c.w][0]
                         for c in find_cards(self.t.snap()) if resolve(name_of(c)) in ("bravo-29", "bravo-bg")}
        pos_before = {resolve(name_of(c)): (c.r, c.c) for c in find_cards(self.t.snap())}
        S = json.loads((self.env.fake / "show.json").read_text()); base = S[0]
        new = dict(base, id="aa", summary="aaa-new", project="aaa", cwd=f"{self.env.home}/work/aaa", mainRepoRoot=f"{self.env.home}/work/aaa", gitBranch="main", tmuxPane="%199",
                   status="idle", subagents=[], pendingTool=None, nativeSessionId="n-aa", logPath=None)
        self.env.write_show(S + [new]); n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.8)
        cs = {resolve(name_of(c)): c for c in find_cards(self.t.snap())}
        self.assertNotEqual({k: (cs[k].r, cs[k].c) for k in labels_before}, {k: pos_before[k] for k in labels_before}, "the layout really moved")
        for n_, lab in labels_before.items():
            c = cs[n_]
            self.assertEqual(self.hits(c.rows[1]), "br", f"{n_}: the yellow follows the card to its new place")
            now = [ch for r, col, ch in self.label_cells() if c.r < r < c.r + 4 and c.c <= col < c.c + c.w]
            self.assertEqual(now, [lab], f"{n_}: the same letter, drawn at the new place")
        self.assertEqual(len(self.label_cells()), 3)
        self.t.keys(labels_before["bravo-bg"]); time.sleep(0.5)
        self.assertEqual(self.focused(), ["bravo-bg"], "the label still jumps to the right card")

    def test_a_card_gone_and_a_new_one_in_the_same_poll_do_not_swap_letters(self):
        self.flash("b")
        row = next(c.r for c in find_cards(self.t.snap()) if resolve(name_of(c)) == "bravo-29") + 1
        old = next(ch for r, c, ch in self.label_cells() if r == row)
        S = json.loads((self.env.fake / "show.json").read_text()); base = S[0]
        new = dict(base, id="bn", summary="bnew-card", project="bnew", cwd=f"{self.env.home}/work/bnew", mainRepoRoot=f"{self.env.home}/work/bnew", gitBranch="main", tmuxPane="%198",
                   status="idle", subagents=[], pendingTool=None, nativeSessionId="n-bn", logPath=None)
        self.env.write_show([s for s in S if s["id"] != "b1"] + [new])  # one poll: bravo-29 disappears, a card that also contains `b` appears
        n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.6)
        cs = {resolve(name_of(c)): c for c in find_cards(self.t.snap())}
        self.assertIn("bnew-card", cs, "the new card is on screen"); nc = cs["bnew-card"]
        mine = [ch for r, col, ch in self.label_cells() if nc.r < r < nc.r + 4 and nc.c <= col < nc.c + nc.w]
        self.assertEqual(len(mine), 1, "the new card has a label"); self.assertNotEqual(mine[0], old, "but not the letter the user saw on the card that just vanished")
        self.assertNotIn(old, [ch for _, _, ch in self.label_cells()], "nobody has it")
        self.t.keys(old); time.sleep(0.5)
        self.assertTrue(self.in_flash()); self.assertEqual(self.message_row(), "s b▏", "pressing the freed letter is ignored: not text, not a jump")
        self.assertEqual(self.env.actions(), [])


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
        self.flash("al"); self.t.esc(); time.sleep(0.4)
        self.assertTrue(self.t.alive()); self.assertEqual(len(find_cards(self.t.snap())), 3, "the filter is still on")
        self.assertIn("j/k h/l move", self.bar())

    def test_a_kept_filter_clears_on_esc_even_when_quit_is_only_q(self):
        self.config('[keys]\nquit = ["q"]\n'); self.t.close(); self.t = self.board()
        self.t.keys("/"); self.t.lit("alpha"); self.t.keys("Enter"); time.sleep(0.5); self.assertEqual(len(find_cards(self.t.snap())), 3)
        self.t.esc(); time.sleep(0.5)
        self.assertEqual(len(find_cards(self.t.snap())), 9, "esc clears the filter although it is not a quit key here"); self.assertTrue(self.t.alive())
        self.t.esc(); time.sleep(0.4); self.assertTrue(self.t.alive(), "and esc is not quit in this config")
        self.t.keys("q"); self.assertEqual(self.t.exit_status(), 0)

    def test_two_escapes_in_a_row_are_alt_esc_and_do_not_clear_the_filter(self):
        self.t.keys("/"); self.t.lit("alpha"); self.t.keys("Enter"); time.sleep(0.5)
        self.t.lit("\x1b\x1b"); time.sleep(0.6)
        self.assertEqual(len(find_cards(self.t.snap())), 3, "alt+esc is not esc: the filter stays")
        self.assertIn("/ alpha", self.t.snap()[0].text)


class Config(Base):
    def test_flash_key_is_configurable(self):
        self.config('[keys]\nflash = ["g"]\n'); self.t.close(); self.t = self.board()
        self.assertIn("g flash", self.bar()); self.assertNotIn("s flash", self.bar())
        self.t.keys("s"); time.sleep(0.4); self.assertFalse(self.in_flash(), "s is not bound any more")
        self.t.keys("g"); self.assertTrue(wait(self.in_flash, 3), self.t.text())
        self.t.lit("br"); time.sleep(0.4); self.assertEqual(len(self.label_cells()), 3); self.assertEqual(self.message_row(), "g br▏", "the message row shows the configured key")

    def test_dim_colour_follows_the_muted_setting(self):
        self.config('[colors]\nmuted = "#336699"\n'); self.t.close(); self.t = self.board(); self.flash()
        c = find_cards(self.t.snap())[1]
        self.assertTrue({x[1] for r in c.rows[1:4] for x in r.cells[1:-1] if x[0].strip()} == {("rgb", 51, 102, 153)}, "dimmed content uses colors.muted")


class InPassthrough(PreviewBase):
    PANES = _t12.PassthroughReview.PANES
    cat, enter_passthrough_on_a3, preview_focused = _t12.PassthroughReview.cat, _t12.PassthroughReview.enter_passthrough_on_a3, _t12.PassthroughReview.preview_focused

    def test_s_is_forwarded_to_the_pane(self):
        self.enter_passthrough_on_a3(); self.t.lit("s"); time.sleep(0.5)
        self.assertEqual(self.cat("a3"), "s"); self.assertNotIn(PROMPT, self.t.text()); self.assertTrue(self.preview_focused())
        self.t.keys("Tab"); time.sleep(0.4); self.t.keys("s"); time.sleep(0.4)
        self.assertIn(PROMPT, self.t.text(), "back on the cards, s starts the flash")


if __name__ == "__main__":
    unittest.main()
