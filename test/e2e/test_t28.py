"""T28 avatars as real images (kitty graphics + Unicode placeholders). Spec: specs/m3-config/design.md「头像用真图」, tasks.md T28.
This file starts with the fixtures' own checks (so a red image test points at agmux, not at the harness); the product tests follow once T28 has a commit."""
import base64, subprocess, unittest
from lib import *
from test_t3 import ready, by_name


def avatars_line(name, counts):
    """the `agmux avatars` line of a set: `id  source  idle 2 · think 2 · ...` for {state: frames}"""
    return f"{name}  builtin  " + " · ".join(f"{s} {counts.get(s, 0)}" for s in AVATAR_STATES)




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

    def test_png_can_be_non_square_and_bad_png_is_not_a_png(self):
        import struct
        w, h = struct.unpack(">II", png((1, 2, 3), 200, height=100)[16:24]); self.assertEqual((w, h), (200, 100)); self.assertTrue(BAD_PNG.startswith(b"\x89PNG"))

    def test_state_set_has_every_pi_emote_state_directory(self):
        env = Env(); self.addCleanup(env.cleanup); d = state_set(env.tmp / "cfg", "demo", {"read": 0, "idle": 2})
        self.assertEqual(sorted(p.name for p in d.iterdir()), sorted(set(AVATAR_STATES) - {"read"})); self.assertEqual(len(list((d / "idle").glob("*.png"))), 2)

    def test_states_fixture_cards_show_the_status_they_stand_for(self):
        env = Env(); self.addCleanup(env.cleanup); exp = build_states_fixture(env); self.assertEqual(len(exp), 17)
        t = Term(env, w=200, h=40); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) == 17, 8), t.text()); cs = find_cards(t.snap())
        word = {"think": "working", "read": "working", "write": "working", "tool": "working", "hi": None, "success": "done", "idle": "idle"}
        for name, state in exp.items():
            c = [x for x in cs if name in x.line(1)]; self.assertEqual(len(c), 1, name); l3 = c[0].line(3)
            if name == "s-waiting": self.assertIn("waiting", l3)
            elif name == "s-attention": self.assertIn("working", l3)
            else: self.assertIn(word[state], l3, f"{name}: {l3}")
        for tool in ("s-read:Read", "s-write:Write", "s-bash:Bash"):
            n, tl = tool.split(":"); self.assertIn(tl, [x for x in cs if n in x.line(1)][0].line(3), "the pending tool is on the card's third line")

    def test_many_fixture_overflows_a_small_screen_and_the_sheet_is_a_sheet(self):
        env = Env(); self.addCleanup(env.cleanup); names = build_many_fixture(env); self.assertEqual(len(names), 22)
        t = Term(env, w=120, h=30); self.addCleanup(t.close); self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 3, 8), t.text())
        shown = find_cards(t.snap()); self.assertLess(len(shown), 22, "22 cards do not fit 120x30: the rest is below the fold")
        data, w, h = sheet_png(); pw, ph, rows, ch = decode_png(data); self.assertEqual((pw, ph), (w, h)); self.assertEqual((w, h), (6 * 180 + 40, 5 * 150 + 40))
        px = lambda x, y: tuple(rows[y][x * ch:x * ch + 3]); self.assertEqual(px(w - 1, 0), SHEET_OUT); self.assertEqual(px(0, h - 1), SHEET_OUT)
        cells = {px(c * 180 + 90, r * 150 + 75) for r in range(5) for c in range(6)}; self.assertEqual(len(cells), 30, "every cell has its own colour"); self.assertTrue(all(sum(c) / 3 <= 200 for c in cells), "all dark by pi's test")
        self.assertEqual(px(5, 5), tuple(v + 1 for v in sheet_colour(0, 0)), "the side strips are one step brighter")
        d19, w19, h19 = sheet_png(5, 4, blank_from=19); _, _, r19, c19 = decode_png(d19); self.assertEqual(tuple(r19[3 * 150 + 75][4 * 180 * c19 + 90 * c19:][:3]), SHEET_OUT, "the 20th cell of a 19-cell sheet is light")

    def test_ptyrun_records_bytes_and_exit_status(self):  # baseline binary: starts on a bare pty, q quits, no graphics sequence (pre-T28 behaviour; T28 tests replace this expectation)
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        r = PtyRun(env); self.addCleanup(r.close)
        self.assertTrue(wait(lambda: b"alpha-main" in bytes(r.data), 8), bytes(r.data)[-300:])
        r.send("q"); self.assertTrue(r.exited(5)); self.assertEqual(r.status, 0)


# ---------------------------------------------------------------------------------------------------------------- product tests (4648d7d)
SETS = SRC / "internal" / "avatar" / "sets" / "aza_choi"  # the built-in set's source frames (pi-emote's own directory layout)
def _group(frames):
    out = {}
    for d, f in frames: out.setdefault(d, []).append(f)
    return out


