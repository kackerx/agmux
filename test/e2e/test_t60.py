"""T60 a background session (no pane) gets a pane when it is asked for: enter / i / tab / H on such a card first open a window running `claude attach <id>` in the tmux session `agmux-bg` (has-session -t =agmux-bg ->
new-window -t =agmux-bg: ..., else new-session -s agmux-bg -x <preview inner width> -y <preview rows> ...; -d -n <card name> -c <cwd> -P -F '#{pane_id}' -- claude attach <id>), then do what was asked (jump / open the
preview and the pass-through / send /handoff, after capture-pane shows something, 5 s at most); p only previews (the box says how to attach). A card that has a pane, or is not a background session, is not attached.
Which pane belongs to which card is read off `tmux list-panes -a -F '#{pane_id}\\t#{session_name}\\t#{pane_dead}\\t#{pane_start_command}'` at every poll, but only while a background session has no pane: the start command
must be `claude attach <id>` with the id as a whole word (quotes and a path before `claude` are fine), a dead pane is not taken; the card gets TmuxPane / TmuxSession (it is grouped under agmux-bg by session). A pane just
opened is the card's at once, for 10 s, after that the listing decides. The window closing (kill-window, attach exiting) gives the card back without a pane and the next press makes a new one. tmux failing: attach -> one
red line in the footer and the action is not done; list-panes -> silent. Spec: specs/m3-config/tasks.md「第十一批」T60.

Two kinds of test: Shape (the fake tmux of the harness records argv, answers new-window / new-session with `%777`, list-panes from a file) and Real (the private tmux server, a fake `claude` that prints `ATTACHED <id>` and
then echoes keys like cat -v; no real background session is ever started)."""
import json, os, re, subprocess, unittest
from lib import *
import test_t37 as T37
from test_t3 import C_FOCUS, border_colours, last_text_row
from test_t11 import PreviewBase, PV_X, pane_text

HERE = pathlib.Path(__file__).resolve().parent
FAKE_CLAUDE = """#!/bin/sh
d="%(d)s"
printf '%%s\\n' "$*" >> "$d/claude.log"; pwd >> "$d/claude.pwd"
if [ "$1" = attach ]; then
  [ -e "$d/delay" ] && sleep "$(cat "$d/delay")"
  stty raw -echo
  printf 'ATTACHED %%s\\r\\n' "$2"
  [ -e "$d/exit-after" ] && sleep "$(cat "$d/exit-after")" && exit 0
  exec env LANG=en_US.UTF-8 python3 -u "%(catv)s"
fi
"""


def add_bg(env, sid, name, cwd=None, **kw):
    S = json.loads((env.fake / "show.json").read_text()); b2 = next(s for s in S if s["id"] == "b2")
    S.append(dict(b2, id=sid, summary=name, cwd=cwd or b2["cwd"], nativeSessionId="n-" + sid, **kw)); env.write_show(S)


def patch_tmux(env):
    """the harness' fake tmux + new-window / new-session answer a pane id, list-panes / capture-pane answer from files"""
    f = env.bin / "tmux"; s = f.read_text(); d = shlex.quote(str(env.fake))
    ins = f'''case "$1" in
  new-window|new-session) echo %777; exit 0;;
  list-panes) [ -e {d}/tmux.panes ] && cat {d}/tmux.panes; exit 0;;
  capture-pane) [ -e {d}/tmux.capture ] && cat {d}/tmux.capture; exit 0;;
esac
'''
    f.write_text(s.replace('case "$1" in\n  display-message)', ins + 'case "$1" in\n  display-message)', 1))


class Shape(T37.Base):
    ATTACH = ("has-session", "new-window", "new-session")

    def setUp(self):
        super().setUp(); patch_tmux(self.env)

    def calls(self): return self.env.tmux_calls(raw=True)

    def attach_calls(self): return [c for c in self.calls() if c[0] in self.ATTACH]

    def board(self, w=232, h=40, config=None):
        cfg = self.env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True, exist_ok=True)
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 9, 10), t.text()); time.sleep(0.4); return t

    def to_bg(self, t, name="bravo-bg"):
        t.keys("l"); time.sleep(0.3); t.keys("j"); time.sleep(0.4)
        self.assertTrue(any(C_FOCUS in border_colours(c) and name in c.line(1) for c in find_cards(t.snap())), t.text())

    def flag(self, name, text="x"): (self.env.fake / name).write_text(text)

    def row(self, t): return "\n".join(r.text for r in t.snap()[-3:])

    def geometry(self, t):
        """(inner width, rows) of the preview box on the screen: what the window should start at"""
        snap = t.snap()
        for r in range(1, len(snap) - 3):
            row = snap[r + 1]
            if len(row.cells) == t.w and row.cells[-1][0] == "╮":
                x = max(i for i, c in enumerate(row.cells) if c[0] == "╭"); n = 0
                for rr in range(r + 2, len(snap)):
                    if snap[rr].cells[x][0] == "│": n += 1
                    else: break
                return len(row.cells) - x - 2 - 2, n  # between the borders, less the one blank cell each side
        return None


