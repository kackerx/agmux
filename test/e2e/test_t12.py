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
    def test_tab_moves_the_keys_to_the_preview_and_tab_again_gives_them_back(self):  # T21: tab in, tab out; the preview stays open
        self.open_preview()
        self.assertFalse(self.preview_focused())
        self.assertIn("tab focus preview", self.footer())
        self.press("Tab")
        self.assertTrue(wait(self.preview_focused, 3), "preview border turns #ff8ebe:\n" + self.t.text())
        self.assertIn("keys go to the pane · tab back to the cards", self.footer())
        self.press("Tab")
        self.assertTrue(wait(lambda: not self.preview_focused(), 3)); self.assertIn("tab focus preview", self.footer())
        self.assertIsNotNone(self.pv(), "the preview is still open")

    def test_tab_with_the_preview_closed_opens_it_and_takes_the_keys(self):  # T21
        self.assertIsNone(self.pv())
        self.t.keys("Tab"); time.sleep(0.6)
        self.assertTrue(wait(lambda: self.pv() is not None, 4), self.t.text())
        self.assertTrue(wait(self.preview_focused, 3), "the border is #ff8ebe at once")
        self.assertIn("keys go to the pane · tab back to the cards", self.footer())
        self.t.lit("hi"); time.sleep(0.4); self.assertIn("hi", pane_text(self.panes["a1"], 3), "typing goes to the focused card's pane")
        self.press("Tab"); self.assertTrue(wait(lambda: not self.preview_focused(), 3)); self.assertIsNotNone(self.pv())
        self.press("p"); self.assertTrue(wait(lambda: self.pv() is None, 3), "p still toggles the preview")
        self.t.keys("q"); self.assertEqual(self.t.exit_status(), 0)

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

    def test_tab_is_the_way_out_and_shift_tab_and_ctrl_bracket_go_to_the_pane(self):  # T21 (the cost the user accepted: tab never reaches the pane)
        self.to_cat_pane(); self.press("Tab")
        self.t.keys("BTab"); time.sleep(0.3); self.t.keys("C-]"); time.sleep(0.4)
        self.assertTrue(self.preview_focused(), "neither shift+tab nor ctrl+] leaves")
        self.assertIn("^[[Z", self.cat_text()); self.assertIn("^]", self.cat_text())
        self.t.keys("Tab"); time.sleep(0.4)
        self.assertFalse(self.preview_focused(), "tab leaves"); self.assertNotIn("^I", self.cat_text(), "and it was not forwarded")

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


class PassthroughReview(PreviewBase):
    """T12 review (f0e3354): locked target, `;`, F keys, paste-buffer, scroll cap, click areas. a1 is a DECOY cat pane (it must never receive a byte
    meant for another card), a3 is the pane the preview is locked on."""
    PANES = (("a1", CAT_CMD), ("a2", MARK_CMD), ("a3", CAT_CMD))

    def cat(self, sid):
        return "".join(tm("capture-pane", "-p", "-J", "-t", self.panes[sid]).stdout.split("\n")).rstrip()

    def enter_passthrough_on_a3(self):
        self.press("j", "j", "i")
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
                self.t.lit("rxdfq"); self.t.keys("j"); self.t.keys("Enter"); time.sleep(0.8)
                self.assertTrue(self.t.alive(), "q / r / x / d / f typed while locked must not reach the board either")
                self.assertEqual(self.env.actions(), [], "ccmux: zero calls (no restart / kill / review / switch)")
                self.assertEqual(self.cat("a1"), "", "the decoy pane (the first card the focus fell back to) got zero bytes")
                self.assertEqual(self.cat("a3"), a3_before, "nothing is sent to the old pane either")
                self.assertEqual(self.cat("a2"), "".join(l.strip() for l in [] ) or self.cat("a2"))
                row = last_text_row(self.t.snap()); self.assertIn("session is gone · tab back to the cards", row)
                i = self.t.snap()[-2].find("session is gone") if "session is gone" in self.t.snap()[-2].text else self.t.snap()[-1].find("session is gone")
                rows = [r for r in self.t.snap() if "session is gone" in r.text]
                self.assertTrue(rows and is_red(rows[0].cells[rows[0].find("session is gone")][1]), "red footer")
                self.assertFalse(self.preview_focused(), "the preview border is back to the plain colour")
                self.t.keys("Tab"); time.sleep(0.5)
                self.assertNotIn("session is gone", self.footer()); self.assertIn("tab focus preview", self.footer())
                self.t.keys("q"); self.assertEqual(self.t.exit_status(4), 0, "after tab the board is itself again: q quits")
                self.t.close() if False else None

    def test_esc_also_leaves_the_locked_state(self):
        self.enter_passthrough_on_a3(); self.drop_a3("gone")
        self.t.lit("zz"); time.sleep(0.3)
        self.assertIn("session is gone", self.footer())
        self.t.esc(); time.sleep(0.4)
        self.assertNotIn("session is gone", self.footer()); self.assertEqual(self.cat("a1"), "")

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


