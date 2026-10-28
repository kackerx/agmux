"""T82 three thick borders on the recall screen (T83, tester side; written from the spec before the code: specs/m6-recall/design.md §2「边框」, specs/m3-config/tasks.md 第十六批 T82): input box, list box and preview box,
each in its own thick (┏━┓ ┃ ┗━┛) border, colours `[colors] recall_input` / `recall_list` / `recall_preview` (defaults #ff8ebe / #6272a4 / #bd93f9); a title sits in the top border: the list `n/total`, the preview the session's
name, the input none. Narrower than 100 columns only the first two boxes are drawn. The three boxes together never need more rows than the terminal has (the list gets fewer rows). Same on the board's `?`.
Read from the screen: corners, sides and their colours (tmux reads a colour 1 off: a tolerance of 3). Skipped until T79-T82 are in the binary (lib_recall.t83_ready())."""
import pathlib, re, subprocess, tempfile, time, unittest
from lib import wait, plain_config
from lib_recall import *

needs = unittest.skipUnless(t83_ready(), "T79-T82 are not in this binary yet: written from the spec")
PLAIN = dict(TMUX="/tmp/fake-tmux,1,0", TMUX_PANE="%1")
DEFAULT = {"input": (255, 142, 190), "list": (98, 114, 164), "preview": (189, 147, 249)}


def near(fg, rgb, tol=3):
    return fg is not None and fg[0] == "rgb" and all(abs(a - b) <= tol for a, b in zip(fg[1:], rgb))


def boxes(t):
    """every thick box on the screen: dicts with top row r, left column c, right column c2, bottom row r2, the title between the top corners, the colour of the top-left corner and whether the frame is whole (every side
    cell ┃ and every bottom cell ━ in the colour of the corner)"""
    snap = t.snap(); out = []
    for r, row in enumerate(snap):
        for c, cell in enumerate(row.cells):
            if cell[0] != "┏": continue
            c2 = next((i for i in range(c + 1, len(row.cells)) if row.cells[i][0] == "┓"), None)
            if c2 is None: continue
            r2 = next((i for i in range(r + 1, len(snap)) if len(snap[i].cells) > c and snap[i].cells[c][0] == "┗"), None)
            if r2 is None: continue
            fg = cell[1]; title = "".join(x[0] for x in row.cells[c + 1:c2]).replace("━", "").strip()
            whole = all(snap[i].cells[c][0] == "┃" and snap[i].cells[c2][0] == "┃" and snap[i].cells[c][1] == fg and snap[i].cells[c2][1] == fg for i in range(r + 1, r2)) and snap[r2].cells[c2][0] == "┛" and snap[r2].cells[c2][1] == fg
            out.append(dict(r=r, c=c, c2=c2, r2=r2, title=title, fg=fg, whole=whole))
    return sorted(out, key=lambda b: (b["c"] > 0 and b["r"] > 0, b["c"], b["r"]))


class Frames:
    """host: start(w, h, conf=None) -> a terminal on the recall screen with the standard corpus (5 sessions, the newest `cart-fix`) in the list"""

    def three(self, t):
        bs = boxes(t); self.assertEqual(len(bs), 3, f"three boxes:\n{t.text()}"); inp, lst, prv = sorted(bs, key=lambda b: (b["c"], b["r"]))
        return inp, lst, prv

    def test_three_thick_boxes_in_the_default_colours_with_whole_frames(self):
        t = self.start(140, 40); bs = boxes(t); self.assertEqual(len(bs), 3, t.text()); by_col = sorted(bs, key=lambda b: (b["c"], b["r"])); inp, lst, prv = by_col
        self.assertEqual(inp["c"], lst["c"], "the input box and the list box are in one column"); self.assertLess(inp["r"], lst["r"], "the input is above the list"); self.assertGreater(prv["c"], lst["c2"] - 1, "the preview is to the right of the list")
        for name, b in (("input", inp), ("list", lst), ("preview", prv)):
            self.assertTrue(b["whole"], f"{name}: every side is ┃ and the bottom corner ┛ in the corner's colour: {b}"); self.assertTrue(near(b["fg"], DEFAULT[name]), f"{name} colour {b['fg']}")

    def test_titles_the_list_has_n_of_total_the_preview_the_session_name_the_input_none(self):
        t = self.start(140, 40); inp, lst, prv = self.three(t)
        self.assertEqual(inp["title"], "", "the input box has no title"); self.assertEqual(lst["title"], "5/5", "no query: every session is a hit (2376ff9: the title counts hits / sessions that can be searched)"); self.assertIn("cart-fix", prv["title"])
        t.keys("Down"); self.assertTrue(wait(lambda: "Audit log export" in self.three(t)[2]["title"], 4), "the preview follows the selection"); self.assertEqual(self.three(t)[1]["title"], "5/5", "the selection does not change the count"); self.assertTrue(wait(lambda: "Audit log export" in self.three(t)[2]["title"], 4), "the preview follows the selection")
        t.lit("'coupon"); self.assertTrue(wait(lambda: self.three(t)[1]["title"] == "1/5" and "cart-fix" in self.three(t)[2]["title"], 5), t.text())

    def test_the_boxes_are_drawn_even_with_no_match_and_say_zero(self):
        t = self.start(140, 40); t.lit("'zzqqxx-nothing"); self.assertTrue(wait(lambda: len(boxes(t)) >= 2 and sorted(boxes(t), key=lambda b: (b["c"], b["r"]))[1]["title"] == "0/5", 5), "no hit: 0 / the 5 sessions there are (5a586a0)\n" + t.text())

    def test_under_100_columns_only_the_first_two_boxes(self):
        t = self.start(99, 40); bs = boxes(t); self.assertEqual(len(bs), 2, t.text()); inp, lst = sorted(bs, key=lambda b: b["r"]); self.assertTrue(near(inp["fg"], DEFAULT["input"]) and near(lst["fg"], DEFAULT["list"]))
        self.assertTrue(inp["whole"] and lst["whole"]); self.assertEqual((inp["c"], lst["c"]), (0, 0)); self.assertGreaterEqual(lst["c2"], 90, "the list takes the width the preview would have")
        t.close(); t = self.start(100, 40); self.assertEqual(len(boxes(t)), 3, "100 columns: all three")

    def test_nothing_overflows_on_a_24_row_terminal_and_on_a_12_row_one(self):
        for h in (24, 12):
            t = self.start(140, h); bs = boxes(t); self.assertEqual(len(bs), 3, f"h={h}\n{t.text()}"); rows = [r for r in t.text().split("\n")]
            self.assertLessEqual(max(b["r2"] for b in bs), h - 2, f"h={h}: the frames end above the footer row"); self.assertTrue(all(b["whole"] for b in bs), f"h={h}: whole frames\n{t.text()}")
            self.assertIn("enter", t.text().rstrip().split("\n")[-1], f"h={h}: the footer is on the last row"); self.assertEqual(len(rows) - (1 if rows[-1] == "" else 0), h)
            lst = sorted(bs, key=lambda b: (b["c"], b["r"]))[1]; self.assertGreaterEqual(lst["r2"] - lst["r"] - 1, 1, f"h={h}: the list keeps at least a row"); t.close()

    def test_the_colours_come_from_the_config(self):
        self.write('[colors]\nrecall_input = "#112233"\nrecall_list = "#445566"\nrecall_preview = "#778899"\n'); t = self.start(140, 40); inp, lst, prv = self.three(t)
        self.assertTrue(near(inp["fg"], (0x11, 0x22, 0x33)) and near(lst["fg"], (0x44, 0x55, 0x66)) and near(prv["fg"], (0x77, 0x88, 0x99)), [b["fg"] for b in (inp, lst, prv)]); self.assertTrue(all(b["whole"] for b in (inp, lst, prv)))

    def test_an_ansi_index_is_a_colour_too(self):
        self.write('[colors]\nrecall_list = "2"\n'); t = self.start(140, 40); inp, lst, prv = self.three(t); self.assertEqual(lst["fg"], ("idx", 2)); self.assertTrue(near(inp["fg"], DEFAULT["input"]), "the others keep their default")