class Command(Shape):
    def test_i_with_no_session_makes_one_with_the_preview_s_size(self):
        self.flag("tmux.fail", "has-session"); t = self.board(); self.to_bg(t); t.keys("i")
        self.assertTrue(wait(lambda: any(c[0] == "new-session" for c in self.calls()), 4), self.calls()); time.sleep(0.5); w, h = self.geometry(t)
        want = ["new-session", "-s", "agmux-bg", "-x", str(w), "-y", str(h), "-d", "-n", "bravo-bg", "-c", f"{self.env.home}/work/bravo", "-P", "-F", "#{pane_id}", "--", "claude", "attach", "b2"]
        self.assertEqual([c for c in self.attach_calls()], [["has-session", "-t", "=agmux-bg"], want], f"-x / -y are the preview's inner width and rows ({w} x {h}):\n{self.attach_calls()}")
        self.assertTrue(wait(lambda: "keys go to the pane" in self.row(t), 3), t.text())

    def test_i_with_the_session_there_opens_a_window_in_it(self):
        t = self.board(); self.to_bg(t); t.keys("i"); self.assertTrue(wait(lambda: any(c[0] == "new-window" for c in self.calls()), 4), self.calls()); time.sleep(0.3)
        self.assertEqual(self.attach_calls(), [["has-session", "-t", "=agmux-bg"], ["new-window", "-t", "=agmux-bg:", "-d", "-n", "bravo-bg", "-c", f"{self.env.home}/work/bravo", "-P", "-F", "#{pane_id}", "--", "claude", "attach", "b2"]])
        self.assertTrue(wait(lambda: "keys go to the pane" in self.row(t), 3), "the pass-through is on: i did what it does on a card with a pane")
        t.lit("hi"); time.sleep(0.6); self.assertIn(["send-keys", "-t", "%777", "-l", "--", "hi"], self.calls(), "typed keys go to the new pane")

    def test_enter_attaches_and_then_jumps(self):
        t = self.board(); self.to_bg(t); t.keys("Enter"); self.assertEqual(t.exit_status(6), 0, t.text()); calls = self.calls(); names = [c[0] for c in calls]
        i = names.index("new-window"); j = names.index("switch-client"); self.assertLess(i, j, "the window first, then the jump"); self.assertIn("%777", calls[j], calls[j])

    def test_tab_attaches_and_opens_the_pass_through(self):
        t = self.board(); self.to_bg(t); t.keys("Tab"); self.assertTrue(wait(lambda: "keys go to the pane" in self.row(t), 4), t.text()); self.assertEqual(len([c for c in self.calls() if c[0] == "new-window"]), 1)

    def test_h_waits_for_something_on_the_window_and_then_sends_handoff(self):
        t = self.board(); self.to_bg(t); self.flag("tmux.capture", "claude is up\n"); t.keys("H"); self.assertTrue(wait(lambda: ["send-keys", "-t", "%777", "Enter"] in self.calls(), 4), self.calls())
        names = [c[0] for c in self.calls()]; k = names.index("send-keys"); self.assertIn("capture-pane", names[:k], "it looked at the window before typing"); self.assertIn(["send-keys", "-t", "%777", "-l", "--", "/handoff"], self.calls())
        self.assertLess(names.index("new-window"), k)

    def test_h_gives_up_waiting_after_five_seconds_and_still_sends(self):
        t = self.board(); self.to_bg(t); t0 = time.time(); t.keys("H"); self.assertTrue(wait(lambda: ["send-keys", "-t", "%777", "Enter"] in self.calls(), 8), self.calls()); dt = time.time() - t0
        self.assertTrue(4.3 < dt < 7.5, f"an empty window is waited for ~5 s, no longer: {dt:.1f} s"); self.assertGreaterEqual(len([c for c in self.calls() if c[0] == "capture-pane" and "-t" in c and "%777" in c]), 20, "polled about every 100 ms")

    def test_p_only_previews_and_does_not_attach(self):
        t = self.board(); self.to_bg(t); t.keys("p"); time.sleep(1.5); self.assertEqual(self.attach_calls(), []); self.assertIn("background session · enter or i attaches it", t.text()); self.assertEqual([c for c in self.calls() if c[0] == "send-keys"], [])

    def test_a_second_press_does_not_open_a_second_window(self):
        t = self.board(); self.to_bg(t); t.keys("i"); self.assertTrue(wait(lambda: "keys go to the pane" in self.row(t), 4)); t.keys("Tab"); time.sleep(0.3); t.keys("i"); time.sleep(0.8); t.keys("Tab"); t.keys("Tab"); time.sleep(0.6)
        self.assertEqual(len([c for c in self.calls() if c[0] in ("new-window", "new-session")]), 1, self.attach_calls())

    def test_a_card_that_has_a_pane_is_not_attached(self):
        t = self.board(); t.keys("j"); time.sleep(0.4); self.assertTrue(any(C_FOCUS in border_colours(c) and "pj-fallback" in c.line(1) for c in find_cards(t.snap())), t.text())
        t.keys("i"); time.sleep(0.6); t.keys("Tab"); time.sleep(0.4); t.keys("H"); time.sleep(1.0); self.assertEqual(self.attach_calls(), [], "pj-fallback has a pane: i / H use it as it is")
        self.assertIn(["send-keys", "-t", "%102", "-l", "--", "/handoff"], self.calls())

    def test_the_name_and_the_cwd_go_as_they_are_quotes_and_spaces_too(self):
        d = self.env.tmp / "dir with space"; d.mkdir(); add_bg(self.env, "q1", 'my "q" it\'s name', cwd=str(d)); t = self.board(h=50)
        for _ in range(9):
            if any(C_FOCUS in border_colours(c) and "my" in c.line(1) for c in find_cards(t.snap())): break
            t.keys("j" if _ % 2 else "l"); time.sleep(0.3)
        else: self.fail(t.text())
        t.keys("i"); self.assertTrue(wait(lambda: any(c[0] == "new-window" for c in self.calls()), 4), self.calls()); c = next(c for c in self.calls() if c[0] == "new-window")
        self.assertEqual(c[c.index("-n") + 1], 'my "q" it\'s name'); self.assertEqual(c[c.index("-c") + 1], str(d)); self.assertEqual(c[-3:], ["claude", "attach", "q1"])

    def test_two_background_cards_get_a_window_each(self):
        add_bg(self.env, "b3", "bravo-bg2"); t = self.board(h=50); self.to_bg(t); t.keys("i"); self.assertTrue(wait(lambda: "keys go to the pane" in self.row(t), 4)); t.keys("Tab"); time.sleep(0.4); t.keys("j"); time.sleep(0.5)
        self.assertTrue(any(C_FOCUS in border_colours(c) and "bravo-bg2" in c.line(1) for c in find_cards(t.snap())), t.text()); t.keys("i"); time.sleep(1.0)
        wins = [c for c in self.calls() if c[0] == "new-window"]; self.assertEqual([c[c.index("-n") + 1] for c in wins], ["bravo-bg", "bravo-bg2"]); self.assertEqual([c[-1] for c in wins], ["b2", "b3"])

    def test_the_overlay_lasts_ten_seconds_then_the_listing_decides(self):
        t = self.board(); self.to_bg(t); t.keys("i"); time.sleep(1.0); t.keys("Tab"); self.assertEqual(len([c for c in self.calls() if c[0] == "new-window"]), 1)
        time.sleep(11.5); self.assertIn("background session", t.text(), "the listing (a fake one: nothing) says the window is not there: no pane again"); t.keys("i"); time.sleep(1.2)
        self.assertEqual(len([c for c in self.calls() if c[0] == "new-window"]), 2, "and the next press makes another")


