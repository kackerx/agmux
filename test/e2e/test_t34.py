"""T34 the cards scroll: the wheel over the cards moves them 3 lines at a time without moving the focus, ctrl+d / ctrl+u (page_down / page_up) half a body, both stop at the ends;
the manual offset stays until a key moves the focus out of view, then the view follows the focus again; the top bar says ` · ↑ more` / ` · ↓ more` when cards are cut off.
Spec: specs/m3-config/tasks.md「第六批」T34. Fixture: 22 sessions m00..m21 (6 per project group) on a 120 x 30 screen: two columns, body 27 lines, half a body = 13."""
import json, re, subprocess, unittest
from lib import *
from test_t3 import ready, border_colours, C_FOCUS

BODY_H, HALF = 27, 13  # h - 3 (top bar, message line, footer); half of it, rounded down


def name_rows(snap, maxcol=None):
    """{card name: screen row} for the m00.. cards whose title line is on screen (cut-off cards are fine: only the name row matters); maxcol: only look left of that column."""
    out = {}
    for i, r in enumerate(snap):
        text = "".join(c[0] for c in (r.cells[:maxcol] if maxcol else r.cells))
        for m in re.findall(r"(?<![\w-])(m\d\d)(?![\w-])", text): out[m] = i
    return out


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.names = build_many_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def board(self, w=120, h=30, prime=True, config=None):
        if config: (self.cfg / "agmux" / "config.toml").write_text(config)
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(name_rows(t.snap())) >= 6, 8), t.text()); time.sleep(0.4)
        if prime: t.keys("l"); time.sleep(0.15); t.keys("h"); time.sleep(0.25)  # the very first scroll after start is test_the_first_scroll_after_start_is_not_lost's business
        return t

    def rows(self, t, maxcol=None): return name_rows(t.snap(), maxcol)
    def top(self, t): return t.snap()[0].text
    def wheel(self, t, n=1, up=False, x=10, y=10):
        for _ in range(n): t.wheel(x, y, up=up); time.sleep(0.12)
        time.sleep(0.25)

    def focused_name(self, t):
        """{the focused card's name}: the name on a row where the thick pink border is, between its left and right border (it need not be fully on screen)"""
        snap = parse_screen(t.raw()); hits = set()
        for r in snap:
            pink = [i for i, c in enumerate(r.cells) if c[0] in "┏┃┓" and c[1] == C_FOCUS]
            if len(pink) >= 2:
                text = "".join(c[0] for c in r.cells[pink[0]:pink[-1] + 1]); hits |= set(re.findall(r"(?<![\w-])(m\d\d)(?![\w-])", text))
        return hits

    def shifted(self, before, after, by):
        """every card on screen both times moved up by `by` lines"""
        common = set(before) & set(after); self.assertTrue(common, f"no card on screen in both views: {before} / {after}")
        self.assertEqual({n: before[n] - after[n] for n in common}, {n: by for n in common}, f"scrolled by {by} lines: {before} -> {after}")


class Wheel(Base):
    def test_three_lines_per_notch_the_focus_stays(self):
        t = self.board(); r0 = self.rows(t); self.assertEqual(self.focused_name(t), {"m00"})
        self.wheel(t); r1 = self.rows(t); self.shifted(r0, r1, 3); self.wheel(t); r2 = self.rows(t); self.shifted(r1, r2, 3)
        self.wheel(t, 3); self.shifted(r2, self.rows(t), 9)
        self.assertNotIn("m00", self.rows(t), "m00, the focused card, is scrolled off the top by now"); t.keys("j"); time.sleep(0.3); self.assertEqual(self.focused_name(t), {"m01"}, "the focus did not move while scrolling: j goes m00 -> m01")

    def test_up_goes_back_and_stops_at_the_top(self):
        t = self.board(); r0 = self.rows(t); self.wheel(t, 4); self.assertNotEqual(self.rows(t), r0); self.wheel(t, 4, up=True); self.assertEqual(self.rows(t), r0, "4 down, 4 up: back where it was")
        self.wheel(t, 5, up=True); self.assertEqual(self.rows(t), r0, "no further up than the first line"); self.assertNotIn("↑", self.top(t))

    def test_down_stops_at_the_last_card(self):
        t = self.board(); self.wheel(t, 30); r = self.rows(t); self.assertIn("m21", r); self.assertIn("m17", r); last = max(r.values())
        self.assertLessEqual(last, 28 - 1, "the last card is fully above the message line"); self.wheel(t, 5); self.assertEqual(self.rows(t), r, "one more notch at the bottom changes nothing")
        top = self.top(t); self.assertIn("↑ more", top); self.assertNotIn("↓ more", top)
        card_bottoms = [i for i, row in enumerate(t.snap()) if "╰" in row.text or "┗" in row.text]; self.assertTrue(card_bottoms and max(card_bottoms) <= 27, "the bottom border of the last cards is on screen")

    def test_wheel_over_the_empty_right_side_without_a_preview_also_scrolls_the_cards(self):
        t = self.board(w=200, h=30); r0 = self.rows(t); self.wheel(t, x=190, y=10); self.shifted(r0, self.rows(t), 3)

    def test_a_click_after_scrolling_picks_the_card_under_the_pointer(self):
        t = self.board(); self.wheel(t, 2); r = self.rows(t); name = "m02"; self.assertIn(name, r)
        t.click(10, r[name]); time.sleep(0.4); self.assertEqual(self.focused_name(t), {name}); self.shifted(r, self.rows(t), 0)


