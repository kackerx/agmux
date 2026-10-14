"""T51 four more actions in [keys], none bound by default: `group_cwd`, `group_project`, `group_session`, `group_state` switch straight to that grouping (what `g` does when it lands there: folds cleared, focus kept,
manual scroll ended; already in that grouping: nothing changes). `[]` means "no key" and is accepted for these four only; a key still belongs to one action; `agmux config` prints the four after `group` with
`# default: none`. Spec: specs/m3-config/tasks.md T51. Oracle and fixture: test_t43 (80x70, one column)."""
import unittest
from lib import *
from test_t11 import PreviewBase, pane_text
from test_t43 import Base, MODES, oracle, want_outline, outline_of
from test_t3 import C_FOCUS, last_text_row

ACTIONS = {"cwd": "group_cwd", "project": "group_project", "session": "group_session", "state": "group_state"}
KEYS4 = '[keys]\ngroup_cwd = ["C"]\ngroup_project = ["P"]\ngroup_session = ["S"]\ngroup_state = ["G"]\n'


def focus_visible(t): return any(c[0] in "┃│╭╮╰╯" and c[1] == C_FOCUS for row in t.snap() for c in row.cells if c)  # a piece of the focused card's border on screen (a clipped card is not a whole card for find_cards)


class Direct(Base):
    def folded(self, t): return sum(x.startswith("▸") for x in self.outline(t))

    def test_g_in_state_goes_straight_to_the_state_grouping_and_the_next_g_cycles_on_to_cwd(self):
        t = self.board('[keys]\ngroup_state = ["G"]\n'); self.expect(t, "cwd"); t.keys("G"); self.expect(t, "state"); self.assertEqual(self.outline(t)[0].split(" ")[1], "working", "working comes first")
        t.keys("g"); self.expect(t, "cwd")

    def test_it_clears_the_folds_and_keeps_the_focused_card(self):
        t = self.board('[keys]\ngroup_state = ["G"]\n'); t.keys("f"); time.sleep(0.4); t.keys("j"); time.sleep(0.4); before = self.focused(t); self.assertEqual(len(before), 1); self.assertEqual(self.folded(t), 1, "the first group is folded, the focus went on to the next one")
        t.keys("G"); self.expect(t, "state"); self.assertEqual(self.folded(t), 0, "every group open"); self.assertEqual(self.focused(t), before, "the same card has the focus")

    def test_already_in_that_grouping_nothing_changes_folds_stay(self):
        t = self.board('[layout]\ngroup_by = "state"\n[keys]\ngroup_state = ["G"]\n'); self.expect(t, "state"); t.keys("f"); time.sleep(0.4); o = self.outline(t); self.assertEqual(self.folded(t), 1)
        t.keys("G"); time.sleep(0.6); self.assertEqual(self.outline(t), o, "same grouping: the fold stays"); self.assertEqual(self.bar(t).count("group:state"), 1)

    def test_all_four_go_to_their_grouping_from_any_other(self):
        t = self.board(KEYS4)
        for src in MODES:
            for dst in MODES:
                if src == dst: continue
                t.keys({"cwd": "C", "project": "P", "session": "S", "state": "G"}[src]); self.expect(t, src); t.keys({"cwd": "C", "project": "P", "session": "S", "state": "G"}[dst]); self.expect(t, dst)

    def test_a_direct_switch_ends_a_manual_scroll_and_shows_the_focus(self):
        self.config('[keys]\ngroup_state = ["G"]\n'); t = Term(self.env, w=80, h=22, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: "↓ more" in t.text(), 10), t.text()); time.sleep(0.4)
        self.assertTrue(focus_visible(t), "the first card is focused and on screen"); t.keys("C-d"); time.sleep(0.5); self.assertIn("↑ more", t.text()); t.keys("C-d"); time.sleep(0.5); t.keys("C-d"); time.sleep(0.5); self.assertFalse(focus_visible(t), "scrolled away: the focused card is off screen"); t.keys("G"); time.sleep(0.8)
        self.assertTrue(focus_visible(t), "the focused card is on screen after the switch:\n" + t.text())

    def test_default_config_the_four_keys_do_nothing_and_g_still_cycles(self):
        t = self.board(""); self.expect(t, "cwd")
        for k in ("C", "P", "S", "G"): t.keys(k); time.sleep(0.3)
        self.expect(t, "cwd"); t.keys("g"); self.expect(t, "project")

    def test_the_key_typed_in_the_search_box_is_text_not_a_switch(self):
        t = self.board('[keys]\ngroup_state = ["G"]\n'); t.keys("/"); time.sleep(0.3); t.keys("G"); time.sleep(0.5); self.assertIn("/ G", t.text()); self.assertNotIn("working", "\n".join(self.outline(t)[:1])); t.keys("Escape"); time.sleep(0.6)
        self.expect(t, "cwd")

    def test_g_itself_can_be_given_to_a_direct_action_and_then_the_cycle_has_no_key(self):
        r = self.cli("config", text='[keys]\ngroup_state = ["g"]\n'); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("# group: no key (another action took its default)", r.stdout)
        t = self.board('[keys]\ngroup_state = ["g"]\n'); t.keys("g"); self.expect(t, "state")
        t.keys("g"); time.sleep(0.4); self.expect(t, "state")


