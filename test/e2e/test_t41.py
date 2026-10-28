"""T41 a branch that does not fit on card line 2 loses its leading `/` segments, one at a time, and gets a `…` in front of what is left (`…alert-group`); when not even the last
segment with the `…` fits, the last segment is cut at the tail (`alert-gr…`, no leading `…`); a branch without `/` is cut at the tail as before; with an agent at the right edge the room is
what the agent leaves; `/` search draws only the shown characters (hits in the left-out segments are not drawn, the rest move with the text); `s` flash reads the shown text
(and, since 9730bc7, the room the agent leaves). Spec: specs/m3-config/tasks.md「第八批」T41, design.md「分组方式」末句.

Oracle: `expect()` below is written from the spec text, not from the product code. Geometry: single column (terminal <= 80 wide), the branch icon is card cell 9, the branch gets the cells up to the
right border, less the agent and one space. One case the spec leaves open and the orchestrator decided: `…last` does not fit but `last` does -> the bare `last` (no mark, no tail cut); the sweep checks it at every width."""
import json, unittest
from lib import *
from lib_recall import cursor
from test_t3 import by_name, C_FOCUS
from test_t37 import SETTING, USER

ICON, MATCH = "", ("rgb", 242, 208, 107)
AGENT_NAME = "orchestrator"
BRANCHES = ["hotfix/alert-group", "feature/permission-dashboard", "feature/team/permission-dashboard-v2", "no-slash-branch-name-that-is-rather-long-indeed-yes",
            "feature/中文分支名称很长很长很长很长很长很长很长很长", "x/" + "averyverylongsinglelastsegment" * 3]  # the last one never fits: its last segment is 90 cells


def wd(s): return disp_width(s)


def cut(s, avail):
    out = ""
    for ch in s:
        if wd(out + ch) + 1 > avail: break
        out += ch
    return out + "…"


def expect(branch, avail):
    """what line 2 shows of `branch` in `avail` cells (after the icon and its space)"""
    if wd(branch) <= avail: return branch
    if "/" not in branch: return cut(branch, avail)
    segs = branch.split("/")
    for k in range(1, len(segs)):
        cand = "…" + "/".join(segs[k:])
        if wd(cand) <= avail: return cand
    last = segs[-1]  # decided (orchestrator, review of 9730bc7): `…last` does not fit but `last` does -> the bare last segment, no mark
    return last if wd(last) <= avail else cut(last, avail)


def fixture(env, branches, agents=()):
    """one session (own project, panes %301..) per branch, names m00..; the sessions in `agents` get a transcript naming the agent AGENT_NAME"""
    names = build_many_fixture(env, sizes=[1] * len(branches)); S = json.loads((env.fake / "show.json").read_text())
    for i, (s, b) in enumerate(zip(S, branches)):
        s["gitBranch"] = b
        if i in agents: log = env.tmp / f"agent{i}.jsonl"; log.write_text(SETTING(AGENT_NAME) + USER); s["logPath"] = str(log)  # (one transcript each: the branch is a line of it now)
    env.write_show(S); return names


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup)

    def board(self, n, w=40, h=40):
        t = Term(self.env, w=w, h=h); self.addCleanup(t.close); self.settle(t, n, w); return t

    def settle(self, t, n, w):
        ok = lambda: (lambda cs: len(cs) == n and all(c.ok and c.w == w for c in cs))(find_cards(t.snap()))
        self.assertTrue(wait(ok, 10), t.text()); time.sleep(0.3)

    def shown(self, card, agent=False):
        """(branch text as drawn, avail) of the card's line 2: from after the icon and its space to the right border, less the agent and one space when there is one"""
        cells = card.rows[2].cells; i = next(k for k, x in enumerate(cells) if x[0] == ICON); end = card.w - 1
        if agent:
            ag = len(AGENT_NAME); self.assertEqual("".join(c[0] for c in cells[end - ag:end]), AGENT_NAME, f"the agent whole at the right edge: {card.line(2)!r}"); end -= ag + 1
        return "".join(c[0] for c in cells[i + 2:end]).rstrip(), end - (i + 2)


