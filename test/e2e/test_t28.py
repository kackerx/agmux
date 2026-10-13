"""T28 avatars as real images (kitty graphics + Unicode placeholders). Spec: specs/m3-config/design.md「头像用真图」, tasks.md T28.
This file starts with the fixtures' own checks (so a red image test points at agmux, not at the harness); the product tests follow once T28 has a commit."""
import base64, subprocess, unittest
from lib import *
from test_t3 import ready, by_name


class Fixtures(unittest.TestCase):
    def test_png_is_a_png_and_colours_differ(self):
        a, b = png((1, 2, 3)), png((4, 5, 6)); self.assertTrue(a.startswith(b"\x89PNG\r\n\x1a\n")); self.assertNotEqual(a, b)

    def test_write_avatar_set_layout(self):
        env = Env(); self.addCleanup(env.cleanup); d = write_avatar_set(env.tmp / "cfg", "demo", {"idle": 1, "working": 3}, **{"working/notes.txt": "x"})
        self.assertEqual(sorted(p.relative_to(d).as_posix() for p in d.rglob("*") if p.is_file()),
                         ["idle/01.png", "working/01.png", "working/02.png", "working/03.png", "working/notes.txt"])
        self.assertEqual(len({p.read_bytes() for p in (d / "working").glob("*.png")}), 3)

    def test_fake_tmux_answers_termname_and_passthrough(self):
        env = Env(); self.addCleanup(env.cleanup)
        def ask(*a): return subprocess.run([str(env.bin / "tmux"), *a], capture_output=True, text=True).stdout.strip()
        q = ("display-message", "-p", "-c", "c-test", "#{client_termname}")
        self.assertEqual((ask(*q), ask("show", "-gv", "allow-passthrough")), ("xterm-256color", "on"))
        env.set_term("xterm-kitty", "off"); self.assertEqual((ask(*q), ask("show", "-gv", "allow-passthrough")), ("xterm-kitty", "off"))
        env.set_term(); self.assertEqual(ask(*q), "xterm-256color")
        self.assertEqual(ask("display-message", "-p", "#{session_name}"), "fl-own", "other display-message queries are unchanged")

    def test_kitty_parser_unwraps_tmux_passthrough_and_joins_chunks(self):
        data = png((9, 9, 9), 64, noise=True); b64 = base64.b64encode(data); parts = [b64[i:i + 4096] for i in range(0, len(b64), 4096)]
        self.assertGreater(len(parts), 1)
        stream = b"junk"
        for k, p in enumerate(parts):
            ctl = b"a=T,U=1,f=100,q=2,i=12632576,c=6,r=2,m=1" if k == 0 and k < len(parts) - 1 else (b"m=1" if k < len(parts) - 1 else b"m=0")
            apc = b"\x1b_G" + ctl + b";" + p + b"\x1b\\"
            stream += b"\x1bPtmux;" + apc.replace(b"\x1b", b"\x1b\x1b") + b"\x1b\\"
        stream += b"\x1b_Ga=d,d=I,i=12632576,q=2\x1b\\"
        cmds = kitty_commands(stream); self.assertEqual(len(cmds), len(parts) + 1)
        self.assertTrue(all(c["wrapped"] for c in cmds[:-1])); self.assertFalse(cmds[-1]["wrapped"])
        im = kitty_images(cmds); self.assertEqual(list(im), [12632576]); self.assertEqual(im[12632576]["data"], data)
        self.assertEqual(im[12632576]["chunks"][:1], [4096]); self.assertEqual(kitty_deletes(cmds), [12632576])

    def test_placeholder_cells_survive_tmux_capture_and_parse(self):
        import time
        sock = "agmux-ph-" + uuid.uuid4().hex[:6]
        prog = ("import sys,time\nD=[0x305,0x30d,0x30e,0x310,0x312,0x33d]\nw=sys.stdout.write\nw('\\x1b[2J')\n"
                "for r in range(2):\n w('\\x1b[%d;3H\\x1b[38;2;0;192;222m' % (r+2))\n for c in range(6): w(chr(0x10EEEE)+chr(D[r])+chr(D[c]))\n w('\\x1b[0m|')\nsys.stdout.flush(); time.sleep(30)\n")
        self.addCleanup(lambda: subprocess.run(["tmux", "-L", sock, "kill-server"], capture_output=True))
        subprocess.run(["tmux", "-L", sock, "-f", "/dev/null", "new-session", "-d", "-s", "p", "-x", "30", "-y", "6", "python3", "-c", prog], check=True)
        raw = wait(lambda: (lambda r: r if PLACEHOLDER in r else None)(subprocess.run(["tmux", "-L", sock, "capture-pane", "-p", "-e", "-t", "p"], capture_output=True, text=True).stdout), 5)
        self.assertTrue(raw)
        cells = ph_cells(raw); self.assertEqual(len(cells), 12)
        self.assertEqual({c[2] for c in cells}, {0x00C0DE}, "id = 0x00c0de from rgb(0,192,222)")
        self.assertEqual([(c[1], c[3], c[4]) for c in cells if c[0] == 1], [(2 + k, 0, k) for k in range(6)])
        self.assertEqual([(c[1], c[3], c[4]) for c in cells if c[0] == 2], [(2 + k, 1, k) for k in range(6)])
        rows = parse_screen(raw); self.assertEqual(sum(1 for x in rows[1].cells if x[0] == PLACEHOLDER), 6, "parse_screen sees one cell per placeholder")

    def test_ptyrun_records_bytes_and_exit_status(self):  # baseline binary: starts on a bare pty, q quits, no graphics sequence (pre-T28 behaviour; T28 tests replace this expectation)
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        r = PtyRun(env); self.addCleanup(r.close)
        self.assertTrue(wait(lambda: b"alpha-main" in bytes(r.data), 8), bytes(r.data)[-300:])
        r.send("q"); self.assertTrue(r.exited(5)); self.assertEqual(r.status, 0)


if __name__ == "__main__":
    unittest.main()
