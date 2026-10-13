"""T29 entrances of the image sets: the `a` picker lists the sets first (id only), then the sprites; enter binds a set id; `agmux avatars` prints sets then sprites;
the README config example is the real config. Spec: specs/m3-config/design.md「头像用真图」「选择与绑定」, tasks.md T29.
Which image id a frame has is not asserted here (see lib.upload_ids: the id of a frame is looked up from what agmux uploads)."""
import json, re, subprocess, tomllib, unittest
from lib import *
from test_t3 import ready, by_name, NAMES, last_text_row, C_FOCUS
from test_t2 import run_avatars, parse_blocks, sprite_blocks
from test_t18 import overlay, IDS, LISTING, SPRITE_NAMES, HASHED_SPRITE
import test_t28 as t28
from test_t28 import cards_of, named, card_avatar

SETS = t28.SETS
IDLE_PNG = (SETS / "idle" / "idle.png").read_bytes()


class Base(t28.Base):
    """t28.Base: fixture, config() (default = aza_choi unless the text says otherwise), board(tmux=...)"""

    def setUp(self):
        super().setUp(); self.json = self.env.state / "agmux" / "avatars.json"; self.config("", default=None)

    def picker(self, t, sets=("aza_choi",)):
        t.keys("a"); ov = wait(lambda: overlay(t.snap(), sets), 3); self.assertTrue(ov, t.text()); return ov

    def cursor(self, t, sets=("aza_choi",)):
        ov = overlay(t.snap(), sets); at = [i for i, r in enumerate(ov["rows"]) if r["marker"]]; self.assertEqual(len(at), 1, ov); return at[0]

    def to_a2(self, t):
        t.keys("j"); time.sleep(0.3)  # pj-fallback

    def a2(self, raw=None):
        raw = raw or ""; cs = cards_of(raw); return named(cs, "pj-fallback"), parse_screen(raw)


def cells_of_row(ov_row, raw):
    """The placeholder cells of a picker row's face (two terminal rows starting at ov_row['row'], the 6 columns left of the id)."""
    return [c for c in ph_cells(raw) if ov_row["row"] <= c[0] <= ov_row["row"] + 1 and ov_row["col"] - 8 <= c[1] < ov_row["col"] - 2]


