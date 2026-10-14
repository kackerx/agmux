"""T52 the `a` picker shows every row: rows = ceil(cells / 5) (25 cells -> 5 rows), it scrolls only when the screen has no room below (or above) the card for all of them. Then it shows fewer rows and the box's own
borders say how many avatars are scrolled out: `╭─ ↑ 5 more ───╮` / `╰─ ↓ 10 more ──╯` (muted label, the rest of the rule in the focus colour, nothing when nothing is that way); the box keeps its height and place
while the cursor moves; a one row box that fits beside the card goes there, not to the top of the screen. Spec: specs/m3-config/tasks.md T52 + design.md「选择框」; geometry is read off the screen (test_t18.find_grid).
The oracle for the counts is the screen itself: the cells drawn are a run of whole grid rows of the listing, the label above counts the cells before the first one, the label below the cells after the last."""
import json, re, unittest
from lib import *
from test_t3 import C_FOCUS, border_colours
from test_t18 import overlay, to_start, LISTING, SPRITE_NAMES, GRID_COLS
import test_t32 as t32

MUTED = (98, 114, 164)


def near(a, b, d=3): return a is not None and all(abs(x - y) <= d for x, y in zip(a, b))  # the muted colour comes back from tmux a step off (97, 113, 163)

TOP, BOTTOM = re.compile(r"^╭─ ↑ (\d+) more ─+╮$"), re.compile(r"^╰─ ↓ (\d+) more ─+╯$")


def edges(snap, ov):
    """(up, down): the count each border says (None = a plain rule), asserting the rule is exactly as wide as the box, a corner at each end, the label muted and the rest the focus colour"""
    x0, y0, w, h = ov["box"]; out = []
    for y, l, r, rx, ar in ((y0, "╭", "╮", TOP, "↑"), (y0 + h - 1, "╰", "╯", BOTTOM, "↓")):
        cells = snap[y].cells[x0:x0 + w]; text = "".join(c[0] for c in cells); assert len(cells) == w and text[0] == l and text[-1] == r, f"border row {text!r}"
        m = rx.match(text)
        if m:
            lab = [c for c in cells if c[0] != "─" and c[0] not in (l, r)]; assert all(c[0] == " " or near(rgb(c[1]), MUTED) for c in lab), f"label not muted: {[(c[0], c[1]) for c in lab]}"
            assert all(c[1] == C_FOCUS for c in cells if c[0] in "─" + l + r), f"the rule is not the focus colour: {text!r}"; out.append(int(m.group(1)))
        else:
            assert set(text[1:-1]) == {"─"}, f"neither a plain rule nor a label: {text!r}"; assert all(c[1] == C_FOCUS for c in cells), f"plain rule colour: {text!r}"; out.append(None)
    return tuple(out)


class Base(t32.Base):
    def focused(self, t):
        fc = [c for c in find_cards(t.snap()) if c.ok and border_colours(c) == {C_FOCUS}]; self.assertEqual(len(fc), 1, t.text()); return fc[0]  # (c.ok: a card the screen cuts off is no card here)

    def at_a2(self, t):
        t.keys("j"); time.sleep(0.3); return self.open(t)

    def state(self, t, order, box=None):
        """read the picker once and check it against the whole listing; -> (ov, up, down)"""
        snap = t.snap(); ov = overlay(snap); self.assertTrue(ov, t.text())
        ids = [r["id"] for r in ov["rows"]]; first = order.index(ids[0]); total = (len(order) + 4) // 5
        self.assertEqual(first % GRID_COLS, 0, f"the first cell drawn starts a grid row: {ids}"); self.assertEqual(ids, order[first:first + len(ids)], "a run of whole rows of the listing")
        self.assertTrue(len(ids) == GRID_COLS * ov["shown"] or first + len(ids) == len(order), f"every shown row is full except the listing's last: {len(ids)} cells in {ov['shown']} rows")
        up, down = edges(snap, ov)
        if ov["shown"] == total: self.assertEqual((up, down), (None, None), "everything shown: plain borders")
        else:
            self.assertEqual(up or 0, first, f"up = cells before the first drawn ({ids[0]} is #{first})"); self.assertEqual(down or 0, len(order) - first - len(ids), "down = cells after the last one drawn, never negative")
        if box: self.assertEqual(ov["box"], box, "the box keeps its place and size while the cursor moves")
        return ov, up, down

    def walk(self, t, order, rows=None):
        """the cursor from the first cell to the last with `l` (the first press of each is the test's own), checking the state at every cell; -> [(cursor id, up, down)]"""
        self.move_to(t, order[0], order); seen = []; box = overlay(t.snap())["box"]
        for i in range(len(order)):
            ov, up, down = self.state(t, order, box); cur = self.cursor(t); self.assertEqual(cur, order[i]); self.assertTrue(all(r["row"] < len(t.snap()) for r in ov["rows"]))
            if rows is not None: self.assertEqual(ov["shown"], rows)
            self.assertIn(cur, [r["id"] for r in ov["rows"]], "the cursor is on screen"); seen.append((cur, up, down)); t.keys("l"); time.sleep(0.12)
        return seen


