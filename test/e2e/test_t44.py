"""T44 `agmux board --stay`: the board lives in a session of its own and a popup (`display-popup -E "tmux attach -t <it>"`) only shows it. enter = switch the user's client to the pane and detach the popup client, the
process stays; q / esc = detach only; Q (the new `exit` action) ends the process; a client of the board's own session that is not a popup (no nested termname, or the one the jump goes to) is never detached; with no popup
client (ordinary pane, the only client, no client at all) it is what it was: the jump / q end the board. Footer in stay with a popup: `q hide · Q quit` (T47: `q quit` with none). Spec: specs/m3-config/tasks.md「第八批」T44.

Real tmux, two servers: the suite's private server (tm()) holds the sessions -- `work` (the user's terminal is attached to it), `tgt` / `tgt2` (the panes the cards point at) and `agboard` (agmux board --stay); a second
server on a socket of its own (tmo()) runs the user's "terminal": a pane whose program is `tmux attach` to the first one, so there are real clients with real nested termnames. A real `display-popup -E` on that client is the popup.
Keys reach the board through the user's terminal (`send-keys` on the second server): the popup has them."""
import json, re, subprocess, unittest, uuid
from lib import *
from test_t3 import C_FOCUS, border_colours, last_text_row


class Real(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        self.out = f"{SOCK}-o{uuid.uuid4().hex[:6]}"; self.procs = []; self.addCleanup(self.stop)
        tm("kill-server"); tm("start-server", ";", "set", "-g", "remain-on-exit", "off")
        tm("new-session", "-d", "-s", "work", "-x", "150", "-y", "45", "cat")
        self.panes = {}
        for sid, sess in (("a1", "tgt"), ("b1", "tgt2")):
            tm("new-session", "-d", "-s", sess, "-x", "100", "-y", "30", "cat"); self.panes[sid] = tm("display", "-p", "-t", sess, "#{pane_id}").stdout.strip()
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            if s["id"] in self.panes: s["tmuxPane"] = self.panes[s["id"]]
        self.env.write_show(S)

    def stop(self):
        for p in self.procs: p.kill()
        self.tmo("kill-server"); tm("kill-server")

    def tmo(self, *a): return subprocess.run(["tmux", "-L", self.out, "-f", "/dev/null", *map(str, a)], capture_output=True, text=True)

    # ---- the layout ---------------------------------------------------------------------------------------------------------------------
    def start_board(self, args="--stay", config=None, session="agboard", w=100):
        if config is not None: (self.cfg / "agmux" / "config.toml").write_text(config)
        cmd = f'{self.env.prefix_real} TMUX="$TMUX" TMUX_PANE="$TMUX_PANE" XDG_CONFIG_HOME={self.cfg} {BIN} board {args}'
        r = tm("new-session", "-d", "-s", session, "-x", str(w), "-y", "40", cmd); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(wait(lambda: len(find_cards(self.screen(session))) >= 5, 10), tm("capture-pane", "-p", "-t", session).stdout); time.sleep(0.3)

    def terminal(self, name="u", session="work", w=150):
        """a user terminal: a pane of the second server running `tmux attach -t session` on the first; returns its client's name"""
        before = set(self.client_names())
        r = self.tmo("new-session", "-d", "-s", name, "-x", str(w), "-y", "45", f"env -u TMUX tmux -L {SOCK} -f /dev/null attach -t {session}"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(wait(lambda: set(self.client_names()) - before, 6)); return (set(self.client_names()) - before).pop()

    def clients(self):
        out = [l.split("|") for l in tm("list-clients", "-F", "#{client_name}|#{client_session}|#{client_termname}").stdout.splitlines() if l]
        return [dict(name=n, session=s, term=t) for n, s, t in out]

    def client_names(self): return [c["name"] for c in self.clients()]

    def sessions_of(self, session): return [c for c in self.clients() if c["session"] == session]

    def popup(self, client, session="agboard", w=100):
        before = {c["name"] for c in self.sessions_of(session)}
        p = subprocess.Popen(["tmux", "-L", SOCK, "-f", "/dev/null", "display-popup", "-c", client, "-w", str(w), "-h", "40", "-E", f"env -u TMUX tmux -L {SOCK} -f /dev/null attach -t {session}"]); self.procs.append(p)
        self.assertTrue(wait(lambda: {c["name"] for c in self.sessions_of(session)} - before, 8), f"the popup client attached: {self.clients()}"); time.sleep(0.6); return p

    def closed(self, p, session="agboard", timeout=6):
        return wait(lambda: p.poll() is not None and not self.sessions_of(session), timeout)

    def press(self, *keys, sess="u"):
        for k in keys: self.tmo("send-keys", "-t", sess, k); time.sleep(0.25)

    def screen(self, session="agboard"): return parse_screen(tm("capture-pane", "-e", "-p", "-t", session).stdout)

    def focused(self, session="agboard"):
        return [re.match(r"\s*(\S+)", c.line(1)[8:]).group(1) for c in find_cards(self.screen(session)) if C_FOCUS in border_colours(c)]

    def alive(self, session="agboard"):
        r = tm("list-panes", "-t", session, "-F", "#{pane_dead}"); return r.returncode == 0 and r.stdout.split() == ["0"]

    def user_session(self, client): return next((c["session"] for c in self.clients() if c["name"] == client), None)


class Stay(Real):
    def test_enter_jumps_the_users_client_closes_the_popup_and_the_board_lives_on(self):
        self.start_board(); uc = self.terminal(); p = self.popup(uc); self.assertEqual(sorted(c["session"] for c in self.clients()), ["agboard", "work"])
        self.press("l"); self.assertEqual(self.focused(), ["bravo-29"]); self.press("Enter")
        self.assertTrue(self.closed(p), f"popup closed: {self.clients()}"); self.assertEqual(self.user_session(uc), "tgt2", "the user's client is on bravo-29's pane now"); self.assertTrue(self.alive(), "the process stays")
        self.assertEqual([c["session"] for c in self.clients()], ["tgt2"], "only the user's client is left")
        p2 = self.popup(uc); self.assertEqual(self.focused(), ["bravo-29"], "attach again: the same card has the focus"); self.assertTrue(self.alive())

    def test_q_and_esc_only_detach_the_popup(self):
        self.start_board(); uc = self.terminal()
        for key, wait_after in (("q", 0.3), ("Escape", 1.2)):
            with self.subTest(key=key):
                p = self.popup(uc); self.press("l"); self.press(key); time.sleep(wait_after)
                self.assertTrue(self.closed(p), f"{key}: {self.clients()}"); self.assertEqual(self.user_session(uc), "work", "the user's client did not move"); self.assertTrue(self.alive(), f"{key}: the process stays")

    def test_attach_again_shows_the_same_focus_and_the_kept_filter(self):
        self.start_board(); uc = self.terminal(); p = self.popup(uc); self.press("/"); self.tmo("send-keys", "-t", "u", "-l", "brav"); time.sleep(0.6); self.press("Enter"); time.sleep(0.4)
        self.assertEqual(self.focused(), ["bravo-29"]); self.press("q"); self.assertTrue(self.closed(p))
        self.popup(uc); scr = self.screen(); top = scr[0].text; self.assertIn("/ brav", top); self.assertEqual(self.focused(), ["bravo-29"]); self.assertEqual(sorted(re.match(r"\s*(\S+)", c.line(1)[8:]).group(1) for c in find_cards(scr)), ["bravo-29", "bravo-bg"])

    def test_the_polling_goes_on_while_the_popup_is_closed(self):
        self.start_board(); uc = self.terminal(); p = self.popup(uc); self.press("q"); self.assertTrue(self.closed(p))
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            if s["id"] == "c1": s["summary"] = "renamed-while-hidden"
        self.env.write_show(S); time.sleep(1.5); self.popup(uc); names = [re.match(r"\s*(\S+)", c.line(1)[8:]).group(1) for c in find_cards(self.screen())]
        self.assertIn("renamed-while-hidden", names); self.assertNotIn("charlie-30", names)

    def test_capital_q_ends_the_process_and_the_popup_with_it(self):
        self.start_board(); uc = self.terminal(); p = self.popup(uc); self.press("Q")
        self.assertTrue(wait(lambda: tm("has-session", "-t", "agboard").returncode != 0, 6), "the session is gone with its only process"); self.assertTrue(self.closed(p)); self.assertEqual(self.user_session(uc), "work")

    def test_a_client_of_another_session_is_not_detached(self):
        """another float / terminal on a different session (`tgt`, a tmux-* client too): not the board's popup, so it stays after the jump"""
        # without AGMUX_CLIENT the board moves the most recently active client (tmux counts whole seconds, and a key typed into a popup does not bump the user's client): the user's terminal is attached 1.3 s after the other one so it is the newer, not a tie that goes either way
        self.start_board(); other = self.terminal("u2", "tgt"); time.sleep(1.3); uc = self.terminal(); time.sleep(0.3); self.assertEqual(self.user_session(other), "tgt")
        p = self.popup(uc); self.press("Enter"); self.assertTrue(self.closed(p)); self.assertEqual(self.user_session(other), "tgt", "the other client is still attached, where it was")
        self.assertEqual(self.user_session(uc), "tgt", "and the jump moved the client the user types on, not the other one"); self.assertTrue(self.alive())

    def test_a_second_client_of_the_own_session_that_is_not_nested_is_not_detached(self):
        """the user's own (non-tmux) terminal on the board's session: termname xterm-256color is not a popup. A bare pty client, made with `script`-less trick: a tmux client attached through `env TERM=xterm-256color`"""
        self.start_board(); uc = self.terminal(); self.tmo("new-session", "-d", "-s", "x", "-x", "150", "-y", "45", f"env -u TMUX TERM=xterm-256color tmux -L {SOCK} -f /dev/null attach -t agboard")
        self.assertTrue(wait(lambda: any(c["session"] == "agboard" for c in self.clients()), 6)); mine = [c for c in self.sessions_of("agboard")][0]
        if re.match(r"(tmux|screen)", mine["term"]): self.skipTest(f"the second client reports termname {mine['term']}: tmux gave it the outer pane's TERM")
        p = self.popup(uc); self.press("Enter"); self.assertTrue(wait(lambda: p.poll() is not None, 6)); self.assertIn(mine["name"], self.client_names(), "the non-nested client of the board's session stays")


class Degrade(Real):
    """no popup client (ordinary pane, the only client, no client at all): q ends the board (the jump no longer does, T47: see test_t47)"""
    def test_q_with_no_popup_client_ends_the_board(self):
        self.start_board(session="solo"); uc = self.terminal("u", "solo"); self.press("q"); self.assertTrue(wait(lambda: tm("has-session", "-t", "solo").returncode != 0, 6), "the only client of its session is the user's: q ends it")

    def test_q_with_no_client_at_all_ends_the_board(self):
        self.start_board(); self.assertEqual(self.clients(), []); tm("send-keys", "-t", "agboard", "q"); self.assertTrue(wait(lambda: tm("has-session", "-t", "agboard").returncode != 0, 6))


class Cli(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def run_cli(self, *a):
        return subprocess.run([BIN, *a], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(self.cfg), "XDG_STATE_HOME": str(self.env.state), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10)

    def test_other_arguments_are_a_usage_error(self):
        for a in (["--nope"], ["--stay", "x"], ["x"], ["--stay", "--stay"]):
            with self.subTest(a=a):
                r = self.run_cli("board", *a); self.assertEqual(r.returncode, 2, r.stderr); self.assertIn("unknown argument", r.stderr); self.assertIn("usage: agmux board [--stay]", r.stderr)

    def test_help_mentions_the_flag(self):
        r = self.run_cli("help"); out = r.stdout + r.stderr; self.assertIn("--stay", out)

    def test_config_prints_exit_after_quit(self):
        out = plain_config(self.run_cli("config").stdout); self.assertRegex(out, r'(?m)^quit = \["q", "esc"\]\nexit = \["Q"\]$')

    def test_the_footer_says_hide_and_quit_in_stay_and_plain_quit_otherwise(self):
        """outside tmux --stay degrades: no `q hide`, one line on the footer why; the plain board says `q quit`"""
        t = Term(self.env, w=220, h=30, args="board", extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 5, 10)); time.sleep(0.3)
        bar = last_text_row(t.snap()); self.assertIn("q quit", bar); self.assertNotIn("hide", bar)
        t2 = Term(self.env, w=220, h=30, args="board --stay", extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t2.close); self.assertTrue(wait(lambda: len(find_cards(t2.snap())) >= 5, 10)); time.sleep(0.3)
        self.assertIn("--stay only works inside tmux", t2.text()); self.assertNotIn("its own tmux session", t2.text()); self.assertIn("q quit", last_text_row(t2.snap()))

    def test_capital_q_quits_a_plain_board_too(self):
        t = Term(self.env, w=120, h=30, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 5, 10)); t.keys("Q"); self.assertEqual(t.exit_status(5), 0)


class Fake(unittest.TestCase):
    """the calls agmux makes, with the fake tmux (T10 style): --stay under a pane of its own session `fl-own`; clients are (activity, name, session, termname, tty) rows"""
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def board(self, clients, fail=None):
        self.env.set_tmux(session="fl-own", clients=clients, fail=fail)
        t = Term(self.env, w=120, h=30, args="board --stay", extra_env=f"XDG_CONFIG_HOME={self.cfg} TMUX=/fake/tmux,1,0 TMUX_PANE=%9"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 5, 10), t.text()); time.sleep(0.4); return t

    def calls(self, *names): return [c for c in self.env.tmux_calls() if c[0] in names]

    USER, POPUP = (300, "outer", "main", "tmux-256color", "/dev/null"), (200, "popup", "fl-own", "tmux-256color", "/dev/null")

    def test_q_detaches_the_popup_client_only_and_the_process_stays(self):
        t = self.board([self.USER, self.POPUP]); t.keys("q"); time.sleep(1.0); self.assertTrue(t.alive(), "q hides, it does not end the board")
        self.assertEqual(self.calls("detach-client"), [["detach-client", "-t", "popup"]]); self.assertEqual(self.calls("switch-client"), [])

    def test_enter_switches_the_users_client_then_detaches_the_popup(self):
        t = self.board([self.USER, self.POPUP]); t.keys("Enter"); time.sleep(1.0); self.assertTrue(t.alive())
        sw = self.calls("switch-client"); self.assertEqual(len(sw), 1); self.assertEqual(sw[0][:3], ["switch-client", "-c", "outer"]); self.assertEqual(self.calls("detach-client"), [["detach-client", "-t", "popup"]])
        names = [c[0] for c in self.calls("switch-client", "detach-client")]; self.assertEqual(names, ["switch-client", "detach-client"], "switch first, then the popup closes")

    def test_a_tmux_client_of_another_session_is_left_alone(self):
        t = self.board([self.USER, self.POPUP, (250, "float-elsewhere", "fl-other", "tmux-256color", "/dev/null")]); t.keys("q"); time.sleep(1.0)
        self.assertEqual(self.calls("detach-client"), [["detach-client", "-t", "popup"]], "only the board's own session's popup")

    def test_a_client_of_the_own_session_with_a_real_terminal_is_left_alone(self):
        t = self.board([self.USER, self.POPUP, (100, "mine", "fl-own", "xterm-256color", "/dev/null")]); t.keys("q"); time.sleep(1.0)
        self.assertEqual(self.calls("detach-client"), [["detach-client", "-t", "popup"]], "the user's own terminal on that session is not a popup")

    def test_a_failing_detach_keeps_the_board_and_says_why(self):
        t = self.board([self.USER, self.POPUP], fail="detach-client"); t.keys("q"); time.sleep(1.0); self.assertTrue(t.alive(), t.text()); self.assertIn("boom from fake tmux", t.text())
        t.keys("Enter"); time.sleep(1.0); self.assertTrue(t.alive()); self.assertIn("boom from fake tmux", t.text())

    def test_a_failing_list_clients_keeps_the_board_and_says_why(self):
        t = self.board([self.USER, self.POPUP], fail="list-clients"); t.keys("q"); time.sleep(1.0); self.assertTrue(t.alive(), t.text()); bar = "\n".join(r.text for r in t.snap()[-3:]); self.assertIn("tmux list-clients", bar, "the message line names the failing command (its stderr is cut off at 120 columns)")
        t.keys("Enter"); time.sleep(1.0); self.assertTrue(t.alive()); self.assertIn("tmux list-clients", "\n".join(r.text for r in t.snap()[-3:]))

    def test_no_popup_client_q_ends_the_board_as_before(self):
        t = self.board([self.USER]); t.keys("q"); self.assertEqual(t.exit_status(5), 0); self.assertEqual(self.calls("detach-client"), [])

    def test_no_popup_client_enter_switches_and_the_board_stays_and_nothing_is_detached(self):
        t = self.board([self.USER]); t.keys("Enter"); time.sleep(1.0); self.assertTrue(t.alive(), t.text()); sw = self.calls("switch-client"); self.assertEqual(len(sw), 1, self.env.tmux_calls()); self.assertEqual(sw[0][:3], ["switch-client", "-c", "outer"])
        self.assertEqual(self.calls("detach-client"), []); t.keys("Enter"); time.sleep(0.8); self.assertEqual(len(self.calls("switch-client")), 2, "and again"); self.assertTrue(t.alive())

    def test_no_client_at_all_enter_says_so_and_the_board_stays(self):
        t = self.board([]); t.keys("Enter"); time.sleep(1.0); self.assertTrue(t.alive(), t.text()); self.assertEqual(self.calls("switch-client", "detach-client"), [])

    def test_two_nested_clients_one_is_the_users_jump_goes_by_the_chosen_one_and_the_other_is_the_popup(self):
        t = self.board([self.USER, self.POPUP]); t.keys("Enter"); time.sleep(1.0); self.assertEqual([c[:3] for c in self.calls("switch-client")], [["switch-client", "-c", "outer"]]); self.assertEqual(self.calls("detach-client"), [["detach-client", "-t", "popup"]]); self.assertTrue(t.alive())


class Footer(Real):
    def test_stay_footer_names_both_keys_while_a_popup_shows_the_board(self):
        self.start_board(w=230); uc = self.terminal(w=240); self.popup(uc, w=230); self.assertTrue(wait(lambda: "q hide · Q quit" in last_text_row(self.screen()), 3), last_text_row(self.screen()))  # (no popup: `q quit`, see test_t47)


if __name__ == "__main__":
    unittest.main()