class Sweep(Base):
    def sweep(self, branches, agents=()):
        names = fixture(self.env, branches, agents); t = self.board(len(branches), w=40, h=45); checked = 0
        for w in range(40, 81):
            if w != 40: t.resize(w, 45); self.settle(t, len(branches), w)
            cards = find_cards(t.snap())
            for i, b in enumerate(branches):
                c = by_name(cards, names[i]); got, avail = self.shown(c, i in agents); want = expect(b, avail)
                self.assertEqual(got, want, f"W={w} avail={avail} branch {b!r}: {c.line(2)!r}"); checked += 1
        return checked

    def test_every_width_without_an_agent(self):
        checked = self.sweep(BRANCHES); self.assertEqual(checked, 41 * len(BRANCHES))

    def test_every_width_with_an_agent_at_the_right_edge(self):
        checked = self.sweep(BRANCHES, agents=range(len(BRANCHES))); self.assertEqual(checked, 41 * len(BRANCHES))


class Examples(Base):
    """the spec's own examples, read off the screen without the oracle"""

    def test_one_cell_short_drops_the_prefix(self):
        names = fixture(self.env, ["hotfix/alert-group"], agents=[0]); t = self.board(1)  # W=40 with the agent: 15 cells; the whole branch is 18
        got, avail = self.shown(find_cards(t.snap())[0], True); self.assertEqual((got, avail), ("…alert-group", 15))

    def test_whole_branch_is_not_marked_when_it_just_fits(self):
        names = fixture(self.env, ["feature/permission-dashboard"]); t = self.board(1)  # 28 cells, 28 room
        got, avail = self.shown(find_cards(t.snap())[0]); self.assertEqual((avail, got), (28, "feature/permission-dashboard"))
        t.resize(41, 40); self.settle(t, 1, 41);  # (41 is still one column: the room is 29)
        self.assertEqual(self.shown(find_cards(t.snap())[0]), ("feature/permission-dashboard", 29))

    def test_a_branch_one_cell_too_long_for_its_room(self):
        fixture(self.env, ["feature/permission-dashboard1"]); t = self.board(1)  # 29 cells in 28
        self.assertEqual(self.shown(find_cards(t.snap())[0]), ("…permission-dashboard1", 28))

    def test_three_segments_lose_one_at_a_time(self):
        fixture(self.env, ["aaaaaaaaaaaa/bbbbbbbbbbbb/cccccccccccc/dddddddd"]); t = self.board(1)  # 49 cells, room 28
        self.assertEqual(self.shown(find_cards(t.snap())[0])[0], "…cccccccccccc/dddddddd")  # b/c/d is 35, c/d 22+1

    def test_only_the_last_segment_left_and_still_too_long_is_cut_at_the_tail_without_a_leading_ellipsis(self):
        fixture(self.env, ["x/" + "y" * 40]); t = self.board(1); got, avail = self.shown(find_cards(t.snap())[0])
        self.assertEqual((got, avail), ("y" * 27 + "…", 28)); self.assertFalse(got.startswith("…"))

    def test_the_last_segment_that_just_fits_without_the_ellipsis_is_shown_bare(self):
        fixture(self.env, ["aaaaaaaa/" + "y" * 28]); t = self.board(1)  # room 28: `…yyyy…` (29) does not fit, the 28-cell last segment does -> decided: shown as it is
        self.assertEqual(self.shown(find_cards(t.snap())[0]), ("y" * 28, 28))

    def test_no_slash_is_cut_at_the_tail(self):
        fixture(self.env, ["z" * 40]); t = self.board(1); self.assertEqual(self.shown(find_cards(t.snap())[0])[0], "z" * 27 + "…")

    def test_the_agent_role_makes_room_a_card_without_one_is_unchanged(self):
        fixture(self.env, ["release/hotfix/alert-group-dashboard"] * 2, agents=[1]); t = self.board(2); cs = find_cards(t.snap())
        self.assertEqual(self.shown(cs[0]), ("…alert-group-dashboard", 28), "no agent: 28 cells, the 22-cell `…alert-group-dashboard`")
        self.assertEqual(self.shown(cs[1], True), ("alert-group-da…", 15), "with the agent: 15 cells; `…alert-group-dashboard` (22) no longer fits and `alert-group-dashboard` is the last segment: cut at the tail, no leading `…`")


