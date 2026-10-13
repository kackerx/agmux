"""T32 the `a` picker is a grid under the focused card (T33: cells 10 wide): 5 columns, up to 4 rows (more scroll), avatars (6 x 2) with their name under them; image sets first (built-in,
then the user's by name), the sprites last; h/j/k/l and arrows move, enter binds, esc / q cancel; the message row names the cursor's avatar. Spec: specs/m3-config/design.md
「头像图片：状态、宽度、选择框」5-6, tasks.md T32. Geometry is read off the screen (test_t18.find_grid): the cell width and the listing are not hard-coded."""
import json, re, subprocess, unittest
from lib import *
from test_t3 import ready, last_text_row, border_colours, C_FOCUS
from test_t18 import overlay, find_grid, LISTING, IDS, SPRITE_NAMES, N_SETS, HASHED_SPRITE, GRID_COLS, CELL_H, _fnv
import test_t28 as t28
from test_t28 import cards_of, named, card_avatar


def grid_place(card_top, card_h, h, total_rows, want=4):
    """The design's placement: straight below the card, else above it, with fewer rows if need be, else the top of the board with one row. -> (y, rows drawn)."""
    below, above = h - 2 - (card_top + card_h), card_top - 1
    for rows in range(want, 0, -1):
        need = 2 + CELL_H * rows
        if below >= need: return card_top + card_h, min(rows, total_rows)
        if above >= need: return card_top - need, min(rows, total_rows)
    return 1, 1


class Base(t28.Base):
    def setUp(self):
        super().setUp(); self.json = self.env.state / "agmux" / "avatars.json"; self.config("", default=None)

    def focused(self, t):
        fc = [c for c in find_cards(t.snap()) if border_colours(c) == {C_FOCUS}]; self.assertEqual(len(fc), 1); return fc[0]

    def open(self, t, tries=3):
        t.keys("a"); ov = wait(lambda: overlay(t.snap()), 3); self.assertTrue(ov, t.text()); return ov

    def cursor(self, t):
        ov = overlay(t.snap()); at = [r["id"] for r in ov["rows"] if r["marker"]]; self.assertEqual(len(at), 1, f"exactly one cursor cell: {ov}"); return at[0]

    def to_a2(self, t):
        t.keys("j"); time.sleep(0.3)  # pj-fallback

    def move_to(self, t, want, order=None):
        order = order or LISTING
        for _ in range(len(order) + 3):
            i = order.index(self.cursor(t)); w = order.index(want)
            if i == w: return
            t.keys("l" if w > i else "h"); time.sleep(0.12)
        self.fail(f"cursor did not reach {want}")

    def msg_row(self, t):
        snap = t.snap(); last = max(i for i, r in enumerate(snap) if r.text.strip()); return snap[last - 1].text  # the row above the footer


    def listing(self, extra=()):
        """ids in picker order when the user has the sets `extra` (names) too: built-in sets, user sets by name, sprites."""
        sets = [i for i in LISTING if not i.startswith("bot-")] + sorted(extra)
        return sets + [i for i in LISTING if i.startswith("bot-") and i not in extra]


