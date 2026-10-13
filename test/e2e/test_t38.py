"""T38 pictures inside tmux go straight to the tty of the tmux client that sits in the real terminal (bare sequences, nothing wrapped, no allow-passthrough needed), and the
terminal is read from `list-clients -F` (#{client_termname}, #{client_tty}) instead of `display-message -c` (which tmux 3.8 answers for the wrong client). A client whose TERM is
tmux- / screen- is a tmux inside something (a popup's own session): the most recently active client that is not one is used. tty not usable -> passthrough through stdout, one note.
Fixture: the fake tmux's list-clients rows (lib.Env.set_tmux) with a real pty as the client tty (lib.PtyTty). Spec: specs/m3-config/tasks.md T38."""
import re, unittest
from lib import *
import test_t28 as t28
from test_t28 import N_BUILTIN

NOTE_PIXEL = 'avatars: pixel; tmux client {} is "{}", not kitty or ghostty (renderer = "image" forces pictures, "pixel" hides this note)'


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True); self.ttys = []

    def tty(self, **kw):
        t = PtyTty(**kw); self.ttys.append(t); self.addCleanup(t.close); return t

    def config(self, text): (self.cfg / "agmux" / "config.toml").write_text(text)

    def run_board(self, extra=None, wait_for=b" sessions", settle=0.6, quit=True, timeout=10):
        """agmux on a bare pty with TMUX set (inside tmux as far as it knows); its stdout is what tmux would get from the pane."""
        e = {"XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-256color", "TMUX": "/fake/tmux,1,0", "TMUX_PANE": "%9", **(extra or {})}
        r = PtyRun(self.env, extra=e); self.addCleanup(r.close); self.assertTrue(wait(lambda: wait_for in bytes(r.data), timeout), bytes(r.data)[-300:]); time.sleep(settle)
        return r

    def frames(self, data):
        return kitty_images(kitty_commands(data))

    def calls(self, name): return [c for c in self.env.tmux_calls() if c[0] == name]

    def board_term(self, extra=""):
        t = Term(self.env, w=200, h=50, extra_env=f"XDG_CONFIG_HOME={self.cfg} TMUX=/fake/tmux,1,0 TMUX_PANE=%9 " + extra); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 9, 10), t.text()); time.sleep(0.4); return t

    def foot(self, t): return "\n".join(r.text for r in t.snap()[-3:])


