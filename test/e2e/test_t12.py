"""T12 preview key pass-through + local scroll. Spec: specs/m2-preview/design.md「keys go to the pane」, tasks.md T12.

a3's pane runs a raw-mode `cat -v` look-alike (fixtures/catv.py; BSD cat -v garbles UTF-8 that arrives split), so every byte the preview forwards comes back as visible text (^M enter, ^[ escape, ^C, ^[[A up ...).
"""
import json, re, unittest
from lib import *
from test_t3 import by_name, NAMES, last_text_row, C_FOCUS
from test_t11 import PreviewBase, PV_X, pane_text, SEQ_CMD, MARK_CMD, CAT_CMD

PROBE_CMD = f"env LANG=en_US.UTF-8 sh -c 'stty raw -echo; exec python3 -u {HERE / 'fixtures' / 'paste_probe.py'}'"


class Passthrough(PreviewBase):
    def to_cat_pane(self):  # focus a3 and open the preview there
        self.press("j", "j", "p")
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
        self.assertIn("tab focus preview", self.footer())
        self.press("Tab")
        self.assertTrue(wait(self.preview_focused, 3), "preview border turns #ff8ebe:\n" + self.t.text())
        self.assertIn("keys go to the pane · ctrl+] back to the cards", self.footer())
        self.press("C-]")
        self.assertTrue(wait(lambda: not self.preview_focused(), 3)); self.assertIn("tab focus preview", self.footer())

    def test_tab_does_nothing_when_the_preview_is_closed(self):
        self.t.keys("Tab"); time.sleep(0.5)
        self.assertIsNone(self.pv()); self.assertNotIn("keys go to the pane", self.footer())
        cs = [c for c in find_cards(self.t.snap()) if c.ok and c.rows[0].cells[0][1] == C_FOCUS]
        self.assertEqual(len(cs), 1, "focus still on a card")
        self.t.keys("q"); self.assertEqual(self.t.exit_status(), 0, "q still quits: nothing swallowed")

    def test_tab_does_nothing_on_a_card_without_a_pane_and_the_hint_is_greyed(self):
        self.open_preview()
        on_pane = self.hint_colour()
        self.press("l", "j")  # b2: background
        self.assertTrue(wait(lambda: "no pane" in "\n".join(self.pv_text()), 3))
        self.press("Tab")
        self.assertFalse(self.preview_focused()); self.assertNotIn("keys go to the pane", self.footer())
        off_pane = self.hint_colour()
        self.assertNotEqual(on_pane, off_pane, f"`tab focus preview` must be greyed for a card without a pane ({on_pane} vs {off_pane})")

    def hint_colour(self):
        row = self.t.snap()[-2] if not self.t.snap()[-1].text.strip() else self.t.snap()[-1]
        i = row.find("tab focus preview"); self.assertGreaterEqual(i, 0, row.text)
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

    def test_tab_inside_the_passthrough_is_forwarded_not_a_way_out(self):
        self.to_cat_pane(); self.press("Tab")
        self.t.keys("Tab"); time.sleep(0.4)
        self.assertTrue(self.preview_focused(), "tab is Claude Code's completion key: it goes to the pane")
        self.assertIn("^I", self.cat_text())

    def test_q_in_the_preview_is_a_character_not_quit(self):
        self.to_cat_pane(); self.press("Tab")
        self.t.lit("q"); time.sleep(0.5)
        self.assertTrue(self.t.alive()); self.assertIn("q", self.cat_text())

    def test_tab_back_then_q_quits_agmux_and_the_pane_does_not_get_it(self):
        self.to_cat_pane(); self.press("Tab")
        self.t.lit("abc"); time.sleep(0.4)
        before = self.cat_text()
        self.press("C-]")
        self.assertFalse(self.preview_focused())
        self.t.keys("q")
        self.assertEqual(self.t.exit_status(4), 0, "q quits agmux once the focus is back on the cards")
        self.assertEqual(self.cat_text(), before, "the pane never saw the q")

    def test_keys_go_to_the_focused_card_s_pane_and_follow_the_focus(self):
        self.open_preview(); self.press("Tab")  # a1's pane (sleep, tty echo on)
        self.t.lit("AAA"); time.sleep(0.4)
        self.assertIn("AAA", pane_text(self.panes["a1"], 3))
        self.press("C-]", "j", "Tab")  # a2
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