class Listing(Shape):
    def panes(self, *lines): (self.env.fake / "tmux.panes").write_text("".join(l + "\n" for l in lines))

    def title(self, t):
        t.keys("p"); time.sleep(1.5); rows = [r.text for r in t.snap()]
        return next((re.sub(r"^.*?(bravo-bg ·)", r"\1", r[100:]).strip() for r in rows if "bravo-bg ·" in r[100:]), None)

    def test_a_pane_started_with_claude_attach_id_is_the_cards_and_nothing_is_attached(self):
        self.panes("%0\tmain\t0\tzsh", "%55\tagmux-bg\t0\tclaude attach b2"); t = self.board(); self.to_bg(t); t.keys("i"); time.sleep(1.0); self.assertEqual(self.attach_calls(), [], "it has a pane already")
        self.assertTrue(wait(lambda: "keys go to the pane" in self.row(t), 3), t.text())
        self.assertTrue(any("%55" in " ".join(c) for c in self.calls() if c[0] in ("display-message", "capture-pane")), "the preview reads %55")

    def test_the_title_has_the_pane_and_the_card_is_in_the_agmux_bg_group_by_session(self):
        self.panes("%55\tagmux-bg\t0\tclaude attach b2"); t = self.board(); self.to_bg(t); self.assertEqual(self.title(t), "bravo-bg · bg · %55 · ⏸ idle")
        t.keys("p"); [t.keys("g") or time.sleep(0.3) for _ in range(2)]; time.sleep(0.8); self.assertTrue(any(re.match(r"▾ agmux-bg \(1\)", r.text.strip()) for r in t.snap()), t.text())

    def test_quotes_a_path_and_a_dead_pane(self):
        for line, claimed in (('%56\ts\t0\t"claude attach b2"', "%56"), ("%57\ts\t0\t'/usr/local/bin/claude' attach 'b2'", "%57"), ("%58\ts\t0\t/opt/x/claude attach b2", "%58"), ("%59\ts\t1\tclaude attach b2", None),
                              ("%60\ts\tclaude attach b2", None), ("%61\ts\t0\tclaude attach b22", None), ("%62\ts\t0\tclaude attach b", None), ("%63\ts\t0\tnot-claude attach b2", None), ("%64\ts\t0\tclaude logs b2", None)):
            with self.subTest(line=line):
                self.setUp(); self.panes(line); t = self.board(); self.to_bg(t); got = self.title(t)
                self.assertEqual(got, f"bravo-bg · bg · {claimed} · ⏸ idle" if claimed else "bravo-bg · bg · ⏸ idle"); t.close()

    def test_the_first_of_several_wins_and_each_id_gets_its_own(self):
        add_bg(self.env, "b3", "bravo-bg2"); self.panes("%70\ts\t0\tclaude attach b2", "%71\ts\t0\tclaude attach b2", "%72\ts\t0\tclaude attach b3"); t = self.board(h=50); self.to_bg(t); self.assertEqual(self.title(t), "bravo-bg · bg · %70 · ⏸ idle")
        t.keys("p"); t.keys("j"); time.sleep(0.5); t.keys("p"); time.sleep(1.5); self.assertTrue(any("bravo-bg2 · bg · %72" in r.text for r in t.snap()), t.text())

    def test_a_pane_that_goes_away_gives_the_card_back_without_one(self):
        self.panes("%55\tagmux-bg\t0\tclaude attach b2"); t = self.board(); self.to_bg(t); self.assertEqual(self.title(t), "bravo-bg · bg · %55 · ⏸ idle"); (self.env.fake / "tmux.panes").write_text("")
        self.assertTrue(wait(lambda: self.title_now(t) == "bravo-bg · bg · ⏸ idle", 8), t.text())

    def title_now(self, t):
        rows = [r.text for r in t.snap()]; return next((re.sub(r"^.*?(bravo-bg ·)", r"\1", r[100:]).strip() for r in rows if "bravo-bg ·" in r[100:]), None)

    def test_list_panes_runs_only_while_a_background_session_has_no_pane(self):
        t = self.board(); time.sleep(5.5); n = len([c for c in self.calls() if c[0] == "list-panes"]); self.assertGreaterEqual(n, 2, "every poll while b2 has no pane")
        S = json.loads((self.env.fake / "show.json").read_text()); self.env.write_show([s for s in S if s["id"] != "b2"]); time.sleep(3.0); k = len([c for c in self.calls() if c[0] == "list-panes"]); time.sleep(5.0)
        self.assertEqual(len([c for c in self.calls() if c[0] == "list-panes"]), k, "no background session without a pane: tmux is not asked")
        c = next(c for c in self.calls() if c[0] == "list-panes"); self.assertEqual(c, ["list-panes", "-a", "-F", "#{pane_id}\t#{session_name}\t#{pane_dead}\t#{pane_start_command}"])


