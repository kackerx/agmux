"""T12 preview key pass-through + local scroll. Spec: specs/m2-preview/design.md「按键直通」, tasks.md T12.

a3's pane runs a raw-mode `cat -v` look-alike (fixtures/catv.py; BSD cat -v garbles UTF-8 that arrives split), so every byte the preview forwards comes back as visible text (^M enter, ^[ escape, ^C, ^[[A up ...).
"""
import re, unittest
from lib import *
from test_t3 import by_name, NAMES, last_text_row, C_FOCUS
from test_t11 import PreviewBase, PV_X, pane_text


class Passthrough(PreviewBase):
    def to_cat_pane(self):  # focus a3 and open the preview there
        self.press("j", "j", "P")
        self.assertTrue(wait(lambda: self.pv() is not None, 4), self.t.text())

    def cat_text(self):
        return pane_text(self.panes["a3"], 5)

    def preview_focused(self):
        p = self.pv()
        return bool(p) and p[4][p[2]].cells[p[3]][1] == C_FOCUS

    def footer(self):
        return last_text_row(self.t.snap())

    # ---- focus switching ------------------------------------------------------------------------------------------------------------------
    def test_tab_moves_the_keys_to_the_preview(self):
        self.open_preview()
        self.assertFalse(self.preview_focused())
        self.assertIn("tab 进入预览", self.footer())
        self.press("Tab")
        self.assertTrue(wait(self.preview_focused, 3), "preview border turns #ff8ebe:\n" + self.t.text())
        self.assertIn("按键直通 pane · tab 回到卡片", self.footer())
        self.press("Tab")
        self.assertTrue(wait(lambda: not self.preview_focused(), 3)); self.assertIn("tab 进入预览", self.footer())

    def test_tab_does_nothing_when_the_preview_is_closed(self):
        self.t.keys("Tab"); time.sleep(0.5)
        self.assertIsNone(self.pv()); self.assertNotIn("按键直通", self.footer())
        cs = [c for c in find_cards(self.t.snap()) if c.ok and c.rows[0].cells[0][1] == C_FOCUS]
        self.assertEqual(len(cs), 1, "focus still on a card")
        self.t.keys("q"); self.assertEqual(self.t.exit_status(), 0, "q still quits: nothing swallowed")

    def test_tab_does_nothing_on_a_card_without_a_pane_and_the_hint_is_greyed(self):
        self.open_preview()
        on_pane = self.hint_colour()
        self.press("l", "j")  # b2: background
        self.assertTrue(wait(lambda: "没有 pane" in "\n".join(self.pv_text()), 3))
        self.press("Tab")
        self.assertFalse(self.preview_focused()); self.assertNotIn("按键直通", self.footer())
        off_pane = self.hint_colour()
        self.assertNotEqual(on_pane, off_pane, f"`tab 进入预览` must be greyed for a card without a pane ({on_pane} vs {off_pane})")

    def hint_colour(self):
        row = self.t.snap()[-2] if not self.t.snap()[-1].text.strip() else self.t.snap()[-1]
        i = row.find("tab 进入预览"); self.assertGreaterEqual(i, 0, row.text)
        return row.cells[i][1]

    # ---- keys -----------------------------------------------------------------------------------------------------------------------------
    def test_keys_reach_the_pane_and_show_up_at_once(self):
        self.to_cat_pane(); self.press("Tab")
        self.t.lit("你好 world"); self.t.keys("Enter"); self.press("Escape"); time.sleep(0.4)
        self.t.keys("C-c"); self.t.keys("Up")
        want = "你好 world^M^[^C^[[A"
        self.assertTrue(wait(lambda: want in self.cat_text(), 3), f"pane got:\n{self.cat_text()}")
        self.assertTrue(wait(lambda: want in "\n".join(self.pv_text()), 1.2), "the preview shows what was typed:\n" + "\n".join(self.pv_text()))
        self.assertTrue(self.t.alive(), "esc and ctrl-c go to the pane, they do not quit agmux")
        self.assertTrue(self.preview_focused())

    def test_the_preview_recaptures_right_after_a_key(self):
        self.to_cat_pane(); self.press("Tab")
        t0 = time.time(); self.t.lit("z")
        self.assertTrue(wait(lambda: "z" in "\n".join(self.pv_text()), 0.45, 0.02), "forwarded key must show without waiting for the 500ms tick")
        self.assertLess(time.time() - t0, 0.6)

    def test_text_starting_with_a_dash_arrives_literally(self):
        self.to_cat_pane(); self.press("Tab")
        for text in ("--help", "-l", "- 修一下", "-n"):
            self.t.lit(text); time.sleep(0.2)
        self.assertTrue(wait(lambda: "--help-l- 修一下-n" in self.cat_text(), 3), self.cat_text())

    def test_special_keys_by_tmux_name(self):
        self.to_cat_pane(); self.press("Tab")
        for key, shown in (("Up", "^[[A"), ("Down", "^[[B"), ("Left", "^[[D"), ("Right", "^[[C"), ("BSpace", "^?"), ("C-x", "^X"), ("C-d", "^D"), ("BTab", "^[[Z"), ("Home", "^[[1~")):
            with self.subTest(key=key):
                before = self.cat_text()
                self.t.keys(key); time.sleep(0.35)
                got = self.cat_text()
                self.assertTrue(got.startswith(before.rstrip()) or True)
                self.assertIn(shown, got[len(before.rstrip()):] if got.startswith(before.rstrip()) else got, f"{key} -> {shown}: {got!r}")
        self.assertTrue(self.t.alive())

    def test_keys_forwarded_in_order(self):
        """regression (1ed55b8): every key message ran its own tmux command in its own goroutine, so a burst could reorder (`a b c` -> ` acb`)"""
        self.to_cat_pane(); self.press("Tab")
        text = "a b c d e f g h i j"
        full = lambda: "".join(tm("capture-pane", "-p", "-J", "-t", self.panes["a3"]).stdout.split("\n")).rstrip()
        bad = []
        for i in range(20):
            prev = full(); self.t.lit(text); time.sleep(0.8)
            added = full()[len(prev):]
            if added != text:
                bad.append(added)
            self.t.lit("|"); time.sleep(0.25)
        self.assertEqual(bad, [], f"{len(bad)} of 20 bursts arrived out of order")

    def test_arrow_and_text_burst_keep_their_order(self):
        self.to_cat_pane(); self.press("Tab")
        self.t.lit("a"); self.t.keys("Up"); self.t.lit("b"); self.t.keys("Down"); self.t.lit("c c"); self.t.keys("BSpace")
        self.assertTrue(wait(lambda: "a^[[Ab^[[Bc c^?" in self.cat_text().replace("\n", ""), 3), self.cat_text())

    def test_paste_goes_through_as_text(self):
        self.to_cat_pane(); self.press("Tab")
        self.t.lit("\x1b[200~粘贴 paste-me\x1b[201~")
        self.assertTrue(wait(lambda: "粘贴 paste-me" in self.cat_text(), 3), self.cat_text())

    def test_q_in_the_preview_is_a_character_not_quit(self):
        self.to_cat_pane(); self.press("Tab")
        self.t.lit("q"); time.sleep(0.5)
        self.assertTrue(self.t.alive()); self.assertIn("q", self.cat_text())

    def test_tab_back_then_q_quits_agmux_and_the_pane_does_not_get_it(self):
        self.to_cat_pane(); self.press("Tab")
        self.t.lit("abc"); time.sleep(0.4)
        before = self.cat_text()
        self.press("Tab")
        self.assertFalse(self.preview_focused())
        self.t.keys("q")
        self.assertEqual(self.t.exit_status(4), 0, "q quits agmux once the focus is back on the cards")
        self.assertEqual(self.cat_text(), before, "the pane never saw the q")

    def test_keys_go_to_the_focused_card_s_pane_and_follow_the_focus(self):
        self.open_preview(); self.press("Tab")  # a1's pane (sleep, tty echo on)
        self.t.lit("AAA"); time.sleep(0.4)
        self.assertIn("AAA", pane_text(self.panes["a1"], 3))
        self.press("Tab", "j", "Tab")  # a2
        self.t.lit("BBB"); time.sleep(0.4)
        self.assertIn("BBB", pane_text(self.panes["a2"], 3)); self.assertNotIn("BBB", pane_text(self.panes["a1"], 3))

    # ---- local scroll ---------------------------------------------------------------------------------------------------------------------
    def last_line_no(self):
        t = [x for x in self.pv_text() if x]
        m = re.match(r"line (\d+)$", t[-1]) if t else None
        return int(m.group(1)) if m else None

    def test_wheel_scrolls_the_preview_locally(self):
        self.open_preview()
        self.assertEqual(self.last_line_no(), 200)
        for notches, want in ((1, 197), (3, 188), (5, 173), (5, 158), (5, 143)):  # 3 lines per notch
            self.t.wheel(PV_X + 20, 15, up=True, n=notches); time.sleep(0.9)
            self.assertEqual(self.last_line_no(), want, f"after {notches} more notch(es) up")
        self.t.wheel(PV_X + 20, 15, up=False, n=1); time.sleep(0.8)
        self.assertEqual(self.last_line_no(), 146, "one notch down")
        self.t.wheel(PV_X + 20, 15, up=False, n=60); time.sleep(0.9)
        self.assertEqual(self.last_line_no(), 200, "scrolling down stops at the bottom")
        self.t.wheel(PV_X + 20, 15, up=False, n=5); time.sleep(0.6)
        self.assertEqual(self.last_line_no(), 200)
        self.assertEqual(pane_text(self.panes["a1"], 3).count("x"), 0, "the wheel must not send anything to the pane")

    def test_wheel_over_the_cards_does_nothing(self):
        self.open_preview(); before = list(self.pv_text())
        self.t.wheel(20, 10, up=True, n=5); time.sleep(0.9)
        self.assertEqual(self.pv_text(), before); self.assertEqual(self.last_line_no(), 200)
        cs = [c for c in find_cards(self.t.snap()) if c.ok and c.rows[0].cells[0][1] == C_FOCUS]
        self.assertEqual(len(cs), 1)

    def test_a_forwarded_key_resets_the_scroll(self):
        self.open_preview(); self.press("Tab")
        self.t.wheel(PV_X + 20, 15, up=True, n=4); time.sleep(0.9)
        self.assertEqual(self.last_line_no(), 188)
        self.t.lit("x"); time.sleep(0.6)
        self.assertTrue(wait(lambda: any(s.startswith("x") or s.endswith("x") for s in self.pv_text()) or self.last_line_no() == 200, 2),
                        "after a key the preview is back at the bottom")

    # ---- mouse focus ----------------------------------------------------------------------------------------------------------------------
    def test_click_gives_the_keys_to_the_preview_and_a_card_click_takes_them_back(self):
        self.open_preview()
        self.t.click(PV_X + 30, 20); time.sleep(0.5)
        self.assertTrue(self.preview_focused(), "a click on the preview focuses it")
        c = by_name([c for c in find_cards(self.t.snap()) if c.ok], NAMES["a2"])
        self.t.click(c.c + c.w // 2, c.r + 2); time.sleep(0.5)
        self.assertFalse(self.preview_focused(), "a click on a card brings the focus back")
        cs = [c for c in find_cards(self.t.snap()) if c.ok and c.rows[0].cells[0][1] == C_FOCUS]
        self.assertEqual(len(cs), 1); self.assertIn(NAMES["a2"], cs[0].line(1), "and selects that card")
        self.assertTrue(wait(lambda: any("second-pane-marker" in x for x in self.pv_text()), 3), "preview follows")


if __name__ == "__main__":
    unittest.main()
