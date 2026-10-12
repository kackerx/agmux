"""T22 search highlight = yellow background + n / N between the matches. Spec: specs/m3-config/tasks.md T22, design.md「`/` 模糊搜索过滤」.

The matched characters are drawn as colors.match (#f2d06b) background with dark text (#2d2a2e, which lipgloss rounds to 44;42;46); after enter keeps the filter,
`n` / `N` move the focus to the next / previous matching card in layout order and wrap around; without a kept filter they do nothing.
"""
import json, re, subprocess, unittest
from lib import *
from test_t3 import ready, last_text_row, border_colours, C_FOCUS, NAMES, by_name

MATCH, INK = ("rgb", 242, 208, 107), ("rgb", 44, 42, 46)
LAYOUT = ["alpha-main", "pj-fallback", "告警会话-超长中文名字-用来测试宽", "bravo-29", "bravo-bg", "charlie-30", "charlie-59", "delta-60", "echo-long"]  # groups left to right, cards top to bottom


def name_of(c):
    return c.line(1)[8:-6].strip()


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        self.t = self.board()

    def config(self, text):
        (self.cfg / "agmux" / "config.toml").write_text(text)

    def board(self, w=200, h=50):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(ready(t), t.text()); return t

    def cli(self, *args):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def search(self, q, enter=True):
        self.t.keys("/"); time.sleep(0.15); self.t.lit(q); time.sleep(0.45)
        if enter:
            self.t.keys("Enter"); time.sleep(0.4)

    def focused(self):
        return [name_of(c) for c in find_cards(self.t.snap()) if border_colours(c) == {C_FOCUS}]

    def hits(self, row):
        """the characters of a row on the match background, and the foreground of those cells"""
        cells = [c for c in row.cells if c[2] == MATCH and c[0].strip()]
        return "".join(c[0] for c in cells), {c[1] for c in cells}

    def bar(self):
        return last_text_row(self.t.snap())


class Colours(Base):
    def test_matches_are_yellow_background_with_dark_text_while_typing_and_when_kept(self):
        for enter in (False, True):
            with self.subTest(kept=enter):
                self.search("brav", enter=enter)
                cs = {name_of(c): c for c in find_cards(self.t.snap())}
                for n in ("bravo-29", "bravo-bg"):  # the focused card too: the highlight is not the focus colour
                    txt, fg = self.hits(cs[n].rows[1]); self.assertEqual(txt, "brav", n); self.assertEqual(fg, {INK}, f"{n}: dark text on the yellow")
                self.assertIn("48;2;242;208;107", self.t.raw(), "the escape sequence the spec names")
                self.t.keys("Escape"); time.sleep(0.4)

    def test_nothing_is_pink_text_any_more(self):
        self.search("brav", enter=False)
        for c in find_cards(self.t.snap()):
            for row in c.rows[1:4]:
                for cell in row.cells[1:-1]:
                    self.assertNotEqual((cell[1] == C_FOCUS and cell[0].strip() and cell[2] != MATCH), True, f"pink text outside the border in {c.line(1)!r}")

    def test_directory_matches_in_the_group_title(self):
        self.search("work/alpha", enter=False)
        title = next(r for r in self.t.snap() if "▾ ~/work/alpha" in r.text)
        self.assertEqual(self.hits(title)[0], "work/alpha")

    def test_match_colour_comes_from_the_config(self):
        self.config('[colors]\nmatch = "#00ff00"\n'); self.t.close(); self.t = self.board()
        self.search("brav", enter=False)
        c = next(c for c in find_cards(self.t.snap()) if name_of(c) == "bravo-bg")
        self.assertEqual("".join(x[0] for x in c.rows[1].cells if x[2] == ("rgb", 0, 255, 0) and x[0].strip()), "brav", "the match background follows colors.match")
        self.assertNotIn("48;2;242;208;107", self.t.raw())

    def test_bad_match_colour_is_an_error(self):
        self.config('[colors]\nmatch = "yellow"\n')
        r = self.cli("config"); self.assertEqual(r.returncode, 1); self.assertIn("colors.match", r.stderr)

    def test_dump_has_the_new_keys_and_colour_and_round_trips(self):
        out = self.cli("config").stdout
        for line in ('next_match = ["n"]', 'prev_match = ["N"]', 'match = "#f2d06b"'):
            self.assertIn(line, out)
        self.config(out); r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout, out)


