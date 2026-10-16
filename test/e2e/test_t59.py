"""T59 a background session (ccmux trackingMode == "background") says so: a muted `bg` at the right edge of its card's second line (`<agent> bg` when the transcript names an agent; a long agent is cut to leave `bg`
whole, a narrow card stays inside its frame); the preview title is `name · bg · status` (`name · bg · %id · status` when it has a pane); no pane: the message line says `background session · enter or i attaches it` (a card
that is not a background session and has no pane: `no tmux pane: can't jump or send`); `agmux show` has `bg` (or nothing) as its 8th column. Spec: specs/m3-config/tasks.md「第十一批」T59. Fake ccmux; the fixture's b2
(bravo-bg) is the background session."""
import json, re, subprocess, unittest
from lib import *
import test_t37 as T37
from test_t3 import ready, last_text_row
from test_t11 import PreviewBase

MUTED = (98, 114, 164)
AGENT = ("rgb", 189, 147, 249)
SETTING = T37.SETTING


def near(c, ref, d=4): return c is not None and all(abs(x - y) <= d for x, y in zip(c, ref))


class Base(T37.Base):
    def edit_show(self, fn):
        S = json.loads((self.env.fake / "show.json").read_text()); fn(S); self.env.write_show(S)

    def tag(self, t, name="bravo-bg"):
        """(text at the right edge of line 2 inside the frame, the cells of it)"""
        c = self.card(t, name); row = c.rows[2]; inner = row.cells[1:c.w - 1]; txt = "".join(x[0] for x in inner); return txt, inner


class Tag(Base):
    def test_a_background_card_has_a_muted_bg_at_the_right_edge_of_line_2(self):
        t = self.board(); txt, cells = self.tag(t); self.assertRegex(txt.rstrip(), r"\bmain\s+bg$", txt); self.assertTrue(txt.endswith("bg"), "touching the right border (no margin since T39)")
        self.assertTrue(all(near(rgb(c[1]), MUTED) for c in cells[-2:]), [(c[0], c[1]) for c in cells[-2:]])
        for other in ("alpha-main", "bravo-29", "charlie-30", "delta-60", "echo-long"): self.assertFalse(self.tag(t, other)[0].rstrip().endswith("bg"), other)

    def test_with_an_agent_it_is_the_agent_then_bg(self):
        self.log("b2", SETTING("orchestrator")); t = self.board(); txt, cells = self.tag(t); self.assertRegex(txt, r"orchestrator bg$", txt)
        i = txt.index("orchestrator"); self.assertEqual({cells[i + k][1] for k in range(12)}, {AGENT}, "the agent keeps its colour"); self.assertTrue(all(near(rgb(c[1]), MUTED) for c in cells[-2:]), "bg is muted")
        self.assertEqual(txt[i + 12], " ")

    def test_the_branch_gives_way_before_bg_does(self):
        self.log("b2", SETTING("orchestrator")); self.edit_show(lambda S: [s.update(gitBranch="feature/a-long-branch-name-that-does-not-fit-next-to-the-agent") for s in S if s["id"] == "b2"])
        t = self.board(w=100, h=40); txt, cells = self.tag(t); self.assertTrue(txt.endswith("orchestrator bg"), txt); self.assertIn("…", txt, "the branch was cut, not the tag")

    def test_a_long_agent_is_cut_and_bg_stays_whole_at_every_width(self):
        self.log("b2", SETTING("an-agent-definition-name-that-is-much-too-long-for-any-card-we-have-here")); self.edit_show(lambda S: [s.update(gitBranch=None) for s in S if s["id"] == "b2"])
        for w in (200, 100, 60, 44):
            with self.subTest(width=w):
                t = self.board(w=w, h=60); c = self.card(t, "bravo-bg"); txt, cells = self.tag(t); self.assertTrue(txt.endswith("… bg") or txt.endswith(" bg"), f"{w}: {txt!r}")
                self.assertIn(c.rows[2].cells[0][0], "│┃"); self.assertIn(c.rows[2].cells[c.w - 1][0], "│┃", "the frame is intact"); self.assertEqual(len(c.rows[2].cells), c.w, "no wider than the card"); self.assertTrue(all(near(rgb(x[1]), MUTED) for x in cells[-2:]))
                t.close()

    def test_a_background_card_with_no_branch_has_just_the_tag(self):
        self.edit_show(lambda S: [s.update(gitBranch=None) for s in S if s["id"] == "b2"]); t = self.board(); txt, cells = self.tag(t); self.assertRegex(txt, r"^[▀▄█ ]{6}\s+bg$", txt)

    def test_the_card_count_and_layout_are_otherwise_as_before(self):
        t = self.board(); cs = [c for c in find_cards(t.snap()) if c.ok]; self.assertEqual(len(cs), 9); self.assertTrue(all(len(c.rows) == 5 for c in cs))


