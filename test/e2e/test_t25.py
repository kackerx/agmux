"""T25 `[layout] max_columns`. Spec: specs/m3-config/tasks.md T25. Default 4 (as before); 1..8; the minimum column width of 40 still applies, so a narrow terminal has fewer columns than asked."""
import subprocess, unittest
from lib import *
from test_t3 import ready, last_text_row, NAMES

FIVE = ["alpha-main", "bravo-29", "charlie-30", "delta-60", "echo-long"]  # the first card of each of the five groups


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def config(self, text):
        (self.cfg / "agmux" / "config.toml").write_text(text)

    def board(self, w, h=50):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(ready(t), t.text()); return t

    def cli(self, *args):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def columns(self, t):
        """(number of cards in the first row of cards, their width, group titles in the first title row)"""
        cs = find_cards(t.snap()); top = min(c.r for c in cs)
        first = sorted((c for c in cs if c.r == top), key=lambda c: c.c)
        titles = [r for r in t.snap() if r.text.lstrip().startswith(("▾", "▸"))][0].text.count("▾")
        return len(first), first[0].w, titles


class Columns(Base):
    def test_five_columns_when_the_terminal_is_wide_enough(self):
        self.config("[layout]\nmax_columns = 5\n"); t = self.board(232)
        n, w, titles = self.columns(t); self.assertEqual((n, titles), (5, 5), t.text())
        self.assertEqual(w, (232 - 2 * 4) // 5, "colW = floor((W - 2(n-1)) / n)")
        first = sorted((c for c in find_cards(t.snap()) if c.r == min(x.r for x in find_cards(t.snap()))), key=lambda c: c.c)
        self.assertEqual([c.line(1)[8:-6].strip() for c in first], FIVE, "one group per column, all five on one row")

    def test_four_columns_is_the_default_and_the_explicit_four(self):
        for text in ("", "[layout]\nmax_columns = 4\n"):
            with self.subTest(config=text):
                self.config(text); t = self.board(232); n, w, titles = self.columns(t)
                self.assertEqual((n, titles), (4, 4), t.text()); self.assertEqual(w, (232 - 2 * 3) // 4); t.close()

    def test_the_minimum_column_width_still_limits_a_narrow_terminal(self):
        self.config("[layout]\nmax_columns = 5\n"); t = self.board(180)
        n, w, titles = self.columns(t); self.assertEqual((n, titles), (4, 4), "180 columns cannot hold five columns of at least 40")
        self.assertGreaterEqual(w, 40)

    def test_one_column(self):
        self.config("[layout]\nmax_columns = 1\n"); t = self.board(232, 60)
        n, w, titles = self.columns(t); self.assertEqual((n, titles), (1, 1)); self.assertEqual(w, 232)

    def test_a_resize_applies_the_limit_live(self):
        self.config("[layout]\nmax_columns = 5\n"); t = self.board(232)
        self.assertEqual(self.columns(t)[0], 5)
        t.resize(180, 50); self.assertTrue(wait(lambda: self.columns(t)[0] == 4, 5), t.text())
        t.resize(232, 50); self.assertTrue(wait(lambda: self.columns(t)[0] == 5, 5), t.text())

    def test_navigation_follows_the_five_columns(self):
        self.config("[layout]\nmax_columns = 5\n"); t = self.board(232)
        for _ in range(4):
            t.keys("l"); time.sleep(0.15)
        t.keys("Enter"); self.assertTrue(wait(lambda: self.env.actions(), 3)); self.assertEqual(self.env.actions(), [["switch", "e1"]], "four l's from the first card reach the fifth column")


class Config(Base):
    def test_out_of_range_and_wrong_types_are_one_line_errors(self):
        cases = {"9": ("layout.max_columns", "out of range"), "0": ("layout.max_columns", "out of range"), "-1": ("layout.max_columns", "out of range"),
                 '"x"': ("layout.max_columns", ""), "4.5": ("layout.max_columns", ""), "true": ("layout.max_columns", "")}
        for v, (setting, why) in cases.items():
            with self.subTest(value=v):
                self.config(f"[layout]\nmax_columns = {v}\n")
                for args in (["config"], ["board"]):
                    r = self.cli(*args); self.assertEqual(r.returncode, 1, f"{args}: {r.stdout!r}")
                    lines = r.stderr.rstrip("\n").split("\n"); self.assertEqual(len(lines), 1, r.stderr)
                    self.assertTrue(lines[0].startswith("agmux: " + str(self.cfg / "agmux" / "config.toml")), lines[0]); self.assertIn(setting, lines[0]); self.assertIn(why, lines[0])

    def test_the_bounds_one_and_eight_are_valid(self):
        for v in (1, 8):
            self.config(f"[layout]\nmax_columns = {v}\n"); r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn(f"max_columns = {v}", r.stdout)

    def test_unknown_layout_setting_and_non_table_layout(self):
        self.config("[layout]\nnope = 1\n"); r = self.cli("config"); self.assertEqual(r.returncode, 1); self.assertIn("layout.nope: unknown setting", r.stderr)
        self.config("layout = 3\n"); r = self.cli("config"); self.assertEqual(r.returncode, 1)
        self.assertEqual(len(r.stderr.rstrip("\n").split("\n")), 1); self.assertIn("layout: must be a table", r.stderr, "T25: the same wording as `keys = 3` / `colors = \"dark\"`")

    def test_the_dump_has_a_layout_section_and_round_trips(self):
        out = self.cli("config").stdout; self.assertIn("[layout]\nmax_columns = 4", out)
        self.config("[layout]\nmax_columns = 5\n"); out = self.cli("config").stdout; self.assertIn("max_columns = 5", out)
        self.config(out); r = self.cli("config"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stdout, out)


if __name__ == "__main__":
    unittest.main()
