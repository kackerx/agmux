"""T17 config file (keys + colours). Spec: specs/m3-config/tasks.md T17, design.md「配置」.

Every test points XDG_CONFIG_HOME at its own temp dir (HOME is a temp dir too, so the user's real ~/.config/agmux is never read).
No config file = the defaults (the T3/T4/T7/T9/T10/T12 suites all run without one and stay green).
"""
import subprocess, unittest
from lib import *
from test_t11 import PreviewBase, pane_text, CAT_CMD
from test_t3 import ready, last_text_row, border_colours, STATUS_COLOUR, C_FOCUS, find_cards, by_name

DEFAULT_KEYS = ["up", "down", "left", "right", "page_down", "page_up", "jump", "fold", "preview", "focus_preview", "leave_preview",
                "reply", "handoff", "search", "next_match", "prev_match", "flash", "group", "restart", "kill", "review", "avatar", "quit", "exit", "confirm"]
DEFAULT_COLOURS = ["working", "idle", "done", "attention", "focus", "match", "border", "name", "branch", "model", "effort", "agent", "dir",
                   "bar_low", "bar_mid", "bar_high", "bar_empty", "muted", "disabled"]


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        self.file = self.cfg / "agmux" / "config.toml"

    def write(self, text):
        self.file.write_text(text)

    def board(self, w=200, h=50):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(ready(t), t.text())
        return t

    def cli(self, *args):
        """run `agmux <args>` with the temp config dir; returns CompletedProcess (stdout/stderr as text)"""
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state),
               "XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-256color", "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def jump_target(self, t):
        t.keys("Enter"); wait(lambda: self.env.actions(), 3)
        a = self.env.actions(); return a[0] if a else None


class Defaults(Base):
    def test_no_file_means_default_keys(self):
        t = self.board()
        for k in ("j", "Down"):
            t.keys(k); time.sleep(0.15)
        self.assertEqual(self.jump_target(t), ["switch", "a3"])

    def test_no_file_footer_is_the_default_one(self):
        t = self.board(); bar = last_text_row(t.snap())
        for want in ("j/k h/l move", "enter jump", "f fold", "p preview", "tab focus preview", "i reply", "H handoff", "r restart", "x kill", "d review", "q quit"):
            self.assertIn(want, bar)

    def test_config_prints_the_defaults_and_path(self):
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue(r.stdout.startswith("[keys]\n"), r.stdout)
        keys = r.stdout.split("[colors]")[0]; cols = r.stdout.split("[colors]")[1].split("[layout]")[0]  # [layout] (T25) and [avatars] (T18) follow the colours
        self.assertEqual([l.split(" = ")[0] for l in keys.splitlines()[1:] if l], DEFAULT_KEYS)
        self.assertEqual([l.split(" = ")[0] for l in cols.splitlines() if l], DEFAULT_COLOURS)
        for line in ('down = ["j", "down"]', 'reply = ["i"]', 'flash = ["s"]', 'focus_preview = ["tab"]', 'leave_preview = ["tab"]', 'quit = ["q", "esc"]', 'next_match = ["n"]', 'prev_match = ["N"]', 'working = "#ffb86c"', 'match = "#f2d06b"', 'name = "5"'):
            self.assertIn(line, r.stdout)
        p = self.cli("config", "--path"); self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(p.stdout.strip(), str(self.file), "--path: $XDG_CONFIG_HOME/agmux/config.toml")

    def test_path_without_xdg_falls_back_to_home_dot_config(self):
        env = {"PATH": "/usr/bin:/bin", "HOME": str(self.env.home)}
        r = subprocess.run([BIN, "config", "--path"], env=env, capture_output=True, text=True, timeout=10)
        self.assertEqual(r.stdout.strip(), str(self.env.home / ".config" / "agmux" / "config.toml"), r.stderr)

    def test_s_no_longer_opens_the_passthrough(self):  # T17: reply moved to i; s is the T20 flash: it must not enter the pass-through or call anything
        t = self.board(); t.keys("s"); time.sleep(0.6)
        self.assertNotIn("keys go to the pane", last_text_row(t.snap()))
        self.assertTrue(t.alive()); self.assertEqual(self.env.actions(), [], "s does nothing: no ccmux / tmux call")
        self.assertEqual(self.env.tmux_calls(), [], "s must not touch tmux (no send-keys, no switch)")


