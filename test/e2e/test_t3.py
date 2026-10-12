"""T3 cards + layout (S1.1 / T5 card redesign), black-box in tmux -L agmux-test. Spec: specs/m1-mvp/design.md「布局与卡片」, specs/m1-mvp/tasks.md T3."""
import json, os, re, subprocess, unittest
from lib import *

NAMES = {"a1": "alpha-main", "a2": "pj-fallback", "a3": "告警会话", "b1": "bravo-29", "b2": "bravo-bg",
         "c1": "charlie-30", "c2": "charlie-59", "d1": "delta-60", "e1": "echo-long"}


def ready(term, n=9, timeout=8):
    return wait(lambda: (lambda cs: cs if len(cs) == n and all(c.ok for c in cs) else None)(find_cards(term.snap())), timeout)


def by_name(cards, needle):
    m = [c for c in cards if needle in c.line(1)]
    assert len(m) == 1, f"{needle!r}: {len(m)} cards matched"
    return m[0]


def group_cards(snap, cards):
    """Group the cards by their column title (`▾ ...`): [(title_row, x, [cards top->bottom])] in reading order. Works when names are truncated."""
    titles = sorted((r, c) for r, row in enumerate(snap) for c, cell in enumerate(row.cells) if isin(cell[0], "▾▸"))
    out = []
    for tr, tx in titles:
        mine = [c for c in cards if c.c == tx and c.r > tr and not any(r2 > tr and r2 < c.r and x2 == tx for r2, x2 in titles)]
        out.append((tr, tx, sorted(mine, key=lambda c: c.r)))
    return out


BAR_KEYS = ("j/k", "h/l", "enter", "H 交接", "s 回复", "r 重启", "x 杀", "d review", "q 退出")


def last_text_row(snap):
    for r in reversed(snap):
        if r.text.strip():
            return r.text
    return ""


