"""T31 image avatars fill the 6 x 2 cell box: the board uploads each frame's centred 3:2 part (at most 128 pixels high; kitty ignores a source rectangle for the virtual
placements the placeholders show, so the pixels are cut by agmux), reads sizes from the PNG header, skips frames that are not valid PNGs or are over 4096 x 4096 and says so.
Spec: specs/m3-config/design.md「头像图片：状态、宽度、选择框」4, tasks.md T31 (acceptance as changed by orchestrator: no x/y/w/h in the control string; the payload is the crop)."""
import re, subprocess, unittest
from lib import *
import test_t28 as t28
from test_t28 import IMG_DIMS

MSG = 'avatar set "{}": {} frame(s) are not usable PNGs (invalid, or over 4096 × 4096) and are skipped, first {}'


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True); self.dir = self.cfg / "agmux" / "avatars"

    def put(self, name, **frames):
        """frames: {"idle/01.png": bytes}"""
        for rel, data in frames.items():
            p = self.dir / name / rel; p.parent.mkdir(parents=True, exist_ok=True); p.write_bytes(data)

    def uploaded(self):
        """{(set, dir, file): decoded payload bytes} of what a board uploads with this config dir."""
        table, imgs = frame_table(self.env, self.cfg); return {k: imgs[i]["data"] for k, i in table.items()}, imgs

    def cli(self, *a):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}
        return subprocess.run([BIN, *a], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)