BUILTIN = _group(scan_set(SETS)) if SETS.exists() else {}  # aza_choi: {state dir: [file names]} (the other built-in sets are in set_list())
N_BUILTIN = sum(len(fr) for _, _, fr in set_list())  # frames of ALL built-in sets (141 at de20b1e): what a board uploads when the user has no sets
BUILTIN_LINE = avatars_line("aza_choi", {k: len(v) for k, v in BUILTIN.items()})  # the first line of `agmux avatars`
IMG_DIMS = lambda data: __import__("struct").unpack(">II", data[16:24])  # (width, height) from a PNG's IHDR
# fixture (test_t3), T30 states: alpha-main working on Bash -> tool, bravo-29 raw idle but a subagent works -> think, a3 / d1 need you -> hi, c2 / e1 done -> success
MOOD = {"alpha-main": "tool", "bravo-29": "think", "charlie-30": "idle", "pj-fallback": "idle", "bravo-bg": "idle", "delta-60": "hi", "告警会话": "hi",
        "charlie-59": "success", "echo-long": "success"}


def builtin_frames(env, cfg):
    """{state: [image id of each frame in file-name order]} of the built-in set, read from what agmux uploads (so it holds whatever the id scheme is)."""
    table, _ = frame_table(env, cfg)
    return {st: [table[("aza_choi", st, f)] for f in files] for st, files in BUILTIN.items()}


def card_avatar(raw, card):
    """The placeholder cells inside the card's avatar box (2 rows x 6 columns right of the border + margin)."""
    return [c for c in ph_cells(raw) if card.r < c[0] <= card.r + 2 and card.c + 1 <= c[1] < card.c + 7]


def avatar_cells(rows, card):
    """Rendered avatar box (rows 1-2, columns c+2..c+7) as tuples, for pixel avatars."""
    return [[(x[0], x[1], x[2], x[3]) for x in rows[card.r + k].cells[card.c + 1:card.c + 7]] for k in (1, 2)]


def cards_of(raw):
    return find_cards(parse_screen(raw))


def named(cards, needle):
    m = [c for c in cards if needle in c.line(1)]
    assert len(m) == 1, f"{needle!r}: {len(m)} cards"
    return m[0]


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True); self.config()

    def config(self, text="", default='default = "aza_choi"'):
        """Unless the text names a default, every card gets the built-in image set (the hash would give a mix of sprites and the set)."""
        if "default" not in text and default:
            text = text.replace("[avatars]\n", "[avatars]\n" + default + "\n", 1) if "[avatars]\n" in text else text + "\n[avatars]\n" + default + "\n"
        (self.cfg / "agmux" / "config.toml").write_text(text)

    def board(self, tmux=True, term="xterm-kitty", passthrough=None, w=200, h=50, extra="", loose=False):
        """tmux=True: agmux believes it runs in tmux (fake tmux answers client_termname=term, allow-passthrough=passthrough or on)."""
        if tmux: self.env.set_term(term, passthrough); self.env.use_pty_tty()
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg} " + (KITTY_ENV if tmux else "") + extra); self.addCleanup(t.close)
        if loose:  # a terminal too small for all nine cards: just wait for the board
            self.assertTrue(wait(lambda: "agmux ·" in t.text(), 8), t.text()); time.sleep(0.5); return t
        cs = ready(t); self.assertTrue(cs, t.text()); return t

    def images(self, t):
        raw = t.raw(); return raw, cards_of(raw)

    def assertAllImages(self, t, msg=""):
        raw, cs = self.images(t); self.assertEqual(len(cs), 9, t.text())
        for c in cs: self.assertEqual(len(card_avatar(raw, c)), 12, f"{msg} card {c.line(1)!r} has no complete 6x2 placeholder avatar:\n{c.text}")

    def assertNoImages(self, t, msg=""):
        raw = t.raw(); self.assertNotIn(PLACEHOLDER, raw, msg + "\n" + t.text())
        cs = cards_of(raw); self.assertEqual(len(cs), 9)
        return cs, parse_screen(raw)