class NextPrev(Base):
    def test_the_footer_of_a_kept_filter(self):
        self.search("a")
        self.assertTrue(self.bar().startswith(" n/N next/prev · esc clear · j/k h/l move"), self.bar())

    def test_the_footer_while_typing_and_without_a_filter_has_no_prefix(self):
        self.assertNotIn("n/N next/prev", self.bar())
        self.t.keys("/"); time.sleep(0.3); self.assertNotIn("n/N next/prev", self.bar())

    def test_n_walks_the_matches_in_layout_order_and_wraps(self):
        self.search("a"); self.assertEqual(self.focused(), ["alpha-main"], "enter lands on the first (best) match")
        seen = []
        for _ in range(10):
            self.t.keys("n"); time.sleep(0.25); seen.append(self.focused()[0])
        self.assertEqual(seen, LAYOUT[1:] + LAYOUT[:2], "layout order, wrapping after the last match")
        self.assertEqual(self.env.actions(), [], "moving between matches never jumps")

    def test_N_goes_backwards_and_wraps(self):
        self.search("a")
        seen = []
        for _ in range(4):
            self.t.keys("N"); time.sleep(0.25); seen.append(self.focused()[0])
        self.assertEqual(seen, ["echo-long", "delta-60", "charlie-59", "charlie-30"], "from the first match backwards, wrapping to the last")
        self.t.keys("n"); time.sleep(0.25); self.assertEqual(self.focused(), ["charlie-59"], "n after N goes forward again")

    def test_n_only_visits_matching_cards(self):
        self.search("bravo")
        self.assertEqual(self.focused(), ["bravo-29"]); self.t.keys("n"); time.sleep(0.3); self.assertEqual(self.focused(), ["bravo-bg"])
        self.t.keys("n"); time.sleep(0.3); self.assertEqual(self.focused(), ["bravo-29"], "two matches: n wraps")

    def test_enter_after_n_jumps_from_the_new_focus(self):
        self.search("bravo"); self.t.keys("n"); self.t.keys("n"); time.sleep(0.3)
        self.t.keys("Enter"); self.assertTrue(wait(lambda: self.env.actions(), 3)); self.assertEqual(self.env.actions(), [["switch", "b1"]])

    def test_esc_clears_and_then_n_does_nothing(self):
        self.search("a"); self.t.keys("n"); time.sleep(0.3); at = self.focused()
        self.t.esc(); time.sleep(0.5); self.assertEqual(len(find_cards(self.t.snap())), 9)
        self.assertEqual(self.focused(), at, "esc keeps the focus where n left it")
        self.t.keys("n"); time.sleep(0.4); self.assertEqual(self.focused(), at, "n does nothing without a kept filter")
        self.t.keys("N"); time.sleep(0.4); self.assertEqual(self.focused(), at); self.assertTrue(self.t.alive())

    def test_n_and_N_do_nothing_when_no_filter_was_ever_set(self):
        before = self.focused(); self.t.keys("n"); self.t.keys("N"); time.sleep(0.4)
        self.assertEqual(self.focused(), before); self.assertTrue(self.t.alive())

    def test_n_is_text_inside_the_input(self):
        self.t.keys("/"); self.t.lit("nN"); time.sleep(0.3)
        self.assertIn("/ nN▏", self.t.snap()[0].text)

    def test_nothing_to_visit_when_the_kept_filter_has_no_match(self):
        self.search("qqq"); self.t.keys("n"); self.t.keys("N"); time.sleep(0.4)
        self.assertTrue(self.t.alive()); self.assertEqual(find_cards(self.t.snap()), [])

    def test_the_keys_are_configurable(self):
        self.config('[keys]\nnext_match = ["m"]\nprev_match = ["M"]\n'); self.t.close(); self.t = self.board()
        self.search("a"); self.t.keys("n"); time.sleep(0.3); self.assertEqual(self.focused(), ["alpha-main"], "n is not bound any more")
        self.t.keys("m"); time.sleep(0.3); self.assertEqual(self.focused(), ["pj-fallback"]); self.t.keys("M"); time.sleep(0.3); self.assertEqual(self.focused(), ["alpha-main"])
        self.assertIn("m/M next/prev", self.bar())

    def test_a_user_binding_of_n_takes_it_from_next_match_without_an_error(self):  # T17 rule: defaults yield to explicit bindings
        self.config('[keys]\ndown = ["n", "down"]\n')
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("# next_match: no key", r.stdout)
        self.t.close(); self.t = self.board(); self.search("a")
        self.assertNotIn("next/prev", self.bar().split("·")[0], "no key for next_match: the footer does not offer it")


