"""T3 cards + layout, black-box in tmux -L agmux-test. Spec: specs/design.md「布局与卡片」, specs/tasks.md T3."""
import os, re, unittest
from lib import *

NAMES = {"a1": "alpha-main", "a2": "pj-fallback", "a3": "告警会话", "b1": "bravo-29", "b2": "bravo-bg",
         "c1": "charlie-30", "c2": "charlie-59", "d1": "delta-60", "e1": "echo-long"}


def ready(term, n=9, timeout=8):
    return wait(lambda: (lambda cs: cs if len(cs) == n and all(c.ok for c in cs) else None)(find_cards(term.snap())), timeout)


def by_name(cards, needle):
    m = [c for c in cards if needle in c.line(1)]
    assert len(m) == 1, f"{needle!r}: {len(m)} cards matched"
    return m[0]


def last_text_row(snap):
    for r in reversed(snap):
        if r.text.strip():
            return r.text
    return ""


class Layout(unittest.TestCase):
    """Column count / width algorithm: n=floor((W+2)/58), colW=floor((W-2(n-1))/n), remainder left empty on the right."""

    def test_widths(self):
        for W in (113, 114, 120, 171, 172, 200, 229, 230):
            with self.subTest(W=W):
                env = Env(); self.addCleanup(env.cleanup)
                fx = build_fixture(env)
                t = Term(env, w=W, h=100); self.addCleanup(t.close)
                cards = ready(t)
                if not cards:
                    self.fail(f"W={W}: 9 well-formed cards never appeared\n{t.text()}")
                n, colW = layout(W)
                snap = t.snap()
                self.assertEqual({c.w for c in cards}, {colW}, f"card widths (want colW={colW})")
                self.assertTrue(all(c.c + c.w <= W for c in cards), "card overflows terminal width")
                first_row = min(c.r for c in cards)
                self.assertEqual(len([c for c in cards if c.r == first_row]), n, f"columns in first band (want {n})")
                grp_ids = [["a1", "a2", "a3"], ["b1", "b2"], ["c1", "c2"], ["d1"], ["e1"]]
                for g, ids in enumerate(grp_ids):
                    want_x = (g % n) * (colW + 2)
                    gc = sorted((by_name(cards, NAMES[i]) for i in ids), key=lambda c: c.r)
                    self.assertEqual({c.c for c in gc}, {want_x}, f"group {g} x (want {want_x})")
                    for a, b in zip(gc, gc[1:]):
                        self.assertEqual(b.r - a.r, 5, f"group {g} cards not stacked contiguously")
                    title = snap[gc[0].r - 1].text
                    self.assertIn("▾", title, f"group {g} title row: {title!r}")
                    pos = snap[gc[0].r - 1].find("▾", want_x)
                    self.assertEqual(pos, want_x, f"group {g} title column")
                    if g < 4:
                        self.assertIn(f"{fx['groups'][g]} ({len(ids)})", title)
                    else:
                        seg = snap[gc[0].r - 1].width_text(want_x, want_x + colW)
                        self.assertTrue("…" in seg and "final-dir (1)" in seg and seg.startswith("▾ "), f"middle-truncated title: {seg!r}")
                # band order: row-major in fixture order
                rows_of_group = [by_name(cards, NAMES[ids[0]]).r for ids in grp_ids]
                for g in range(len(grp_ids)):
                    for h in range(g + 1, len(grp_ids)):
                        if g // n == h // n:
                            self.assertEqual(rows_of_group[g], rows_of_group[h], "same band, different top row")
                        else:
                            self.assertLess(rows_of_group[g], rows_of_group[h])
                self.assertIn("agmux · 9 sessions", snap[0].text, "top bar")
                bar = last_text_row(snap)
                for k in ("j/k", "h/l", "enter", "H 交接", "s 发送", "r 重启", "x 杀", "d review", "q 退出"):
                    self.assertIn(k, bar, "bottom bar hints")

    def test_resize(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        t = Term(env, w=200, h=100); self.addCleanup(t.close)
        self.assertTrue(ready(t))
        for W in (114, 113, 200, 57, 230):
            t.resize(W, 100)
            n, colW = layout(W)
            ok = wait(lambda: (lambda cs: cs and len(cs) == 9 and all(c.ok and c.w == colW for c in cs) and
                               len([c for c in cs if c.r == min(x.r for x in cs)]) == n)(find_cards(t.snap())), 6)
            self.assertTrue(ok, f"after resize to {W} (want n={n}, colW={colW}):\n{t.text()}")


class Content(unittest.TestCase):
    """200x50 (the T3 acceptance geometry): card content, colours, focus."""

    @classmethod
    def setUpClass(cls):
        cls.env = Env(); cls.fx = build_fixture(cls.env)
        cls.t = Term(cls.env, w=200, h=50)
        cls.cards = ready(cls.t)
        assert cls.cards, "cards never appeared:\n" + cls.t.text()
        cls.snap = cls.t.snap()

    @classmethod
    def tearDownClass(cls):
        cls.t.close(); cls.env.cleanup()

    def card(self, sid):
        return by_name(self.cards, NAMES[sid])

    def test_fits_50_rows_and_geometry(self):
        self.assertTrue(all(c.ok for c in self.cards), [c.why for c in self.cards if not c.ok])
        self.assertEqual({c.w for c in self.cards}, {layout(200)[1]})  # 3 columns, colW 64
        self.assertEqual(len([c for c in self.cards if c.r == min(x.r for x in self.cards)]), 3)

    def test_names_and_fallback(self):
        scr = self.t.text()
        for sid, n in NAMES.items():
            self.assertIn(n, scr)
        for bad in ("None", "null", "<nil>"):
            self.assertNotIn(bad, scr)
        c = self.card("a3")
        self.assertIn("…", c.line(1), "long CJK name should be truncated with …")

    def test_name_colour_magenta(self):
        c = self.card("a1")
        i = c.rows[1].find("alpha-main")
        self.assertGreaterEqual(i, 0)
        self.assertTrue(is_magenta(c.rows[1].cells[i][1]), c.rows[1].cells[i])

    def test_context_bar(self):
        exp = {"alpha-main": C_YELLOW, "bravo-29": C_GREEN, "charlie-30": C_YELLOW, "charlie-59": C_YELLOW, "delta-60": C_RED}
        for name, col in exp.items():
            with self.subTest(name):
                c = by_name(self.cards, name); p = self.fx["pct"][name]
                row = c.rows[2]
                sq = [x for x in row.cells if x[0] == "■"]
                self.assertEqual(len(sq), 10, row.text)
                filled = [x for x in sq if x[1] != C_DIM]
                self.assertIn(len(filled), {p // 10, round(p / 10)}, f"{p}% filled cells")  # rounding rule unspecified
                self.assertTrue(all(x[1] == col for x in filled), f"{p}% colour {[x[1] for x in filled]} want {col}")
                self.assertTrue(all(x[1] == C_DIM for x in sq[len(filled):]) and all(x[1] == C_DIM for x in sq if x not in filled))
                self.assertIn(f"{p}%", row.text)
        c = self.card("a2")  # no HUD file
        sq = [x for x in c.rows[2].cells if x[0] == "■"]
        self.assertEqual((len(sq), {x[1] for x in sq}), (10, {C_DIM}))
        self.assertIn("--", c.line(2))

    def test_border_colours(self):
        for sid in ("a3", "d1"):  # attentionType set / status waiting
            self.assertTrue(all(is_red(x[1]) for x in self.card(sid).border_cells()), f"{sid}: whole border red")
        for sid in ("a2", "b1", "c1", "c2", "e1"):
            self.assertFalse(any(is_red(x[1]) for x in self.card(sid).border_cells()), f"{sid} border must not be red")

    def test_status_and_segments(self):
        a1, a2 = self.card("a1"), self.card("a2")
        self.assertIn("⏵ working", a1.line(2)); self.assertIn("Bash", a1.line(2))
        self.assertIn("⏸ idle", a2.line(2))  # (a2 is raw idle, no attentionState)
        self.assertIn("2 agents", a1.line(2))
        for sid in NAMES:
            if sid not in ("a1", "b1"):
                self.assertNotIn("agent", self.card(sid).line(2), f"{sid}: 0 subagents -> segment hidden")
            self.assertNotIn("shell", self.card(sid).line(2), "S1 shells fixed 0 -> hidden")
        b1 = self.card("b1").line(2)
        self.assertIn("⏵ working · agents", b1, "raw idle + a working subagent -> `⏵ working · agents` (ccmux getEffectiveStatus)")
        self.assertNotIn("idle", b1); self.assertNotIn("done", b1); self.assertNotIn("Bash", b1)
        self.assertNotIn("agents", self.card("a1").line(2).replace("⚇ 2 agents", ""), "raw working: pendingTool, never `agents`")
        self.assertIn("● done", self.card("e1").line(2), "attentionState 'read' also counts as done")
        d1, c2 = self.card("d1"), self.card("c2")
        self.assertIn("⏳ waiting", d1.line(2))
        i = d1.rows[2].find("waiting"); self.assertTrue(is_red(d1.rows[2].cells[i][1]), "waiting is red")
        self.assertIn("● done", c2.line(2))
        j = c2.rows[2].find("done"); self.assertTrue(is_green(c2.rows[2].cells[j][1]), "done is green")
        i = a1.rows[2].find("working"); self.assertTrue(is_orange(a1.rows[2].cells[i][1]), "working is orange")
        j = a2.rows[2].find("idle"); r = rgb(a2.rows[2].cells[j][1]); self.assertTrue(r and r[2] >= r[0], "idle is blue-grey")

    def test_line1_branch_time(self):
        a1 = self.card("a1")
        self.assertIn("⑂ feature/qa", a1.line(1)); self.assertIn("…", a1.line(1))
        self.assertRegex(a1.line(1), r"\b1m\b")
        self.assertIn("⑂ main", self.card("a2").line(1))

    def test_line3_prompt_model(self):
        a1, b2 = self.card("a1"), self.card("b2")
        self.assertIn("← orch: 先修折行", a1.line(3), "cross-session message -> ← <from-name>: <first body line>")
        for bad in ("cross-session", "第二行不该出现", "<", "from-mode"):
            self.assertNotIn(bad, a1.line(3))
        self.assertIn("构建完成 (exit 0)", self.card("c2").line(3), "task-notification -> its <summary>")
        for bad in ("task-id", "<", "x1"):
            self.assertNotIn(bad, self.card("c2").line(3))
        self.assertIn("修 折行 问题", self.card("d1").line(3), "other -> first non-empty line, paired tags stripped")
        self.assertNotIn("第二行不该出现", self.card("d1").line(3))
        e1 = self.card("e1").line(3)
        for keep in ("<leader>w", "Vec<T>", "a < b"):
            self.assertIn(keep, e1, "only paired lowercase tags are stripped; this stays verbatim")
        a2 = self.card("a2").line(3)
        self.assertIn("← peer:", a2, "empty cross-session body -> `← <from-name>:`"); self.assertNotIn("This came from", a2)
        self.assertIn("opus", a1.line(3).lower(), "model from jsonl fallback (claude-opus-5-5)")
        self.assertIn("后台任务：交接文档已写好", b2.line(3))
        self.assertNotIn("should-not-show", b2.line(3))

    def test_avatar_region_and_band_colour(self):
        for sid in NAMES:
            c = self.card(sid)
            for i in (1, 2, 3):
                chars = c.rows[i].width_text(2, 8)
                self.assertEqual(len(chars), 6, f"{sid} row {i}")
                self.assertTrue(set(chars) <= set(" ▀▄█"), f"{sid} row {i}: {chars!r}")
        def band(sid):  # the bottom half-pixel of the last avatar row: fg of '▄', bg of '▀'
            return [x[1] if x[0] == "▄" else x[2] for x in self.card(sid).rows[3].cells[2:8] if x[0] in ("▄", "▀")]
        self.assertTrue(any(is_orange(x) for x in band("a1")), f"claude band orange: {band('a1')}")
        self.assertTrue(band("b1") and all(is_green(x) for x in band("b1")), f"codex band green: {band('b1')}")
        g = band("c1")  # c1 is idle -> whole head darkened, so only check it is neither orange nor green
        self.assertTrue(g and not any(is_orange(x) or is_green(x) for x in g), f"other agent band grey: {g}")

    def test_initial_focus_highlight(self):
        lums = {sid: lum(self.card(sid).rows[0].cells[0][1]) for sid in NAMES}
        others = [v for sid, v in lums.items() if sid not in ("a1", "a3", "d1")]  # a3/d1: red border (attention/waiting)
        self.assertGreater(lums["a1"], max(others), f"first card is focused and must have the brightest border: {lums}")

    def test_focus_moves_and_brightens(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        t = Term(env, w=200, h=50); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs)
        sig = lambda cards: {n: [lum(x[1]) for x in by_name(cards, n).border_cells()] for n in NAMES.values()}
        before = sig(cs)
        t.keys("j")
        after = wait(lambda: (lambda s: s if s != before else None)(sig(ready(t) or cs)), 3)
        self.assertTrue(after, "border did not change after j")
        changed = [n for n in before if before[n] != after[n]]
        self.assertEqual(sorted(changed), ["alpha-main", "pj-fallback"], "only old and new focus change")
        mean = lambda l: sum(v for v in l if v is not None) / len(l)
        self.assertGreater(mean(after["pj-fallback"]), mean(before["pj-fallback"]), "focused border gets brighter")
        self.assertLess(mean(after["alpha-main"]), mean(before["alpha-main"]), "old focus dims back")


class StderrFooter(unittest.TestCase):
    """Multi-line ccmux stderr must not push the screen: top bar stays, total rows == terminal height, only the first line shows."""

    def test_multiline_stderr(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env); env.flag("fail")
        t = Term(env, w=200, h=50); self.addCleanup(t.close); self.assertTrue(ready(t))
        t.keys("r")
        self.assertTrue(wait(lambda: "boom from fake ccmux" in t.text(), 3), t.text())
        rows = t.text().split("\n")
        while rows and rows[-1] == "" and len(rows) > 50: rows.pop()
        self.assertEqual(len(rows), 50, "pane rows")
        self.assertIn("agmux · 9 sessions", rows[0], "top bar must stay on the first row")
        self.assertIn("q 退出", rows[49], "key hints on the last row")
        self.assertIn("boom from fake ccmux", "\n".join(rows[-3:]))
        self.assertNotIn("second stderr line", t.text()); self.assertNotIn("third stderr line", t.text())

    def test_multiline_poll_error(self):  # the same for a failing `ccmux show --json` poll (shown on the footer while the board keeps its cards)
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        t = Term(env, w=200, h=50); self.addCleanup(t.close); self.assertTrue(ready(t))
        env.flag("showfail")
        self.assertTrue(wait(lambda: "show exploded" in t.text(), 8), t.text())
        rows = t.text().split("\n")
        while rows and rows[-1] == "" and len(rows) > 50: rows.pop()
        self.assertEqual(len(rows), 50, "pane rows")
        self.assertIn("agmux · 9 sessions", rows[0]); self.assertIn("q 退出", rows[49])
        self.assertNotIn("second poll-error line", t.text())


class ModelCache(unittest.TestCase):
    """Model on the card survives the transcript tail losing its last assistant entry (design「C. jsonl 兜底」)."""

    def test_card_keeps_model(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        t = Term(env, w=200, h=50); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs)
        self.assertIn("opus", by_name(cs, "alpha-main").line(3).lower())
        log = env.tmp / "a1.jsonl"
        with open(log, "a") as f:  # a big attachment line: the assistant entry is now outside the 64KB tail
            f.write('{"type":"attachment","content":"' + "A" * (200 << 10) + '"}\n')
        n0 = env.show_calls()
        self.assertTrue(wait(lambda: env.show_calls() >= n0 + 2, 8), "board should have polled again")
        time.sleep(0.6)
        c = by_name(find_cards(t.snap()), "alpha-main")
        self.assertIn("opus", c.line(3).lower(), "model must be kept from the previous poll:\n" + t.text())


class FooterMessages(unittest.TestCase):
    def test_corrupt_avatars_json_shown_in_footer(self):  # design 67b96d5: path + fix hint on the footer, no exit
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        (env.state / "agmux").mkdir(exist_ok=True); (env.state / "agmux" / "avatars.json").write_text("{not json")
        t = Term(env, w=200, h=50); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs); self.assertTrue(t.alive())
        self.assertTrue(wait(lambda: "avatars.json" in "\n".join(r.text for r in t.snap()[-3:]), 3),
                        "footer should name the corrupt file:\n" + t.text())


if __name__ == "__main__":
    unittest.main()