class Kitty(Base):
    """termname xterm-kitty + allow-passthrough on (design「探测」): the avatar is 2 x 6 U+10EEEE cells, foreground = the frame's image id."""

    def test_every_card_shows_a_complete_placeholder_avatar_with_the_right_id(self):
        frames = builtin_frames(self.env, self.cfg); t = self.board(); raw, cs = self.images(t)
        self.assertEqual(len(cs), 9)
        for name, state in MOOD.items():
            ids, _ = frames_for(frames, state); cells = card_avatar(raw, named(cs, name))
            self.assertEqual(len(cells), 12, f"{name}: {cells}")
            self.assertEqual({c[2] for c in cells}, {cells[0][2]}, f"{name}: one id for the whole avatar")
            self.assertIn(cells[0][2], ids, f"{name}: id {cells[0][2]:#x} not in {[hex(i) for i in ids]}")
            self.assertEqual(sorted((c[0] - named(cs, name).r, c[1] - named(cs, name).c, c[3], c[4]) for c in cells),
                             sorted((r, 1 + k, r - 1, k) for r in (1, 2) for k in range(6)), f"{name}: every cell carries its own row and column")

    def test_no_pixel_avatar_left_and_the_text_stays_where_it_was(self):
        t = self.board(); raw, cs = self.images(t)
        t2 = Term(self.env, w=200, h=50, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t2.close)  # same fixture, no tmux, TERM=xterm-256color: pixels
        pix = ready(t2); self.assertTrue(pix)
        for c in cs:
            p = [x for x in pix if x.r == c.r and x.c == c.c]; self.assertEqual(len(p), 1)
            self.assertEqual((c.w, len(c.rows)), (p[0].w, len(p[0].rows)))
            for k in (1, 2, 3): self.assertEqual(c.rows[k].width_text(7, c.w), p[0].rows[k].width_text(7, c.w), f"row {k}: everything right of the avatar is identical in both modes")
        self.assertEqual(raw.count("█"), 0, "no pixel avatar blocks")

    def test_working_frames_cycle_in_order_one_step_per_500ms(self):
        frames = builtin_frames(self.env, self.cfg); t = self.board(); seen = []; t0 = time.time()
        while time.time() - t0 < 4.5:
            raw, cs = self.images(t); seen.append((time.time() - t0, {n: card_avatar(raw, named(cs, n))[0][2] for n in MOOD}))
            time.sleep(0.05)
        for n, state in MOOD.items():
            ids, shown = frames_for(frames, state); seq = [s[n] for _, s in seen]; order = [seq[i + 1] for i in range(len(seq) - 1) if seq[i] != seq[i + 1]]
            if shown == "idle":  # the calm face, plus a blink of one tick every 8 ticks (tests/test_t30 times it)
                self.assertTrue(set(seq) <= set(ids), f"{n}: only idle frames: {set(seq)}"); self.assertEqual(seq[0], ids[0], f"{n}: starts on the calm face"); continue
            self.assertEqual(set(seq), set(ids), f"{n} ({state} -> {shown}): every frame of its state appears")
            for a, b in zip([seq[0]] + order, order): self.assertEqual(b, ids[(ids.index(a) + 1) % len(ids)], f"{n}: frames advance in file-name order and wrap: {order}")
        w = [s[1]["alpha-main"] for s in seen]; ts = [s[0] for s in seen]
        times = [ts[i + 1] for i in range(len(w) - 1) if w[i] != w[i + 1]]
        gaps = [b - a for a, b in zip(times, times[1:])]
        self.assertGreaterEqual(len(times), 6, f"changes seen: {times}")
        self.assertTrue(all(0.35 <= g <= 0.7 for g in gaps), f"frame tick is 500ms, gaps = {gaps}")

    def test_the_xterm_ghostty_name_also_counts(self):
        self.assertAllImages(self.board(term="xterm-ghostty"))

    def test_passthrough_all_counts_as_on(self):
        self.assertAllImages(self.board(passthrough="all"))

    def test_resize_keeps_complete_avatars(self):
        t = self.board(); t.resize(120, 50)
        self.assertTrue(wait(lambda: (lambda cs: len(cs) == 9 and all(c.ok for c in cs))(find_cards(t.snap())), 5), t.text())
        self.assertAllImages(t, "after resize")

    def test_the_pictures_go_to_the_client_enter_would_use(self):  # T38: termname and tty come from list-clients; the chosen client is the most recent outside the own session
        a, b = PtyTty(), PtyTty(); self.addCleanup(a.close); self.addCleanup(b.close)
        self.env.set_term("xterm-kitty", "on"); self.env.set_tmux(clients=[(100, "outer-1", "main", "xterm-kitty", a.path), (300, "outer-2", "work", "xterm-kitty", b.path)])
        t = Term(self.env, w=200, h=50, extra_env=f"XDG_CONFIG_HOME={self.cfg} " + KITTY_ENV + "TMUX_PANE=%9 "); self.addCleanup(t.close); self.assertTrue(ready(t)); time.sleep(0.5)
        self.assertEqual((len(kitty_images(kitty_commands(a.bytes()))), len(kitty_images(kitty_commands(b.bytes())))), (0, N_BUILTIN))
        self.assertEqual([c for c in self.env.tmux_calls() if "#{client_termname}" in c or "allow-passthrough" in c], [], "no display-message -c, no allow-passthrough query")

    def test_AGMUX_CLIENT_names_the_client_that_gets_the_pictures(self):
        a, b = PtyTty(), PtyTty(); self.addCleanup(a.close); self.addCleanup(b.close)
        self.env.set_tmux(clients=[(100, "outer-1", "main", "xterm-kitty", a.path), (300, "outer-2", "work", "xterm-kitty", b.path)])
        t = Term(self.env, w=200, h=50, extra_env=f"XDG_CONFIG_HOME={self.cfg} " + KITTY_ENV + "TMUX_PANE=%9 AGMUX_CLIENT=outer-1 "); self.addCleanup(t.close); self.assertTrue(ready(t)); time.sleep(0.5)
        self.assertEqual((len(kitty_images(kitty_commands(a.bytes()))), len(kitty_images(kitty_commands(b.bytes())))), (N_BUILTIN, 0))

    def test_a_card_bound_to_a_sprite_stays_pixel_in_image_mode(self):
        self.config('[avatars.bind]\n"bravo-29" = "bot-03"\n')
        t = self.board(); raw, cs = self.images(t)
        self.assertEqual(card_avatar(raw, named(cs, "bravo-29")), [])
        self.assertEqual(len(card_avatar(raw, named(cs, "alpha-main"))), 12)
        row = parse_screen(raw)[named(cs, "bravo-29").r + 1]
        self.assertTrue(any(x[0] not in " " for x in row.cells[named(cs, "bravo-29").c + 1:named(cs, "bravo-29").c + 7]), "a pixel avatar is drawn instead")

    def test_default_hash_in_image_mode_picks_image_sets_only(self):  # T36: a session nobody bound never gets a pixel sprite when pictures are drawn
        self.config('[avatars]\ndefault = "hash"\n'); t = self.board(); self.assertAllImages(t)

    def test_default_hash_in_pixel_mode_picks_sprites_only(self):
        self.config('[avatars]\ndefault = "hash"\n'); raw = self.assertNoImages(self.board(term="xterm-256color"))[1]; self.assertTrue(raw)


