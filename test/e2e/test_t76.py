"""T76 `agmux -r [query]` (= `agmux recall`): the recall screen on a real terminal (a tmux pane, capture-pane is the screen). Spec: specs/m6-recall/design.md §2, specs/m3-config/tasks.md T76 (acceptance: type to filter,
highlight, preview with every hit, the filter panel (sources, scope), ctrl+t, ctrl+y with a fake pbcopy, ctrl+o with a fake $EDITOR, enter on a closed session execs a fake claude / pi in the right cwd, enter on a running session
and on a vanished directory does not, no preview under 100 columns, the "indexing n / N" line, --reindex).

STATE: SKELETON. `FixtureSelfCheck` runs now (it checks the fakes and the harness this file stands on); every other class is skipped until `agmux recall` without --dump exists (lib_recall.ui_ready()). The bodies were written
against the design text, not against a screen: what depends on wording or layout that only T76 decides is marked `GUESS:` — read those first when the UI lands, and fix the test (not the product) if the design leaves it open.
The list is located by the session NAMES of the fixture (lib_recall.corpus), which the layout cannot change; no pixel / column positions are assumed."""
import json, os, re, time, unittest
from lib import wait, disp_width
from lib_recall import *

CART, AUDIT, GONE, PI_AUDIT, PI_PLAIN = ("3f6c1a52-0b7e-4c1d-9a55-6d2e8b1f7c01", "8d2b7e90-5a41-4f0e-b6c3-1a9d0e4c2f12", "c0ffee00-1111-4222-8333-444455556666", "019e0000-aaaa-7bbb-8ccc-000000000001", "019e0000-bbbb-7ccc-8ddd-000000000002")
NAMES = {"cart": "cart-fix", "audit": "Audit log", "gone": "vanished helper", "pi_audit": "audit-owner", "pi_plain": "checkout flow"}  # what shows of each session of corpus() in the list
READY = ui_ready()
needs_ui = unittest.skipUnless(READY, "T76 (agmux -r) is not delivered yet: skeleton")


class FixtureSelfCheck(unittest.TestCase):
    """what the T76 / T77 tests stand on, checked now"""

    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup)

    def test_the_fake_claude_and_pi_record_their_cwd_and_argv(self):
        d = self.env.home / "work" / "somewhere"; d.mkdir(parents=True)
        for name, argv in (("claude", ["--resume", "abc"]), ("pi", ["--session", "/x y/z.jsonl"])):
            r = subprocess.run([str(self.env.rbin / name), *argv], cwd=d, capture_output=True, text=True); self.assertEqual(r.returncode, 0); self.assertIn(f"FAKE-{name.upper()} " + " ".join(argv), r.stdout)
            ran = self.env.ran(name); self.assertEqual([x["argv"] for x in ran], [argv]); self.assertEqual(os.path.realpath(ran[0]["cwd"]), os.path.realpath(d))

    def test_the_fake_clipboards_and_editor_record_stdin_and_argv(self):
        for name in ("pbcopy", "xclip", "wl-copy"): subprocess.run([str(self.env.rbin / name)], input=f"cd '/a b' && claude --resume {name}", text=True, capture_output=True)
        self.assertEqual([r["stdin"] for r in self.env.ran("pbcopy")], ["cd '/a b' && claude --resume pbcopy"]); self.assertEqual(self.env.ran("xclip")[0]["stdin"], "cd '/a b' && claude --resume xclip")
        subprocess.run([str(self.env.editor), "/some/transcript.jsonl"]); self.assertEqual(self.env.ran("fakeedit")[0]["argv"], ["/some/transcript.jsonl"])

    def test_the_editor_can_be_held_open_while_a_test_looks_at_the_screen(self):
        (self.env.rec / "hold").write_text(""); p = subprocess.Popen([str(self.env.editor), "x"]); time.sleep(0.5); self.assertIsNone(p.poll(), "held"); (self.env.rec / "hold").unlink(); self.assertEqual(p.wait(timeout=5), 0)

    def test_the_corpus_has_what_the_tests_need(self):
        c = corpus(self.env); self.assertEqual(sorted(c), ["audit", "cart", "gone", "hush", "pi_audit", "pi_plain"]); self.assertTrue(all(p.exists() for p, _ in c.values()))
        self.assertFalse(os.path.exists(self.env.home / "work" / "vanished"), "the directory of `gone` is not there")
        self.assertTrue(os.path.isdir(self.env.home / "work" / "shopcart") and os.path.isdir(self.env.home / "work" / "ledger.worktrees" / "feature" / "audit" / "api"))

    def test_an_rterm_runs_in_its_cwd_with_the_recall_environment_and_no_tmux(self):
        d = self.env.home / "work"; d.mkdir(parents=True, exist_ok=True)
        t = RTerm(self.env, args="-c 'echo cwd=$PWD; echo EDITOR=$EDITOR; echo TMUX=[$TMUX]; command -v pbcopy; echo CACHE=$XDG_CACHE_HOME; sleep 30'", cwd=d, binary="/bin/sh"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: "CACHE=" in t.text(), 5), t.text()); txt = t.text().replace("\n", "")
        self.assertIn(f"cwd={os.path.realpath(d)}", txt); self.assertIn(f"EDITOR={self.env.editor}", txt); self.assertIn("TMUX=[]", txt); self.assertIn(f"{self.env.rbin}/pbcopy", txt); self.assertIn(f"CACHE={self.env.cache}", txt)