class Crop(Base):
    def check(self, w, h, name="x"):
        self.put("kit", **{"idle/01.png": png_pixels(w, h, lambda x, y: (x % 256, y % 256, (x // 256 + 16 * (y // 256)) % 256))})
        data, _ = self.uploaded(); return decode_png(data[("kit", "idle", "01.png")]), (w, h)

    def test_sizes_follow_the_rule(self):
        for w, h, want in ((128, 128, (128, 85)), (200, 100, (150, 100)), (64, 64, (64, 42)), (300, 100, (150, 100)), (100, 300, (100, 66)), (1, 1, (1, 1)), (2, 1, (1, 1)), (3, 2, (3, 2)), (1024, 1024, (192, 128)), (30, 400, (30, 20))):
            with self.subTest(size=f"{w}x{h}"):
                (pw, ph, rows, ch), _ = self.check(w, h)
                self.assertEqual((pw, ph), want, f"{w}x{h} -> centred 3:2 (rounded down), at most 128 high"); self.assertEqual((pw, ph), expected_upload_size(w, h))
                shutil_rm(self.dir / "kit")

    def test_the_cut_is_the_middle_of_the_picture(self):
        (w, h, rows, ch), _ = self.check(200, 100)  # wider than 3:2: the sides go; pixel (x, y) = (x, y, 0)
        self.assertEqual((w, h), (150, 100)); px = lambda x, y: tuple(rows[y][x * ch:x * ch + 3])
        self.assertEqual((px(0, 0), px(149, 99), px(75, 50)), ((25, 0, 0), (174, 99, 0), (100, 50, 0)), "columns 25..174 of the 200 are kept")
        shutil_rm(self.dir / "kit")
        (w, h, rows, ch), _ = self.check(128, 128)  # square: top and bottom go (21 rows each side)
        self.assertEqual((w, h), (128, 85)); px = lambda x, y: tuple(rows[y][x * ch:x * ch + 3])
        self.assertEqual((px(0, 0)[:2], px(127, 84)[:2], px(64, 42)[:2]), ((0, 21), (127, 105), (64, 63)), "rows 21..105 of the 128 are kept")

    def test_a_3_2_picture_that_is_small_goes_as_it_is_byte_for_byte(self):
        src = png_pixels(60, 40, lambda x, y: (x, y, 7)); self.put("kit", **{"idle/01.png": src}); data, _ = self.uploaded()
        self.assertEqual(data[("kit", "idle", "01.png")], src, "already 3:2 and not over 128 high: no re-encoding")

    def test_a_tall_picture_is_shrunk_by_nearest_pixel_after_the_cut(self):
        (w, h, rows, ch), _ = self.check(1024, 1024)  # cut: 1024 x 682 starting at y = 171; shrunk to 192 x 128
        self.assertEqual((w, h), (192, 128)); cw, chh, cy = 1024, 682, 171
        px = lambda x, y: tuple(rows[y][x * ch:x * ch + 3])
        for ox, oy in ((0, 0), (191, 127), (96, 64), (10, 100), (150, 3), (47, 90)):
            sx = (2 * ox + 1) * cw // (2 * 192); sy = cy + (2 * oy + 1) * chh // (2 * 128)  # the sample point: centre of the output pixel mapped into the cut
            self.assertEqual(px(ox, oy), (sx % 256, sy % 256, (sx // 256 + 16 * (sy // 256)) % 256), f"output pixel ({ox},{oy}) is the source pixel ({sx},{sy})")

    def test_the_control_string_has_no_source_rectangle(self):
        self.put("kit", **{"idle/01.png": png((9, 9, 9), 200, height=100)}); _, imgs = self.uploaded()
        for i, v in imgs.items(): self.assertFalse({"x", "y", "w", "h"} & set(v["ctl"]), f"{v['ctl']}: kitty ignores a source rectangle for placeholders: the crop is in the pixels")

    def test_every_built_in_frame_is_cropped_the_same_way(self):
        _, imgs = self.uploaded(); self.assertGreater(len(imgs), 100); dims = {IMG_DIMS(v["data"]) for v in imgs.values()}
        self.assertIn((64, 42), dims, "a built-in 64x64 frame is sent as 64x42 (cut, not scaled up)"); self.assertTrue(all(h <= 128 and w >= 1 for w, h in dims), dims)

    def test_an_unchanged_frame_keeps_its_id_and_a_changed_one_gets_another(self):
        self.put("kit", **{"idle/01.png": png((1, 2, 3), 30, height=20)}); a, imgs1 = frame_table(self.env, self.cfg); a2, _ = frame_table(self.env, self.cfg); self.assertEqual(a, a2)
        self.put("kit", **{"idle/01.png": png((9, 2, 3), 30, height=20)}); b, _ = frame_table(self.env, self.cfg)
        self.assertNotEqual(a[("kit", "idle", "01.png")], b[("kit", "idle", "01.png")]); self.assertEqual({k: v for k, v in a.items() if k[0] != "kit"}, {k: v for k, v in b.items() if k[0] != "kit"})


def shutil_rm(p):
    __import__("shutil").rmtree(p, ignore_errors=True)


class BadFrames(Base):
    def test_invalid_and_truncated_pngs_are_skipped_and_counted_once_per_set(self):
        good = png((1, 2, 3), 12, height=8)
        self.put("kit", **{"idle/01.png": good, "idle/02.png": BAD_PNG, "idle/03.png": good[:30], "idle/04.png": b"just text", "think/01.png": good, "think/02.png": good[:len(good) // 2]})
        r = self.cli("avatars"); self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(r.stderr, "agmux: " + MSG.format("kit", 4, "idle/02.png") + "\n", "one line per set: how many, and the first one")
        self.assertIn("kit  user  idle 1 · think 1 · read 0 · write 0 · tool 0 · hi 0 · success 0", r.stdout, "only the usable frames are counted")
        r = PtyRun(self.env, extra={"XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-kitty"}); self.addCleanup(r.close); self.assertTrue(wait(lambda: b" sessions" in bytes(r.data), 8))
        self.assertEqual(len(kitty_images(kitty_commands(bytes(r.data)))), t28.N_BUILTIN + 2, "uploaded: the usable frames only (kit/idle/01 and kit/think/01)")

    def test_the_board_keeps_running_shows_the_line_and_uploads_only_the_good_frames(self):
        good = png((1, 2, 3), 12, height=8); self.put("kit", **{"idle/01.png": good, "idle/02.png": BAD_PNG, "think/01.png": good[:20]})
        env = self.env; env.set_term("xterm-kitty", "on")
        r = PtyRun(env, extra={"XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-kitty"}); self.addCleanup(r.close)
        self.assertTrue(wait(lambda: b" sessions" in bytes(r.data), 8)); time.sleep(0.5)
        self.assertIn(MSG.format("kit", 2, "idle/02.png").encode(), bytes(r.data), "on the board's bottom row")
        self.assertEqual(len(kitty_images(kitty_commands(bytes(r.data)))), t28.N_BUILTIN + 1, "built-in frames + kit's one good frame")
        r.send("q"); self.assertTrue(r.exited(5)); self.assertEqual(r.status, 0)

    def test_a_set_whose_idle_frames_are_all_bad_is_skipped_whole(self):
        good = png((1, 2, 3), 12, height=8); self.put("kit", **{"idle/01.png": BAD_PNG, "think/01.png": good})
        r = self.cli("avatars"); self.assertEqual(r.returncode, 0)
        self.assertIn(MSG.format("kit", 1, "idle/01.png"), r.stderr); self.assertIn('avatar set "kit" has no idle/*.png: skipped', r.stderr); self.assertNotIn("kit  user", r.stdout)

    def test_a_picture_over_4096_either_way_is_refused_without_decoding(self):
        ok = png((1, 2, 3), 12, height=8)
        self.put("kit", **{"idle/01.png": ok, "idle/02.png": png((5, 5, 5), 4097, height=1), "idle/03.png": png((5, 5, 5), 1, height=4097), "idle/04.png": png((5, 5, 5), 4096, height=1)})
        r = self.cli("avatars"); self.assertEqual(r.stderr, "agmux: " + MSG.format("kit", 2, "idle/02.png") + "\n", r.stderr)
        self.assertIn("kit  user  idle 2 ·", r.stdout, "4096 exactly is fine: idle/01 and idle/04 count")

    def test_a_good_set_next_to_a_bad_one_is_unaffected(self):
        self.put("good", **{"idle/01.png": png((1, 2, 3), 12, height=8)}); self.put("bad", **{"idle/01.png": BAD_PNG, "idle/02.png": png((4, 5, 6), 12, height=8)})
        r = self.cli("avatars"); self.assertEqual(r.stderr, "agmux: " + MSG.format("bad", 1, "idle/01.png") + "\n"); self.assertIn("good  user  idle 1", r.stdout); self.assertIn("bad  user  idle 1", r.stdout)


if __name__ == "__main__":
    unittest.main()