@needs
class RecallFrames(Frames, unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); self.c = corpus(self.env)

    def write(self, text): self.env.cfg.joinpath("agmux", "config.toml").write_text(text)

    def start(self, w, h):
        t = RTerm(self.env, args="-r", w=w, h=h); self.addCleanup(t.close); self.assertTrue(wait(lambda: t.visible({"a": "cart-fix"}), 10), t.text()); time.sleep(0.5); return t


@needs
class BoardFrames(Frames, unittest.TestCase):
    """the recall screen inside the board: the same frames"""

    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); self.c = corpus(self.env); self.env.reg("live1", name="live-one", nameSource="user", tmux="sess:@1.%51", cwd=str(self.env.home / "work" / "live"))

    def write(self, text): self.env.cfg.joinpath("agmux", "config.toml").write_text(text)

    def start(self, w, h):
        t = RTerm(self.env, args="board", w=w, h=h, tmux=True, **PLAIN); self.addCleanup(t.close); self.assertTrue(wait(lambda: "live-one" in t.text(), 10), t.text()); time.sleep(0.5); t.keys("?")
        self.assertTrue(wait(lambda: t.visible({"a": "cart-fix"}), 10), t.text()); time.sleep(0.5); return t


@needs
class Config(unittest.TestCase):
    def setUp(self):
        self.d = tempfile.TemporaryDirectory(); self.addCleanup(self.d.cleanup); (pathlib.Path(self.d.name) / "agmux").mkdir()

    def run_config(self, text=None, *args):
        if text is not None: (pathlib.Path(self.d.name) / "agmux" / "config.toml").write_text(text)
        return subprocess.run([BIN, "config", *args], capture_output=True, text=True, env={"PATH": "/usr/bin:/bin", "HOME": self.d.name, "XDG_CONFIG_HOME": self.d.name}, timeout=10)

    def test_the_template_lists_the_three_colours_with_their_defaults_in_the_colors_section(self):
        out = plain_config(self.run_config("").stdout); colours = out.split("[colors]")[1].split("[layout]")[0]
        for k, v in (("recall_input", "#ff8ebe"), ("recall_list", "#6272a4"), ("recall_preview", "#bd93f9")): self.assertIn(f'{k} = "{v}"', colours)

    def test_what_is_set_is_read_back_and_prints_again_unchanged(self):
        r = self.run_config('[colors]\nrecall_input = "#112233"\nrecall_preview = "5"\n'); self.assertEqual(r.returncode, 0, r.stderr); p = plain_config(r.stdout); self.assertIn('recall_input = "#112233"', p); self.assertIn('recall_preview = "5"', p)
        self.assertEqual(self.run_config(r.stdout).stdout, r.stdout, "idempotent")

    def test_a_bad_colour_is_one_line_naming_the_key_and_exit_1(self):
        for key in ("recall_input", "recall_list", "recall_preview"):
            r = self.run_config(f'[colors]\n{key} = "nonsense"\n'); self.assertEqual(r.returncode, 1, key); self.assertIn(key, r.stderr); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)


if __name__ == "__main__":
    unittest.main()
