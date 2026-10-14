"""T49 ten more built-in image sets (developer / researcher / operator / tester / designer, naruto / sasuke / sakura / kakashi / itachi): `agmux avatars` lists 20 sets then the 5 sprites in a fixed order, each new set with all
seven states; a set of the user's with a built-in id replaces it in place; the picker (4 rows of 5, scrolling) shows the same order; the board uploads 281 frames (141 + 10 x 14) through the client tty without a cut APC; the default
hash picks among all 20 sets. Spec: specs/m3-config/tasks.md T49. The order is written out here from the spec, not read from the product."""
import json, re, subprocess, time, unittest
from lib import *
from lib import BUILTIN_ORDER
import test_t28 as t28
from test_t18 import sweep, to_start, overlay, SPRITE_NAMES, IDS, find_grid
from test_t32 import Base as B32
from test_t38 import Base as B38
from test_t28 import card_avatar, cards_of, IMG_DIMS

SETS20 = ["aza_choi", "cipher", "default", "forge", "moss", "nova", "oana", "quill", "red", "sprocket", "developer", "researcher", "operator", "tester", "designer", "naruto", "sasuke", "sakura", "kakashi", "itachi"]
NEW10 = SETS20[10:]
ALL7 = "idle 2 · think 2 · read 2 · write 2 · tool 2 · hi 2 · success 2"


