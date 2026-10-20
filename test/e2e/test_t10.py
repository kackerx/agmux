"""T10: enter / s / H go straight to tmux (r / d / x stay on ccmux). Spec: specs/m2-preview/design.md「switch / send 迁到 tmux」, tasks.md T10.

T14: `s` no longer opens a prompt: it opens the preview (if closed) and puts the focus in it (= P + tab); what is typed afterwards goes to the pane.
Two layers:
  * FakeTmux — a `tmux` script first on PATH records argv and answers display-message / list-clients from files (lib.Env.set_tmux); agmux is
    started with TMUX_PANE set (or not) to exercise every client-selection branch. Nothing here can reach a real tmux server.
  * RealTmux — real tmux on the private -L socket: an outer client (pty-attached), a popup running a nested `fl` session in which agmux runs,
    a target pane running `cat`. Enter must switch the OUTER client to the target and close the popup, leaving the fl session alive;
    s / H must reach the pane (cat echoes them).
"""
import fcntl, os, pty, struct, subprocess, termios, threading, unittest
from lib import *
from test_t3 import ready, by_name, NAMES, last_text_row, C_FOCUS, border_colours

CLIENTS_FMT = "#{client_activity}\t#{client_name}\t#{client_session}\t#{client_termname}\t#{client_tty}"
A1_PANE = "%101"  # fixture: a1 (alpha-main) lives in %101; b2 is a background session without a pane


def foot(t, n=3):
    return "\n".join(r.text for r in t.snap()[-n:])


