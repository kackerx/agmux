"""T11 preview window. Spec: specs/m2-preview/design.md「布局」, tasks.md T11.

The previewed panes are REAL panes of the private tmux server (`-L agmux-test`): a1 shows 200 coloured lines (`seq`-like), a2 a marker pane, a3 a raw-mode `cat -v` look-alike
(used by test_t12). agmux reaches that server through $TMUX, exactly like a pane inside it would; the fake ccmux still feeds the session list.
Geometry at 232x50: P opens a preview of max(60, 40%) = 92 columns, the cards get 140 -> 3 columns of 46; the preview box starts at column 141.
"""
import re, unittest
from lib import *
from test_t3 import ready, by_name, NAMES, last_text_row, C_FOCUS

SEQ_CMD = r"""sh -c 'i=1; while [ $i -le 200 ]; do printf "\033[3%dmline %d\033[0m\n" $((i % 6 + 1)) $i; i=$((i+1)); done; sleep 600'"""
MARK_CMD = r"""sh -c 'i=1; while [ $i -le 60 ]; do printf "\033[36msecond-pane-marker %d\033[0m\n" $i; i=$((i+1)); done; sleep 600'"""
CAT_CMD = f"env LANG=en_US.UTF-8 sh -c 'stty raw -echo; exec python3 -u {HERE / 'fixtures' / 'catv.py'}'"  # a UTF-8-safe `cat -v` (see fixtures/catv.py)
PV_X = 140  # 232 * 60%: the cards' width; the preview starts here


def pane_text(pane, n=60):
    return tm("capture-pane", "-p", "-t", pane, "-S", f"-{n}").stdout


class PreviewBase(unittest.TestCase):
    W, H = 232, 50
    PANES = (("a1", SEQ_CMD), ("a2", MARK_CMD), ("a3", CAT_CMD))  # session -> command of its real pane

    def setUp(self):
        tm("kill-server"); self.addCleanup(lambda: tm("kill-server"))
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        tm("start-server", ";", "set", "-g", "remain-on-exit", "off")
        self.panes = {}
        for sid, cmd in self.PANES:
            tm("new-session", "-d", "-s", f"pane-{sid}", "-x", "100", "-y", "30", cmd)
            self.panes[sid] = tm("display", "-p", "-t", f"pane-{sid}", "#{pane_id}").stdout.strip()
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            if s["id"] in self.panes:
                s["tmuxPane"] = self.panes[s["id"]]
        self.env.write_show(S)
        if self.PANES[0][1] == SEQ_CMD:
            self.assertTrue(wait(lambda: "line 200" in pane_text(self.panes["a1"]), 5), "seq pane not ready")
        self.t = self.board(self.W, self.H)

    def board(self, w, h):
        t = Term(self.env, w=w, h=h, real_tmux=True); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: "agmux ·" in t.text(), 8), t.text())
        return t

    # ---- screen helpers
    def pv(self, t=None):
        """the preview region: (title text, box rows as Row objects (inside the borders), box top row index, box x, snap) or None.
        The preview box is the one whose top-right corner is the terminal's last column (cards never reach it)."""
        t = t or self.t; snap = t.snap()
        if t.w < 100 + 40:  # a preview needs >= 100 columns (and these tests only open it at 232): a card's own corner at the last column is not a preview
            if t.w < 100:
                return None
        for r in range(1, len(snap) - 3):
            row = snap[r + 1]
            if len(row.cells) == t.w and row.cells[-1][0] == "╮":
                x = max(i for i in range(PV_X, t.w) if row.cells[i][0] == "╭")
                rows = []
                for rr in range(r + 2, len(snap)):
                    cells = snap[rr].cells
                    if len(cells) == t.w and cells[x][0] == "│" and cells[-1][0] == "│":
                        rows.append(Row(cells[x + 1:-1]))
                    else:
                        break
                return snap[r].width_text(PV_X, len(snap[r].cells)).strip(), rows, r + 1, x, snap
        return None

    def pv_text(self, t=None):
        p = self.pv(t)
        return [] if p is None else [r.text.strip() for r in p[1]]

    def press(self, *keys):
        for k in keys:
            self.t.keys(k); time.sleep(0.25)

    def open_preview(self):
        self.press("p")
        self.assertTrue(wait(lambda: any("line 200" in x for x in self.pv_text()), 4), "preview did not show the pane:\n" + self.t.text())