class Keys(Base):
    def test_rebinding_down_replaces_the_default_keys(self):
        self.write('[keys]\ndown = ["n", "down"]\n')
        t = self.board()
        t.keys("j"); time.sleep(0.4)
        self.assertEqual(self.jump_target(t), ["switch", "a1"], "j is no longer bound: focus did not move")

    def test_new_key_moves_focus_and_arrow_still_does(self):
        self.write('[keys]\ndown = ["n", "down"]\n')
        t = self.board()
        t.keys("n"); time.sleep(0.2); t.keys("Down"); time.sleep(0.2)
        self.assertEqual(self.jump_target(t), ["switch", "a3"])

    def test_footer_follows_the_config(self):
        self.write('[keys]\ndown = ["n", "down"]\nreply = ["r"]\nrestart = ["R"]\n')
        t = self.board(); bar = last_text_row(t.snap())
        self.assertIn("n/k h/l move", bar, bar)
        self.assertIn("r reply", bar); self.assertIn("R restart", bar)
        self.assertNotIn("j/k", bar)

    def test_reply_default_is_i_and_configurable(self):
        t = self.board(); self.assertIn("i reply", last_text_row(t.snap())); t.close()
        self.write('[keys]\nreply = ["e"]\n')
        t = self.board(); bar = last_text_row(t.snap())
        self.assertIn("e reply", bar); self.assertNotIn("i reply", bar)

    def test_quit_rebound_to_z(self):
        self.write('[keys]\nquit = ["z"]\n')
        t = self.board(); t.keys("q"); time.sleep(0.5); self.assertTrue(t.alive(), "q is no longer quit")
        t.keys("z"); self.assertEqual(t.exit_status(), 0)

    def test_ctrl_c_on_the_cards_always_quits_even_if_quit_is_rebound(self):  # orchestrator: not configurable; in the pass-through it goes to the pane (PreviewKeys)
        for cfg in ("", '[keys]\nquit = ["z"]\n'):
            with self.subTest(config=cfg):
                self.write(cfg)
                t = self.board(); t.keys("C-c")
                self.assertEqual(t.exit_status(), 0); self.assertEqual(self.env.actions(), [])
                t.close()

    def test_modifier_key_binding(self):  # ctrl+<letter> is a valid key name
        self.write('[keys]\ndown = ["ctrl+n"]\n')
        t = self.board(); t.keys("C-n"); time.sleep(0.3)
        self.assertEqual(self.jump_target(t), ["switch", "a2"])


class KeysReview(Base):
    """T17 review (f24bbe5): an explicit binding takes the key from the default of another action; the one rule left is a conflict between two explicit ones."""

    def test_up_j_takes_j_from_down_and_the_footer_follows(self):
        self.write('[keys]\nup = ["j"]\n')
        out = self.cli("config"); self.assertEqual(out.returncode, 0, out.stderr)
        self.assertIn('up = ["j"]', out.stdout); self.assertIn('down = ["down"]', out.stdout, "down lost its j, kept the arrow")
        t = self.board(); bar = last_text_row(t.snap())
        self.assertIn("down/j h/l move", bar, bar); self.assertNotIn("j/k", bar)
        t.keys("Down"); time.sleep(0.2); t.keys("Down"); time.sleep(0.2)  # a3
        t.keys("k"); time.sleep(0.2)  # k is nothing any more (up was replaced, not extended)
        t.keys("j"); time.sleep(0.2)  # now moves UP: a2
        self.assertEqual(self.jump_target(t), ["switch", "a2"])

    def test_two_explicit_bindings_of_one_key_still_conflict(self):
        for text, bad in (('[keys]\nup = ["n"]\ndown = ["n"]\n', '"n" is already bound to up'), ('[keys]\nup = ["j", "k"]\ndown = ["j"]\n', '"j" is already bound to up')):
            with self.subTest(text):
                self.write(text); r = self.cli("config"); self.assertEqual(r.returncode, 1); self.assertIn(bad, r.stderr)

    def test_confirm_enter_is_valid_and_confirms(self):
        self.write('[keys]\nconfirm = ["enter"]\n')
        t = self.board(); t.keys("x"); time.sleep(0.3); t.keys("y"); time.sleep(0.6)
        self.assertEqual(self.env.actions(), [], "y is not the confirm key any more: it cancels")
        t.keys("x"); time.sleep(0.3); t.keys("Enter")
        self.assertTrue(wait(lambda: self.env.actions(), 3), t.text()); self.assertEqual(self.env.actions(), [["kill", "a1"]], "enter confirms the kill")
        t.close(); self.env.calls() and (self.env.fake / "calls.log").unlink()
        t = self.board(); self.assertEqual(self.jump_target(t), ["switch", "a1"], "outside the prompt enter is still jump")

    def test_alt_space_binding_folds(self):
        self.write('[keys]\nfold = ["alt+space"]\n')
        t = self.board(); self.assertIn("alt+space fold", last_text_row(t.snap()))
        t.keys("f"); time.sleep(0.4); self.assertIn("▾ ~/work/alpha (3)", t.text(), "f is no longer fold")
        t.keys("M-Space"); time.sleep(0.5)
        self.assertIn("▸ ~/work/alpha (3)", t.text(), t.text()); self.assertEqual(len(find_cards(t.snap())), 6, "the alpha group is folded: 3 cards gone")
        t.keys("M-Space"); time.sleep(0.5); self.assertIn("▾ ~/work/alpha (3)", t.text())


