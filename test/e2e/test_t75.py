"""T75 recall index + Claude / pi sources, through `agmux recall --dump [--reindex] [--] [query...]` (one session per line: source, id, name, project, branch, mtime, hits, snippet; tab separated).
Spec: specs/m6-recall/design.md §3 §4, specs/m3-config/tasks.md T75 (+ the dev notes under 第十五批). Black box: transcripts of the real formats in a throw-away $HOME (lib_recall: Claude `projects/*/*.jsonl`, pi
`.pi/agent/sessions/*/*.jsonl`), the cache in a temp $XDG_CACHE_HOME. The fuzzy matcher is a subsequence one (fzf): a query that must NOT match something is written with the `'exact` operator or is long enough.
What the dump does not show (the cache's own fields) is read from the cache file: <cache>/agmux/recall/<source>-<transcript name>.json."""
import hashlib, json, os, pathlib, re, shutil, threading, unittest
from lib_recall import *

AUDIT, CART, GONE = "8d2b7e90-5a41-4f0e-b6c3-1a9d0e4c2f12", "3f6c1a52-0b7e-4c1d-9a55-6d2e8b1f7c01", "c0ffee00-1111-4222-8333-444455556666"
PI_AUDIT, PI_PLAIN = "019e0000-aaaa-7bbb-8ccc-000000000001", "019e0000-bbbb-7ccc-8ddd-000000000002"
PI_STAMP = "2026-10-01T08-00-00-000Z"


class Base(unittest.TestCase):
    def setUp(self):
        self.env = RecallEnv(); self.addCleanup(self.env.cleanup); self.c = corpus(self.env)

    def dump(self, *q, **kw):
        rows, r = self.env.dump(*q, **kw); self.assertEqual(r.returncode, 0, r.stderr); return rows

    def ids(self, rows): return [x.id for x in rows]

    def tree(self, root):
        """every file under root: {relative path: (size, mtime_ns, sha1)}"""
        out = {}
        for dp, _, fs in os.walk(root):
            for f in fs:
                p = os.path.join(dp, f); st = os.stat(p); out[os.path.relpath(p, root)] = (st.st_size, st.st_mtime_ns, hashlib.sha1(pathlib.Path(p).read_bytes()).hexdigest())
        return out