class Direct(Base):
    def test_the_pictures_go_to_the_clients_tty_as_bare_sequences_and_not_to_stdout(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)])
        r = self.run_board(); self.assertEqual(len(self.frames(tty.bytes())), N_BUILTIN, "every frame, on the client's tty")
        data = tty.bytes(); self.assertNotIn(b"Ptmux", data, "no passthrough wrapper"); self.assertNotIn(b"\x1b[?1049h", data, "tmux's client is on the alternate screen already")
        for c in kitty_commands(data): self.assertFalse(c["wrapped"]); self.assertLessEqual(len(c["payload"]), 4096)
        out = bytes(r.data); self.assertNotIn(b"\x1b_G", out, "nothing of it on stdout (the pane)"); self.assertNotIn(b"Ptmux", out)
        r.send("q"); self.assertTrue(r.exited(5))

    def test_every_frame_arrives_whole_in_order_and_equals_the_uploads_outside_tmux(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); self.run_board()
        via_tty = [(c["ctl"].get("i"), c["payload"]) for c in kitty_commands(tty.bytes())]
        out = PtyRun(self.env, extra={"XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-kitty"}); self.addCleanup(out.close); self.assertTrue(wait(lambda: b" sessions" in bytes(out.data), 10))
        direct = [(c["ctl"].get("i"), c["payload"]) for c in kitty_commands(bytes(out.data))]
        self.assertEqual(via_tty, direct, "the same sequences, chunk for chunk, whether they go to a tty inside tmux or to the terminal outside it")

    def test_refresh_client_follows_the_upload_for_that_client(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); self.run_board()
        self.assertEqual(self.calls("refresh-client"), [["refresh-client", "-t", "outer"]], "tmux is asked to draw the client again, in case its own drawing was cut into")
        names = [c[0] for c in self.env.tmux_calls()]; self.assertLess(names.index("list-clients"), names.index("refresh-client"))

    def test_a_failing_refresh_client_changes_nothing(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)], fail="refresh-client"); r = self.run_board()
        self.assertEqual(len(self.frames(tty.bytes())), N_BUILTIN); self.assertNotIn(b"cannot write", bytes(r.data), "no note: the pictures are there")

    def test_allow_passthrough_is_not_asked_and_display_message_c_is_not_used(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); self.env.set_term(passthrough="off"); self.run_board()
        self.assertEqual([c for c in self.env.tmux_calls() if "allow-passthrough" in c or "#{client_termname}" in c or c[:2] == ["display-message", "-p"] and "-c" in c], [], "the termname comes from list-clients")
        self.assertEqual(len(self.frames(tty.bytes())), N_BUILTIN, "passthrough off does not matter any more")

    def test_the_format_asks_for_termname_and_tty(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); self.run_board()
        self.assertEqual(self.calls("list-clients")[0], ["list-clients", "-F", "#{client_activity}\t#{client_name}\t#{client_session}\t#{client_termname}\t#{client_tty}"])

    def test_the_board_shows_pictures_on_screen(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); (self.cfg / "agmux" / "config.toml").write_text('[avatars]\ndefault = "aza_choi"\n')
        t = self.board_term(); raw = t.raw(); cs = find_cards(parse_screen(raw)); self.assertTrue(all(len(t28.card_avatar(raw, c)) == 12 for c in cs)); self.assertEqual(len(self.frames(tty.bytes())), N_BUILTIN)