class NoKeyLeft(Base):
    """c1b8fcb: a default action whose only default key was taken by the user's own binding is simply unbound (no error); only the user writing an empty list is an error."""

    def test_down_x_leaves_kill_unbound(self):
        self.write('[keys]\ndown = ["x"]\n')
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("# kill: no key (another action took its default)", r.stdout); self.assertNotIn("kill = ", r.stdout)
        t = self.board(); bar = last_text_row(t.snap())
        self.assertNotIn("x kill", bar, bar)
        t.keys("x"); time.sleep(0.3)  # moves down now
        t.keys("y"); time.sleep(0.5)
        self.assertEqual(self.env.actions(), [], "x is not kill: no confirm prompt, y has nothing to confirm")
        self.assertEqual(self.jump_target(t), ["switch", "a2"], "x moved the focus down one card")

    def test_the_dump_with_a_commented_action_loads_again_and_is_stable(self):
        self.write('[keys]\ndown = ["x"]\n')
        out = self.cli("config").stdout; self.write(out)
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout, out, "pasting the dump back changes nothing")
        t = self.board(); self.assertNotIn("x kill", last_text_row(t.snap()))

    def test_user_written_empty_list_is_still_an_error(self):
        self.write('[keys]\nkill = []\n')
        for args in (["config"], ["board"]):
            r = self.cli(*args); self.assertEqual(r.returncode, 1); self.assertIn("keys.kill: needs at least one key", r.stderr)

    def test_fold_a_starts_and_avatar_has_no_key(self):
        self.write('[keys]\nfold = ["a"]\n')
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn("# avatar: no key (another action took its default)", r.stdout); self.assertIn('fold = ["a"]', r.stdout)
        self.write(r.stdout); self.assertEqual(self.cli("config").stdout, r.stdout)
        t = self.board(); bar = last_text_row(t.snap()); self.assertIn("a fold", bar); self.assertNotIn("a avatar", bar)
        t.keys("a"); time.sleep(0.4); self.assertIn("▸ ~/work/alpha (3)", t.text(), "a folds the group now"); self.assertNotIn("avatar for", t.text())

    def test_two_explicit_bindings_of_x_are_still_an_error(self):
        self.write('[keys]\nflash = ["x"]\nkill = ["x"]\n')
        r = self.cli("config"); self.assertEqual(r.returncode, 1); self.assertIn('"x" is already bound to flash', r.stderr)


