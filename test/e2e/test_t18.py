"""T18 avatars by session name + [avatars] config + the `a` picker. Spec: specs/m3-config/design.md「头像 [avatars]」「面板里选头像」, tasks.md T18.

Binding key = the name shown on the card (summary, else project); state file ($XDG_STATE_HOME/agmux/avatars.json) > config [avatars.bind] > [avatars] default.
Idle / done cards show the sprite exactly as `agmux avatars` prints it (no eye animation), so the idle cards are used to read which sprite a card has.
"""
import json, subprocess, unittest
import test_t12 as _t12
from lib import *
from test_t2 import chars, run_avatars, parse_blocks
from test_t3 import ready, by_name, NAMES, last_text_row, border_colours, C_FOCUS
from test_t11 import PreviewBase, CAT_CMD

IDLE = ("a2", "c1", "c2", "e1")  # a2 pj-fallback (no summary: named after its project), c1 charlie-30, c2 charlie-59, e1 echo-long
UNIQUE = ["bot-02", "bot-03", "bot-04", "bot-05"]  # bot-01 and bot-06 print the same glyphs (they differ in colour only), so a card's sprite can only be told apart by glyphs for these
IDS = ["bot-01", "bot-02", "bot-03", "bot-04", "bot-05", "bot-06"]
SPRITE_NAMES = ["Clawd", "Boxy", "Sprout", "Boo", "Mochi", "Cyclops"]


def overlay(snap):
    """The picker: dict(title, rows=[dict(id, name, sprite, marker, current)]) or None when it is not on screen."""
    title = next((r for r in snap if "avatar for " in r.text), None)
    if not title:
        return None
    name = re.search(r"avatar for ([^│]*?)\s*│", title.text).group(1)
    rows = []
    for i, row in enumerate(snap):
        m = re.search(r"(bot-\d\d)  (\S+)", row.text)
        if not m:
            continue
        c = row.find(m.group(1))
        top = "".join(x[0] for x in row.cells[c - 8:c - 2]); bottom = "".join(x[0] for x in snap[i + 1].cells[c - 8:c - 2])
        rows.append(dict(id=m.group(1), name=m.group(2), sprite=(top, bottom), marker="▸" in row.text, current=row.cells[c][1] == C_FOCUS))
    return dict(title="avatar for " + name, rows=rows, cols=(title.find("avatar for"), title.find("avatar for") + 26))


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        self.conf = self.cfg / "agmux" / "config.toml"
        self.json = self.env.state / "agmux" / "avatars.json"
        self.blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in parse_blocks(run_avatars(self.env).stdout)}

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
        """the sprite ids whose glyphs the card shows (a 1-tuple, or ("bot-01", "bot-06") for those two)"""
        s = self.sprite_of(t, sid); m = tuple(i for i, b in self.blocks.items() if b == s)
        self.assertTrue(m, f"{sid}: the card sprite {s} is not one of the 6")
        return m

    def open_picker(self, t):
        t.keys("a")
        ov = wait(lambda: overlay(t.snap()), 3); self.assertTrue(ov, t.text())
        return ov

    def cursor(self, t):
        ov = overlay(t.snap()); at = [i for i, r in enumerate(ov["rows"]) if r["marker"]]
        self.assertEqual(len(at), 1, f"exactly one cursor row: {ov}"); return at[0]

    def move_to(self, t, want):
        """Move the cursor to sprite id `want` with j/k, whatever row it starts on."""
        for _ in range(8):
            i = self.cursor(t); w = IDS.index(want)
            if i == w:
                return
            t.keys("j" if w > i else "k"); time.sleep(0.2)
        self.fail(f"cursor did not reach {want}")

    def to_a2(self, t):
        t.keys("j"); time.sleep(0.3)  # a2 = pj-fallback, the 2nd card of the first column