class Pages(Base):
    def test_ctrl_d_and_ctrl_u_move_half_a_body_without_moving_the_focus(self):
        t = self.board(); r0 = self.rows(t); t.keys("C-d"); time.sleep(0.4); r1 = self.rows(t); self.shifted(r0, r1, HALF)
        t.keys("C-d"); time.sleep(0.4); r2 = self.rows(t); self.shifted(r1, r2, HALF)
        t.keys("C-u"); time.sleep(0.4); self.shifted(r2, self.rows(t), -HALF); t.keys("C-u"); time.sleep(0.4); self.assertEqual(self.rows(t), r0); self.assertEqual(self.focused_name(t), {"m00"}, "the focus never moved")

    def test_pages_stop_at_both_ends(self):
        t = self.board(); r0 = self.rows(t)
        for _ in range(8): t.keys("C-d"); time.sleep(0.15)
        time.sleep(0.3); r = self.rows(t); self.assertIn("m21", r); self.assertNotIn("↓ more", self.top(t)); t.keys("C-d"); time.sleep(0.4); self.assertEqual(self.rows(t), r, "no further")
        for _ in range(8): t.keys("C-u"); time.sleep(0.15)
        time.sleep(0.3); self.assertEqual(self.rows(t), r0); t.keys("C-u"); time.sleep(0.4); self.assertEqual(self.rows(t), r0)

    def test_pages_and_the_wheel_add_up(self):
        t = self.board(); r0 = self.rows(t); t.keys("C-d"); time.sleep(0.3); self.wheel(t, 2); self.shifted(r0, self.rows(t), HALF + 6); t.keys("C-u"); time.sleep(0.3); self.shifted(r0, self.rows(t), 6)


class TopBar(Base):
    def test_the_hint_has_three_states(self):
        t = self.board(); self.assertEqual(self.top(t), " agmux · 22 sessions · ↓ more", "cards below, nothing above")
        self.wheel(t, 3); self.assertEqual(self.top(t), " agmux · 22 sessions · ↑ more · ↓ more", "both")
        self.wheel(t, 30); self.assertEqual(self.top(t), " agmux · 22 sessions · ↑ more", "only above, at the bottom")
        self.wheel(t, 40, up=True); self.assertEqual(self.top(t), " agmux · 22 sessions · ↓ more", "back at the top")

    def test_no_hint_when_everything_fits(self):
        env = self.env; env.cleanup(); self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        t = Term(self.env, w=200, h=50); self.addCleanup(t.close); self.assertTrue(ready(t)); self.assertEqual(self.top(t), " agmux · 9 sessions")

    def test_the_hint_follows_the_focus_scrolling_too(self):
        t = self.board(); self.assertIn("↓ more", self.top(t))
        for _ in range(8): t.keys("j"); time.sleep(0.12)  # down the first column, the view follows the focus: cards above are cut off now
        time.sleep(0.3); self.assertIn("↑ more", self.top(t))

    def test_while_filtering_the_top_bar_is_the_filter(self):
        t = self.board(); self.wheel(t, 2); t.keys("/"); time.sleep(0.3); t.lit("m1"); time.sleep(0.4); self.assertNotIn("more", self.top(t)); t.esc(); time.sleep(0.3)