class Colours(Base):
    def test_working_border_colour(self):
        self.write('[colors]\nworking = "#123456"\n')
        t = self.board(); cs = find_cards(t.snap())
        self.assertEqual(border_colours(by_name(cs, "bravo-29")), {("rgb", 18, 52, 86)}, "b1 is working: its border takes #123456")
        self.assertEqual(border_colours(by_name(cs, "charlie-59")), {STATUS_COLOUR["done"]}, "other statuses keep their colours")
        raw = t.raw(); self.assertIn("18;52;86", raw)

    def test_focus_colour(self):
        self.write('[colors]\nfocus = "#00ff00"\n')
        t = self.board(); a1 = by_name(find_cards(t.snap()), "alpha-main")
        self.assertEqual(border_colours(a1), {("rgb", 0, 255, 0)})
        self.assertNotIn(C_FOCUS, set().union(*(border_colours(c) for c in find_cards(t.snap()))))

    def test_ansi_index_colour_is_accepted(self):
        self.write('[colors]\nworking = "9"\n')
        self.board()  # no error: it is a valid ANSI index; ready() already asserted the 9 cards drew

    def test_colours_without_a_file_are_the_defaults(self):
        t = self.board(); cs = find_cards(t.snap())
        self.assertEqual(border_colours(by_name(cs, "bravo-29")), {STATUS_COLOUR["working"]})


class Errors(Base):
    CASES = {
        "duplicate": ('[keys]\nquit = ["j"]\ndown = ["j"]\n', "keys.quit", '"j" is already bound to down'),  # both are the user's own: that is the only conflict left
        "ctrl+c": ('[keys]\nquit = ["ctrl+c"]\n', "keys.quit", "ctrl+c can't be bound (it always quits)"),
        "ctrl+c on another action": ('[keys]\nfold = ["ctrl+c"]\n', "keys.fold", "ctrl+c can't be bound"),
        "ctrl+i is tab": ('[keys]\nfold = ["ctrl+i"]\n', "keys.fold", 'write "tab"'),
        "ctrl+m is enter": ('[keys]\nfold = ["ctrl+m"]\n', "keys.fold", 'write "enter"'),
        "keys is not a table": ("keys = 3\n", "keys", "must be a table"),
        "colors is not a table": ('colors = "dark"\n', "colors", "must be a table"),
        "duplicate-between-two-new-keys": ('[keys]\ndown = ["n"]\nup = ["n"]\n', "keys.down", '"n" is already bound to up'),
        "bad colour": ('[colors]\nworking = "#12345"\n', "colors.working", "#rrggbb"),
        "colour out of range": ('[colors]\nworking = "256"\n', "colors.working", "0-255"),
        "unknown colour": ('[colors]\nnope = "#123456"\n', "colors.nope", "unknown color"),
        "unknown action": ('[keys]\nnope = ["n"]\n', "keys.nope", "unknown action"),
        "unknown section": ('[extra]\na = 1\n', "extra", "unknown setting"),
        "bad key name": ('[keys]\ndown = ["bogus-key"]\n', "keys.down", "not a key name"),
        "empty key list": ('[keys]\ndown = []\n', "keys.down", "at least one key"),
        "toml syntax": ('[keys\n', "", ""),
    }

    def test_each_error_is_one_line_on_stderr_and_exit_1(self):
        for name, (text, setting, why) in self.CASES.items():
            with self.subTest(name):
                self.write(text)
                for args in (["board"], ["config"]):
                    r = self.cli(*args)
                    self.assertEqual(r.returncode, 1, f"{args}: {r.stdout!r} {r.stderr!r}")
                    lines = r.stderr.rstrip("\n").split("\n")
                    self.assertEqual(len(lines), 1, f"one line, got {r.stderr!r}")
                    self.assertTrue(lines[0].startswith("agmux: "), lines[0]); self.assertIn(str(self.file), lines[0])
                    self.assertIn(setting, lines[0]); self.assertIn(why, lines[0])
                    self.assertEqual(r.stdout, "", "nothing on stdout (no alt-screen escape, no config dump)")

    def test_a_broken_config_never_takes_the_screen_over(self):
        self.write('[keys]\nquit = ["j"]\ndown = ["j"]\n')
        t = Term(self.env, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertEqual(t.exit_status(), 1)
        self.assertEqual(tm("display", "-p", "-t", t.name, "#{alternate_on}").stdout.strip(), "0", "no alternate screen was entered")
        hist = tm("capture-pane", "-p", "-S", "-", "-t", t.name).stdout  # the pane is dead: the message has scrolled into the history
        self.assertIn("already bound", hist); self.assertNotIn("agmux ·", hist)

    def test_missing_file_and_empty_file_are_fine(self):
        self.assertEqual(self.cli("config").returncode, 0)
        self.write(""); self.assertEqual(self.cli("config").returncode, 0)
        self.write("# just a comment\n"); self.assertEqual(self.cli("config").returncode, 0)

    def test_config_usage_error(self):
        r = self.cli("config", "--bogus"); self.assertEqual(r.returncode, 2); self.assertIn("usage: agmux config", r.stderr)


class RoundTrip(Base):
    def test_config_output_written_back_behaves_the_same(self):
        self.write('[keys]\ndown = ["n", "down"]\n[colors]\nworking = "#123456"\n')
        out = self.cli("config").stdout
        self.assertIn('down = ["n", "down"]', out); self.assertIn('working = "#123456"', out)
        self.assertIn('up = ["k", "up"]', out, "the dump is the whole effective config: defaults included")
        self.write(out)
        again = self.cli("config").stdout; self.assertEqual(again, out, "idempotent")
        t = self.board(); t.keys("n"); time.sleep(0.3)
        self.assertEqual(border_colours(by_name(find_cards(t.snap()), "bravo-29")), {("rgb", 18, 52, 86)})
        self.assertEqual(self.jump_target(t), ["switch", "a2"])

    def test_defaults_written_back_are_valid(self):
        out = self.cli("config").stdout; self.write(out)
        r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout, out)