NAMES_FULL = {"告警会话": "告警会话-超长中文名字-用来测试宽度 1"}


class NotKitty(Base):
    def test_other_terminals_get_pixels(self):
        cs, rows = self.assertNoImages(self.board(term="xterm-256color"))
        self.assertTrue(any(x[0] not in " " for x in rows[cs[0].r + 1].cells[cs[0].c + 1:cs[0].c + 7]))

    def test_passthrough_off_no_longer_matters_the_pictures_go_to_the_tty(self):  # T38
        self.assertAllImages(self.board(passthrough="off"))

    def test_auto_in_a_plain_terminal_in_tmux_says_why(self):
        t = self.board(term="xterm-256color"); self.assertNoImages(t)
        self.assertIn('not kitty or ghostty', "\n".join(r.text for r in t.snap()[-3:]))

    def test_pixel_forces_pixels_and_does_not_even_ask_tmux(self):
        self.config('[avatars]\nrenderer = "pixel"\n'); self.assertNoImages(self.board())
        self.assertEqual([c for c in self.env.tmux_calls() if c[0] == "list-clients"], [], "pixel never asks tmux")

    def test_image_forces_images_without_the_terminal_check(self):
        self.config('[avatars]\nrenderer = "image"\n'); self.assertAllImages(self.board(term="xterm-256color"))


    def test_outside_tmux_TERM_decides(self):
        self.assertNoImages(self.board(tmux=False))  # TERM=xterm-256color
        t = self.board(tmux=False, extra="TERM=xterm-kitty "); self.assertAllImages(t)

    def test_outside_tmux_ghostty_by_TERM_PROGRAM(self):
        self.assertAllImages(self.board(tmux=False, extra="TERM_PROGRAM=ghostty "))

    def test_outside_tmux_renderer_image_forces(self):
        self.config('[avatars]\nrenderer = "image"\n'); self.assertAllImages(self.board(tmux=False))

    def test_TERM_PROGRAM_inside_tmux_is_not_used(self):  # design: the server's TERM_PROGRAM is whatever started it; only client_termname counts
        self.assertNoImages(self.board(term="xterm-256color", extra="TERM_PROGRAM=ghostty TERM=xterm-kitty "))


def upload_ids_for(tc, fixtures):
    """{png: id} of a fresh environment whose config dir was prepared by fixtures(cfg)."""
    env = Env(); tc.addCleanup(env.cleanup); build_fixture(env); cfg = env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True); fixtures(cfg)
    return upload_ids(env, cfg)


