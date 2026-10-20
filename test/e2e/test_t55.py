"""T55 mark_read (default key `m`): on a focused done card it marks the done as seen the way the preview / enter do (seen.json {session id: statusChangedAt}), the card is drawn idle at once and the message line says
`read <name>` (muted) in the same frame as the card changes; on a card that is not done (working / waiting / idle / seen already), on a folded group title: nothing happens, no message. In the search box, the pass-through,
flash, the picker and the kill prompt `m` is not the action. A restart keeps it idle; a new statusChangedAt is a new done. `agmux config` lists `mark_read = ["m"]`; rebinding / key conflicts as for the other actions, `[]` is an error.
Spec: specs/m3-config/tasks.md T55. Fixture: test_t45 (build_group_fixture + statusChangedAt; shop-feat g03 and shop-hf2 g02 are done, shop-main working, shop-hf waiting; 100 columns = 2 columns, focus starts on shop-main)."""
import json, re, subprocess, unittest
import test_t45 as T45
from lib import *
from test_t3 import C_FOCUS, border_colours, last_text_row
from test_t45 import KEY0, T0, T1
from test_t18 import find_grid

MUTED = (98, 114, 164)


def near(c, ref, d=4): return c is not None and all(abs(x - y) <= d for x, y in zip(c, ref))


class Base(T45.Base):
    def card(self, t, name):
        m = [c for c in find_cards(t.snap()) if re.match(r"\s*(?:▲ )?(\S+)", c.line(1)[8:]).group(1) == name]; self.assertEqual(len(m), 1, f"{name}: {len(m)} cards"); return m[0]

    def look(self, t, name="shop-feat"):
        """(status word of the card, message line text, message cells) from ONE capture, or None for a capture torn by a repaint"""
        s = t.snap(); cs = [c for c in find_cards(s) if c.ok and re.match(r"\s*(?:▲ )?(\S+)", c.line(1)[8:]).group(1) == name]
        if len(cs) != 1: return None
        m = re.search(r"(⏵ working|⏸ idle|● done|⏳ waiting)", cs[0].line(3)); i = max(i for i, r in enumerate(s) if r.text.strip())
        return (m.group(1) if m else None, s[i - 1].text.strip(), s[i - 1].cells)

    def msg(self, t): return (self.look(t) or (None, "?", None))[1]

    def cli(self, *args, text=None):
        cfg = self.env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True, exist_ok=True)
        if text is not None: (cfg / "agmux" / "config.toml").write_text(text)
        return subprocess.run([BIN, *args], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(cfg), "LANG": "en_US.UTF-8"},
                              capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def board_cfg(self, cfg, w=100, h=60):
        d = self.env.tmp / "cfg" / "agmux"; d.mkdir(parents=True, exist_ok=True); (d / "config.toml").write_text(cfg)
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.env.tmp / 'cfg'}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 8, 10), t.text()); time.sleep(0.3); return t

    def nothing(self, t, secs=1.2):
        time.sleep(secs); self.assertEqual(self.entries(), {}); self.assertFalse(self.seen.exists(), "seen.json not created")
        self.assertFalse(self.msg(t).startswith("read "), self.msg(t)); self.assertTrue(t.alive())