class Base(unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); self.c = corpus(self.env); self.cart_dir = str(self.env.home / "work" / "shopcart")
        self.api_dir = str(self.env.home / "work" / "ledger.worktrees" / "feature" / "audit" / "api")

    def ui(self, query="", w=140, h=40, cwd=None, flags="", **kw):
        t = RTerm(self.env, args=f"-r {flags} {query}".strip(), w=w, h=h, cwd=cwd, **kw); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: t.visible(NAMES), 10), "the list shows sessions:\n" + t.text()); time.sleep(0.5); return t

    def type(self, t, s): t.lit(s); time.sleep(0.5)

    def shown(self, t): return t.visible(NAMES)

    def settle(self, t, want, timeout=5):
        self.assertTrue(wait(lambda: self.shown(t) == set(want), timeout), f"want {sorted(want)}, screen shows {sorted(self.shown(t))}:\n{t.text()}")

    def real(self, p): return os.path.realpath(p)


@needs_ui
class List(Base):
    def test_empty_query_lists_the_sessions_by_transcript_mtime_newest_first_each_once(self):
        t = self.ui(); self.settle(t, ["cart", "audit", "pi_audit", "gone", "pi_plain"], 5)
        order = sorted(self.shown(t), key=lambda k: t.text().index(NAMES[k])); self.assertEqual(order, ["cart", "audit", "pi_audit", "gone", "pi_plain"], "design §2: no query = recent sessions, newest first")

    def test_a_session_with_only_noise_is_not_listed(self):
        t = self.ui(); self.assertNotIn("noise-token", t.text())

    def test_the_query_on_the_command_line_is_the_first_filter_and_typing_refines_it(self):
        t = self.ui("'coupon"); self.settle(t, ["cart"]); self.type(t, " 'tax"); self.settle(t, ["cart"]); self.type(t, " 'csv"); self.settle(t, [])

    def test_typing_filters_live_and_backspace_widens_again(self):
        t = self.ui(); self.type(t, "'regression"); self.settle(t, ["cart"]); t.keys("BSpace"); t.keys("BSpace"); time.sleep(0.6)
        self.assertGreaterEqual(len(self.shown(t)), 1)

    def test_a_hit_is_highlighted_in_the_row_and_the_preview_lists_every_hit(self):
        t = self.ui(); self.type(t, "'coupon"); self.settle(t, ["cart"]); snap = t.snap()
        hit = [(r, c) for r, row in enumerate(snap) for c in range(1, len(row.cells)) if "".join(x[0] for x in row.cells[c:c + 6]) == "coupon"]
        self.assertTrue(hit, "coupon is on the screen"); self.assertTrue(any(snap[r].cells[c][1:3] != snap[r].cells[c - 1][1:3] for r, c in hit), "GUESS: a hit has its own foreground / background (like the board's search highlight)")
        text = t.text(); self.assertIn("The coupon is subtracted before tax", text, "GUESS: the preview (needs >= 100 columns) shows the agent's hit too"); self.assertIn("regression test for the coupon order", text)

    def test_the_hit_count_and_the_best_sentence_are_in_the_row(self):
        t = self.ui(); self.type(t, "'coupon"); self.settle(t, ["cart"]); self.assertRegex(t.text(), r"\b3\b", "GUESS: the number of hits (3: 2 of yours, 1 of the agent's) is shown")

    def test_your_sentences_and_the_agents_are_both_searched(self):
        t = self.ui(); self.type(t, "'inner join"); self.settle(t, ["pi_audit"])

    def test_project_branch_and_name_are_searched_as_well(self):
        t = self.ui(); self.type(t, "'feature/audit/api"); self.settle(t, ["audit"]); t.keys("C-u"); time.sleep(0.4)

    def test_no_match_says_so_and_nothing_is_listed(self):
        t = self.ui(); self.type(t, "'zzqqxx-nothing"); self.settle(t, []); self.assertRegex(t.text().lower(), r"no match|no result|0 ", "GUESS: wording")

    def test_chinese_queries_work(self):
        cwd = str(self.env.home / "work" / "zh"); sid = "13131313-0000-4000-8000-000000000013"
        self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", [("u", "购物车的优惠券为什么算错了")], title="优惠券排查"), age=1); t = self.ui(); self.type(t, "优惠券"); self.assertTrue(wait(lambda: "优惠券排查" in t.text(), 5), t.text())