class Upload(unittest.TestCase):
    """What agmux writes to stdout before the TUI starts (PtyRun records it)."""

    def run_board(self, extra, config=None, fixtures=None, quit=True, wait_for=b"alpha-main"):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        cfg = env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True)
        if config: (cfg / "agmux" / "config.toml").write_text(config)
        if fixtures: fixtures(cfg)
        env.set_term("xterm-kitty", "on")
        r = PtyRun(env, extra={"XDG_CONFIG_HOME": str(cfg), **extra}); self.addCleanup(r.close)
        self.assertTrue(wait(lambda: wait_for in bytes(r.data), 8), bytes(r.data)[-400:])
        if quit: r.send("q"); self.assertTrue(r.exited(5)); self.assertEqual(r.status, 0)
        return env, r

    def check_builtin(self, cmds):
        """every built-in frame, uploaded once each: a 24-bit non-zero id per frame (all different), the file's centred 3:2 crop (T31) as payload, a=T U=1 f=100 q=2 c=6 r=2, chunks <= 4096."""
        im = kitty_images(cmds); self.assertEqual(len(im), N_BUILTIN, sorted(im))
        self.assertTrue(all(0 < i <= 0xFFFFFF for i in im), "ids fit the placeholder's 24-bit foreground and are never 0")
        keys = [(s, d, f) for s, _, fr in set_list() for d, f in fr]
        for (sid, d, f), (i, v) in zip(keys, im.items()):  # upload order = sets in load order, frames in state / file-name order
            src = (SRC / "internal" / "avatar" / "sets" / sid / d / f).read_bytes(); self.assertEqual(IMG_DIMS(v["data"]), expected_upload_size(*IMG_DIMS(src)), f"{sid}/{d}/{f}: the 3:2 crop")
            if expected_upload_size(*IMG_DIMS(src)) == IMG_DIMS(src): self.assertEqual(v["data"], src, f"{sid}/{d}/{f}: already 3:2 and small: sent as it is")
        for i, d in im.items():
            ctl = d["ctl"]; self.assertEqual({x: ctl.get(x) for x in "a U f q c r".split()}, dict(a="T", U="1", f="100", q="2", c="6", r="2"), ctl)
            self.assertTrue(all(n <= 4096 for n in d["chunks"]), d["chunks"]); self.assertTrue(all(n == 4096 for n in d["chunks"][:-1]), "full chunks until the last")
        return im

    def test_passthrough_fallback_in_tmux_every_frame_once_each_chunk_wrapped_with_doubled_escapes(self):
        env, r = self.run_board({"TMUX": "/fake/tmux,1,0", "TERM": "xterm-256color"})
        data = bytes(r.data); cmds = kitty_commands(data); self.check_builtin(cmds)
        self.assertTrue(all(c["wrapped"] for c in cmds), "inside tmux every APC rides in \\ePtmux;..")
        self.assertEqual(data.count(b"\x1bPtmux;\x1b\x1b_G"), len(cmds)); self.assertNotIn(b"\x1b_G", data.replace(b"\x1b\x1b_G", b""), "no bare (unwrapped) APC")
        self.assertGreater(max(len(c["payload"]) for c in cmds), 4000); self.assertTrue(any(c["ctl"].get("m") == "1" for c in cmds), "large frames are chunked (m=1 .. m=0)")

    def test_outside_tmux_the_alternate_screen_is_entered_exactly_once_before_the_upload(self):  # kitty keeps images per screen: uploaded on the main screen, the board (alt screen) shows none
        env, r = self.run_board({"TERM": "xterm-kitty"}); data = bytes(r.data); first = data.find(b"\x1b_G")
        self.assertGreater(first, 0); self.assertEqual(data[:first].count(b"\x1b[?1049h"), 1, "exactly one \\e[?1049h before the first image sequence")

    def test_passthrough_fallback_in_tmux_adds_no_screen_switch_before_the_upload(self):  # tmux's client is on the alternate screen already
        env, r = self.run_board({"TMUX": "/fake/tmux,1,0"}); data = bytes(r.data); first = data.find(b"\x1bPtmux;")
        self.assertGreaterEqual(first, 0); self.assertNotIn(b"\x1b[?1049h", data[:first])

    def test_outside_tmux_bare_apc(self):
        env, r = self.run_board({"TERM": "xterm-kitty"}); data = bytes(r.data); cmds = kitty_commands(data)
        self.check_builtin(cmds); self.assertFalse(any(c["wrapped"] for c in cmds)); self.assertNotIn(b"Ptmux", data)

    def test_upload_comes_before_the_first_frame_of_the_tui(self):
        env, r = self.run_board({"TERM": "xterm-kitty"}); data = bytes(r.data)
        self.assertLess(data.rfind(b"\x1b_G"), data.find(b"alpha-main"), "bubbletea must not interleave with the upload")

    def test_nothing_is_deleted_on_quit(self):  # final design: a second board's exit must not blank the first's pictures
        for extra in ({"TERM": "xterm-kitty"}, {"TMUX": "/fake/tmux,1,0"}):
            env, r = self.run_board(extra); cmds = kitty_commands(bytes(r.data))
            self.assertEqual(kitty_deletes(cmds), []); self.assertFalse([c for c in cmds if c["ctl"].get("a") == "d"], "no a=d anywhere in the output")

    def test_pixel_terminals_get_no_graphics_bytes(self):
        for extra, cfg in (({"TERM": "xterm-256color"}, None), ({"TERM": "xterm-kitty"}, '[avatars]\nrenderer = "pixel"\n')):
            env, r = self.run_board(extra, config=cfg); self.assertNotIn(b"\x1b_G", bytes(r.data)); self.assertNotIn(b"Ptmux", bytes(r.data))

    def test_ghostty_by_TERM_PROGRAM_uploads(self):
        env, r = self.run_board({"TERM": "xterm-256color", "TERM_PROGRAM": "ghostty"}); self.check_builtin(kitty_commands(bytes(r.data)))

    def test_ctrl_c_also_leaves_cleanly_without_a_delete(self):
        env, r = self.run_board({"TERM": "xterm-kitty"}, quit=False); r.send("\x03"); self.assertTrue(r.exited(5))
        self.assertEqual(kitty_deletes(kitty_commands(bytes(r.data))), [])

    # ---- user sets
    def test_a_user_set_is_uploaded_with_its_own_frames_each_with_its_own_id(self):
        def mk(cfg): write_avatar_set(cfg, "mine", {"idle": 1, "think": 2})
        env, r = self.run_board({"TERM": "xterm-kitty"}, fixtures=mk); im = kitty_images(kitty_commands(bytes(r.data)))
        self.assertEqual(len(im), N_BUILTIN + 3, "the built-in frames + the user's 3")
        d = env.tmp / "cfg" / "agmux" / "avatars" / "mine"
        self.assertEqual(sorted(IMG_DIMS(v["data"]) for v in list(im.values())[-3:]), [expected_upload_size(8, 8)] * 3, "the user's three 8x8 frames, cropped to 3:2")

    def test_ids_are_stable_across_starts_and_follow_the_picture(self):
        a = upload_ids_for(self, lambda cfg: write_avatar_set(cfg, "mine", {"idle": 2}))
        b = upload_ids_for(self, lambda cfg: write_avatar_set(cfg, "mine", {"idle": 2}))
        self.assertEqual(a, b, "the same frame has the same id in every start")
        self.assertEqual(len(set(a.values())), len(a), "different frames, different ids")
        def edited(cfg): d = write_avatar_set(cfg, "mine", {"idle": 2}); (d / "idle" / "01.png").write_bytes(png((1, 2, 3), 8))
        c = upload_ids_for(self, edited)
        old = [i for p, i in a.items() if p not in c]; new = [i for p, i in c.items() if p not in a]
        self.assertEqual((len(old), len(new)), (1, 1)); self.assertNotEqual(old, new, "editing a picture changes its id (design: the hash covers the bytes)")
        self.assertEqual({i for p, i in a.items() if p in c}, {i for p, i in c.items() if p in a}, "the untouched frames keep theirs")

    def test_a_user_set_with_a_builtin_name_replaces_it(self):
        env, r = self.run_board({"TERM": "xterm-kitty"}, fixtures=lambda cfg: write_avatar_set(cfg, "aza_choi", {"idle": 2}))
        im = kitty_images(kitty_commands(bytes(r.data))); aza = sum(len(fr) for s, _, fr in set_list() if s == "aza_choi"); self.assertEqual(len(im), N_BUILTIN - aza + 2, "the other built-in sets and the user's two frames: the built-in aza_choi is gone")
        d = env.tmp / "cfg" / "agmux" / "avatars" / "aza_choi"; self.assertEqual([IMG_DIMS(v["data"]) for v in im.values()][:2], [expected_upload_size(8, 8)] * 2, "the replaced set stays in place (first)")

    def test_only_png_files_count(self):
        def mk(cfg): write_avatar_set(cfg, "mine", {"idle": 1}, **{"idle/notes.txt": "x", "idle/photo.jpg": b"\xff\xd8\xff", "think/readme.md": "y"})
        env, r = self.run_board({"TERM": "xterm-kitty"}, fixtures=mk); im = kitty_images(kitty_commands(bytes(r.data)))
        self.assertEqual(len(im), N_BUILTIN + 1, "one extra frame: mine/idle/01.png")

    def test_a_set_without_idle_is_skipped_and_reported_on_the_footer(self):
        def mk(cfg): write_avatar_set(cfg, "noidle", {"think": 2})
        env, r = self.run_board({"TERM": "xterm-kitty"}, fixtures=mk, quit=False, wait_for=b"no idle")
        self.assertIn(b'avatar set "noidle" has no idle/*.png: skipped', bytes(r.data)); self.assertIsNone(r.status)
        r.send("q"); self.assertTrue(r.exited(5)); self.assertEqual(r.status, 0)
        self.assertEqual(len(kitty_images(kitty_commands(bytes(r.data)))), N_BUILTIN, "nothing of that set was uploaded")


