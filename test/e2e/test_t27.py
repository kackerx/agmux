"""T27 effort: `agmux statusline` saves the payload, the board reads effort.level and the card's line 3 shows `model·level` in colors.effort (muted dot).
A top-level string `effort` is not the real payload and is not read. Spec: specs/m3-config/tasks.md「第四批」T27, design m1 「布局与卡片」line 3 (shed order: detail, counts, model·effort)."""
import json, subprocess, unittest
from lib import *
from test_t3 import ready, by_name, HudModel

PINK = ("rgb", 1, 2, 3)  # #010203, nothing else on the card uses it


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env, with_hud=False)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True)

    def config(self, text):
        (self.cfg / "agmux" / "config.toml").write_text(text)

    def feed(self, sid, payload):
        p = json.dumps({"session_id": sid, **payload}).encode()
        r = subprocess.run([BIN, "statusline"], input=p, capture_output=True, env={"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state)})
        self.assertEqual(r.returncode, 0, r.stderr)

    def board(self, w=200, h=50):
        t = Term(self.env, w=w, h=h, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs, t.text()); return t, cs

    def b1(self, cs):
        return by_name(cs, "bravo-29")


class Show(Base):
    def test_effort_level_shows_after_the_model_in_the_effort_colour(self):
        self.feed("n-b1", {"model": {"display_name": "Fancy Model 9"}, "effort": {"level": "high"}, "context_window": {"used_percentage": 40}})
        t, cs = self.board(); c = self.b1(cs); row = c.rows[3]
        self.assertRegex(c.line(3), r"Fancy Model 9·high[│┃]$", c.line(3))
        i = row.find("high"); self.assertGreaterEqual(i, 0)
        eff = {row.cells[i + k][1] for k in range(4)}; self.assertEqual(len(eff), 1, "one colour for the whole level")
        mod = {row.cells[row.find("Fancy") + k][1] for k in range(5)}
        self.assertEqual(len(mod), 1)
        self.assertNotEqual(eff, mod, "default effort colour differs from the model colour")
        self.assertNotIn("·high", by_name(cs, "alpha-main").line(3), "only the session that has it")

    def test_colors_effort_changes_the_escape_code(self):
        self.config('[colors]\neffort = "#010203"\n')
        self.feed("n-b1", {"model": {"display_name": "Fancy Model 9"}, "effort": {"level": "xhigh"}})
        t, cs = self.board(); row = self.b1(cs).rows[3]; i = row.find("xhigh")
        self.assertGreaterEqual(i, 0, row.text)
        self.assertEqual({row.cells[i + k][1] for k in range(5)}, {PINK})
        self.assertNotEqual(row.cells[row.find("Fancy")][1], PINK, "the model keeps its own colour")
        self.assertNotEqual(row.cells[i - 1][1], PINK, "the dot is muted, not effort-coloured")

    def test_each_value_is_shown_verbatim(self):
        for lvl in ("low", "medium", "max"):
            with self.subTest(lvl):
                self.setUp(); self.feed("n-b1", {"model": {"display_name": "M"}, "effort": {"level": lvl}})
                t, cs = self.board(); self.assertIn("M·" + lvl, self.b1(cs).line(3)); t.close(); self.env.cleanup()

    def test_live_update_and_removal(self):
        self.feed("n-b1", {"model": {"display_name": "Fancy Model 9"}})
        t, cs = self.board(); self.assertNotIn("9·", self.b1(cs).line(3))
        self.feed("n-b1", {"model": {"display_name": "Fancy Model 9"}, "effort": {"level": "high"}})
        self.assertTrue(wait(lambda: "·high" in by_name(find_cards(t.snap()), "bravo-29").line(3), 8), t.text())
        self.feed("n-b1", {"model": {"display_name": "Fancy Model 9"}})
        self.assertTrue(wait(lambda: "9·" not in by_name(find_cards(t.snap()), "bravo-29").line(3), 8), t.text())


class Absent(Base):
    """No usable effort.level: no `·effort`, and everything else the payload carried (model name, 60% bar) stays."""

    def healthy(self, payload):
        self.feed("n-b1", {"model": {"display_name": "Fancy Model 9"}, "context_window": {"used_percentage": 60}, **payload})
        t, cs = self.board(); c = self.b1(cs); self.assertTrue(t.alive())
        self.assertNotRegex(c.line(3), r"Fancy Model 9·|high|null", c.line(3))
        self.assertIn("Fancy Model 9", c.line(3), "model name lost:\n" + c.text)
        self.assertEqual(len([x for x in c.rows[3].cells if x[0] == "■" and x[1] == C_RED]), 4, "context bar lost (60% = 4 red cells):\n" + c.line(3))

    def test_missing_field(self): self.healthy({})
    def test_level_null(self): self.healthy({"effort": {"level": None}})
    def test_level_missing_in_object(self): self.healthy({"effort": {}})
    def test_effort_null(self): self.healthy({"effort": None})
    def test_empty_level_string(self): self.healthy({"effort": {"level": ""}})
    def test_other_fields_named_effort_are_not_read(self): self.healthy({"reasoning_effort": "xhigh", "thinking": {"effort": "xhigh"}})

    # wrong types must not cost the model or the bar (review of b35eb49: json.Unmarshal type error threw the whole file away)
    def test_level_number(self): self.healthy({"effort": {"level": 3}})
    def test_level_array(self): self.healthy({"effort": {"level": ["high"]}})
    def test_level_object(self): self.healthy({"effort": {"level": {"x": 1}}})
    def test_level_bool(self): self.healthy({"effort": {"level": True}})
    def test_effort_top_level_string_xhigh(self): self.healthy({"effort": "xhigh"})
    def test_effort_top_level_string_high(self): self.healthy({"effort": "high"})
    def test_effort_number(self): self.healthy({"effort": 3})
    def test_effort_array(self): self.healthy({"effort": ["high"]})

    def test_no_model_in_payload_does_not_crash(self):  # effort rides on the model; nothing to attach it to
        self.feed("n-b1", {"effort": {"level": "high"}})
        t, cs = self.board(); self.assertTrue(t.alive())
        self.assertNotRegex(self.b1(cs).line(3), r"·high")


class Narrow(Base):
    """design m1 line 3: when room runs out the detail goes first, then counts, then the model (with its effort) is cut from the right; the bar and status word stay."""

    def l3(self, w):
        t = Term(self.env, w=w, h=50, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs, f"w={w}\n" + t.text()); c = self.b1(cs)
        self.assertEqual(len([x for x in c.rows[3].cells if x[0] == "■"]), 6, f"w={w}: bar intact: {c.line(3)}")
        self.assertIn("working", c.line(3), f"w={w}: status word stays: {c.line(3)}")
        t.close(); return c.line(3)

    def test_model_and_effort_are_cut_together_from_the_right(self):
        self.feed("n-b1", {"model": {"display_name": "Fancy Model 9"}, "effort": {"level": "xhigh"}})
        full = "Fancy Model 9·xhigh"; seen = {}
        for w in range(90, 201, 10): seen[w] = self.l3(w)
        self.assertTrue(any(full in l for l in seen.values()), f"never wide enough to show it whole: {seen}")
        cut = [l for l in seen.values() if full not in l and "Fancy" in l]
        for l in cut:  # whatever is shown is a prefix of the full text (so effort can only vanish after the model is cut at its end)
            m = l.split("  ")[-1].strip("│ ").strip(); frag = m.rstrip("…")
            self.assertTrue(full.startswith(frag) or not frag, f"not a prefix of model·effort: {l!r}")
            self.assertNotIn("·xhigh", l)
        for l in seen.values():
            if "·" in l.split("working")[-1].split("  ")[-1]:  # the dot is only there with some of the effort
                self.assertIn("Fancy Model 9", l)

    def test_effort_never_shows_without_the_model(self):
        self.feed("n-b1", {"model": {"display_name": "Fancy Model 9"}, "effort": {"level": "xhigh"}})
        for w in range(90, 201, 10):
            tail = self.l3(w).split("working")[-1]
            self.assertFalse("xhigh" in tail and "Fancy" not in tail, f"w={w}: {tail!r}")