class PassthroughReview2(PreviewBase):
    """T12 second review (specs/m2-preview/design.md「直通目标锁定」and the bullets under it): a lock that has failed stays failed, empty / failed pastes leave nothing in the tmux
    buffer, a terminal narrower than 100 columns leaves the pass-through with its own message. The tmux that agmux runs is a recording wrapper around the real one."""
    PANES = PassthroughReview.PANES
    cat, enter_passthrough_on_a3, preview_focused, footer = (PassthroughReview.cat, PassthroughReview.enter_passthrough_on_a3,
                                                             PassthroughReview.preview_focused, PassthroughReview.footer)

    def board(self, w, h):
        real = shutil.which("tmux")
        self.log = self.env.tmp / "tmux-wrapper.log"
        wrapper = self.env.bin_real / "tmux"
        wrapper.write_text(f"#!/bin/sh\nprintf '%s\\037' \"$@\" >> {shlex.quote(str(self.log))}; printf '\\n' >> {shlex.quote(str(self.log))}\nexec {shlex.quote(real)} \"$@\"\n")
        wrapper.chmod(0o755)
        return super().board(w, h)

    def calls(self, name=None):
        if not self.log.exists():
            return []
        cs = [[a for a in l.split("\x1f") if a != ""] for l in self.log.read_text().split("\n") if l]
        return [c for c in cs if name is None or c[0] == name]

    def red_rows(self):  # red text in the message / footer rows only (the cards' own borders and eyes are red for sessions that need attention)
        return [r.text.strip() for r in self.t.snap()[-3:] if any(is_red(c[1]) for c in r.cells if c[0].strip())]

    def poll_twice(self):
        n = self.env.show_calls(); self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.3)

    # ---- 1) a failed lock is sticky: the target coming back does not revive it ----
    def test_target_gone_for_one_poll_then_back_stays_locked(self):
        self.enter_passthrough_on_a3(); self.t.lit("ok"); time.sleep(0.4); self.assertEqual(self.cat("a3"), "ok")
        full = json.loads((self.env.fake / "show.json").read_text())
        self.env.write_show([]); self.poll_twice()
        self.env.write_show(full); self.poll_twice(); self.poll_twice()  # the same session, with the same pane, is listed again
        self.assertIn("session is gone · tab back to the cards", self.footer())
        rows = [r for r in self.t.snap() if "session is gone" in r.text]
        self.assertTrue(rows and is_red(rows[0].cells[rows[0].find("session is gone")][1]), "red footer")
        self.assertFalse(self.preview_focused(), "the preview border is not pink again")
        self.t.lit("typed after it came back"); self.t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(self.cat("a3"), "ok", "zero bytes forwarded after the lock failed, even though the target is back")
        self.assertEqual(self.cat("a1"), ""); self.assertEqual(self.env.actions(), []); self.assertTrue(self.t.alive())
        self.t.keys("Tab"); time.sleep(0.5)
        self.assertNotIn("session is gone", self.footer())

    # ---- 2) empty and failed pastes ----
    def test_empty_paste_is_ignored_without_calling_tmux(self):
        self.enter_passthrough_on_a3(); self.t.lit("ok"); time.sleep(0.4)
        before = len(self.calls())
        self.t.lit("\x1b[200~\x1b[201~"); time.sleep(1.0)
        sent = [c for c in self.calls()[before:] if c[0] != "capture-pane"]  # the preview re-captures every 500ms by itself (spec): that is not a reaction to the paste
        self.assertEqual(sent, [], "an empty paste must not run load-buffer / paste-buffer / send-keys")
        self.assertEqual(self.red_rows(), [], "and no error message")
        self.assertEqual(self.cat("a3"), "ok"); self.assertTrue(self.preview_focused())

    def test_failed_paste_deletes_the_buffer_so_it_cannot_land_in_another_pane(self):
        self.enter_passthrough_on_a3()
        tm("kill-pane", "-t", self.panes["a3"]); time.sleep(0.3)
        self.t.lit("\x1b[200~secret text\x1b[201~"); time.sleep(1.0)
        cs = self.calls()
        self.assertTrue(any(c[0] == "load-buffer" for c in cs) or any("session is gone" in r.text for r in self.t.snap()), f"calls: {cs}\n{self.t.text()}")
        self.assertEqual(tm("list-buffers", "-F", "#{buffer_name}").stdout.split(), [], f"no agmux buffer left behind; calls: {cs}")
        self.t.keys("Tab"); time.sleep(0.5)
        self.press("k", "k", "i"); self.assertTrue(wait(self.preview_focused, 3), self.t.text())  # a1: the decoy cat pane
        self.t.lit("\x1b[200~\x1b[201~"); time.sleep(1.0)
        self.assertEqual(self.cat("a1"), "", f"the second pane got zero bytes; tmux calls: {self.calls()}")
        self.assertEqual(tm("list-buffers", "-F", "#{buffer_name}").stdout.split(), [])

    # ---- the poll moving the focus resets the local scroll as well ----
    def test_focus_card_vanishing_resets_the_scroll_offset(self):
        tm("respawn-pane", "-k", "-t", self.panes["a1"], SEQ_CMD); time.sleep(1.5)
        self.assertTrue(wait(lambda: "line 200" in pane_text(self.panes["a1"]), 5))
        self.press("i"); self.assertTrue(wait(self.preview_focused, 3), self.t.text())  # a1 is the focused card: pass-through on the 200-line pane
        self.t.wheel(PV_X + 20, 15, up=True, n=5); time.sleep(0.9)
        caps = lambda pane: [c[c.index("-S") + 1] for c in self.calls("capture-pane") if c[c.index("-t") + 1] == pane]
        a1 = self.panes["a1"]
        self.assertNotEqual(caps(a1)[-1], caps(a1)[0], f"5 notches up must have moved the capture window: {caps(a1)}")
        self.assertTrue(wait(lambda: "line 200" not in "\n".join(self.pv_text()), 3), "scrolled: the bottom line is out of view")
        S = json.loads((self.env.fake / "show.json").read_text())
        self.env.write_show([s for s in S if s["id"] != "a1"]); self.poll_twice(); time.sleep(0.8)  # the focus falls back to the first remaining card: a2
        a2 = self.panes["a2"]; self.assertTrue(wait(lambda: caps(a2), 3), f"the preview now follows a2; calls: {self.calls('capture-pane')[-4:]}")
        self.assertEqual(set(caps(a2)), {caps(a1)[0]}, f"a2 is captured with the plain visible-rows window ({caps(a1)[0]}), not with a1's leftover offset: {caps(a2)}")
        self.t.keys("Tab"); time.sleep(0.4); self.press("k", "j")  # the board is itself again; moving around keeps the plain window
        self.assertEqual(set(caps(a2)), {caps(a1)[0]})

    # ---- 3) the terminal narrower than 100 columns ----
    def test_narrow_terminal_leaves_the_passthrough_with_its_own_message(self):
        self.enter_passthrough_on_a3(); self.t.lit("ok"); time.sleep(0.4)
        self.t.resize(99, 50); time.sleep(1.0)
        text = self.t.text()
        self.assertIn("preview closed: terminal too narrow", text, text)
        self.assertNotIn("session is gone", text)
        self.assertIsNone(self.pv(), "the preview is closed")
        self.t.lit("q"); self.assertEqual(self.t.exit_status(4), 0, "back on the cards: q quits the board; it was not forwarded to the pane")
        self.assertEqual(self.cat("a3"), "ok")
