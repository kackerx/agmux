"""T53 pin a card to the top of its group: action `pin`, default key `t`, toggles the focused card. A pinned card sorts first in its group, pinned ones in the order they were pinned (first pinned first), the others
keep the name order; unpinning drops it back. Kept by the card name (AvatarKey: it survives a restart that changes the session id) in $XDG_STATE_HOME/agmux/pins.json ({name: RFC3339 time}), under every grouping,
dropped when no session has the name any more; a corrupt file counts as empty and the board says so in one footer line. `▲ ` before the name (focus colour). Search, flash, fold are not affected; `agmux show` is not.
Spec: specs/m3-config/tasks.md T53 (dev notes: keyed by name, not by id). Fixture and oracle: test_t43 (8 sessions, 80x70, one column)."""
import json, re, subprocess, unittest
from lib import *
from test_t43 import Base as _B43, oracle
from test_t3 import C_FOCUS, border_colours

MATCH = ("rgb", 242, 208, 107)
NEXT = {"cwd": "project", "project": "session", "session": "state", "state": "cwd"}


def outline(snap):
    """the screen top to bottom: '▾ title (n)' and the card names, a pinned card as '▲name'"""
    cards = {c.r + 1: c for c in find_cards(snap)}; out = []
    for r, row in enumerate(snap):
        t = row.text.strip()
        if t.startswith(("▾", "▸")): out.append(re.sub(r"\s*━+$", "", t).rstrip())
        elif r in cards:
            m = re.match(r"\s*(▲ )?(\S+)", cards[r].line(1)[8:]); out.append(("▲" if m.group(1) else "") + m.group(2))
    return out


def with_pins(groups, pins):
    """oracle() groups with the pinned names first in pin order, marked ▲"""
    out = []
    for title, names in groups:
        top = [n for n in pins if n in names]; out.append((title, ["▲" + n for n in top] + [n for n in names if n not in top]))
    return out


def flat(groups, folded=()):
    out = []
    for title, names in groups:
        out.append(f"{'▸' if title in folded else '▾'} {title} ({len(names)})")
        if title not in folded: out += names
    return out


class PinBase(_B43):
    def setUp(self):
        super().setUp(); self.pins_json = self.env.state / "agmux" / "pins.json"

    def pins(self): return json.loads(self.pins_json.read_text()) if self.pins_json.exists() else {}

    def expect_pins(self, t, mode, pins, folded=(), timeout=8):
        want = flat(with_pins(oracle(self.sessions(), mode, self.home), pins), folded)
        def same():
            try: return outline(t.snap()) == want
            except (IndexError, AttributeError): return False  # a capture torn by a repaint
        self.assertTrue(wait(same, timeout), f"mode {mode}, pins {pins}:\nwant {want}\ngot  {outline(t.snap())}\n{t.text()}")

    def at(self, t, name):
        line = self.focused(t); return bool(line) and re.search(rf"(?:\s|▲ ){re.escape(name)}\s", line[0]) is not None

    def goto(self, t, name):
        """move the focus to the card by hand (j down, then k up); the key presses are the test's own"""
        for key in ("j", "k"):
            for _ in range(10):
                if wait(lambda: self.at(t, name), 0.3): return
                t.keys(key); time.sleep(0.15)
        self.fail(f"could not focus {name}: {self.focused(t)}\n{t.text()}")

    def pin(self, t, name):
        self.goto(t, name); t.keys("t"); time.sleep(0.5)

    def msg(self, t):
        """the message line: the row above the footer"""
        s = t.snap(); i = max(i for i, r in enumerate(s) if r.text.strip()); return s[i - 1].text.strip()


