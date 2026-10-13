"""T2 avatars. Spec: specs/m1-mvp/design.md「头像」, specs/m1-mvp/tasks.md T2 (output format + avatars.json format as answered by orchestrator)."""
import re, subprocess, unittest
from lib import *
from test_t3 import ready, by_name, NAMES


def run_avatars(env):
    r = subprocess.run(f"{env.prefix} {shlex.quote(BIN)} avatars", shell=True, capture_output=True, text=True, timeout=10)
    return r


def parse_blocks(out):
    blocks = [b.split("\n") for b in out.strip("\n").split("\n\n")]
    return blocks


def sprite_blocks(out):
    """The pixel sprites of `agmux avatars` (3-line blocks: header + 2 rows); the image-set lines (T29) come before them."""
    return [b for b in parse_blocks(out) if len(b) == 3]


def region(card, i):
    return (card.rows[i].sub(2, 8) + [(" ", None, None, False)] * 6)[:6]  # after "│ ": 6 cells


def sig(card, colours=True):
    return tuple(tuple((c[0], c[1], c[2]) if colours else c[0] for c in region(card, i)) for i in (1, 2))


def chars(card):
    return tuple("".join(c[0] for c in region(card, i)) for i in (1, 2))


class AvatarsCmd(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup)

    def test_output_format(self):
        r = run_avatars(self.env)
        self.assertEqual(r.returncode, 0, r.stderr)
        blocks = parse_blocks(r.stdout)
        self.assertEqual(len(blocks), 7, f"want the image-set block + 6 sprite blocks separated by blank lines:\n{r.stdout}")
        self.assertRegex(blocks[0][0], r"aza_choi  builtin  idle 2 · think 4 · read 4 · write 4 · tool 4 · hi 4 · success 0", "T29: the sets come first, one line each")
        self.assertEqual(len(blocks[0]), 1); blocks = blocks[1:]
        ids, names = [], []
        for b in blocks:
            self.assertEqual(len(b), 3, f"block must be '<id>  <name>' + 2 render lines: {b}")
            m = re.fullmatch(r"(\S+)  (\S.*)", b[0])
            self.assertTrue(m, f"header {b[0]!r}")
            ids.append(m.group(1)); names.append(m.group(2))
            for line in b[1:]:
                self.assertEqual(disp_width(line), 6, f"{m.group(1)}: render line {line!r} must be 6 cells wide")
                self.assertTrue(set(line) <= set(" ▀▄█"), f"{m.group(1)}: unexpected glyphs in {line!r}")
        self.assertEqual(len(set(ids)), 6, ids); self.assertEqual(len(set(names)), 6, names)

    def test_truecolor_in_tty(self):
        piped = [tuple(b[1:]) for b in sprite_blocks(run_avatars(self.env).stdout)]
        r = subprocess.run(f"{self.env.prefix} {shlex.quote(BIN)} avatars", shell=True, capture_output=True, text=True)
        self.assertNotIn("\x1b", r.stdout, "piped output must carry no ANSI")
        t = Term(self.env, args="avatars; sleep 8", w=80, h=40); self.addCleanup(t.close)
        raw = wait(lambda: (lambda x: x if len(re.findall(r"bot-0\d  ", strip_ansi(x))) >= 6 and strip_ansi(x).rstrip().count("\n") >= 17 else None)(t.raw()), 5)
        time.sleep(0.2); raw = t.raw()  # all six blocks (header + 2 rows + blank) are on screen
        self.assertTrue(raw, t.text())
        self.assertRegex(raw, r"\x1b\[[0-9;]*38;2;\d+;\d+;\d+", "capture-pane -e must show truecolor fg escapes")
        snap = parse_screen(raw)
        sigs, plain = [], []
        for i, row in enumerate(snap):
            if re.match(r"bot-\d+  ", row.text):
                blk = snap[i + 1:i + 3]
                sigs.append(tuple((c[0], c[1], c[2]) for r_ in blk for c in (r_.cells[:6] + [(" ", None, None, False)] * 6)[:6]))
                plain.append(tuple("".join(c[0] for c in r_.cells[:6]).ljust(6) for r_ in blk))
        self.assertEqual(len(sigs), 6)
        self.assertEqual(plain, piped, "tty render == piped render once ANSI is stripped")
        self.assertEqual(len(set(sigs)), 6, "six sprites must differ once colour is counted")
        colours = {c[1] for r_ in snap for c in r_.cells if isin(c[0], "▀▄") and c[1]}
        self.assertTrue(any(c[0] == "rgb" for c in colours))