class Sets(Base):
    def test_a_user_set_bound_to_a_card_is_drawn_with_its_own_frames(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 3, "read": 2, "write": 2, "tool": 2, "hi": 2, "success": 2}); ids = upload_ids(self.env, self.cfg)
        mine = {st: [ids[p.read_bytes()] for p in sorted((d / st).glob("*.png"))] for st in AVATAR_STATES}
        self.config('[avatars]\ndefault = "mine"\n'); t = self.board(); raw, cs = self.images(t)
        for n, st in MOOD.items():
            got = {c[2] for c in card_avatar(raw, named(cs, n))}; self.assertTrue(got and got <= set(mine[st]), f"{n} ({st}): {got} vs {mine[st]}")

    def test_a_missing_state_shows_the_idle_frame(self):
        d = write_avatar_set(self.cfg, "mine", {"idle": 1}); idle = upload_ids(self.env, self.cfg)[(d / "idle" / "01.png").read_bytes()]
        self.config('[avatars]\ndefault = "mine"\n'); t = self.board(); raw, cs = self.images(t)
        for n in MOOD: self.assertEqual({c[2] for c in card_avatar(raw, named(cs, n))}, {idle}, f"{n}: every state falls back to idle")

    def test_a_set_without_idle_is_reported_and_the_board_keeps_running(self):
        write_avatar_set(self.cfg, "noidle", {"working": 2}); write_avatar_set(self.cfg, "fine", {"idle": 1}); (self.cfg / "agmux" / "avatars" / "loose.txt").write_text("x")
        t = self.board(); foot = "\n".join(r.text for r in t.snap()[-3:])
        self.assertIn('avatar set "noidle" has no idle/*.png: skipped', foot); self.assertNotIn("fine", foot); self.assertNotIn("loose", foot); self.assertTrue(t.alive())
        self.assertAllImages(t)  # the built-in set is still the default here

    def test_a_bound_set_is_a_stable_sprite_in_pixel_mode(self):
        def h(s):
            x = 2166136261
            for b in s.encode(): x = ((x ^ b) * 16777619) & 0xFFFFFFFF
            return x
        n = h("aza_choi") % 5 + 1  # Sprites[hash % 5]: bot-01..05 in order (pixel mode: the hash pool is the sprites only)
        self.config('[avatars]\nrenderer = "pixel"\n[avatars.bind]\n"bravo-29" = "aza_choi"\n"alpha-main" = "aza_choi"\n')
        t = self.board(); raw = t.raw(); cs = cards_of(raw); rows = parse_screen(raw); first = avatar_cells(rows, named(cs, "bravo-29"))
        self.assertNotIn(PLACEHOLDER, raw); self.assertTrue(any(x[0] != " " for r in first for x in r))
        t.close(); self.config(f'[avatars]\nrenderer = "pixel"\n[avatars.bind]\n"bravo-29" = "bot-0{n}"\n"alpha-main" = "aza_choi"\n')
        t = self.board(); raw = t.raw(); cs = cards_of(raw); rows = parse_screen(raw)
        self.assertEqual(avatar_cells(rows, named(cs, "bravo-29")), first, f"a set id shows the sprite bot-0{n} (fnv32a('aza_choi') % 5) in pixel mode")
        t.close(); self.board()  # and again: stable across runs
        t = self.board(); self.assertEqual(avatar_cells(parse_screen(t.raw()), named(cards_of(t.raw()), "bravo-29")), first)

    def test_bind_and_state_file_accept_set_ids(self):
        self.config('[avatars]\ndefault = "bot-02"\n[avatars.bind]\n"bravo-29" = "aza_choi"\n'); t = self.board(); raw, cs = self.images(t)
        self.assertEqual(len(card_avatar(raw, named(cs, "bravo-29"))), 12); self.assertEqual(card_avatar(raw, named(cs, "alpha-main")), [])
        t.close(); (self.env.state / "agmux").mkdir(exist_ok=True)
        (self.env.state / "agmux" / "avatars.json").write_text(json.dumps({"alpha-main": "aza_choi"})); self.config('[avatars]\ndefault = "bot-02"\n')
        t = self.board(); raw, cs = self.images(t); self.assertEqual(len(card_avatar(raw, named(cs, "alpha-main"))), 12)


