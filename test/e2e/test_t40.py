"""T40 [layout] flow = "bands" (default: each n groups are a row as tall as its tallest group) | "stack" (group k goes to column k mod n and every column stacks its groups on its own,
no alignment between columns); group title rows are drawn `▾ ~/path (n) ━━━━` to the column's right edge (T42: dir colour, was muted), folded titles too, and a title that fills the column has no line.
Spec: specs/m3-config/tasks.md「第七批」T40 (+ T39: column gap 1, no padding inside the border)."""
import re, subprocess, unittest
from lib import *
from test_t3 import ready, border_colours, C_FOCUS

MUTED = ("rgb", 98, 114, 164)
GROUP_SIZES = [6, 2, 3, 5]  # p0..p3: unequal on purpose


def titles(snap):
    """[(row, col, text)] of the group title rows: a ▾ / ▸ cell and the rest of its title up to the next column's start"""
    out = []
    for r, row in enumerate(snap):
        for c, cell in enumerate(row.cells):
            if cell[0] in "▾▸": out.append((r, c, row))
    return out


def title_info(snap):
    """{group path tail: dict(row, col, folded, rule_cells, text)}: title text up to the rule, the ━ run (cells) after it"""
    res = {}
    for r, c, row in titles(snap):
        cells = row.cells; j = c
        while j < len(cells) and cells[j][0] != "━": j += 1
        text = "".join(x[0] for x in cells[c:j]).strip(); k = j
        while k < len(cells) and cells[k][0] == "━": k += 1
        m = re.search(r"(p\d|\S+)\s+\(\d+\)", text); key = re.search(r"(p\d+)", text).group(1) if re.search(r"p\d+", text) else text
        res[key] = dict(row=r, col=c, folded=cells[c][0] == "▸", rule=(j, k), text=text, rule_colours={cells[x][1] for x in range(j, k)})
    return res


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def config(self, text): (self.cfg / "agmux" / "config.toml").write_text(text)

    def board(self, w=120, h=60, many=True):
        self.names = build_many_fixture(self.env, sizes=GROUP_SIZES) if many else build_fixture(self.env)
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= (min(len(self.names), 4) if many else 9), 10), t.text()); time.sleep(0.4); return t

    def cli(self, *a, text=None):
        if text is not None: self.config(text)
        return subprocess.run([BIN, *a], env={"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10)

    def name_rows(self, t):
        out = {}
        for i, r in enumerate(t.snap()):
            for m in re.findall(r"(?<![\w-])(m\d\d)(?![\w-])", r.text): out[m] = i
        return out

    def group_of(self, name): return next(k for k, s in enumerate(GROUP_SIZES) if int(name[1:]) < sum(GROUP_SIZES[:k + 1]))


class Config(Base):
    def test_the_default_is_bands_and_config_prints_it_after_max_columns(self):
        out = plain_config(self.cli("config", text="").stdout); self.assertIn('[layout]\nmax_columns = 4\nflow = "bands"\n', out)

    def test_stack_round_trips(self):
        out = self.cli("config", text='[layout]\nflow = "stack"\n'); self.assertEqual(out.returncode, 0, out.stderr); self.assertIn('flow = "stack"', out.stdout)
        self.config(out.stdout); self.assertEqual(self.cli("config").stdout, out.stdout, "idempotent")

    def test_a_bad_value_is_one_line_naming_the_choices(self):
        for v in ('"x"', '""', '"Stack"', "3", "true"):
            with self.subTest(v=v):
                r = self.cli("config", text=f"[layout]\nflow = {v}\n"); self.assertNotEqual(r.returncode, 0); self.assertEqual(len(r.stderr.strip().splitlines()), 1, r.stderr)
                self.assertIn("layout.flow", r.stderr)
        r = self.cli("config", text='[layout]\nflow = "x"\n'); self.assertTrue(r.stderr.strip().endswith('layout.flow: "x" is not a flow (use "bands" or "stack")'), r.stderr)

    def test_max_columns_and_flow_together(self):
        r = self.cli("config", text='[layout]\nmax_columns = 3\nflow = "stack"\n'); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn("max_columns = 3\nflow = \"stack\"", plain_config(r.stdout))

    def test_readme_documents_flow(self):
        readme = (SRC / "README.md").read_text(); self.assertIn("flow", readme); self.assertIn("stack", readme); self.assertIn("bands", readme)


class Titles(Base):
    def test_every_title_has_a_heavy_rule_in_the_dir_colour_to_the_right_edge_of_its_column(self):
        t = self.board(many=False, w=200, h=50); snap = t.snap(); ti = [(r, c, row) for r, c, row in titles(snap)]; self.assertGreaterEqual(len(ti), 4)
        cards = find_cards(snap); colw = cards[0].w; checked = 0
        for r, c, row in ti:
            cells = row.cells; j = c
            while j < len(cells) and cells[j][0] != "━": j += 1
            if j >= len(cells): continue  # the one that fills its column (below)
            k = j
            while k < len(cells) and cells[k][0] == "━": k += 1
            self.assertTrue(all(cells[x][1] == cells[c][1] for x in range(j, k)), "T42: the rule is in the colour of the title mark (dir, not muted)"); self.assertNotEqual(cells[j][1], MUTED); self.assertEqual(k - c, colw, f"title + rule fill the column ({colw}) exactly: {row.text[c:k + 2]!r}")
            self.assertEqual(cells[j - 1][0], " ", "a space between the count and the rule"); checked += 1
        self.assertGreaterEqual(checked, 3)

    def test_a_title_that_fills_the_column_has_no_rule(self):
        t = self.board(many=False, w=200, h=50); long = [row for r, c, row in titles(t.snap()) if "final-dir" in row.text]; self.assertTrue(long, t.text()); self.assertNotIn("━", long[0].text)

    def test_a_folded_title_has_the_rule_too(self):
        t = self.board(many=False, w=200, h=50); t.keys("f"); time.sleep(0.5)
        folded = [row for r, c, row in titles(t.snap()) if row.cells[c][0] == "▸"]; self.assertTrue(folded, t.text()); self.assertIn("━", folded[0].text)

    def test_the_title_text_is_still_findable(self):
        t = self.board(many=False, w=200, h=50); self.assertRegex(t.text(), r"▾ ~/work/alpha \(3\) ━+"); self.assertRegex(t.text(), r"▾ ~/work/bravo \(2\) ━+")

    def test_the_title_in_the_focus_colour_when_folded_and_focused(self):
        t = self.board(many=False, w=200, h=50); t.keys("f"); time.sleep(0.5); focused = [row for r, c, row in titles(t.snap()) if row.cells[c][0] == "▸" and row.cells[c][1] == C_FOCUS]; self.assertTrue(focused)


class Bands(Base):
    def test_default_bands_rows_share_a_top_and_the_next_row_starts_below_the_tallest(self):
        t = self.board(); ti = title_info(t.snap()); p = {k: ti[k] for k in ("p0", "p1", "p2", "p3")}
        self.assertEqual(p["p0"]["row"], p["p1"]["row"], "band 1: p0 and p1 on the same row"); self.assertEqual(p["p2"]["row"], p["p3"]["row"], "band 2: p2 and p3 on the same row")
        self.assertGreater(p["p2"]["row"], p["p0"]["row"] + 1 + 6 * 5, "below the tallest group of band 1 (p0: title + 6 cards of 5 rows)")
        self.assertEqual(p["p2"]["col"], p["p0"]["col"], "p2 under p0's column"); self.assertEqual(p["p3"]["col"], p["p1"]["col"])

    def test_explicit_bands_is_the_same_as_the_default(self):
        a = self.board(); first = [r.text for r in a.snap()]; a.close(); self.config('[layout]\nflow = "bands"\n'); b = self.board()
        self.assertEqual([r.text for r in b.snap()], first, "same screen")


class Stack(Base):
    def setUp(self):
        super().setUp(); self.config('[layout]\nflow = "stack"\n')

    def test_group_k_goes_to_column_k_mod_n_and_each_column_stacks_on_its_own(self):
        t = self.board(); ti = title_info(t.snap()); p = {k: ti[k] for k in ("p0", "p1", "p2", "p3")}
        self.assertEqual(p["p0"]["col"], p["p2"]["col"], "n=2: p2 under p0"); self.assertEqual(p["p1"]["col"], p["p3"]["col"], "p3 under p1")
        self.assertNotEqual(p["p0"]["col"], p["p1"]["col"]); self.assertEqual(p["p0"]["row"], p["p1"]["row"], "both columns start at the top")
        self.assertEqual(p["p2"]["row"], p["p0"]["row"] + 1 + 6 * 5, "p2's title right under p0's last card: no gap, no alignment with the other column")
        self.assertEqual(p["p3"]["row"], p["p1"]["row"] + 1 + 2 * 5, "p3's title right under p1's last card (p1 is short): its own height")
        self.assertLess(p["p3"]["row"], p["p2"]["row"], "the columns are independent")

    def test_three_columns_take_the_groups_round_robin(self):
        sizes = [2, 3, 1, 4, 2]; self.names = build_many_fixture(self.env, sizes=sizes); t = Term(self.env, w=130, h=60, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 12, 10)); time.sleep(0.4); ti = title_info(t.snap()); cols = sorted({v["col"] for v in ti.values()}); self.assertEqual(len(cols), 3, ti)
        col = {k: cols.index(v["col"]) for k, v in ti.items()}; self.assertEqual([col[f"p{k}"] for k in range(5)], [0, 1, 2, 0, 1], "group k in column k mod 3")
        self.assertEqual(ti["p3"]["row"], ti["p0"]["row"] + 1 + 2 * 5); self.assertEqual(ti["p4"]["row"], ti["p1"]["row"] + 1 + 3 * 5)

    def test_one_column_is_just_a_stack_of_all_groups(self):
        t = self.board(w=60); ti = title_info(t.snap()); self.assertEqual(len({v["col"] for v in ti.values()}), 1)
        rows = sorted(v["row"] for v in ti.values()); self.assertGreaterEqual(len(rows), 2); self.assertEqual(rows[1] - rows[0], 1 + 6 * 5, "p1's title right under p0's cards")

    def test_same_cards_in_both_flows_just_placed_differently(self):
        a = self.board(h=80); sa = self.name_rows(a); a.close(); self.config('[layout]\nflow = "bands"\n'); b = self.board(h=80); sb = self.name_rows(b)
        self.assertEqual(sorted(sa), sorted(sb), "all 16 cards in both"); self.assertNotEqual(sa, sb, "but not at the same rows (p3 sits higher in stack)")
        self.assertLess(sa["m11"], sb["m11"], "m11 is the first card of p3: higher in stack than in bands")

    def focused(self, t):
        hits = set()
        for r in t.snap():
            pink = [i for i, c in enumerate(r.cells) if c[0] in "┏┃┓" and c[1] == C_FOCUS]
            if len(pink) >= 2: hits |= set(re.findall(r"(?<![\w-])(m\d\d)(?![\w-])", "".join(c[0] for c in r.cells[pink[0]:pink[-1] + 1])))
        return hits

    def test_j_goes_on_down_the_column_across_groups_and_h_l_switch_columns(self):
        t = self.board(h=80); self.assertEqual(self.focused(t), {"m00"})
        for _ in range(5): t.keys("j"); time.sleep(0.12)
        self.assertEqual(self.focused(t), {"m05"}, "the last card of p0"); t.keys("j"); time.sleep(0.2); self.assertEqual(self.focused(t), {"m08"}, "on down into p2, the next group of the same column (p1 is the other column)")
        t.keys("l"); time.sleep(0.2); t.keys("k"); time.sleep(0.2); self.assertTrue(self.focused(t) & {"m06", "m07", "m11", "m12", "m13", "m14", "m15"}, f"l moved to the neighbouring column: {self.focused(t)}")

    def test_in_bands_j_also_walks_the_column_but_p2_and_p3_are_a_row_apart(self):
        self.config('[layout]\nflow = "bands"\n'); t = self.board(h=80)
        for _ in range(6): t.keys("j"); time.sleep(0.12)
        self.assertEqual(self.focused(t), {"m08"}, "bands: j goes down the same column across the bands too")

    def test_stack_scrolls_and_clicks_like_bands(self):
        t = self.board(h=30); r0 = self.name_rows(t); t.wheel(10, 10, up=False); time.sleep(0.4); t.wheel(10, 10, up=False); time.sleep(0.4); r1 = self.name_rows(t)
        common = set(r0) & set(r1); self.assertTrue(common); self.assertEqual({r0[n] - r1[n] for n in common}, {6}, "two notches = 6 lines whatever the flow is")
        name = sorted(r1)[1]; t.click(10 if int(name[1:]) < 6 else 70, r1[name]); time.sleep(0.4)


if __name__ == "__main__":
    unittest.main()
