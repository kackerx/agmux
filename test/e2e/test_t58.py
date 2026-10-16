"""T58 the card name of a session with no summary (a background session started with `claude --bg -n <name>`) is the last `{"type":"custom-title","customTitle":"…"}` line of its transcript (logPath); looked for in the
last 64KB first, else the whole file is scanned once (streamed, the last one wins) and what was scanned is not read again, only what is added; a rename (a new custom-title line) is followed and does not go back to the old name
once it has scrolled out of the last 64KB; a shorter file or another logPath starts over; no title anywhere = the project name; a summary is always used and the transcript is not looked at. `agmux show` has the same name
(2nd column). Pins / avatars are keyed by that name. Spec: specs/m3-config/tasks.md「第十一批」T58. Fake ccmux (summary null) + fake transcripts; the fixture's b1 / c1 / c2 / d1 have no summary here."""
import json, subprocess, unittest
from lib import *
import test_t37 as T37
from test_t3 import ready, C_FOCUS, border_colours

TITLE = lambda name: json.dumps({"type": "custom-title", "customTitle": name}, ensure_ascii=False) + "\n"
FILL = json.dumps({"type": "user", "message": {"content": "x" * 900}}) + "\n"  # about 1KB of nothing
USER = '{"type":"user"}\n'


def filler(kb): return FILL * kb


class Base(T37.Base):
    NAMELESS = ("b1", "c1", "c2", "d1")  # the fixture's bravo-29 / charlie-30 / charlie-59 / delta-60; their project names are bravo / charlie / charlie / delta

    def setUp(self):
        super().setUp(); self.edit(lambda S: [s.update(summary=None) for s in S if s["id"] in self.NAMELESS])

    def edit(self, fn):
        S = json.loads((self.env.fake / "show.json").read_text()); fn(S); self.env.write_show(S)

    def has(self, t, name): return any(name in c.line(1) for c in find_cards(t.snap()) if c.ok)

    def show_names(self): return {l.split("\t")[0]: l.split("\t")[1] for l in subprocess.run([BIN, "show"], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10).stdout.splitlines() if l}

    def settle(self, t, name, secs=7): self.assertTrue(wait(lambda: self.has(t, name), secs), f"want a card named {name}:\n{t.text()}")

    def gone(self, t, name, secs=1.0): time.sleep(secs); self.assertFalse(self.has(t, name), f"no card named {name}:\n{t.text()}")


class Source(Base):
    def test_a_title_in_the_tail_names_the_card(self):
        self.log("b1", USER + TITLE("review-bot") + USER); t = self.board(); self.settle(t, "review-bot"); self.assertFalse(self.has(t, "bravo-29")); self.assertEqual(self.show_names()["b1"], "review-bot")

    def test_a_title_at_the_start_of_a_big_file_is_found_by_the_scan(self):
        self.log("b1", TITLE("head-title") + filler(300)); t = self.board(); self.settle(t, "head-title"); self.assertEqual(self.show_names()["b1"], "head-title", "agmux show finds it the same way")

    def test_the_last_title_wins(self):
        self.log("b1", TITLE("first") + USER + TITLE("second") + filler(200) + TITLE("third") + USER); t = self.board(); self.settle(t, "third"); self.assertFalse(self.has(t, "first")); self.assertFalse(self.has(t, "second"))
        self.log("c1", TITLE("one") + filler(200) + TITLE("two") + filler(100)); self.settle(t, "two")  # both outside the tail: the scan streams to the end

    def test_no_title_anywhere_is_the_project_name(self):
        self.log("b1", filler(200)); self.log("c1", USER); t = self.board(); self.settle(t, "bravo "); self.assertTrue(self.has(t, "charlie"), "(c1 has the project name charlie)"); self.assertEqual(self.show_names()["b1"], "bravo")

    def test_a_session_without_a_readable_transcript_is_the_project_name(self):
        self.edit(lambda S: [s.update(logPath=str(self.env.tmp / "missing.jsonl")) for s in S if s["id"] == "b1"]); t = self.board(); self.settle(t, "bravo "); self.assertEqual(self.show_names()["b1"], "bravo")

    def test_a_summary_wins_and_the_transcript_is_not_looked_at(self):
        self.edit(lambda S: [s.update(summary="given-by-ccmux") for s in S if s["id"] == "b1"]); self.log("b1", TITLE("from-the-file") + USER); t = self.board(); self.settle(t, "given-by-ccmux"); self.assertFalse(self.has(t, "from-the-file"))
        self.assertEqual(self.show_names()["b1"], "given-by-ccmux")

    def test_the_line_has_to_be_a_custom_title_with_a_title(self):
        # other lines that mention the word, an empty title, and a broken line are not titles
        self.log("b1", json.dumps({"type": "user", "message": {"content": '{"type":"custom-title","customTitle":"in-a-prompt"}'}}) + "\n" + json.dumps({"type": "custom-title", "customTitle": ""}) + "\n" + '{"type":"custom-title","customTitle":"cut-off\n' + USER)
        t = self.board(); self.settle(t, "bravo "); self.assertFalse(self.has(t, "in-a-prompt")); self.assertFalse(self.has(t, "cut-off"))

    def test_quotes_wide_characters_and_spaces_in_the_title(self):
        self.log("b1", TITLE('后台 "q" 名 x')); t = self.board(); self.settle(t, '后台 "q" 名 x'); self.assertEqual(self.show_names()["b1"], '后台 "q" 名 x')

    def test_a_title_is_read_from_a_big_transcript_without_stalling_the_board(self):
        self.log("b1", TITLE("big-one") + FILL * 20000); t0 = time.time(); t = self.board(); self.settle(t, "big-one", 6); self.assertLess(time.time() - t0, 8); self.assertTrue(t.alive())  # ~20 MB
        t.keys("j"); time.sleep(0.3); self.assertTrue(t.alive())