class SetDirs(Base):
    """Odd things found in the avatars directory."""

    def cli(self, *a):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *a], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def test_a_symlinked_set_directory_is_recognised(self):
        real = write_avatar_set(self.env.tmp / "elsewhere", "linked", {"idle": 1, "think": 2}); (self.cfg / "agmux" / "avatars").mkdir(parents=True, exist_ok=True)
        os.symlink(real, self.cfg / "agmux" / "avatars" / "linked")
        self.assertIn("linked  user  idle 1 · think 2 · read 0 · write 0 · tool 0 · hi 0 · success 0", self.cli("avatars").stdout)
        ids = upload_ids(self.env, self.cfg); self.assertIn((real / "idle" / "01.png").read_bytes(), ids, "uploaded too")
        self.config('[avatars]\ndefault = "linked"\n'); t = self.board(); raw, cs = self.images(t)
        self.assertEqual({c[2] for c in card_avatar(raw, named(cs, "charlie-30"))}, {ids[(real / "idle" / "01.png").read_bytes()]})

    def test_a_dangling_symlink_and_a_plain_file_are_ignored(self):
        d = self.cfg / "agmux" / "avatars"; d.mkdir(parents=True, exist_ok=True); os.symlink(self.env.tmp / "nowhere", d / "dangling"); (d / "file.png").write_bytes(png())
        r = self.cli("avatars"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout.split("\n\n")[0].split("\n"), set_lines(self.cfg)); self.assertEqual(r.stderr, "")

    def test_a_set_named_hash_is_skipped_and_reported(self):
        write_avatar_set(self.cfg, "hash", {"idle": 1}); r = self.cli("avatars")
        self.assertEqual(r.returncode, 0); self.assertNotIn("hash  user", r.stdout)
        self.assertIn('avatar set "hash" skipped: "hash" is reserved', r.stderr)
        t = self.board(); foot = "\n".join(x.text for x in t.snap()[-3:]); self.assertIn('avatar set "hash" skipped', foot); self.assertTrue(t.alive())
        self.config('[avatars]\ndefault = "hash"\n'); self.assertEqual(self.cli("config").returncode, 0, "default = hash still means hashing")

    def test_dot_files_and_dot_directories_do_not_count(self):  # macOS leaves ._* copies on some volumes
        d = write_avatar_set(self.cfg, "mine", {"idle": 1}, **{"idle/._01.png": b"junk", "idle/.hidden.png": png((9, 9, 9)), "think/._x.png": b"junk"})
        write_avatar_set(self.cfg, ".hidden", {"idle": 1})
        out = self.cli("avatars"); self.assertIn("mine  user  idle 1 · think 0 · read 0 · write 0 · tool 0 · hi 0 · success 0", out.stdout); self.assertNotIn(".hidden  ", out.stdout); self.assertEqual(out.stderr, "")
        self.assertEqual(len(frame_table(self.env, self.cfg)[0]), N_BUILTIN + 1)


