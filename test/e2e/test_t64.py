"""T64 under the session grouping a background session goes with the foreground sessions of the same directory: its group key is the tmux session of the foreground sessions (those with a pane) in the same cwd, else (same
mainRepoRoot: a worktree of the same project); with several tmux sessions to choose from the one with the most such foreground sessions, then the one with the most recent activity, then the name; none = as before (its own
pane's session, `(no pane)` without one). The key is the same before and after the attach (the pane in agmux-bg does not move the card). Only the session grouping changes; the `bg` tag does not. Spec: specs/m3-config/tasks.md
「第十三批」T64. Fake ccmux / fake tmux (the harness' one, with test_t60's patch: new-window answers `%777`, list-panes is read from a file)."""
import datetime, json, re, unittest
from lib import *
from test_t3 import C_FOCUS, border_colours
from test_t43 import outline_of, want_outline
from test_t60 import patch_tmux

NOW = datetime.datetime.now(datetime.timezone.utc)


def ago(s): return iso(NOW - datetime.timedelta(seconds=s))


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True); patch_tmux(self.env); self.home = str(self.env.home); self.n = 0; self.S = []

    def add(self, name, cwd, target=None, bg=False, root=None, active=60, sid=None, **kw):
        """a session: foreground (a pane, in tmux session `target`) or background (no pane); cwd under ~/work/"""
        self.n += 1; sid = sid or f"s{self.n:02d}"; path = f"{self.home}/work/{cwd}"
        s = dict(id=sid, summary=name, agentType="claude", cwd=path, project=cwd.split("/")[0], mainRepoRoot=f"{self.home}/work/{root}" if root else path, isWorktree=bool(root), worktreeRoot=None, gitBranch="main", status="idle", attentionType=None, pendingTool=None,
                 tmuxPane=None if bg else f"%{500 + self.n}", tmuxTarget=None if bg else f"{target} # main:0.0", trackingMode="background" if bg else "native", subagents=[], nativeSessionId="n-" + sid, logPath=None,
                 lastActivityAt=ago(active), updatedAt=iso(NOW), backgroundDetail=None, lastPrompt="a prompt" if bg else None, prompts=["a prompt"] if bg else [], attentionState=None)
        s.update(kw); self.S.append(s); return s

    def write(self): self.env.write_show(self.S)

    def board(self, group_by="session", w=80, h=70):
        self.write(); (self.cfg / "agmux" / "config.toml").write_text(f'[layout]\nmax_columns = 1\ngroup_by = "{group_by}"\n')
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= len(self.S), 10), t.text()); time.sleep(0.5); return t

    def groups(self, t):
        """{group title: [card names]} of the screen"""
        out, cur = {}, None
        for l in outline_of(t.snap()):
            m = re.match(r"[▾▸] (.+) \((\d+)\)$", l)
            if m: cur = m.group(1); out[cur] = []
            elif cur is not None: out[cur].append(l)
        return {k: sorted(v) for k, v in out.items()}

    def settle(self, t, want, secs=8):
        def same():
            try: return self.groups(t) == want
            except (IndexError, AttributeError): return False  # a capture torn by a repaint
        self.assertTrue(wait(same, secs), f"want {want}\ngot  {self.groups(t)}\n{t.text()}")

    def focus(self, t, name):
        """walk the columns from the top left (j down each, then back up and one column right) until the card has the focus"""
        for k in ["h"] * 4 + ["k"] * 10: t.keys(k); time.sleep(0.06)
        for col in range(4):
            for row in range(10):
                if any(C_FOCUS in border_colours(c) and name in c.line(1) for c in find_cards(t.snap())): return
                t.keys("j"); time.sleep(0.15)
            for k in ["k"] * 10 + ["l"]: t.keys(k); time.sleep(0.06)
        self.fail(f"could not focus {name}\n{t.text()}")