class Tall(Base):
    def test_25_cells_show_all_five_rows_with_plain_borders_on_a_tall_screen(self):
        t = self.board(tmux=False, w=80, h=40, loose=True); ov = self.at_a2(t); self.assertEqual(ov["shown"], 5, t.text()); self.assertEqual(ov["box"][2:], (52, 2 + 3 * 5))
        self.assertEqual([r["id"] for r in ov["rows"]], LISTING); self.assertEqual(len(LISTING), 25); self.assertEqual(edges(t.snap(), ov), (None, None), "no more line, the borders are a plain rule")
        self.assertEqual([r["name"] for r in ov["rows"]][20:], SPRITE_NAMES, "the fifth row, the sprites, is on screen")
        t.keys("q"); time.sleep(0.2); self.assertIsNone(overlay(t.snap()))

    def test_the_box_never_moves_or_changes_while_the_cursor_walks_all_25(self):
        t = self.board(tmux=False, w=80, h=40, loose=True); self.at_a2(t); self.walk(t, LISTING, rows=5)

    def test_rows_follow_the_cell_count_25_26_30_31(self):
        for extra, rows, last in ((1, 6, 1), (5, 6, 5), (6, 7, 1)):
            with self.subTest(user_sets=extra):
                names = [f"zz{k}" for k in range(extra)]
                for n in names: write_avatar_set(self.cfg, n, {"idle": 1})
                order = self.listing(names); self.assertEqual(len(order), 25 + extra)
                t = self.board(tmux=False, w=80, h=70, loose=True); ov = self.at_a2(t)
                self.assertEqual(ov["shown"], rows, t.text()); self.assertEqual([r["id"] for r in ov["rows"]], order); self.assertEqual(ov["box"][3], 2 + 3 * rows)
                self.assertEqual(edges(t.snap(), ov), (None, None)); self.assertEqual(len([r for r in ov["rows"] if r["gr"] == rows - 1]), last, "the last row holds what is left")
                t.close()
                for n in names: shutil.rmtree(self.cfg / "agmux" / "avatars" / n)