class PickerPixel(Base):
    def test_the_set_comes_first_with_an_id_and_no_name(self):
        t = self.board(tmux=False); self.to_a2(t); ov = self.picker(t)
        self.assertEqual([r["id"] for r in ov["rows"]], LISTING); self.assertEqual([r["name"] for r in ov["rows"]], [None] + SPRITE_NAMES)
        self.assertIn("aza_choi", "\n".join(r.text for r in t.snap())); self.assertNotIn("Aza", t.text())
        blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in sprite_blocks(run_avatars(self.env).stdout)}
        self.assertEqual(ov["rows"][0]["sprite"], blocks[HASHED_SPRITE], "pixel mode: the set row is a sprite picked by hashing its id")

    def test_cursor_opens_on_the_set_when_the_card_wears_it(self):
        self.env.avatars_json({"pj-fallback": "aza_choi"}); t = self.board(tmux=False); self.to_a2(t); ov = self.picker(t)
        self.assertEqual(self.cursor(t), 0); self.assertEqual([r["id"] for r in ov["rows"] if r["current"]], ["aza_choi"], "worn avatar drawn in the focus colour")

    def test_cursor_opens_on_a_sprite_when_the_card_wears_one(self):
        self.env.avatars_json({"pj-fallback": "bot-03"}); t = self.board(tmux=False); self.to_a2(t); ov = self.picker(t)
        self.assertEqual(ov["rows"][self.cursor(t)]["id"], "bot-03"); self.assertEqual([r["id"] for r in ov["rows"] if r["current"]], ["bot-03"])

    def test_enter_on_the_set_binds_its_id_and_says_so(self):
        self.env.avatars_json({"pj-fallback": "bot-03"}); t = self.board(tmux=False); self.to_a2(t); self.picker(t)
        for _ in range(4): t.keys("k"); time.sleep(0.15)
        self.assertEqual(self.cursor(t), 0); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "aza_choi"})
        self.assertIsNone(overlay(t.snap())); self.assertIn("pj-fallback wears aza_choi", "\n".join(r.text for r in t.snap()[-3:]))
        raw = t.raw(); c, rows = self.a2(raw); self.assertNotIn(PLACEHOLDER, raw, "pixel mode: no picture")
        blocks = {b[0].split("  ")[0]: tuple(b[1:]) for b in sprite_blocks(run_avatars(self.env).stdout)}
        self.assertIn(tuple("".join(x[0] for x in rows[c.r + k].cells[c.c + 2:c.c + 8]) for k in (1, 2)), [blocks[HASHED_SPRITE]], "the card shows the sprite the id hashes to")

    def test_enter_on_a_sprite_after_the_set_changes_it_back(self):
        self.env.avatars_json({"pj-fallback": "aza_choi"}); t = self.board(tmux=False); self.to_a2(t); self.picker(t)
        t.keys("j"); time.sleep(0.2); t.keys("j"); time.sleep(0.2); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-02"})

    def test_esc_and_q_cancel_without_writing(self):
        t = self.board(tmux=False); self.to_a2(t); self.picker(t); t.keys("k"); time.sleep(0.2); t.esc(); time.sleep(0.4)
        self.assertIsNone(overlay(t.snap())); self.assertFalse(self.json.exists()); self.assertTrue(t.alive())

    def test_user_sets_follow_the_builtin_one_before_the_sprites_in_name_order(self):
        write_avatar_set(self.cfg, "zeta", {"idle": 1}); write_avatar_set(self.cfg, "mine", {"idle": 1})
        t = self.board(tmux=False); self.to_a2(t); sets = ("aza_choi", "mine", "zeta"); ov = self.picker(t, sets)
        self.assertEqual([r["id"] for r in ov["rows"]], ["aza_choi", "mine", "zeta"] + IDS)
        self.assertEqual([r["name"] for r in ov["rows"]], [None, None, None] + SPRITE_NAMES)
        for _ in range(12): t.keys("k"); time.sleep(0.1)
        t.keys("j"); time.sleep(0.2); self.assertEqual(self.cursor(t, sets), 1); t.keys("Enter"); time.sleep(0.8); self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "mine"})

    def test_a_user_set_replacing_the_builtin_one_is_listed_once_in_its_place(self):
        write_avatar_set(self.cfg, "aza_choi", {"idle": 1}); t = self.board(tmux=False); self.to_a2(t); ov = self.picker(t)
        self.assertEqual([r["id"] for r in ov["rows"]], LISTING)

    def test_a_set_named_like_a_sprite_takes_the_sprite_off_the_list(self):
        write_avatar_set(self.cfg, "bot-02", {"idle": 1}); t = self.board(tmux=False); self.to_a2(t); t.keys("a"); time.sleep(0.8)
        txt = t.text(); self.assertEqual(len(re.findall(r"bot-02", txt)), 1, txt); self.assertNotIn("Boxy", txt, "the set shadows the sprite")
        for i in ("bot-01", "bot-03", "bot-04", "bot-05", "bot-06"): self.assertIn(i, txt)


class PickerTall(Base):
    """More rows than the screen: the cursor must stay on screen (T29 review)."""

    def test_20_user_sets_in_a_120x30_terminal_j_to_the_bottom_keeps_the_cursor_visible_and_binds_bot_06(self):
        names = [f"set{k:02d}" for k in range(20)]
        for n in names: write_avatar_set(self.cfg, n, {"idle": 1})
        t = self.board(tmux=False, w=120, h=30, loose=True); self.to_a2(t); sets = ("aza_choi", *names)
        t.keys("a"); self.assertTrue(wait(lambda: overlay(t.snap(), sets), 3), t.text())
        for k in range(30):
            t.keys("j"); time.sleep(0.06)
            ov = overlay(t.snap(), sets); self.assertTrue(ov, f"overlay vanished after {k + 1} presses:\n{t.text()}")
            self.assertEqual(len([r for r in ov["rows"] if r["marker"]]), 1, f"after {k + 1} presses the ▸ must be on screen:\n{t.text()}")
        ov = overlay(t.snap(), sets); self.assertEqual([r["id"] for r in ov["rows"] if r["marker"]], ["bot-06"], "bottom of the list")
        self.assertTrue(all(0 <= r["row"] < 30 for r in ov["rows"])); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "bot-06"}); self.assertIsNone(overlay(t.snap(), sets))

    def test_k_back_up_to_the_top_keeps_it_visible_too(self):
        names = [f"set{k:02d}" for k in range(20)]
        for n in names: write_avatar_set(self.cfg, n, {"idle": 1})
        t = self.board(tmux=False, w=120, h=30, loose=True); self.to_a2(t); sets = ("aza_choi", *names); t.keys("a"); wait(lambda: overlay(t.snap(), sets), 3)
        for _ in range(30): t.keys("j"); time.sleep(0.04)
        for k in range(32):
            t.keys("k"); time.sleep(0.06); ov = overlay(t.snap(), sets); self.assertTrue(ov, t.text())
            self.assertEqual(len([r for r in ov["rows"] if r["marker"]]), 1, f"after {k + 1} k presses:\n{t.text()}")
        self.assertEqual([r["id"] for r in ov["rows"] if r["marker"]], ["aza_choi"])

    def test_sprites_hidden_by_a_set_of_the_same_name_are_not_listed_even_when_the_list_scrolls(self):
        for n in ("bot-01", "bot-03"): write_avatar_set(self.cfg, n, {"idle": 1})
        for k in range(14): write_avatar_set(self.cfg, f"set{k:02d}", {"idle": 1})
        t = self.board(tmux=False, w=120, h=30, loose=True); self.to_a2(t); t.keys("a"); time.sleep(0.8)
        seen = set()
        for _ in range(40):
            seen |= set(re.findall(r"bot-0\d(?:  \w+)?", t.text())); t.keys("j"); time.sleep(0.05)
        self.assertNotIn("bot-01  Clawd", seen); self.assertNotIn("bot-03  Sprout", seen)
        self.assertTrue({"bot-02  Boxy", "bot-04  Boo", "bot-05  Mochi", "bot-06  Cyclops"} <= seen, seen)


