"""T46 the pictures written straight to a tmux client's tty are not cut by tmux's own output on that tty: on macOS a pty holds 1024 bytes, a 4 KB APC written in one go was taken in pieces
and a BEL (set-titles) landing between them ended the APC early in kitty, which printed the rest of the base64 as text. Now: sequences < 1024 bytes (base64 <= 768, framing included), each
written when the pty has room for it whole. Spec: specs/m3-config/tasks.md T46 (design.md 「头像用真图」).
Black box: a pty standing in for the terminal whose reader is slow (the queue stays full) while another writer -- tmux -- puts BELs on the same tty."""
import os, re, threading, unittest
from lib import *
from test_t38 import Base
from test_t28 import N_BUILTIN


class Bells:
    """tmux's own output on the client's tty: one BEL every `gap` seconds written to the slave side, concurrently with agmux's upload."""
    def __init__(self, tty, gap=0.002):
        self.tty, self.gap, self.n, self.stop = tty, gap, 0, False
        self.th = threading.Thread(target=self.run, daemon=True); self.th.start()

    def run(self):
        while not self.stop:
            try: os.write(self.tty._slave, b"\x07"); self.n += 1
            except OSError: pass
            time.sleep(self.gap)

    def close(self): self.stop = True; self.th.join(2)


def cut_apcs(data):
    """(APCs seen, APCs that have a BEL or a bare ESC in them, i.e. that kitty would end early): the bytes between ESC _ and ESC \; a BEL outside an APC is tmux's and fine."""
    seen = cut = 0; i = 0
    while True:
        i = data.find(b"\x1b_G", i)
        if i < 0: return seen, cut
        j = data.find(b"\x1b\\", i + 3); k = data.find(b"\x07", i + 3)
        seen += 1
        if k >= 0 and (j < 0 or k < j): cut += 1; i = k + 1
        else: i = j + 2 if j >= 0 else len(data)


class Interleave(Base):
    def upload(self, gap=0.002, delay=0.002):
        tty = self.tty(chunk=1024, delay=delay); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)])
        b = Bells(tty, gap); self.addCleanup(b.close)
        r = self.run_board(wait_for=b" sessions", settle=0.3, timeout=90)
        last = [0, time.time()]
        def quiet():  # the upload is over when no byte but the bells has arrived for 1 s
            n = len(tty.bytes().replace(b"\x07", b""))
            if n != last[0]: last[:] = [n, time.time()]
            return time.time() - last[1] > 1
        self.assertTrue(wait(quiet, 90, 0.2), "the upload finishes")
        b.close(); time.sleep(0.2); return r, tty, tty.bytes(), b.n

    def test_no_apc_is_cut_by_tmuxs_bells_on_the_same_tty(self):
        r, tty, data, bells = self.upload(); seen, cut = cut_apcs(data)
        self.assertGreater(bells, 200, "tmux wrote to the tty all through the upload"); self.assertGreaterEqual(seen, 100, "a real amount of sequences")
        self.assertEqual(cut, 0, f"{cut} of {seen} APCs had a BEL inside ({bells} bells written meanwhile)")
        imgs = kitty_images(kitty_commands(data.replace(b"\x07", b""))); self.assertEqual(len(imgs), N_BUILTIN, "every picture arrived, whole and decodable")
        for i in imgs.values(): self.assertTrue(i["data"].startswith(b"\x89PNG"))
        r.send("q"); self.assertTrue(r.exited(5))

    def test_every_sequence_is_whole_in_one_piece_of_the_stream(self):
        # no BEL (or anything else) between an APC's first and last byte, and the framing + <= 768 base64 fits the 1024 a pty holds
        r, tty, data, bells = self.upload(); cmds = kitty_commands(data)
        self.assertGreater(len(cmds), 100)
        for c in cmds: self.assertLess(len(c["raw"]), 1024, c["raw"][:40]); self.assertLessEqual(len(c["payload"]), 768)
        self.assertEqual(len(kitty_commands(data.replace(b"\x07", b""))), len(cmds), "stripping the bells finds no more sequences: none was split")


class Framing(Base):
    def test_a_ttys_chunks_are_768_with_payloads_in_multiples_of_4_except_the_last_of_a_picture(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); r = self.run_board()
        cmds = kitty_commands(tty.bytes()); self.assertGreater(len(cmds), 100); imgs = kitty_images(cmds); self.assertEqual(len(imgs), N_BUILTIN)
        for im in imgs.values():
            self.assertTrue(all(n == 768 for n in im["chunks"][:-1]), im["chunks"][:5]); self.assertLessEqual(im["chunks"][-1], 768)
            self.assertTrue(im["data"].startswith(b"\x89PNG"))
        for c in cmds:
            self.assertLess(len(c["raw"]), 1024)
            if c["ctl"].get("m") == "1": self.assertEqual(len(c["payload"]) % 4, 0, "a non-final chunk's base64 is a multiple of 4")
        r.send("q"); self.assertTrue(r.exited(5))

    def test_outside_tmux_the_chunks_are_still_4096(self):
        out = PtyRun(self.env, extra={"XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-kitty"}); self.addCleanup(out.close); self.assertTrue(wait(lambda: b" sessions" in bytes(out.data), 10)); time.sleep(0.5)
        imgs = kitty_images(kitty_commands(bytes(out.data))); self.assertEqual(len(imgs), N_BUILTIN)
        for im in imgs.values(): self.assertTrue(all(n == 4096 for n in im["chunks"][:-1]), im["chunks"][:3])

    def test_through_tmux_passthrough_the_chunks_are_still_4096(self):
        self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", "/dev/agmux-no-such-tty")]); r = self.run_board()  # a tty that cannot be opened -> passthrough on stdout
        imgs = kitty_images(kitty_commands(bytes(r.data))); self.assertEqual(len(imgs), N_BUILTIN)
        for im in imgs.values(): self.assertTrue(all(n == 4096 for n in im["chunks"][:-1]), im["chunks"][:3])
        r.send("q"); self.assertTrue(r.exited(5))


if __name__ == "__main__":
    unittest.main()