class Short(Base):
    """80x24: the card a2 has about 10 rows under it"""
    def place(self, t, w, h):
        fc = self.focused(t); ov = self.open(t); gy, gr = t32.grid_place(fc.r, 5, h, (len(LISTING) + 4) // 5); return fc, ov, gy, gr

    def test_fewer_rows_with_the_count_in_the_borders_and_a_box_that_stays_put(self):
        t = self.board(tmux=False, w=80, h=24, loose=True); t.keys("j"); time.sleep(0.3); fc, ov, gy, gr = self.place(t, 80, 24)
        self.assertEqual((ov["box"][1], ov["shown"]), (gy, gr), t.text()); self.assertLess(gr, 5, "this screen is too short for all five rows"); to_start(t)
        self.assertEqual(edges(t.snap(), overlay(t.snap())), (None, 25 - 5 * gr), "at the first cell: nothing above, a count below"); self.assertRegex(t.text(), rf"╰─ ↓ {25 - 5 * gr} more ─+╯")
        seen = self.walk(t, LISTING, rows=gr)
        self.assertEqual(seen[0][1:], (None, 25 - 5 * gr)); self.assertEqual(seen[-1][0], "bot-05"); self.assertEqual(seen[-1][1:], (25 - 5 * gr, None), "on the last row ↑ is there and ↓ is a plain rule")
        self.assertTrue(any(u and d for _, u, d in seen), "in between both ends have a count")

    def test_the_counts_for_a_listing_whose_last_row_is_not_full(self):
        for extra in (1, 6):
            with self.subTest(user_sets=extra):
                names = [f"zz{k}" for k in range(extra)]
                for n in names: write_avatar_set(self.cfg, n, {"idle": 1})
                order = self.listing(names); t = self.board(tmux=False, w=80, h=24, loose=True); t.keys("j"); time.sleep(0.3); self.open(t)
                seen = self.walk(t, order); self.assertEqual(seen[-1][2], None); self.assertGreater(seen[-1][1], 0)
                self.assertTrue(all(d is None or d > 0 for _, _, d in seen) and all(u is None or u > 0 for _, u, _ in seen), "never 0 or negative: a plain rule instead")
                t.close()
                for n in names: shutil.rmtree(self.cfg / "agmux" / "avatars" / n)

    def test_three_digit_counts_fit_the_border(self):
        for k in range(120): write_avatar_set(self.cfg, f"s{k:03d}", {"idle": 1})
        t = self.board(tmux=False, w=80, h=24, loose=True); t.keys("j"); time.sleep(0.3); ov = self.open(t); to_start(t, 160); ov = overlay(t.snap()); up, down = edges(t.snap(), ov)
        self.assertEqual((up, down), (None, 145 - 5 * ov["shown"])); self.assertGreaterEqual(down, 100); self.assertEqual(ov["box"][2], 52)

    def test_enter_binds_the_set_under_the_cursor_after_scrolling(self):
        for target in (17, 24):
            with self.subTest(cell=LISTING[target]):
                t = self.board(tmux=False, w=80, h=24, loose=True); t.keys("j"); time.sleep(0.3); self.open(t); self.move_to(t, LISTING[target])
                ov, up, down = self.state(t, LISTING); self.assertTrue(up, "scrolled"); self.assertEqual(self.cursor(t), LISTING[target]); t.keys("Enter"); time.sleep(0.8)
                self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": LISTING[target]}); self.assertIsNone(overlay(t.snap())); t.close(); self.json.unlink()

    def test_esc_cancels_a_scrolled_picker_without_writing(self):
        t = self.board(tmux=False, w=80, h=24, loose=True); t.keys("j"); time.sleep(0.3); self.open(t); self.move_to(t, "bot-05"); t.esc(); self.assertIsNone(overlay(t.snap())); self.assertFalse(self.json.exists())

    def test_mouse_clicks_do_not_reach_the_open_picker(self):
        t = self.board(tmux=False, w=80, h=24, loose=True); t.keys("j"); time.sleep(0.3); ov = self.open(t); box = ov["box"]; cur = self.cursor(t); cell = ov["rows"][7]
        t.click(cell["col"] + 2, cell["row"]); time.sleep(0.5); after = overlay(t.snap()); self.assertTrue(after, "still open"); self.assertEqual(after["box"], box); self.assertEqual(self.cursor(t), cur); self.assertFalse(self.json.exists())


class Placement(Base):
    def test_the_box_follows_the_design_on_every_height_from_12_to_40_and_a_one_row_box_stays_beside_the_card(self):
        bad, one, modes, checked = [], [], set(), 0
        for h in list(range(12, 25)) + [26, 30, 40]:
            t = self.board(tmux=False, w=80, h=h, loose=True)
            for _ in range(3):
                fc = [c for c in find_cards(t.snap()) if c.ok and border_colours(c) == {C_FOCUS}]
                if fc and fc[0].r + 5 <= h:
                    fc = fc[0]; ov = self.open(t); gy, gr = t32.grid_place(fc.r, 5, h, 5); checked += 1
                    got = (ov["box"][1], ov["shown"]); modes.add("below" if gy == fc.r + 5 else "above" if gy < fc.r else "top")
                    if got != (gy, gr): bad.append((h, fc.r, "got", got, "want", (gy, gr)))
                    else:
                        self.state(t, LISTING)
                        if gr == 1 and gy != 1: one.append((h, fc.r, gy))
                        if gy + ov["box"][3] > fc.r and gy < fc.r + 5 and gy != 1: bad.append((h, fc.r, "the box covers its card", gy))
                    t.keys("q"); time.sleep(0.2)
                t.keys("j"); time.sleep(0.25)
            t.close()
        self.assertEqual(bad, []); self.assertGreaterEqual(checked, 20); self.assertTrue({"below", "above"} <= modes, modes); self.assertTrue(one, "a one row box was placed beside a card at least once (not at the top of the screen)")


if __name__ == "__main__":
    unittest.main()