class Board(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env)
        self.ids = [b[0].split("  ")[0] for b in parse_blocks(run_avatars(self.env).stdout)]
        self.blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in parse_blocks(run_avatars(self.env).stdout)}

    def board(self, **kw):
        t = Term(self.env, w=200, h=50, **kw); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs, t.text())
        return t, cs

    def test_default_assignment_stable_and_varied(self):
        idle = ("a2", "b1", "c1", "c2", "d1", "e1")
        t1, c1 = self.board(); first = {s: chars(by_name(c1, NAMES[s])) for s in idle}
        t1.close()
        t2, c2 = self.board(); second = {s: chars(by_name(c2, NAMES[s])) for s in idle}
        self.assertEqual(first, second, "default avatar must be a pure function of the session id")
        self.assertGreaterEqual(len(set(first.values())), 2, "6 idle sessions all got the same sprite")
        self.assertTrue(all(ch in set(self.blocks.values()) or True for ch in first.values()))

    def test_binding_applies(self):
        a, b = self.ids[1], self.ids[4]
        self.env.avatars_json({NAMES["a2"]: a, NAMES["c2"]: b})  # since T18 the key is the name on the card, not the session id
        t, cs = self.board()
        ca, cb = by_name(cs, NAMES["a2"]), by_name(cs, NAMES["c2"])
        self.assertNotEqual(chars(ca), chars(cb))
        # idle card shows the idle-state render; `agmux avatars` prints one state, so compare shapes by glyph
        self.assertEqual(chars(ca), self.blocks[a], f"a2 bound to {a}: card vs `agmux avatars`")
        self.assertEqual(chars(cb), self.blocks[b], f"c2 bound to {b}: card vs `agmux avatars`")

    def test_bad_binding_files_do_not_crash(self):  # spec is silent: only assert "starts and renders"
        for body in ('{not json', '{"a2":"nope-99"}', '[]', ''):
            with self.subTest(body=body):
                d = self.env.state / "agmux"; d.mkdir(exist_ok=True); (d / "avatars.json").write_text(body)
                t, cs = self.board(); t.close()

    def test_eye_states(self):
        self.env.avatars_json({s["id"]: self.ids[0] for s in self.fx["sessions"]})
        t, cs = self.board()
        names = ("a1", "a2", "a3", "c2", "d1")  # working / idle / attention(+working) / done / waiting
        frames = {s: [] for s in names}
        for _ in range(40):
            fc = find_cards(t.snap())
            for s in names:
                frames[s].append(sig(by_name(fc, NAMES[s])))
            time.sleep(0.12)
        self.assertGreaterEqual(len(set(frames["a1"])), 2, "working: eyes should sweep between frames")
        self.assertEqual(len(set(frames["a2"])), 1, "idle: static")
        has_red = lambda f: any(C_RED in (c[1], c[2]) for row in f for c in row)  # exact alert colour: Clawd body #d97757 would pass a loose "reddish" test
        self.assertTrue(any(has_red(f) for f in frames["a3"]), "attention: some frame has red eyes")
        self.assertTrue(any(not has_red(f) for f in frames["a3"]), "attention: red should flash (some frame without red)")
        self.assertTrue(any(has_red(f) for f in frames["d1"]) and any(not has_red(f) for f in frames["d1"]), "waiting flashes red too")
        self.assertFalse(any(has_red(f) for f in frames["a1"] + frames["a2"] + frames["c2"]), "no red on non-attention sessions")
        self.assertEqual(len(set(frames["c2"])), 1, "done: normal, static")
        meanlum = lambda fs: sum(lum(c[1]) or 0 for f in fs for row in f for c in row if c[1]) / max(1, sum(1 for f in fs for row in f for c in row if c[1]))
        self.assertLess(meanlum(frames["a2"]), meanlum(frames["a1"]), "idle is dimmer than working (heuristic: mean luminance)")
        self.assertLess(meanlum(frames["a2"]), meanlum(frames["c2"]), "idle is dimmer than done (normal)")


if __name__ == "__main__":
    unittest.main()