class Failing(Shape):
    def test_a_failing_new_window_is_one_red_line_and_the_action_is_not_done(self):
        self.flag("tmux.fail", "new-window"); t = self.board(); self.to_bg(t); t.keys("Enter"); self.assertTrue(wait(lambda: "attach bravo-bg" in self.row(t), 4), t.text()); self.assertTrue(t.alive(), "the board stays")
        self.assertEqual(t.text().count("attach bravo-bg"), 1); self.assertNotIn("switch-client", [c[0] for c in self.calls()], "enter did not jump: there is nothing to jump to")
        line = next(r for r in t.snap()[-3:] if "attach bravo-bg" in r.text); txt = [c for c in line.cells if c[0].strip()]; self.assertTrue(all(rgb(c[1])[0] > 200 and rgb(c[1])[1] < 120 for c in txt[:20]), [(c[0], c[1]) for c in txt[:20]])
        t.keys("i"); time.sleep(1.0); self.assertTrue(t.alive()); self.assertNotIn("keys go to the pane", self.row(t))

    def test_a_failing_new_session_too(self):
        self.flag("tmux.fail", "has-session")  # has-session failing = a new session is made; then that one is made to fail by hand
        f = self.env.bin / "tmux"; f.write_text(f.read_text().replace("  new-window|new-session) echo %777; exit 0;;", "  new-session) echo 'tmux: no server boom' >&2; exit 1;;\n  new-window) echo %777; exit 0;;", 1))
        t = self.board(); self.to_bg(t); t.keys("i"); self.assertTrue(wait(lambda: "attach bravo-bg" in self.row(t), 4), t.text()); self.assertIn("no server boom", self.row(t)); self.assertTrue(t.alive())

    def test_a_failing_list_panes_is_silent(self):
        self.flag("tmux.fail", "list-panes"); t = self.board(); self.to_bg(t); time.sleep(4.5); self.assertEqual(self.row(t).count("list-panes"), 0, self.row(t)); self.assertNotIn("boom", t.text()); self.assertTrue(t.alive())
        self.assertEqual(len(find_cards(t.snap())), 9, "the board is as it is")

    def test_not_inside_tmux_the_board_does_not_crash_when_asked_to_attach(self):
        self.flag("tmux.fail", "has-session"); f = self.env.bin / "tmux"; f.write_text(f.read_text().replace("  new-window|new-session) echo %777; exit 0;;", "  new-window|new-session) echo 'no server running on /tmp/tmux-501/default' >&2; exit 1;;", 1))
        t = self.board(); self.to_bg(t)
        t.keys("i"); time.sleep(0.8); self.assertIn("no server running", self.row(t), "the reason, on the footer"); self.assertTrue(t.alive())
        for k in ("Tab", "Enter", "H", "p"): t.keys(k); time.sleep(0.8); self.assertTrue(t.alive(), k)