class Columns(Base):
    def test_one_line_per_session_with_eight_tab_separated_fields_newest_first_for_no_query(self):
        rows = self.dump()
        self.assertEqual(self.ids(rows), [CART, AUDIT, PI_AUDIT, GONE, PI_PLAIN], "by transcript mtime, newest first; the session with only noise has no line")
        self.assertEqual([x.source for x in rows], ["claude", "claude", "pi", "claude", "pi"])
        out = self.env.run("recall", "--dump").stdout; self.assertEqual({l.count("\t") for l in out.splitlines()}, {7}, "no tab or newline inside a field")

    def test_the_fields_of_a_claude_session_named_by_the_user(self):
        x = self.dump()[0]
        self.assertEqual((x.name, x.project, x.branch, x.mtime, x.hits), ("cart-fix", "shopcart", "feat/cart", rfc3339(when(10)), "5"), "custom-title beats the later ai-title; 3 sentences of yours + 2 replies")

    def test_the_last_thing_you_said_is_the_snippet_for_no_query(self):
        self.assertEqual(self.dump()[0].snippet, "now add a regression test for the coupon order")

    def test_a_worktree_is_listed_under_the_main_repository_a_directory_that_is_gone_under_its_own_name(self):
        by = {x.id: x for x in self.dump()}
        self.assertEqual((by[AUDIT].project, by[PI_AUDIT].project), ("ledger", "ledger"), "…/ledger.worktrees/feature/audit/api is a worktree of the repo `ledger`")
        self.assertEqual(by[GONE].project, "vanished", "the directory does not exist: its base name")
        self.assertEqual(by[PI_PLAIN].project, "shopcart")

    def test_the_name_is_the_ai_title_without_a_custom_one_and_the_first_sentence_without_any(self):
        by = {x.id: x for x in self.dump()}
        self.assertEqual(by[AUDIT].name, "Audit log export"); self.assertEqual(by[GONE].name, "rename the vanished helper before the directory goes")

    def test_pi_name_is_the_session_info_name_else_the_first_sentence_and_its_id_is_the_one_in_the_header_not_the_file_name(self):
        by = {x.id: x for x in self.dump()}
        self.assertEqual(by[PI_AUDIT].name, "audit-owner-join"); self.assertEqual(by[PI_PLAIN].name, "summarise the checkout flow for the new hire")
        self.assertEqual(by[PI_AUDIT].branch, "", "pi records no branch")

    def test_the_last_session_info_wins_and_the_last_branch_that_is_not_HEAD(self):
        sid = "aaaa1111-0000-4000-8000-000000000001"; cwd = str(self.env.home / "work" / "wanderer")
        lines = claude_lines(sid, cwd, "feat/a", [("u", "first message of the day"), ("raw", {"type": "user", "isSidechain": False, "cwd": cwd, "gitBranch": "HEAD", "message": {"role": "user", "content": "detached head sentence here"}}),
                                                  ("u", "second message after the checkout")], title="first-name") + [dumps({"type": "custom-title", "customTitle": "second-name"})]
        self.env.claude_file(cwd, sid, lines, age=5); x = [x for x in self.dump() if x.id == sid][0]
        self.assertEqual((x.name, x.branch), ("second-name", "feat/a"), "HEAD is not a branch; the later custom-title replaces the earlier one")
        pid = "019e1111-0000-7000-8000-00000000000a"; self.env.pi_file(cwd, pid, pi_lines(pid, cwd, [("u", "hello there pi")], name="one") + [dumps({"type": "session_info", "id": "z", "name": "two", "timestamp": "2026-10-01T09:00:00.000Z"})], age=6)
        self.assertEqual([x for x in self.dump() if x.id == pid][0].name, "two")

    def test_claude_session_id_in_the_dump_is_the_transcript_file_name(self):
        sid = "bbbb2222-0000-4000-8000-000000000002"; cwd = str(self.env.home / "work" / "idcheck")
        self.env.claude_file(cwd, sid, claude_lines("some-other-id-in-the-lines", cwd, "main", [("u", "a sentence for the id check")]), age=3)
        self.assertIn(sid, self.ids(self.dump()), "`claude --resume <id>` takes the file's name")