class FakeTmux(unittest.TestCase):
    def start(self, tmux_pane="%9", extra="", **kw):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.env.set_tmux(**kw)
        ee = (f"TMUX_PANE={tmux_pane} " if tmux_pane else "") + extra
        self.t = Term(self.env, w=200, h=50, extra_env=ee); self.addCleanup(self.t.close)
        self.assertTrue(ready(self.t), self.t.text())
        return self.t

    def tcalls(self, n, timeout=3):
        wait(lambda: len(self.env.tmux_calls()) >= n, timeout)
        return self.env.tmux_calls()

    # ---- enter ---------------------------------------------------------------------------------------------------------------------------
    def test_nested_float_without_agmux_client_only_switches(self):
        self.start(session="fl-own", clients=[(100, "outer-1", "main"), (300, "outer-2", "work"), (500, "float", "fl-own")])
        self.t.keys("Enter")
        c = self.tcalls(5)
        self.assertEqual(c, [["display-message", "-p", "-t", "%9", "#{session_name}"], ["list-clients", "-F", CLIENTS_FMT],
                             ["show-environment", "-h", "-t", "fl-own", "AGMUX_CLIENT"], ["show-environment", "-t", "fl-own", "AGMUX_CLIENT"], ["switch-client", "-c", "outer-2", "-t", A1_PANE]],
                         "own session via TMUX_PANE; no $AGMUX_CLIENT -> ask the HIDDEN session environment (-h; unset here), then the plain one (T38; unset too); most recent client NOT attached to the own session; switch")
        self.assertEqual(self.t.exit_status(), 0); time.sleep(0.3)
        self.assertEqual(len(self.env.tmux_calls()), 5, "without $AGMUX_CLIENT nothing is detached (the heuristic cannot tell a float from a real terminal)")
        self.assertEqual(self.env.calls(), [], "no ccmux switch any more")

    def test_agmux_client_switches_then_detaches_the_other_clients_of_its_own_session(self):
        self.start(session="fl-own", clients=[(100, "outer-1", "main"), (300, "outer-2", "work"), (500, "float", "fl-own"), (400, "float-2", "fl-own")],
                   extra="AGMUX_CLIENT=outer-1")
        self.t.keys("Enter")
        c = self.tcalls(5)
        self.assertEqual(c, [["display-message", "-p", "-t", "%9", "#{session_name}"], ["list-clients", "-F", CLIENTS_FMT],
                             ["switch-client", "-c", "outer-1", "-t", A1_PANE], ["detach-client", "-t", "float"], ["detach-client", "-t", "float-2"]],
                         "$AGMUX_CLIENT is used as is; then each OTHER client of agmux's own session is detached by name (never `-s`)")
        self.assertEqual(self.t.exit_status(), 0)

    def test_agmux_client_inside_the_own_session_is_rejected_and_the_heuristic_takes_over(self):
        """T15 review: a named client attached to agmux's own session is the float's inner client (or a reused tty name): treat it as unset"""
        self.start(session="fl-own", clients=[(100, "outer-1", "main"), (500, "float", "fl-own")], extra="AGMUX_CLIENT=float")
        self.t.keys("Enter"); c = self.tcalls(3); time.sleep(0.4)
        self.assertEqual(self.env.tmux_calls()[2], ["switch-client", "-c", "outer-1", "-t", A1_PANE], "not the float itself: the heuristic picks the outside client")
        self.assertEqual([x[0] for x in self.env.tmux_calls()], ["display-message", "list-clients", "switch-client"], "and nothing is detached")

    def test_agmux_client_that_no_longer_exists_falls_back_without_detaching(self):
        self.start(session="fl-own", clients=[(100, "outer-1", "main"), (300, "outer-2", "work"), (500, "float", "fl-own")], extra="AGMUX_CLIENT=/dev/gone")
        self.t.keys("Enter"); self.tcalls(3); time.sleep(0.4)
        self.assertEqual([x[0] for x in self.env.tmux_calls()], ["display-message", "list-clients", "switch-client"], "process value is not asked again from the session, and not detached")
        self.assertEqual(self.env.tmux_calls()[2], ["switch-client", "-c", "outer-2", "-t", A1_PANE])

    def test_session_environment_value_is_validated_too(self):
        for label, val in (("not attached", "AGMUX_CLIENT=/dev/gone"), ("inside the own session", "AGMUX_CLIENT=float")):
            with self.subTest(label):
                self.start(session="fl-own", session_env=val, clients=[(100, "outer-1", "main"), (300, "outer-2", "work"), (500, "float", "fl-own")])
                self.t.keys("Enter"); self.tcalls(4); time.sleep(0.4)
                self.assertEqual([x[0] for x in self.env.tmux_calls()], ["display-message", "list-clients", "show-environment", "switch-client"])
                self.assertEqual(self.env.tmux_calls()[3], ["switch-client", "-c", "outer-2", "-t", A1_PANE], "heuristic, no detach"); self.t.close()

    # ---- T15: AGMUX_CLIENT from the session environment -------------------------------------------------------------------------------------
    def test_session_environment_names_the_client(self):
        self.start(session="fl-own", session_env="AGMUX_CLIENT=/dev/outer",
                   clients=[(100, "/dev/other", "work"), (200, "/dev/outer", "main"), (500, "float", "fl-own"), (400, "float-2", "fl-own")])
        self.t.keys("Enter"); c = self.tcalls(6)
        self.assertEqual(c, [["display-message", "-p", "-t", "%9", "#{session_name}"], ["list-clients", "-F", CLIENTS_FMT],
                             ["show-environment", "-h", "-t", "fl-own", "AGMUX_CLIENT"], ["switch-client", "-c", "/dev/outer", "-t", A1_PANE],
                             ["detach-client", "-t", "float"], ["detach-client", "-t", "float-2"]],
                         "the session's AGMUX_CLIENT is an exact choice: switch it, then detach the other clients of agmux's own session by name")
        self.assertEqual(self.t.exit_status(), 0)

    def test_unset_or_failing_session_environment_falls_back_to_the_heuristic(self):
        # T38: a hidden "-AGMUX_CLIENT" (unset on purpose) is final; anything else unusable goes on to ask the plain session variable too
        for label, env_val, fail, asks in (("-AGMUX_CLIENT", "-AGMUX_CLIENT", None, 1), ("empty", "", None, 2), ("error", None, "show-environment", 2), ("garbage", "SOMETHING_ELSE=1", None, 2)):
            with self.subTest(show_environment=label):
                self.start(session="fl-own", session_env=env_val, fail=fail,
                           clients=[(100, "outer-1", "main"), (300, "outer-2", "work"), (500, "float", "fl-own")])
                self.t.keys("Enter"); c = self.tcalls(3 + asks); time.sleep(0.4)
                self.assertEqual([x[0] for x in self.env.tmux_calls()], ["display-message", "list-clients"] + ["show-environment"] * asks + ["switch-client"],
                                 "heuristic (most recent client outside the own session), and no detach")
                self.assertEqual(self.env.tmux_calls()[2 + asks], ["switch-client", "-c", "outer-2", "-t", A1_PANE]); self.t.close()

    def test_a_plain_session_variable_is_read_when_no_hidden_one_is_there(self):  # T38
        self.start(session="fl-own", session_env_plain="AGMUX_CLIENT=/dev/outer", clients=[(100, "/dev/other", "work"), (200, "/dev/outer", "main"), (500, "float", "fl-own")])
        self.t.keys("Enter"); c = self.tcalls(6)
        self.assertEqual(c[2:], [["show-environment", "-h", "-t", "fl-own", "AGMUX_CLIENT"], ["show-environment", "-t", "fl-own", "AGMUX_CLIENT"], ["switch-client", "-c", "/dev/outer", "-t", A1_PANE],
                                 ["detach-client", "-t", "float"]], "-h first, then the plain one; its value is an exact choice like the hidden one")

    def test_the_hidden_variable_wins_over_the_plain_one_and_stops_the_asking(self):  # T38
        self.start(session="fl-own", session_env="AGMUX_CLIENT=/dev/hidden", session_env_plain="AGMUX_CLIENT=/dev/plain",
                   clients=[(100, "/dev/hidden", "main"), (200, "/dev/plain", "work"), (500, "float", "fl-own")])
        self.t.keys("Enter"); c = self.tcalls(5); time.sleep(0.3)
        self.assertEqual([x[0] for x in self.env.tmux_calls()], ["display-message", "list-clients", "show-environment", "switch-client", "detach-client"], "one show-environment")
        self.assertEqual(self.env.tmux_calls()[3], ["switch-client", "-c", "/dev/hidden", "-t", A1_PANE])

    def test_an_explicit_hidden_unset_does_not_ask_the_plain_variable(self):  # T38
        self.start(session="fl-own", session_env="-AGMUX_CLIENT", session_env_plain="AGMUX_CLIENT=/dev/plain", clients=[(100, "/dev/plain", "main"), (300, "outer-2", "work"), (500, "float", "fl-own")])
        self.t.keys("Enter"); c = self.tcalls(4); time.sleep(0.3)
        self.assertEqual([x[0] for x in self.env.tmux_calls()], ["display-message", "list-clients", "show-environment", "switch-client"])
        self.assertEqual(self.env.tmux_calls()[3], ["switch-client", "-c", "outer-2", "-t", A1_PANE], "the heuristic: the plain value is ignored")

    def test_process_environment_wins_and_skips_show_environment(self):
        self.start(session="fl-own", session_env="AGMUX_CLIENT=/dev/session-one", extra="AGMUX_CLIENT=/dev/process-one",
                   clients=[(100, "/dev/process-one", "main"), (200, "/dev/session-one", "work"), (500, "float", "fl-own")])
        self.t.keys("Enter"); c = self.tcalls(4); time.sleep(0.4)
        self.assertEqual([x[0] for x in self.env.tmux_calls()], ["display-message", "list-clients", "switch-client", "detach-client"], "no show-environment")
        self.assertEqual(self.env.tmux_calls()[2], ["switch-client", "-c", "/dev/process-one", "-t", A1_PANE])

    def test_session_environment_is_asked_with_the_exact_session_name(self):
        for own in ("my float", "fl ", " fl"):
            with self.subTest(own=repr(own)):
                self.start(session=own, session_env="AGMUX_CLIENT=outer", clients=[(100, "outer", "main"), (500, "float", own)])
                self.t.keys("Enter"); self.tcalls(5); time.sleep(0.4)
                self.assertIn(["show-environment", "-h", "-t", own, "AGMUX_CLIENT"], self.env.tmux_calls()); self.t.close()

    def test_no_show_environment_without_an_own_session(self):  # no TMUX_PANE, or a stale one
        self.start(tmux_pane=None, session_env="AGMUX_CLIENT=outer", clients=[(100, "outer", "main")])
        self.t.keys("Enter"); self.tcalls(2); time.sleep(0.4)
        self.assertEqual([x[0] for x in self.env.tmux_calls()], ["list-clients", "switch-client"])

    def test_direct_popup_without_tmux_pane(self):  # `display-popup -E agmux board`: no TMUX_PANE
        self.start(tmux_pane=None, clients=[(100, "outer-1", "main"), (300, "outer-2", "work")])
        self.t.keys("Enter")
        c = self.tcalls(2)
        self.assertEqual(c, [["list-clients", "-F", CLIENTS_FMT], ["switch-client", "-c", "outer-2", "-t", A1_PANE]], "no display-message, no detach")
        self.assertEqual(self.t.exit_status(), 0)

    def test_direct_popup_with_agmux_client_does_not_detach_anything(self):
        self.start(tmux_pane=None, clients=[(100, "outer-1", "main"), (500, "float", "fl-own")], extra="AGMUX_CLIENT=outer-1")
        self.t.keys("Enter"); self.tcalls(2); time.sleep(0.4)
        self.assertEqual([x[0] for x in self.env.tmux_calls()], ["list-clients", "switch-client"], "no own session known: nothing to detach")

    def test_ordinary_pane_single_terminal_falls_back_and_does_not_detach(self):
        self.start(session="main", clients=[(100, "only", "main")], extra="AGMUX_CLIENT=only")
        self.t.keys("Enter")
        c = self.tcalls(3)
        self.assertEqual(c, [["display-message", "-p", "-t", "%9", "#{session_name}"], ["list-clients", "-F", CLIENTS_FMT],
                             ["switch-client", "-c", "only", "-t", A1_PANE]])
        self.assertEqual(self.t.exit_status(), 0); time.sleep(0.3)
        self.assertEqual(len(self.env.tmux_calls()), 3, "nothing is detached when the only terminal is agmux's own")

    def test_ordinary_pane_fallback_without_agmux_client(self):
        self.start(session="main", clients=[(100, "only", "main"), (200, "second", "main")])
        self.t.keys("Enter"); c = self.tcalls(5)
        self.assertEqual(c[2:4], [["show-environment", "-h", "-t", "main", "AGMUX_CLIENT"], ["show-environment", "-t", "main", "AGMUX_CLIENT"]]); self.assertEqual(c[4], ["switch-client", "-c", "second", "-t", A1_PANE], "every client is in agmux's own session: the most recent of them")
        self.assertEqual(self.t.exit_status(), 0); time.sleep(0.3); self.assertEqual(len(self.env.tmux_calls()), 5, "no detach on the fallback")

    def test_session_names_with_spaces_compare_exactly(self):
        for own in ("my float", "fl ", " fl", "a  b"):
            with self.subTest(own=repr(own)):
                self.start(session=own, clients=[(100, "outer", "main"), (500, "float", own), (400, "float-twin", own.strip())],
                           extra="AGMUX_CLIENT=outer")
                self.t.keys("Enter"); c = self.tcalls(4); time.sleep(0.4)
                detached = [x[2] for x in self.env.tmux_calls() if x[0] == "detach-client"]
                # float-twin sits in own.strip(): the SAME session when there is nothing to strip, a different one ("fl" vs "fl ") otherwise
                self.assertEqual(detached, ["float"] + (["float-twin"] if own.strip() == own else []),
                                 f"session {own!r}: only clients attached to exactly that name (trailing / leading spaces count) are detached")
                self.t.close()

    def test_display_message_failure_is_not_an_error(self):  # a stale TMUX_PANE in the environment
        self.start(session="fl-own", clients=[(100, "outer-1", "main"), (300, "outer-2", "work")], fail="display-message")
        self.t.keys("Enter")
        c = self.tcalls(3)
        self.assertEqual([x[0] for x in c], ["display-message", "list-clients", "switch-client"])
        self.assertEqual(c[2], ["switch-client", "-c", "outer-2", "-t", A1_PANE], "no own session: the most recent client of all")
        self.assertEqual(self.t.exit_status(), 0)

    def test_switch_failure_does_not_detach_and_does_not_exit(self):
        self.start(session="fl-own", clients=[(100, "outer", "main"), (500, "float", "fl-own")], fail="switch-client", extra="AGMUX_CLIENT=outer")
        self.t.keys("Enter")
        self.assertTrue(wait(lambda: "boom from fake tmux" in foot(self.t), 3), self.t.text())
        self.assertNotIn("second tmux line", self.t.text()); self.assertTrue(self.t.alive())
        time.sleep(0.4)
        self.assertEqual([c[0] for c in self.env.tmux_calls()][-1], "switch-client", "no detach after a failed switch")

    def test_background_session_makes_no_ccmux_call_and_no_jump_or_typing(self):
        self.start(clients=[(100, "outer", "main")])
        self.t.keys("l"); time.sleep(0.25); self.t.keys("j"); time.sleep(0.25)  # b2: tmuxPane null
        for k in ("Enter", "H"):
            self.t.keys(k); time.sleep(0.4)
        # T60: enter / H on a background card open `claude attach` in a window of agmux-bg (test_t60 has the details); what must still not happen: ccmux is not called, no jump, nothing is typed anywhere
        self.assertEqual(self.env.calls(), []); self.assertEqual({c[0] for c in self.env.tmux_calls()} - {"has-session", "new-window", "new-session", "display-message", "list-clients", "show-environment"}, set(), self.env.tmux_calls())
        self.assertTrue(self.t.alive())

    def test_no_clients_shows_the_hint_and_stays(self):
        self.start(session="fl-own", clients=[])
        self.t.keys("Enter")
        self.assertTrue(wait(lambda: "no tmux client to switch" in foot(self.t), 3), self.t.text())
        self.assertIn("AGMUX_CLIENT", foot(self.t)); self.assertTrue(self.t.alive(), "the board must not exit")
        self.assertEqual([c[0] for c in self.env.tmux_calls()], ["display-message", "list-clients", "show-environment", "show-environment"], "no switch attempted")
        self.assertEqual(len([c for c in find_cards(self.t.snap()) if c.ok]), 9)

    # ---- send ----------------------------------------------------------------------------------------------------------------------------
    def preview_open_and_focused(self):
        snap = self.t.snap()
        top = next((r for r in range(1, len(snap) - 3) if len(snap[r + 1].cells) == self.t.w and snap[r + 1].cells[-1][0] == "╮"), None)
        if top is None:
            return False, False
        x = max(i for i in range(100, self.t.w) if snap[top + 1].cells[i][0] == "╭")
        return True, snap[top + 1].cells[x][1] == ("rgb", 255, 142, 190)

    def sent_text(self):
        """what reached the pane, in order: literal text pieces joined, special keys in [brackets]"""
        out = ""
        for c in self.env.tmux_calls():
            if c[0] == "send-keys" and "-l" in c:
                out += " ".join(c[c.index("--") + 1:])
            elif c[0] == "send-keys":
                out += f"[{c[-1]}]"
        return out

    def test_s_opens_the_preview_with_the_focus_and_typing_goes_to_the_pane(self):
        self.start()
        self.t.keys("i"); time.sleep(0.6)
        self.assertEqual(self.preview_open_and_focused(), (True, True), "s = P + tab: preview open, border #ff8ebe\n" + self.t.text())
        self.assertIn("keys go to the pane", foot(self.t))
        self.t.lit("hello world"); time.sleep(0.5)
        self.assertEqual(self.sent_text(), "hello world")
        self.t.keys("Enter"); time.sleep(0.4)
        self.assertEqual(self.sent_text(), "hello world[Enter]", "Enter is forwarded as the Enter key, at once")
        self.assertTrue(all(c[2] == A1_PANE for c in self.env.tmux_calls() if c[0] == "send-keys"), "all to the focused card's pane")
        self.assertEqual(self.env.calls(), [], "no ccmux send"); self.assertTrue(self.t.alive())

    def test_chinese_and_dash_text_arrive_whole_and_in_order(self):
        self.start()
        self.t.keys("i"); time.sleep(0.6)
        for text in ("你好 世界，请继续", "--help", "- 修一下", "-n", "a'b\"c $HOME `x`"):
            before = len(self.sent_text()); self.t.lit(text); time.sleep(0.5)
            self.assertEqual(self.sent_text()[before:], text, f"{text!r}")
        for c in self.env.tmux_calls():
            if c[0] == "send-keys" and "-l" in c:
                self.assertEqual(c[c.index("-l") + 1], "--", "literal text always after `--`")

    def test_s_with_the_preview_already_open_only_moves_the_focus(self):
        self.start()
        self.t.keys("p"); time.sleep(0.6)
        self.assertEqual(self.preview_open_and_focused(), (True, False))
        self.t.keys("i"); time.sleep(0.5)
        self.assertEqual(self.preview_open_and_focused(), (True, True), "still open (s is not a toggle), now focused")
        self.t.keys("Tab"); time.sleep(0.4)
        self.assertEqual(self.preview_open_and_focused(), (True, False), "tab leaves the pass-through; the preview stays open")

    def test_s_on_a_card_without_a_pane_does_nothing(self):
        self.start()
        S = json.loads((self.env.fake / "show.json").read_text())
        for x in S:
            if x["id"] == "c1": x.update(tmuxPane=None)  # a native session that lost its pane (a background session would get one: T60)
        self.env.write_show(S); self.assertTrue(wait(lambda: self.env.show_calls() >= 2, 4)); time.sleep(2.5)
        for k in ("l", "l", "j", "k", "h", "l"):
            if any("charlie-30" in c.line(1) and C_FOCUS in border_colours(c) for c in find_cards(self.t.snap())): break
            self.t.keys(k); time.sleep(0.3)
        self.assertTrue(any("charlie-30" in c.line(1) and C_FOCUS in border_colours(c) for c in find_cards(self.t.snap())), self.t.text())
        n = len(self.env.tmux_calls())
        self.t.keys("i"); time.sleep(0.5)
        self.assertEqual(self.preview_open_and_focused(), (False, False), "no preview")
        self.assertIn("no tmux pane: can't jump or send", foot(self.t)); self.assertEqual(foot(self.t).count("no tmux pane: can't jump or send"), 1)
        self.t.lit("zzz"); time.sleep(0.3)
        self.assertEqual(self.sent_text(), "", "keys are not forwarded anywhere"); self.assertEqual(self.env.calls(), [])

    def test_s_below_100_columns_hints_and_does_nothing(self):
        self.start(); self.t.close()
        t = Term(self.env, w=99, h=50, extra_env="TMUX_PANE=%9"); self.addCleanup(t.close)
        self.assertTrue(ready(t) or wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) >= 1, 5))
        t.keys("i"); time.sleep(0.5)
        self.assertIn("at least 100 columns", "\n".join(r.text for r in t.snap()[-3:]))
        t.lit("zzz"); time.sleep(0.3); self.assertEqual(self.sent_text(), "")

    def test_paste_goes_through_load_buffer_and_paste_buffer(self):
        self.start()
        self.t.keys("i"); time.sleep(0.6)
        self.t.lit("\x1b[200~first line\nsecond; line\x1b[201~"); time.sleep(0.8)
        c = [x for x in self.env.tmux_calls() if x[0] in ("load-buffer", "paste-buffer", "send-keys")]
        self.assertEqual(c, [["load-buffer", "-b", "agmux", "-"], ["paste-buffer", "-p", "-d", "-b", "agmux", "-t", A1_PANE]],
                         "a paste keeps bracketed paste: the text goes in through stdin, then paste-buffer -p -d; no send-keys")
        self.assertEqual(self.env.tmux_stdin(), "first line\nsecond; line", "the buffer holds exactly what was pasted")

    def test_forwarding_failure_is_shown_on_the_message_line(self):
        self.start()
        self.t.keys("i"); time.sleep(0.6)
        (self.env.fake / "tmux.fail").write_text("send-keys")
        self.t.lit("x"); time.sleep(0.8)
        self.assertIn("boom from fake tmux", foot(self.t)); self.assertNotIn("second tmux line", self.t.text()); self.assertTrue(self.t.alive())

    def test_footer_says_reply(self):
        self.start()
        bar = last_text_row(self.t.snap())
        self.assertIn("i reply", bar); self.assertNotIn("i send", bar)

    def test_blank_and_cancel_prompts_are_gone(self):  # the old prompt is deleted: `s` then Esc is just Esc going to the pane
        self.start()
        self.t.keys("i"); time.sleep(0.5); self.t.esc(); time.sleep(0.4)
        self.assertTrue(self.t.alive(), "esc goes to the pane, it does not quit")
        self.assertEqual(self.sent_text(), "[Escape]")

    def test_enter_comes_at_least_150ms_after_the_text(self):
        self.start()
        self.t.keys("H"); c = self.tcalls(2)
        self.assertEqual([x[0] for x in c], ["send-keys", "send-keys"])
        wait(lambda: len(self.env.tmux_times()) >= 2, 3)  # the fake logs argv first, then spends a perl start on the timestamp: calls can be visible before their time is
        times = self.env.tmux_times()
        self.assertGreaterEqual(len(times), 2)
        self.assertGreaterEqual(times[1] - times[0], 0.14, f"Enter must wait ~150ms after the text (so Claude Code does not read it as paste): {times}")
        self.assertLess(times[1] - times[0], 1.0)

    def test_H_hands_over(self):
        self.start()
        self.t.keys("H")
        c = self.tcalls(2)
        self.assertEqual(c, [["send-keys", "-t", A1_PANE, "-l", "--", "/handoff"], ["send-keys", "-t", A1_PANE, "Enter"]])
        self.assertEqual(self.env.calls(), []); self.assertTrue(self.t.alive())

    def test_send_failure_shows_first_line_and_skips_the_enter(self):
        self.start(fail="send-keys")
        self.t.keys("H")
        self.assertTrue(wait(lambda: "boom from fake tmux" in foot(self.t), 3), self.t.text())
        self.assertNotIn("second tmux line", self.t.text())
        self.assertEqual(len(self.env.tmux_calls()), 1, "if the text did not go, Enter must not be sent either"); self.assertTrue(self.t.alive())

    def test_r_ends_the_process_and_types_the_resume_command_into_its_pane(self):
        self.start()
        self.t.keys("r"); self.assertTrue(wait(lambda: ["send-keys", "-t", "%101", "Enter"] in self.env.tmux_calls(), 8), self.env.tmux_calls())
        self.assertEqual(self.env.tmux_calls(), [["send-keys", "-t", "%101", "-l", "--", "claude --resume a1"], ["send-keys", "-t", "%101", "Enter"]]); self.assertEqual(self.env.killed(), ["a1"]); self.assertEqual(self.env.calls(), [])

    def test_x_ends_the_process_and_touches_neither_tmux_nor_anything_else(self):
        self.start(); self.t.keys("x"); time.sleep(0.25); self.t.keys("y"); self.assertTrue(wait(lambda: self.env.killed() == ["a1"], 5), self.env.killed())
        time.sleep(0.5); self.assertEqual(self.env.tmux_calls(), [], "x signals the process: tmux is not asked"); self.assertEqual(self.env.calls(), [])

    def test_d_runs_hunk_at_the_root_of_the_repo_and_touches_neither_tmux_nor_a_signal(self):
        self.start(); self.env.git_repo(self.env.home / "work" / "alpha"); self.t.keys("d"); self.assertTrue(wait(lambda: self.env.calls() == [["hunk", "diff", "--watch"]], 5), self.env.calls())
        self.assertEqual(os.path.realpath((self.env.fake / "hunk.cwd").read_text().strip()), os.path.realpath(self.env.home / "work" / "alpha")); self.assertEqual(self.env.tmux_calls(), []); self.assertEqual(self.env.killed(), [])

    def test_burst_keys_keep_their_order_with_tmux_calls(self):
        self.start()
        self.t.lit("HHj")
        c = self.tcalls(4)
        self.assertEqual([x[0] for x in c], ["send-keys"] * 4)
        self.assertEqual([x[-1] for x in c], ["/handoff", "Enter", "/handoff", "Enter"])