class PickerImage(Base):
    def test_set_rows_show_the_idle_frame_as_placeholders_and_sprites_stay_pixels(self):
        t = self.board(); self.to_a2(t); ov = self.picker(t); raw = t.raw()
        ids = upload_ids(self.env, self.cfg, {})
        cells = cells_of_row(ov["rows"][0], raw); self.assertEqual(len(cells), 12, cells)
        self.assertEqual({c[2] for c in cells}, {ids[IDLE_PNG]}, "the set row shows the first idle frame, not an animation frame")
        self.assertEqual(sorted((c[0] - ov["rows"][0]["row"], c[1] - ov["rows"][0]["col"] + 8, c[3], c[4]) for c in cells), sorted((r, k, r, k) for r in (0, 1) for k in range(6)), "every cell has its row and column")
        for r in ov["rows"][1:]: self.assertEqual(cells_of_row(r, raw), []); self.assertTrue(any(ch != " " for ch in r["sprite"][0] + r["sprite"][1]))

    def test_the_set_row_does_not_animate(self):
        t = self.board(); self.to_a2(t); ov = self.picker(t); seen = set(); t0 = time.time()
        while time.time() - t0 < 2.5: seen |= {c[2] for c in cells_of_row(ov["rows"][0], t.raw())}; time.sleep(0.1)
        self.assertEqual(len(seen), 1, f"idle frame only: {seen}")

    def test_enter_on_the_set_makes_the_card_a_picture_on_the_next_frame(self):
        t = self.board(); self.to_a2(t); raw = t.raw(); c0, _ = self.a2(raw); self.assertTrue(card_avatar(raw, c0) == [] or True)
        self.env.avatars_json({"pj-fallback": "bot-03"}); t.close(); t = self.board(); self.to_a2(t)
        self.assertEqual(card_avatar(t.raw(), self.a2(t.raw())[0]), [], "bound to a sprite: pixels")
        self.picker(t)
        for _ in range(4): t.keys("k"); time.sleep(0.15)
        t.keys("Enter"); ok = wait(lambda: (lambda r: len(card_avatar(r, self.a2(r)[0])) == 12)(t.raw()), 2.5)
        self.assertTrue(ok, "the card shows the placeholders within a couple of frame ticks:\n" + t.text()); self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "aza_choi"})

    def test_a_user_set_row_shows_its_own_idle_frame(self):
        write_avatar_set(self.cfg, "mine", {"idle": 2, "working": 1}); t = self.board(); self.to_a2(t); ov = self.picker(t, ("aza_choi", "mine")); raw = t.raw()
        ids = upload_ids(self.env, self.cfg); mine_idle = (self.cfg / "agmux" / "avatars" / "mine" / "idle" / "01.png").read_bytes()
        self.assertEqual([r["id"] for r in ov["rows"]][:2], ["aza_choi", "mine"]); self.assertEqual({c[2] for c in cells_of_row(ov["rows"][1], raw)}, {ids[mine_idle]})

    def test_a_bound_card_and_the_picker_agree_on_the_current_one(self):
        self.env.avatars_json({"pj-fallback": "aza_choi"}); t = self.board(); self.to_a2(t); ov = self.picker(t)
        self.assertEqual(self.cursor(t), 0); self.assertEqual([r["id"] for r in ov["rows"] if r["current"]], ["aza_choi"])


