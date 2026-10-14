"""T43 grouping: `[layout] group_by = "cwd" | "project" | "session" | "state"` (default cwd) and the `g` action cycling cwd -> project -> session -> state -> cwd, the footer saying `g group:<now>` right after `f fold`.
cwd = the path (as before); project = ccmux's `project` ("" -> `(no project)`); session = the part of `tmuxTarget` before its first ` # ` (null -> `(no pane)`, always last); state = working / waiting / done / idle in that
fixed order (the card's own status word); groups of the name kinds are titled with the bare name, `name (n)`; cards inside a group stay in name order; switching opens every group (folds are cleared) and keeps
the focus; a name title draws no path hit of the search and `s` flash reads the shown title. Spec: specs/m3-config/tasks.md「第八批」T43, design.md「分组方式」 (+ the orchestrator's answers: (no pane) last,
project key = the `project` field, session = everything before the first ` # `, background sessions go by their own state).

Oracle: `oracle()` below is written from the spec text; every outline on screen is compared with it. One column (80 wide), tall enough for everything: the outline is the screen read top to bottom."""
import json, re, subprocess, unittest
from lib import *
from test_t3 import C_FOCUS, border_colours, last_text_row
from test_t11 import PreviewBase, pane_text

MATCH = ("rgb", 242, 208, 107)
MODES = ["cwd", "project", "session", "state"]
STATE_ORDER = ["working", "waiting", "done", "idle"]
W, H = 80, 70


def state_of(s):
    if s["status"] == "working" or any(a["status"] == "working" for a in s["subagents"]): return "working"
    if s["status"] == "waiting": return "waiting"
    return "done" if s["attentionState"] else "idle"  # (idle, nothing running, attentionState set)


def oracle(S, mode, home):
    """[(title, [card names])] in screen order, from the sessions (show.json dicts)"""
    g = {}
    for s in S:
        name = s["summary"] or s["project"]
        if mode == "cwd": title = s["cwd"].replace(home, "~", 1)
        elif mode == "project": title = s["project"] or "(no project)"
        elif mode == "session": title = s["tmuxTarget"].split(" # ")[0] if s["tmuxTarget"] else "(no pane)"
        else: title = state_of(s)
        g.setdefault(title, []).append(name)
    keys = STATE_ORDER if mode == "state" else sorted(g, key=lambda k: (k == "(no pane)", k))
    return [(k, sorted(g[k])) for k in keys if k in g]


def outline_of(snap):
    """the screen, one column: ['▾ title (n)', 'card name', ...] top to bottom (a folded title is '▸ ...')"""
    cards = {c.r + 1: c for c in find_cards(snap)}; out = []
    for r, row in enumerate(snap):
        t = row.text.strip()
        if t.startswith(("▾", "▸")): out.append(re.sub(r"\s*━+$", "", t).rstrip())
        elif r in cards: out.append(re.match(r"\s*(\S+)", cards[r].line(1)[8:]).group(1))
    return out