class Layout(Base):
    def test_cells_in_order_with_names_under_the_avatars(self):
        t = self.board(tmux=False); self.to_a2(t); ov = self.open(t)
        self.assertEqual([r["id"] for r in ov["rows"]], LISTING); self.assertEqual([r["name"] for r in ov["rows"]], [None] * N_SETS + SPRITE_NAMES, "a set shows its id, a sprite its name")
        self.assertEqual([(r["gr"], r["gc"]) for r in ov["rows"]], [(i // 5, i % 5) for i in range(len(LISTING))])
        self.assertEqual(LISTING[:1], ["aza_choi"], "the built-in sets come first, aza_choi among them"); self.assertEqual(LISTING[N_SETS:], IDS, "the last row(s): the sprites")
        self.assertEqual(len(LISTING), N_SETS + 5)
        blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in t28.__dict__.get("sprite_blocks", lambda o: [])("")} if False else None

    def test_every_cell_has_a_two_row_avatar_and_the_sprites_are_what_agmux_avatars_prints(self):
        from test_t2 import sprite_blocks, run_avatars
        blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in sprite_blocks(run_avatars(self.env).stdout)}
        t = self.board(tmux=False); self.to_a2(t); ov = self.open(t)
        for r in ov["rows"]:
            self.assertTrue(all(len(x) == 6 for x in r["sprite"]) and any(ch != " " for x in r["sprite"] for ch in x), f"{r['id']}: a 6x2 face")
        self.assertEqual([r["sprite"] for r in ov["rows"] if r["id"] in IDS], [blocks[i] for i in IDS])
        self.assertEqual(ov["rows"][0]["sprite"], blocks[HASHED_SPRITE], "pixel mode: a set cell is drawn with the sprite its id hashes to (fnv32a(id) % 5)")
        for r in ov["rows"][:N_SETS]: self.assertEqual(r["sprite"], blocks[f"bot-0{_fnv(r['id']) % 5 + 1}"], f"{r['id']}")

    def test_the_box_sits_under_the_focused_card_at_its_left_edge(self):
        t = self.board(tmux=False); self.to_a2(t); card = self.focused(t); ov = self.open(t); x0, y0, w, h = ov["box"]
        self.assertEqual((x0, y0), (card.c, card.r + 5)); self.assertEqual(w, 5 * ov["cell_w"] + 2); self.assertEqual(h, 2 + 3 * ov["shown"]); self.assertEqual(ov["shown"], min(4, (len(LISTING) + 4) // 5))

    def test_placement_follows_the_design_for_every_card_and_several_screens(self):
        bad, checked, modes = [], 0, set()
        for (w, h) in ((200, 50), (200, 30), (120, 40), (200, 24), (200, 20), (200, 16)):
            t = self.board(tmux=False, w=w, h=h, loose=True); cards = find_cards(t.snap())
            if not cards: t.close(); continue
            for k in range(min(len(cards), 9)):
                fc = [c for c in find_cards(t.snap()) if c.ok and border_colours(c) == {C_FOCUS}]
                if not fc or fc[0].r + 5 > h: t.keys("j"); time.sleep(0.15); continue  # (a card cut off by the screen)
                fc = fc[0]
                ov = self.open(t); gy, gr = grid_place(fc.r, 5, h, (len(LISTING) + 4) // 5)
                gx = min(fc.c, max(w - ov["box"][2], 0))
                got = (ov["box"][0], ov["box"][1], ov["shown"]); checked += 1; modes.add("below" if gy == fc.r + 5 else "above" if gy < fc.r else "top")
                if got != (gx, gy, gr): bad.append((w, h, fc.c, fc.r, "got", got, "want", (gx, gy, gr)))
                t.keys("q"); time.sleep(0.2); t.keys("j"); time.sleep(0.2)
            t.close()
        self.assertEqual(bad, [], "below the card, else above, else fewer rows; x clipped to the screen")
        self.assertGreaterEqual(checked, 15, "enough (screen, card) cases were really checked"); self.assertTrue({"below", "above"} <= modes, f"both placements were exercised: {modes}")

    def test_a_card_in_the_last_column_keeps_the_box_on_screen(self):
        t = self.board(tmux=False, w=200, h=50)
        for _ in range(12):  # walk to the last column
            t.keys("l"); time.sleep(0.12)
        fc = self.focused(t); ov = self.open(t); self.assertGreater(fc.c, 100); self.assertLessEqual(ov["box"][0] + ov["box"][2], 200, ov["box"])
        self.assertEqual(ov["box"][0], min(fc.c, 200 - ov["box"][2]))

    def test_message_row_and_footer(self):
        t = self.board(tmux=False); self.to_a2(t); self.open(t); cur = self.cursor(t)
        self.assertIn(cur, self.msg_row(t), "the message row names the cursor's id"); foot = last_text_row(t.snap()); self.assertEqual(foot, " h/j/k/l move · enter bind · q cancel")
        for k, want in (("l", LISTING[LISTING.index(cur) + 1]),):
            t.keys(k); time.sleep(0.2); self.assertIn(want, self.msg_row(t))
        self.move_to(t, "bot-03"); self.assertRegex(self.msg_row(t), r"bot-03  Sprout", "a sprite: id and name")
        self.move_to(t, "aza_choi"); self.assertNotRegex(self.msg_row(t), r"aza_choi  \w", "a set: the id only")

    def test_the_footer_skips_an_action_with_no_key(self):
        self.config('[keys]\nfold = ["enter"]\n'); t = self.board(tmux=False); self.to_a2(t); self.open(t)
        self.assertEqual(last_text_row(t.snap()), " h/j/k/l move · q cancel")

    def cli(self, *a):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *a], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)


class Moving(Base):
    def test_h_l_wrap_across_rows_j_k_move_a_row_and_everything_clamps(self):
        t = self.board(tmux=False); self.to_a2(t); self.open(t)
        for _ in range(40): t.keys("h"); time.sleep(0.03)
        self.assertEqual(self.cursor(t), LISTING[0], "h at the very first stays")
        for _ in range(40): t.keys("k"); time.sleep(0.03)
        self.assertEqual(self.cursor(t), LISTING[0], "k on the top row stays")
        t.keys("l"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[1]); t.keys("j"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[6], "j: one grid row down, same column")
        t.keys("k"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[1]); t.keys("Right"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[2]); t.keys("Down"); time.sleep(0.15)
        self.assertEqual(self.cursor(t), LISTING[7]); t.keys("Left"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[6]); t.keys("Up"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[1])
        for _ in range(4): t.keys("l"); time.sleep(0.08)
        self.assertEqual(self.cursor(t), LISTING[5], "l at the end of a grid row goes on to the first of the next")
        for _ in range(40): t.keys("j"); time.sleep(0.04)
        self.assertEqual(LISTING.index(self.cursor(t)) // 5, (len(LISTING) - 1) // 5, "j stops on the last grid row")
        for _ in range(40): t.keys("l"); time.sleep(0.03)
        self.assertEqual(self.cursor(t), LISTING[-1], "l at the very last stays")

    def test_j_into_a_shorter_last_row_lands_on_the_last_one(self):
        write_avatar_set(self.cfg, "zz1", {"idle": 1}); order = self.listing(["zz1"])  # one more entry: the last grid row holds 1-5 cells
        t = self.board(tmux=False); self.to_a2(t); self.open(t); n = len(order); last_row = (n - 1) // 5; self.assertGreater(n % 5 or 5, 0)
        self.move_to(t, order[0], order); t.keys("l"); time.sleep(0.1)
        # walk to the last cell of the second-to-last row at a column that does not exist on the last row, then press j
        short = n - last_row * 5  # cells on the last row
        if short < 5:
            self.move_to(t, order[(last_row - 1) * 5 + 4], order); t.keys("j"); time.sleep(0.2)
            self.assertEqual(self.cursor(t), order[-1], f"the row below has only {short} cells: j goes to its last")
            t.keys("j"); time.sleep(0.2); self.assertEqual(self.cursor(t), order[-1], "j on the last row stays")
        else:
            write_avatar_set(self.cfg, "zz2", {"idle": 1})

    def test_the_cursor_starts_on_the_worn_avatar_and_the_name_is_in_the_focus_colour(self):
        self.env.avatars_json({"pj-fallback": "bot-03"}); t = self.board(tmux=False); self.to_a2(t); ov = self.open(t)
        self.assertEqual(self.cursor(t), "bot-03"); self.assertEqual([r["id"] for r in ov["rows"] if r["marker"]], ["bot-03"])
        t.keys("h"); time.sleep(0.2); ov = overlay(t.snap()); self.assertEqual(([r["id"] for r in ov["rows"] if r["marker"]], [r["id"] for r in ov["rows"] if r["current"]]), (["bot-02"], ["bot-03"]),
                                                                              "the worn one keeps its focus-coloured name when the cursor leaves it; the cursor cell is inverted")

    def test_a_set_can_be_the_worn_one_too(self):
        self.env.avatars_json({"pj-fallback": "aza_choi"}); t = self.board(tmux=False); self.to_a2(t); self.open(t); self.assertEqual(self.cursor(t), "aza_choi")

    def test_other_keys_do_nothing_while_the_grid_is_open(self):
        t = self.board(tmux=False); self.to_a2(t); self.open(t); cur = self.cursor(t); mask = lambda: re.sub(r"[▀▄█]", "", t.text())
        before = mask()
        for k in ("x", "f", "p", "i", "H", "r", "d", "y", "Tab", "s", "/", "n"): t.keys(k); time.sleep(0.15)
        time.sleep(0.4); self.assertEqual(mask(), before); self.assertEqual(self.cursor(t), cur); self.assertEqual(self.env.actions(), []); self.assertFalse(self.json.exists())


class Choosing(Base):
    def test_enter_binds_by_name_closes_and_the_card_changes(self):
        t = self.board(tmux=False); self.to_a2(t); self.open(t); self.move_to(t, "bot-04"); t.keys("Enter"); time.sleep(0.8)
        self.assertIsNone(overlay(t.snap())); self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-04"}); self.assertIn("pj-fallback wears Boo", "\n".join(r.text for r in t.snap()[-3:]))
        from test_t2 import sprite_blocks, run_avatars, chars
        blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in sprite_blocks(run_avatars(self.env).stdout)}
        c = [x for x in find_cards(t.snap()) if "pj-fallback" in x.line(1)][0]; self.assertEqual(chars(c), blocks["bot-04"])
        self.assertTrue(t.alive()); self.assertEqual(self.env.actions(), [], "binding calls neither ccmux nor tmux")

    def test_enter_on_a_set_binds_its_id(self):
        t = self.board(tmux=False); self.to_a2(t); self.open(t); self.move_to(t, "aza_choi"); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "aza_choi"}); self.assertIn("pj-fallback wears aza_choi", "\n".join(r.text for r in t.snap()[-3:]))

    def test_esc_and_q_cancel_without_writing(self):
        for key in ("esc", "q"):
            with self.subTest(key=key):
                self.setUp(); t = self.board(tmux=False); self.to_a2(t); self.open(t); t.keys("l"); time.sleep(0.2); t.keys("j"); time.sleep(0.2)
                if key == "esc": t.esc()
                else: t.keys("q")
                self.assertTrue(wait(lambda: overlay(t.snap()) is None, 3)); self.assertTrue(t.alive()); self.assertFalse(self.json.exists())
                t.keys("q"); self.assertEqual(t.exit_status(), 0, "and now q quits as usual")

    def test_enter_right_away_keeps_the_avatar(self):
        self.env.avatars_json({"pj-fallback": "bot-03"}); t = self.board(tmux=False); self.to_a2(t); self.open(t); t.keys("Enter"); time.sleep(0.6)
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-03"}); self.assertIsNone(overlay(t.snap()))

    def test_bot_06_is_gone_a_binding_in_the_state_file_falls_to_the_hash(self):
        self.env.avatars_json({"pj-fallback": "bot-06"}); t = self.board(tmux=False); self.to_a2(t)
        from test_t2 import sprite_blocks, run_avatars, chars
        blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in sprite_blocks(run_avatars(self.env).stdout)}
        c = [x for x in find_cards(t.snap()) if "pj-fallback" in x.line(1)][0]; self.assertIn(chars(c), blocks.values(), "an ordinary sprite, picked by the hash")
        t.close(); t2 = self.board(tmux=False); t2.keys("j"); time.sleep(0.3); self.env.avatars_json({}); self.assertTrue(t2.alive())

    def test_a_config_binding_to_bot_06_is_an_error_that_says_it_was_removed(self):
        for text, tail in (('[avatars.bind]\n"x" = "bot-06"\n', True), ('[avatars]\ndefault = "bot-06"\n', True), ('[avatars.bind]\n"x" = "bot-99"\n', False)):
            with self.subTest(text=text):
                self.config(text, default=None) if False else (self.cfg / "agmux" / "config.toml").write_text(text)
                r = subprocess.run([BIN, "config"], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg)}, capture_output=True, text=True, timeout=10)
                self.assertNotEqual(r.returncode, 0); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)
                self.assertEqual(r.stderr.strip().endswith("; bot-06 was removed; pick another"), tail, r.stderr); self.assertRegex(r.stderr, r"an avatar id"); self.assertNotIn("bot-06,", r.stderr.split("(")[1].split(")")[0])