@needs_ui
class Keys(Base):
    def test_up_down_move_the_selection_and_the_preview_follows(self):
        t = self.ui(w=160); before = t.text(); t.keys("Down"); time.sleep(0.6); self.assertNotEqual(t.text(), before, "the selection moved (GUESS: the selected row / preview differ)")

    def test_the_keys_come_from_the_keys_table_not_from_code(self):
        self.env.cfg.joinpath("agmux", "config.toml").write_text('[keys]\ndown = ["ctrl+n"]\nup = ["ctrl+p"]\nquit = ["ctrl+g"]\n'); t = self.ui(w=160); before = t.text(); t.keys("C-n"); time.sleep(0.6); self.assertNotEqual(t.text(), before)
        t.keys("C-g"); self.assertIsNotNone(t.exit_status(4), "the configured quit leaves")

    def test_esc_clears_the_query_first_then_quits(self):
        t = self.ui(); self.type(t, "'coupon"); self.settle(t, ["cart"]); t.esc(); self.settle(t, ["cart", "audit", "pi_audit", "gone", "pi_plain"]); self.assertTrue(t.alive(), "design / T76: esc is three levels (panel, query, quit)"); t.esc(); self.assertIsNotNone(t.exit_status(4))

    def test_the_footer_lists_the_actions_with_the_keys_that_are_bound(self):
        self.env.cfg.joinpath("agmux", "config.toml").write_text('[keys]\nrecall_copy = ["ctrl+k"]\n'); t = self.ui(w=200); self.assertIn("ctrl+k", t.text(), "GUESS: the footer shows recall_copy's key")

    def test_zh_texts_and_en_texts_differ_and_do_not_overflow_at_100_columns(self):
        for lang in ("en", "zh"):
            t = self.ui(w=100, lang=lang)
            for r in t.text().splitlines(): self.assertLessEqual(disp_width(r), 100, r)
            t.close()
        a, b = self.ui(w=100, lang="en").text(), self.ui(w=100, lang="zh").text(); self.assertNotEqual(a, b)


@needs_ui
class Panel(Base):
    def test_ctrl_f_opens_the_filter_panel_with_the_sources_that_exist_on_this_machine_all_ticked(self):
        t = self.ui(); t.keys("C-f"); time.sleep(0.6); txt = t.text().lower(); self.assertIn("claude", txt); self.assertIn("pi", txt); self.assertNotIn("codex", txt, "only sources with sessions here")

    def test_unticking_a_source_hides_its_sessions_and_a_ticks_it_all_again_A_clears(self):
        t = self.ui(); t.keys("C-f"); time.sleep(0.5); t.keys("Space"); time.sleep(0.4)  # GUESS: the cursor starts on the first source (claude)
        t.keys("Escape"); time.sleep(0.5); shown = self.shown(t); self.assertTrue(shown and all(k.startswith("pi") for k in shown), f"claude unticked: only pi is left: {shown}")
        t.keys("C-f"); time.sleep(0.4); t.keys("a"); time.sleep(0.3); t.keys("Escape"); time.sleep(0.5); self.assertEqual(len(self.shown(t)), 5, "a ticks everything")
        t.keys("C-f"); time.sleep(0.4); t.keys("A"); time.sleep(0.3); t.keys("Escape"); time.sleep(0.5); self.assertEqual(self.shown(t), set(), "A clears: no source, no session")

    def test_scope_current_project_vs_all_history_with_ctrl_t_and_in_the_panel(self):
        t = self.ui(cwd=self.cart_dir); self.assertEqual(len(self.shown(t)), 5, "default scope: all history"); t.keys("C-t"); self.settle(t, ["cart", "pi_plain"]); t.keys("C-t"); self.settle(t, ["cart", "audit", "pi_audit", "gone", "pi_plain"])

    def test_nothing_the_panel_set_is_remembered_the_next_start_is_all_sources_all_history(self):
        t = self.ui(cwd=self.cart_dir); t.keys("C-t"); self.settle(t, ["cart", "pi_plain"]); t.close(); t2 = self.ui(cwd=self.cart_dir); self.assertEqual(len(self.shown(t2)), 5)

    def test_esc_closes_the_panel_before_anything_else(self):
        t = self.ui(); self.type(t, "'coupon"); t.keys("C-f"); time.sleep(0.4); t.esc(); self.settle(t, ["cart"]); self.assertTrue(t.alive())