class Noise(Base):
    def sentences(self, sid):
        e = self.env.cache_json(f"claude-{sid}.json"); return [l["x"] for l in e["users"] or []], [l["x"] for l in e["replies"] or []]

    def test_what_nobody_said_is_not_a_sentence(self):
        cwd = str(self.env.home / "work" / "noisy"); sid = "cccc3333-0000-4000-8000-000000000003"
        side = {"type": "assistant", "isSidechain": True, "cwd": cwd, "gitBranch": "main", "message": {"role": "assistant", "content": [{"type": "text", "text": "subagent chatter that is not part of the session"}]}}
        meta = {"type": "user", "isMeta": True, "isSidechain": False, "cwd": cwd, "gitBranch": "main", "message": {"role": "user", "content": "Caveat: the messages below were generated by the user while running local commands"}}
        compact = {"type": "user", "isCompactSummary": True, "isSidechain": False, "cwd": cwd, "gitBranch": "main", "message": {"role": "user", "content": "A summary of the earlier conversation, long enough to keep"}}
        noise = ["This session is being continued from a previous conversation that ran out of context.", "<local-command-stdout>local-noise</local-command-stdout>", "<command-name>/clear</command-name>", "<command-message>clear</command-message>",
                 "<bash-input>ls</bash-input>", "<task-notification><task-id>b1</task-id></task-notification>", "<system-reminder>reminder-noise</system-reminder>", "[Request interrupted by user]", "ok", "y", "好的", "  \n "]
        turns = [("u", t) for t in noise] + [("raw", side), ("raw", meta), ("raw", compact), ("u", "[Image #1] what is wrong in this screenshot"), ("u", "a real question about the build"), ("a", "A real answer about the build."), ("a", "ok")]
        self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", turns), age=2); self.dump()
        users, replies = self.sentences(sid)
        self.assertEqual(users, ["[Image #1] what is wrong in this screenshot", "a real question about the build"], "the user said [Image #n]: it stays; everything above is the harness talking, or too short")
        self.assertEqual(replies, ["A real answer about the build."], "sidechain, tool calls / results, thinking and a 2-letter reply are not sentences")
        for q in ("local-noise", "reminder-noise", "chatter", "Caveat", "tool output that must never", "earlier conversation", "ran out of context"): self.assertEqual(self.dump("'" + q), [], f"{q!r} is in the transcript but nobody said it")

    def test_a_session_with_nothing_but_noise_is_not_listed_even_when_the_query_is_the_noise(self):
        self.assertNotIn(self.c["hush"][1], self.ids(self.dump())); self.assertEqual(self.dump("'noise-token"), [])

    def test_tool_results_and_thinking_of_both_agents_are_never_searchable(self):
        self.assertEqual(self.dump("'tool output that must never"), []); self.assertEqual(self.dump("'hmm"), [])

    def test_a_sentence_is_one_line_cut_at_200_characters_in_characters_not_bytes(self):
        cwd = str(self.env.home / "work" / "long"); sid = "dddd4444-0000-4000-8000-000000000004"
        long_en = "word " * 80; long_zh = "找回会话" * 80
        self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", [("u", long_en), ("u", long_zh), ("u", "one\n\ttwo   three\r\nfour")]), age=2); self.dump()
        users, _ = self.sentences(sid)
        self.assertEqual(users, [" ".join(long_en.split())[:200], long_zh[:200], "one two three four"], "white space folded to one space, at most 200 characters (a Chinese character is one)")
        self.assertEqual(len(users[1].encode()), 600, "200 characters of 3 bytes: cut by characters")

    def test_a_sentence_said_twice_is_kept_once_and_each_role_keeps_its_own(self):
        cwd = str(self.env.home / "work" / "dupes"); sid = "eeee5555-0000-4000-8000-000000000005"
        self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", [("u", "run the tests again please"), ("a", "Running the tests now."), ("u", "run the tests again please"), ("a", "Running the tests now."), ("a", "run the tests again please")]), age=2); self.dump()
        users, replies = self.sentences(sid); self.assertEqual(users, ["run the tests again please"]); self.assertEqual(replies, ["Running the tests now.", "run the tests again please"])

    def test_at_most_300_of_each_role_the_first_stays_and_the_newest_follow(self):
        cwd = str(self.env.home / "work" / "many"); sid = "ffff6666-0000-4000-8000-000000000006"
        turns = [t for i in range(400) for t in (("u", f"user sentence number {i:03d}"), ("a", f"agent sentence number {i:03d}"))]
        self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", turns), age=2); rows = self.dump(); users, replies = self.sentences(sid)
        want = lambda who: [f"{who} sentence number {i:03d}" for i in [0] + list(range(101, 400))]
        self.assertEqual((users, replies), (want("user"), want("agent")), "300 each: the first, then the last 299")
        self.assertEqual([x.hits for x in rows if x.id == sid], ["600"])
        self.assertEqual(self.dump("'number", "'399")[0].id, sid, "the newest are searchable"); self.assertEqual([x for x in self.dump("'number", "'050") if x.id == sid], [], "dropped ones are not")

    def test_a_broken_line_in_the_middle_is_skipped_and_the_rest_is_read(self):
        cwd = str(self.env.home / "work" / "broken"); sid = "99999999-0000-4000-8000-000000000009"; lines = claude_lines(sid, cwd, "main", [("u", "before the broken line here")])
        lines += ['{"type":"user","message":{"role":"user","content":"cut off in the mid', "not json at all"] + claude_lines(sid, cwd, "main", [("u", "after the broken line here")])[2:]
        self.env.claude_file(cwd, sid, lines, age=2); users, _ = (self.dump(), self.sentences(sid))[1]
        self.assertEqual(users, ["before the broken line here", "after the broken line here"])