class Highlight(Base):
    BR = ["release/hotfix/alert-group-dashboard"]  # W=40: room 28 -> `…alert-group-dashboard` (22); `release/hotfix/` is left out

    def search(self, t, q):
        t.keys("/"); time.sleep(0.15); t.lit(q); time.sleep(0.5)

    def lit(self, row): return "".join(c[0] for c in row.cells if c[2] == MATCH and c[0].strip())

    def card(self, t, agent=False):
        cs = find_cards(t.snap()); self.assertEqual(len(cs), 1, t.text()); return cs[0]

    def test_a_hit_in_the_shown_part_is_drawn_where_the_text_is(self):
        fixture(self.env, self.BR); t = self.board(1); self.search(t, "dashboard"); c = self.card(t)
        self.assertEqual(self.shown(c)[0], "…alert-group-dashboard"); self.assertEqual(self.lit(c.rows[2]), "dashboard")
        i = c.rows[2].find("dashboard"); self.assertEqual([c.rows[2].cells[i + k][2] == MATCH for k in range(9)], [True] * 9, "exactly under the letters of `dashboard`")
        self.assertEqual(c.rows[2].cells[i - 1][2] == MATCH, False)

    def test_a_hit_in_the_left_out_segments_draws_nothing_but_the_card_stays(self):
        fixture(self.env, self.BR + ["other/branch"]); t = self.board(2); self.search(t, "release")
        cs = find_cards(t.snap()); self.assertEqual(len(cs), 1, "the card still matches (the whole branch is searched)"); self.assertEqual(self.shown(cs[0])[0], "…alert-group-dashboard"); self.assertEqual(self.lit(cs[0].rows[2]), "")

    def test_a_hit_that_spans_the_cut_draws_only_what_is_shown(self):
        fixture(self.env, self.BR); t = self.board(1); self.search(t, "hotfix/alert"); c = self.card(t)
        self.assertEqual(self.lit(c.rows[2]), "alert", "`hotfix/` is not on the card; the `…` is not drawn as a hit either"); self.assertTrue(c.ok, c.why)

    def test_tail_cut_branch_highlight_stops_at_the_cut(self):
        fixture(self.env, ["x/" + "y" * 20 + "needle" + "z" * 20]); t = self.board(1); self.search(t, "needle"); c = self.card(t)
        self.assertEqual(self.shown(c)[0], "y" * 20 + "needle" + "z" + "…"); self.assertEqual(self.lit(c.rows[2]), "needle"); self.assertTrue(c.ok, c.why)

    def test_the_tail_ellipsis_does_not_take_the_hit_background(self):
        fixture(self.env, ["x/" + "y" * 20 + "needle" + "z" * 20]); t = self.board(1); self.search(t, "needle" + "z" * 14); c = self.card(t); cells = c.rows[2].cells  # the hit runs past the cut
        i = next(k for k, x in enumerate(cells) if x[0] == "…"); self.assertEqual(self.lit(c.rows[2]), "needle" + "z"); self.assertEqual(cells[i - 1][2], MATCH, "the last shown character is still a hit")
        self.assertNotEqual(cells[i][2], MATCH, "the `…` is not a hit")

    def test_the_ellipsis_of_a_cut_name_is_name_coloured_and_not_a_hit(self):
        """32a2435: the cut mark is given by the caller: name colour on the name row (it had lost it and was the default colour), never the hit's yellow"""
        name = "".join(f"{i:02d}" for i in range(35)); fixture(self.env, ["main"]); S = json.loads((self.env.fake / "show.json").read_text()); S[0]["summary"] = name; self.env.write_show(S)
        t = self.board(1); self.search(t, name[10:60]); c = self.card(t); cells = c.rows[1].cells; i = next(k for k, x in enumerate(cells) if x[0] == "…"); first = cells[9]  # (the hit starts in the shown part and runs past the cut)
        self.assertEqual(cells[i - 1][2], MATCH, "the last shown character is a hit"); self.assertNotEqual(cells[i][2], MATCH, "the `…` is not a hit"); self.assertEqual(first[0], "0")
        self.assertNotEqual(first[1], None); self.assertEqual(cells[i][1], first[1], f"the `…` has the name colour {first[1]}, got {cells[i][1]}")

    def test_the_ellipsis_of_a_cut_branch_is_branch_coloured_and_the_status_detail_one_is_detail_coloured(self):
        fixture(self.env, ["z" * 60]); S = json.loads((self.env.fake / "show.json").read_text()); S[0].update(status="working", pendingTool="mcp__server__" + "t" * 40); self.env.write_show(S)
        t = self.board(1); c = find_cards(t.snap())[0]; br = c.rows[2].cells; i = next(k for k, x in enumerate(br) if x[0] == "…"); self.assertEqual(br[i][1], br[10][1], "branch row: `…` in the branch colour")
        st = c.rows[3].cells; k = next(k for k, x in enumerate(st) if x[0] == "…")
        self.assertEqual(st[k][1], st[k - 1][1], f"status row: `…` in the detail colour: {c.line(3)!r}")

    def test_a_query_longer_than_the_top_bar_scrolls_so_the_caret_stays_in_view(self):
        # (was test_the_top_bar_ellipsis_is_plain: T79's shared input does not cut a long query with `…` any more, it shows the part around the caret)
        fixture(self.env, ["main"]); t = self.board(1); t.keys("/"); time.sleep(0.2); t.lit("q" * 60 + "rst"); time.sleep(0.6); top = t.snap()[0]
        self.assertTrue(top.text.rstrip().endswith("0/1") or "/" in top.text, top.text); self.assertIn("qrst", top.text, "the end of the query, where the caret is, is what shows"); self.assertNotIn("…", top.text)
        x, y, vis = cursor(t); self.assertTrue(vis and y == 0 and 0 < x < t.w, f"the real caret is on the top row, inside the screen: {(x, y, vis)}"); self.assertLessEqual(disp_width(top.text), t.w)

    def test_a_cut_branch_with_an_agent_keeps_the_hit_in_the_shown_part(self):
        fixture(self.env, ["feature-alert-with-long-name-for-test"], agents=[0]); t = self.board(1); self.search(t, "alert"); c = self.card(t)
        self.assertEqual(self.shown(c, True)[0], "feature-alert-…"); self.assertEqual(self.lit(c.rows[2]), "alert")