class RealBase(PreviewBase):
    """the private tmux server (PreviewBase) + the three real panes of a1..a3 + a fake `claude`; b2 (bravo-bg) is a background session without a pane"""
    W, H = 232, 50

    def setUp(self):
        super().setUp(); d = self.env.tmp / "fakebin"; d.mkdir(); self.fakebin = d
        (d / "claude").write_text(FAKE_CLAUDE % dict(d=str(self.env.fake), catv=str(HERE / "fixtures" / "catv.py"))); (d / "claude").chmod(0o755)
        # tmux gives a new window the PATH of the tmux client that asked for it: the board's (env.prefix_real). The fake is put first there; the python side's PATH has the user's REAL claude, so nothing here runs a bare `claude`
        os.symlink(d / "claude", self.env.bin_real / "claude")

    def to_bg(self, name="bravo-bg", keys=("l", "j")):
        self.press(*keys); self.assertTrue(wait(lambda: any(C_FOCUS in border_colours(c) and name in c.line(1) for c in find_cards(self.t.snap())), 3), self.t.text())

    def windows(self): return [l for l in tm("list-windows", "-t", "=agmux-bg", "-F", "#{window_name}\t#{pane_id}\t#{pane_start_command}\t#{window_width}x#{window_height}\t#{pane_current_path}").stdout.splitlines() if l]

    def has_session(self): return tm("has-session", "-t", "=agmux-bg").returncode == 0

    def claude_log(self):
        p = self.env.fake / "claude.log"; return p.read_text().splitlines() if p.exists() else []

    def shown(self, text, secs=5): return wait(lambda: text in "\n".join(r.text for r in (self.pv() or ("", []))[1]), secs)

    def geometry(self, t=None):
        p = self.pv(t); return (len(p[1][0].cells) - 2, len(p[1]))