class PreviewKeys(PreviewBase):
    """Real panes (a3 = raw `cat -v` look-alike) behind a board that reads a config: the pass-through intercepts only leave_preview."""
    PANES = (("a1", CAT_CMD), ("a2", CAT_CMD), ("a3", CAT_CMD))
    CONFIG = '[keys]\ndown = ["n", "down"]\nleave_preview = ["ctrl+g"]\n'

    def board(self, w, h):
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)
        (self.cfg / "agmux" / "config.toml").write_text(self.CONFIG)
        t = Term(self.env, w=w, h=h, real_tmux=True, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: "agmux ·" in t.text(), 8), t.text())
        return t

    def cat(self, sid="a3"):
        return "".join(tm("capture-pane", "-p", "-J", "-t", self.panes[sid]).stdout.split("\n")).rstrip()

    def focused(self):
        p = self.pv(); return bool(p) and p[4][p[2]].cells[p[3]][1] == C_FOCUS

    def to_a3_passthrough(self, key="i"):
        self.press("n", "n", key)  # the rebound down key
        self.assertTrue(wait(self.focused, 3), self.t.text())

    def test_rebound_down_key_moves_focus_and_footer_shows_it(self):
        self.press("n"); self.press("p")
        self.assertTrue(wait(lambda: self.pv() is not None, 4), self.t.text())
        self.assertIn("pj-fallback", self.pv()[0], "n moved focus a1 -> a2 (pj-fallback); the preview title names the focused session")
        self.assertIn("n/k h/l move", last_text_row(self.t.snap()))

    def test_s_starts_the_flash_not_the_passthrough_and_i_enters_it(self):  # since T20 s is the global jump; reply (pass-through) is i
        self.press("n", "n", "p"); self.assertTrue(wait(lambda: self.pv() is not None, 4))
        self.press("s"); time.sleep(0.5)
        self.assertIn("flash: type to narrow", last_text_row(self.t.snap()), "s starts the flash (T23: type a substring first)")
        self.assertFalse(self.focused(), "s does not give the keys to the pane"); self.assertEqual(self.cat(), "", "and s is not typed into the pane")
        self.t.esc(); time.sleep(0.4); self.assertNotIn("flash:", last_text_row(self.t.snap()))
        self.press("i"); self.assertTrue(wait(self.focused, 3), self.t.text())

    def test_only_the_configured_leave_key_is_intercepted(self):
        self.to_a3_passthrough()
        for k in ("C-]", "n", "j", "q"):  # default leave key, the rebound key, the old key and the quit key all belong to the pane now
            self.press(k)
        self.assertTrue(wait(lambda: self.cat() == "^]njq", 3), f"pane got {self.cat()!r}")
        self.assertTrue(self.focused() and self.t.alive(), "none of those left the pass-through or quit agmux")
        self.press("C-g")
        self.assertTrue(wait(lambda: not self.focused(), 3), self.t.text())
        self.assertEqual(self.cat(), "^]njq", "ctrl+g was taken by agmux, not forwarded")
        self.assertIn("tab focus preview", last_text_row(self.t.snap()))

    def test_tab_and_ctrl_c_are_forwarded(self):
        self.to_a3_passthrough()
        self.press("Tab"); self.press("C-c")
        self.assertTrue(wait(lambda: self.cat() == "^I^C", 3), f"pane got {self.cat()!r}")
        self.assertTrue(self.focused() and self.t.alive())

    def test_footer_in_the_passthrough_names_the_configured_leave_key(self):
        self.to_a3_passthrough()
        self.assertIn("ctrl+g back to the cards", last_text_row(self.t.snap()))


