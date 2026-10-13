"""T29 entrances of the image sets: `agmux avatars` prints sets then sprites; the README config example is the real config (the `a` picker itself is test_t32: a grid since T32). Spec: specs/m3-config/design.md「头像用真图」「选择与绑定」, tasks.md T29.
Which image id a frame has is not asserted here (see lib.upload_ids: the id of a frame is looked up from what agmux uploads)."""
import json, re, subprocess, tomllib, unittest
from lib import *
from test_t3 import ready, by_name, NAMES, last_text_row, C_FOCUS
from test_t2 import run_avatars, parse_blocks, sprite_blocks
from test_t18 import overlay, IDS, LISTING, SPRITE_NAMES, HASHED_SPRITE
import test_t28 as t28
from test_t28 import BUILTIN_LINE
from test_t28 import cards_of, named, card_avatar

SETS = t28.SETS
IDLE_PNG = (SETS / "idle" / "idle.png").read_bytes()  # the calm face: first of the idle frames (its source file; ids are looked up with lib.upload_ids)


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
        raw = raw or ""  # while the picker is still on screen its box looks like a card named "avatar for pj-fallback": not the card
        cs = [c for c in cards_of(raw) if "pj-fallback" in c.line(1) and "avatar for" not in c.line(1)]; assert len(cs) == 1, f"pj-fallback: {len(cs)} cards"; return cs[0], parse_screen(raw)


class AvatarsCmd(Base):
    def run_cmd(self, *args, config=None):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-256color", "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def sets_block(self):
        return self.run_cmd("avatars").stdout.split("\n\n")[0].split("\n")

    def test_sets_first_one_line_each_then_a_blank_line_then_the_sprites(self):
        r = self.run_cmd("avatars"); self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stderr, "")
        self.assertEqual(r.stdout.split("\n\n")[0].split("\n"), set_lines(self.cfg)); self.assertEqual(r.stdout.split("\n")[0], BUILTIN_LINE)
        self.assertEqual([b[0] for b in sprite_blocks(r.stdout)], [f"{i}  {n}" for i, n in zip(IDS, SPRITE_NAMES)]); self.assertNotIn("\x1b", r.stdout)

    def test_user_sets_say_where_they_come_from_and_their_frame_counts(self):
        write_avatar_set(self.cfg, "mine", {"idle": 1, "think": 3}); write_avatar_set(self.cfg, "zeta", {"idle": 2, "success": 1})
        out = self.sets_block(); self.assertEqual(out, set_lines(self.cfg)); self.assertEqual(out[-2:], ["mine  user  idle 1 · think 3 · read 0 · write 0 · tool 0 · hi 0 · success 0", "zeta  user  idle 2 · think 0 · read 0 · write 0 · tool 0 · hi 0 · success 1"],
                                                                                        "built-in sets first, then the user's in name order")

    def test_a_user_set_with_the_builtin_name_replaces_its_line_in_place(self):
        write_avatar_set(self.cfg, "aza_choi", {"idle": 2}); out = self.sets_block()
        self.assertEqual(out[0], "aza_choi  user  idle 2 · think 0 · read 0 · write 0 · tool 0 · hi 0 · success 0"); self.assertEqual(out, set_lines(self.cfg)); self.assertEqual(len(out), len(set_lines()))

    def test_only_png_files_are_counted(self):
        write_avatar_set(self.cfg, "mine", {"idle": 1}, **{"idle/a.txt": "x", "think/b.jpg": b"\xff", "success/c.png.bak": b"x", "idle/idle_v2.png": png((8, 8, 8))})
        self.assertIn("mine  user  idle 1 · think 0 · read 0 · write 0 · tool 0 · hi 0 · success 0", self.run_cmd("avatars").stdout)  # idle_v2.png is an alternate take, not a frame (T33)

    def test_a_set_named_like_a_sprite_replaces_the_sprite_block(self):
        write_avatar_set(self.cfg, "bot-02", {"idle": 1}); r = self.run_cmd("avatars")
        self.assertIn("bot-02  user  idle 1", r.stdout); self.assertEqual([b[0].split("  ")[0] for b in sprite_blocks(r.stdout)], ["bot-01", "bot-03", "bot-04", "bot-05"])

    def test_a_skipped_set_goes_to_stderr_in_one_line_and_the_exit_code_stays_0(self):
        write_avatar_set(self.cfg, "noidle", {"think": 2}); r = self.run_cmd("avatars")
        self.assertEqual(r.returncode, 0); self.assertEqual(r.stderr, 'agmux: avatar set "noidle" has no idle/*.png: skipped\n'); self.assertNotIn("noidle", r.stdout)

    def test_help_mentions_sets_then_sprites(self):
        r = self.run_cmd("help"); self.assertRegex(r.stdout, r"avatars\s+print every avatar: image sets, then pixel sprites")


class Readme(unittest.TestCase):
    """The configuration example in README.md is a real config: it loads, and its [avatars] lines are what `agmux config` prints by default."""
    README = (SRC / "README.md").read_text()

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