class Follow(Base):
    def test_a_background_session_is_in_the_group_of_the_foreground_sessions_of_its_directory(self):
        self.add("fg-one", "proj", "blessmuse"); self.add("fg-two", "proj", "blessmuse"); self.add("bg-x", "proj", bg=True, sid="bgx"); self.add("elsewhere", "other", "dev"); t = self.board()
        self.settle(t, {"blessmuse": ["bg-x", "fg-one", "fg-two"], "dev": ["elsewhere"]}); self.assertNotIn("(no pane)", self.groups(t)); self.assertNotIn("agmux-bg", self.groups(t))
        self.assertTrue(any("bg-x" in c.line(1) and c.rows[2].text.rstrip().rstrip("│┃").rstrip().endswith("bg") for c in find_cards(t.snap())), "the bg tag is still on the card")

    def test_it_stays_when_it_is_attached_and_its_pane_is_in_agmux_bg(self):
        self.add("fg-one", "proj", "blessmuse"); self.add("fg-two", "proj", "blessmuse"); self.add("bg-x", "proj", bg=True, sid="bgx"); t = self.board(); want = {"blessmuse": ["bg-x", "fg-one", "fg-two"]}; self.settle(t, want); self.focus(t, "bg-x")
        t.keys("i"); self.assertTrue(wait(lambda: any(c[0] == "new-window" for c in self.env.tmux_calls()), 5), self.env.tmux_calls()); time.sleep(0.8); self.settle(t, want, 3)  # the overlay: TmuxPane = %777 in agmux-bg
        (self.env.fake / "tmux.panes").write_text("%777\tagmux-bg\t0\tclaude attach bgx\n"); time.sleep(5.0); self.assertEqual(self.groups(t), want, "and when the listing has it too: no agmux-bg group, no move")

    def test_a_background_session_with_no_foreground_in_its_directory_keeps_the_old_behaviour(self):
        # a background session whose directory has no foreground session keeps the old behaviour: no pane -> (no pane), a pane -> the pane's session
        self.add("fg-one", "proj", "blessmuse"); self.add("lonely", "solo", bg=True, sid="lone"); t = self.board(); self.settle(t, {"blessmuse": ["fg-one"], "(no pane)": ["lonely"]}); self.focus(t, "lonely"); t.keys("i"); self.assertTrue(wait(lambda: any(c[0] == "new-window" for c in self.env.tmux_calls()), 5))
        (self.env.fake / "tmux.panes").write_text("%777\tagmux-bg\t0\tclaude attach lone\n"); self.settle(t, {"blessmuse": ["fg-one"], "agmux-bg": ["lonely"]}, 10)

    def test_the_directory_must_be_the_same_cwd_not_a_parent_or_a_sibling(self):
        self.add("fg-one", "proj", "blessmuse"); self.add("sub", "proj/sub", bg=True, sid="b1"); self.add("sib", "proj2", bg=True, sid="b2"); t = self.board()
        self.settle(t, {"blessmuse": ["fg-one"], "(no pane)": ["sib", "sub"]})

    def test_only_foreground_sessions_with_a_pane_count_and_other_background_sessions_do_not(self):
        self.add("bg-a", "proj", bg=True, sid="ba"); self.add("bg-b", "proj", bg=True, sid="bb"); self.add("gone", "proj", "blessmuse", tmuxPane=None, tmuxTarget=None, trackingMode="native"); t = self.board()
        self.settle(t, {"(no pane)": ["bg-a", "bg-b", "gone"]})