class Following(Base):
    def test_the_first_scroll_after_start_is_not_lost(self):
        t = self.board(prime=False); r0 = self.rows(t); self.wheel(t); self.shifted(r0, self.rows(t), 3)

    def test_the_first_page_key_after_start_is_not_lost(self):
        t = self.board(prime=False); r0 = self.rows(t); t.keys("C-d"); time.sleep(0.4); self.shifted(r0, self.rows(t), HALF)

    def test_keys_that_keep_the_focus_in_view_leave_the_manual_offset_alone(self):
        t = self.board(); self.wheel(t, 2); r = self.rows(t); self.assertNotIn("m00", r, "m00 (the focus) is scrolled off")
        t.keys("j"); time.sleep(0.4); self.assertEqual(self.focused_name(t), {"m01"}); self.assertEqual(self.rows(t), r, "m01 is in view: the offset stays")
        t.keys("j"); time.sleep(0.4); self.assertEqual(self.focused_name(t), {"m02"}); self.assertEqual(self.rows(t), r)
        t.keys("l"); time.sleep(0.4); self.assertEqual(self.rows(t), r, "so does a move into the other column while the focus is in view")

    def test_a_key_that_puts_the_focus_out_of_view_brings_the_view_back_to_it(self):
        t = self.board(); self.wheel(t, 30); self.assertIn("m21", self.rows(t))
        t.keys("j"); time.sleep(0.5); self.assertEqual(self.focused_name(t), {"m01"}); r = self.rows(t)
        self.assertIn("m01", r); self.assertGreater(r["m01"], 0); self.assertIn("m00", r, "following the focus: back at the top where m01 lives"); self.assertNotIn("↑ more", self.top(t))

    def test_k_up_to_a_card_above_the_view_follows_it_too(self):
        t = self.board()
        for _ in range(12): t.keys("j"); time.sleep(0.1)  # the focus goes down the column (view follows)
        time.sleep(0.3); t.keys("C-u"); time.sleep(0.3); t.keys("C-u"); time.sleep(0.3)  # scroll away from the focus
        t.keys("k"); time.sleep(0.5); name = list(self.focused_name(t))[0]; self.assertIn(name, self.rows(t), "the focused card is in view after k")

    def test_ctrl_d_does_not_move_the_focus_so_j_afterwards_moves_from_where_it_was(self):
        t = self.board(); t.keys("C-d"); time.sleep(0.4); t.keys("j"); time.sleep(0.4); self.assertEqual(self.focused_name(t), {"m01"}, "j goes from m00 to m01, not from what is on screen")


class Polled(Base):
    """The focused card disappears from ccmux's list while the board runs: the focus falls back to the first card and the view follows it (no stale manual offset); scrolling keeps working."""

    def drop(self, name):
        S = json.loads((self.env.fake / "show.json").read_text()); self.env.write_show([s for s in S if s["summary"] != name])

    def test_the_focus_card_deleted_by_a_poll_resets_the_view_to_following(self):
        t = self.board(); self.wheel(t, 3); self.assertNotIn("m00", self.rows(t)); self.drop("m00")
        self.assertTrue(wait(lambda: "21 sessions" in self.top(t), 8), self.top(t)); time.sleep(0.5); r = self.rows(t)
        self.assertEqual(self.focused_name(t), {"m01"}, "the focus fell back to the first card"); self.assertIn("m01", r); self.assertLess(r["m01"], 6, "the view is back at the top, where the focus is")

    def test_the_first_scroll_after_the_focus_card_was_deleted_works(self):
        t = self.board(prime=False); self.drop("m00"); self.assertTrue(wait(lambda: "21 sessions" in self.top(t), 8), self.top(t)); time.sleep(0.5)
        r0 = self.rows(t); self.wheel(t); self.shifted(r0, self.rows(t), 3, ) ; self.wheel(t, 2, up=True)
        t.keys("C-d"); time.sleep(0.4); r1 = self.rows(t); self.assertNotEqual(r1, r0, "ctrl+d works right after as well")

    def test_a_scroll_right_after_the_poll_dropped_the_focus_card_ctrl_d_too(self):
        t = self.board(prime=False); self.drop("m00"); self.assertTrue(wait(lambda: "21 sessions" in self.top(t), 8), self.top(t)); time.sleep(0.5)
        r0 = self.rows(t); t.keys("C-d"); time.sleep(0.4); self.shifted(r0, self.rows(t), HALF)

    def test_a_card_removed_somewhere_else_does_not_disturb_a_manual_scroll(self):
        t = self.board(); self.wheel(t, 4); self.assertTrue(self.rows(t)); self.drop("m20"); self.assertTrue(wait(lambda: "21 sessions" in self.top(t), 8)); time.sleep(0.5)
        self.assertEqual(self.focused_name(t) | {"x"}, {"x"}, "the focus card m00 is still scrolled off"); self.assertNotIn("m00", self.rows(t)); self.assertIn("↑ more", self.top(t))


