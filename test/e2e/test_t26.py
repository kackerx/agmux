"""T26 `colors.border`: one colour for the border of every card that is not focused (empty = follow the status, as before). The focus border stays colors.focus; the status line and the avatar keep the status colours."""
import subprocess, unittest
from lib import *
from test_t3 import ready, by_name, NAMES, border_colours, STATUS_COLOUR, C_FOCUS

BLUE = ("rgb", 18, 52, 86)  # #123456
# the fixture: a1 focused (working); a2 idle, a3 alert(+working), b1 working, b2 idle, c1 idle, c2 done, d1 alert (waiting), e1 idle (read)
WANT = {"a2": "idle", "a3": "alert", "b1": "working", "b2": "idle", "c1": "idle", "c2": "done", "d1": "alert", "e1": "idle"}  # (e1 is `read`: idle since T67)


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def config(self, text):
        (self.cfg / "agmux" / "config.toml").write_text(text)

    def board(self, w=200, h=50):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs, t.text()); return t

    def cli(self, *args):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def status_text_colour(self, card, word):
        row = card.rows[3]; i = row.find(word); self.assertGreaterEqual(i, 0, row.text); return row.cells[i][1]


class Border(Base):
    def test_every_unfocused_card_gets_the_border_colour_and_the_focused_one_keeps_pink(self):
        self.config('[colors]\nborder = "#123456"\n'); t = self.board(); snap = t.snap()
        for sid in WANT:
            self.assertEqual(border_colours(by_name(find_cards(snap), NAMES[sid])), {BLUE}, f"{sid}: working / done / alert / idle all the same")
        self.assertEqual(border_colours(by_name(find_cards(snap), NAMES["a1"])), {C_FOCUS}, "the focused card is still #ff8ebe")
        self.assertIn("18;52;86", t.raw(), "the escape sequence the spec names")

    def test_the_status_line_and_the_avatar_still_follow_the_status(self):
        t0 = self.board(); base = {sid: self.status_text_colour(by_name(find_cards(t0.snap()), NAMES[sid]), w) for sid, w in (("b1", "working"), ("c2", "done"), ("a2", "idle"), ("d1", "waiting"))}
        t0.close(); self.config('[colors]\nborder = "#123456"\n'); t = self.board(); cs = find_cards(t.snap())
        for sid, w in (("b1", "working"), ("c2", "done"), ("a2", "idle"), ("d1", "waiting")):
            self.assertEqual(self.status_text_colour(by_name(cs, NAMES[sid]), w), base[sid], f"{sid}: the status word keeps its colour")
        self.assertEqual(self.status_text_colour(by_name(cs, NAMES["b1"]), "working"), STATUS_COLOUR["working"])

    def test_without_the_setting_nothing_changes(self):
        t = self.board(); cs = find_cards(t.snap())
        for sid, st in WANT.items():
            self.assertEqual(border_colours(by_name(cs, NAMES[sid])), {STATUS_COLOUR[st]}, f"{sid} ({st}): the border follows the status")
        self.assertEqual(border_colours(by_name(cs, NAMES["a1"])), {C_FOCUS})

    def test_an_empty_string_means_follow_the_status(self):
        self.config('[colors]\nborder = ""\n'); t = self.board(); cs = find_cards(t.snap())
        for sid, st in WANT.items():
            self.assertEqual(border_colours(by_name(cs, NAMES[sid])), {STATUS_COLOUR[st]})

    def test_the_focus_colour_setting_still_wins_on_the_focused_card(self):
        self.config('[colors]\nborder = "#123456"\nfocus = "#00ff00"\n'); t = self.board(); cs = find_cards(t.snap())
        self.assertEqual(border_colours(by_name(cs, NAMES["a1"])), {("rgb", 0, 255, 0)}); self.assertEqual(border_colours(by_name(cs, NAMES["b1"])), {BLUE})

    def test_focus_moves_and_the_old_card_goes_back_to_the_border_colour(self):
        self.config('[colors]\nborder = "#123456"\n'); t = self.board()
        t.keys("j"); time.sleep(0.4); cs = find_cards(t.snap())
        self.assertEqual(border_colours(by_name(cs, NAMES["a2"])), {C_FOCUS}); self.assertEqual(border_colours(by_name(cs, NAMES["a1"])), {BLUE})

    def test_an_ansi_index_is_accepted(self):
        self.config('[colors]\nborder = "8"\n'); self.board()

    def test_the_picker_flash_and_preview_borders_are_not_the_card_border(self):
        self.config('[colors]\nborder = "#123456"\n'); t = self.board(); t.keys("s"); time.sleep(0.4)
        cs = find_cards(t.snap()); self.assertTrue(cs); t.esc(); time.sleep(0.3)  # (flash dims everything; it must not crash with the setting)
        self.assertTrue(t.alive())


class Config(Base):
    def test_dump_round_trips_and_lists_border(self):
        self.assertIn('border = ""', self.cli("config").stdout)
        self.config('[colors]\nborder = "#585b70"\n'); out = self.cli("config").stdout; self.assertIn('border = "#585b70"', out)
        self.config(out); r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout, out)

    def test_only_border_may_be_empty(self):
        self.config('[colors]\nworking = ""\n'); r = self.cli("config"); self.assertEqual(r.returncode, 1); self.assertIn("colors.working", r.stderr)

    def test_a_bad_border_value_is_one_line_and_says_empty_is_allowed(self):
        self.config('[colors]\nborder = "red"\n')
        for args in (["config"], ["board"]):
            r = self.cli(*args); self.assertEqual(r.returncode, 1); lines = r.stderr.rstrip("\n").split("\n"); self.assertEqual(len(lines), 1, r.stderr)
            self.assertIn("colors.border", lines[0]); self.assertIn('or "" to follow the status', lines[0], "T26 review: the message mentions the empty value")


if __name__ == "__main__":
    unittest.main()
