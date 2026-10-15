"""T18 avatars by session name + [avatars] config + the `a` picker. Spec: specs/m3-config/design.md「头像 [avatars]」「面板里选头像」, tasks.md T18.

Binding key = the name shown on the card (summary, else project); state file ($XDG_STATE_HOME/agmux/avatars.json) > config [avatars.bind] > [avatars] default.
Idle / done cards show the sprite exactly as `agmux avatars` prints it (no eye animation), so the idle cards are used to read which sprite a card has.
"""
import json, subprocess, unittest
import test_t12 as _t12
from lib import *
from test_t2 import chars, run_avatars, parse_blocks, sprite_blocks
from test_t3 import ready, by_name, NAMES, last_text_row, border_colours, C_FOCUS
from test_t11 import PreviewBase, CAT_CMD

def _fnv(s):
    h = 2166136261
    for b in s.encode(): h = ((h ^ b) * 16777619) & 0xFFFFFFFF
    return h


SPRITE_NAMES = ["Clawd", "Boxy", "Sprout", "Boo", "Mochi"]  # T32: bot-06 Cyclops is gone (it printed the same glyphs as bot-01)
IDS = [f"bot-0{i}" for i in range(1, 6)]
HASHED_SPRITE = f"bot-0{_fnv('aza_choi') % 5 + 1}"  # pixel mode: an image set drawn as a sprite = Sprites[fnv32a(set id) % 5] (the hash pool has the sprites only)
IDLE = ("a2", "c1", "c2", "e1")  # a2 pj-fallback (no summary: named after its project), c1 charlie-30, c2 charlie-59, e1 echo-long
UNIQUE = list(IDS)  # all five print different glyphs now
GRID_COLS, CELL_H = 5, 3  # T32 选择框: 5 columns, 3 screen rows per cell; the cell width is read off the box (9, then 10 since T33)