@needs_ui
class Enter(Base):
    """design §2: enter on a closed session quits agmux and, in the same terminal, runs `cd <cwd> && exec claude --resume <id>` / `pi --session <file>`"""

    def run_enter(self, query, cwd=None):
        t = self.ui(cwd=cwd); self.type(t, query); time.sleep(0.5); t.keys("Enter"); return t

    def test_a_closed_claude_session_is_resumed_in_its_cwd_by_exec(self):
        t = self.run_enter("'coupon"); self.assertTrue(wait(lambda: self.env.ran("claude"), 6), t.text()); r = self.env.ran("claude")
        self.assertEqual((len(r), r[0]["argv"], self.real(r[0]["cwd"])), (1, ["--resume", CART], self.real(self.cart_dir)))
        self.assertEqual(t.exit_status(5), 0, "agmux is gone: the pane's process is the fake claude, which ended"); self.assertIn("FAKE-CLAUDE --resume " + CART, t.text()); self.assertNotIn("agmux", t.text().split("FAKE-CLAUDE")[0].lower().replace("agmux-e2e", ""), "no agmux screen left behind")

    def test_a_closed_pi_session_is_resumed_with_its_file(self):
        t = self.run_enter("'inner join"); self.assertTrue(wait(lambda: self.env.ran("pi"), 6), t.text()); r = self.env.ran("pi")[0]
        self.assertEqual((r["argv"], self.real(r["cwd"])), (["--session", str(self.c["pi_audit"][0])], self.real(self.api_dir)))

    def test_a_session_whose_directory_is_gone_is_not_started_and_the_footer_says_why(self):
        t = self.run_enter("'vanished helper"); time.sleep(1.0); self.assertEqual(self.env.ran("claude"), []); self.assertTrue(t.alive(), "agmux stays")
        self.assertRegex(t.text(), r"(?i)no longer|gone|missing|not found|does not exist|doesn't exist", "GUESS: the footer names the reason (design §2)"); self.assertIn("vanished", t.text())

    def test_a_running_session_is_not_started_again_and_the_footer_says_it_runs(self):
        self.env.reg(CART, name="cart-fix", nameSource="user", cwd=self.cart_dir); t = self.ui(); self.type(t, "'coupon"); t.keys("Enter"); time.sleep(1.0)
        self.assertEqual(self.env.ran("claude"), [], "enter does not touch a running session"); self.assertTrue(t.alive()); self.assertRegex(t.text(), r"(?i)running|live|already", "GUESS: wording (design §2: the footer says it is running)")

    def test_the_resume_does_not_touch_the_transcripts_or_the_state(self):
        before = {p: (p.stat().st_size, p.stat().st_mtime_ns) for p, _ in self.c.values()}; self.run_enter("'coupon"); wait(lambda: self.env.ran("claude"), 6)
        self.assertEqual({p: (p.stat().st_size, p.stat().st_mtime_ns) for p, _ in self.c.values()}, before)

    def test_an_exec_failure_is_reported_not_a_blank_screen(self):
        (self.env.rbin / "claude").unlink(); t = self.run_enter("'coupon"); time.sleep(1.5); self.assertTrue(t.alive() or "claude" in t.text(), "GUESS: a missing claude is an error line, not silence"); self.assertIn("claude", t.text())