class Attach(RealBase):
    def test_i_opens_the_window_sized_like_the_preview_shows_its_output_and_takes_the_typing(self):
        (self.env.home / "work" / "bravo").mkdir(parents=True, exist_ok=True); self.assertFalse(self.has_session()); self.to_bg(); self.t.keys("i")
        self.assertTrue(wait(lambda: len(self.windows()) == 1, 5), self.windows()); w = self.windows()[0].split("\t"); self.assertEqual(w[0], "bravo-bg", "the window has the card's name"); self.assertEqual(w[1][0], "%")
        self.assertRegex(w[2], r"claude attach b2$"); self.assertTrue(wait(lambda: self.claude_log() == ["attach b2"], 4), self.claude_log()); self.assertEqual(pathlib.Path(w[4]).resolve(), (self.env.home / "work" / "bravo").resolve(), "-c: the session's cwd")
        self.assertTrue(self.shown("ATTACHED b2", 6), "the preview shows what the window printed\n" + self.t.text()); time.sleep(0.8); W, H = self.geometry(); self.assertEqual(w[3], f"{W}x{H}", "new-session -x <preview inner width> -y <preview rows>")
        self.assertIn("bravo-bg · bg · " + w[1], self.pv()[0], "the title has the pane now"); self.assertTrue(wait(lambda: "keys go to the pane" in last_text_row(self.t.snap()) or "keys go to the pane" in "\n".join(r.text for r in self.t.snap()[-3:]), 3))
        self.t.lit("hello"); self.assertTrue(wait(lambda: "hello" in pane_text(w[1]), 4), "typing reaches the window: " + pane_text(w[1]))
        self.assertTrue(self.shown("hello", 4), "and the preview shows it")

    def test_tab_does_the_same_and_p_after_it_closes_the_preview_only(self):
        self.to_bg(); self.t.keys("Tab"); self.assertTrue(wait(lambda: len(self.windows()) == 1, 5), self.windows()); self.assertTrue(self.shown("ATTACHED b2", 6), self.t.text()); self.t.keys("Tab"); time.sleep(0.4)
        self.t.keys("p"); time.sleep(0.8); self.assertIsNone(self.pv()); self.assertEqual(len(self.windows()), 1, "closing the preview does not close the window")

    def test_p_does_not_attach_and_says_how(self):
        self.to_bg(); self.t.keys("p"); time.sleep(2.0); self.assertFalse(self.has_session()); self.assertTrue(self.shown("background session · enter or i attaches it", 3), self.t.text())
        self.assertEqual(self.pv()[0], "bravo-bg · bg · ⏸ idle")

    def test_pressing_again_makes_no_second_window(self):
        self.to_bg(); self.t.keys("i"); self.assertTrue(self.shown("ATTACHED b2", 6)); self.t.keys("Tab"); time.sleep(0.4); self.t.keys("i"); time.sleep(1.0); self.t.keys("Tab"); self.t.keys("Tab"); time.sleep(0.8)
        self.assertEqual(len(self.windows()), 1, self.windows()); self.assertEqual(self.claude_log(), ["attach b2"], "claude was started once")
        time.sleep(3.5); self.assertEqual(len(self.windows()), 1, "nor when the overlay is over and the listing decides"); self.assertEqual(self.claude_log(), ["attach b2"])

    def test_h_waits_until_claude_has_drawn_something_then_sends_handoff_and_enter(self):
        (self.env.fake / "delay").write_text("1.5"); self.to_bg(); t0 = time.time(); self.t.keys("H")
        self.assertTrue(wait(lambda: "/handoff^M" in pane_text(self.windows()[0].split("\t")[1] if self.windows() else "%999"), 8), "the Enter arrives as ^M (raw mode: it came after claude was reading)\n" + (pane_text(self.windows()[0].split("\t")[1]) if self.windows() else ""))
        self.assertGreater(time.time() - t0, 1.3, "not before the window had anything")

    def test_enter_makes_the_window_and_switches_the_users_client_to_it(self):
        uc = self.user_client(); self.to_bg(); self.t.keys("Enter"); self.assertTrue(wait(lambda: len(self.windows()) == 1, 5), self.windows())
        pane = self.windows()[0].split("\t")[1]; self.assertTrue(wait(lambda: tm("display-message", "-p", "-t", uc, "-c", uc, "#{pane_id}").stdout.strip() == pane or tm("list-clients", "-F", "#{client_name}|#{client_session}").stdout.count("|agmux-bg") >= 1, 5), tm("list-clients").stdout)

    def user_client(self):
        sock = tm("display-message", "-p", "#{socket_path}").stdout.strip(); out = f"{self.env.tmp}/out.sock"
        r = subprocess.run(["tmux", "-L", f"agmux-out-{os.getpid()}", "-f", "/dev/null", "new-session", "-d", "-s", "u", "-x", "150", "-y", "45", f"env -u TMUX tmux -S {sock} -f /dev/null attach -t pane-a1"], capture_output=True, text=True); self.assertEqual(r.returncode, 0, r.stderr)
        self.addCleanup(lambda: subprocess.run(["tmux", "-L", f"agmux-out-{os.getpid()}", "kill-server"], capture_output=True))
        self.assertTrue(wait(lambda: tm("list-clients", "-F", "#{client_name}").stdout.strip(), 6)); return tm("list-clients", "-F", "#{client_name}").stdout.split()[0]