class Layout(unittest.TestCase):
    """Column count / width algorithm: n=min(4, floor((W+2)/42)), colW=floor((W-2(n-1))/n), remainder left empty on the right."""

    def test_widths(self):
        for W in (40, 81, 82, 123, 124, 165, 166, 200, 207, 208, 232, 249, 250, 300):
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
                self.assertEqual(len([c for c in cards if c.r == first_row]), min(n, 5), f"columns in first band (want {min(n, 5)}; fixture has 5 groups)")
                gs = group_cards(snap, cards)
                self.assertEqual([len(g[2]) for g in gs], fx["sizes"], "cards per group, in group order")
                for g, (tr, tx, gc) in enumerate(gs):
                    want_x = (g % n) * (colW + 2)
                    self.assertEqual(tx, want_x, f"group {g} title column")
                    self.assertEqual({c.c for c in gc}, {want_x}, f"group {g} cards x")
                    for a, b in zip(gc, gc[1:]):
                        self.assertEqual(b.r - a.r, 5, f"group {g} cards not stacked contiguously")
                    self.assertEqual(gc[0].r, tr + 1, "cards start right under the title")
                    seg = snap[tr].width_text(want_x, want_x + colW)
                    if g < 4:
                        self.assertIn(f"{fx['groups'][g]} ({len(gc)})", seg)
                    else:
                        self.assertTrue("…" in seg and "final-dir (1)" in seg and seg.startswith("▾ "), f"middle-truncated title: {seg!r}")
                    if g // n:  # a later band starts below the tallest column of the previous one
                        self.assertGreater(tr, max(c.r for h in range(g // n * n - n, g // n * n) for c in gs[h][2]) + 4)
                self.assertIn("agmux · 9 sessions", snap[0].text, "top bar")
                bar = last_text_row(snap)
                for k in (BAR_KEYS if W >= 100 else BAR_KEYS[:1]):
                    self.assertIn(k, bar, "bottom bar hints")

    def test_resize(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        t = Term(env, w=200, h=100); self.addCleanup(t.close)
        self.assertTrue(ready(t))
        for W in (82, 81, 200, 232, 41, 250):
            t.resize(W, 100)
            n, colW = layout(W)
            ok = wait(lambda: (lambda cs: cs and len(cs) == 9 and all(c.ok and c.w == colW for c in cs) and
                               len([c for c in cs if c.r == min(x.r for x in cs)]) == min(n, 5))(find_cards(t.snap())), 6)
            self.assertTrue(ok, f"after resize to {W} (want n={n}, colW={colW}):\n{t.text()}")


BAND = {"claude": (255, 184, 108), "codex": (80, 250, 123), "other": (98, 114, 164)}  # #ffb86c #50fa7b #6272a4
STATUS_COLOUR = {"working": ("rgb", 255, 184, 108), "done": ("rgb", 80, 250, 123), "idle": C_DIM, "alert": C_RED}  # border = status colour
C_FOCUS = ("rgb", 255, 142, 190)  # #ff8ebe: the focused card's thick border, overriding the status colour
PROMPT_TEXT = ("先修折行", "构建完成", "修 折行", "<leader>", "后台任务", "← orch", "← peer", "cross-session", "task-notification")


def border_colours(card):
    return {x[1] for x in card.border_cells()}


class Content(unittest.TestCase):
    """200x50: 4 columns of 48. Card = 5 rows: border / avatar+icon+name·status·time / avatar+branch·model / bar·agents·shell / border."""

    W = 200

    @classmethod
    def setUpClass(cls):
        cls.env = Env(); cls.fx = build_fixture(cls.env)
        cls.t = Term(cls.env, w=cls.W, h=50)
        cls.cards = ready(cls.t)
        assert cls.cards, "cards never appeared:\n" + cls.t.text()
        cls.snap = cls.t.snap()

    @classmethod
    def tearDownClass(cls):
        cls.t.close(); cls.env.cleanup()

    def card(self, sid):
        return by_name(self.cards, NAMES[sid])

    def test_geometry(self):
        n, colW = layout(self.W)
        self.assertTrue(all(c.ok for c in self.cards), [c.why for c in self.cards if not c.ok])
        self.assertEqual({c.w for c in self.cards}, {colW})
        self.assertEqual(len([c for c in self.cards if c.r == min(x.r for x in self.cards)]), min(n, 5))
        self.assertTrue(all(len(c.rows) == 5 for c in self.cards), "5 rows: border + 3 body + border (no prompt row)")

    def test_names_and_fallback(self):
        scr = self.t.text()
        for sid, n in NAMES.items():
            self.assertIn(n, scr)
        for bad in ("None", "null", "<nil>"):
            self.assertNotIn(bad, scr)
        self.assertIn("…", self.card("a3").line(1), "long CJK name truncated with …")
        self.assertTrue(self.card("a3").ok, "CJK truncation must keep the border aligned")

    def test_no_prompt_row(self):
        scr = self.t.text()
        for gone in PROMPT_TEXT + ("\U000f0b7b",):
            self.assertNotIn(gone, scr, "S1.1 dropped the lastPrompt / backgroundDetail row")

    def test_line1_name_and_time_only(self):
        scr = self.t.text()
        for icon in ("\uec82", "\uec81", "\uec20"):
            self.assertNotIn(icon, scr, "T13: the agent icon in front of the name is gone (the avatar's band colour says the agent type)")
        for sid, name in NAMES.items():
            c = self.card(sid); row = c.rows[1]; l1 = c.line(1)
            j = row.find(name[:4]); self.assertGreaterEqual(j, 0, l1)
            self.assertTrue(is_magenta(row.cells[j][1]), f"{sid}: name magenta")
            self.assertRegex(l1, r"\b1m [│┃]$", f"{sid}: the relative time is the last thing before the border: {l1!r}")
            for word in ("working", "idle", "done", "waiting", "⚇", "⑂"):
                self.assertNotIn(word, l1, f"{sid}: line 1 is name + time only")
        self.assertEqual(self.card("a1").rows[1].width_text(8, 10), "  ")

    def test_line2_is_the_branch_alone(self):
        a1, a2 = self.card("a1"), self.card("a2")
        self.assertIn("\ue725 feature/qa", a1.line(2)); self.assertIn("…", a1.line(2), "a long branch is cut with …")
        self.assertIn("\ue725 main", a2.line(2))
        self.assertGreaterEqual(a2.rows[2].find("\ue725"), 10, "the branch icon U+E725 sits right after the avatar columns")
        self.assertNotIn("⑂", self.t.text(), "the old branch glyph is gone")
        for card in self.cards:
            self.assertNotRegex(card.line(2), r"Opus|Sonnet|Fable|⚇|⏵|⏸|●|⏳", "line 2 holds the branch only")

    def test_line3_bar_status_counts_model(self):
        a1, b1, c2, d1, a2 = self.card("a1"), self.card("b1"), self.card("c2"), self.card("d1"), self.card("a2")
        self.assertEqual(a1.rows[3].find("■"), 2, "the bar starts under the avatar, at the left edge of the body")
        self.assertEqual(a1.rows[3].width_text(2, 8), "■" * 6); self.assertEqual(a1.rows[3].width_text(8, 10), "  ")
        self.assertRegex(a1.line(3), r"■{6}  ⏵ working · Bash  ⚇ 2 +Opus 5\.5 [│┃]$", "bar · status · detail · count · model flush right")
        self.assertIn("⏵ working · agents  ⚇ 1", b1.line(3)); self.assertNotIn("Opus", b1.line(3))
        self.assertRegex(c2.line(3), r"■{6}  ● done +[│┃]$"); self.assertRegex(d1.line(3), r"■{6}  ⏳ waiting +[│┃]$")
        self.assertRegex(a2.line(3), r"■{6}  ⏸ idle +[│┃]$", "idle: no detail, no counts, no model")
        for sid in NAMES:
            if sid not in ("a1", "b1"):
                self.assertNotIn("⚇", self.card(sid).line(3), f"{sid}: 0 subagents -> segment hidden")
            self.assertNotIn("⚙", self.card(sid).line(3), "M1 shells fixed 0 -> hidden")
        i = a1.rows[3].find("working"); self.assertTrue(is_orange(a1.rows[3].cells[i][1]), "working is orange")
        i = d1.rows[3].find("waiting"); self.assertTrue(is_red(d1.rows[3].cells[i][1]), "waiting is red")
        i = c2.rows[3].find("done"); self.assertTrue(is_green(c2.rows[3].cells[i][1]), "done is green")
        i = a2.rows[3].find("idle"); r = rgb(a2.rows[3].cells[i][1]); self.assertTrue(r and r[2] >= r[0], "idle is blue-grey")

    def test_context_bar(self):
        exp = {"alpha-main": C_YELLOW, "bravo-29": C_GREEN, "charlie-30": C_YELLOW, "charlie-59": C_YELLOW, "delta-60": C_RED}
        for name, col in exp.items():
            with self.subTest(name):
                c = by_name(self.cards, name); p = self.fx["pct"][name]; row = c.rows[3]
                sq = [x for x in row.cells if x[0] == "■"]
                self.assertEqual(len(sq), 6, row.text)
                filled = [x for x in sq if x[1] != C_DIM]
                self.assertEqual(len(filled), (p * 6 + 50) // 100, f"{p}%: filled cells of 6 (rounded)")
                self.assertTrue(all(x[1] == col for x in filled), f"{p}% colour {[x[1] for x in filled]} want {col}")
                self.assertTrue(all(x[1] == C_DIM for x in sq[len(filled):]))
                self.assertNotRegex(row.text, r"\d\s*%", "T8: no percentage number; the colour is the hint")
        c = self.card("a2")
        sq = [x for x in c.rows[3].cells if x[0] == "■"]
        self.assertEqual((len(sq), {x[1] for x in sq}), (6, {C_DIM})); self.assertNotIn("--", c.line(3), "unknown: six dark cells, no `--`")
        for card in self.cards:
            self.assertNotIn("%", card.line(3)); self.assertNotIn("--", card.line(3))

    def test_border_colour_is_status_colour(self):
        self.assertEqual(border_colours(self.card("a1")), {C_FOCUS}, "a1 is focused: #ff8ebe overrides its (working) status colour")
        want = {"a2": "idle", "a3": "alert", "b1": "working", "b2": "idle", "c1": "idle", "c2": "done", "d1": "alert", "e1": "done"}
        for sid, st in want.items():
            self.assertEqual(border_colours(self.card(sid)), {STATUS_COLOUR[st]}, f"{sid} ({st}): every border cell has the status colour")

    def test_focus_is_a_thick_pink_border(self):
        a1, a2 = self.card("a1"), self.card("a2")  # a1 is focused at start
        self.assertEqual((a1.rows[0].cells[0][0], a1.rows[1].cells[0][0], a1.rows[4].cells[0][0]), ("┏", "┃", "┗"), "focused: thick line")
        self.assertEqual(border_colours(a1), {C_FOCUS}, "focus: #ff8ebe, not the status colour")
        self.assertNotIn(C_FOCUS, set().union(*(border_colours(c) for c in self.cards if c is not a1)), "only the focused card is pink")
        for c in self.cards:
            if c is not a1:
                self.assertEqual((c.rows[0].cells[0][0], c.rows[1].cells[0][0]), ("╭", "│"), "unfocused: rounded thin line")

    def test_avatar_region_and_band_colour(self):
        for sid in NAMES:
            c = self.card(sid)
            for i in (1, 2):
                chars = c.rows[i].width_text(2, 8)
                self.assertEqual(len(chars), 6, f"{sid} row {i}")
                self.assertTrue(set(chars) <= set(" ▀▄█"), f"{sid} row {i}: {chars!r}")
                self.assertEqual(c.rows[i].width_text(8, 10), "  ", "two blank cells between avatar and text")
            self.assertEqual(self.card(sid).rows[3].cells[2][0], "■", "row 3 is not part of the avatar")

        def band(sid):  # bottom half-pixel of the last avatar row: fg of '▄', bg of '▀'
            return [x[1] if x[0] == "▄" else x[2] for x in self.card(sid).rows[2].cells[2:8] if x[0] in ("▄", "▀")]
        self.assertTrue(any(is_orange(x) for x in band("a1")), f"claude band orange: {band('a1')}")
        self.assertTrue(band("b1") and all(is_green(x) for x in band("b1")), f"codex band green: {band('b1')}")
        g = band("c1")
        self.assertTrue(g and not any(is_orange(x) or is_green(x) for x in g), f"other agent band grey: {g}")


class FocusMove(unittest.TestCase):
    def test_pink_thick_border_follows_focus(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        t = Term(env, w=200, h=50); self.addCleanup(t.close); self.assertTrue(ready(t))
        before = {n: border_colours(by_name(find_cards(t.snap()), n)) for n in NAMES.values()}
        t.keys("j")
        self.assertTrue(wait(lambda: by_name(find_cards(t.snap()), NAMES["a2"]).rows[0].cells[0][0] == "┏", 3), "focus moved to a2 (thick)")
        cs = find_cards(t.snap())
        self.assertEqual(by_name(cs, NAMES["a1"]).rows[0].cells[0][0], "╭", "a1 back to rounded")
        after = {n: border_colours(by_name(cs, n)) for n in NAMES.values()}
        changed = sorted(n for n in NAMES.values() if after[n] != before[n])
        self.assertEqual(changed, sorted([NAMES["a1"], NAMES["a2"]]), "only the old and the new focus change colour")
        self.assertEqual(after[NAMES["a2"]], {C_FOCUS}); self.assertEqual(after[NAMES["a1"]], {STATUS_COLOUR["working"]}, "old focus gets its status colour back")


class Wide232(unittest.TestCase):
    """The user's popup: 232 columns -> capped at 4 columns of 56 (5 columns left no room for branch names); the 5th group wraps to a second band."""

    def test_four_columns_max(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        for W, colW in ((232, 56), (300, 73), (250, 61)):
            with self.subTest(W=W):
                t = Term(env, w=W, h=50); self.addCleanup(t.close)
                cs = ready(t); self.assertTrue(cs, t.text())
                self.assertEqual(layout(W), (4, colW))
                self.assertEqual({c.w for c in cs}, {colW})
                top = min(x.r for x in cs)
                self.assertEqual(sorted(c.c for c in cs if c.r == top), [i * (colW + 2) for i in range(4)], "4 columns")
                snap = t.snap(); self.assertEqual(snap[top - 1].text.count("▾"), 4, snap[top - 1].text)
                echo = [c for c in cs if "echo-long" in c.line(1)]
                self.assertEqual(len(echo), 1); self.assertGreater(echo[0].r, top + 15, "the fifth group is on a second band")
                self.assertEqual(echo[0].c, 0)
                t.close()


class Narrow(unittest.TestCase):
    """40-wide columns (82 cols): line 1 keeps the time and only cuts a name that does not fit; line 3 keeps the status word and gives way detail -> counts -> model."""

    def test_yielding_order(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        t = Term(env, w=82, h=100); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs, t.text())
        self.assertEqual({c.w for c in cs}, {40})
        for c in cs:
            self.assertRegex(c.line(1), r"\b\d+[smhd] [│┃]$", f"time is always the right end of line 1: {c.line(1)!r}")
            self.assertRegex(c.line(3), r"(⏵ working|⏸ idle|● done|⏳ waiting)", f"the status word is always on line 3: {c.line(3)!r}")
        a1 = sorted(cs, key=lambda c: (c.r, c.c))[0]  # alpha-main
        self.assertIn("alpha-main", a1.line(1), "the name fits: whole")
        self.assertIn("…", a1.line(2), "the branch is cut")
        self.assertIn("Opus 5.5", a1.line(3), "model stays at 40 columns"); self.assertIn("⚇ 2", a1.line(3))
        cjk = [c for c in cs if "告警" in c.line(1)][0]
        self.assertIn("…", cjk.line(1), "a name that does not fit is cut"); self.assertTrue(cjk.ok)


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


class LongPendingTool(unittest.TestCase):
    """design T13: line 3 keeps the status word always; what gives way first is the `· pendingTool` detail, then the ⚇ / ⚙ counts, then the model.
    Line 1 keeps the time and cuts the name only when it does not fit; line 2 cuts the branch."""

    TOOL = "mcp__claude_ai_Claude_Docs__batch"
    NAME = "agmux-orchestrator-extra24"  # 26 characters
    BRANCH = "feature/qa-acceptance-pap-be-dev"

    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        S = json.loads((self.env.fake / "show.json").read_text())
        for s in S:
            if s["id"] == "a1":
                s.update(summary=self.NAME, gitBranch=self.BRANCH, pendingTool=self.TOOL)
        self.env.write_show(S)

    def test_names_and_branches_whole_on_48_columns(self):
        t = Term(self.env, w=200, h=50); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs, t.text())
        c = [x for x in cs if "agmux-orc" in x.line(1)][0]
        self.assertEqual(c.w, 48)
        self.assertIn(self.NAME, c.line(1), "a 26-character name fits a 48-column card"); self.assertRegex(c.line(1), r"\b1m [│┃]$")
        self.assertIn(self.BRANCH, c.line(2), "and so does `feature/qa-acceptance-pap-be-dev`")

    def test_line3_gives_way_detail_then_counts_then_model(self):
        for W in (82, 124, 166, 200, 232, 300, 400):
            with self.subTest(W=W):
                t = Term(self.env, w=W, h=100); self.addCleanup(t.close)
                cs = ready(t); self.assertTrue(cs, t.text())
                c = [x for x in cs if "agmux-orc" in x.line(1)][0]; l3 = c.line(3)
                self.assertIn("⏵ working", l3, f"W={W}: the status word never gives way: {l3!r}")
                detail, counts, model = "·" in l3, "⚇" in l3, "Opus" in l3
                self.assertTrue(not detail or counts, f"W={W}: the detail is cut before the counts: {l3!r}")
                self.assertTrue(not counts or model, f"W={W}: the counts are cut before the model: {l3!r}")
                if detail:
                    d = l3.split("· ", 1)[1].split("  ")[0].strip()
                    self.assertRegex(d, r"^\w", f"W={W}: a shown detail has real text, not a bare `…`: {l3!r}")
                if W >= 232:
                    self.assertTrue(model and counts, f"W={W}: wide enough for counts and model: {l3!r}")
                if W >= 400:
                    self.assertIn(self.TOOL, l3); self.assertIn("Opus 5.5", l3)
                self.assertRegex(c.line(1), r"\b\d+[smhd] [│┃]$", f"W={W}: time at the right edge of line 1")
                t.close()


class BarEdges(unittest.TestCase):
    """0% and unknown are both six dark cells; any non-zero percentage fills at least one cell (design: 非零至少 1 格)."""

    def test_zero_unknown_small_and_full(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env, with_hud=False)
        for sid, p in (("n-a1", 0), ("n-b1", 1), ("n-c1", 4), ("n-c2", 100)):
            env.hud(sid, json.dumps({"context_window": {"used_percentage": p}}))
        t = Term(env, w=200, h=50); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs)

        def fills(name):
            sq = [x for x in by_name(cs, name).rows[3].cells if x[0] == "■"]
            return len(sq), [x[1] for x in sq if x[1] != C_DIM]
        self.assertEqual(fills("alpha-main"), (6, []), "0%: six dark cells")
        self.assertEqual(fills("pj-fallback"), (6, []), "unknown: six dark cells")
        n, f = fills("bravo-29"); self.assertEqual((n, f), (6, [C_GREEN]), "1% -> one green cell")
        n, f = fills("charlie-30"); self.assertEqual((n, f), (6, [C_GREEN]), "4% -> still one cell")
        n, f = fills("charlie-59"); self.assertEqual((n, f), (6, [C_RED] * 6), "100% -> six red cells")
        for c in cs:
            self.assertNotRegex(c.rows[3].width_text(2, 14), r"%|--|\d", "nothing but the bar on the left of line 3")


class HudModel(unittest.TestCase):
    """T6 acceptance: the model.display_name that `agmux statusline` captured shows on the card (full chain: payload -> file -> store -> card)."""

    def feed(self, env, sid, display_name, pct=33, **extra):
        p = json.dumps({"session_id": sid, "model": {"display_name": display_name}, "context_window": {"used_percentage": pct, "remaining_percentage": 100 - pct}, **extra}).encode()
        r = subprocess.run([BIN, "statusline"], input=p, capture_output=True, env={"PATH": "/usr/bin:/bin", "HOME": str(env.home), "XDG_STATE_HOME": str(env.state)})
        assert r.returncode == 0, r.stderr

    def test_display_name_and_percentage_from_statusline(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env, with_hud=False)
        self.feed(env, "n-b1", "Fancy Model 9", pct=77)  # b1 has no jsonl: the HUD name is the only model source
        t = Term(env, w=200, h=50); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs)
        b1 = by_name(cs, "bravo-29")
        self.assertRegex(b1.line(3), r"Fancy Model 9 [│┃]$", b1.line(3)); self.assertNotIn("%", b1.line(3))
        sq = [x for x in b1.rows[3].cells if x[0] == "■"]
        self.assertEqual(len([x for x in sq if x[1] == C_RED]), (77 * 6 + 50) // 100, "77% -> red, 5 of 6 cells")

    def test_effort_is_not_shown_yet(self):  # design: effort's field name is unverified, so it must not be read
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env, with_hud=False)
        self.feed(env, "n-b1", "Fancy Model 9", effort="xhigh", reasoning_effort="xhigh", thinking={"effort": "xhigh"})
        t = Term(env, w=200, h=50); self.addCleanup(t.close)
        cs = ready(t); self.assertTrue(cs); self.assertNotIn("xhigh", t.text())

    def test_live_update_when_statusline_runs_again(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env, with_hud=False)
        t = Term(env, w=200, h=50); self.addCleanup(t.close); self.assertTrue(ready(t))
        self.feed(env, "n-b1", "Late Model", pct=12)
        self.assertTrue(wait(lambda: "Late Model" in by_name(find_cards(t.snap()), "bravo-29").line(3), 8), "board should pick the file up on the next poll")


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