class Mark(Base):
    def test_m_on_a_done_card_makes_it_idle_in_the_same_frame_as_the_message(self):
        t = self.board(); t.keys("l"); time.sleep(0.5); self.assertEqual(self.look(t)[0], "● done", "shop-feat is done and focused"); self.assertEqual(self.entries(), {})
        t0 = time.time(); t.keys("m"); seen = []
        while time.time() - t0 < 1.5:
            r = self.look(t)
            if r: seen.append((round(time.time() - t0, 2), r[0], r[1]))
            if r and r[0] == "⏸ idle" and r[1] == "read shop-feat": break
        self.assertTrue(seen and seen[-1][1:] == ("⏸ idle", "read shop-feat"), f"idle + 'read shop-feat' together: {seen}")
        self.assertLess(seen[-1][0], 0.45, f"at once, not at the next 0.5 s poll: {seen}")
        for at, word, msg in seen:
            self.assertEqual(word == "⏸ idle", msg == "read shop-feat", f"the card and the message change in one frame: {seen}")
        self.assertEqual(self.entries(), {"g03": KEY0}, "the same record as the preview and enter write")
        self.assertTrue(t.alive(), "the board stays and the focus is still on the card"); self.assertIn(C_FOCUS, border_colours(self.card(t, "shop-feat")))

    def test_the_message_is_muted_and_the_other_done_stays_done(self):
        t = self.board(); t.keys("l"); t.keys("m"); self.assertTrue(wait(lambda: self.msg(t) == "read shop-feat", 3), self.msg(t))
        cells = self.look(t)[2]; txt = [c for c in cells if c[0].strip()]; self.assertTrue(txt and all(near(rgb(c[1]), MUTED) for c in txt), [(c[0], c[1]) for c in txt])
        self.assertEqual(self.word(t, "shop-hf2"), "● done", "only the focused one"); self.assertEqual(self.entries(), {"g03": KEY0})

    def test_it_goes_through_the_card_name_that_is_shown(self):
        t = self.board(); t.keys("j"); t.keys("j"); self.assertEqual(self.look(t, "shop-hf2")[0], "● done"); t.keys("m")
        self.assertTrue(wait(lambda: self.msg(t) == "read shop-hf2" and self.word(t, "shop-hf2") == "⏸ idle", 3), self.msg(t)); self.assertEqual(self.entries(), {"g02": KEY0}); self.assertEqual(self.word(t, "shop-feat"), "● done")

    def test_a_second_m_on_the_card_that_is_idle_now_does_nothing(self):
        t = self.board(); t.keys("l"); t.keys("m"); self.assertTrue(wait(lambda: self.word(t, "shop-feat") == "⏸ idle", 3)); before = self.seen.read_bytes()
        for _ in range(4): t.keys("g"); time.sleep(0.3)  # a regrouping clears the message line; round once and the board is as it was
        self.assertEqual(self.msg(t), ""); t.keys("m"); time.sleep(1.0); self.assertEqual(self.msg(t), "", "no message for a card that is not done"); self.assertEqual(self.seen.read_bytes(), before)

    def test_every_m_on_the_done_cards_in_turn_reads_all_of_them(self):
        t = self.board(); t.keys("l"); t.keys("m"); self.assertTrue(wait(lambda: self.word(t, "shop-feat") == "⏸ idle", 3)); t.keys("h"); t.keys("j"); t.keys("j")
        self.assertEqual(self.look(t, "shop-hf2")[0], "● done"); t.keys("m"); self.assertTrue(wait(lambda: self.word(t, "shop-hf2") == "⏸ idle", 3)); self.assertEqual(self.entries(), {"g02": KEY0, "g03": KEY0})

    def test_nothing_is_sent_to_ccmux_or_tmux_and_the_board_does_not_leave(self):
        t = self.board(); t.keys("l"); t.keys("m"); self.assertTrue(wait(lambda: self.word(t, "shop-feat") == "⏸ idle", 3)); time.sleep(0.5)
        self.assertEqual(self.env.actions(), [], "m is not a jump"); self.assertFalse(any(c[0] == "switch-client" for c in self.env.tmux_calls())); self.assertTrue(t.alive())


class NotDone(Base):
    def test_working_waiting_and_plain_idle_cards_do_nothing_and_say_nothing(self):
        self.edit(lambda by: by["sql-b"].update(status="idle"))
        t = self.board(); self.assertEqual(self.look(t, "shop-main")[0], "⏵ working"); t.keys("m"); self.nothing(t)  # focus starts on shop-main
        t.keys("j"); time.sleep(0.4); self.assertEqual(self.look(t, "shop-hf")[0], "⏳ waiting"); t.keys("m"); self.nothing(t); self.assertEqual(self.look(t, "shop-hf")[0], "⏳ waiting")
        for _ in range(8):
            if self.look(t, "sql-b") and C_FOCUS in border_colours(self.card(t, "sql-b")): break
            t.keys("j"); time.sleep(0.25)
        self.assertEqual(self.look(t, "sql-b")[0], "⏸ idle"); t.keys("m"); self.nothing(t)

    def test_a_folded_group_title_does_nothing(self):
        t = self.board(); # the focus starts on shop-main; folding its group puts the focus on the title row
        t.keys("f"); time.sleep(0.6); self.assertTrue(any(r.text.lstrip().startswith("▸") for r in t.snap()), t.text()); self.assertEqual(self.entries(), {})
        t.keys("m"); self.nothing(t); self.assertTrue(any(r.text.lstrip().startswith("▸") for r in t.snap()), "still folded, nothing opened")


