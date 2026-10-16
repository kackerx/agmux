"""T61 `[layout] preview_width` (percent of the terminal width, 30-70, default 40): the preview takes max(60, width * pct / 100) columns, the cards get the rest (their column count follows from it as ever); out of range
is `layout.preview_width: 71 is out of range (30 to 70 percent)` and exit 1; `agmux config` prints `preview_width = N  # default: 40 — ...` right after max_columns and what it prints reads back. The preview itself is cut,
not wrapped (test_t56): a pane wider than the box keeps its left side row by row. Spec: specs/m3-config/tasks.md T61. Real tmux panes (private server), the board in a pane of it.

Geometry (read off the screen): the preview region is previewW columns at the right edge; its box starts one column in (91 wide at 232 columns = 92 - 1)."""
import subprocess, unittest
from lib import *
from test_t11 import PreviewBase
from test_t56 import box, sh


def cut(l, n):
    o, w = "", 0
    for ch in l:
        if w + cw(ch) > n: break
        o += ch; w += cw(ch)
    return o


def pw(w, pct): return max(60, w * pct // 100)


class Base(PreviewBase):
    PANES = (("a1", sh("printf abc")), ("a2", sh("printf abc")), ("a3", sh("printf abc")))

    def setUp(self):
        super().setUp(); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def config(self, text): (self.cfg / "agmux" / "config.toml").write_text(text)

    def cli(self, *args):
        return subprocess.run([BIN, *args], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def board_with(self, w, h, text=None):
        if text is not None: self.config(text)
        t = Term(self.env, w=w, h=h, real_tmux=True, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 3, 10), t.text()); time.sleep(0.4); return t

    def open_preview(self, t):
        t.keys("p"); self.assertTrue(wait(lambda: box(t.snap(), t.w) is not None, 5), t.text()); time.sleep(0.9); return box(t.snap(), t.w)

    def columns(self, t):
        return len({c.c for c in find_cards(t.snap()) if c.ok})


class Width(Base):
    def test_default_is_40_percent_as_before(self):
        t = self.board_with(253, 50); x, top, rows = self.open_preview(t); self.assertEqual(253 - (x - 1), 101, "40% of 253 = 101 columns for the preview region")
        self.assertEqual(self.columns(t), layout(253 - 101)[0], "the cards get the rest: 152 columns = 3 of them")

    def test_the_four_values_at_253_columns(self):
        for pct, want, cols in ((40, 101, 3), (55, 139, 2), (30, 75, 4), (70, 177, 1)):
            with self.subTest(pct=pct):
                t = self.board_with(253, 50, f"[layout]\npreview_width = {pct}\n"); x, top, rows = self.open_preview(t); self.assertEqual(253 - (x - 1), want, f"{pct}%: preview columns"); self.assertEqual(pw(253, pct), want)
                self.assertEqual(self.columns(t), min(cols, layout(253 - want)[0]), f"{pct}%: card columns"); self.assertEqual(layout(253 - want)[0], cols); self.assertTrue(t.alive()); t.close()

    def test_a_narrow_terminal_gets_the_60_column_floor(self):
        for pct in (30, 40):
            with self.subTest(pct=pct):
                t = self.board_with(100, 40, f"[layout]\npreview_width = {pct}\n"); x, top, rows = self.open_preview(t); self.assertEqual(100 - (x - 1), 60, "30% and 40% of 100 are under 60: 60 columns"); self.assertEqual(self.columns(t), 1); t.close()

    def test_70_percent_of_a_narrow_terminal_leaves_a_narrow_card_area_and_nothing_breaks(self):
        t = self.board_with(120, 40, "[layout]\npreview_width = 70\n"); x, top, rows = self.open_preview(t); self.assertEqual(120 - (x - 1), 84); self.assertTrue(t.alive()); self.assertGreaterEqual(len([c for c in find_cards(t.snap()) if c.ok]), 1, "cards are still drawn in the 36 columns that are left")
        t.keys("j"); time.sleep(0.4); self.assertTrue(t.alive())

    def test_the_text_cells_follow_the_width(self):
        for pct in (30, 55):
            with self.subTest(pct=pct):
                t = self.board_with(232, 50, f"[layout]\npreview_width = {pct}\n"); x, top, rows = self.open_preview(t); self.assertEqual(len(rows[0].cells) - 2, pw(232, pct) - 1 - 2 - 2, f"{pct}% of 232 = {pw(232, pct)}: the box is 1 less, minus the borders and the pad cells"); t.close()

    def test_below_100_columns_there_is_still_no_preview(self):
        t = self.board_with(99, 40, "[layout]\npreview_width = 30\n"); w0 = {c.w for c in find_cards(t.snap()) if c.ok}; t.keys("p"); time.sleep(0.8); self.assertIn("at least 100 columns", "\n".join(r.text for r in t.snap()[-3:])); self.assertEqual({c.w for c in find_cards(t.snap()) if c.ok}, w0, "the cards keep the whole width"); self.assertTrue(t.alive())

    def test_a_pane_wider_than_the_box_keeps_its_left_side_at_40_and_at_55_percent(self):
        W = 150; lines = ["┌" + "─" * 30 + "┬" + "─" * 60 + "┬" + "─" * 55 + "┐", "│ 名称 (wide)" + " " * 18 + "│ " + "description " * 4 + " " * 12 + "│ " + "x" * 53 + " │", "├" + "─" * 30 + "┼" + "─" * 60 + "┼" + "─" * 55 + "┤",
                                       "│ row two" + " " * 22 + "│ " + "y" * 58 + "│ " + "z" * 53 + " │", "└" + "─" * 30 + "┴" + "─" * 60 + "┴" + "─" * 55 + "┘"]
        lines = [l + " " * (W - sum(cw(ch) for ch in l)) if sum(cw(ch) for ch in l) < W else l for l in lines]
        tm("resize-window", "-t", "pane-a1", "-x", 200); tm("respawn-pane", "-k", "-t", self.panes["a1"], "env LANG=en_US.UTF-8 sh -c 'cat <<EOT\n" + "\n".join(lines) + "\nEOT\nsleep 600'"); time.sleep(0.9)
        for pct in (40, 55):
            with self.subTest(pct=pct):
                t = self.board_with(253, 50, f"[layout]\npreview_width = {pct}\n"); x, top, rows = self.open_preview(t); n = len(rows[0].cells) - 2; out = [r.width_text(1, len(r.cells) - 1).rstrip() for r in rows]; i = next(k for k, c in enumerate(out) if c.startswith("┌"))
                self.assertEqual(out[i:i + 5], [cut(l, n).rstrip() for l in lines], f"{pct}%: {n} text cells; each row the left side of the original"); self.assertEqual(n, pw(253, pct) - 1 - 2 - 2)
                t.close()


class Setting(Base):
    def test_config_prints_the_line_with_the_default_after_max_columns(self):
        out = self.cli("config").stdout; lines = out.splitlines(); j = next(k for k, l in enumerate(lines) if l.startswith("max_columns")); self.assertTrue(lines[j + 1].startswith("preview_width = 40"), lines[j:j + 3])
        self.assertIn("# default: 40 — the preview's share of the terminal width, in percent (30–70; never under 60 columns)", lines[j + 1])

    def test_it_prints_what_is_set_keeps_the_default_in_the_comment_and_reads_back(self):
        self.config("[layout]\npreview_width = 55\n"); r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); line = next(l for l in r.stdout.splitlines() if l.startswith("preview_width")); self.assertTrue(line.startswith("preview_width = 55"), line); self.assertIn("# default: 40 —", line)
        self.config(r.stdout); r2 = self.cli("config"); self.assertEqual(r2.stdout, r.stdout, "printed, read back, printed: the same")

    def test_the_two_ends_are_valid(self):
        for n in (30, 70):
            self.config(f"[layout]\npreview_width = {n}\n"); r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn(f"preview_width = {n}", r.stdout)

    def test_outside_30_to_70_is_one_line_exit_1(self):
        for n in (71, 29, 0, 100, -5):
            with self.subTest(n=n):
                self.config(f"[layout]\npreview_width = {n}\n")
                for cmd in ("config", "board"):
                    r = self.cli(cmd); self.assertEqual(r.returncode, 1, (cmd, r.stdout, r.stderr)); self.assertTrue(r.stderr.strip().endswith(f"layout.preview_width: {n} is out of range (30 to 70 percent)"), r.stderr); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)

    def test_a_value_that_is_not_an_integer_is_an_error_too(self):
        for v in ('"40"', "40.5", "true"):
            with self.subTest(v=v):
                self.config(f"[layout]\npreview_width = {v}\n"); r = self.cli("config"); self.assertEqual(r.returncode, 1, r.stdout); self.assertIn("preview_width", r.stderr); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)

    def test_the_message_names_the_file(self):
        self.config("[layout]\npreview_width = 71\n"); r = self.cli("config"); self.assertIn("config.toml", r.stderr)

    def test_no_layout_section_is_40(self):
        self.config(""); self.assertIn("preview_width = 40", self.cli("config").stdout)


if __name__ == "__main__":
    unittest.main()
