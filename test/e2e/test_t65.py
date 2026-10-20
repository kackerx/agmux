"""T65 a ccmux record of a daemon's spare process (`claude --bg` pre-warms them; ccmux lists them as trackingMode background) is not a session: background + no logPath + no lastPrompt + no prompts. It has no card, no row in
`agmux show`, no count in the top bar, and no key can make agmux run `claude attach <its id>` (which would make the daemon claim the spare as a NEW session forking the one running in that directory). Everything else is shown as
before: a background record with a prompt (a real session just started: no transcript yet), with a transcript but no prompt, a native record without either. Spec: specs/m3-config/tasks.md「第十三批」T65. Fake ccmux / fake tmux
(the harness' one with test_t60's patch: new-window answers `%777`, so an attach would show up as a call)."""
import json, subprocess, unittest
from lib import *
from test_t3 import C_FOCUS, border_colours
from test_t60 import patch_tmux, add_bg

SPARE = dict(id="dce23d2f", summary=None, project="blessmuse", logPath=None, lastPrompt="", prompts=[], trackingMode="background", tmuxPane=None, tmuxTarget=None)


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env); patch_tmux(self.env); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def spare(self, **kw):
        S = json.loads((self.env.fake / "show.json").read_text()); b2 = next(s for s in S if s["id"] == "b2")
        S.append({**b2, **SPARE, "nativeSessionId": "n-spare", **kw}); self.env.write_show(S); return S

    def board(self, w=200, h=50, n=9):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) >= n, 10), t.text()); time.sleep(0.6); return t

    def show_ids(self):
        out = subprocess.run([BIN, "show"], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10).stdout
        return [l.split("\t")[0] for l in out.splitlines() if l]

    def cards(self, t): return [c for c in find_cards(t.snap()) if c.ok]


class Spare(Base):
    def test_the_spare_record_has_no_card_no_show_row_and_no_count(self):
        self.spare(); self.assertEqual(sorted(self.show_ids()), sorted(["a1", "a2", "a3", "b1", "b2", "c1", "c2", "d1", "e1"]), "agmux show: the nine sessions of the fixture, not the spare"); t = self.board()
        self.assertEqual(len(self.cards(t)), 9); self.assertIn("agmux · 9 sessions", t.text().split("\n")[0]); self.assertEqual(sum("blessmuse" in c.line(1) for c in self.cards(t)), 0, "the card the old board showed, named after the project")

    def test_the_other_missing_field_spellings_are_all_spares(self):
        for kw in (dict(lastPrompt=None), dict(lastPrompt=""), dict(prompts=None), dict(prompts=[]), dict(lastPrompt=None, prompts=None), dict(logPath="")):
            with self.subTest(kw=kw):
                S = json.loads((self.env.fake / "show.json").read_text()); S = [s for s in S if s["id"] != "dce23d2f"]; self.env.write_show(S); self.spare(**kw); self.assertNotIn("dce23d2f", self.show_ids())
        S = [s for s in json.loads((self.env.fake / "show.json").read_text()) if s["id"] != "dce23d2f"]; S.append({**next(s for s in S if s["id"] == "b2"), **SPARE, "nativeSessionId": "n-spare"}); [s.pop("prompts", None) for s in S if s["id"] == "dce23d2f"]; [s.pop("lastPrompt", None) for s in S if s["id"] == "dce23d2f"]; self.env.write_show(S)
        self.assertNotIn("dce23d2f", self.show_ids(), "neither field in the record at all")

    def test_a_record_that_has_something_of_a_session_is_shown(self):
        for label, kw in (("a prompt only in lastPrompt (a session just started: no transcript yet)", dict(lastPrompt="first words", summary="fresh-bg")), ("a prompt only in prompts", dict(prompts=["first words"], summary="fresh-bg")),
                          ("a transcript, no prompt", dict(logPath=str(self.env.tmp / "a1.jsonl"), summary="fresh-bg"))):
            with self.subTest(label=label):
                S = [s for s in json.loads((self.env.fake / "show.json").read_text()) if s["id"] != "dce23d2f"]; self.env.write_show(S); self.spare(**kw)
                self.assertIn("dce23d2f", self.show_ids()); t = self.board(n=10); self.assertEqual(len(self.cards(t)), 10); self.assertTrue(any("fresh-bg" in c.line(1) for c in self.cards(t))); self.assertIn("agmux · 10 sessions", t.text().split("\n")[0]); t.close()

    def test_a_native_record_without_transcript_and_prompt_is_a_session(self):
        S = json.loads((self.env.fake / "show.json").read_text()); n = {**next(s for s in S if s["id"] == "c1"), "id": "nat1", "summary": "native-quiet", "logPath": None, "lastPrompt": "", "prompts": [], "nativeSessionId": "n-nat1"}; S.append(n); self.env.write_show(S)
        self.assertIn("nat1", self.show_ids()); t = self.board(n=10); self.assertTrue(any("native-quiet" in c.line(1) for c in self.cards(t)))

    def test_a_background_record_with_a_pane_and_nothing_else_is_filtered_too(self):
        # (T65's rule is on the record's kind and what it has, not on a pane: a spare that someone attached by hand is still the daemon's spare in ccmux's listing)
        self.spare(tmuxPane="%999", tmuxTarget="agmux-bg # x:0.0"); self.assertNotIn("dce23d2f", self.show_ids())

    def test_seen_pins_and_avatars_are_not_confused_by_the_spare_having_no_name(self):
        self.spare(); t = self.board(); t.keys("j"); time.sleep(0.4); t.keys("t"); time.sleep(0.8); self.assertTrue(t.alive()); self.assertEqual(len(self.cards(t)), 9)


class NoAttach(Base):
    def test_no_key_on_any_card_runs_claude_attach_for_the_spare_or_asks_tmux_about_it(self):
        self.spare(); t = self.board(w=232, h=50); t.keys("p"); time.sleep(0.3)
        for step in range(12):  # walk the board down and press the keys that start an attach on every card (enter would end the board)
            for k in ("i", "Tab", "Tab", "H"): t.keys(k); time.sleep(0.25)
            t.keys("j"); time.sleep(0.2)
        calls = self.env.tmux_calls(raw=True); self.assertEqual([c for c in calls if "dce23d2f" in " ".join(c)], [], "tmux was never asked about the spare"); self.assertEqual(self.env.calls(), [], "nor ccmux")
        ids = [c[-1] for c in calls if c[0] in ("new-window", "new-session")]; self.assertTrue(set(ids) <= {"b2"}, f"only the real background session (bravo-bg, b2) is ever attached: {ids}")

    def test_the_real_background_session_next_to_a_spare_is_still_attachable(self):
        self.spare(); t = self.board(); t.keys("l"); time.sleep(0.3); t.keys("j"); time.sleep(0.4); self.assertTrue(any(C_FOCUS in border_colours(c) and "bravo-bg" in c.line(1) for c in self.cards(t)), t.text()); t.keys("Enter"); time.sleep(1.2)
        new = [c for c in self.env.tmux_calls() if c[0] in ("new-window", "new-session")]; self.assertEqual(len(new), 1, self.env.tmux_calls()); self.assertEqual(new[0][-3:], ["claude", "attach", "b2"])


if __name__ == "__main__":
    unittest.main()