class Scrolling(Base):
    def many(self, n=20):
        names = [f"set{k:02d}" for k in range(n)]
        for nm in names: write_avatar_set(self.cfg, nm, {"idle": 1})
        return names, self.listing(names)

    def test_more_than_four_rows_scroll_with_the_cursor_row_in_the_middle(self):
        names, order = self.many(); t = self.board(tmux=False, w=200, h=50, loose=True); t.keys("j"); time.sleep(0.3); ov = self.open(t); total = (len(order) + 4) // 5
        self.assertEqual(ov["shown"], 4); self.move_to(t, order[0], order)
        for target in (7, 12, 17, 22, 27, len(order) - 1, 3, 0):
            self.move_to(t, order[target], order); ov = overlay(t.snap()); first = min(max(target // 5 - 4 // 2, 0), total - 4)
            self.assertEqual(ov["shown"], 4); ids = [r["id"] for r in ov["rows"]]
            self.assertEqual(ids, order[first * 5:(first + 4) * 5][:len(ids)], f"cursor on {order[target]} (grid row {target // 5}): rows {first}..{first + 3} are shown (cursor row second from the top)")
            self.assertEqual(self.cursor(t), order[target])

    def test_scrolling_in_a_short_terminal_keeps_the_cursor_visible_to_the_last_one_and_binds_bot_05(self):
        names, order = self.many(); t = self.board(tmux=False, w=120, h=30, loose=True); t.keys("j"); time.sleep(0.3); self.open(t)
        for k in range(len(order) + 5):
            t.keys("l"); time.sleep(0.04); ov = overlay(t.snap()); self.assertTrue(ov, f"grid vanished after {k + 1} presses:\n{t.text()}"); self.assertEqual(len([r for r in ov["rows"] if r["marker"]]), 1, t.text())
            self.assertTrue(all(0 <= r["row"] < 30 for r in ov["rows"]))
        self.assertEqual(self.cursor(t), "bot-05"); t.keys("Enter"); time.sleep(0.8); self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-05"})

    def test_k_back_up_keeps_it_visible_too(self):
        names, order = self.many(); t = self.board(tmux=False, w=120, h=30, loose=True); t.keys("j"); time.sleep(0.3); self.open(t)
        for _ in range(len(order) + 3): t.keys("l"); time.sleep(0.03)
        for k in range(len(order) + 3):
            t.keys("h"); time.sleep(0.04); ov = overlay(t.snap()); self.assertTrue(ov, t.text()); self.assertEqual(len([r for r in ov["rows"] if r["marker"]]), 1, f"{k}: {t.text()}")
        self.assertEqual(self.cursor(t), order[0])

    def test_user_sets_come_after_the_built_in_ones_in_name_order_and_before_the_sprites(self):
        for n in ("zeta", "mine"): write_avatar_set(self.cfg, n, {"idle": 1})
        order = self.listing(["mine", "zeta"]); t = self.board(tmux=False, w=200, h=50, loose=True); t.keys("j"); time.sleep(0.3); self.open(t)
        for _ in range(len(order)): t.keys("l"); time.sleep(0.03)
        seen = []
        for k in range(len(order) + 2):
            seen.append(self.cursor(t)); t.keys("h"); time.sleep(0.05)
        self.assertEqual(seen[:len(order)][::-1], order)
        self.assertEqual(order[N_SETS:N_SETS + 2], ["mine", "zeta"], "user sets right after the built-in ones")

    def test_a_set_with_a_sprite_name_takes_the_sprite_off_the_list(self):
        write_avatar_set(self.cfg, "bot-02", {"idle": 1}); order = self.listing(["bot-02"]); self.assertEqual(len(order), len(LISTING))
        t = self.board(tmux=False); self.to_a2(t); ov = self.open(t); self.assertEqual([r["id"] for r in ov["rows"]], order)
        self.assertNotIn("Boxy", "\n".join(r.text for r in t.snap())); self.assertEqual(len([r for r in ov["rows"] if r["id"] == "bot-02"]), 1)


class ImageMode(Base):
    def board_img(self, **kw):
        return self.board(tmux=True, **kw)

    def cell_ids(self, raw, ov):
        out = {}
        for r in ov["rows"]:
            cells = [c for c in ph_cells(raw) if r["row"] <= c[0] <= r["row"] + 1 and r["col"] <= c[1] < r["col"] + ov["cell_w"]]
            out[r["id"]] = {c[2] for c in cells}, len(cells)
        return out

    def test_set_cells_are_pictures_the_first_idle_frame_and_sprites_stay_pixels(self):
        self.config('[avatars]\ndefault = "bot-01"\n'); table, _ = frame_table(self.env, self.cfg)
        t = self.board_img(); self.to_a2(t); ov = self.open(t); raw = t.raw(); got = self.cell_ids(raw, ov)
        for sid in LISTING[:N_SETS]:
            first_idle = next(k for k in table if k[0] == sid and k[1] == "idle")
            self.assertEqual(got[sid], ({table[first_idle]}, 12), f"{sid}: its idle frame 0 (the calm face), 6x2 placeholders")
        for sid in IDS: self.assertEqual(got[sid], (set(), 0), f"{sid} is a pixel sprite")

    def test_the_picker_faces_do_not_blink(self):
        self.config('[avatars]\ndefault = "bot-01"\n'); t = self.board_img(); self.to_a2(t); ov = self.open(t); seen = set()
        for _ in range(60):
            g = self.cell_ids(t.raw(), ov); seen |= {(k, tuple(sorted(v[0]))) for k, v in g.items() if k == "aza_choi"}; time.sleep(0.1)
        self.assertEqual(len(seen), 1, f"aza_choi's idle has a blink frame; the picker shows the calm one only: {seen}")

    def test_enter_on_a_set_turns_the_card_into_a_picture(self):
        self.config('[avatars]\ndefault = "bot-01"\n'); t = self.board_img(); self.to_a2(t); card = self.focused(t); self.assertEqual(card_avatar(t.raw(), card), [])
        self.open(t); self.move_to(t, "aza_choi"); t.keys("Enter")
        self.assertTrue(wait(lambda: len(card_avatar(t.raw(), [c for c in cards_of(t.raw()) if "pj-fallback" in c.line(1) and find_grid(parse_screen(t.raw())) is None][0])) == 12 if find_grid(t.snap()) is None else False, 3),
                        "the card shows placeholders within a tick or two")

    def test_default_hash_in_image_mode_picks_image_sets_only(self):  # T36
        self.config('[avatars]\ndefault = "hash"\n'); t = self.board_img(); raw = t.raw(); cs = cards_of(raw); self.assertEqual(len(cs), 9)
        self.assertTrue(all(len(card_avatar(raw, c)) == 12 for c in cs), "no unbound session wears a pixel sprite")


if __name__ == "__main__":
    unittest.main()