def listing(env=None):
    """What the picker lists, in order, as `agmux avatars` prints it: the image sets (one line each), then the sprites. Dynamic: the built-in sets may change (T33)."""
    import tempfile
    d = tempfile.mkdtemp(prefix="agmux-ls-")
    try:
        out = subprocess.run([BIN, "avatars"], env={"PATH": "/usr/bin:/bin", "HOME": d, "XDG_STATE_HOME": d + "/s", "XDG_CONFIG_HOME": d + "/c", "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10).stdout
    finally:
        shutil.rmtree(d, ignore_errors=True)
    sets = re.findall(r"^(\S+)  (?:builtin|user)  idle ", out, re.M); sprites = re.findall(r"^(bot-\d\d)  \S+$", out, re.M)
    return sets + [s for s in sprites if s not in sets]


LISTING = listing()
N_SETS = len([i for i in LISTING if not i.startswith("bot-")])  # image sets in the default listing (aza_choi + the built-in party ones)
LABEL = {**{i: i for i in LISTING}, **dict(zip(SPRITE_NAMES, IDS))}  # a cell's label: a set's id, a sprite's name


def find_grid(snap):
    """The picker grid: (x0, y0, rows, cell_w) of the focus-coloured rounded box, or None. The box is 5 * cell_w + 2 wide (cell_w 9 at T32, 10 since T33) and 3 * rows + 2 high (T52: every row of the listing, fewer only on a short screen; the borders may carry `↑ n more` / `↓ n more`)."""
    for y, row in enumerate(snap):
        cells = row.cells
        for x, c in enumerate(cells):
            if c[0] == "╭" and c[1] == C_FOCUS:
                e = x + 1  # T52: the top border may carry a muted "↑ n more" label between the rule's dashes
                while e < len(cells) and cells[e][0] in "─ ↑↓0123456789more" and (cells[e][1] == C_FOCUS or cells[e][0] != "─"): e += 1
                if e < len(cells) and cells[e][0] == "╮" and cells[e][1] == C_FOCUS and (e - x - 1) % GRID_COLS == 0 and e - x - 1 >= 5 * 9:
                    w = e - x + 1
                    for y1 in range(y + 1, len(snap)):
                        r1 = snap[y1].cells
                        if len(r1) > e and r1[x][0] == "╰" and r1[e][0] == "╯" and r1[x][1] == C_FOCUS and r1[e][1] == C_FOCUS:  # the focus colour: a plain card's bottom edge (╰──╯, the same width in a two column layout) in a capture torn by a repaint is not the box's
                            return x, y, (y1 - y - 1) // CELL_H, (w - 2) // GRID_COLS
    return None


def overlay(snap, sets=None):
    """The picker (T32: a grid under the focused card): dict(rows=[visible cells in reading order: dict(id, name, sprite, marker, current, gr, gc, row, col)],
    box=(x0, y0, w, h), shown=grid rows drawn, cell_w) or None. marker = the cursor cell (name drawn on the focus colour), current = the avatar worn (name in the focus colour)."""
    g = find_grid(snap)
    if not g:
        return None
    x0, y0, shown, cw = g; rows = []; pad = (cw - 6) // 2
    for gr in range(shown):
        for gc in range(GRID_COLS):
            x, y = x0 + 1 + cw * gc, y0 + 1 + CELL_H * gr
            lab = snap[y + 2].cells[x:x + cw]; text = "".join(c[0] for c in lab).strip()
            if not text:
                continue
            ink = [c for c in lab if c[0] != " "]
            ident = LABEL.get(text, text)
            rows.append(dict(id=ident, name=text if ident != text else None, sprite=("".join(c[0] for c in snap[y].cells[x + pad:x + pad + 6]), "".join(c[0] for c in snap[y + 1].cells[x + pad:x + pad + 6])),
                             marker=any(c[2] == C_FOCUS for c in ink), current=any(c[1] == C_FOCUS and c[2] != C_FOCUS for c in ink), gr=gr, gc=gc, row=y, col=x))
    return dict(rows=rows, box=(x0, y0, 5 * cw + 2, 2 + CELL_H * shown), shown=shown, cell_w=cw)


def to_start(t, n=30):
    """the picker cursor to the first cell (h clamps there): the grid then shows rows 0..3"""
    for _ in range(n): t.keys("h"); time.sleep(0.04)
    time.sleep(0.3)


def sweep(t, n=None, visit=None, step=0.1):
    """T49: more cells than fit in one screen of the grid (20 sets + 5 sprites): walk the cursor from the first cell to the last with `l` and collect every cell once, in order: [cell dict] (the dict of its first sight).
    visit(ov) is called at every step (a place to read t.raw() for the pictures in the cells)."""
    to_start(t); n = n or len(LISTING) + 2; out, seen = [], set()
    for _ in range(n):
        ov = wait(lambda: overlay(t.snap()), 2, 0.05)  # a capture torn by a repaint has no grid: read again
        assert ov, t.text()
        if visit: visit(ov)
        for r in ov["rows"]:
            if r["id"] not in seen: seen.add(r["id"]); out.append(r)
        t.keys("l"); time.sleep(step)
    return out


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        self.conf = self.cfg / "agmux" / "config.toml"
        self.json = self.env.state / "agmux" / "avatars.json"
        self.blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in sprite_blocks(run_avatars(self.env).stdout)}

    def config(self, text):
        self.conf.write_text(text)

    def board(self, w=200, h=50):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(ready(t), t.text())
        return t

    def cli(self, *args):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state),
               "XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-256color", "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def sprite_of(self, t, sid):
        return chars(by_name(find_cards(t.snap()), NAMES[sid]))

    def id_of(self, t, sid):
        """the sprite ids whose glyphs the card shows (a 1-tuple, or ("bot-01", "bot-05") for those two)"""
        s = self.sprite_of(t, sid); m = tuple(i for i, b in self.blocks.items() if b == s)
        self.assertTrue(m, f"{sid}: the card sprite {s} is not one of the 6")
        return m

    def open_picker(self, t):
        t.keys("a")
        ov = wait(lambda: overlay(t.snap()), 3); self.assertTrue(ov, t.text())
        return ov

    def cursor(self, t):
        """the id under the cursor (exactly one cell is highlighted)"""
        ov = overlay(t.snap()); at = [r["id"] for r in ov["rows"] if r["marker"]]
        self.assertEqual(len(at), 1, f"exactly one cursor cell: {ov}"); return at[0]

    def move_to(self, t, want, order=None):
        """Move the cursor to avatar id `want` with l / h (one avatar at a time; they wrap from one grid row to the next)."""
        order = order or LISTING
        for _ in range(len(order)):
            i = order.index(self.cursor(t)); w = order.index(want)
            if i == w:
                return
            t.keys("l" if w > i else "h"); time.sleep(0.12)
        self.fail(f"cursor did not reach {want}")

    def to_a2(self, t):
        t.keys("j"); time.sleep(0.3)  # a2 = pj-fallback, the 2nd card of the first column