class Config(Base):
    def cli(self, *args, config=None):
        if config is not None: self.config(config)
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def test_default_table_has_renderer_auto_before_default(self):
        self.config("", default=None); out = plain_config(self.cli("config").stdout); self.assertIn('[avatars]\nrenderer = "auto"\ndefault = "hash"\n', out)

    def test_renderer_values_round_trip(self):
        for v in ("auto", "image", "pixel"):
            r = self.cli("config", config=f'[avatars]\nrenderer = "{v}"\n'); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn(f'renderer = "{v}"', r.stdout)

    def test_bad_renderer_is_one_line_naming_the_choices(self):
        r = self.cli("config", config='[avatars]\nrenderer = "kitty"\n')
        self.assertNotEqual(r.returncode, 0); self.assertIn('avatars.renderer: "kitty" is not auto, image or pixel', r.stderr); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)

    def test_default_and_bind_accept_set_ids_and_reject_unknown_ones(self):
        write_avatar_set(self.cfg, "mine", {"idle": 1})
        r = self.cli("config", config='[avatars]\ndefault = "mine"\n[avatars.bind]\n"x" = "aza_choi"\n'); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertIn('default = "mine"', r.stdout); self.assertIn('"x" = "aza_choi"', r.stdout)
        r = self.cli("config", config='[avatars]\ndefault = "nosuch"\n')
        self.assertNotEqual(r.returncode, 0); self.assertIn('avatars.default: "nosuch"', r.stderr); self.assertIn("aza_choi", r.stderr); self.assertIn("mine", r.stderr); self.assertIn("bot-01", r.stderr)
        r = self.cli("config", config='[avatars.bind]\n"x" = "nosuch"\n'); self.assertNotEqual(r.returncode, 0); self.assertIn('avatars.bind."x": "nosuch" is not an avatar id', r.stderr)


if __name__ == "__main__":
    unittest.main()