class Picker(Base):
    def test_footer_lists_the_key(self):
        t = self.board(); self.assertIn("a avatar", last_text_row(t.snap()))

    def test_a_opens_a_centred_list_of_the_six_sprites(self):
        t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2")
        ov = self.open_picker(t)
        self.assertEqual(ov["title"], "avatar for pj-fallback", "the title names the focused card")
        self.assertEqual([r["id"] for r in ov["rows"]], IDS); self.assertEqual([r["name"] for r in ov["rows"]], SPRITE_NAMES)
        self.assertEqual([r["sprite"] for r in ov["rows"]], [self.blocks[i] for i in IDS], "each row shows the sprite `agmux avatars` prints for that id")
        self.assertEqual(len([r for r in ov["rows"] if r["current"]]), 1); self.assertIn([r["id"] for r in ov["rows"] if r["current"]][0], cur, "the current avatar (the card's) is drawn in the focus colour, only that one")
        mid = (ov["cols"][0] + ov["cols"][1]) // 2  # centred: the picker is not stuck in a corner
        self.assertLess(abs(mid - 100), 16, f"horizontally centred on 200 columns: {ov['cols']}")
        self.assertIn("enter bind", last_text_row(t.snap())); self.assertIn("q cancel", last_text_row(t.snap()))

    def test_j_and_k_clamp_at_the_ends(self):
        t = self.board(); self.to_a2(t); self.open_picker(t)
        for _ in range(8):
            t.keys("k"); time.sleep(0.1)
        self.assertEqual(self.cursor(t), 0, "k at the first row stays there")
        t.keys("j"); time.sleep(0.2); self.assertEqual(self.cursor(t), 1)
        for _ in range(9):
            t.keys("j"); time.sleep(0.1)
        self.assertEqual(self.cursor(t), 5, "j at the last row stays there")
        t.keys("Down"); time.sleep(0.2); self.assertEqual(self.cursor(t), 5)
        t.keys("Up"); time.sleep(0.2); self.assertEqual(self.cursor(t), 4, "arrows move too (the default up / down keys)")

    def test_cursor_starts_on_the_current_avatar(self):
        t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2")
        ov = self.open_picker(t)
        self.assertIn(ov["rows"][self.cursor(t)]["id"], cur, "enter right away would change nothing")
        self.assertEqual([r["id"] for r in ov["rows"] if r["marker"]], [r["id"] for r in ov["rows"] if r["current"]], "cursor row = current row")

    def test_j_j_enter_binds_the_avatar_two_below_the_current_one(self):
        self.env.avatars_json({"pj-fallback": "bot-02"})
        t = self.board(); self.to_a2(t)
        self.open_picker(t); self.assertEqual(self.cursor(t), 1, "the cursor opens on bot-02, the current avatar")
        t.keys("j"); time.sleep(0.2); t.keys("j"); time.sleep(0.2); t.keys("Enter"); time.sleep(0.8)
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
        ov = self.open_picker(t); self.assertEqual([r["id"] for r in ov["rows"] if r["current"]], [want])
        t.keys("q"); t.close()
        t2 = self.board(); self.to_a2(t2)
        self.assertEqual(self.id_of(t2, "a2"), (want,), "still bound after restarting the board")

    def test_binding_one_card_leaves_the_others_alone(self):
        t = self.board(); before = {s: self.id_of(t, s) for s in IDLE if s != "a2"}
        self.to_a2(t); want = next(i for i in UNIQUE if i not in self.id_of(t, "a2"))
        self.open_picker(t); self.move_to(t, want); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual({s: self.id_of(t, s) for s in IDLE if s != "a2"}, before)

    def test_a_second_binding_keeps_the_first(self):
        t = self.board(); self.to_a2(t)
        self.open_picker(t); self.move_to(t, "bot-03"); t.keys("Enter"); time.sleep(0.6)
        t.keys("j"); time.sleep(0.2); t.keys("j"); time.sleep(0.2)  # a3
        t.keys("j"); time.sleep(0.2)  # b-column? whichever card comes next; bind it too
        self.open_picker(t); self.move_to(t, "bot-06"); t.keys("Enter"); time.sleep(0.6)
        d = json.loads(self.json.read_text()); self.assertEqual(len(d), 2, d); self.assertEqual(d["pj-fallback"], "bot-03"); self.assertIn("bot-06", d.values())

    def test_esc_and_q_cancel_without_writing(self):
        for key in ("esc", "q"):
            with self.subTest(key=key):
                self.setUp()
                t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2")
                self.open_picker(t); t.keys("j"); time.sleep(0.2); t.keys("j"); time.sleep(0.2)
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
        self.env.avatars_json({"echo-long": "bot-06"}); before = self.json.read_bytes()
        t = self.board(); self.to_a2(t); self.open_picker(t); t.keys("j"); time.sleep(0.2); t.esc(); time.sleep(0.5)
        self.assertEqual(self.json.read_bytes(), before)

    def test_other_keys_do_nothing_while_the_picker_is_open(self):
        t = self.board(); self.to_a2(t); self.open_picker(t)
        mask = lambda: re.sub(r"[▀▄█]", "", t.text())  # the working cards animate their eyes
        before = mask(); picker = overlay(t.snap())
        for k in ("x", "f", "p", "i", "H", "r", "d", "y", "h", "l", "Tab"):
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
        self.open_picker(t); t.keys("j"); time.sleep(0.2); t.keys("Enter"); time.sleep(0.8)
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
        t = self.board(); self.to_a2(t); self.open_picker(t); t.keys("j"); time.sleep(0.2); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(self.json.read_text(), "{not json", "the file the user may want to repair is left as it is")
        self.assertIn("not valid JSON", t.text()); self.assertTrue(t.alive())

    def test_the_key_is_configurable(self):
        self.config('[keys]\navatar = ["A"]\n')
        t = self.board(); self.assertIn("A avatar", last_text_row(t.snap())); self.assertNotIn("a avatar", last_text_row(t.snap()))
        t.keys("a"); time.sleep(0.4); self.assertIsNone(overlay(t.snap()), "a is no longer bound")
        t.keys("A"); self.assertTrue(wait(lambda: overlay(t.snap()), 3), t.text())


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

    def test_sprites_that_print_the_same_glyphs_still_differ_in_colour(self):  # bot-01 Clawd and bot-06 Cyclops
        from test_t2 import region
        sig = lambda card: tuple((c[0], c[1]) for i in (1, 2) for c in region(card, i))  # glyph + foreground only: the cell background is the card's own
        self.config('[avatars.bind]\n"pj-fallback" = "bot-01"\n"charlie-30" = "bot-06"\n')
        t = self.board(); c = find_cards(t.snap()); one, six = sig(by_name(c, "pj-fallback")), sig(by_name(c, "charlie-30")); t.close()
        self.assertNotEqual(one, six, "two different sprites")
        self.config('[avatars.bind]\n"pj-fallback" = "bot-06"\n"charlie-30" = "bot-01"\n')
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
        "avatar key taken": ('[keys]\navatar = ["x"]\n', "keys.kill", 'no key left after "x" was taken by avatar'),  # T17 review: the explicit binding wins
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

    def test_the_message_for_an_unknown_sprite_lists_the_valid_ones(self):
        self.config('[avatars]\ndefault = "bot-99"\n')
        err = self.cli("config").stderr
        for i in IDS:
            self.assertIn(i, err)

    def test_config_prints_the_avatars_sections_and_round_trips(self):
        self.config('[avatars]\ndefault = "bot-02"\n[avatars.bind]\n"pj-fallback" = "bot-05"\n')
        out = self.cli("config").stdout
        self.assertIn('avatar = ["a"]', out); self.assertIn("[avatars]\ndefault = \"bot-02\"", out); self.assertIn('"pj-fallback" = "bot-05"', out)
        self.config(out)
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout, out, "idempotent")
        t = self.board(); self.assertEqual((self.id_of(t, "a2"), self.id_of(t, "c1")), (("bot-05",), ("bot-02",)))

    def test_default_dump_says_hash_and_has_no_binds(self):
        out = self.cli("config").stdout
        self.assertIn('[avatars]\ndefault = "hash"', out); self.assertNotIn("pj-fallback", out)
        self.config(out); self.assertEqual(self.cli("config").returncode, 0)


class PickerInPassthrough(PreviewBase):
    """`a` belongs to the pane while the preview has the keys (real pane: a3 is a raw `cat -v`)."""
    PANES = _t12.PassthroughReview.PANES
    cat, enter_passthrough_on_a3, preview_focused = _t12.PassthroughReview.cat, _t12.PassthroughReview.enter_passthrough_on_a3, _t12.PassthroughReview.preview_focused

    def test_a_is_forwarded_not_a_picker(self):
        self.enter_passthrough_on_a3()
        self.t.lit("a"); time.sleep(0.5)
        self.assertEqual(self.cat("a3"), "a"); self.assertIsNone(overlay(self.t.snap())); self.assertTrue(self.preview_focused())
        self.t.keys("C-]"); time.sleep(0.4); self.t.keys("a"); time.sleep(0.5)
        self.assertTrue(overlay(self.t.snap()), "back on the cards, a opens the picker")


if __name__ == "__main__":
    unittest.main()