class Lifecycle(RealBase):
    def test_killing_the_window_gives_the_card_back_and_the_next_press_makes_a_new_one(self):
        self.to_bg(); self.t.keys("i"); self.assertTrue(self.shown("ATTACHED b2", 6)); self.t.keys("Tab"); time.sleep(0.4); tm("kill-window", "-t", "=agmux-bg:bravo-bg")
        self.assertTrue(wait(lambda: self.pv() is not None and "background session · enter or i attaches it" in "\n".join(r.text for r in self.pv()[1]), 12), "back to no pane within a poll or two\n" + self.t.text())
        self.assertEqual(self.pv()[0], "bravo-bg · bg · ⏸ idle"); self.t.keys("i"); self.assertTrue(self.shown("ATTACHED b2", 8), self.t.text()); self.assertEqual(self.claude_log(), ["attach b2", "attach b2"], "claude was started again")

    def test_attach_exiting_closes_the_window_and_the_card_is_without_a_pane_again(self):
        (self.env.fake / "exit-after").write_text("2"); self.to_bg(); self.t.keys("i"); self.assertTrue(self.shown("ATTACHED b2", 6))
        self.assertTrue(wait(lambda: tm("list-panes", "-a", "-F", "#{pane_dead}").stdout.split().count("1") >= 1 or not self.has_session(), 8), "attach exited (the harness keeps dead panes: remain-on-exit)")
        self.assertTrue(wait(lambda: self.pv() is not None and self.pv()[0] == "bravo-bg · bg · ⏸ idle", 20), "once the 10 s overlay is over the dead pane is not the card's\n" + self.t.text())

    def test_a_dead_pane_is_not_taken_for_the_card(self):
        tm("set-option", "-g", "remain-on-exit", "on"); (self.env.fake / "exit-after").write_text("1"); self.to_bg(); self.t.keys("i"); self.assertTrue(wait(lambda: any(l.split("\t")[0] == "bravo-bg" for l in self.windows()), 5))
        self.assertTrue(wait(lambda: tm("list-panes", "-a", "-F", "#{pane_dead}").stdout.split().count("1") >= 1, 8), "the window stays (remain-on-exit) with a dead pane"); self.t.keys("Tab"); self.t.keys("Escape"); time.sleep(3.5)
        self.assertTrue(wait(lambda: self.pv() is None or self.pv()[0] == "bravo-bg · bg · ⏸ idle", 8), self.pv()[0] if self.pv() else None)

    def test_a_restarted_board_finds_the_window_again(self):
        self.to_bg(); self.t.keys("i"); self.assertTrue(self.shown("ATTACHED b2", 6)); pane = self.windows()[0].split("\t")[1]; self.t.close()
        t2 = self.board(self.W, self.H); self.t = t2; time.sleep(0.5); self.to_bg(); self.t.keys("p"); self.assertTrue(wait(lambda: self.pv() is not None and pane in self.pv()[0], 6), self.pv()); self.assertTrue(self.shown("ATTACHED b2", 5))
        self.t.keys("i"); time.sleep(1.2); self.assertEqual(len(self.windows()), 1); self.assertEqual(self.claude_log(), ["attach b2"], "no new window")

    def test_a_window_a_person_made_by_hand_with_claude_attach_is_found_too(self):
        r = tm("new-session", "-d", "-s", "byhand", "-x", "100", "-y", "30", f"{self.fakebin}/claude attach b2"); self.assertEqual(r.returncode, 0, r.stderr); self.to_bg(); self.t.keys("p")
        self.assertTrue(wait(lambda: self.pv() is not None and "bravo-bg · bg · %" in self.pv()[0], 6), self.pv()); self.assertTrue(self.shown("ATTACHED b2", 5))