class Rename(Base):
    def test_a_rename_is_followed_and_stays_when_it_scrolls_out_of_the_tail(self):
        self.log("b1", TITLE("old-name") + USER); t = self.board(); self.settle(t, "old-name")
        self.log("b1", TITLE("new-name"), append=True); self.settle(t, "new-name"); self.assertFalse(self.has(t, "old-name"))
        self.log("b1", filler(100), append=True); time.sleep(5.0); self.assertTrue(self.has(t, "new-name"), f"100KB later the new name is still there (not back to old-name):\n{t.text()}"); self.assertFalse(self.has(t, "old-name"))
        self.log("b1", TITLE("third-name"), append=True); self.settle(t, "third-name"); self.assertFalse(self.has(t, "new-name"))

    def test_a_rename_that_never_was_in_the_tail_of_the_first_look(self):
        self.log("b1", TITLE("old-name") + filler(200)); t = self.board(); self.settle(t, "old-name")  # found by the scan
        self.log("b1", filler(100) + TITLE("newer") + filler(10), append=True); self.settle(t, "newer"); self.assertFalse(self.has(t, "old-name"))
        self.log("b1", filler(100), append=True); time.sleep(5.0); self.assertTrue(self.has(t, "newer"), f"the first title came from a scan and is remembered; the rename was in the tail and has now scrolled out: still the new one\n{t.text()}"); self.assertFalse(self.has(t, "old-name"))

    def test_a_half_written_line_is_not_a_title_until_it_is_finished(self):
        self.log("b1", TITLE("complete") + USER); t = self.board(); self.settle(t, "complete")
        self.log("b1", '{"type":"custom-title","customTitle":"par', append=True); time.sleep(5.0); self.assertTrue(self.has(t, "complete"), "the half line changes nothing")
        self.log("b1", 'tial"}\n', append=True); self.settle(t, "partial")

    def test_a_title_that_comes_after_the_card_was_first_seen(self):
        self.log("b1", USER * 3); t = self.board(); self.settle(t, "bravo "); self.log("b1", TITLE("born-later"), append=True); self.settle(t, "born-later"); self.assertFalse(self.has(t, "bravo-29"))

    def test_a_shorter_file_starts_over(self):
        self.log("b1", TITLE("long-file-title") + filler(150)); t = self.board(); self.settle(t, "long-file-title")
        self.log("b1", TITLE("short-file-title") + USER); self.settle(t, "short-file-title"); self.assertFalse(self.has(t, "long-file-title"))
        self.log("b1", USER); self.settle(t, "bravo ", 7)  # the new, shorter file has no title: the project name

    def test_another_logpath_starts_over(self):
        a = self.log("b1", TITLE("file-a") + filler(50)); t = self.board(); self.settle(t, "file-a")
        p = self.env.tmp / "other.jsonl"; p.write_text(TITLE("file-b") + filler(300))  # longer than a: only the path tells it is another one
        self.edit(lambda S: [s.update(logPath=str(p)) for s in S if s["id"] == "b1"]); self.settle(t, "file-b"); self.assertFalse(self.has(t, "file-a"))
        q = self.env.tmp / "third.jsonl"; q.write_text(USER * 10); self.edit(lambda S: [s.update(logPath=str(q)) for s in S if s["id"] == "b1"]); self.settle(t, "bravo ")

    def test_two_sessions_have_their_own_titles(self):
        self.log("b1", TITLE("name-one") + filler(100)); self.log("c1", TITLE("name-two") + filler(100)); t = self.board(); self.settle(t, "name-one"); self.settle(t, "name-two")
        self.log("c1", TITLE("name-two-renamed"), append=True); self.settle(t, "name-two-renamed"); self.assertTrue(self.has(t, "name-one"))


class Keyed(Base):
    def test_pin_and_the_avatar_binding_are_kept_by_the_title(self):
        self.log("b1", TITLE("keyed-name") + USER); t = self.board(); self.settle(t, "keyed-name")
        t.keys("l"); time.sleep(0.3); t.keys("j"); time.sleep(0.5); self.assertTrue(any(C_FOCUS in border_colours(c) and "keyed-name" in c.line(1) for c in find_cards(t.snap())), t.text())
        t.keys("t"); time.sleep(1.0); pins = json.loads((self.env.state / "agmux" / "pins.json").read_text()); self.assertEqual(list(pins), ["keyed-name"], "pins.json is keyed by the name the card shows")


if __name__ == "__main__":
    unittest.main()
