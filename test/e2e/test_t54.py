"""T54 the avatar picker is closed by the same key that opened it: with the picker open, the `avatar` key (default a) does what esc / q do -- the picker goes, nothing is written, the avatar does not change, and the
next opening puts the cursor on the avatar worn again (not where it was left). Rebound (`avatar = ["A"]`): A opens and closes, a does nothing in the picker. Other keys are as before. `agmux config` says "open or close".
Spec: specs/m3-config/tasks.md T54. Fixture: test_t18 (build_fixture; a2 = pj-fallback is the second card of the first column)."""
import json, unittest
from lib import *
from test_t18 import Base, overlay, find_grid
from test_t3 import last_text_row


class Toggle(Base):
    def gone(self, t, secs=3): return wait(lambda: overlay(t.snap()) is None, secs)

    def test_a_a_closes_the_picker_and_writes_nothing(self):
        t = self.board(); self.to_a2(t); cur = self.id_of(t, "a2"); self.open_picker(t); t.keys("a")
        self.assertTrue(self.gone(t), "the second a closed it\n" + t.text()); self.assertTrue(t.alive(), "the board is still there"); self.assertFalse(self.json.exists(), "avatars.json: not written")
        self.assertEqual(self.id_of(t, "a2"), cur, "the avatar is the one it was"); self.assertEqual(self.env.actions(), []); self.assertEqual(self.env.tmux_calls(), [])

    def test_with_the_cursor_moved_a_still_closes_without_choosing(self):
        self.env.avatars_json({"pj-fallback": "bot-02"}); before = self.json.read_bytes()
        t = self.board(); self.to_a2(t); self.open_picker(t); self.assertEqual(self.cursor(t), "bot-02")
        for k in ("l", "l", "j"): t.keys(k); time.sleep(0.2)
        self.assertNotEqual(self.cursor(t), "bot-02", "the cursor did move"); t.keys("a")
        self.assertTrue(self.gone(t), t.text()); time.sleep(0.5); self.assertEqual(self.json.read_bytes(), before, "the file is byte for byte as it was"); self.assertEqual(self.id_of(t, "a2"), ("bot-02",))

    def test_it_opens_on_the_avatar_worn_again_not_where_the_cursor_was_left(self):
        self.env.avatars_json({"pj-fallback": "bot-03"})
        t = self.board(); self.to_a2(t); self.open_picker(t)
        for k in ("l", "l", "l"): t.keys(k); time.sleep(0.2)
        self.assertNotEqual(self.cursor(t), "bot-03"); t.keys("a"); self.assertTrue(self.gone(t)); t.keys("a")
        self.assertTrue(wait(lambda: overlay(t.snap()), 3), t.text()); self.assertEqual(self.cursor(t), "bot-03", "back on the one worn"); t.keys("Enter"); time.sleep(0.6)
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-03"}, "enter right after reopening keeps it"); self.assertEqual(self.id_of(t, "a2"), ("bot-03",))

    def test_a_a_a_is_open_closed_open(self):
        t = self.board(); self.to_a2(t); self.open_picker(t); t.keys("a"); self.assertTrue(self.gone(t)); t.keys("a")
        self.assertTrue(wait(lambda: overlay(t.snap()), 3), "the third a opens it again\n" + t.text()); self.assertFalse(self.json.exists())
        t.keys("a"); self.assertTrue(self.gone(t)); self.assertTrue(t.alive())

    def test_esc_and_q_still_close_it_and_other_keys_still_do_nothing(self):
        t = self.board(); self.to_a2(t)
        for key in ("esc", "q"):
            self.open_picker(t); t.esc() if key == "esc" else t.keys("q"); self.assertTrue(self.gone(t), key); self.assertTrue(t.alive(), key)
        self.open_picker(t); before = overlay(t.snap())["rows"]
        for k in ("x", "f", "p", "i", "H", "r", "d", "y", "t", "m"): t.keys(k); time.sleep(0.15)
        time.sleep(0.4); self.assertEqual(overlay(t.snap())["rows"], before, "still open, same cursor"); self.assertEqual(self.env.actions(), []); self.assertFalse(self.json.exists())

    def test_the_footer_still_says_a_avatar(self):
        t = self.board(); self.assertIn("a avatar", last_text_row(t.snap()))

    def test_on_a_short_screen_where_the_list_scrolls_a_closes_it_too(self):
        t = Term(self.env, w=80, h=24, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: find_cards(t.snap()), 8), t.text()); time.sleep(0.5); t.keys("a"); self.assertTrue(wait(lambda: find_grid(t.snap()), 3), t.text()); self.assertIn("more", "".join(r.text for r in t.snap()), "this screen scrolls the list")
        for k in ("j", "j", "j"): t.keys(k); time.sleep(0.15)
        t.keys("a"); self.assertTrue(wait(lambda: find_grid(t.snap()) is None, 3), t.text()); self.assertFalse(self.json.exists()); self.assertTrue(t.alive())


class Rebound(Base):
    def test_A_opens_and_closes_and_a_does_nothing_in_the_picker(self):
        self.config('[keys]\navatar = ["A"]\n'); t = self.board(); self.to_a2(t)
        t.keys("A"); self.assertTrue(wait(lambda: overlay(t.snap()), 3), t.text()); before = overlay(t.snap())["rows"]
        t.keys("a"); time.sleep(0.5); self.assertIsNotNone(overlay(t.snap()), "a is not the key now: the picker stays"); self.assertEqual(overlay(t.snap())["rows"], before, "same cursor")
        t.keys("A"); self.assertTrue(wait(lambda: overlay(t.snap()) is None, 3), "A closes it\n" + t.text()); self.assertFalse(self.json.exists()); self.assertTrue(t.alive())
        t.keys("a"); time.sleep(0.4); self.assertIsNone(overlay(t.snap()), "and a opens nothing on the cards")

    def test_a_key_that_quits_elsewhere_toggles_without_quitting(self):
        self.config('[keys]\navatar = ["q"]\n'); t = self.board(); self.to_a2(t)
        t.keys("q"); self.assertTrue(wait(lambda: overlay(t.snap()), 3), t.text()); t.keys("q"); self.assertTrue(wait(lambda: overlay(t.snap()) is None, 3), t.text())
        self.assertTrue(t.alive(), "q was the avatar key: it did not quit the board"); self.assertFalse(self.json.exists())

    def test_config_says_open_or_close(self):
        out = self.cli("config").stdout; self.assertIn('avatar = ["a"]                # default: a — open or close the avatar picker under the focused card', out)
        self.assertNotIn("open the avatar picker under", out)


if __name__ == "__main__":
    unittest.main()