class Choosing(Base):
    def test_the_most_recent_client_outside_the_own_session_is_the_one(self):
        a, b = self.tty(), self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "older", "main", "xterm-kitty", a.path), (300, "newer", "work", "xterm-kitty", b.path), (500, "float", "fl-own", "xterm-kitty", self.tty().path)])
        self.run_board(); self.assertEqual((len(self.frames(a.bytes())), len(self.frames(b.bytes()))), (0, N_BUILTIN), "the newer one; the float's own client is never it"); self.assertEqual(self.calls("refresh-client"), [["refresh-client", "-t", "newer"]])

    def test_a_nested_tmux_client_is_skipped_for_the_most_recent_real_terminal(self):
        real, nested, own = self.tty(), self.tty(), self.tty()
        self.env.set_tmux(session="fl-own", clients=[(100, "outer", "agmux", "xterm-kitty", real.path), (300, "popup", "other", "tmux-256color", nested.path), (500, "float", "fl-own", "tmux-256color", own.path)])
        r = self.run_board(); self.assertEqual((len(self.frames(real.bytes())), len(self.frames(nested.bytes())), len(self.frames(own.bytes()))), (N_BUILTIN, 0, 0), "the kitty client got them; the tmux-256color ones nothing")
        self.assertEqual(self.calls("refresh-client"), [["refresh-client", "-t", "outer"]])

    def test_screen_256color_counts_as_nested_too(self):
        real, nested = self.tty(), self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-ghostty", real.path), (300, "inner", "other", "screen-256color", nested.path)])
        self.run_board(); self.assertEqual((len(self.frames(real.bytes())), len(self.frames(nested.bytes()))), (N_BUILTIN, 0))

    def test_the_most_recent_among_several_real_terminals_when_the_chosen_one_is_nested(self):
        a, b, n = self.tty(), self.tty(), self.tty()
        self.env.set_tmux(session="fl-own", clients=[(100, "kitty-old", "s1", "xterm-kitty", a.path), (200, "kitty-new", "s2", "xterm-kitty", b.path), (400, "popup", "s3", "tmux-256color", n.path)])
        self.run_board(); self.assertEqual((len(self.frames(a.bytes())), len(self.frames(b.bytes()))), (0, N_BUILTIN))

    def test_agmux_client_in_the_process_environment_names_the_client(self):
        a, b = self.tty(), self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "mine", "main", "xterm-kitty", a.path), (300, "newer", "work", "xterm-kitty", b.path)])
        self.run_board(extra={"AGMUX_CLIENT": "mine"}); self.assertEqual((len(self.frames(a.bytes())), len(self.frames(b.bytes()))), (N_BUILTIN, 0)); self.assertEqual(self.calls("refresh-client"), [["refresh-client", "-t", "mine"]])
        self.assertEqual(self.calls("show-environment"), [], "the process variable is final")

    def test_agmux_client_in_the_hidden_session_variable_then_the_plain_one(self):
        a, b, c = self.tty(), self.tty(), self.tty()
        self.env.set_tmux(session="fl-own", session_env="AGMUX_CLIENT=hid", session_env_plain="AGMUX_CLIENT=pl", clients=[(100, "hid", "s1", "xterm-kitty", a.path), (200, "pl", "s2", "xterm-kitty", b.path), (300, "x", "s3", "xterm-kitty", c.path)])
        self.run_board(); self.assertEqual([len(self.frames(t.bytes())) for t in (a, b, c)], [N_BUILTIN, 0, 0], "-h wins")
        self.env.set_tmux(session="fl-own", session_env=None, session_env_plain="AGMUX_CLIENT=pl", clients=[(100, "hid", "s1", "xterm-kitty", self.tty().path), (200, "pl", "s2", "xterm-kitty", b.path), (300, "x", "s3", "xterm-kitty", c.path)])
        (self.env.fake / "tmux.env").unlink(missing_ok=True); self.run_board(); self.assertGreater(len(self.frames(b.bytes())), 0, "no hidden one: the plain one names the client")

    def test_a_hidden_unset_does_not_fall_through_to_the_plain_variable(self):
        a, b = self.tty(), self.tty(); self.env.set_tmux(session="fl-own", session_env="-AGMUX_CLIENT", session_env_plain="AGMUX_CLIENT=old", clients=[(100, "old", "s1", "xterm-kitty", a.path), (300, "recent", "s2", "xterm-kitty", b.path)])
        self.run_board(); self.assertEqual((len(self.frames(a.bytes())), len(self.frames(b.bytes()))), (0, N_BUILTIN), "the heuristic: the most recent")


class Detection(Base):
    def test_auto_in_a_plain_terminal_says_why_and_draws_pixels(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-256color", tty.path)]); t = self.board_term()
        self.assertEqual(len(self.frames(tty.bytes())), 0); self.assertNotIn(PLACEHOLDER, t.raw()); self.assertIn(NOTE_PIXEL.format("outer", "xterm-256color"), "".join(r.text for r in t.snap()[-3:]).replace("\n", ""))

    def test_renderer_pixel_hides_the_note_and_asks_no_one(self):
        self.config('[avatars]\nrenderer = "pixel"\n'); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-256color", self.tty().path)]); t = self.board_term()
        self.assertNotIn("not kitty", self.foot(t)); self.assertEqual(self.calls("list-clients"), [], "pixel never asks tmux")

    def test_only_nested_clients_and_auto_says_which(self):
        a = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "popup", "s1", "tmux-256color", a.path)]); t = self.board_term()
        self.assertNotIn(PLACEHOLDER, t.raw()); self.assertIn('tmux client popup is "tmux-256color", not kitty or ghostty', self.foot(t).replace("\n", "")); self.assertEqual(len(self.frames(a.bytes())), 0)

    def test_renderer_image_forces_even_for_a_plain_terminal_and_writes_to_its_tty(self):
        self.config('[avatars]\nrenderer = "image"\n'); tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-256color", tty.path)]); self.run_board()
        self.assertEqual(len(self.frames(tty.bytes())), N_BUILTIN)

    def test_image_with_only_nested_clients_falls_back_to_passthrough_with_a_note(self):
        self.config('[avatars]\nrenderer = "image"\n'); a = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "popup", "s1", "tmux-256color", a.path)]); r = self.run_board()
        out = bytes(r.data); self.assertIn(b"every tmux client is itself a tmux", out); self.assertTrue(any(c["wrapped"] for c in kitty_commands(out)), "through stdout, wrapped for tmux's passthrough"); self.assertEqual(len(self.frames(a.bytes())), 0)

    def test_no_client_at_all_in_auto_is_pixels_with_a_note(self):
        self.env.set_tmux(session="fl-own", clients=[]); t = self.board_term(); self.assertNotIn(PLACEHOLDER, t.raw()); self.assertIn("no tmux client found to draw pictures through", self.foot(t))


