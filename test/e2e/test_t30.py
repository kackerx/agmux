"""T30 image-avatar states aligned with pi-emote: idle / think / read / write / tool / hi / success, picked per session from status + pendingTool (store), with a fallback chain,
old directory names as aliases and an idle blink. Spec: specs/m3-config/design.md「头像图片：状态、宽度、选择框」1–3, tasks.md T30.
Uses lib.build_states_fixture (17 sessions, one per case) and kitty mode (fake tmux says xterm-kitty); image ids are looked up from what agmux uploads (lib.upload_ids)."""
import re, subprocess, unittest
from lib import *
import test_t28 as t28
from test_t28 import card_avatar, SETS, BUILTIN, BUILTIN_LINE

NAME = re.compile(r"s-[a-z0-9]+")


class Base(t28.Base):
    """The 17-session fixture instead of the default nine; every card wears the user set named in default_set (config default)."""

    def setUp(self):
        super().setUp(); self.expect = build_states_fixture(self.env)

    def states_board(self, default="mine", w=200, h=40, **kw):
        self.config(f'[avatars]\ndefault = "{default}"\n'); t = self.board(w=w, h=h, loose=True, **kw)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) == len(self.expect), 8), t.text()); return t

    def ids_by_card(self, t):
        raw = t.raw(); out = {}
        for c in find_cards(parse_screen(raw)):
            m = NAME.search(c.line(1))
            if m:
                cells = card_avatar(raw, c); out[m.group(0)] = cells[0][2] if len(cells) == 12 and len({x[2] for x in cells}) == 1 else None
        return out

    def set_ids(self, d):
        """{state dir: [image id per frame in file-name order]} of the user set directory d, from the upload."""
        ids = upload_ids(self.env, self.cfg)
        return {p.name: [ids.get(f.read_bytes()) for f in sorted(p.glob("*.png"))] for p in sorted(d.iterdir()) if p.is_dir()}  # None = not uploaded (an alias dir the new name shadows)

    def check_states(self, frames, default="mine", secs=2.6):
        """Every card shows, over `secs` seconds, only frames of the state it must show (through the fallback chain), and a multi-frame state steps through them in order, one per tick."""
        t = self.states_board(default); seen = sample_series(lambda: self.ids_by_card(t), secs, 0.05)
        for name, state in self.expect.items():
            want, shown = frames_for(frames, state)
            seq = [s[name] for _, s in seen]; self.assertNotIn(None, seq, f"{name}: a card lost its picture")
            if shown == "idle":
                self.assertTrue(set(seq) <= set(want), f"{name} ({state}): {set(seq)} not within idle {want}"); continue
            self.assertTrue(set(seq) <= set(want), f"{name} ({state} -> {shown}): {[hex(i) for i in set(seq)]} not within {[hex(i) for i in want]}")
            order = [seq[i + 1] for i in range(len(seq) - 1) if seq[i] != seq[i + 1]]
            for a, b in zip([seq[0]] + order, order): self.assertEqual(b, want[(want.index(a) + 1) % len(want)], f"{name}: frames advance in file-name order: {order}")
            if len(want) > 1: self.assertGreaterEqual(len(set(seq)), 2, f"{name} ({state}): the frames should cycle, saw {set(seq)}")


class Mapping(Base):
    def test_each_state_shows_its_own_frames(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 2, "read": 2, "write": 2, "tool": 2, "hi": 2, "success": 2}); self.check_states(self.set_ids(d))

    def test_priority_hi_beats_a_pending_tool(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 2, "tool": 2, "hi": 2}); frames = self.set_ids(d)
        t = self.states_board(); seen = {self.ids_by_card(t)["s-attention"] for _ in range(1)}
        self.assertTrue(seen <= set(frames["hi"]), f"s-attention (permission prompt while Bash is pending) must show hi, not tool: {seen}")
        self.assertTrue(self.ids_by_card(t)["s-waiting"] in frames["hi"], "waiting shows hi")

    def test_a_working_session_with_only_subagents_thinks(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 2, "tool": 2}); frames = self.set_ids(d); t = self.states_board()
        self.assertIn(self.ids_by_card(t)["s-agents"], frames["think"]); self.assertIn(self.ids_by_card(t)["s-think"], frames["think"])

    def test_every_reading_and_writing_tool_goes_to_its_state(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 1, "read": 1, "write": 1, "tool": 1}); f = self.set_ids(d); t = self.states_board(); got = self.ids_by_card(t)
        for n in ("s-read", "s-grep", "s-glob", "s-webfetch", "s-websearch"): self.assertEqual(got[n], f["read"][0], f"{n}: Read / Grep / Glob / WebFetch / WebSearch -> read")
        for n in ("s-write", "s-edit", "s-multiedit", "s-notebook"): self.assertEqual(got[n], f["write"][0], f"{n}: Write / Edit / MultiEdit / NotebookEdit -> write")
        for n in ("s-bash", "s-mcp"): self.assertEqual(got[n], f["tool"][0], f"{n}: any other pending tool -> tool")
        self.assertEqual(got["s-think"], f["think"][0])


