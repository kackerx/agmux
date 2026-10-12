"""T7 fixed ordering + `f` fold. Spec: specs/m1-mvp/design.md「布局与卡片」(排序固定 / 折叠), specs/m1-mvp/tasks.md T7."""
import random, unittest
from lib import *
from test_t3 import ready, by_name, group_cards, NAMES, last_text_row

SID_BY_NAME = {v: k for k, v in NAMES.items()}  # full names are unique at 200 columns (prefixes are not: bravo-29 / bravo-bg)


def sid_of(card, extra=None):
    line = card.line(1)
    for k, v in (extra or {}).items():
        if k in line:
            return v
    for name, sid in SID_BY_NAME.items():
        if name in line:
            return sid
    raise AssertionError(f"unrecognised card: {line!r}")


def structure(term, extra=None):
    """[(title_text, x, [sid top->bottom])] in reading order + every card's (r, c, sid)."""
    snap = term.snap(); cards = [c for c in find_cards(snap) if c.ok]
    gs = group_cards(snap, cards)
    return ([(snap[tr].text[tx:tx + 60].strip(), tx, [sid_of(c, extra) for c in gc]) for tr, tx, gc in gs],
            sorted((c.r, c.c, sid_of(c, extra)) for c in cards))


def foot(t, n=3):
    return "\n".join(r.text for r in t.snap()[-n:])


