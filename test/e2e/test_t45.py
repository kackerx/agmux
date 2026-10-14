"""T45 / T48 a done the user looked at on the board is read: a preview / the pass-through opens on it, or enter jumps to it (T48: resting on it, however long, no longer counts) -> $XDG_STATE_HOME/agmux/seen.json
gets {session id: statusChangedAt}, and the card is drawn idle from then on (status word, border colour); a restart keeps it; a new statusChangedAt is a new done; ccmux clearing attentionState (or the session going)
removes the entry; a null statusChangedAt is never recorded; a corrupt seen.json is empty + one footer line and is not overwritten; waiting / attention cards are never "seen".
Spec: specs/m3-config/tasks.md「第八批」T45, T48. (`agmux show` has no done column: it prints `idle` either way, so there is nothing to compare there.)

Fixture: build_group_fixture + statusChangedAt on every session; shop-feat and shop-hf2 are done (idle + attentionState unread). At 100 columns (2 columns of 50): row 1 = shop-main | shop-feat, row 2 = shop-hf, shop-hf2 | solo-bg;
the focus starts on shop-main: `l` = shop-feat, `j j` = shop-hf2 (via shop-hf)."""
import json, re, unittest
from lib import *
from test_t3 import by_name, border_colours, last_text_row
from test_t44 import Real

T0, T1 = "2026-10-13T10:00:00.000Z", "2026-10-13T11:30:00.000Z"
DONE = ("shop-feat", "shop-hf2")


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_group_fixture(self.env); self.seen = self.env.state / "agmux" / "seen.json"
        def f(by):
            for s in by.values(): s["statusChangedAt"] = T0
            by["shop-feat"].update(status="idle", attentionState="unread")
        self.edit(f)

    def edit(self, fn):
        S = json.loads((self.env.fake / "show.json").read_text()); fn({s["summary"]: s for s in S}); self.env.write_show(S)

    def board(self, w=100, h=60):
        t = Term(self.env, w=w, h=h); self.addCleanup(t.close); self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 8, 10), t.text()); time.sleep(0.3); return t

    def card(self, t, name):
        m = [c for c in find_cards(t.snap()) if re.match(r"\s*(\S+)", c.line(1)[8:]).group(1) == name]; self.assertEqual(len(m), 1, f"{name}: {len(m)} cards"); return m[0]

    def word(self, t, name):
        m = re.search(r"(⏵ working|⏸ idle|● done|⏳ waiting|● \w+)", self.card(t, name).line(3)); return m.group(1) if m else self.card(t, name).line(3)

    def entries(self): return json.loads(self.seen.read_text()) if self.seen.exists() else {}


KEY0 = "2026-10-13T10:00:00Z"  # seen.json writes the time in RFC3339 (the .000 fraction is dropped)


class NoDwell(Base):
    """T48: resting on a done card is not seeing it (the focus on it only shows it, the result is not read until a preview / the pass-through opens or enter jumps): it stays done, whatever the time."""
    def rest(self, t, secs=3.0):
        time.sleep(secs); self.assertEqual((self.word(t, "shop-hf2"), self.word(t, "shop-feat")), ("● done", "● done"), self.card(t, "shop-hf2").text); self.assertFalse(self.seen.exists(), f"no entry: {self.entries()}")

    def test_three_seconds_on_a_done_card_leave_it_done_and_record_nothing(self):
        t = self.board(); self.assertEqual((self.word(t, "shop-hf2"), self.word(t, "shop-feat")), ("● done", "● done")); self.assertFalse(self.seen.exists(), "nothing is seen at the start")
        t.keys("j"); t.keys("j"); self.rest(t); t.keys("k"); t.keys("l"); self.rest(t, 1.5)  # shop-hf2 for 3 s, then shop-feat

    def test_back_and_forth_over_the_done_cards_and_then_resting_counts_nothing(self):
        t = self.board()
        for k in ("l", "h", "j", "j", "k", "k", "l", "h", "j", "j"): t.keys(k); time.sleep(0.12)
        self.rest(t, 3.0)

    def test_a_card_seen_by_the_preview_is_drawn_like_any_idle_one_status_word_and_border(self):
        self.edit(lambda by: by["sql-b"].update(status="idle"))  # a plain idle card that never has the focus here
        t = self.board(); t.keys("j"); t.keys("j"); t.keys("p"); self.assertTrue(wait(lambda: self.word(t, "shop-hf2") == "⏸ idle", 4)); t.keys("p"); time.sleep(0.4)  # the preview opened and closed
        t.keys("l"); time.sleep(0.4)  # the focus moves off (its border is the focus colour while it is on it)
        plain = {c for c in border_colours(self.card(t, "sql-b")) if c}; seen = {c for c in border_colours(self.card(t, "shop-hf2")) if c}; done = {c for c in border_colours(self.card(t, "shop-feat")) if c}
        self.assertEqual(seen, plain, f"the seen done has the idle border {plain}, not the done one {done}"); self.assertNotEqual(done, plain)

    def test_resting_on_a_card_that_is_not_done_marks_nothing(self):
        t = self.board(); time.sleep(1.8); self.assertEqual(self.entries(), {}, "shop-main is working"); self.assertEqual(self.word(t, "shop-hf2"), "● done")

    def test_waiting_cards_are_never_seen(self):
        t = self.board(); t.keys("j"); t.keys("p"); time.sleep(1.2); self.assertEqual(self.word(t, "shop-hf"), "⏳ waiting"); self.assertEqual(self.entries(), {}, "the preview on a waiting card marks nothing")
        t.keys("p"); self.edit(lambda by: by["shop-hf"].update(attentionType="permission", status="working", pendingTool="Bash")); time.sleep(1.8); self.assertEqual(self.entries(), {}, "needs you: not a done")