class Search(Base):
    def test_a_keyword_finds_the_sessions_whose_sentences_have_it_best_first_with_the_hit_count_and_the_best_sentence(self):
        rows = self.dump("'coupon"); self.assertEqual(self.ids(rows), [CART]); x = rows[0]
        self.assertEqual(x.hits, "3", "3 sentences have it: 2 of yours, 1 of the agent's"); self.assertIn("coupon", x.snippet)
        self.assertEqual((x.name, x.project, x.branch), ("cart-fix", "shopcart", "feat/cart"), "the columns do not depend on the query")

    def test_the_agents_sentences_are_searched_too_in_both_agents(self):
        r = self.dump("'rounding"); self.assertEqual((self.ids(r), r[0].snippet), ([CART], "Thanks, the rounding happens per line."))
        r = self.dump("'inner join"); self.assertEqual((self.ids(r), r[0].snippet), ([PI_AUDIT], "The export joins owners with an inner join, so null owners drop out."))

    def test_a_sentence_of_yours_wins_a_tie_with_the_agents(self):
        cwd = str(self.env.home / "work" / "ties"); sid = "12121212-0000-4000-8000-000000000012"
        self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", [("a", "the tie phrase is here"), ("u", "the tie phrase is here too")]), age=1)
        r = self.dump("'tie phrase"); self.assertEqual((r[0].id, r[0].snippet), (sid, "the tie phrase is here too"))

    def test_project_branch_and_name_are_part_of_what_is_matched(self):
        self.assertEqual(sorted(self.ids(self.dump("'shopcart"))), sorted([CART, PI_PLAIN]), "project name: both sessions of the repo")
        self.assertEqual(self.ids(self.dump("'feature/audit/api")), [AUDIT], "the branch (pi records none)")
        self.assertEqual(self.ids(self.dump("'audit-owner-join")), [PI_AUDIT], "the session name")
        self.assertEqual(self.ids(self.dump("^pi", "'ledger")), [PI_AUDIT], "the source name is the start of what is matched: `^pi`")
        self.assertEqual(sorted(self.ids(self.dump("'ledger"))), sorted([AUDIT, PI_AUDIT]), "project of a worktree")

    def test_a_match_on_the_name_alone_lists_the_session_with_no_body_hits(self):
        r = self.dump("'cart-fix"); self.assertEqual((self.ids(r), r[0].hits), ([CART], "0"), "tasks.md T75 decision (2): the hit count is sentences whose own text matched; the name is not in the text")

    def test_no_match_is_empty_output_and_exit_0(self):
        r = self.env.run("recall", "--dump", "'zzqqxx-nothing"); self.assertEqual((r.returncode, r.stdout, r.stderr), (0, "", ""))

    def test_fzf_operators_exact_prefix_negation_and_a_second_word_is_an_and(self):
        self.assertEqual(self.ids(self.dump("'coupon", "'tax")), [CART]); self.assertEqual(self.dump("'coupon", "'csv"), [], "all words must match (in one sentence)")
        self.assertEqual(self.ids(self.dump("'audit", "!'owner")), [AUDIT], "`!'owner` drops the sessions that say owner (the pi one has it in its name)")
        self.assertEqual(sorted(self.ids(self.dump("^pi"))), sorted([PI_AUDIT, PI_PLAIN]), "^ anchors at the start of the matched string, which starts with the source")
        self.assertEqual(self.ids(self.dump("csv$")), [AUDIT], "$ anchors at the end: the sentence's last word")

    def test_smartcase_a_capital_in_the_query_makes_it_case_sensitive(self):
        self.assertEqual(self.ids(self.dump("'Renamed")), [GONE]); self.assertEqual(self.ids(self.dump("'renamed")), [GONE], "lower case matches either")
        self.assertEqual(self.dump("'RENAMED"), [], "an upper case query does not match `Renamed`")

    def test_best_score_first_then_newest_for_equal_scores(self):
        a, b = str(self.env.home / "work" / "tie-a"), str(self.env.home / "work" / "tie-b")
        self.env.claude_file(a, "a0a0a0a0-0000-4000-8000-00000000000a", claude_lines("x", a, "main", [("u", "identical tie sentence")]), age=8)
        self.env.claude_file(b, "b0b0b0b0-0000-4000-8000-00000000000b", claude_lines("x", b, "main", [("u", "identical tie sentence")]), age=4)
        self.assertEqual([x.id[:2] for x in self.dump("'identical tie")], ["b0", "a0"], "the same sentence in two sessions: the newer transcript first")

    def test_a_query_of_several_words_is_the_same_as_one_argument_with_spaces(self):
        self.assertEqual(self.dump("'coupon 'tax"), self.dump("'coupon", "'tax"))

    def test_chinese_sentences_are_found_and_shown_whole(self):
        cwd = str(self.env.home / "work" / "zh"); sid = "13131313-0000-4000-8000-000000000013"
        self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", [("u", "购物车的优惠券为什么算错了"), ("a", "优惠券在税前扣除。")]), age=1)
        r = self.dump("优惠券"); self.assertEqual((self.ids(r), r[0].hits), ([sid], "2")); self.assertIn(r[0].snippet, ("购物车的优惠券为什么算错了", "优惠券在税前扣除。"), "the best sentence, whole")
        self.assertEqual(self.dump("税前")[0].snippet, "优惠券在税前扣除。")