class LeaveWithTab(PreviewBase):
    """T21: tab is both focus_preview and leave_preview by default (config says nothing here): tab in, tab out, ctrl+] is just a key for the pane."""
    PANES = (("a1", CAT_CMD), ("a2", CAT_CMD), ("a3", CAT_CMD))
    CONFIG = ""
    cat, focused = PreviewKeys.cat, PreviewKeys.focused

    def board(self, w, h):
        cfg = self.env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True)
        (cfg / "agmux" / "config.toml").write_text(self.CONFIG)
        t = Term(self.env, w=w, h=h, real_tmux=True, extra_env=f"XDG_CONFIG_HOME={cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: "agmux ·" in t.text(), 8), t.text())
        return t

    def test_tab_goes_in_and_tab_comes_out(self):
        self.press("j", "j", "p"); self.assertTrue(wait(lambda: self.pv() is not None, 4))
        self.press("Tab"); self.assertTrue(wait(self.focused, 3), self.t.text())
        self.assertIn("tab back to the cards", last_text_row(self.t.snap()))
        self.press("C-]"); self.assertEqual(wait(lambda: self.cat() == "^]", 3), True, f"ctrl+] is forwarded now: {self.cat()!r}")
        self.press("Tab"); self.assertTrue(wait(lambda: not self.focused(), 3), self.t.text())
        self.assertEqual(self.cat(), "^]", "the tab that left was not forwarded")
        self.assertIn("tab focus preview", last_text_row(self.t.snap()))

    def test_default_config_dump_has_no_ctrl_bracket(self):
        r = subprocess.run([BIN, "config"], env={"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(self.env.tmp / "cfg")}, capture_output=True, text=True)
        self.assertIn('leave_preview = ["tab"]', r.stdout); self.assertNotIn("ctrl+]", r.stdout)


class LeaveWithCtrlBracket(LeaveWithTab):
    """leave_preview = ["ctrl+]"] brings the old key back: it leaves, and tab (still focus_preview on the cards) is forwarded inside the pass-through."""
    CONFIG = '[keys]\nleave_preview = ["ctrl+]"]\n'

    def test_tab_goes_in_and_tab_comes_out(self):  # (replaced: this class tests the opposite)
        self.press("j", "j", "p"); self.assertTrue(wait(lambda: self.pv() is not None, 4))
        self.press("Tab"); self.assertTrue(wait(self.focused, 3), "tab still opens the pass-through from the cards")
        self.assertIn("ctrl+] back to the cards", last_text_row(self.t.snap()))
        self.press("Tab"); self.assertEqual(wait(lambda: self.cat() == "^I", 3), True, f"inside, tab goes to the pane: {self.cat()!r}"); self.assertTrue(self.focused())
        self.press("C-]"); self.assertTrue(wait(lambda: not self.focused(), 3), self.t.text())
        self.assertEqual(self.cat(), "^I", "ctrl+] was taken by agmux, not forwarded"); self.assertIsNotNone(self.pv(), "the preview stays open")

    def test_default_config_dump_has_no_ctrl_bracket(self):
        r = subprocess.run([BIN, "config"], env={"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(self.env.tmp / "cfg")}, capture_output=True, text=True)
        self.assertIn('leave_preview = ["ctrl+]"]', r.stdout); self.assertIn('focus_preview = ["tab"]', r.stdout)


if __name__ == "__main__":
    unittest.main()