# ------------------------------------------------------------------------------------------------------------------------------------------
class PtyClient:
    """A real tmux client attached to `session` through a pty (tmux needs a client to show popups / to switch)."""

    def __init__(self, session, w=200, h=50):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            os.execvpe("tmux", ["tmux", "-L", SOCK, "attach", "-t", session], {"TERM": "xterm-256color", "PATH": os.environ["PATH"], "LANG": "en_US.UTF-8"})
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", h, w, 0, 0))
        threading.Thread(target=self._drain, daemon=True).start()
        self.name = wait(lambda: self._find(session), 5)

    def _drain(self):
        try:
            while os.read(self.fd, 65536):
                pass
        except OSError:
            pass

    @staticmethod
    def _find(session):
        for line in tm("list-clients", "-F", "#{client_name}\t#{client_session}").stdout.splitlines():
            n, s = line.split("\t")
            if s == session:
                return n

    def close(self):
        try:
            os.close(self.fd); os.kill(self.pid, 9); os.waitpid(self.pid, 0)
        except OSError:
            pass


def clients():
    return {n: s for n, s in (l.split("\t") for l in tm("list-clients", "-F", "#{client_name}\t#{client_session}").stdout.splitlines())}


class RealTmux(unittest.TestCase):
    """Real tmux on the private socket. TMUX / TMUX_PANE reach agmux because the pane lives in this very server."""

    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env)
        tm("kill-server"); self.addCleanup(lambda: tm("kill-server"))
        self.target_cmd = "cat"
        tm("start-server", ";", "set", "-g", "status", "off", ";", "set", "-g", "remain-on-exit", "off", ";",
           "new-session", "-d", "-s", "main", "-x", "200", "-y", "50", "sleep 600", ";",
           "new-session", "-d", "-s", "target", "-x", "100", "-y", "30", self.target_cmd)
        self.pane = tm("display", "-p", "-t", "target", "#{pane_id}").stdout.strip()
        self.assertRegex(self.pane, r"^%\d+$")
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            if s["id"] == "a1":
                s["tmuxPane"] = self.pane  # the fixture's a1 now lives in the real `cat` pane
        self.env.write_show(S)
        # agmux sees: the fake ccmux, the REAL tmux, and (through the pane's environment) $TMUX / $TMUX_PANE
        q = shlex.quote
        self.agmux = (f"env PATH={q(str(self.env.bin_real))}:/opt/homebrew/bin:/usr/bin:/bin HOME={q(str(self.env.home))} "
                      f"XDG_STATE_HOME={q(str(self.env.state))} TERM=xterm-256color COLORTERM=truecolor LANG=en_US.UTF-8 {q(BIN)} board")

    def pane_text(self):
        return tm("capture-pane", "-p", "-t", self.pane).stdout

    def drive(self, session):
        """keystrokes into agmux's own pane"""
        return lambda *k: tm("send-keys", "-t", session, *k)

    def wait_board(self, session):
        self.assertTrue(wait(lambda: "agmux · 9 sessions" in tm("capture-pane", "-p", "-t", session).stdout, 8), tm("capture-pane", "-p", "-t", session).stdout)

    def nested(self, agmux_client, target_in_float=False, via_session_env=False):
        """outer client on `main`; a popup on it runs `attach -t fl-x`; agmux runs in fl-x's first pane (which keeps a shell after it exits)"""
        outer = PtyClient("main"); self.addCleanup(outer.close); self.assertTrue(outer.name, "outer client never attached")
        env_c = f"AGMUX_CLIENT={shlex.quote(outer.name)} " if agmux_client else ""
        agmux = self.agmux.replace("env PATH=", f"env {env_c}PATH=", 1)
        tm("new-session", "-d", "-s", "fl-x", "-x", "180", "-y", "45", f"sh -c '{agmux}; sleep 600'")
        if target_in_float:  # the target pane lives in the float's OWN session
            pane = tm("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "fl-x", self.target_cmd).stdout.strip()
            S = json.loads((self.env.fake / "show.json").read_text())
            for x in S:
                if x["id"] == "a1":
                    x["tmuxPane"] = pane
            self.env.write_show(S); self.pane = pane
        if via_session_env:  # the launcher's way (T15): the board is already running in the resident session; its process environment cannot be reached
            tm("set-environment", "-h", "-t", "fl-x", "AGMUX_CLIENT", outer.name)  # -h: hidden, so panes opened later do NOT inherit it as a process variable
            probe = tm("new-window", "-d", "-P", "-F", "#{pane_id}", "-t", "fl-x", "sh -c 'env | grep -c ^AGMUX_CLIENT= > /tmp/agmux-e2e-probe.$$; sleep 1'").stdout.strip()
            time.sleep(0.5)
            leaked = subprocess.run("cat /tmp/agmux-e2e-probe.* 2>/dev/null; rm -f /tmp/agmux-e2e-probe.*", shell=True, capture_output=True, text=True).stdout.split()
            self.assertEqual(leaked, ["0"], "a pane opened after `set-environment -h` must not have AGMUX_CLIENT in its process environment")
            tm("kill-window", "-t", probe)
        pop = subprocess.Popen(["tmux", "-L", SOCK, "display-popup", "-c", outer.name, "-B", "-x", "0", "-y", "0", "-w", "190", "-h", "48", "-E",
                                f"env -u TMUX tmux -L {SOCK} attach -t fl-x"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(lambda: pop.poll() is None and pop.kill())
        self.assertTrue(wait(lambda: "fl-x" in clients().values(), 5), f"float never attached: {clients()}")
        self.wait_board("fl-x")
        float_name = next(n for n, sess in clients().items() if sess == "fl-x")
        self.assertEqual(sorted(clients().values()), ["fl-x", "main"], "outer client on main, float client on fl-x")
        if target_in_float:
            self.assertTrue(wait(lambda: self.pane in tm("list-panes", "-s", "-t", "fl-x", "-F", "#{pane_id}").stdout, 3))
            time.sleep(2.2)  # the board polls the fake ccmux every 2s: a1 now points at the new pane
        return outer, pop, float_name

    def test_nested_float_without_agmux_client_switches_but_leaves_the_popup(self):
        outer, pop, float_name = self.nested(agmux_client=False)
        self.drive("fl-x")("Enter")  # a1 -> the real `cat` pane
        self.assertTrue(wait(lambda: clients().get(outer.name) == "target", 5), f"outer client did not switch to the target: {clients()}")
        time.sleep(0.8)
        self.assertEqual(clients().get(float_name), "fl-x", "without $AGMUX_CLIENT the float is NOT detached (the heuristic must not kick a real terminal)")
        self.assertIsNone(pop.poll(), "the popup stays open")
        self.assertEqual(tm("has-session", "-t", "fl-x").returncode, 0); self.assertEqual(self.env.calls(), [], "no ccmux switch")

    def test_nested_float_with_agmux_client_switches_and_closes_the_popup(self):
        outer, pop, float_name = self.nested(agmux_client=True)
        self.drive("fl-x")("Enter")
        self.assertTrue(wait(lambda: clients().get(outer.name) == "target", 5), f"outer client did not switch to the target: {clients()}")
        self.assertTrue(wait(lambda: float_name not in clients(), 5), f"float client still attached: {clients()}")
        self.assertTrue(wait(lambda: pop.poll() is not None, 5), "popup must close")
        self.assertEqual(tm("has-session", "-t", "fl-x").returncode, 0, "the fl session survives")
        self.assertEqual(clients().get(outer.name), "target", "the outer client was not kicked")
        self.assertEqual(self.env.calls(), [], "no ccmux switch")

    def test_session_environment_closes_the_popup_like_the_process_environment(self):
        outer, pop, float_name = self.nested(agmux_client=False, via_session_env=True)
        self.drive("fl-x")("Enter")
        self.assertTrue(wait(lambda: clients().get(outer.name) == "target", 5), f"outer client did not switch to the target: {clients()}")
        self.assertTrue(wait(lambda: float_name not in clients(), 5), f"float client still attached: {clients()}")
        self.assertTrue(wait(lambda: pop.poll() is not None, 5), "popup closes")
        self.assertEqual(tm("has-session", "-t", "fl-x").returncode, 0, "the fl session survives"); self.assertEqual(clients().get(outer.name), "target")

    def test_target_inside_the_float_session_keeps_the_outer_client_attached(self):
        outer, pop, float_name = self.nested(agmux_client=True, target_in_float=True)
        self.drive("fl-x")("Enter")
        self.assertTrue(wait(lambda: clients().get(outer.name) == "fl-x", 5), f"outer client should now show the fl session (the target is in it): {clients()}")
        self.assertTrue(wait(lambda: float_name not in clients(), 5), f"the float client is detached by name: {clients()}")
        self.assertTrue(wait(lambda: pop.poll() is not None, 5), "popup closes")
        time.sleep(0.8)
        self.assertEqual(clients().get(outer.name), "fl-x", "a `detach-client -s fl-x` would have kicked the outer client too; -t <float> must not")
        self.assertEqual(tm("has-session", "-t", "fl-x").returncode, 0)

    def test_s_and_H_reach_the_real_pane(self):
        tm("new-session", "-d", "-s", "fl-x", "-x", "232", "-y", "50", f"sh -c '{self.agmux}; sleep 600'")
        self.wait_board("fl-x")
        d = self.drive("fl-x")
        d("i"); time.sleep(0.6)
        tm("send-keys", "-t", "fl-x", "-l", "--", "你好 world"); time.sleep(0.3); d("Enter")
        self.assertTrue(wait(lambda: self.pane_text().count("你好 world") >= 2, 5), f"cat did not echo it:\n{self.pane_text()}")
        tm("send-keys", "-t", "fl-x", "-l", "--", "--help -x"); time.sleep(0.3); d("Enter")
        self.assertTrue(wait(lambda: self.pane_text().count("--help -x") >= 2, 5), self.pane_text())
        d("Tab"); time.sleep(0.4); d("H")  # tab back to the cards, then the hand-over
        self.assertTrue(wait(lambda: self.pane_text().count("/handoff") >= 2, 5), self.pane_text())
        lines = [l for l in self.pane_text().splitlines() if l.strip()]
        self.assertEqual(lines[:6], ["你好 world", "你好 world", "--help -x", "--help -x", "/handoff", "/handoff"],
                         "cat echoes each line (tty echo, then cat's output): intact, in order, each ended by Enter")
        self.assertEqual(self.env.calls(), [], "no ccmux send")

    def test_ordinary_pane_with_one_terminal(self):
        tm("new-session", "-d", "-s", "plain", "-x", "180", "-y", "45", f"sh -c '{self.agmux}; sleep 600'")
        only = PtyClient("plain"); self.addCleanup(only.close); self.assertTrue(only.name)
        self.wait_board("plain")
        self.drive("plain")("Enter")
        self.assertTrue(wait(lambda: clients().get(only.name) == "target", 5), f"the only client should follow to the target: {clients()}")
        self.assertEqual(tm("has-session", "-t", "plain").returncode, 0, "own session is not detached or killed")
        self.assertIn(only.name, clients(), "the client itself stays attached (to the target session now)")


if __name__ == "__main__":
    unittest.main()