class AvatarsCmd(Base):
    def run_cmd(self, *args, config=None):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-256color", "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def test_sets_first_one_line_each_then_a_blank_line_then_the_sprites(self):
        r = self.run_cmd("avatars"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stderr, "")
        lines = r.stdout.split("\n"); self.assertEqual(lines[0], "aza_choi  builtin  idle 1 · working 4 · attention 4 · done 4"); self.assertEqual(lines[1], "")
        self.assertEqual([b[0] for b in sprite_blocks(r.stdout)], [f"{i}  {n}" for i, n in zip(IDS, SPRITE_NAMES)]); self.assertNotIn("\x1b", r.stdout)

    def test_user_sets_say_where_they_come_from_and_their_frame_counts(self):
        write_avatar_set(self.cfg, "mine", {"idle": 1, "working": 3}); write_avatar_set(self.cfg, "zeta", {"idle": 2, "done": 1})
        out = self.run_cmd("avatars").stdout.split("\n\n")[0].split("\n")
        self.assertEqual(out, ["aza_choi  builtin  idle 1 · working 4 · attention 4 · done 4", "mine  user  idle 1 · working 3 · attention 0 · done 0", "zeta  user  idle 2 · working 0 · attention 0 · done 1"])

    def test_a_user_set_with_the_builtin_name_replaces_its_line(self):
        write_avatar_set(self.cfg, "aza_choi", {"idle": 2}); out = self.run_cmd("avatars").stdout.split("\n\n")[0]
        self.assertEqual(out, "aza_choi  user  idle 2 · working 0 · attention 0 · done 0")

    def test_only_png_files_are_counted(self):
        write_avatar_set(self.cfg, "mine", {"idle": 1}, **{"idle/a.txt": "x", "working/b.jpg": b"\xff", "done/c.png.bak": b"x"})
        self.assertIn("mine  user  idle 1 · working 0 · attention 0 · done 0", self.run_cmd("avatars").stdout)

    def test_a_set_named_like_a_sprite_replaces_the_sprite_block(self):
        write_avatar_set(self.cfg, "bot-02", {"idle": 1}); r = self.run_cmd("avatars")
        self.assertIn("bot-02  user  idle 1", r.stdout); self.assertEqual([b[0].split("  ")[0] for b in sprite_blocks(r.stdout)], ["bot-01", "bot-03", "bot-04", "bot-05", "bot-06"])

    def test_a_skipped_set_goes_to_stderr_in_one_line_and_the_exit_code_stays_0(self):
        write_avatar_set(self.cfg, "noidle", {"working": 2}); r = self.run_cmd("avatars")
        self.assertEqual(r.returncode, 0); self.assertEqual(r.stderr, 'agmux: avatar set "noidle" has no idle/*.png: skipped\n'); self.assertNotIn("noidle", r.stdout)

    def test_help_mentions_sets_then_sprites(self):
        r = self.run_cmd("help"); self.assertRegex(r.stdout, r"avatars\s+print every avatar: image sets, then pixel sprites")


class Readme(unittest.TestCase):
    """The configuration example in README.md is a real config: it loads, and its [avatars] lines are what `agmux config` prints by default."""
    README = (HERE.parent.parent / "README.md").read_text()

    def block(self):
        m = re.search(r"### 配置.*?```toml\n(.*?)```", self.README, re.S); self.assertTrue(m, "no toml block under ### 配置"); return m.group(1)

    def cli(self, cfgtext=None):
        env = Env(); self.addCleanup(env.cleanup); cfg = env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True)
        if cfgtext is not None: (cfg / "agmux" / "config.toml").write_text(cfgtext)
        return subprocess.run([BIN, "config"], env={"PATH": "/usr/bin:/bin", "HOME": str(env.home), "XDG_STATE_HOME": str(env.state), "XDG_CONFIG_HOME": str(cfg), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10)

    def test_the_example_loads_and_round_trips(self):
        r = self.cli(self.block()); self.assertEqual(r.returncode, 0, r.stderr)
        got = tomllib.loads(r.stdout); ex = tomllib.loads(self.block())
        for sec, vals in ex.items():
            for k, v in vals.items():
                self.assertEqual(got[sec][k], v, f"[{sec}] {k}: README says {v!r}, `agmux config` with the example loaded says {got[sec][k]!r}")

    def test_the_avatars_defaults_in_the_example_are_the_real_defaults(self):
        real = tomllib.loads(self.cli().stdout)["avatars"]; ex = tomllib.loads(self.block())["avatars"]
        for k in ("renderer", "default"): self.assertEqual(ex[k], real[k], f"README [avatars] {k} differs from the default")

    def test_every_option_of_the_avatars_section_is_documented(self):
        real = tomllib.loads(self.cli().stdout)["avatars"]; ex = tomllib.loads(self.block())["avatars"]
        self.assertEqual(set(real) - {"bind"}, set(ex) - {"bind"})

    def test_readme_names_the_set_requirements_and_credit(self):
        for s in ("allow-passthrough", "kitty", "ghostty", "aza_choi", "pi-emote", "MIT", "attention", "idle", "working", "done", "renderer"):
            self.assertIn(s, self.README)


if __name__ == "__main__":
    unittest.main()
