"""T37 card line 2 shows the agent definition at the right edge (colors.agent, default #bd93f9) when the session's transcript names one: the last `{"type":"agent-setting",
"agentSetting":"..."}` line in the tail (final 64KB, read every poll), else the first one in the head (first 64KB, remembered per file); the branch is cut first when both do not
fit; `agmux show` has the agent as its 7th column. Spec: specs/m3-config/tasks.md「第七批」T37 (source rule as changed by the review of ebcc6db)."""
import json, subprocess, unittest
from lib import *
from test_t3 import ready, by_name

AGENT = ("rgb", 189, 147, 249)  # #bd93f9
SETTING = lambda name: json.dumps({"type": "agent-setting", "agentSetting": name}) + "\n"
FILL = json.dumps({"type": "user", "message": {"content": "x" * 900}}) + "\n"  # ~1KB of nothing
USER = '{"type":"user"}\n'


def filler(kb): return FILL * kb


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env); self.logs = {}

    def log(self, sid, text, append=False):
        """sets session sid's transcript (a jsonl file next to the fake state) and points the fixture session at it"""
        p = self.logs.get(sid) or (self.env.tmp / f"{sid}.agent.jsonl"); self.logs[sid] = p
        with open(p, "a" if append else "w") as f: f.write(text)
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            if s["id"] == sid: s["logPath"] = str(p)
        self.env.write_show(S); return p

    def board(self, w=200, h=50, config=None):
        cfg = self.env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True, exist_ok=True)
        if config: (cfg / "agmux" / "config.toml").write_text(config)
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={cfg}"); self.addCleanup(t.close); self.assertTrue(ready(t), t.text()); return t

    def card(self, t, name): return by_name(find_cards(t.snap()), name)

    def agent_of(self, t, name):
        """the right-edge text of line 2 in the agent colour, '' when there is none"""
        row = self.card(t, name).rows[2]; return "".join(c[0] for c in row.cells[8:] if c[1] == AGENT and c[0] != " ")  # (right of the avatar: a sprite may share the colour)

    def show_col(self):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "LANG": "en_US.UTF-8"}
        out = subprocess.run([BIN, "show"], env=env, capture_output=True, text=True, timeout=10).stdout
        return {l.split("\t")[1]: l.split("\t") for l in out.splitlines() if l}


class Source(Base):
    def test_the_first_line_names_the_agent(self):
        self.log("b1", SETTING("orchestrator") + USER); t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "orchestrator")

    def test_no_agent_setting_line_the_second_line_is_as_before(self):
        self.log("b1", USER + USER); t = self.board(); c = self.card(t, "bravo-29"); self.assertEqual(self.agent_of(t, "bravo-29"), ""); self.assertRegex(c.line(2), r"main\s+[│┃]$")

    def test_a_session_without_any_log_is_as_before(self):
        t = self.board(); self.assertEqual(self.agent_of(t, "charlie-30"), "")

    def test_the_last_line_in_the_tail_wins_a_resume_changed_the_agent(self):
        self.log("b1", SETTING("orchestrator") + USER * 5 + SETTING("reviewer") + USER); t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "reviewer")

    def test_a_long_session_resumed_with_agent_much_later_shows_it(self):
        self.log("b1", USER + filler(130) + SETTING("late-agent") + USER); t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "late-agent", "the setting is in the tail, not in the first 64KB")

    def test_only_in_the_head_of_a_long_transcript_still_shows(self):
        self.log("b1", SETTING("head-agent") + filler(150) + USER); t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "head-agent", "no setting in the tail: the first one of the head")

    def test_head_and_tail_the_tail_wins(self):
        self.log("b1", SETTING("old-agent") + filler(150) + SETTING("new-agent") + USER); t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "new-agent")

    def test_only_after_the_first_64KB_and_before_the_tail_is_not_read(self):  # neither the head nor the tail: documented limit
        self.log("b1", USER + filler(80) + SETTING("lost") + filler(80)); t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "")

    def test_other_line_types_and_bad_values_are_ignored(self):
        self.log("b1", json.dumps({"type": "user", "agentSetting": "nope"}) + "\n" + json.dumps({"type": "agent-setting", "agentSetting": ""}) + "\n" + json.dumps({"type": "agent-setting", "agentSetting": 5}) + "\n"
                 + '{"type":"agent-setting","agentSetting":\n' + USER)
        t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "")

    def test_the_agent_appears_on_a_later_poll_when_the_line_is_written_later(self):
        self.log("b1", USER); t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "")
        self.log("b1", SETTING("late") , append=True)
        self.assertTrue(wait(lambda: self.agent_of(t, "bravo-29") == "late", 8), "short file: looked at again every poll"); t.close()

    def test_a_change_in_the_tail_shows_up_on_the_next_poll(self):
        self.log("b1", SETTING("first") + USER); t = self.board(); self.assertEqual(self.agent_of(t, "bravo-29"), "first")
        self.log("b1", SETTING("second") + USER, append=True); self.assertTrue(wait(lambda: self.agent_of(t, "bravo-29") == "second", 8), "resume with another agent: the new one")