def want_outline(groups, folded=()):
    out = []
    for title, names in groups:
        out.append(f"{'▸' if title in folded else '▾'} {title} ({len(names)})")
        if title not in folded: out += names
    return out


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        self.fx = build_group_fixture(self.env); self.home = str(self.env.home)

    def config(self, text): (self.cfg / "agmux" / "config.toml").write_text(text)

    def sessions(self): return json.loads((self.env.fake / "show.json").read_text())

    def edit(self, fn):
        S = self.sessions(); fn(S); self.env.write_show(S)

    def board(self, config=None, w=W, h=H):
        if config is not None: self.config(config)
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 5, 10), t.text()); time.sleep(0.4); return t

    def outline(self, t): return outline_of(t.snap())

    def expect(self, t, mode, S=None, folded=(), timeout=8):
        want = want_outline(oracle(S or self.sessions(), mode, self.home), folded)
        def same():
            try: return outline_of(t.snap()) == want
            except IndexError: return False  # a capture torn by a repaint in progress (a card cut short): look again
        ok = wait(same, timeout)
        self.assertTrue(ok, f"mode {mode}:\nwant {want}\ngot  {outline_of(t.snap())}\n{t.text()}")

    def bar(self, t): return last_text_row(t.snap())

    def cli(self, *a, text=None):
        if text is not None: self.config(text)
        return subprocess.run([BIN, *a], env={"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10)

    def focused(self, t): return [c.line(1) for c in find_cards(t.snap()) if C_FOCUS in border_colours(c)]


class Config(Base):
    def test_the_default_is_cwd_and_config_prints_it_after_flow(self):
        out = plain_config(self.cli("config", text="").stdout); self.assertIn('[layout]\nmax_columns = 4\nflow = "bands"\ngroup_by = "cwd"\n', out)

    def test_each_mode_round_trips(self):
        for m in MODES:
            with self.subTest(m=m):
                r = self.cli("config", text=f'[layout]\ngroup_by = "{m}"\n'); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn(f'group_by = "{m}"', r.stdout)
                self.config(r.stdout); self.assertEqual(self.cli("config").stdout, r.stdout, "idempotent")

    def test_a_bad_value_is_one_line_naming_the_choices(self):
        for v in ('"x"', '""', '"Project"', '"cwd "', "3", "true"):
            with self.subTest(v=v):
                r = self.cli("config", text=f"[layout]\ngroup_by = {v}\n"); self.assertNotEqual(r.returncode, 0); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr); self.assertIn("layout.group_by", r.stderr)
        r = self.cli("config", text='[layout]\ngroup_by = "x"\n')
        self.assertTrue(r.stderr.strip().endswith('layout.group_by: "x" is not a grouping (use cwd, project, session, state)'), r.stderr)

    def test_the_board_does_not_start_on_a_bad_value(self):
        self.config('[layout]\ngroup_by = "nope"\n'); t = Term(self.env, w=W, h=H, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertEqual(t.exit_status(8), 1, t.text()); self.assertIn('layout.group_by: "nope" is not a grouping', t.text())

    def test_g_is_an_action_in_the_keys_table_and_readme_documents_it(self):
        out = plain_config(self.cli("config", text="").stdout); self.assertRegex(out, r'(?m)^group = \["g"\]$')
        readme = (SRC / "README.md").read_text(); self.assertIn("group_by", readme); self.assertIn("`g`", readme)
        for m in MODES: self.assertIn(m, readme)


class Modes(Base):
    def test_default_is_cwd(self):
        t = self.board(""); self.expect(t, "cwd")

    def test_the_footer_says_the_grouping_right_after_f_fold(self):
        """(the key line is ~160 cells: it is cut at the 80-wide outline board, so these boards are 220 wide)"""
        t = self.board("", w=220, h=30); bar = self.bar(t); self.assertIn("f fold · g group:cwd · p preview", bar)
        for m in MODES[1:] + ["cwd"]:
            t.keys("g"); self.assertTrue(wait(lambda: f"f fold · g group:{m} · p preview" in self.bar(t), 3), f"{m}: {self.bar(t)}")

    def test_start_mode_from_config(self):
        for m in MODES[1:]:
            with self.subTest(m=m):
                t = self.board(f'[layout]\ngroup_by = "{m}"\n'); self.expect(t, m); t.close()
                t = self.board(f'[layout]\ngroup_by = "{m}"\n', w=220, h=30); self.assertIn(f"g group:{m}", self.bar(t)); t.close()

    def test_g_goes_around_in_the_documented_order_and_the_footer_follows(self):
        t = self.board(""); self.expect(t, "cwd")
        for m in MODES[1:] + ["cwd"]:
            t.keys("g"); self.expect(t, m)
        t.keys("g"); t.keys("g"); t.keys("g"); self.expect(t, "state")  # three more: project, session, state

    def test_g_from_a_configured_start_continues_the_cycle(self):
        t = self.board('[layout]\ngroup_by = "session"\n'); t.keys("g"); self.expect(t, "state"); t.keys("g"); self.expect(t, "cwd")

    def test_the_fixture_on_its_own_terms(self):
        """not through the oracle: 3 worktree cwds merge into shopcart (4 cards); tmux sessions alpha / beta / `gamma sess` + (no pane) last; the four states in their fixed order"""
        t = self.board(""); fx = self.fx
        for m in MODES:
            if m != "cwd": t.keys("g")
            want = want_outline([(k, fx["groups"][m][k]) for k in fx["order"][m]])
            self.assertTrue(wait(lambda: outline_of(t.snap()) == want, 6), f"{m}:\nwant {want}\ngot  {outline_of(t.snap())}")
        self.assertEqual(fx["order"]["state"], STATE_ORDER)

    def test_the_top_bar_count_does_not_change(self):
        t = self.board(""); top = t.snap()[0].text.strip()
        for m in MODES[1:]: t.keys("g"); self.expect(t, m); self.assertEqual(t.snap()[0].text.strip(), top)

    def test_project_names_the_group_and_an_empty_project_is_no_project(self):
        self.edit(lambda S: S[-1].update(project="")); t = self.board('[layout]\ngroup_by = "project"\n'); self.expect(t, "project"); self.assertIn("▾ (no project) (1)", self.outline(t))

    def test_session_name_is_everything_before_the_first_hash_separator(self):
        def f(S): S[0]["tmuxTarget"] = "my # odd # name # win:1.0"; S[1]["tmuxTarget"] = "Zeta # w:1.0"
        self.edit(f); t = self.board('[layout]\ngroup_by = "session"\n'); self.expect(t, "session")
        o = self.outline(t); self.assertIn("▾ my (1)", o); self.assertIn("▾ Zeta (1)", o); self.assertEqual(o[-2:], ["▾ (no pane) (1)", "solo-bg"])

    def test_no_pane_is_last_whatever_the_names(self):
        self.edit(lambda S: S[0].update(tmuxTarget="zzz # w:0.0")); t = self.board('[layout]\ngroup_by = "session"\n'); self.expect(t, "session"); o = self.outline(t); self.assertEqual(o.index("▾ (no pane) (1)"), len(o) - 2)
        self.assertLess(o.index("▾ zzz (1)"), o.index("▾ (no pane) (1)"))

    def test_state_has_only_the_groups_that_exist_in_the_fixed_order(self):
        def f(S):
            for s in S:
                if s["summary"] in ("shop-hf", "shop-hf2"): s.update(status="idle", attentionState=None)  # no waiting, no done
        self.edit(f); t = self.board('[layout]\ngroup_by = "state"\n'); self.expect(t, "state"); self.assertEqual([x for x in self.outline(t) if x.startswith("▾")], ["▾ working (3)", "▾ waiting (1)", "▾ idle (4)"])

    def test_state_edge_cases(self):
        """a done card is idle with attentionState set (read or unread); a card working only because of a subagent is working; a waiting one is waiting"""
        def f(S):
            by = {s["summary"]: s for s in S}
            by["shop-feat"].update(status="idle", attentionState="read")
            by["solo-bg"].update(status="idle", subagents=[{"agentId": "z", "status": "working", "pendingTool": None}])
        self.edit(f); t = self.board('[layout]\ngroup_by = "state"\n'); self.expect(t, "state"); o = self.outline(t)
        self.assertEqual(o[o.index("▾ done (2)") + 1:o.index("▾ done (2)") + 3], ["shop-feat", "shop-hf2"]); self.assertIn("solo-bg", o[o.index("▾ working (4)") + 1:o.index("▾ working (4)") + 5])


class Cards(Base):
    def test_cards_stay_in_name_order_inside_every_group(self):
        t = self.board("")
        for m in MODES:
            if m != "cwd": t.keys("g")
            self.expect(t, m)

    def test_state_cards_keep_their_own_status_line(self):
        t = self.board('[layout]\ngroup_by = "state"\n'); self.expect(t, "state"); snap = t.snap()
        for c in find_cards(snap):
            name = re.match(r"\s*(\S+)", c.line(1)[8:]).group(1); st = state_of(next(s for s in self.sessions() if s["summary"] == name))
            word = {"working": "working", "waiting": "waiting", "done": "done", "idle": "idle"}[st]; self.assertIn(word, c.line(3), f"{name}: {c.line(3)!r}")

    def test_a_poll_that_changes_a_state_moves_the_card_live(self):
        t = self.board('[layout]\ngroup_by = "state"\n'); self.expect(t, "state")
        def f(S):
            by = {s["summary"]: s for s in S}; by["sql-a"].update(status="waiting"); by["shop-hf"].update(status="idle", attentionState=None); by["shop-feat"].update(status="working")
        self.edit(f); self.expect(t, "state")

    def test_a_poll_that_moves_a_session_changes_its_group_in_session_and_project_modes(self):
        t = self.board('[layout]\ngroup_by = "session"\n'); self.expect(t, "session")
        self.edit(lambda S: S[0].update(tmuxTarget="delta # w:0.0", tmuxPane="%999")); self.expect(t, "session")
        self.edit(lambda S: S[-1].update(tmuxTarget="alpha # main:9.9", tmuxPane="%998", trackingMode="native")); self.expect(t, "session")
        t.keys("g"); self.expect(t, "state"); t.keys("g"); self.expect(t, "cwd"); self.edit(lambda S: S[2].update(project="newproj")); t.keys("g"); self.expect(t, "project")
        self.assertIn("▾ newproj (1)", self.outline(t))


class Folding(Base):
    def test_g_opens_every_group_and_folds_do_not_come_back_by_cycling(self):
        t = self.board(""); t.keys("f"); time.sleep(0.4); o = self.outline(t); self.assertEqual(sum(x.startswith("▸") for x in o), 1, o)
        for m in MODES[1:] + ["cwd"]:
            t.keys("g"); self.expect(t, m); self.assertEqual(sum(x.startswith("▸") for x in self.outline(t)), 0, f"{m}: every group open")
        t.keys("f"); time.sleep(0.4); self.assertEqual(sum(x.startswith("▸") for x in self.outline(t)), 1)
        t.keys("g"); self.expect(t, "project"); t.keys("f"); time.sleep(0.4); self.assertEqual(sum(x.startswith("▸") for x in self.outline(t)), 1)
        t.keys("g"); t.keys("g"); t.keys("g"); self.expect(t, "cwd")

    def test_folding_in_one_mode_does_not_fold_the_same_title_in_another(self):
        """the same word as a title in two modes: a project and a tmux session both called `alpha`"""
        def f(S):
            for s in S:
                if s["summary"] == "sql-b": s.update(project="alpha")
        self.edit(f); t = self.board('[layout]\ngroup_by = "project"\n'); self.assertIn("▾ alpha (1)", self.outline(t))
        t.keys("f"); time.sleep(0.4)  # (focus starts on the first card: the shopcart group)
        t.keys("g"); self.expect(t, "session"); self.assertEqual(sum(x.startswith("▸") for x in self.outline(t)), 0)

    def test_the_focus_stays_on_its_card_and_the_screen_is_not_scrolled_away(self):
        t = self.board(""); t.keys("j"); t.keys("j"); t.keys("j"); time.sleep(0.4); before = self.focused(t); self.assertEqual(len(before), 1)
        for m in MODES[1:] + ["cwd"]:
            t.keys("g"); self.expect(t, m); self.assertEqual(self.focused(t), before, f"{m}: the same card has the focus")


class FoldedTitle(Base):
    def test_g_on_a_folded_group_puts_the_focus_on_the_first_card_of_that_group(self):
        """focus a card of the 3rd group (sqlmux), fold it (the focus goes to its title), g: the focus is its first card (sql-a), not the first card of the board"""
        t = self.board("")
        for _ in range(12):
            f = self.focused(t)
            if f and "sql-" in f[0]: break
            t.keys("j"); time.sleep(0.25)
        self.assertTrue(self.focused(t) and "sql-" in self.focused(t)[0], t.text()); t.keys("f"); time.sleep(0.5)
        self.assertEqual(self.focused(t), [], "the card is folded away: the title has the focus"); t.keys("g"); self.expect(t, "project")
        f = self.focused(t); self.assertEqual(len(f), 1, t.text()); self.assertIn("sql-a", f[0], f"the group's first card, not the board's first (shop-feat): {f}")

    def test_g_on_the_first_group_folded_also_lands_on_its_first_card(self):
        t = self.board(""); t.keys("f"); time.sleep(0.5); t.keys("g"); self.expect(t, "project"); self.assertIn("shop-main", self.focused(t)[0])


class Search(Base):
    def lit(self, row): return "".join(c[0] for c in row.cells if c[2] == MATCH and c[0].strip())

    def titles(self, t): return [r for r in t.snap() if r.text.lstrip().startswith(("▾", "▸"))]

    def test_a_name_title_draws_no_path_hit_but_the_path_title_still_does(self):
        t = self.board(""); t.keys("/"); time.sleep(0.2); t.lit("shopcart.worktrees"); time.sleep(0.6)
        lit = [self.lit(r) for r in self.titles(t)]; self.assertTrue(any("shopcart.worktrees" in x for x in lit), f"control: the cwd title is lit: {lit}")
        t.keys("Enter"); time.sleep(0.4); t.keys("g"); time.sleep(0.6); self.assertTrue(wait(lambda: any("▾ shopcart" in r.text for r in self.titles(t)), 5), t.text())
        self.assertEqual([self.lit(r) for r in self.titles(t)], [""] * len(self.titles(t)), "project titles are names: no path hit drawn")
        self.assertEqual(sorted(o for o in self.outline(t) if not o.startswith("▾")), ["shop-feat", "shop-hf", "shop-hf2"], "the filter still matches the same cards")

    def test_a_kept_filter_survives_g_and_shows_only_its_cards_in_the_new_groups(self):
        t = self.board(""); t.keys("/"); time.sleep(0.2); t.lit("sql"); time.sleep(0.5); t.keys("Enter"); time.sleep(0.4); self.assertEqual(sorted(x for x in self.outline(t) if not x.startswith("▾")), ["sql-a", "sql-b", "sql-c"])
        t.keys("g"); time.sleep(0.6); o = self.outline(t); self.assertEqual(o, ["▾ sqlmux (3)", "sql-a", "sql-b", "sql-c"]); self.assertRegex(t.snap()[0].text, r"3/8")
        t.keys("g"); time.sleep(0.6); self.assertEqual(self.outline(t), ["▾ alpha (1)", "sql-b", "▾ gamma sess (2)", "sql-a", "sql-c"])

    def test_g_typed_in_the_search_box_is_text_not_the_action(self):
        t = self.board(""); t.keys("/"); time.sleep(0.2); t.lit("g"); time.sleep(0.5); self.assertIn("/ g▏", t.snap()[0].text); t.esc(); time.sleep(0.4)
        self.expect(t, "cwd")

    def test_flash_reads_the_shown_title(self):
        t = self.board('[layout]\ngroup_by = "project"\n'); t.keys("s"); time.sleep(0.3); t.lit("sqlmux"); time.sleep(0.6)
        hits = [self.lit(r) for r in self.titles(t)]; self.assertIn("sqlmux", hits, f"the project title is the hit: {hits}")
        t.esc(); t.keys("g"); t.keys("g"); time.sleep(0.5); t.keys("s"); time.sleep(0.3); t.lit("wait"); time.sleep(0.6); hits = [self.lit(r) for r in self.titles(t)]; self.assertIn("wait", hits, f"state title: {hits}")


class Flow(Base):
    def test_both_flows_lay_out_the_groups_in_every_mode(self):
        n = (130 + 1) // 41
        for flow in ("bands", "stack"):
            t = self.board(f'[layout]\nflow = "{flow}"\n', w=130, h=60)
            for m in MODES:
                if m != "cwd": t.keys("g")
                groups = oracle(self.sessions(), m, self.home); titles = [k for k, _ in groups]
                def cols():
                    out = {}
                    for r in t.snap():
                        for mm in re.finditer(r"[▾▸] (.+?) \((\d+)\)", r.text): out[mm.group(1)] = (mm.start(), int(mm.group(2)))
                    return out
                if m == "cwd": self.assertTrue(wait(lambda: len(cols()) == len(groups), 6), f"{flow}/cwd: {cols()}"); continue  # (cwd titles are cut in a 43-wide column)
                self.assertTrue(wait(lambda: {k: c[1] for k, c in cols().items()} == {k: len(v) for k, v in groups}, 6), f"{flow}/{m}: {cols()}\n{t.text()}")
                xs = sorted({c[0] for c in cols().values()}); self.assertLessEqual(len(xs), n)
                self.assertEqual([xs.index(cols()[k][0]) for k in titles], [k % n for k in range(len(titles))], f"{flow}/{m}: group k in column k mod {n}")
            t.close()


class Passthrough(PreviewBase):
    """with the keys on the preview's pane `g` is a character for the pane (a3 runs a raw cat -v), not the action; after tab back it cycles"""
    def test_g_goes_to_the_pane_in_the_preview_and_cycles_on_the_cards(self):
        self.press("j", "j", "p"); self.assertTrue(wait(lambda: self.pv() is not None, 4), self.t.text()); self.press("Tab")
        self.assertTrue(wait(lambda: "keys go to the pane" in last_text_row(self.t.snap()), 3)); self.t.lit("g"); time.sleep(0.5)
        self.assertIn("g", pane_text(self.panes["a3"], 5), "the pane got the g"); bar = last_text_row(self.t.snap()); self.assertIn("keys go to the pane", bar)
        self.press("Tab"); self.assertTrue(wait(lambda: "g group:cwd" in last_text_row(self.t.snap()) or "tab focus preview" in last_text_row(self.t.snap()), 3))
        self.assertTrue(any("▾ ~/work/alpha (3)" in r.text for r in self.t.snap()), "still cwd groups")
        self.t.keys("g"); self.assertTrue(wait(lambda: any("▾ alpha (2)" in r.text for r in self.t.snap()), 4), "g on the cards: project groups (the fixture's project names)\n" + self.t.text())


class Keys(Base):
    def test_group_follows_the_t17_key_rules(self):
        """an explicit binding takes the key from a default action (which then has none); two explicit bindings of one key are an error"""
        r = self.cli("config", text='[keys]\ngroup = ["x"]\n'); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn('group = ["x"]', r.stdout); self.assertIn("# kill: no key (another action took its default)", r.stdout)
        r = self.cli("config", text='[keys]\ngroup = ["G"]\nkill = ["G"]\n'); self.assertEqual(r.returncode, 1); self.assertIn('"G" is already bound to group', r.stderr); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)

    def test_a_rebound_key_cycles_and_the_footer_shows_it(self):
        t = self.board('[keys]\ngroup = ["G"]\n', w=220, h=30); self.assertIn("f fold · G group:cwd · p preview", self.bar(t)); t.keys("g"); time.sleep(0.5); self.assertIn("G group:cwd", self.bar(t), "g no longer cycles")
        t.keys("G"); self.assertTrue(wait(lambda: "G group:project" in self.bar(t), 3), self.bar(t))


if __name__ == "__main__":
    unittest.main()