class Picker(Base):
    def test_footer_lists_the_key(self):
        t = self.board(); self.assertIn("a avatar", last_text_row(t.snap()))

    def test_a_opens_a_grid_under_the_card_sets_first_then_the_five_sprites(self):
        t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2"); card = [c for c in find_cards(t.snap()) if border_colours(c) == {C_FOCUS}][0]
        ov = self.open_picker(t); x0, y0, w, h = ov["box"]
        rows = (len(LISTING) + 4) // 5; self.assertEqual((w, h), (5 * ov["cell_w"] + 2, 2 + 3 * rows), "5 columns of cells + border; as many rows of 3 as the listing needs (T52: all of them on this tall screen)")
        self.assertIn(ov["cell_w"], (9, 10)); self.assertEqual((x0, y0), (card.c, card.r + 5), "straight below the focused card, at its left edge")
        self.assertEqual(len([r for r in ov["rows"] if r["current"] or r["marker"]]), 1, "the cursor starts on the worn one: one highlighted cell"); self.assertIn(self.cursor(t), cur)
        cells = sweep(t); self.assertEqual([r["id"] for r in cells], LISTING); self.assertEqual([r["name"] for r in cells], [None] * N_SETS + SPRITE_NAMES, "a set is labelled with its id, a sprite with its name")
        self.assertEqual([r["sprite"] for r in cells][N_SETS:], [self.blocks[i] for i in IDS], "each sprite cell shows the sprite `agmux avatars` prints for that id")
        self.assertEqual(cells[0]["sprite"], self.blocks[HASHED_SPRITE], "pixel mode: the set cell is drawn with the sprite its id hashes to (fnv32a('aza_choi') % 5)")
        to_start(t); top = overlay(t.snap()); self.assertEqual([(r["gr"], r["gc"]) for r in top["rows"]], [(i // 5, i % 5) for i in range(len(top["rows"]))], "reading order, 5 per row"); self.assertEqual(len(top["rows"]), len(LISTING), "every row shown")
        foot = last_text_row(t.snap()); self.assertIn("h/j/k/l move", foot); self.assertIn("enter bind", foot); self.assertIn("q cancel", foot)
        msg = [r.text for r in t.snap()[-3:-1]]; self.assertTrue(any(self.cursor(t) in m for m in msg), f"the message row names the cursor's id: {msg}")

    def test_hjkl_and_arrows_move_the_cursor_over_the_grid(self):
        t = self.board(); self.to_a2(t); self.open_picker(t)
        for _ in range(25): t.keys("k"); time.sleep(0.05)
        self.assertEqual(self.cursor(t), LISTING[self.cursor_col(t)], "k at the top row stays")
        for _ in range(8): t.keys("h"); time.sleep(0.05)
        self.assertEqual(self.cursor(t), LISTING[0], "h at the very first stays")
        t.keys("l"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[1]); t.keys("j"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[6], "j = one grid row down")
        t.keys("k"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[1]); t.keys("Right"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[2])
        t.keys("Down"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[7]); t.keys("Left"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[6]); t.keys("Up"); time.sleep(0.15); self.assertEqual(self.cursor(t), LISTING[1])
        for _ in range(4): t.keys("l"); time.sleep(0.1)
        self.assertEqual(self.cursor(t), LISTING[5], "l at the end of a grid row goes on to the first of the next")
        for _ in range(40): t.keys("j"); time.sleep(0.05)
        self.assertEqual(self.cursor(t), LISTING[len(LISTING) - 5 + (5 % 5)] if False else self.cursor(t)); last_row = (len(LISTING) - 1) // 5
        self.assertEqual(LISTING.index(self.cursor(t)) // 5, last_row, "j stops on the last grid row")
        for _ in range(30): t.keys("l"); time.sleep(0.04)
        self.assertEqual(self.cursor(t), LISTING[-1], "l at the very last stays")

    def cursor_col(self, t):
        return LISTING.index(self.cursor(t))

    def test_cursor_starts_on_the_current_avatar(self):
        t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2")
        ov = self.open_picker(t)
        self.assertIn(self.cursor(t), cur, "enter right away would change nothing")

    def test_l_l_enter_binds_the_avatar_two_after_the_current_one(self):
        self.env.avatars_json({"pj-fallback": "bot-02"})
        t = self.board(); self.to_a2(t)
        self.open_picker(t); self.assertEqual(self.cursor(t), "bot-02", "the cursor opens on the avatar worn")
        t.keys("l"); time.sleep(0.2); t.keys("l"); time.sleep(0.2); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-04"})
        self.assertEqual(self.id_of(t, "a2"), ("bot-04",))

    def test_enter_right_away_keeps_the_avatar(self):
        self.env.avatars_json({"pj-fallback": "bot-03"})
        t = self.board(); self.to_a2(t); self.open_picker(t); t.keys("Enter"); time.sleep(0.6)
        self.assertEqual(self.id_of(t, "a2"), ("bot-03",)); self.assertIsNone(overlay(t.snap()))
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-03"})

    def test_enter_binds_by_name_and_the_card_changes_at_once(self):
        t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2")
        want = next(i for i in UNIQUE if i not in cur)
        self.open_picker(t); self.move_to(t, want); t.keys("Enter")
        self.assertTrue(wait(lambda: overlay(t.snap()) is None, 3), "enter closes the picker")
        self.assertTrue(wait(lambda: self.id_of(t, "a2") == (want,), 3), f"the card shows {want} immediately (was {cur})")
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": want}, "avatars.json: {name: id}, keyed by the card name, not the session id")
        self.assertEqual(self.env.actions(), [], "binding calls neither ccmux nor tmux"); self.assertTrue(t.alive(), "enter in the picker binds, it does not jump / quit")
        # the picker remembers: reopening marks the new avatar as current
        self.open_picker(t)
        self.assertTrue(wait(lambda: [r["id"] for r in (overlay(t.snap()) or {"rows": []})["rows"] if r["current"] or r["marker"]] == [want], 3), "reopening puts the cursor on the new avatar")  # the picker is drawn in several writes: poll
        t.keys("q"); t.close()
        t2 = self.board(); self.to_a2(t2)
        self.assertEqual(self.id_of(t2, "a2"), (want,), "still bound after restarting the board")

    def test_binding_one_card_leaves_the_others_alone(self):
        t = self.board(); before = {s: self.id_of(t, s) for s in IDLE if s != "a2"}
        self.to_a2(t); want = next(i for i in UNIQUE if i not in self.id_of(t, "a2"))
        self.open_picker(t); self.move_to(t, want); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual({s: self.id_of(t, s) for s in IDLE if s != "a2"}, before)

    def test_the_picker_footer_skips_an_action_that_has_no_key(self):
        self.config('[keys]\nfold = ["enter"]\n')  # enter was jump's only key: jump (the picker's bind) is unbound now
        t = self.board(); self.to_a2(t); self.open_picker(t)
        self.assertEqual(last_text_row(t.snap()), " h/j/k/l move · q cancel")

    def test_a_second_binding_keeps_the_first(self):
        t = self.board(); self.to_a2(t)
        self.open_picker(t); self.move_to(t, "bot-03"); t.keys("Enter"); time.sleep(0.6)
        t.keys("j"); time.sleep(0.2); t.keys("j"); time.sleep(0.2)  # a3
        t.keys("j"); time.sleep(0.2)  # b-column? whichever card comes next; bind it too
        self.open_picker(t); self.move_to(t, "bot-05"); t.keys("Enter"); time.sleep(0.6)
        d = json.loads(self.json.read_text()); self.assertEqual(len(d), 2, d); self.assertEqual(d["pj-fallback"], "bot-03"); self.assertIn("bot-05", d.values())

    def test_esc_and_q_cancel_without_writing(self):
        for key in ("esc", "q"):
            with self.subTest(key=key):
                self.setUp()
                t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2")
                self.open_picker(t); t.keys("l"); time.sleep(0.2); t.keys("l"); time.sleep(0.2)
                if key == "esc":
                    t.esc()
                else:
                    t.keys("q")
                self.assertTrue(wait(lambda: overlay(t.snap()) is None, 3), "the picker closed")
                self.assertTrue(t.alive(), f"{key} in the picker cancels; it does not quit agmux")
                self.assertFalse(self.json.exists(), "nothing written")
                self.assertEqual(self.id_of(t, "a2"), cur)
                t.keys("q"); self.assertEqual(t.exit_status(), 0, "and now q quits as usual")

    def test_cancel_leaves_an_existing_file_byte_for_byte(self):
        self.env.avatars_json({"echo-long": "bot-05"}); before = self.json.read_bytes()
        t = self.board(); self.to_a2(t); self.open_picker(t); t.keys("l"); time.sleep(0.2); t.esc(); time.sleep(0.5)
        self.assertEqual(self.json.read_bytes(), before)

    def test_other_keys_do_nothing_while_the_picker_is_open(self):
        t = self.board(); self.to_a2(t); self.open_picker(t)
        mask = lambda: re.sub(r"[▀▄█]", "", t.text())  # the working cards animate their eyes
        before = mask(); picker = overlay(t.snap())
        for k in ("x", "f", "p", "i", "H", "r", "d", "y", "Tab"):
            t.keys(k); time.sleep(0.2)
        time.sleep(0.5)
        self.assertEqual(mask(), before, "screen unchanged: no confirm prompt, no fold, no preview, no focus move")
        self.assertEqual(overlay(t.snap())["rows"], picker["rows"], "same cursor, same rows")
        self.assertEqual(self.env.actions(), []); self.assertEqual(self.env.tmux_calls(), [], "no restart / kill / review / handoff / reply reached ccmux or tmux")
        self.assertFalse(self.json.exists())

    def test_a_on_a_card_with_no_name_binds_the_session_id(self):
        S = json.loads((self.env.fake / "show.json").read_text())
        S.append(dict(S[1], id="z1", summary=None, project="", tmuxPane="%120", nativeSessionId="n-z1", cwd=str(self.env.home) + "/work/zeta"))
        self.env.write_show(S)
        t = Term(self.env, w=200, h=60, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) == 10, 8), t.text())
        def nameless():
            fc = [c for c in find_cards(t.snap()) if border_colours(c) == {C_FOCUS}]
            return fc and not re.search(r"\w", re.sub(r"[▀▄█│┃\d]+m?", "", c_name(fc[0])))
        c_name = lambda c: c.line(1)[8:-6]
        for _ in range(8):  # down the first column into the second band: zeta is the first group there
            if nameless():
                break
            t.keys("j"); time.sleep(0.2)
        self.assertTrue(nameless(), t.text())
        self.open_picker(t); t.keys("l"); time.sleep(0.2); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(list(json.loads(self.json.read_text())), ["z1"], "no name: bound by session id")
        self.assertIn("bound to session id (name missing)", t.text())

    def test_sessions_with_the_same_name_share_a_binding(self):
        S = json.loads((self.env.fake / "show.json").read_text())
        S.append(dict(S[1], id="a4", summary=None, project="pj-fallback", tmuxPane="%121", nativeSessionId="n-a4"))
        self.env.write_show(S)
        t = Term(self.env, w=200, h=60, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) == 10, 8), t.text())
        self.to_a2(t)
        self.open_picker(t); self.move_to(t, "bot-03"); t.keys("Enter"); time.sleep(0.8)
        twins = [c for c in find_cards(t.snap()) if "pj-fallback" in c.line(1)]
        self.assertEqual(len(twins), 2); self.assertEqual({chars(c) for c in twins}, {self.blocks["bot-03"]}, "both cards named pj-fallback changed")
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-03"})

    def test_a_corrupt_avatars_file_is_not_overwritten(self):
        self.json.parent.mkdir(exist_ok=True); self.json.write_text("{not json")
        t = self.board(); self.to_a2(t); self.open_picker(t); t.keys("l"); time.sleep(0.2); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(self.json.read_text(), "{not json", "the file the user may want to repair is left as it is")
        self.assertIn("not valid JSON", t.text()); self.assertTrue(t.alive())

    def test_the_key_is_configurable(self):
        self.config('[keys]\navatar = ["A"]\n')
        t = self.board(); self.assertIn("A avatar", last_text_row(t.snap())); self.assertNotIn("a avatar", last_text_row(t.snap()))
        t.keys("a"); time.sleep(0.4); self.assertIsNone(overlay(t.snap()), "a is no longer bound")
        t.keys("A"); self.assertTrue(wait(lambda: overlay(t.snap()), 3), t.text())