class Flash(Base):
    PROMPT = "flash: type to narrow"  # (the bar is cut at 40 columns)

    def flash(self, t, text):
        t.keys("s"); self.assertTrue(wait(lambda: self.PROMPT in t.text(), 3), t.text()); t.lit(text); time.sleep(0.6)

    def hits(self, c): return "".join(x[0] for x in c.rows[2].cells if x[2] == MATCH and x[0].strip())

    def test_flash_marks_hits_in_the_shown_text_only(self):
        fixture(self.env, ["release/hotfix/alert-group-dashboard"]); t = self.board(1); self.flash(t, "alert"); c = find_cards(t.snap())[0]
        self.assertEqual(self.shown(c)[0][:6], "…alert"); self.assertEqual(self.hits(c), "alert")
        t.esc(); self.flash(t, "hotfix"); c = find_cards(t.snap())[0]; self.assertEqual(self.hits(c), "", "left out of the card: nothing to mark")
        t.esc(); self.flash(t, "release"); self.assertEqual(self.hits(find_cards(t.snap())[0]), "")

    def test_flash_reads_the_room_the_agent_leaves(self):
        fixture(self.env, ["feature-alert-with-long-name-for-test"], agents=[0]); t = self.board(1); got = self.shown(find_cards(t.snap())[0], True)[0]
        self.assertEqual(got, "feature-alert-…", "the room is 15")
        self.flash(t, "alert"); self.assertEqual(self.hits(find_cards(t.snap())[0]), "alert")
        t.esc(); self.flash(t, "name"); self.assertEqual(self.hits(find_cards(t.snap())[0]), "", "`name` is in the cut part, not on the card")


if __name__ == "__main__":
    unittest.main()