class FlashMode(Base):
    def test_the_wheel_does_not_move_the_cards_while_flash_labels_are_up(self):
        t = self.board(); t.keys("s"); time.sleep(0.3); t.lit("m1"); time.sleep(0.4); r0 = self.rows(t); self.assertTrue(r0)
        self.wheel(t, 3); self.assertEqual(self.rows(t), r0, "flash mode: labels belong to the cards where they are; the wheel is ignored"); t.keys("C-d"); time.sleep(0.3); self.assertEqual(self.rows(t), r0, "so is ctrl+d")
        t.esc(); time.sleep(0.3); self.wheel(t); self.shifted(r0, self.rows(t), 3)


class ClickAfterScroll(Base):
    def where(self, t, name):
        for i, r in enumerate(t.snap()):
            x = r.find(name)
            if x >= 0: return i, x
        return None

    def test_every_visible_card_is_hit_where_it_is_drawn(self):
        t = self.board(); self.wheel(t, 5); names = sorted(self.rows(t)); self.assertGreaterEqual(len(names), 6); hit = 0
        for name in names:
            self.wheel(t, 5, up=True); self.wheel(t, 5)  # same view again (a click that moved the focus may have re-followed)
            pos = self.where(t, name)
            if not pos: continue
            t.click(pos[1] + 2, pos[0]); time.sleep(0.3); self.assertEqual(self.focused_name(t), {name}, f"click on {name} at {pos}"); hit += 1
        self.assertGreaterEqual(hit, 6, "most of the visible cards were really clicked")


class WithPreview(Base):
    def test_the_wheel_over_the_preview_scrolls_the_preview_not_the_cards(self):
        t = self.board(); t.keys("p"); time.sleep(0.6); r0 = self.rows(t, 50); self.assertTrue(r0)
        self.wheel(t, 3, x=110, y=10); self.assertEqual(self.rows(t, 50), r0, "x in the preview: the cards stay")
        self.wheel(t, 2, x=5, y=10); self.shifted(r0, self.rows(t, 50), 6)


class Config(Base):
    def cli(self, *a):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *a], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def test_config_prints_the_new_actions_after_right(self):
        out = self.cli("config").stdout; self.assertIn('right = ["l", "right"]\npage_down = ["ctrl+d"]\npage_up = ["ctrl+u"]\njump = ["enter"]', out)

    def test_the_keys_are_configurable(self):
        t = self.board(config='[keys]\npage_down = ["ctrl+f"]\npage_up = ["ctrl+b"]\n'); r0 = self.rows(t)
        t.keys("C-d"); time.sleep(0.4); self.assertEqual(self.rows(t), r0, "ctrl+d is no longer bound")
        t.keys("C-f"); time.sleep(0.4); r1 = self.rows(t); self.shifted(r0, r1, HALF); t.keys("C-b"); time.sleep(0.4); self.assertEqual(self.rows(t), r0)

    def test_a_default_key_taken_by_another_explicit_binding_leaves_page_down_unbound(self):
        (self.cfg / "agmux" / "config.toml").write_text('[keys]\nquit = ["q", "ctrl+d"]\n'); r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("# page_down: no key", r.stdout); self.assertIn('quit = ["q", "ctrl+d"]', r.stdout)

    def test_unknown_page_action_names_are_rejected_like_any_other(self):
        (self.cfg / "agmux" / "config.toml").write_text('[keys]\npage_dwon = ["x"]\n'); r = self.cli("config"); self.assertEqual(r.returncode, 1); self.assertIn("page_dwon", r.stderr)

    def test_readme_documents_the_keys(self):
        readme = (SRC / "README.md").read_text(); self.assertRegex(readme, r"ctrl\+d.*ctrl\+u")
        self.assertIn("more", readme)


if __name__ == "__main__":
    unittest.main()