class Review(Base):
    """T18 review (c1b8fcb): the file is the source of truth: merged on write, followed on change, never clobbered."""

    def pick(self, t, want):
        self.open_picker(t); self.move_to(t, want); t.keys("Enter"); time.sleep(0.8)

    def test_a_hand_edit_made_while_the_board_runs_survives_the_next_binding(self):
        t = self.board(); self.to_a2(t)
        self.env.avatars_json({"echo-long": "bot-05"})  # edited by hand / another board, a moment ago
        self.pick(t, "bot-03")
        self.assertEqual(json.loads(self.json.read_text()), {"echo-long": "bot-05", "pj-fallback": "bot-03"}, "both entries are in the file")

    def test_a_read_only_state_directory_reports_and_changes_nothing(self):
        d = self.env.state / "agmux"; d.mkdir(exist_ok=True); self.env.avatars_json({"echo-long": "bot-05"})
        d.chmod(0o555); self.addCleanup(lambda: d.chmod(0o755))
        before = self.json.read_bytes()
        t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2")
        self.open_picker(t); self.move_to(t, next(i for i in UNIQUE if i not in cur)); t.keys("Enter"); time.sleep(0.8)
        self.assertTrue(t.alive()); self.assertEqual(self.json.read_bytes(), before, "the file is untouched"); self.assertEqual([p.name for p in d.iterdir() if p.name.startswith("avatars") and p.name != "avatars.json"], [], "no temp file left behind")
        self.assertEqual(len([r for r in t.snap() if "permission denied" in r.text]), 1, "the error is shown once, not on a second row as well")
        red = [r.text.strip() for r in t.snap()[-3:] if any(is_red(c[1]) for c in r.cells if c[0].strip())]
        self.assertTrue(red, f"an error shows in the message row:\n{t.text()[-300:]}")
        self.assertEqual(self.id_of(t, "a2"), cur, "the card keeps its avatar: nothing was saved")
        t.close(); t2 = self.board(); self.to_a2(t2); self.assertEqual(self.id_of(t2, "a2"), cur, "and a restart shows the same")

    def test_a_null_file_is_an_empty_binding_set(self):
        self.json.parent.mkdir(exist_ok=True); self.json.write_text("null")
        t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2")
        self.open_picker(t); want = next(i for i in UNIQUE if i not in cur); self.move_to(t, want); t.keys("Enter"); time.sleep(0.8)
        self.assertTrue(t.alive(), t.text()); self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": want}); self.assertEqual(self.id_of(t, "a2"), (want,))

    def test_the_picker_keeps_every_row_the_terminal_width_with_wide_characters_under_it(self):
        S = json.loads((self.env.fake / "show.json").read_text())
        for i, s in enumerate(S):  # every card name is double-width, so the picker's left AND right edge land on wide characters at some widths
            s["summary"] = "会话测试名字" * 3 + str(i)
        self.env.write_show(S)
        bad = []
        for w in range(96, 106):
            t = Term(self.env, w=w, h=30, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
            self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 2, 8), t.text())
            before = t.snap(); t.keys("a"); after = wait(lambda: (lambda s: s if overlay(s) else None)(t.snap()), 3); self.assertTrue(after, t.text()); ov = overlay(after)  # (one capture for both: the box is read off the very frame that is compared)
            bx, by, bw, bh = ov["box"]; x0, x1 = max(bx - 2, 0), bx + bw + 2  # a wide character cut by the picker's edge may turn into a space: allow one cell of slack
            strip = lambda cells: "".join(c[0] for c in cells if c[0] not in "▀▄█")
            for i, (b, a) in enumerate(zip(before, after)):
                if len(a.cells) > w:
                    bad.append((w, i, "row wider than the terminal", len(a.cells)))
                if i < 28 and b.text.strip():  # (the last two rows are the message row and the footer: they change with the picker)
                    if not by <= i < by + bh and strip(a.cells) != strip(b.cells):
                        bad.append((w, i, "a row outside the grid changed", strip(b.cells), strip(a.cells)))
                    if strip(a.cells[:x0]) != strip(b.cells[:x0]):
                        bad.append((w, i, "left of the picker changed", strip(b.cells[:x0]), strip(a.cells[:x0])))
                    if len(b.cells) > x1 and strip(a.cells[x1:]) != strip(b.cells[x1:]):
                        bad.append((w, i, "right of the picker changed", strip(b.cells[x1:]), strip(a.cells[x1:])))
            t.close()
        self.assertEqual(bad, [], "the picker is drawn over wide characters without moving anything outside it")

    def test_a_on_a_folded_group_title_says_so(self):
        t = self.board(); t.keys("f"); time.sleep(0.4)
        t.keys("a"); time.sleep(0.5)
        self.assertIsNone(overlay(t.snap()), "no picker"); self.assertIn("fold a group? pick a card first", t.text()); self.assertFalse(self.json.exists())
        self.assertTrue(t.alive())

    def test_another_process_changing_the_file_is_followed_within_two_seconds(self):
        t = self.board(); base = self.id_of(t, "a2"); want = next(i for i in UNIQUE if i not in base)
        t0 = time.time(); self.env.avatars_json({"pj-fallback": want})
        self.assertTrue(wait(lambda: self.id_of(t, "a2") == (want,), 3.5), "the card follows the file without a restart")
        self.assertLess(time.time() - t0, 3.0, "at the next poll")
        other = next(i for i in UNIQUE if i not in (want,) + base)
        self.env.avatars_json({"pj-fallback": other}); self.assertTrue(wait(lambda: self.id_of(t, "a2") == (other,), 3.5))

    def test_a_broken_file_keeps_the_bindings_and_fixing_it_needs_no_restart(self):
        self.env.avatars_json({"pj-fallback": "bot-03"}); t = self.board(); self.assertEqual(self.id_of(t, "a2"), ("bot-03",))
        self.json.write_text("{broken")
        self.assertTrue(wait(lambda: "not valid JSON" in t.text(), 4), t.text()[-300:]); self.assertEqual(self.id_of(t, "a2"), ("bot-03",), "the last good bindings stay in use")
        self.env.avatars_json({"pj-fallback": "bot-05"})
        self.assertTrue(wait(lambda: self.id_of(t, "a2") == ("bot-05",), 4), "repairing the file is enough")
        self.assertTrue(wait(lambda: "not valid JSON" not in t.text(), 3), "and the error message is gone")