class Passthrough(PreviewBase):
    """with the keys on the preview's pane (a3 runs a raw cat -v) `G` is a character for the pane, not group_state"""
    def board(self, w, h):
        cfg = self.env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True); (cfg / "agmux" / "config.toml").write_text('[keys]\ngroup_state = ["G"]\n')
        t = Term(self.env, w=w, h=h, real_tmux=True, extra_env=f"XDG_CONFIG_HOME={cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: "agmux ·" in t.text(), 8), t.text()); return t

    def press(self, *keys):
        for k in keys: self.t.keys(k); time.sleep(0.3)

    def test_the_key_goes_to_the_pane_in_the_pass_through_and_switches_on_the_cards(self):
        self.press("j", "j", "p"); self.assertTrue(wait(lambda: self.pv() is not None, 4), self.t.text()); self.press("Tab"); self.assertTrue(wait(lambda: "keys go to the pane" in last_text_row(self.t.snap()), 3))
        self.t.lit("G"); time.sleep(0.5); self.assertIn("G", pane_text(self.panes["a3"], 5), "the pane got the G"); self.assertTrue(any("▾ ~/work/alpha (3)" in r.text for r in self.t.snap()), "still cwd groups")
        self.press("Tab"); self.t.keys("G"); self.assertTrue(wait(lambda: any(r.text.strip().startswith("▾ working") for r in self.t.snap()), 4), "on the cards G switches to the state grouping\n" + self.t.text())


class Config(Base):
    def test_config_prints_the_four_after_group_and_before_restart_with_the_default_none(self):
        out = self.cli("config", text="").stdout; lines = [l.split(" = ")[0] for l in out.splitlines() if re.match(r"^[a-z_]+ = ", l)]; i = lines.index("group")
        self.assertEqual(lines[i:i + 6], ["group", "group_cwd", "group_project", "group_session", "group_state", "restart"])
        for a, m in ACTIONS.items(): self.assertRegex(out, rf"(?m)^{m} = \[\] +# default: none — switch straight to the {a} grouping$")

    def test_bound_keys_print_as_values_and_the_note_stays(self):
        out = self.cli("config", text=KEYS4).stdout
        for k, m in zip("CPSG", ACTIONS.values()): self.assertRegex(out, rf'(?m)^{m} = \["{k}"\] +# default: none — ')
        self.config(out); r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout, out, "written back: the same")

    def test_an_empty_list_is_fine_for_the_four_and_an_error_for_the_others(self):
        for a in ACTIONS.values():
            r = self.cli("config", text=f"[keys]\n{a} = []\n"); self.assertEqual(r.returncode, 0, f"{a}: {r.stderr}")
        r = self.cli("config", text="[keys]\nup = []\n"); self.assertEqual(r.returncode, 1); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr); self.assertIn("up", r.stderr)
        r = self.cli("config", text="[keys]\ngroup = []\n"); self.assertEqual(r.returncode, 1, "group (the cycle) still needs a key")

    def test_a_key_belongs_to_one_action(self):
        r = self.cli("config", text='[keys]\ngroup_state = ["G"]\ngroup_cwd = ["G"]\n'); self.assertEqual(r.returncode, 1); self.assertRegex(r.stderr, r'"G" is already bound to group_(state|cwd)'); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)
        r = self.cli("config", text='[keys]\ngroup_state = ["G"]\ngroup = ["G"]\n'); self.assertEqual(r.returncode, 1)
        r = self.cli("config", text='[keys]\ngroup_state = ["x"]\n'); self.assertEqual(r.returncode, 1, "x is bound to kill explicitly? no: default holder gives way") if False else None

    def test_an_explicit_key_takes_the_default_of_another_action_the_t17_way(self):
        r = self.cli("config", text='[keys]\ngroup_state = ["x"]\n'); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn('group_state = ["x"]', r.stdout); self.assertIn("# kill: no key (another action took its default)", r.stdout)

    def test_the_footer_has_no_new_words(self):
        t = self.board('[keys]\ngroup_state = ["G"]\n', w=220, h=30); bar = self.bar(t); self.assertIn("f fold · g group:cwd · p preview", bar); self.assertNotIn("group_state", bar); self.assertNotIn("state", bar.replace("group:state", ""))


if __name__ == "__main__":
    unittest.main()