class Incremental(Base):
    def test_a_sentence_added_to_a_transcript_makes_one_more_hit_and_only_the_new_bytes_are_read(self):
        p, sid = self.c["cart"]; self.dump(); name = f"claude-{sid}.json"; before = self.env.cache_json(name); size0 = p.stat().st_size
        self.assertEqual((before["offset"], before["size"]), (size0, size0), "read up to the end")
        others = {n: (self.env.recall_dir / n).stat().st_mtime_ns for n in self.env.cache_files() if n != name}
        self.assertEqual(self.dump("'coupon")[0].hits, "3")
        self.env.append(p, claude_lines(sid, "/x", "feat/cart", [("u", "also check the coupon for free shipping")])[2:], age=1)
        self.assertEqual(self.dump("'coupon")[0].hits, "4", "one sentence more"); after = self.env.cache_json(name)
        self.assertEqual((after["offset"], after["size"]), (p.stat().st_size, p.stat().st_size)); self.assertGreater(after["offset"], size0)
        self.assertEqual([l["x"] for l in after["users"]], [l["x"] for l in before["users"]] + ["also check the coupon for free shipping"], "the old sentences were not read again: same, in the same order; new one last")
        self.assertEqual(after["mtime"] // 10**9, when(1), "the dump's mtime follows")
        self.assertEqual(self.dump()[0].mtime, rfc3339(when(1)))
        self.assertEqual({n: (self.env.recall_dir / n).stat().st_mtime_ns for n in others}, others, "the cache files of the other sessions were not even rewritten")

    def test_a_session_that_grows_moves_to_the_top_and_gets_the_new_title(self):
        p, sid = self.c["gone"]; self.dump(); self.env.append(p, [dumps({"type": "custom-title", "customTitle": "now-named"})], age=1)
        rows = self.dump(); self.assertEqual((rows[0].id, rows[0].name), (sid, "now-named"), "a head the new part does not repeat stays as it was (cwd, branch): project / branch still there")
        self.assertEqual((rows[0].project, rows[0].branch), ("vanished", "main"))

    def test_a_line_the_agent_has_not_finished_is_left_for_next_time(self):
        p, sid = self.c["cart"]; self.dump(); full = claude_lines(sid, "/x", "feat/cart", [("u", "half written sentence about the coupon")])[2]
        self.env.append(p, [full[:60]], age=1, newline=False)
        self.assertEqual(self.dump("'half written"), [], "no newline yet: not a sentence"); mid = self.env.cache_json(f"claude-{sid}.json")
        self.assertLess(mid["offset"], p.stat().st_size, "the offset stops before the unfinished line")
        self.env.append(p, [full[60:]], age=1, newline=True)
        self.assertEqual(self.ids(self.dump("'half written")), [sid], "the line is read once it is finished")

    def test_a_transcript_that_got_shorter_is_read_again_whole(self):
        p, sid = self.c["cart"]; self.dump(); self.assertEqual(self.ids(self.dump("'coupon")), [CART])
        p.write_text("\n".join(claude_lines(sid, "/x", "feat/cart", [("u", "a completely different and shorter conversation")])) + "\n"); self.env.touch(p, 2)
        self.assertEqual(self.dump("'coupon"), [], "none of the old sentences is left"); r = self.dump("'completely different"); self.assertEqual((self.ids(r), r[0].hits), ([CART], "1"))
        e = self.env.cache_json(f"claude-{sid}.json"); self.assertEqual(e["offset"], p.stat().st_size)

    def test_a_transcript_that_is_gone_is_no_longer_listed_and_its_cache_file_is_removed(self):
        p, sid = self.c["audit"]; self.dump(); self.assertIn(f"claude-{sid}.json", self.env.cache_files())
        p.unlink(); rows = self.dump(); self.assertNotIn(sid, self.ids(rows)); self.assertEqual(len(rows), 4)
        self.assertNotIn(f"claude-{sid}.json", self.env.cache_files()); self.assertEqual(self.dump("'export the audit log as csv"), [])

    def test_a_pi_transcript_is_gone_the_same_way(self):
        p, sid = self.c["pi_plain"]; self.dump(); p.unlink(); self.assertNotIn(sid, self.ids(self.dump())); self.assertEqual(len([n for n in self.env.cache_files() if n.startswith("pi-")]), 1)

    def test_a_new_transcript_appears_and_an_unchanged_one_is_not_rewritten(self):
        self.dump(); before = {n: (self.env.recall_dir / n).stat().st_mtime_ns for n in self.env.cache_files()}
        cwd = str(self.env.home / "work" / "fresh"); sid = "14141414-0000-4000-8000-000000000014"; self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", [("u", "a brand new session just started")]), age=0)
        rows = self.dump(); self.assertEqual(rows[0].id, sid); self.assertEqual({n: (self.env.recall_dir / n).stat().st_mtime_ns for n in before}, before)

    def test_the_cache_is_what_is_used_the_second_time_and_reindex_throws_it_away(self):
        p, sid = self.c["cart"]; self.dump(); name = f"claude-{sid}.json"; e = self.env.cache_json(name); e["users"][0]["x"] = "tampered sentence from the cache"; self.env.put_cache(name, e)
        self.assertEqual(self.ids(self.dump("'tampered sentence")), [CART], "transcript unchanged (mtime, size): the cache is trusted")
        rows, r = self.env.dump(flags=("--dump", "--reindex"), **{}); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(self.dump("'tampered"), [], "--reindex read the transcripts again"); self.assertEqual(self.dump("'cart total is wrong")[0].id, CART); self.assertEqual(len(rows), 5)

    def test_reindex_with_a_query_rebuilds_then_searches(self):
        self.dump(); (self.env.recall_dir / "notes.txt").write_text("x")
        rows, r = self.env.dump("'coupon", flags=("--dump", "--reindex")); self.assertEqual(self.ids(rows), [CART])
        self.assertNotIn("notes.txt", self.env.cache_files(), "the cache directory is rebuilt from scratch")
        self.assertEqual(len(self.env.cache_files()), 6)

    def test_a_cache_file_of_another_version_or_damaged_is_read_again_from_the_transcript(self):
        self.dump(); n1, n2 = f"claude-{CART}.json", f"claude-{AUDIT}.json"
        e = self.env.cache_json(n1); e["v"] = 999; e["users"] = [{"t": "2026-01-01T00:00:00Z", "x": "sentence of a format nobody reads"}]; self.env.put_cache(n1, e)
        (self.env.recall_dir / n2).write_text('{"v":1,"source":"claude","users":[{"t":"')  # torn
        rows = self.dump(); self.assertEqual(self.ids(rows), [CART, AUDIT, PI_AUDIT, GONE, PI_PLAIN]); self.assertEqual(self.dump("'format nobody reads"), [])
        self.assertEqual(self.dump("'coupon")[0].hits, "3"); self.assertEqual(self.env.cache_json(n1)["v"], self.env.cache_json(f"claude-{GONE}.json")["v"], "rewritten at the current version (not 999)")

    def test_cache_files_are_named_source_dash_transcript_name_and_hold_what_the_design_says(self):
        self.dump(); self.assertEqual(self.env.cache_files(), sorted([f"claude-{CART}.json", f"claude-{AUDIT}.json", f"claude-{GONE}.json", f"claude-{self.c['hush'][1]}.json", f"pi-{PI_STAMP}_{PI_AUDIT}.json", f"pi-{PI_STAMP}_{PI_PLAIN}.json"]))
        e = self.env.cache_json(f"claude-{CART}.json")
        for k in ("v", "source", "id", "path", "cwd", "name", "branch", "mtime", "size", "offset", "users", "replies"): self.assertIn(k, e, f"design §4 field {k}")
        self.assertEqual((e["source"], e["id"], e["path"], e["name"], e["branch"]), ("claude", CART, str(self.c["cart"][0]), "cart-fix", "feat/cart")); self.assertEqual(e["mtime"], when(10) * 10**9)
        self.assertEqual(e["cwd"], str(self.env.home / "work" / "shopcart")); self.assertEqual(len(e["users"]), 3); self.assertEqual(len(e["replies"]), 2)
        self.assertTrue(all({"t", "x"} <= set(l) for l in e["users"] + e["replies"]))

    def test_the_cache_is_written_whole_or_not_at_all_no_temp_files_stay_and_parallel_runs_agree(self):
        outs, errs = [], []

        def go():
            r = self.env.run("recall", "--dump"); outs.append(r.stdout); errs.append(r.stderr)
        ts = [threading.Thread(target=go) for _ in range(5)]; [t.start() for t in ts]; [t.join() for t in ts]
        self.assertEqual(len(set(outs)), 1, "five first builds at once give the same list"); self.assertEqual("".join(errs), "")
        self.assertEqual([n for n in os.listdir(self.env.recall_dir) if not re.fullmatch(r"(claude|pi)-.+\.json", n)], [], "no .tmp-* leftovers")
        for n in self.env.cache_files(): json.loads((self.env.recall_dir / n).read_text())


class Safety(Base):
    def test_nothing_the_agents_own_is_touched_and_the_cache_is_the_only_thing_written(self):
        before = {"claude": self.tree(self.env.claude), "pi": self.tree(self.env.home / ".pi")}; self.dump(); self.dump("'coupon"); self.env.run("recall", "--dump", "--reindex")
        self.assertEqual({"claude": self.tree(self.env.claude), "pi": self.tree(self.env.home / ".pi")}, before, "transcripts: same bytes, same mtime")
        self.assertEqual(sorted(os.listdir(self.env.cache)), ["agmux"]); self.assertEqual(sorted(os.listdir(self.env.cache / "agmux")), ["recall"])
        self.assertEqual(sorted(os.listdir(self.env.state)), [], "no state written"); self.assertEqual(sorted(p for p in os.listdir(self.env.home) if p.startswith(".") and p not in (".claude", ".pi")), [])

    def test_the_cache_goes_under_home_dot_cache_without_xdg_cache_home(self):
        r = self.env.run("recall", "--dump", XDG_CACHE_HOME=None); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertTrue((self.env.home / ".cache" / "agmux" / "recall").is_dir()); self.assertEqual(os.listdir(self.env.cache / "agmux") if (self.env.cache / "agmux").exists() else [], [])

    def test_claude_config_dir_and_pi_coding_agent_dir_move_the_sources(self):
        other = self.env.tmp / "elsewhere"; sid = "15151515-0000-4000-8000-000000000015"; cwd = str(self.env.home / "work" / "moved")
        p = other / "claude" / "projects" / "-x" / f"{sid}.jsonl"; p.parent.mkdir(parents=True); p.write_text("\n".join(claude_lines(sid, cwd, "main", [("u", "a session in another config dir")])) + "\n")
        q = other / "pi" / "sessions" / "--x--" / f"{PI_STAMP}_{PI_PLAIN[:-1]}9.jsonl"; q.parent.mkdir(parents=True); q.write_text("\n".join(pi_lines(PI_PLAIN[:-1] + "9", cwd, [("u", "a pi session in another agent dir")])) + "\n")
        rows, r = self.env.dump(CLAUDE_CONFIG_DIR=str(other / "claude"), PI_CODING_AGENT_DIR=str(other / "pi")); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(sorted(x.snippet for x in rows), ["a pi session in another agent dir", "a session in another config dir"], "only the moved sources; the ones under $HOME are not read")

    def test_no_agents_at_all_is_an_empty_list_not_an_error(self):
        env = RecallEnv(); self.addCleanup(env.cleanup); shutil.rmtree(env.claude); shutil.rmtree(env.home / ".pi")
        r = env.run("recall", "--dump"); self.assertEqual((r.returncode, r.stdout, r.stderr), (0, "", ""))
        env2 = RecallEnv(); self.addCleanup(env2.cleanup); r = env2.run("recall", "--dump"); self.assertEqual((r.returncode, r.stdout, r.stderr), (0, "", ""), "agent directories there but empty")

    def test_a_transcript_that_cannot_be_read_is_one_line_on_stderr_and_the_rest_is_listed(self):
        p, sid = self.c["gone"]; os.chmod(p, 0)
        self.addCleanup(lambda: os.chmod(p, 0o644))
        rows, r = self.env.dump(); self.assertEqual(r.returncode, 1, "56fd337: a transcript that cannot be read makes the exit status 1 (the rest is still listed)"); self.assertTrue(r.stderr.startswith("agmux: recall:") and "permission denied" in r.stderr.lower(), r.stderr)
        self.assertEqual(self.ids(rows), [CART, AUDIT, PI_AUDIT, PI_PLAIN])

    def test_the_cache_directory_that_cannot_be_made_is_an_error_not_a_panic(self):
        (self.env.cache / "agmux").write_text("in the way"); r = self.env.run("recall", "--dump")
        self.assertTrue(r.stderr.startswith("agmux:") and str(self.env.cache) in r.stderr and "panic" not in r.stderr and "goroutine" not in r.stderr, r.stderr)


class Cli(Base):
    def test_a_double_dash_ends_the_options_the_rest_is_the_query(self):
        self.dump(); name = f"claude-{CART}.json"; e = self.env.cache_json(name); e["users"][0]["x"] = "tampered sentence from the cache"; self.env.put_cache(name, e)
        r = self.env.run("recall", "--dump", "--", "--reindex"); self.assertEqual((r.returncode, r.stderr), (0, ""), "`--reindex` after `--` is a word to search for, not an option")
        self.assertEqual(self.env.cache_json(name)["users"][0]["x"], "tampered sentence from the cache", "the cache was not reset")
        self.assertEqual(self.ids(self.dump("'tampered sentence")), [CART], "(and a reset would have been visible: the transcript has the real sentence)")

    def test_a_query_that_looks_like_an_option_is_found_after_the_terminator(self):
        cwd = str(self.env.home / "work" / "dashes"); sid = "16161616-0000-4000-8000-000000000016"
        self.env.claude_file(cwd, sid, claude_lines(sid, cwd, "main", [("u", "what does --force do to the branch")]), age=1)
        r = self.env.run("recall", "--dump", "--", "--force"); self.assertEqual(r.returncode, 0, r.stderr); self.assertIn(sid, [l.split("\t")[1] for l in r.stdout.splitlines()])
        r = self.env.run("recall", "--dump", "--", "--force", "branch"); self.assertEqual([l.split("\t")[1] for l in r.stdout.splitlines()], [sid], "several words after the terminator")

    def test_an_unknown_option_is_usage_on_stderr_and_exit_2(self):
        r = self.env.run("recall", "--dump", "--nope"); self.assertEqual((r.returncode, r.stdout), (2, "")); self.assertIn("unknown option --nope", r.stderr); self.assertIn("usage: agmux recall [--dump] [--reindex] [--] [query...]", r.stderr)
        self.assertEqual(self.env.cache_files(), [], "and nothing was indexed")

    def test_options_in_either_order(self):
        for flags in (("--dump", "--reindex"), ("--reindex", "--dump")):
            rows, r = self.env.dump(flags=flags); self.assertEqual((r.returncode, len(rows)), (0, 5), flags)

    def test_the_usage_in_help_mentions_recall(self):
        out = self.env.run("--help"); self.assertIn("recall --dump", out.stdout + out.stderr)


if __name__ == "__main__":
    unittest.main()