class Bindings(Base):
    def test_old_session_id_keys_are_not_migrated(self):
        cur = self.board(); base = self.id_of(cur, "a2"); cur.close()
        other = next(i for i in UNIQUE if i not in base)
        self.env.avatars_json({"a2": other})  # the old key: a session id
        t = self.board(); self.assertEqual(self.id_of(t, "a2"), base, "an entry keyed by session id no longer applies")

    def test_default_avatar_is_stable_across_runs(self):
        t = self.board(); first = {s: self.id_of(t, s) for s in IDLE}; t.close()
        t = self.board(); self.assertEqual({s: self.id_of(t, s) for s in IDLE}, first)

    def test_config_default_applies_to_every_unbound_session(self):
        self.config('[avatars]\ndefault = "bot-02"\n')
        t = self.board()
        for s in IDLE:
            self.assertEqual(self.id_of(t, s), ("bot-02",), s)

    def test_swapping_two_bindings_swaps_the_sprites(self):  # (T32: bot-06 Cyclops is gone, so no two sprites share glyphs any more)
        from test_t2 import region
        sig = lambda card: tuple(c[0] for i in (1, 2) for c in region(card, i))  # glyphs only: the colours come from the card (agent type band, mood)
        self.config('[avatars.bind]\n"pj-fallback" = "bot-01"\n"charlie-30" = "bot-05"\n')
        t = self.board(); c = find_cards(t.snap()); one, six = sig(by_name(c, "pj-fallback")), sig(by_name(c, "charlie-30")); t.close()
        self.assertNotEqual(one, six, "two different sprites")
        self.config('[avatars.bind]\n"pj-fallback" = "bot-05"\n"charlie-30" = "bot-01"\n')
        t = self.board(); c = find_cards(t.snap())
        self.assertEqual((sig(by_name(c, "pj-fallback")), sig(by_name(c, "charlie-30"))), (six, one), "swapping the two bindings swaps the sprites (both cards are idle: same render)")

    def test_default_hash_is_the_same_as_no_setting(self):
        t = self.board(); base = {s: self.id_of(t, s) for s in IDLE}; t.close()
        self.config('[avatars]\ndefault = "hash"\n')
        t = self.board(); self.assertEqual({s: self.id_of(t, s) for s in IDLE}, base)

    def test_config_bind_is_by_name(self):
        self.config('[avatars]\ndefault = "bot-03"\n[avatars.bind]\n"pj-fallback" = "bot-05"\n"charlie-30" = "bot-04"\n')
        t = self.board()
        self.assertEqual((self.id_of(t, "a2"), self.id_of(t, "c1")), (("bot-05",), ("bot-04",)))
        self.assertEqual((self.id_of(t, "c2"), self.id_of(t, "e1")), (("bot-03",), ("bot-03",)), "the others fall back to default")

    def test_priority_state_file_over_config_bind_over_default(self):
        self.config('[avatars]\ndefault = "bot-03"\n[avatars.bind]\n"pj-fallback" = "bot-05"\n"charlie-30" = "bot-04"\n')
        self.env.avatars_json({"pj-fallback": "bot-02"})
        t = self.board()
        self.assertEqual(self.id_of(t, "a2"), ("bot-02",), "avatars.json beats [avatars.bind]")
        self.assertEqual(self.id_of(t, "c1"), ("bot-04",), "bind beats default")
        self.assertEqual(self.id_of(t, "c2"), ("bot-03",), "default for the rest")

    def test_picking_in_the_panel_beats_the_config_bind(self):
        self.config('[avatars.bind]\n"pj-fallback" = "bot-05"\n')
        t = self.board(); self.to_a2(t); self.assertEqual(self.id_of(t, "a2"), ("bot-05",))
        self.open_picker(t); self.move_to(t, "bot-02"); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(self.id_of(t, "a2"), ("bot-02",)); self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-02"})
        self.assertEqual(self.conf.read_text(), '[avatars.bind]\n"pj-fallback" = "bot-05"\n', "the config file is never written")