class Persistence(Base):
    def read_one(self):
        t = self.board(); t.keys("l"); t.keys("m"); self.assertTrue(wait(lambda: self.entries() == {"g03": KEY0} and self.word(t, "shop-feat") == "⏸ idle", 4)); return t

    def test_a_restart_of_the_board_is_still_idle_and_the_other_done_is_still_done(self):
        t = self.read_one(); t.close(); t2 = self.board(); self.assertEqual((self.word(t2, "shop-feat"), self.word(t2, "shop-hf2")), ("⏸ idle", "● done"))

    def test_a_new_status_change_is_a_new_done(self):
        t = self.read_one(); self.edit(lambda by: by["shop-feat"].update(statusChangedAt=T1)); self.assertTrue(wait(lambda: self.word(t, "shop-feat") == "● done", 4), "done again")
        t.keys("m"); self.assertTrue(wait(lambda: self.word(t, "shop-feat") == "⏸ idle" and self.entries().get("g03") == "2026-10-13T11:30:00Z", 4), self.entries())

    def test_a_corrupt_seen_json_is_not_overwritten_and_the_message_is_not_read(self):
        self.seen.parent.mkdir(parents=True, exist_ok=True); self.seen.write_text("{not json"); t = self.board(); t.keys("l"); t.keys("m"); time.sleep(1.5)
        self.assertEqual(self.seen.read_text(), "{not json"); self.assertNotEqual(self.msg(t), "read shop-feat", "no claim that it was read when it was not"); self.assertEqual(self.word(t, "shop-feat"), "● done"); self.assertTrue(t.alive())

    def test_a_read_only_state_directory_says_the_error_and_the_card_stays_done(self):
        d = self.env.state / "agmux"; d.mkdir(parents=True, exist_ok=True); d.chmod(0o500); self.addCleanup(lambda: d.chmod(0o700))
        t = self.board(); t.keys("l"); t.keys("m"); time.sleep(1.5); self.assertEqual(self.word(t, "shop-feat"), "● done"); self.assertNotEqual(self.msg(t), "read shop-feat", self.msg(t)); self.assertNotEqual(self.msg(t), "", "it says something"); self.assertTrue(t.alive())


class Inputs(Base):
    """m is a letter / not the action wherever the keys mean something else"""
    def test_search_box_takes_it_as_text(self):
        t = self.board(); t.keys("l"); t.keys("/"); time.sleep(0.3); t.lit("m"); time.sleep(0.6); self.assertIn("/ m", t.text()); self.nothing(t, 0.3); self.assertEqual(self.word(t, "shop-feat"), "● done")

    def test_flash_does_not_mark(self):
        t = self.board(); t.keys("l"); t.keys("s"); time.sleep(0.3); t.lit("m"); time.sleep(0.6); self.nothing(t, 0.3); self.assertEqual(self.word(t, "shop-feat"), "● done")

    def test_the_kill_prompt_takes_it_as_no_and_reads_nothing(self):
        t = self.board(); t.keys("l"); t.keys("x"); time.sleep(0.5); self.assertIn("y to confirm", "\n".join(r.text for r in t.snap()[-4:]), t.text()); t.keys("m"); time.sleep(0.6)
        self.assertNotIn("y to confirm", "\n".join(r.text for r in t.snap()[-4:]), "any key but y cancels, as before"); self.nothing(t, 0.3); self.assertEqual(self.env.actions(), [], "and nothing was killed"); self.assertEqual(self.word(t, "shop-feat"), "● done")

    def test_the_avatar_picker_ignores_it(self):
        t = self.board(); t.keys("l"); t.keys("a"); time.sleep(0.6); self.assertTrue(find_grid(t.snap()), "the picker is open")
        t.keys("m"); time.sleep(0.6); self.assertEqual(self.entries(), {}); self.assertNotIn("read dora", t.text()); t.keys("a"); time.sleep(0.5); self.assertEqual(self.word(t, "shop-feat"), "● done"); self.assertEqual(self.entries(), {})

    def test_the_pass_through_sends_the_letter_to_the_pane(self):
        t = self.board(); t.keys("l"); t.keys("Tab"); self.assertTrue(wait(lambda: "keys go to the pane" in last_text_row(t.snap()), 3), t.text()); self.assertTrue(wait(lambda: "g03" in self.entries(), 2), "tab marked it (T45)")
        before = self.seen.read_bytes(); n = len(self.env.tmux_calls()); t.lit("m"); time.sleep(0.8)
        sent = [c for c in self.env.tmux_calls()[n:] if c[0] == "send-keys"]; self.assertTrue(any(c[-1] == "m" for c in sent), f"m went to the pane: {self.env.tmux_calls()[n:]}")
        self.assertFalse(self.msg(t).startswith("read "), self.msg(t)); self.assertEqual(self.seen.read_bytes(), before)