class Pin(PinBase):
    def test_t_pins_the_focused_card_first_in_its_group_with_a_triangle_in_the_focus_colour(self):
        t = self.board(""); self.expect_pins(t, "cwd", []); self.goto(t, "sql-c"); t.keys("t")
        self.expect_pins(t, "cwd", ["sql-c"]); self.assertEqual(outline(t.snap())[-4:], ["▾ ~/work/sqlmux (3)", "▲sql-c", "sql-a", "sql-b"][-4:])
        self.assertTrue(wait(lambda: self.msg(t) == "pinned sql-c", 3), self.msg(t)); self.assertTrue(self.at(t, "sql-c"), "the focus stays on the card that moved")
        c = next(c for c in find_cards(t.snap()) if C_FOCUS in border_colours(c)); cells = t.snap()[c.r + 1].cells; i = next(k for k, x in enumerate(cells) if x[0] == "▲")
        self.assertEqual(cells[i][1], C_FOCUS, "▲ in the focus colour (the card is focused)"); self.assertEqual("".join(x[0] for x in cells[i:i + 8]), "▲ sql-c ")
        self.assertEqual(list(self.pins()), ["sql-c"], "pins.json is keyed by the card name"); self.assertRegex(self.pins()["sql-c"], r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z$")

    def test_an_unfocused_pinned_card_has_the_triangle_in_the_focus_colour_too(self):
        t = self.board(""); self.pin(t, "sql-c"); [(t.keys("g"), time.sleep(0.3)) for _ in range(4)]; self.goto(t, "shop-main")  # back in cwd, the focus is elsewhere
        c = next(c for c in find_cards(t.snap()) if "▲" in c.line(1)); self.assertNotIn(C_FOCUS, border_colours(c), "this card is not the focused one")
        cells = t.snap()[c.r + 1].cells; i = next(k for k, x in enumerate(cells) if x[0] == "▲"); self.assertEqual(cells[i][1], C_FOCUS)

    def test_t_again_unpins_and_the_card_falls_back_to_the_name_order(self):
        t = self.board(""); self.pin(t, "sql-c"); self.expect_pins(t, "cwd", ["sql-c"]); t.keys("t")
        self.expect_pins(t, "cwd", []); self.assertTrue(wait(lambda: self.msg(t) == "unpinned sql-c", 3), self.msg(t)); self.assertEqual(self.pins(), {}, "the entry is gone")
        self.assertNotIn("▲", t.text())

    def test_pinned_cards_follow_the_time_they_were_pinned_not_their_names(self):
        t = self.board(""); self.pin(t, "sql-b"); self.pin(t, "sql-a"); self.expect_pins(t, "cwd", ["sql-b", "sql-a"])
        self.pin(t, "sql-c"); self.expect_pins(t, "cwd", ["sql-b", "sql-a", "sql-c"])
        t.keys("t"); self.expect_pins(t, "cwd", ["sql-b", "sql-a"])  # the focus is on sql-c: unpin it
        self.goto(t, "sql-b"); t.keys("t"); self.expect_pins(t, "cwd", ["sql-a"]); self.assertEqual(sorted(self.pins()), ["sql-a"])

    def test_it_holds_under_every_grouping_and_a_card_can_be_pinned_in_any_of_them(self):
        t = self.board(""); pins = ["sql-c", "shop-hf2", "sql-a"]
        for n in pins: self.pin(t, n)
        self.expect_pins(t, "cwd", pins)
        for m in ("project", "session", "state"):
            t.keys("g"); self.expect_pins(t, m, pins)
        self.assertEqual(NEXT["state"], "cwd"); self.pin(t, "shop-main"); pins.append("shop-main"); self.expect_pins(t, "state", pins)  # pinned while in the state grouping
        for m in ("cwd", "project", "session"):
            t.keys("g"); self.expect_pins(t, m, pins)
        self.assertEqual(sorted(self.pins()), sorted(pins))

    def test_two_pins_in_one_group_of_another_grouping_keep_the_order_they_were_pinned_in(self):
        t = self.board('[layout]\ngroup_by = "project"\n'); self.pin(t, "shop-main"); self.pin(t, "shop-hf"); self.expect_pins(t, "project", ["shop-main", "shop-hf"])
        t.keys("g"); t.keys("g"); t.keys("g"); self.expect_pins(t, "cwd", ["shop-main", "shop-hf"])

    def test_a_pin_survives_a_restart_of_the_board_in_the_same_order(self):
        t = self.board(""); self.pin(t, "sql-b"); self.pin(t, "sql-a"); self.pin(t, "shop-hf2"); before = outline(t.snap()); t.close(); got = self.pins(); self.assertEqual(sorted(got), ["shop-hf2", "sql-a", "sql-b"])
        self.assertEqual(sorted(got, key=got.get), ["sql-b", "sql-a", "shop-hf2"], "the times in the file are in pin order")
        t2 = self.board(""); self.expect_pins(t2, "cwd", ["sql-b", "sql-a", "shop-hf2"]); self.assertEqual(outline(t2.snap()), before)

    def test_a_restart_that_gives_the_session_a_new_id_keeps_the_pin(self):
        t = self.board(""); self.pin(t, "sql-b"); self.expect_pins(t, "cwd", ["sql-b"])
        def f(S):
            for s in S:
                if s["summary"] == "sql-b": s["id"], s["nativeSessionId"] = "g99-restarted", "n-g99"
        self.edit(f); time.sleep(2.5); self.expect_pins(t, "cwd", ["sql-b"]); self.assertEqual(list(self.pins()), ["sql-b"], "still the one entry, keyed by the name")
        t.close(); t2 = self.board(""); self.expect_pins(t2, "cwd", ["sql-b"])

    def test_a_session_that_is_gone_for_a_few_seconds_keeps_its_pin_and_an_empty_list_once_too(self):
        t = self.board(""); self.pin(t, "sql-b"); self.pin(t, "sql-a"); self.expect_pins(t, "cwd", ["sql-b", "sql-a"]); S = self.sessions()
        self.env.write_show([]); time.sleep(3.5); self.assertEqual(sorted(self.pins()), ["sql-a", "sql-b"], "ccmux said nothing for a few polls (a restart gap): the pins stay")
        self.env.write_show(S); self.expect_pins(t, "cwd", ["sql-b", "sql-a"], timeout=8)
        self.edit(lambda S: S.remove(next(s for s in S if s["summary"] == "sql-b"))); time.sleep(4); self.assertEqual(sorted(self.pins()), ["sql-a", "sql-b"], "one session gone for 4 s")
        self.env.write_show(S); self.expect_pins(t, "cwd", ["sql-b", "sql-a"], timeout=8); self.assertEqual(sorted(self.pins(), key=self.pins().get), ["sql-b", "sql-a"], "same pin times")

    def test_the_entry_goes_when_no_session_has_had_the_name_for_over_60_seconds_and_a_new_one_is_not_pinned(self):
        t = self.board(""); self.pin(t, "sql-b"); self.pin(t, "sql-a"); gone = next(s for s in self.sessions() if s["summary"] == "sql-b"); t0 = time.time()
        self.edit(lambda S: S.remove(next(s for s in S if s["summary"] == "sql-b"))); time.sleep(30)
        self.assertEqual(sorted(self.pins()), ["sql-a", "sql-b"], "30 s in: still kept")
        self.assertTrue(wait(lambda: sorted(self.pins()) == ["sql-a"], 50), f"sql-b's entry is dropped after the grace ({time.time() - t0:.0f} s in): {self.pins()}"); took = time.time() - t0
        self.assertGreater(took, 55, "not before the 60 s are over (counted from the first poll without it)"); self.assertLess(took, 85); self.expect_pins(t, "cwd", ["sql-a"], timeout=5)
        self.edit(lambda S: S.append(gone)); time.sleep(2.5); self.expect_pins(t, "cwd", ["sql-a"]); self.assertNotIn("▲sql-b", outline(t.snap())); self.assertEqual(sorted(self.pins()), ["sql-a"])

    def test_a_double_t_pins_and_unpins_and_ends_unpinned_even_when_pressed_at_once(self):
        t = self.board(""); self.goto(t, "sql-c"); t.keys("t", "t"); self.expect_pins(t, "cwd", [], timeout=8); time.sleep(1.5); self.expect_pins(t, "cwd", []); self.assertEqual(self.pins(), {}, "pin + unpin: nothing left")
        t.keys("t", "t", "t"); self.expect_pins(t, "cwd", ["sql-c"], timeout=8); time.sleep(1.5); self.expect_pins(t, "cwd", ["sql-c"]); self.assertEqual(list(self.pins()), ["sql-c"], "three presses: pinned")

    def test_two_sessions_with_the_same_name_are_pinned_together(self):
        self.edit(lambda S: S.append({**next(s for s in S if s["summary"] == "sql-a"), "id": "g77", "nativeSessionId": "n-g77", "tmuxPane": "%499", "tmuxTarget": "alpha # x:9.0"}))
        t = self.board(""); self.pin(t, "sql-a"); self.assertEqual(sum(x == "▲sql-a" for x in outline(t.snap())), 2, outline(t.snap()))

    def test_a_pin_made_in_one_board_shows_up_in_another(self):
        a = self.board(""); b = self.board(""); self.pin(a, "sql-c"); self.assertTrue(wait(lambda: "▲sql-c" in outline(b.snap()), 8), f"the other board never saw it:\n{b.text()}")
        self.goto(b, "sql-c"); b.keys("t"); self.assertTrue(wait(lambda: "▲sql-c" not in outline(a.snap()), 8), "and the unpin comes back")

    def test_nothing_moves_while_the_board_polls(self):
        t = self.board(""); self.pin(t, "sql-b"); self.pin(t, "sql-c"); self.expect_pins(t, "cwd", ["sql-b", "sql-c"])
        for _ in range(6):
            time.sleep(0.6); self.assertEqual(outline(t.snap()), flat(with_pins(oracle(self.sessions(), "cwd", self.home), ["sql-b", "sql-c"])))

    def test_t_on_a_group_title_does_nothing(self):
        t = self.board(""); t.keys("f"); time.sleep(0.5); self.assertEqual(self.focused(t), [], "the focus is on the folded title"); t.keys("t"); time.sleep(0.6)
        self.assertEqual(self.pins(), {}); self.assertNotIn("pinned", self.msg(t)); self.assertNotIn("▲", t.text()); self.assertEqual(sum(l.startswith("▸") for l in outline(t.snap())), 1, "the board is as it was")


class Others(PinBase):
    def test_search_filters_pinned_cards_too_and_t_in_the_box_is_text(self):
        t = self.board(""); self.pin(t, "sql-c"); t.keys("/"); time.sleep(0.3); t.lit("sql"); time.sleep(0.7)
        self.assertEqual(outline(t.snap()), ["▾ ~/work/sqlmux (3)", "▲sql-c", "sql-a", "sql-b"], t.text())
        t.lit("t"); time.sleep(0.5); self.assertIn("/ sqlt", t.text()); self.assertEqual(sorted(self.pins()), ["sql-c"], "t in the search box typed a letter")

    def test_t_in_the_search_box_does_not_pin(self):
        t = self.board(""); t.keys("/"); time.sleep(0.3); t.lit("t"); time.sleep(0.5); self.assertIn("/ t", t.text()); self.assertEqual(self.pins(), {})

    def test_flash_labels_the_name_where_it_is_drawn_after_the_triangle(self):
        t = self.board(""); self.pin(t, "sql-c"); t.keys("s"); time.sleep(0.3); t.lit("sql-c"); time.sleep(0.7)
        row = next(r for r in t.snap() if "▲" in r.text); text = "".join(c[0] for c in row.cells); lit = "".join(c[0] for c in row.cells if c[2] == MATCH and c[0].strip())
        self.assertEqual(lit, "sql-c", f"the hit covers the name, not the triangle: {text!r}"); i = text.index("▲"); hit_at = [k for k, c in enumerate(row.cells) if c[2] == MATCH]
        self.assertEqual(hit_at[0], i + 2, "starting right after '▲ '")

    def test_folding_the_group_and_opening_it_again_keeps_the_order(self):
        t = self.board(""); self.pin(t, "sql-c"); t.keys("f"); time.sleep(0.5); self.assertIn("▸ ~/work/sqlmux (3)", outline(t.snap())); self.assertNotIn("▲sql-c", outline(t.snap()))
        t.keys("f"); time.sleep(0.5); self.expect_pins(t, "cwd", ["sql-c"])

    def test_a_pin_does_not_change_agmux_show(self):
        t = self.board(""); self.pin(t, "sql-c"); r = subprocess.run([BIN, "show"], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL); self.assertEqual(r.returncode, 0, r.stderr); self.assertNotIn("▲", r.stdout)


class Corrupt(PinBase):
    def test_a_corrupt_pins_json_counts_as_empty_and_says_so_in_one_line(self):
        self.pins_json.parent.mkdir(parents=True, exist_ok=True); self.pins_json.write_text("{not json")
        t = self.board(""); self.expect_pins(t, "cwd", []); self.assertTrue(wait(lambda: self.msg(t) != "", 3), "a message line"); m = self.msg(t); self.assertFalse(m.startswith(("pinned", "unpinned")), m)
        rows = [r.text for r in t.snap()]; self.assertEqual(sum(1 for r in rows if "agmu" in r and "state" in r), 1, "one line"); self.assertEqual(len(find_cards(t.snap())), 8)
        self.goto(t, "sql-c"); t.keys("t"); time.sleep(1.2); self.assertIn("agmux ·", t.text(), "the board is alive"); self.assertEqual(self.pins_json.read_text(), "{not json", "a pin on a file that does not parse leaves the user's file alone")

    def test_a_valid_file_after_a_corrupt_one_is_read_again(self):
        self.pins_json.parent.mkdir(parents=True, exist_ok=True); self.pins_json.write_text("[1,")
        t = self.board(""); self.expect_pins(t, "cwd", []); self.pins_json.write_text(json.dumps({"sql-c": "2026-10-14T00:00:00Z"})); self.expect_pins(t, "cwd", ["sql-c"], timeout=8)

    def test_a_hand_written_file_with_plain_times_pins_in_time_order(self):
        self.pins_json.parent.mkdir(parents=True, exist_ok=True); self.pins_json.write_text(json.dumps({"sql-a": "2026-10-14T09:00:00Z", "sql-b": "2026-10-14T08:00:00Z", "nobody": "2026-10-14T07:00:00Z"}))
        t = self.board(""); self.expect_pins(t, "cwd", ["sql-b", "sql-a"]); self.assertNotIn("▲nobody", outline(t.snap()))


class Wide(unittest.TestCase):
    """the default fixture: a3's name is long Chinese (cut with …); pinned it moves first in its group and the ▲ takes two cells"""
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.fx = build_fixture(self.env)

    def test_a_pinned_long_wide_name_still_fits_the_card(self):
        t = Term(self.env, w=60, h=60); self.addCleanup(t.close); self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 5, 10), t.text()); time.sleep(0.4)
        for _ in range(8):
            if "告警会话" in next((c.line(1) for c in find_cards(t.snap()) if C_FOCUS in border_colours(c)), ""): break
            t.keys("j"); time.sleep(0.2)
        t.keys("t"); time.sleep(0.8); snap = t.snap(); cards = find_cards(snap); c = next(c for c in cards if "▲" in c.line(1)); line = c.line(1)
        self.assertTrue(c.ok, c.why); self.assertIn("▲ 告警会话", line); self.assertIn("…", line, "still cut"); self.assertTrue(line.rstrip().endswith(("1m┃", "1m│")), line)
        self.assertTrue(snap[c.r - 1].text.lstrip().startswith("▾ ~/work/alpha"), "right under the group title: first of its group\n" + t.text())