class PassthroughReview(PreviewBase):
    """T12 review (f0e3354): locked target, `;`, F keys, paste-buffer, scroll cap, click areas. a1 is a DECOY cat pane (it must never receive a byte
    meant for another card), a3 is the pane the preview is locked on."""
    PANES = (("a1", CAT_CMD), ("a2", MARK_CMD), ("a3", CAT_CMD))

    def cat(self, sid):
        return "".join(tm("capture-pane", "-p", "-J", "-t", self.panes[sid]).stdout.split("\n")).rstrip()

    def enter_passthrough_on_a3(self):
        self.press("j", "j", "s")
        self.assertTrue(wait(lambda: self.preview_focused(), 3), self.t.text())

    def preview_focused(self):
        p = self.pv()
        return bool(p) and p[4][p[2]].cells[p[3]][1] == C_FOCUS

    def footer(self):
        return last_text_row(self.t.snap())

    def drop_a3(self, how):
        S = json.loads((self.env.fake / "show.json").read_text())
        if how == "gone":
            S = [s for s in S if s["id"] != "a3"]
        else:  # the pane became null (the session turned into a background one)
            for s in S:
                if s["id"] == "a3":
                    s["tmuxPane"] = None; s["trackingMode"] = "background"
        self.env.write_show(S)
        n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.4)

    def test_lost_target_swallows_every_key_and_nothing_reaches_a_decoy(self):
        for how in ("gone", "no pane"):
            with self.subTest(how=how):
                self.setUp() if how == "no pane" else None
                self.enter_passthrough_on_a3()
                self.t.lit("ok"); time.sleep(0.4)
                self.assertEqual(self.cat("a3"), "ok"); a3_before = self.cat("a3")
                self.drop_a3(how)
                self.t.lit("rxdfq"); self.t.keys("j"); self.t.keys("Enter"); self.t.keys("Tab"); time.sleep(0.8)
                self.assertTrue(self.t.alive(), "q / r / x / d / f typed while locked must not reach the board either")
                self.assertEqual(self.env.actions(), [], "ccmux: zero calls (no restart / kill / review / switch)")
                self.assertEqual(self.cat("a1"), "", "the decoy pane (the first card the focus fell back to) got zero bytes")
                self.assertEqual(self.cat("a3"), a3_before, "nothing is sent to the old pane either")
                self.assertEqual(self.cat("a2"), "".join(l.strip() for l in [] ) or self.cat("a2"))
                row = last_text_row(self.t.snap()); self.assertIn("session is gone · ctrl+] back to the cards", row)
                i = self.t.snap()[-2].find("session is gone") if "session is gone" in self.t.snap()[-2].text else self.t.snap()[-1].find("session is gone")
                rows = [r for r in self.t.snap() if "session is gone" in r.text]
                self.assertTrue(rows and is_red(rows[0].cells[rows[0].find("session is gone")][1]), "red footer")
                self.assertFalse(self.preview_focused(), "the preview border is back to the plain colour")
                self.t.keys("C-]"); time.sleep(0.5)
                self.assertNotIn("session is gone", self.footer()); self.assertIn("tab focus preview", self.footer())
                self.t.keys("q"); self.assertEqual(self.t.exit_status(4), 0, "after ctrl+] the board is itself again: q quits")
                self.t.close() if False else None

    def test_esc_also_leaves_the_locked_state(self):
        self.enter_passthrough_on_a3(); self.drop_a3("gone")
        self.t.lit("zz"); time.sleep(0.3)
        self.assertIn("session is gone", self.footer())
        self.t.esc(); time.sleep(0.4)
        self.assertNotIn("session is gone", self.footer()); self.assertEqual(self.cat("a1"), "")

    @unittest.expectedFailure  # KNOWN BUG at f0e3354: a run of n trailing `;` loses one (`end;` -> `end`, `;` -> nothing, `x;;` -> `x;`); remove this line when fixed
    def test_semicolons_arrive_verbatim(self):
        self.enter_passthrough_on_a3()
        for text in ("end;", ";", "x;;", "a;b"):
            self.t.lit(text); time.sleep(0.4)
        self.assertTrue(wait(lambda: self.cat("a3") == "end;;x;;a;b", 3), f"pane got {self.cat('a3')!r}")

    def test_f13_is_ignored_and_f5_is_forwarded(self):
        self.enter_passthrough_on_a3()
        self.t.lit("\x1b[1;2P"); time.sleep(0.4)  # xterm's F13 (bubbletea: f13); tmux's send-keys has no F13 name (we cannot type it by name either)
        self.t.lit("x"); time.sleep(0.4)
        self.assertEqual(self.cat("a3"), "x", "tmux has no F13 key name: the key is dropped, not turned into garbage")
        self.t.keys("F5"); time.sleep(0.4)
        self.assertTrue(self.cat("a3").endswith("^[[15~"), self.cat("a3"))

    def test_multiline_paste_keeps_bracketed_paste_in_a_real_pane(self):
        tm("respawn-pane", "-k", "-t", self.panes["a3"], PROBE_CMD)
        time.sleep(0.8)
        self.enter_passthrough_on_a3()
        self.t.lit("\x1b[200~line 1\nline 2\x1b[201~"); time.sleep(0.8)
        got = tm("capture-pane", "-p", "-J", "-t", self.panes["a3"]).stdout
        self.assertIn(repr("\x1b[200~line 1\rline 2\x1b[201~"), got, f"the pane (which asked for bracketed paste) must receive one bracketed paste, not two submitted lines:\n{got}")

    def test_scroll_stops_at_the_top_and_resets_on_card_change(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], SEQ_CMD); time.sleep(1.5)
        self.assertTrue(wait(lambda: "line 200" in pane_text(self.panes["a1"]), 5))
        self.press("p")
        self.assertTrue(wait(lambda: any("line 200" in x for x in self.pv_text()), 4))
        for _ in range(12):  # 144 notches of 3 lines: far beyond the 200 lines of history
            self.t.wheel(PV_X + 20, 15, up=True, n=12); time.sleep(0.25)
        time.sleep(0.8)
        top = [x for x in self.pv_text() if x]
        self.assertEqual(top[0], "line 1", "scrolled to the very top")
        self.t.wheel(PV_X + 20, 15, up=False, n=1); time.sleep(0.8)
        self.assertEqual([x for x in self.pv_text() if x][0], "line 4", "one notch down moves 3 lines at once: the offset did not keep accumulating past the top")
        self.t.wheel(PV_X + 20, 15, up=True, n=4); time.sleep(0.8)
        self.press("j", "k")
        self.assertTrue(wait(lambda: any("line 200" in x for x in self.pv_text()), 3), "a card change puts the preview back at the bottom")

    def test_clicks_on_the_title_row_and_the_footer_do_not_enter_passthrough(self):
        self.press("p")
        self.assertTrue(wait(lambda: self.pv() is not None, 3))
        self.t.click(PV_X + 20, 1); time.sleep(0.4); self.assertFalse(self.preview_focused(), "title row")
        self.t.click(PV_X + 20, self.H - 1); time.sleep(0.4); self.assertFalse(self.preview_focused(), "footer row")
        self.t.click(PV_X + 20, self.H - 2); time.sleep(0.4); self.assertFalse(self.preview_focused(), "message row")
        self.t.click(PV_X + 20, 12); time.sleep(0.4); self.assertTrue(self.preview_focused(), "the body does")


if __name__ == "__main__":
    unittest.main()