class Immediate(Base):
    def test_opening_the_preview_on_a_done_card_marks_it_at_once(self):
        t = self.board(); t0 = time.time(); t.keys("l"); t.keys("p")
        self.assertTrue(wait(lambda: "g03" in self.entries(), 0.45), f"within 0.45 s (a rest would take 1 s+): {self.entries()}"); self.assertEqual(self.word(t, "shop-feat") in ("⏸ idle", "● done"), True)

    def test_the_pass_through_marks_it_at_once(self):
        t = self.board(); t.keys("l"); t.keys("Tab"); self.assertTrue(wait(lambda: "g03" in self.entries(), 0.45), f"{self.entries()}\n{t.text()}")

    def test_enter_marks_it_and_the_board_leaves(self):
        t = self.board(); t.keys("l"); t.keys("Enter"); self.assertEqual(t.exit_status(5), 0, t.text()); self.assertEqual(self.entries(), {"g03": KEY0})
        self.assertTrue(any(c[0] == "switch-client" for c in self.env.tmux_calls()), "it jumped")


class Persistence(Base):
    def seen_board(self):
        t = self.board(); t.keys("j"); t.keys("j"); t.keys("p"); self.assertTrue(wait(lambda: self.entries() == {"g02": KEY0}, 4)); t.keys("p"); time.sleep(0.4); return t  # the preview opened on shop-hf2 and closed again

    def test_a_restart_is_still_idle_and_the_other_done_is_still_done(self):
        t = self.seen_board(); t.close()
        t2 = self.board(); self.assertEqual((self.word(t2, "shop-hf2"), self.word(t2, "shop-feat")), ("⏸ idle", "● done"))

    def test_a_new_status_change_is_a_new_done(self):
        t = self.seen_board(); self.edit(lambda by: by["shop-hf2"].update(statusChangedAt=T1)); self.assertTrue(wait(lambda: self.word(t, "shop-hf2") == "● done", 4), "back to done when the time changed")
        time.sleep(2.5); self.assertEqual(self.entries()["g02"], KEY0, "(the old entry stays until the new one is made; the focus resting on it makes none, T48)"); self.assertEqual(self.word(t, "shop-hf2"), "● done")
        t.keys("p"); self.assertTrue(wait(lambda: self.entries().get("g02") == "2026-10-13T11:30:00Z" and self.word(t, "shop-hf2") == "⏸ idle", 6), "opening the preview on it: seen again")

    def test_ccmux_clearing_the_attention_state_removes_the_entry(self):
        t = self.seen_board(); self.edit(lambda by: by["shop-hf2"].update(attentionState=None)); self.assertTrue(wait(lambda: "g02" not in self.entries(), 6), self.entries()); self.assertEqual(self.word(t, "shop-hf2"), "⏸ idle")

    def test_a_session_that_is_gone_loses_its_entry(self):
        t = self.seen_board(); S = json.loads((self.env.fake / "show.json").read_text()); self.env.write_show([s for s in S if s["summary"] != "shop-hf2"]); self.assertTrue(wait(lambda: "g02" not in self.entries(), 6), self.entries())

    def test_the_entries_of_other_sessions_are_kept_when_one_is_removed(self):
        t = self.seen_board(); t.keys("k"); t.keys("k"); t.keys("l"); t.keys("p"); self.assertTrue(wait(lambda: set(self.entries()) == {"g02", "g03"}, 4), self.entries())
        self.edit(lambda by: by["shop-hf2"].update(attentionState=None)); self.assertTrue(wait(lambda: set(self.entries()) == {"g03"}, 6), self.entries())