class ShowColumn(Base):
    def rows(self):
        out = subprocess.run([BIN, "show"], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10).stdout
        return {l.split("\t")[0]: l.split("\t") for l in out.splitlines() if l}

    def test_agmux_show_has_bg_as_the_eighth_column_for_a_background_session_only(self):
        rows = self.rows(); self.assertTrue(all(len(r) == 8 for r in rows.values()), {k: len(v) for k, v in rows.items()}); self.assertEqual(rows["b2"][7], "bg")
        self.assertEqual([k for k, r in rows.items() if r[7]], ["b2"], "no other row has it"); self.assertEqual(rows["b1"][7], "")

    def test_the_other_columns_are_where_they_were(self):
        self.log("b2", SETTING("orchestrator")); r = self.rows()["b2"]; self.assertEqual((r[0], r[1], r[3], r[6]), ("b2", "bravo-bg", "idle", "orchestrator")); self.assertEqual(self.rows()["a1"][5], "41%")


class Texts(Base):
    """the message line: it names the way out of a card that has no pane"""
    def msg(self, t): return [r.text.strip() for r in t.snap()[-3:-1]][0]

    def goto(self, t, name):
        for k in ("l", "j", "l", "j", "h", "k") * 2:
            if any(C_FOCUS_IN(c) and name in c.line(1) for c in find_cards(t.snap())): return
            t.keys(k); time.sleep(0.25)
        self.fail(f"could not focus {name}\n{t.text()}")

    def test_background_card_without_a_pane_says_enter_or_i_attaches_it(self):
        t = self.board(); t.keys("l"); t.keys("j"); self.assertTrue(wait(lambda: "background session · enter or i attaches it" in self.msg(t), 3), t.text()); self.assertNotIn("no tmux pane", t.text())

    def test_a_native_card_with_no_pane_says_so_and_a_card_with_a_pane_says_nothing(self):
        self.edit_show(lambda S: [s.update(tmuxPane=None) for s in S if s["id"] == "c1"]); t = self.board(); self.goto(t, "charlie-30")
        self.assertTrue(wait(lambda: self.msg(t) == "no tmux pane: can't jump or send", 3), f"{self.msg(t)!r}\n{t.text()}"); t.keys("h"); time.sleep(0.6); self.assertTrue(any(C_FOCUS_IN(c) and "bravo-29" in c.line(1) for c in find_cards(t.snap())), t.text()); self.assertEqual(self.msg(t), "", "bravo-29 has a pane: nothing is said")
        self.assertNotIn("attaches", t.text())

    def test_enter_on_a_native_card_without_a_pane_does_not_try_to_attach(self):
        self.edit_show(lambda S: [s.update(tmuxPane=None) for s in S if s["id"] == "c1"]); t = self.board(); self.goto(t, "charlie-30"); t.keys("Enter"); time.sleep(0.8)
        self.assertTrue(t.alive()); self.assertEqual([c[0] for c in self.env.tmux_calls() if c[0] in ("new-window", "new-session", "has-session")], [], "only background sessions get a window")


def C_FOCUS_IN(c):
    from test_t3 import C_FOCUS, border_colours
    return C_FOCUS in border_colours(c)


class Preview(PreviewBase):
    """real tmux panes: the preview's title and the text in the box"""
    def go_bg(self):
        self.press("l", "j")  # b2

    def test_title_is_name_bg_status_and_the_box_says_how_to_attach(self):
        self.press("p"); self.assertTrue(wait(lambda: self.pv() is not None, 4)); self.go_bg(); time.sleep(0.8)
        self.assertTrue(wait(lambda: self.pv()[0] == "bravo-bg · bg · ⏸ idle", 4), self.pv()[0]); title, rows, top, x, snap = self.pv(); self.assertEqual(title, "bravo-bg · bg · ⏸ idle", "no pane: no pane id in the title"); self.assertIn("background session · enter or i attaches it", "\n".join(r.text for r in rows))
        self.assertNotIn("no pane", "\n".join(r.text for r in rows))

    def test_p_alone_does_not_attach_it(self):
        self.press("p"); self.go_bg(); time.sleep(1.5); self.assertEqual(tm("list-sessions", "-F", "#{session_name}").stdout.split().count("agmux-bg"), 0, "no agmux-bg session was made")

    def test_with_a_pane_the_title_has_bg_and_the_pane_id(self):
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            if s["id"] == "b2": s["tmuxPane"] = self.panes["a2"]  # still trackingMode background
        self.env.write_show(S); self.press("p"); self.assertTrue(wait(lambda: self.pv() is not None, 4)); self.go_bg(); self.assertTrue(wait(lambda: self.pv()[0] == f"bravo-bg · bg · {self.panes['a2']} · ⏸ idle", 5), self.pv()[0]); time.sleep(0.8)
        self.assertEqual(self.pv()[0], f"bravo-bg · bg · {self.panes['a2']} · ⏸ idle"); self.assertIn("second-pane-marker", "\n".join(r.text for r in self.pv()[1]), "its pane is shown like any other")

    def test_a_native_card_has_no_bg_in_its_title(self):
        self.press("p"); self.assertTrue(wait(lambda: self.pv() is not None, 4)); time.sleep(0.8); self.assertRegex(self.pv()[0], rf"^alpha-main · {re.escape(self.panes['a1'])} · ⏵ working · Bash$")


if __name__ == "__main__":
    unittest.main()