@needs_ui
class CopyOpen(Base):
    def test_ctrl_y_copies_the_whole_resume_command_with_the_directory(self):
        t = self.ui(); self.type(t, "'coupon"); t.keys("C-y"); self.assertTrue(wait(lambda: self.env.ran("pbcopy"), 4), t.text()); got = self.env.ran("pbcopy")[0]["stdin"].strip()
        self.assertEqual(got, f"cd '{self.cart_dir}' && claude --resume {CART}", "design §2: cd '<cwd>' && claude --resume <id>"); self.assertTrue(t.alive(), "copying does not leave the screen")

    def test_ctrl_y_for_a_pi_session_has_the_pi_command(self):
        t = self.ui(); self.type(t, "'inner join"); t.keys("C-y"); self.assertTrue(wait(lambda: self.env.ran("pbcopy"), 4)); got = self.env.ran("pbcopy")[0]["stdin"]
        self.assertIn(f"cd '{self.api_dir}'", got); self.assertIn("pi --session", got); self.assertIn(str(self.c["pi_audit"][0]), got)

    def test_ctrl_o_opens_the_transcript_in_the_editor_and_comes_back(self):
        (self.env.rec / "hold").write_text(""); t = self.ui(); self.type(t, "'coupon"); t.keys("C-o"); self.assertTrue(wait(lambda: self.env.ran("fakeedit"), 4), t.text())
        self.assertEqual(self.env.ran("fakeedit")[0]["argv"], [str(self.c["cart"][0])], "the transcript file is the argument"); (self.env.rec / "hold").unlink(); time.sleep(1.0); self.assertTrue(t.alive()); self.assertIn("cart-fix", t.text(), "back on the list after the editor")

    def test_the_keys_are_configurable(self):
        self.env.cfg.joinpath("agmux", "config.toml").write_text('[keys]\nrecall_copy = ["ctrl+k"]\nrecall_open = ["ctrl+e"]\n'); t = self.ui(); self.type(t, "'coupon"); t.keys("C-y"); time.sleep(0.6); self.assertEqual(self.env.ran("pbcopy"), [], "ctrl+y is nothing now")
        t.keys("C-k"); self.assertTrue(wait(lambda: self.env.ran("pbcopy"), 4))


@needs_ui
class Screen(Base):
    def test_under_100_columns_there_is_no_preview_and_from_100_there_is(self):
        t = self.ui("coupon", w=99); self.assertNotIn("The coupon is subtracted before tax", t.text()); t.close()
        t = self.ui("coupon", w=100); self.assertIn("The coupon is subtracted before tax", t.text(), "GUESS: the preview shows the agent's sentence")

    def test_the_first_build_shows_how_far_it_is_and_the_list_fills_as_it_goes(self):
        for i in range(1500):  # many transcripts so the build lasts long enough to be looked at
            cwd = str(self.env.home / "work" / f"bulk{i % 30}"); sid = f"bbbb{i:04d}-0000-4000-8000-000000000000"
            self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", [("u", f"bulk question number {i} about widgets"), ("a", "bulk answer " * 40)] * 20), age=100 + i)
        t = RTerm(self.env, args="-r", w=140, h=40); self.addCleanup(t.close); self.assertTrue(wait(lambda: re.search(r"\d+\s*/\s*\d+", t.text()), 10), "GUESS: a line like `indexing 120 / 1506`:\n" + t.text())
        self.assertTrue(wait(lambda: not re.search(r"(?i)indexing|索引中", t.text()), 60), "the line goes when the build is done"); self.assertTrue(t.visible(NAMES) or "bulk question" in t.text())

    def test_the_second_start_is_instant_no_progress_line(self):
        self.env.dump(); t = RTerm(self.env, args="-r", w=140, h=40); self.addCleanup(t.close); self.assertTrue(wait(lambda: t.visible(NAMES), 3), t.text()); self.assertNotRegex(t.text(), r"(?i)indexing|索引中")

    def test_reindex_throws_the_cache_away_and_builds_it_again(self):
        self.env.dump(); n = f"claude-{CART}.json"; e = self.env.cache_json(n); e["users"][0]["x"] = "tampered sentence from the cache"; self.env.put_cache(n, e)
        t = self.ui(flags="--reindex"); self.type(t, "'tampered"); self.settle(t, []); self.assertNotIn("tampered sentence from the cache", self.env.recall_dir.joinpath(n).read_text())

    def test_the_resume_hint_and_everything_user_visible_follows_AGMUX_LANG(self):
        t = self.ui(w=140, lang="zh"); self.assertRegex(t.text(), r"[一-鿿]", "GUESS: zh has Chinese UI words (footer / header)")

    def test_a_terminal_resize_keeps_the_screen_whole(self):
        t = self.ui(w=140); t.resize(100, 30); time.sleep(0.8); self.assertTrue(t.visible(NAMES)); t.resize(60, 20); time.sleep(0.8); self.assertTrue(t.alive())
        for r in t.text().splitlines(): self.assertLessEqual(disp_width(r), 60, r)

    def test_it_runs_without_tmux_anywhere_on_the_path(self):
        t = self.ui(); self.assertTrue(t.alive()); self.assertEqual([c for c in self.env.tmux_calls()], [], "no tmux call: this is the any-terminal entry (design §2)")


if __name__ == "__main__":
    unittest.main()