class Fallback(Base):
    def test_a_set_with_only_idle_and_think_read_write_tool_fall_back_to_think_hi_and_success_to_idle(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 2, "read": 0, "write": 0, "tool": 0, "hi": 0, "success": 0}); self.check_states(self.set_ids(d))

    def test_a_set_with_only_idle_shows_idle_everywhere(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 0, "read": 0, "write": 0, "tool": 0, "hi": 0, "success": 0}); f = self.set_ids(d); t = self.states_board()
        self.assertEqual(set(self.ids_by_card(t).values()), {f["idle"][0]})

    def test_read_falls_to_think_not_to_tool(self):  # chain: read / write / tool -> think -> idle; a sibling state is never borrowed
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 0, "read": 0, "write": 2, "tool": 2, "hi": 0, "success": 0}); f = self.set_ids(d); t = self.states_board(); got = self.ids_by_card(t)
        self.assertEqual(got["s-read"], f["idle"][0], "no think either: all the way to idle"); self.assertIn(got["s-write"], f["write"]); self.assertIn(got["s-bash"], f["tool"])
        self.assertEqual(got["s-think"], f["idle"][0]); self.assertEqual(got["s-done"], f["idle"][0]); self.assertEqual(got["s-attention"], f["idle"][0])

    def test_success_and_hi_do_not_borrow_think(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 2, "read": 0, "write": 0, "tool": 0, "hi": 0, "success": 0}); f = self.set_ids(d); t = self.states_board(); got = self.ids_by_card(t)
        self.assertEqual((got["s-done"], got["s-waiting"], got["s-attention"]), (f["idle"][0],) * 3)


class Aliases(Base):
    def old_set(self, **n):
        """a set in the old directory spelling: working / attention / done (+ idle)."""
        return write_avatar_set(self.cfg, "mine", {"idle": 1, **n})

    def test_working_attention_done_are_the_old_names_of_think_hi_success(self):
        d = self.old_set(working=2, attention=2, done=2); f = self.set_ids(d); t = self.states_board(); got = self.ids_by_card(t)
        for n in ("s-think", "s-agents", "s-read", "s-write", "s-bash"): self.assertIn(got[n], f["working"], f"{n}: working = think (and read / write / tool fall back to it)")
        for n in ("s-attention", "s-waiting"): self.assertIn(got[n], f["attention"], f"{n}: attention = hi")
        self.assertIn(got["s-done"], f["done"], "done = success")

    def test_the_new_name_wins_when_both_exist(self):
        d = write_avatar_set(self.cfg, "mine", {"idle": 1, "think": 1, "working": 2}); f = self.set_ids(d); t = self.states_board()
        self.assertEqual(f["working"], [None, None], "the shadowed old directory is not even uploaded")
        self.assertEqual(self.ids_by_card(t)["s-think"], f["think"][0]); self.assertEqual(self.ids_by_card(t)["s-bash"], f["think"][0])

    def test_agmux_avatars_counts_old_names_under_the_new_ones(self):
        write_avatar_set(self.cfg, "mine", {"idle": 1, "working": 3, "attention": 2, "done": 1}); write_avatar_set(self.cfg, "both", {"idle": 1, "think": 1, "working": 4})
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        out = subprocess.run([BIN, "avatars"], env=env, capture_output=True, text=True, timeout=10).stdout.split("\n\n")[0].split("\n")
        self.assertEqual(out, set_lines(self.cfg)); self.assertEqual(out[-2:], ["both  user  idle 1 · think 1 · read 0 · write 0 · tool 0 · hi 0 · success 0", "mine  user  idle 1 · think 3 · read 0 · write 0 · tool 0 · hi 2 · success 1"],
                                                                      "the old names count under the new ones; the new name wins when both exist (both: think 1, not 5)")


class Builtin(Base):
    def test_the_built_in_set_uses_pi_emotes_own_directories(self):
        self.assertEqual(sorted(BUILTIN), ["hi", "idle", "read", "think", "tool", "write"], "aza_choi's source tree has pi-emote's directories (no success: it falls back to idle)")
        self.assertEqual(BUILTIN["idle"], ["idle.png", "idle_blink.png"])

    def test_agmux_avatars_prints_the_seven_states(self):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        self.assertEqual(subprocess.run([BIN, "avatars"], env=env, capture_output=True, text=True, timeout=10).stdout.split("\n")[0], BUILTIN_LINE)

    def test_cards_show_the_built_in_frames_of_their_state(self):
        ids = upload_ids(self.env, self.cfg); f = {st: [ids[(SETS / st / n).read_bytes()] for n in names] for st, names in BUILTIN.items()}
        self.check_states(f, default="aza_choi")