class Listing(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.cfg = self.env.tmp / "cfg"; self.cfg.mkdir()

    def avatars(self):
        r = subprocess.run([BIN, "avatars"], env={"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(self.cfg), "XDG_STATE_HOME": str(self.env.state), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10)
        self.assertEqual(r.returncode, 0, r.stderr); return r.stdout

    def test_the_helper_order_is_the_spec_order(self):
        self.assertEqual(BUILTIN_ORDER, SETS20)

    def test_twenty_builtin_sets_in_the_fixed_order_then_a_blank_line_then_the_five_sprites(self):
        out = self.avatars(); sets, _, rest = out.partition("\n\n"); lines = sets.split("\n")
        self.assertEqual([l.split("  ")[0] for l in lines], SETS20); self.assertTrue(all("  builtin  " in l for l in lines), lines)
        self.assertEqual(re.findall(r"^(bot-\d\d)  ", rest, re.M), IDS)

    def test_each_new_set_has_all_seven_states(self):
        for l in self.avatars().split("\n\n")[0].split("\n"):
            sid = l.split("  ")[0]
            if sid in NEW10: self.assertEqual(l, f"{sid}  builtin  {ALL7}", f"{sid}: every state has frames (14 in all)")

    def test_a_user_set_with_a_builtin_id_replaces_it_and_keeps_its_place(self):
        write_avatar_set(self.cfg, "designer", {"idle": 1}); write_avatar_set(self.cfg, "naruto", {"idle": 3, "hi": 1}); lines = self.avatars().split("\n\n")[0].split("\n")
        self.assertEqual([l.split("  ")[0] for l in lines], SETS20, "same 20 ids, same positions")
        self.assertEqual(lines[14], "designer  user  idle 1 · think 0 · read 0 · write 0 · tool 0 · hi 0 · success 0"); self.assertEqual(lines[15], "naruto  user  idle 3 · think 0 · read 0 · write 0 · tool 0 · hi 1 · success 0")
        self.assertEqual(lines[13], f"tester  builtin  {ALL7}")

    def test_a_user_set_of_another_name_comes_after_the_twenty(self):
        write_avatar_set(self.cfg, "mine", {"idle": 1}); self.assertEqual([l.split("  ")[0] for l in self.avatars().split("\n\n")[0].split("\n")], SETS20 + ["mine"])


class Picker(B32):
    def test_the_picker_walks_the_twenty_then_the_sprites_in_four_scrolling_rows(self):
        t = self.board(tmux=False); self.to_a2(t); ov = self.open(t); self.assertEqual(ov["shown"], 4, "25 cells, 4 rows at most")
        cells = sweep(t); self.assertEqual([r["id"] for r in cells], SETS20 + IDS); self.assertEqual([r["name"] for r in cells][20:], SPRITE_NAMES)
        order = SETS20 + IDS; total = (len(order) + 4) // 5
        for target in (13, 14, 15, 19, 20, 24, 17, 0):
            self.move_to(t, order[target], order); ov = overlay(t.snap()); first = min(max(target // 5 - 4 // 2, 0), total - 4)
            self.assertEqual([r["id"] for r in ov["rows"]], order[first * 5:(first + 4) * 5][:len(ov["rows"])], f"cursor on {order[target]}: rows {first}..{first + 3}"); self.assertEqual(self.cursor(t), order[target])

    def test_enter_on_a_new_set_binds_it(self):
        t = self.board(tmux=False); self.to_a2(t); self.open(t); self.move_to(t, "kakashi", SETS20 + IDS); t.keys("Enter"); time.sleep(0.8)
        self.assertEqual(json.loads(self.json.read_text()), {"pj-fallback": "kakashi"})


class Upload(B38):
    def test_281_frames_go_to_the_client_tty_whole_and_decodable_and_fast_enough(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); t0 = time.time(); r = self.run_board(settle=0, timeout=30)
        last = [0, time.time()]
        def quiet():
            n = len(tty.bytes())
            if n != last[0]: last[:] = [n, time.time()]
            return time.time() - last[1] > 0.5
        self.assertTrue(wait(quiet, 30, 0.05)); took = last[1] - t0; data = tty.bytes(); cmds = kitty_commands(data); imgs = kitty_images(cmds)
        self.assertEqual(len(imgs), 281, "141 + 10 sets x 7 states x 2 frames"); self.assertGreater(len(cmds), 4000, "768-byte chunks"); self.assertEqual(len(re.findall(rb"\x1b_G", data)), len(cmds), "every APC is a whole sequence")
        for c in cmds: self.assertLess(len(c["raw"]), 1024)
        for i in imgs.values(): self.assertTrue(i["data"].startswith(b"\x89PNG"))
        self.assertLess(took, 6.0, f"the whole upload ({len(data) / 1e6:.1f} MB) with a fast reader took {took:.1f}s (dev: < 1 s)"); r.send("q"); self.assertTrue(r.exited(5))

    def test_the_new_frames_are_the_126x84_crops_and_distinct_per_set(self):
        tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); r = self.run_board(); imgs = list(kitty_images(kitty_commands(tty.bytes())).values())
        keys = [(s, d, f) for s, _, fr in set_list() for d, f in fr]; self.assertEqual(len(keys), len(imgs))
        idle = {}
        for (s, d, f), im in zip(keys, imgs):
            if s in NEW10:
                self.assertEqual(IMG_DIMS(im["data"]), (126, 84), f"{s}/{d}/{f}")
                if (d, f) == ("idle", "idle.png"): idle[s] = im["data"]
        self.assertEqual(sorted(idle), sorted(NEW10)); self.assertEqual(len(set(idle.values())), 10, "ten different faces"); r.send("q")

    def test_a_user_set_with_a_builtin_id_replaces_its_frames_in_the_upload(self):
        write_avatar_set(self.cfg, "designer", {"idle": 1}); tty = self.tty(); self.env.set_tmux(session="fl-own", clients=[(100, "outer", "main", "xterm-kitty", tty.path)]); r = self.run_board()
        self.assertEqual(len(kitty_images(kitty_commands(tty.bytes()))), 281 - 14 + 1); r.send("q")


class Hash(t28.Base):
    def test_the_default_hash_in_image_mode_reaches_the_new_sets(self):
        self.config('[avatars]\ndefault = "hash"\n'); table, _ = frame_table(self.env, self.cfg); who = {}
        for (s, d, f), i in table.items(): who.setdefault(i, s)
        t = self.board(); raw = t.raw(); sets = set()
        for c in cards_of(raw):
            ids = {p[2] for p in card_avatar(raw, c)}; self.assertEqual(len(card_avatar(raw, c)), 12); sets |= {who[i] for i in ids if i in who}
        self.assertTrue(sets & set(NEW10), f"9 cards hash over 20 sets: {sorted(sets)}"); self.assertTrue(sets <= set(SETS20), sets)


if __name__ == "__main__":
    unittest.main()