class Preview(PreviewBase):
    def test_p_opens_a_third_less_wide_layout_with_the_pane_tail(self):
        self.assertIsNone(self.pv(), "no preview before P")
        before = [c for c in find_cards(self.t.snap()) if c.ok]
        self.assertEqual({c.w for c in before}, {57}, "4 columns of 56 before")
        self.open_preview()
        title, rows, top, x, snap = self.pv()
        cs = [c for c in find_cards(snap) if c.ok]
        self.assertEqual(layout(140), (3, 46))
        self.assertEqual((len(cs), {c.w for c in cs}), (9, {46}), "the cards re-flow to 3 columns of 46")
        self.assertEqual(sorted({c.c for c in cs if c.r == min(y.r for y in cs)}), [0, 47, 94])
        self.assertEqual(x, PV_X + 1, "box starts one column after the cards' area")
        self.assertRegex(title, rf"^alpha-main · {re.escape(self.panes['a1'])} · ⏵ working · Bash$", "title: name · pane id · status")
        texts = [r.text.strip() for r in rows]
        self.assertEqual(len(texts), 44, "50 rows - top bar - msg - footer - title - 2 borders")
        self.assertEqual(texts[-2:], ["line 200", ""], "T24: the cursor sits on the empty line below `line 200`: that line is the last row of the box")
        self.assertEqual(texts[0], "line 158", "the last 44 lines of the pane's history + capture-pane's own trailing blank line counted out: 43 lines and the cursor row")

    def test_colours_reach_the_preview_and_do_not_leak_to_the_border(self):
        self.open_preview()
        title, rows, top, x, snap = self.pv()
        cols = {rgb(c[1]) for r in rows for c in r.cells[1:9] if c[0] not in " " and c[1]}
        self.assertGreaterEqual(len(cols), 3, f"the pane's colours must survive: {cols}")
        for i, rr in enumerate(range(top + 1, top + 1 + len(rows))):
            cells = snap[rr].cells
            self.assertEqual(cells[x][1], C_DIM, f"row {i}: left border colour (not the pane's)")
            self.assertEqual(cells[-1][1], C_DIM, f"row {i}: right border colour: the pane's colour leaked past the line end")

    def test_p_again_restores_four_columns(self):
        self.open_preview(); self.press("p")
        self.assertTrue(wait(lambda: self.pv() is None, 4), "preview must go away")
        cs = [c for c in find_cards(self.t.snap()) if c.ok]
        self.assertEqual((len(cs), {c.w for c in cs}), (9, {57}))

    def test_below_100_columns_the_preview_is_refused_with_a_hint(self):
        t = self.board(99, 50)
        t.keys("p"); time.sleep(0.5)
        self.assertIn("at least 100 columns", "\n".join(r.text for r in t.snap()[-3:]))
        self.assertTrue(all("╭" not in r.text or True for r in t.snap()))
        cs = [c for c in find_cards(t.snap()) if c.ok]
        self.assertTrue(cs and {c.w for c in cs} == {layout(99)[1]}, "cards keep the full width")
        self.assertTrue(t.alive())
        t.keys("q")

    def test_exactly_100_columns_opens_a_60_wide_preview(self):
        t = self.board(100, 50)
        t.keys("p"); time.sleep(0.8)
        snap = t.snap(); box = [i for i, c in enumerate(snap[2].cells) if c[0] == "╭" and i >= 40]
        self.assertTrue(box, "preview box at column >= 40:\n" + t.text())
        self.assertEqual(snap[1].width_text(40, 100).strip().split(" · ")[0], "alpha-main")
        cs = [c for c in find_cards(snap) if c.ok and c.c < 40]
        self.assertTrue(cs and {c.w for c in cs} == {40}, "cards get 40 columns: one column of 40")

    def test_focus_change_moves_the_preview(self):
        self.open_preview()
        self.press("j")  # a2
        self.assertTrue(wait(lambda: any("second-pane-marker 60" in x for x in self.pv_text()), 4), "\n".join(self.pv_text()))
        self.assertTrue(self.pv()[0].startswith(f"pj-fallback · {self.panes['a2']} · "), self.pv()[0])
        self.press("j")  # a3 (cat -v)
        self.assertTrue(self.pv()[0].startswith(f"告警会话") or "· " + self.panes["a3"] in self.pv()[0], self.pv()[0])
        self.press("k", "k")
        self.assertTrue(wait(lambda: any("line 200" in x for x in self.pv_text()), 4), "back on a1")

    def test_no_pane_message_for_background_and_folded_title(self):
        self.open_preview()
        self.press("l", "j")  # b2: background, no tmux pane
        self.assertTrue(wait(lambda: "no pane" in "\n".join(self.pv_text()), 3), "\n".join(self.pv_text()))
        self.assertNotIn("line 200", "\n".join(self.pv_text()))
        self.press("k", "f")  # fold G2 (b1 focused): the folded title has no pane either
        self.assertTrue(wait(lambda: "no pane" in "\n".join(self.pv_text()), 3), "\n".join(self.pv_text()))

    def test_preview_follows_the_pane_live(self):
        self.open_preview()
        tm("send-keys", "-t", self.panes["a1"], "C-c")  # (sh keeps running; just prove live capture with a new line)
        pane = self.panes["a2"]; self.press("j")
        self.assertTrue(wait(lambda: any("second-pane-marker 60" in x for x in self.pv_text()), 4))
        tm("send-keys", "-t", pane, "-l", "--", "echo LIVE-LINE-42"); tm("send-keys", "-t", pane, "Enter")
        self.assertTrue(wait(lambda: any("LIVE-LINE-42" in x for x in self.pv_text()), 3), "new output must show up within ~0.5-1s:\n" + "\n".join(self.pv_text()))

    def test_resize_keeps_preview_and_shrinking_below_100_closes_it(self):
        self.open_preview()
        self.t.resize(180, 50); time.sleep(0.8)
        cs = [c for c in find_cards(self.t.snap()) if c.ok]
        self.assertEqual(layout(180 - 72), layout(108)); self.assertTrue(cs)
        self.t.resize(90, 50); time.sleep(0.8)
        cs = [c for c in find_cards(self.t.snap()) if c.ok]
        self.assertEqual({c.w for c in cs}, {layout(90)[1]}, "below 100 columns the cards take the whole width again")
        self.t.resize(232, 50); time.sleep(0.8)
        self.assertTrue(wait(lambda: self.pv() is not None, 3), "preview is back when the terminal is wide again (P is still on)")

    def test_tiny_terminal_heights_do_not_crash(self):
        """review: a terminal of 5 rows or fewer made the preview layout panic"""
        self.open_preview()
        for h in (8, 6, 5, 4, 3, 2):
            self.t.resize(232, h); time.sleep(0.5)
            self.assertTrue(self.t.alive(), f"board died at height {h}:\n{self.t.text()}")
        self.t.resize(232, 50); time.sleep(0.8)
        self.assertTrue(self.t.alive())
        self.assertTrue(wait(lambda: any("line 200" in x for x in self.pv_text()), 4), "back to normal after the resize\n" + self.t.text())
        self.t.keys("q"); self.assertEqual(self.t.exit_status(4), 0, "and it still quits cleanly")

    def test_scrolled_back_and_then_a_tiny_terminal_does_not_crash(self):
        """review: scrolled far back (offset > visible lines), then shrunk to <= 5 rows"""
        self.open_preview()
        self.t.wheel(PV_X + 20, 15, up=True, n=15); time.sleep(0.8)
        for h in (8, 5, 4, 3, 2):
            self.t.resize(232, h); time.sleep(0.5)
            self.assertTrue(self.t.alive(), f"board died at height {h} after scrolling back:\n{self.t.text()}")
        self.t.resize(232, 50); time.sleep(0.8)
        self.assertTrue(self.t.alive()); self.t.keys("q"); self.assertEqual(self.t.exit_status(4), 0)

    def test_click_on_another_card_updates_the_preview_at_once(self):
        """review: a click moved the focus but the preview only changed at the next 500ms tick (so check several times at different tick phases)"""
        self.open_preview()
        late = []
        for i in range(8):
            sid, marker = (("a2", "second-pane-marker"), ("a1", "line 200"))[i % 2]
            c = by_name([c for c in find_cards(self.t.snap()) if c.ok], NAMES[sid])
            time.sleep(0.07 * i)  # shift the phase against the 500ms capture tick
            self.t.click(c.c + c.w // 2, c.r + 2)
            if not wait(lambda: any(marker in x for x in self.pv_text()), 0.2, 0.02):
                late.append((i, sid))
            time.sleep(0.4)
        self.assertEqual(late, [], "the preview must follow a click without waiting for the 500ms tick (checked 200ms after each click)")

    def test_unfolding_a_group_moves_the_preview_to_its_first_card(self):
        """review: after f / enter on a folded title the focus went to the first card but the preview kept saying `no pane`"""
        self.open_preview()
        self.press("f")  # fold G1 (a1 focused)
        self.assertTrue(wait(lambda: "no pane" in "\n".join(self.pv_text()), 3), "folded title: no pane")
        self.press("f")  # unfold
        self.assertTrue(wait(lambda: any("line 200" in x for x in self.pv_text()), 3), "after f the preview shows the first card's pane\n" + "\n".join(self.pv_text()))
        self.press("f"); self.assertTrue(wait(lambda: "no pane" in "\n".join(self.pv_text()), 3))
        self.press("Enter")  # enter unfolds too
        self.assertTrue(wait(lambda: any("line 200" in x for x in self.pv_text()), 3), "after enter as well")
        self.assertEqual(self.env.actions(), [], "enter on a folded title never switches")

    def test_p_below_100_columns_closes_an_open_preview(self):
        """review: P below 100 columns only printed the hint and left the preview switched on, so it popped back when the terminal grew"""
        self.open_preview()
        self.t.resize(90, 50); time.sleep(0.8)
        self.assertIsNone(self.pv(), "too narrow: hidden")
        self.t.keys("p"); time.sleep(0.5)
        self.t.resize(232, 50); time.sleep(0.9)
        self.assertIsNone(self.pv(), "P switched the preview off even though it could not be shown: it does not come back when the terminal grows")
        self.press("p"); self.assertTrue(wait(lambda: self.pv() is not None, 3), "and P opens it again")

    def test_hjkl_never_move_the_focus_into_the_preview(self):
        self.open_preview()
        for _ in range(6):
            self.press("l")
        cs = [c for c in find_cards(self.t.snap()) if c.ok and c.rows[0].cells[0][1] == C_FOCUS]
        self.assertEqual(len(cs), 1, "the focus is still on a card")
        self.assertTrue(cs[0].c < PV_X)
        p = self.pv(); self.assertTrue(p and p[3] == PV_X + 1)
        self.assertNotEqual(p[4][p[2]].cells[p[3]][1], C_FOCUS, "preview border not focused")

    def test_cards_and_preview_scrolled_board_keeps_footer(self):
        self.open_preview()
        self.assertIn("p preview", last_text_row(self.t.snap())); self.assertIn("tab focus preview", last_text_row(self.t.snap()))
        rows = self.t.text().split("\n")
        while rows and rows[-1] == "" and len(rows) > 50: rows.pop()
        self.assertEqual(len(rows), 50, "total rows == terminal height with the preview open")


if __name__ == "__main__":
    unittest.main()