class Edges(Base):
    def test_a_null_status_changed_at_is_never_recorded(self):
        self.edit(lambda by: by["shop-hf2"].update(statusChangedAt=None)); t = self.board(); t.keys("j"); t.keys("j"); t.keys("p"); time.sleep(2.0)
        self.assertEqual(self.entries(), {}, "no time, no telling this done from the next (even with the preview open)"); self.assertEqual(self.word(t, "shop-hf2"), "● done", "it stays done until ccmux clears it")

    def test_a_corrupt_seen_json_is_empty_says_one_line_and_is_not_overwritten(self):
        self.seen.parent.mkdir(parents=True, exist_ok=True); self.seen.write_text("{not json"); t = self.board()
        self.assertEqual((self.word(t, "shop-hf2"), self.word(t, "shop-feat")), ("● done", "● done"), "nothing is seen")
        bar = "\n".join(r.text for r in t.snap()[-3:]); self.assertIn("seen.json", bar); self.assertEqual(len([l for l in bar.splitlines() if "seen.json" in l]), 1, bar)
        t.keys("l"); t.keys("p"); time.sleep(2.0); self.assertEqual(self.seen.read_text(), "{not json", "the file is not overwritten (a preview on a done card would have written it)"); self.assertTrue(t.alive())


class Stay(Real):
    """--stay (T44) + T45 + T48, real tmux: in a popup too, the focus resting on a done card does not read it (no dwell, no hidden state any more); a preview or enter does. bravo-29 (the first card of column 2, `l`) is made a done."""
    def setUp(self):
        super().setUp(); self.seen = self.env.state / "agmux" / "seen.json"
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            s["statusChangedAt"] = T0
            if s["summary"] == "bravo-29": s.update(status="idle", subagents=[], attentionState="unread")
        self.env.write_show(S)

    def entries(self): return json.loads(self.seen.read_text()) if self.seen.exists() else {}

    def word(self, name):
        c = next(c for c in find_cards(self.screen()) if re.match(r"\s*(\S+)", c.line(1)[8:]).group(1) == name); m = re.search(r"(⏵ working|⏸ idle|● done|⏳ waiting)", c.line(3)); return m.group(1) if m else c.line(3)

    def test_watching_a_done_card_in_a_stay_popup_counts_nothing_but_a_preview_does(self):
        self.start_board(w=200); uc = self.terminal(w=210); p = self.popup(uc, w=200); self.press("l"); time.sleep(3.0); self.assertEqual(self.entries(), {}, "3 s on the done card in the popup"); self.assertEqual(self.word("bravo-29"), "● done")
        self.press("p"); self.assertTrue(wait(lambda: self.entries() == {"b1": KEY0}, 4), self.entries()); self.assertTrue(wait(lambda: self.word("bravo-29") == "⏸ idle", 3))

    def test_a_popup_that_was_closed_and_attached_again_still_counts_nothing_by_resting(self):
        self.start_board(); uc = self.terminal(); p = self.popup(uc); self.press("l"); self.press("q"); self.assertTrue(self.closed(p)); p2 = self.popup(uc); time.sleep(3.0)
        self.assertEqual(self.entries(), {}); self.assertEqual(self.word("bravo-29"), "● done")

    def test_enter_on_a_done_card_marks_it_even_though_the_board_stays(self):
        self.start_board(); uc = self.terminal(); p = self.popup(uc); self.press("l"); self.press("Enter"); self.assertTrue(self.closed(p))
        self.assertTrue(wait(lambda: self.entries() == {"b1": KEY0}, 4), self.entries()); self.assertTrue(self.alive()); self.assertEqual(self.word("bravo-29"), "⏸ idle")


if __name__ == "__main__":
    unittest.main()