class Choice(Base):
    def test_the_tmux_session_with_more_foreground_sessions_of_the_directory_wins(self):
        self.add("a1", "proj", "alpha"); self.add("a2", "proj", "alpha"); self.add("b1", "proj", "beta", active=1); self.add("bg-x", "proj", bg=True, sid="bgx"); t = self.board()
        self.settle(t, {"alpha": ["a1", "a2", "bg-x"], "beta": ["b1"]}, 10)

    def test_equal_numbers_go_to_the_session_with_the_most_recent_activity(self):
        self.add("a1", "proj", "alpha", active=300); self.add("b1", "proj", "beta", active=5); self.add("bg-x", "proj", bg=True, sid="bgx"); t = self.board(); self.settle(t, {"alpha": ["a1"], "beta": ["b1", "bg-x"]}, 10)
        self.S[0]["lastActivityAt"] = ago(1); self.S[1]["lastActivityAt"] = ago(600); self.write(); self.settle(t, {"alpha": ["a1", "bg-x"], "beta": ["b1"]}, 10)

    def test_the_most_recent_foreground_of_a_session_counts_not_the_oldest(self):
        self.add("a1", "proj", "alpha", active=900); self.add("a2", "proj", "alpha", active=3); self.add("b1", "proj", "beta", active=100); self.add("b2", "proj", "beta", active=200); self.add("bg-x", "proj", bg=True, sid="bgx"); t = self.board()
        self.settle(t, {"alpha": ["a1", "a2", "bg-x"], "beta": ["b1", "b2"]}, 10)

    def test_a_full_tie_is_decided_the_same_way_every_poll(self):
        self.add("a1", "proj", "alpha", active=30, lastActivityAt=ago(30)); self.add("b1", "proj", "beta", active=30); self.S[1]["lastActivityAt"] = self.S[0]["lastActivityAt"]; self.add("bg-x", "proj", bg=True, sid="bgx"); t = self.board(); seen = set()
        for _ in range(6):
            g = self.groups(t); seen.add(tuple((k, tuple(v)) for k, v in sorted(g.items()))); time.sleep(1.2)
        self.assertEqual(len(seen), 1, f"it did not jump about: {seen}"); self.assertEqual([k for k, v in self.groups(t).items() if "bg-x" in v], ["alpha"], "the name decides: alpha before beta")

    def test_a_cwd_match_beats_a_bigger_group_of_the_same_project(self):
        # bg in ~/work/proj; one foreground in the same directory (alpha), two worktrees of the project (beta): the same cwd wins
        self.add("same", "proj", "alpha"); self.add("wt1", "proj-wt1", "beta", root="proj"); self.add("wt2", "proj-wt2", "beta", root="proj"); self.add("bg-x", "proj", bg=True, sid="bgx"); t = self.board()
        self.settle(t, {"alpha": ["bg-x", "same"], "beta": ["wt1", "wt2"]}, 10)

    def test_the_same_main_repo_root_counts_when_no_foreground_has_the_cwd(self):
        self.add("wt1", "proj-wt1", "beta", root="proj"); self.add("bg-x", "proj", bg=True, sid="bgx", mainRepoRoot=f"{self.home}/work/proj"); self.add("unrelated", "zzz", "alpha"); t = self.board(); self.settle(t, {"alpha": ["unrelated"], "beta": ["bg-x", "wt1"]}, 10)

    def test_a_worktree_background_session_follows_a_foreground_session_of_its_main_repo(self):
        self.add("main-fg", "proj", "alpha"); self.add("bg-wt", "proj-wt1", bg=True, sid="bgw", root="proj"); self.add("unrelated", "zzz", "beta"); t = self.board(); self.settle(t, {"alpha": ["bg-wt", "main-fg"], "beta": ["unrelated"]}, 10)


class OtherGroupings(Base):
    def test_cwd_project_and_state_group_the_background_session_as_before(self):
        self.add("fg-one", "proj", "blessmuse"); self.add("bg-x", "proj", bg=True, sid="bgx", status="working"); self.add("other", "elsewhere", "dev", status="waiting")
        for mode, want in (("cwd", {"~/work/elsewhere": ["other"], "~/work/proj": ["bg-x", "fg-one"]}), ("project", {"elsewhere": ["other"], "proj": ["bg-x", "fg-one"]}), ("state", {"working": ["bg-x"], "waiting": ["other"], "idle": ["fg-one"]})):
            with self.subTest(mode=mode):
                t = self.board(mode); self.settle(t, want); t.close()

    def test_g_around_the_modes_and_back_to_session_keeps_it_with_the_foreground(self):
        self.add("fg-one", "proj", "blessmuse"); self.add("bg-x", "proj", bg=True, sid="bgx"); t = self.board("cwd"); self.settle(t, {"~/work/proj": ["bg-x", "fg-one"]})
        t.keys("g"); time.sleep(0.4); t.keys("g"); self.settle(t, {"blessmuse": ["bg-x", "fg-one"]}); t.keys("g"); time.sleep(0.4); t.keys("g"); self.settle(t, {"~/work/proj": ["bg-x", "fg-one"]})

    def test_foreground_sessions_keep_their_own_key_whatever_a_background_one_is_doing(self):
        self.add("fg-one", "proj", "blessmuse"); self.add("fg-two", "proj", "gamma sess"); self.add("bg-x", "proj", bg=True, sid="bgx"); t = self.board(); g = self.groups(t); self.assertEqual(g["blessmuse"].count("fg-one"), 1); self.assertEqual(g["gamma sess"].count("fg-two"), 1)
        self.assertEqual(sum("bg-x" in v for v in g.values()), 1, "the card is in one group only")


if __name__ == "__main__":
    unittest.main()