class Blink(Base):
    """idle: the first frame is the calm face; the other frames show for one tick (500 ms) in every 8, each card starting at a different offset (hash of its key)."""

    def blinks(self, secs):
        self.expect = build_states_fixture(self.env, {f"s-idle{c}": ("idle", "idle", None, None, None, []) for c in "abcdefgh"})  # eight idle cards: eight different keys
        ids = upload_ids(self.env, self.cfg); calm = ids[(SETS / "idle" / "idle.png").read_bytes()]; blink = ids[(SETS / "idle" / "idle_blink.png").read_bytes()]
        t = self.states_board(default="aza_choi"); seen = sample_series(lambda: self.ids_by_card(t), secs, 0.04)
        return calm, blink, seen

    _memo = None

    def blink_data(self):
        """One 33.5 s sampling shared by the two timing tests (they only read it)."""
        if Blink._memo is None:
            calm, blink, seen = self.blinks(33.5); ups = {}
            for n in self.expect:
                seq = [(t, s[n]) for t, s in seen]; self.assertTrue({i for _, i in seq} <= {calm, blink}, f"{n}: only the calm face and the blink: {({hex(i) for _, i in seq})}")
                ups[n] = ([seq[i][0] for i in range(1, len(seq)) if seq[i][1] == blink and seq[i - 1][1] == calm],
                          [seq[i][0] for i in range(1, len(seq)) if seq[i][1] == calm and seq[i - 1][1] == blink])
            Blink._memo = (ups, seen[-1][0])
        return Blink._memo

    def test_every_idle_card_blinks_8_times_in_64_ticks_8_ticks_apart(self):
        ups, total = self.blink_data()
        for n, (u, d) in ups.items():
            self.assertTrue(7 <= len(u) <= 9, f"{n}: {len(u)} blinks in {total:.1f}s (8 per 64 ticks = 32 s): {[round(x, 1) for x in u]}")
            for a, b in zip(u, u[1:]): self.assertTrue(3.5 <= b - a <= 4.5, f"{n}: blinks 8 ticks (4 s) apart: {[round(x, 1) for x in u]}")
            for x in u:
                y = next((y for y in d if y > x), None)
                if y is not None: self.assertTrue(0.3 <= y - x <= 0.75, f"{n}: a blink lasts one tick (500 ms): {y - x:.2f}s")

    def test_cards_do_not_all_blink_in_the_same_tick(self):
        ups, _ = self.blink_data(); phase = {n: u[0] % 4.0 for n, (u, d) in ups.items()}
        self.assertGreater(max(phase.values()) - min(phase.values()), 0.4, f"offsets by a hash of the card's key: first-blink phases (s mod 4) {phase}")

    def test_a_multi_frame_idle_blinks_through_its_frames_in_turn(self):
        d = state_set(self.cfg, "mine", {"idle": 3, "think": 0, "read": 0, "write": 0, "tool": 0, "hi": 0, "success": 0}); f = self.set_ids(d)["idle"]
        t = self.states_board(); seen = sample_series(lambda: self.ids_by_card(t), 17.0, 0.05)
        for n in [n for n, s in self.expect.items() if s == "idle"]:
            seq = [s[n] for _, s in seen]; self.assertEqual(seq[0], f[0], n); blinks = [seq[i] for i in range(1, len(seq)) if seq[i] != f[0] and seq[i - 1] == f[0]]
            self.assertGreaterEqual(len(blinks), 3, f"{n}: {blinks}"); self.assertTrue(set(blinks) <= set(f[1:]))
            for a, b in zip(blinks, blinks[1:]): self.assertEqual(b, f[1:][(f[1:].index(a) + 1) % 2], f"{n}: blink frames take turns in file-name order: {[hex(x) for x in blinks]}")

    def test_a_single_idle_frame_never_changes(self):
        d = state_set(self.cfg, "mine", {"idle": 1, "think": 0, "read": 0, "write": 0, "tool": 0, "hi": 0, "success": 0}); f = self.set_ids(d)["idle"]
        t = self.states_board(); seen = sample_series(lambda: self.ids_by_card(t), 9.0, 0.1)
        self.assertEqual({v for _, s in seen for v in s.values()}, set(f))

    def test_busy_states_do_not_blink_they_step_every_tick(self):
        d = state_set(self.cfg, "mine", {"idle": 3, "think": 3, "read": 0, "write": 0, "tool": 0, "hi": 0, "success": 0}); f = self.set_ids(d)
        t = self.states_board(); seen = sample_series(lambda: self.ids_by_card(t), 5.0, 0.04)
        for n in ("s-think", "s-bash"):
            seq = [s[n] for _, s in seen]; ch = [seen[i][0] for i in range(1, len(seq)) if seq[i] != seq[i - 1]]
            self.assertGreaterEqual(len(ch), 8, f"{n}: {len(ch)} changes in 5 s"); gaps = [b - a for a, b in zip(ch, ch[1:])]
            # 采样器在负载下会停顿，把某次换帧晚记、下一次显得很近：看均值（≈一个 500ms tick）、不漏拍、没有近乎同时的两次换帧，不看每个间隔
            self.assertTrue(0.4 <= (ch[-1] - ch[0]) / (len(ch) - 1) <= 0.6 and all(0.1 <= g <= 0.9 for g in gaps), f"{n}: one frame per 500 ms tick: {[round(g, 2) for g in gaps]}")
            self.assertTrue(set(seq) <= set(f["think"]))


if __name__ == "__main__":
    unittest.main()