class Ordering(unittest.TestCase):
    def test_order_does_not_depend_on_ccmux_array_order(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        sess = json.loads((env.fake / "show.json").read_text())
        rng = random.Random(7)
        variants = [sess, sess[::-1]] + [rng.sample(sess, len(sess)) for _ in range(3)]
        self.assertEqual(len({tuple(x["id"] for x in v) for v in variants}), 5, "five different array orders")
        env.write_show_orders(variants)
        t = Term(env, w=200, h=50); self.addCleanup(t.close)
        self.assertTrue(ready(t), t.text())
        seen, polls = [], env.show_calls()
        while len(seen) < 7:  # one capture per poll, each poll served a different order
            self.assertTrue(wait(lambda: env.show_calls() > polls, 6), "board stopped polling")
            polls = env.show_calls(); time.sleep(0.3)
            seen.append(structure(t))
        self.assertGreaterEqual(env.show_calls(), 7)
        titles, cards = seen[0]
        self.assertEqual([g[2] for g in titles], [["a1", "a2", "a3"], ["b1", "b2"], ["c1", "c2"], ["d1"], ["e1"]], "cwd order, then name order")
        for i, s in enumerate(seen[1:], 1):
            self.assertEqual(s, seen[0], f"capture #{i} differs: a card moved when the array order changed")

    def test_same_name_by_id_and_lexicographic(self):
        env = Env(); self.addCleanup(env.cleanup)
        now = datetime.datetime.now(datetime.timezone.utc)

        def mk(id, name, sub, branch):
            return dict(id=id, summary=name, agentType="claude", cwd=f"{env.home}/{sub}", project=sub, mainRepoRoot=f"{env.home}/{sub}", gitBranch=branch,
                        status="idle", attentionType=None, pendingTool=None, tmuxPane="%1", trackingMode="native", subagents=[], nativeSessionId="n-" + id,
                        logPath=None, lastActivityAt=iso(now), updatedAt=iso(now), backgroundDetail=None, attentionState=None)
        S = [mk("t2", "twin", "z", "b-two"), mk("t10", "twin", "z", "b-ten"), mk("m1", "mid", "z", "b-mid"), mk("k1", "aaa", "z", "b-aaa"),
             mk("p1", "p", "ab", "b-p"), mk("p2", "p", "a", "b-pa"), mk("p3", "p", "m", "b-pm")]
        env.write_show_orders([S, S[::-1], S[3:] + S[:3]])
        t = Term(env, w=200, h=50); self.addCleanup(t.close)
        cs = wait(lambda: (lambda c: c if len(c) == 7 and all(x.ok for x in c) else None)(find_cards(t.snap())), 8); self.assertTrue(cs, t.text())
        for _ in range(3):  # three polls, three different array orders
            n = env.show_calls(); self.assertTrue(wait(lambda: env.show_calls() > n, 6)); time.sleep(0.3)
            snap = t.snap(); gs = group_cards(snap, [c for c in find_cards(snap) if c.ok])
            titles = [snap[tr].text[tx:tx + 30].split(" (")[0].strip() for tr, tx, _ in gs]
            self.assertEqual(titles, ["▾ ~/a", "▾ ~/ab", "▾ ~/m", "▾ ~/z"], "groups by cwd string: /a < /ab < /m < /z")
            z = [c.line(2) for c in gs[3][2]]
            order = [next(b for b in ("b-aaa", "b-mid", "b-ten", "b-two") if b in line) for line in z]
            self.assertEqual(order, ["b-aaa", "b-mid", "b-ten", "b-two"], "by name (aaa < mid < twin), same name by id as a string (t10 < t2)")

    def test_focus_follows_the_session_when_a_rename_re_sorts(self):
        env = Env(); self.addCleanup(env.cleanup); build_fixture(env)
        t = Term(env, w=200, h=50); self.addCleanup(t.close); self.assertTrue(ready(t))
        t.keys("j"); time.sleep(0.3)  # a2
        S = json.loads((env.fake / "show.json").read_text()); S[0]["summary"] = "zzz-late"; env.write_show(S)  # a1 now sorts after pj-fallback
        extra = {"zzz-late": "a1"}
        self.assertTrue(wait(lambda: [x for x in structure(t, extra)[0] if x[1] == 0][0][2][:2] == ["a2", "a1"], 8), "rename re-sorts the group: " + t.text())
        t.keys("Enter"); wait(lambda: env.calls(), 3)
        self.assertEqual(env.calls(), [["switch", "a2"]], "focus stayed on the session (a2), not on the row position")


class Fold(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.t = Term(self.env, w=200, h=50); self.addCleanup(self.t.close)
        self.assertTrue(ready(self.t), self.t.text())

    def calls(self):
        time.sleep(0.8); return self.env.calls()

    def test_footer_lists_f(self):
        self.assertIn("f 折叠", last_text_row(self.t.snap()))

    def test_fold_and_restore(self):
        before = structure(self.t)
        self.t.keys("f")
        self.assertTrue(wait(lambda: structure(self.t)[0][0][0].startswith("▸"), 3), self.t.text())
        titles, cards = structure(self.t)
        self.assertRegex(titles[0][0], r"^▸ ~/work/alpha \(3\)", "folded title keeps the count")
        self.assertEqual(titles[0][2], [], "no cards under a folded title")
        self.assertEqual(len(cards), 6)
        self.assertTrue(all(s.startswith("▾") for s, _, _ in titles[1:]))
        self.assertEqual([x[1] for x in titles], [x[1] for x in before[0]], "a folded group still occupies its column slot")
        for g in range(1, 4):  # same band as the folded group: not a single card moves
            self.assertEqual(titles[g][2], before[0][g][2])
        keep = {c for c in before[1] if c[2] in ("b1", "b2", "c1", "c2", "d1")}
        self.assertTrue(keep <= set(cards), "cards of the other groups in the same band keep their exact (row, col)")
        self.assertEqual({c[1] for c in cards if c[2] == "e1"}, {c[1] for c in before[1] if c[2] == "e1"}, "later band keeps its column")
        self.t.keys("f")
        self.assertTrue(wait(lambda: structure(self.t) == before, 3), "f again restores the exact layout:\n" + self.t.text())
        self.assertEqual(self.env.calls(), []); self.assertTrue(self.t.alive())

    def test_folded_title_is_focusable_and_inert(self):
        self.t.keys("l"); time.sleep(0.3); self.t.keys("f")  # fold G2 (focus on b1)
        self.assertTrue(wait(lambda: structure(self.t)[0][1][0].startswith("▸"), 3))
        for k in ("H", "r", "d"):
            self.t.keys(k); time.sleep(0.4)
        self.t.keys("s"); time.sleep(0.3)
        self.assertNotIn("发送到", foot(self.t, 4), "no `s` prompt on a folded title")
        self.t.keys("x"); time.sleep(0.3)
        self.assertNotRegex(foot(self.t, 4), "杀掉|y 确认", "no kill confirm on a folded title")
        self.t.keys("y")
        self.assertEqual(self.calls(), [], "a folded title has no session: H / r / d / s / x must do nothing (never act on a hidden card)")
        self.assertTrue(self.t.alive()); self.assertEqual(len([c for c in find_cards(self.t.snap()) if c.ok]), 7)
        self.t.keys("j"); time.sleep(0.3); self.t.keys("k"); time.sleep(0.3)  # a one-item column: j / k stay put
        self.t.keys("Enter")  # enter on a folded title unfolds it, it does not switch
        self.assertTrue(wait(lambda: structure(self.t)[0][1][0].startswith("▾"), 3), self.t.text())
        self.assertEqual(self.calls(), [], "enter on a title must not call ccmux switch")
        self.t.keys("Enter")
        wait(lambda: self.env.calls(), 3)
        self.assertEqual(self.env.calls(), [["switch", "b1"]], "after unfolding the focus is the group's first card")

    def test_h_l_walk_through_folded_titles(self):
        self.t.keys("l"); time.sleep(0.3); self.t.keys("f")  # G2 folded, focused
        self.assertTrue(wait(lambda: structure(self.t)[0][1][0].startswith("▸"), 3), "G2 folded")
        self.t.keys("l"); time.sleep(0.3); self.t.keys("Enter")  # G3 first card
        wait(lambda: self.env.calls(), 3); self.assertEqual(self.env.calls(), [["switch", "c1"]])

    def test_h_back_onto_a_folded_title_then_f_unfolds(self):
        self.t.keys("l"); time.sleep(0.3); self.t.keys("f")
        self.assertTrue(wait(lambda: structure(self.t)[0][1][0].startswith("▸"), 3), "G2 folded")
        self.t.keys("h"); time.sleep(0.3)  # a1 (the focus left the folded title)
        self.t.keys("j"); time.sleep(0.3); self.t.keys("j"); time.sleep(0.3)  # a3: the only card whose rows overlap nothing on the right
        self.t.keys("l"); time.sleep(0.3)  # spatial fallback (T9): the nearest column to the right is the folded title
        self.t.keys("f")
        self.assertTrue(wait(lambda: structure(self.t)[0][1][0].startswith("▾"), 3), "f on a folded title unfolds it")
        self.assertEqual(self.calls(), [])

    def test_fold_survives_polls_and_is_per_run(self):
        self.t.keys("f"); n = self.env.show_calls()
        self.assertTrue(wait(lambda: self.env.show_calls() >= n + 2, 8)); time.sleep(0.3)
        self.assertTrue(structure(self.t)[0][0][0].startswith("▸"), "polling must not unfold")
        self.t.keys("q"); self.assertEqual(self.t.exit_status(), 0)
        t2 = Term(self.env, w=200, h=50); self.addCleanup(t2.close); self.assertTrue(ready(t2))
        self.assertTrue(structure(t2)[0][0][0].startswith("▾"), "fold state is not remembered across runs")

    def test_two_folded_groups_keep_their_slots(self):
        before = structure(self.t)
        for _ in range(2):
            self.t.keys("l"); time.sleep(0.2)
        self.t.keys("f"); time.sleep(0.3); self.t.keys("h"); time.sleep(0.2); self.t.keys("h"); time.sleep(0.2); self.t.keys("f")
        self.assertTrue(wait(lambda: [s[0][0] for s in structure(self.t)[0]][:3] == ["▸", "▾", "▸"], 3), self.t.text())
        titles, cards = structure(self.t)
        self.assertEqual([x[1] for x in titles], [x[1] for x in before[0]])
        self.assertEqual({c[2] for c in cards}, {"b1", "b2", "d1", "e1"})


if __name__ == "__main__":
    unittest.main()