class Fallback(Base):
    def stdout_wrapped(self, r):
        cmds = kitty_commands(bytes(r.data)); return len(cmds) > 0 and all(c["wrapped"] for c in cmds), len(kitty_images(cmds))

    def test_a_tty_that_cannot_be_opened_falls_back_to_one_layer_of_passthrough_and_says_why(self):
        self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", "/dev/agmux-no-such-tty")]); r = self.run_board()
        ok, n = self.stdout_wrapped(r); self.assertTrue(ok); self.assertEqual(n, N_BUILTIN); self.assertIn(b"cannot write to /dev/agmux-no-such-tty", bytes(r.data)); self.assertIn(b"allow-passthrough", bytes(r.data))

    def test_a_tty_that_is_not_a_device_is_refused_not_created(self):
        for tty in (str(self.env.tmp / "plain-file"), "relative-name", "client-123"):
            with self.subTest(tty=tty):
                self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty)]); r = self.run_board(); ok, n = self.stdout_wrapped(r)
                self.assertTrue(ok and n == N_BUILTIN, "passthrough instead"); self.assertIn(b"cannot write to", bytes(r.data)); self.assertFalse((self.env.tmp / "plain-file").exists()); self.assertFalse((pathlib.Path.cwd() / "client-123").exists(), "no file named after the client appeared")

    def test_an_empty_tty_field_falls_back_too(self):
        self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", "")]); (self.env.fake / "tmux.tty").unlink(missing_ok=True); r = self.run_board()
        # (an empty field is filled by the fake with a file path; a device-less value must end in passthrough)
        ok, n = self.stdout_wrapped(r); self.assertTrue(ok and n == N_BUILTIN)

    def test_a_terminal_that_stops_reading_falls_back_after_about_two_seconds(self):
        stuck = self.tty(read=False); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", stuck.path)]); t0 = time.time()
        r = self.run_board(settle=0.3); took = time.time() - t0; ok, n = self.stdout_wrapped(r)
        self.assertTrue(ok and n == N_BUILTIN, "passthrough took over"); self.assertIn(b"stopped reading", bytes(r.data)); self.assertGreater(took, 1.5, "it waited for the tty first"); self.assertLess(took, 8)

    def test_a_slow_reader_that_keeps_making_progress_is_waited_for(self):
        slow = self.tty(chunk=8192, delay=0.05); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", slow.path)]); r = self.run_board(settle=0.3, wait_for=b" sessions", timeout=60)
        wait(lambda: len(kitty_images(kitty_commands(slow.bytes()))) == N_BUILTIN, 20)
        self.assertEqual(len(kitty_images(kitty_commands(slow.bytes()))), N_BUILTIN, "everything arrived on the tty"); self.assertNotIn(b"stopped reading", bytes(r.data)); self.assertNotIn(b"Ptmux", bytes(r.data))


if __name__ == "__main__":
    unittest.main()