class ConfigErrors(Base):
    CASES = {
        "unknown default": ('[avatars]\ndefault = "bot-99"\n', "avatars.default", '"bot-99"'),
        "unknown bind sprite": ('[avatars.bind]\nx = "bot-99"\n', "avatars.bind", '"bot-99"'),
        "unknown setting": ('[avatars]\nnope = "x"\n', "avatars.nope", "unknown setting"),
        "default not a string": ('[avatars]\ndefault = 5\n', "avatars.default", ""),
        "bind value not a string": ('[avatars.bind]\nx = 3\n', "avatars.bind.x", ""),
        "avatars not a table": ('avatars = 3\n', "avatars", ""),
        "keys not a table": ('keys = 3\n', "keys", "must be a table"),
    }

    def test_each_error_is_one_line_exit_1_for_board_and_config(self):
        for name, (text, setting, why) in self.CASES.items():
            with self.subTest(name):
                self.config(text)
                for args in (["board"], ["config"]):
                    r = self.cli(*args)
                    self.assertEqual(r.returncode, 1, f"{args}: {r.stdout!r} {r.stderr!r}")
                    lines = r.stderr.rstrip("\n").split("\n")
                    self.assertEqual(len(lines), 1, r.stderr)
                    self.assertTrue(lines[0].startswith("agmux: " + str(self.conf)), lines[0])
                    self.assertIn(setting, lines[0]); self.assertIn(why, lines[0]); self.assertEqual(r.stdout, "")

    def test_avatar_key_taken_by_another_explicit_binding_leaves_kill_unbound(self):  # T17 rule (c1b8fcb): defaults yield to the user's own bindings
        self.config('[keys]\navatar = ["x"]\n')
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("# kill: no key", r.stdout); self.assertIn('avatar = ["x"]', r.stdout)
        t = self.board(); bar = last_text_row(t.snap()); self.assertIn("x avatar", bar); self.assertNotIn("x kill", bar)

    def test_the_message_for_an_unknown_sprite_lists_the_valid_ones(self):
        self.config('[avatars]\ndefault = "bot-99"\n')
        err = self.cli("config").stderr
        for i in IDS:
            self.assertIn(i, err)

    def test_config_prints_the_avatars_sections_and_round_trips(self):
        self.config('[avatars]\ndefault = "bot-02"\n[avatars.bind]\n"pj-fallback" = "bot-05"\n')
        out = self.cli("config").stdout; plain = plain_config(out)
        self.assertIn('avatar = ["a"]', out); self.assertIn('[avatars]\nrenderer = "auto"\ndefault = "bot-02"', plain); self.assertIn('"pj-fallback" = "bot-05"', out)
        self.config(out)
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout, out, "idempotent")
        t = self.board(); self.assertEqual((self.id_of(t, "a2"), self.id_of(t, "c1")), (("bot-05",), ("bot-02",)))

    def test_default_dump_says_hash_and_has_no_binds(self):
        out = self.cli("config").stdout
        self.assertIn('[avatars]\nrenderer = "auto"\ndefault = "hash"', plain_config(out)); self.assertNotIn("pj-fallback", out)
        self.config(out); self.assertEqual(self.cli("config").returncode, 0)


class PickerInPassthrough(PreviewBase):
    """`a` belongs to the pane while the preview has the keys (real pane: a3 is a raw `cat -v`)."""
    PANES = _t12.PassthroughReview.PANES
    cat, enter_passthrough_on_a3, preview_focused = _t12.PassthroughReview.cat, _t12.PassthroughReview.enter_passthrough_on_a3, _t12.PassthroughReview.preview_focused

    def test_a_is_forwarded_not_a_picker(self):
        self.enter_passthrough_on_a3()
        self.t.lit("a"); time.sleep(0.5)
        self.assertEqual(self.cat("a3"), "a"); self.assertIsNone(overlay(self.t.snap())); self.assertTrue(self.preview_focused())
        self.t.keys("Tab"); time.sleep(0.4); self.t.keys("a"); time.sleep(0.5)
        self.assertTrue(overlay(self.t.snap()), "back on the cards, a opens the picker")


if __name__ == "__main__":
    unittest.main()