class Keys(Base):
    def test_config_lists_mark_read_with_the_default_key(self):
        out = self.cli("config", text="").stdout; self.assertIn('mark_read = ["m"]             # default: m — mark the focused card\'s done as seen (it turns idle)', out)
        lines = [l.split(" = ")[0] for l in out.splitlines() if " = [" in l]; self.assertEqual(lines.index("mark_read"), lines.index("pin") + 1, "right after pin")

    def test_another_key_marks_and_m_no_longer_does(self):
        t = self.board_cfg('[keys]\nmark_read = ["M"]\n'); t.keys("l"); t.keys("m"); self.nothing(t, 1.0); self.assertEqual(self.word(t, "shop-feat"), "● done")
        t.keys("M"); self.assertTrue(wait(lambda: self.word(t, "shop-feat") == "⏸ idle" and self.msg(t) == "read shop-feat", 3), self.msg(t)); self.assertEqual(self.entries(), {"g03": KEY0})

    def test_mark_read_cannot_be_empty(self):
        r = self.cli("config", text="[keys]\nmark_read = []\n"); self.assertNotEqual(r.returncode, 0); self.assertTrue(r.stderr.strip().endswith("keys.mark_read: needs at least one key"), r.stderr)
        r = self.cli("board", text="[keys]\nmark_read = []\n"); self.assertNotEqual(r.returncode, 0); self.assertIn("keys.mark_read: needs at least one key", r.stderr)

    def test_a_key_another_action_asks_for_is_an_error_and_m_taken_by_another_action_leaves_mark_read_without_a_key(self):
        r = self.cli("config", text='[keys]\nmark_read = ["x"]\nkill = ["x"]\n'); self.assertNotEqual(r.returncode, 0); self.assertIn("already bound to", r.stderr)
        out = self.cli("config", text='[keys]\npin = ["m"]\n').stdout; self.assertIn("# mark_read: no key (another action took its default)", out)
        out = self.cli("config", text='[keys]\nmark_read = ["t"]\n').stdout; self.assertIn("# pin: no key (another action took its default)", out); self.assertIn('mark_read = ["t"]', out)

    def test_pin_on_m_pins_and_does_not_read(self):
        t = self.board_cfg('[keys]\npin = ["m"]\n'); t.keys("l"); t.keys("m"); self.assertTrue(wait(lambda: self.msg(t) == "pinned shop-feat", 3), self.msg(t)); self.assertEqual(self.entries(), {}); self.assertEqual(self.word(t, "shop-feat"), "● done")

    def test_the_footer_does_not_gain_a_hint(self):
        t = self.board(); bar = last_text_row(t.snap()); self.assertNotIn("mark", bar); self.assertNotIn("m read", bar)


class Stay(T45.Stay):
    """real tmux popup (--stay): m marks and the board stays"""
    def test_m_in_a_stay_popup_marks_the_card_and_the_popup_stays(self):
        self.start_board(); uc = self.terminal(); p = self.popup(uc); self.press("l"); time.sleep(0.5); self.assertEqual(self.word("bravo-29"), "● done"); self.press("m")
        self.assertTrue(wait(lambda: self.entries() == {"b1": KEY0}, 4), self.entries()); self.assertTrue(wait(lambda: self.word("bravo-29") == "⏸ idle", 3)); self.assertFalse(self.closed(p), "the popup is still open"); self.assertTrue(self.alive())


for _n in [n for n in dir(T45.Stay) if n.startswith("test_")]: setattr(Stay, _n, None)  # only the new case runs here


if __name__ == "__main__":
    unittest.main()