class Ids(RealBase):
    def setUp(self):
        super().setUp(); add_bg(self.env, "bg-1", "worker-one"); add_bg(self.env, "bg-12", "worker-twelve")

    def focus(self, name):
        focused = lambda: any(C_FOCUS in border_colours(c) and name in c.line(1) for c in find_cards(self.t.snap()))
        if focused(): return
        for k in ("h", "h", "h", "k", "k", "k", "k", "k", "k", "l"): self.t.keys(k); time.sleep(0.12)  # the leftmost column, its top card, then the first card of the bravo column
        time.sleep(0.3)
        for _ in range(8):
            if wait(focused, 0.4): return
            self.t.keys("j"); time.sleep(0.25)
        self.fail(f"{name}\n{self.t.text()}")

    def test_bg_1_and_bg_12_each_get_and_keep_their_own_window(self):
        self.t.keys("p"); self.assertTrue(wait(lambda: self.pv() is not None, 4)); self.focus("worker-one"); self.t.keys("i"); self.assertTrue(self.shown("ATTACHED bg-1", 6), self.t.text()); self.t.keys("Tab"); time.sleep(0.3)
        self.focus("worker-twelve"); self.t.keys("i"); self.assertTrue(self.shown("ATTACHED bg-12", 6), self.t.text()); self.t.keys("Tab"); time.sleep(0.3)
        names = sorted(l.split("\t")[0] for l in self.windows()); self.assertEqual(names, ["worker-one", "worker-twelve"]); self.assertEqual(sorted(self.claude_log()), ["attach bg-1", "attach bg-12"])
        self.focus("worker-one"); time.sleep(1.5); self.assertIn("ATTACHED bg-1", "\n".join(r.text for r in self.pv()[1])); self.assertNotIn("ATTACHED bg-12", "\n".join(r.text for r in self.pv()[1]))
        tm("kill-window", "-t", "=agmux-bg:worker-one"); self.assertTrue(wait(lambda: "attaches it" in "\n".join(r.text for r in self.pv()[1]), 12), "bg-1 lost its window")
        self.focus("worker-twelve"); time.sleep(1.5); self.assertIn("ATTACHED bg-12", "\n".join(r.text for r in self.pv()[1]), "bg-12 keeps its own (the prefix did not mix them up)")

    def test_a_name_with_spaces_and_quotes_and_a_cwd_with_spaces(self):
        d = self.env.tmp / "work dir"; d.mkdir(); add_bg(self.env, "bg-q", 'say "hi" it\'s', cwd=str(d)); self.t.keys("p"); self.assertTrue(wait(lambda: self.pv() is not None, 4)); time.sleep(1.5)
        self.t.keys("/"); time.sleep(0.3); self.t.lit("say"); time.sleep(0.5); self.t.keys("Enter"); time.sleep(0.8); self.t.keys("i")
        self.assertTrue(wait(lambda: any(l.startswith('say "hi" it\'s\t') for l in self.windows()), 6), self.windows()); w = next(l for l in self.windows() if l.startswith("say")).split("\t")
        self.assertEqual(pathlib.Path(w[4]).resolve(), d.resolve()); self.assertTrue(wait(lambda: "attach bg-q" in self.claude_log(), 4), self.claude_log()); self.assertTrue(self.shown("ATTACHED bg-q", 6))


if __name__ == "__main__":
    unittest.main()