class Layout(Base):
    def test_the_agent_is_at_the_right_edge_and_in_the_agent_colour(self):
        self.log("b1", SETTING("orchestrator")); t = self.board(); c = self.card(t, "bravo-29"); row = c.rows[2]; txt = row.text
        self.assertRegex(txt, r"main\s+orchestrator[│┃]$", txt); i = row.find("orchestrator"); self.assertEqual({row.cells[i + k][1] for k in range(12)}, {AGENT})
        self.assertEqual(i + 12, c.w - 1, "touching the right border (no margin since T39)")

    def test_colors_agent_changes_it(self):
        self.log("b1", SETTING("orchestrator")); t = self.board(config='[colors]\nagent = "#010203"\n'); row = self.card(t, "bravo-29").rows[2]; i = row.find("orchestrator")
        self.assertEqual({row.cells[i + k][1] for k in range(12)}, {("rgb", 1, 2, 3)})

    def test_the_branch_is_cut_first_when_both_do_not_fit(self):
        self.log("a1", SETTING("orchestrator")); t = self.board(w=100, h=40); c = self.card(t, "alpha-main"); txt = c.line(2)  # a1 has a very long branch
        self.assertIn("orchestrator", txt); self.assertIn("…", txt, "the branch is the one that was cut"); self.assertLess(txt.index("…"), txt.index("orchestrator")); self.assertEqual(self.agent_of(t, "alpha-main"), "orchestrator", "the agent whole")

    def test_a_card_without_a_branch_shows_only_the_agent_on_the_right(self):
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            if s["id"] == "b1": s["gitBranch"] = None
        self.env.write_show(S); self.log("b1", SETTING("orchestrator")); t = self.board(); c = self.card(t, "bravo-29")
        self.assertRegex(c.line(2), r"^[│┃][▀▄█ ]{6}\s+orchestrator[│┃]$", c.line(2)); self.assertNotIn("", c.line(2))

    def test_a_very_narrow_card_cuts_the_agent_too_with_an_ellipsis(self):
        self.log("b1", SETTING("a-very-long-agent-definition-name-that-cannot-fit-in-a-narrow-column")); t = self.board(w=44, h=60); c = self.card(t, "bravo-29")
        self.assertIn("…", c.line(2)); self.assertEqual(c.rows[2].cells[0][0] in "│┃", True); self.assertEqual(c.rows[2].cells[c.w - 1][0] in "│┃", True, "the card frame is intact")

    def test_search_highlights_in_the_branch_still_work_next_to_an_agent(self):
        self.log("b1", SETTING("orchestrator")); t = self.board(); t.keys("/"); time.sleep(0.3); t.lit("'main"); time.sleep(0.5); c = self.card(t, "bravo-29")
        self.assertEqual(self.agent_of(t, "bravo-29"), "orchestrator")


class Show(Base):
    def test_agmux_show_has_the_agent_as_the_seventh_column_and_bg_as_the_eighth(self):
        self.log("b1", SETTING("orchestrator")); rows = self.show_col(); self.assertEqual(len(rows["bravo-29"]), 8, rows["bravo-29"]); self.assertEqual(rows["bravo-29"][6], "orchestrator")
        self.assertEqual(rows["charlie-30"][6], "", "an empty column when there is none"); self.assertTrue(all(len(r) == 8 for r in rows.values()))  # T59: the 8th is `bg` or empty

    def test_the_help_text_mentions_it(self):
        env = {"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "LANG": "en_US.UTF-8"}; out = subprocess.run([BIN, "help"], env=env, capture_output=True, text=True).stdout
        self.assertRegex(out, r"show\s+print sessions one per line:.*context%, agent")


class Config(Base):
    def cli(self, *a, text=None):
        cfg = self.env.tmp / "cfg2"; (cfg / "agmux").mkdir(parents=True, exist_ok=True)
        if text is not None: (cfg / "agmux" / "config.toml").write_text(text)
        return subprocess.run([BIN, *a], env={"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(cfg), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10)

    def test_the_default_colour_is_listed_between_effort_and_dir(self):
        out = plain_config(self.cli("config", text="").stdout); self.assertIn('effort = "13"\nagent = "#bd93f9"\ndir = "11"', out)

    def test_a_bad_colour_is_reported_like_the_others(self):
        r = self.cli("config", text='[colors]\nagent = "nope"\n'); self.assertEqual(r.returncode, 1); self.assertIn("colors.agent", r.stderr)


if __name__ == "__main__":
    unittest.main()
