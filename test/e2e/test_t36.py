"""T36 card line 3: the status word is drawn in the working colour (a pending tool's name stays muted; the `agents` detail and the `⚇ n` counts that were drawn so are gone since T67); in image mode an avatar nobody bound is
always a picture (the hash picks among the image sets only), in pixel mode always a sprite. Spec: specs/m3-config/tasks.md「第七批」T36."""
import subprocess, unittest
from lib import *
import test_t28 as t28
from test_t3 import ready, by_name, C_FOCUS

WORKING = ("rgb", 255, 184, 108)  # #ffb86c
MUTED = ("rgb", 98, 114, 164)  # #6272a4


class Base(t28.Base):
    def setUp(self):
        super().setUp(); self.config("", default=None)

    def colour_of(self, card, text):
        row = card.rows[3]; i = row.find(text); self.assertGreaterEqual(i, 0, f"{text!r} in {row.text!r}"); cols = {row.cells[i + k][1] for k in range(len(text)) if row.cells[i + k][0] != " "}
        self.assertEqual(len(cols), 1, f"{text!r} is one colour: {cols}"); c = cols.pop()
        return WORKING if c == WORKING else MUTED if all(abs(a - b) <= 2 for a, b in zip(c[1:], MUTED[1:])) else c  # (tmux's colour round trip is off by one here and there)


class Colours(Base):
    def cards(self, config=None):
        if config: self.config(config, default=None)
        t = self.board(tmux=False); return t, find_cards(t.snap())

    def test_a_pending_tool_name_stays_muted(self):  # (the `⚇ n` counts and the `agents` detail went with the subagent list, T67)
        t, cs = self.cards(); c = by_name(cs, "alpha-main"); self.assertIn("working · Bash", c.line(3)); self.assertNotIn("⚇", c.line(3))
        self.assertEqual(self.colour_of(c, "Bash"), MUTED, "a tool name is secondary text")

    def test_the_status_word_keeps_its_own_colour(self):
        t, cs = self.cards(); c = by_name(cs, "bravo-29"); self.assertEqual(self.colour_of(c, "working"), WORKING)

    def test_the_status_word_follows_colors_working_and_the_tool_name_does_not_move(self):
        t, cs = self.cards('[colors]\nworking = "#010203"\n'); new = ("rgb", 1, 2, 3)
        self.assertEqual(self.colour_of(by_name(cs, "bravo-29"), "working"), new); self.assertEqual(self.colour_of(by_name(cs, "alpha-main"), "Bash"), MUTED, "the tool name does not move with colors.working")

    def test_cards_with_neither_are_unchanged(self):
        t, cs = self.cards(); c = by_name(cs, "charlie-30"); self.assertNotIn("⚇", c.line(3)); self.assertNotIn("agents", c.line(3))


class HashPool(unittest.TestCase):
    def fixture(self, env):
        names = build_many_fixture(env, n=100, per_group=25); return names

    def board(self, tmux, **kw):
        env = Env(); self.addCleanup(env.cleanup); self.fixture(env); cfg = env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True)
        if tmux: env.set_term("xterm-kitty", "on"); env.use_pty_tty()
        t = Term(env, w=200, h=140, extra_env=f"XDG_CONFIG_HOME={cfg} " + (KITTY_ENV if tmux else "")); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) == 100, 15), t.text()); time.sleep(0.5); return t

    def test_image_mode_every_unbound_session_is_a_picture(self):
        t = self.board(True); raw = t.raw(); cs = find_cards(parse_screen(raw)); self.assertEqual(len(cs), 100)
        bad = [c.line(1) for c in cs if len(t28.card_avatar(raw, c)) != 12]; self.assertEqual(bad, [], "100 sessions, none bound: no pixel sprite among them")

    def test_pixel_mode_every_unbound_session_is_a_sprite(self):
        t = self.board(False); raw = t.raw(); self.assertNotIn(PLACEHOLDER, raw); cs = find_cards(parse_screen(raw)); self.assertEqual(len(cs), 100)
        self.assertTrue(all(any(x[0] != " " for x in c.rows[1].cells[1:7]) for c in cs), "every card has a pixel avatar")

    def test_a_sprite_binding_is_still_honoured_in_image_mode(self):
        env = Env(); self.addCleanup(env.cleanup); self.fixture(env); cfg = env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True); (cfg / "agmux" / "config.toml").write_text('[avatars.bind]\n"m00" = "bot-02"\n')
        env.set_term("xterm-kitty", "on"); env.use_pty_tty(); t = Term(env, w=200, h=140, extra_env=f"XDG_CONFIG_HOME={cfg} " + KITTY_ENV); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) == 100, 15)); raw = t.raw(); cs = find_cards(parse_screen(raw)); c0 = [c for c in cs if " m00 " in c.line(1)][0]
        self.assertEqual(t28.card_avatar(raw, c0), [], "bound to a sprite: pixels"); self.assertEqual(len([c for c in cs if t28.card_avatar(raw, c)]), 99)


if __name__ == "__main__":
    unittest.main()