class Config(PinBase):
    def test_config_lists_pin_with_the_default_key(self):
        out = self.cli("config", text="").stdout; self.assertIn('pin = ["t"]                   # default: t — pin the focused card to the top of its group', out)

    def test_another_key_pins_and_t_no_longer_does(self):
        t = self.board('[keys]\npin = ["T"]\n'); self.goto(t, "sql-c"); t.keys("t"); time.sleep(0.6); self.assertEqual(self.pins(), {}); t.keys("T"); self.expect_pins(t, "cwd", ["sql-c"])

    def test_a_key_that_belongs_to_another_action_is_taken_over_like_every_other_key(self):
        out = self.cli("config", text='[keys]\nfold = ["t"]\n').stdout; self.assertIn("# pin: no key (another action took its default)", out)
        out = self.cli("config", text='[keys]\npin = ["f"]\n').stdout; self.assertIn("# fold: no key (another action took its default)", out)

    def test_pin_cannot_be_empty(self):
        r = self.cli("config", text="[keys]\npin = []\n"); self.assertNotEqual(r.returncode, 0); self.assertTrue(r.stderr.strip().endswith("keys.pin: needs at least one key"), r.stderr)

    def test_without_a_pin_the_card_list_has_no_triangle(self):
        t = self.board(""); self.assertNotIn("▲", t.text())


if __name__ == "__main__":
    unittest.main()