class Review(Base):
    """T22 review (2069326): n / N start from the card that is drawn focused on screen; the footer offers the keys that exist."""

    def test_n_starts_from_the_card_on_screen_when_the_focused_card_stopped_matching(self):
        self.search("a"); self.assertEqual(self.focused(), ["alpha-main"])
        S = json.loads((self.env.fake / "show.json").read_text())
        for x in S:
            if x["id"] == "a1":
                x["summary"] = "zzz-xyz"; x["project"] = "zzz"; x["gitBranch"] = "q"; x["cwd"] = f"{self.env.home}/work/zzz"; x["mainRepoRoot"] = x["cwd"]  # name + branch + directory no longer contain an `a`
        self.env.write_show(S); n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.6)
        shown = [name_of(c) for c in find_cards(self.t.snap())]
        self.assertNotIn("zzz-xyz", shown, "the renamed card stopped matching `a` and left the screen")
        first = self.focused(); self.assertEqual(len(first), 1, self.t.text()[:600])
        self.t.keys("n"); time.sleep(0.3)
        order = [name_of(c) for c in sorted(find_cards(self.t.snap()), key=lambda c: (c.c, c.r))]
        self.assertEqual(self.focused(), [order[1]], "n goes to the SECOND card of the screen: it starts from the card drawn focused (the first), not from a card that is gone")

    def test_n_follows_the_layout_order_not_the_score_order(self):
        self.search("'ravo"); self.assertEqual(self.focused(), ["bravo-29"])
        self.t.keys("n"); time.sleep(0.3); self.assertEqual(self.focused(), ["bravo-bg"])
        self.t.keys("N"); time.sleep(0.3); self.assertEqual(self.focused(), ["bravo-29"])

    def test_footer_offers_only_the_keys_that_exist(self):
        self.config('[keys]\ndown = ["n", "down"]\n'); self.t.close(); self.t = self.board(); self.search("a")
        bar = self.bar(); self.assertIn("N prev", bar); self.assertNotIn("n/N", bar, bar); self.assertNotIn("n next", bar)
        self.config('[keys]\ndown = ["N", "down"]\n'); self.t.close(); self.t = self.board(); self.search("a")
        bar = self.bar(); self.assertIn("n next", bar); self.assertNotIn("prev", bar.split("esc clear")[0], bar); self.assertNotIn("n/N", bar)
        self.config('[keys]\nprev_match = ["ctrl+p"]\n'); self.t.close(); self.t = self.board(); self.search("a")
        self.assertIn("n/ctrl+p next/prev", self.bar())
        self.config('[keys]\nnext_match = ["m"]\nprev_match = ["M"]\n'); self.t.close(); self.t = self.board(); self.search("a")
        self.assertIn("m/M next/prev", self.bar())


if __name__ == "__main__":
    unittest.main()
