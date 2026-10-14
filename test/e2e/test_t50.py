"""T50 `agmux config` prints the complete commented template (the golden file internal/config/testdata/config.example.toml): the values are the effective ones, the `# default:` notes are fixed, the bindings of
[avatars.bind] come sorted after the comment block; the output is a valid config that reads back to the same output. Spec: specs/m3-config/tasks.md T50. Oracle: the golden file in the source tree under test."""
import difflib, subprocess, tomllib, unittest
from lib import *

TEMPLATE = (SRC / "internal" / "config" / "testdata" / "config.example.toml").read_text()


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True); self.file = self.cfg / "agmux" / "config.toml"

    def out(self, text=None, cfg=None):
        if text is not None: self.file.write_text(text)
        r = subprocess.run([BIN, "config"], env={"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(cfg or self.cfg), "XDG_STATE_HOME": str(self.env.state), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10)
        self.assertEqual(r.returncode, 0, r.stderr); self.assertEqual(r.stderr, ""); return r.stdout

    def diff(self, a, b): return [l for l in difflib.unified_diff(a.splitlines(), b.splitlines(), "template", "output", lineterm="", n=0) if not l.startswith(("---", "+++", "@@"))]


class Default(Base):
    def test_the_default_output_is_the_template_line_for_line(self):
        out = self.out(); self.assertEqual(self.diff(TEMPLATE, out), [], "no difference from the golden file"); self.assertEqual(out, TEMPLATE)

    def test_no_config_file_at_all_is_the_same(self):
        self.assertFalse(self.file.exists()); self.assertEqual(self.out(), TEMPLATE)

    def test_an_empty_file_is_the_same(self):
        self.assertEqual(self.out(""), TEMPLATE)

    def test_it_is_a_valid_config_every_action_and_color_has_a_line_with_its_default(self):
        out = self.out(); d = tomllib.loads(out); self.assertEqual(set(d), {"keys", "colors", "layout", "avatars"}); self.assertEqual(d["avatars"]["bind"], {}, "an empty [avatars.bind]")
        from test_t17 import DEFAULT_KEYS
        for a in DEFAULT_KEYS: self.assertRegex(out, rf"(?m)^{a} = \[.*\]\s+# default: ", a)
        self.assertEqual(sorted(d["keys"]), sorted(DEFAULT_KEYS)); self.assertEqual(len(d["colors"]), 19, "every color")
        for k in list(d["keys"]) + list(d["colors"]) + ["max_columns", "flow", "group_by", "renderer", "default"]: self.assertRegex(out, rf"(?m)^{k} = .*# default: ", k)

    def test_the_template_ends_with_the_bind_section_and_its_comments(self):
        out = self.out(); self.assertTrue(out.rstrip().endswith("`agmux avatars import <sheet.png> --name <id>`.")); self.assertTrue(out.endswith("\n"))


class Values(Base):
    CUSTOM = '[layout]\nmax_columns = 5\nflow = "stack"\ngroup_by = "state"\n[colors]\nborder = "#112233"\nname = "#abcdef"\n[keys]\nflash = ["S", "ctrl+f"]\nquit = ["q"]\n[avatars]\nrenderer = "image"\ndefault = "nova"\n'

    def test_changed_values_show_but_every_default_note_stays(self):
        out = self.out(self.CUSTOM); delta = self.diff(TEMPLATE, out)
        self.assertEqual(len(delta), 18, "9 lines changed (a minus and a plus each) and nothing else:\n" + "\n".join(delta)); d = tomllib.loads(out)
        self.assertEqual((d["layout"]["max_columns"], d["layout"]["flow"], d["layout"]["group_by"]), (5, "stack", "state")); self.assertEqual((d["colors"]["border"], d["colors"]["name"]), ("#112233", "#abcdef"))
        self.assertEqual((d["keys"]["flash"], d["keys"]["quit"]), (["S", "ctrl+f"], ["q"])); self.assertEqual((d["avatars"]["renderer"], d["avatars"]["default"]), ("image", "nova"))
        for l in out.splitlines():
            m = re.match(r"^(max_columns|flow|group_by|border|name|flash|quit|renderer|default) = .*# (default: .*)$", l)
            if m: self.assertIn(m.group(2), "\n".join(TEMPLATE.splitlines()), f"the note of {m.group(1)} is the template's: {l!r}")
        self.assertRegex(out, r'(?m)^max_columns = 5 +# default: 4 — 1 to 8; fewer columns are used when the terminal is narrow$')
        self.assertRegex(out, r'(?m)^border = "#112233" +# default: "" \(follow the status\) — borders of unfocused cards$')

    def test_changed_value_lines_differ_only_in_the_value(self):
        out = self.out(self.CUSTOM); T = TEMPLATE.splitlines(); O = out.splitlines(); self.assertEqual(len(T), len(O), "same number of lines")
        for t, o in zip(T, O):
            if t != o: self.assertEqual(t.split("# default:", 1)[1:], o.split("# default:", 1)[1:], f"{o!r}: the note after the value is unchanged"); self.assertEqual(t.split(" = ")[0], o.split(" = ")[0], "same key")

    def test_the_output_written_back_gives_the_same_output(self):
        for text in ("", self.CUSTOM):
            with self.subTest(text=text[:20]):
                first = self.out(text); self.file.write_text(first); second = self.out(); self.assertEqual(second, first, "agmux config > config.toml; agmux config: the same"); self.file.write_text(second); self.assertEqual(self.out(), first)

    def test_a_generated_file_is_a_working_config_for_the_board(self):
        self.file.write_text(self.out(self.CUSTOM)); build_fixture(self.env); t = Term(self.env, w=200, h=40, extra_env=f"XDG_CONFIG_HOME={self.cfg}"); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len(find_cards(t.snap())) >= 8, 10), t.text()); t.keys("q"); self.assertEqual(t.exit_status(5), 0)


class Binds(Base):
    def test_bindings_are_printed_sorted_in_the_bind_section_after_its_comments(self):
        out = self.out('[avatars.bind]\n"zeta" = "nova"\n"alpha" = "cipher"\n"mid" = "oana"\n'); tail = out.split("[avatars.bind]\n", 1)[1].splitlines()
        self.assertTrue(all(l.startswith("#") for l in tail[:-3]), tail); self.assertEqual(tail[-3:], ['"alpha" = "cipher"', '"mid" = "oana"', '"zeta" = "nova"'], "comments first, then the bindings by name")
        self.assertEqual(self.diff(TEMPLATE, out), ['+"alpha" = "cipher"', '+"mid" = "oana"', '+"zeta" = "nova"'])

    def test_names_with_quotes_unicode_and_control_characters_read_back(self):
        names = ['has "quote"', "back\\slash", "tab\tname", "bell\a", "esc\x1bseq", "del\x7f", "会话-名字", "emoji-🙂", "nul\x00x", "ls x"]
        lines = []
        for n in names: lines.append("{} = \"nova\"".format('"' + "".join(c if (c.isprintable() and c not in '"\\') else ("\\\\" if c == "\\" else '\\"' if c == '"' else "\\u%04x" % ord(c)) for c in n) + '"'))
        first = self.out("[avatars.bind]\n" + "\n".join(lines) + "\n"); d = tomllib.loads(first); self.assertEqual(sorted(d["avatars"]["bind"]), sorted(names), "every name survives the print")
        self.file.write_text(first); self.assertEqual(self.out(), first, "and printing the printed file changes nothing")
        for l in first.splitlines(): self.assertNotRegex(l, r"[\x00-\x08\x0a-\x1f\x7f ]|\\x[0-9a-f]{2}|\\a", f"escapes are TOML's \\uXXXX, no raw control: {l!r}")


class Other(Base):
    def test_a_bad_config_still_fails_with_the_error_and_exit_code(self):
        self.file.write_text('[keys]\nup = ["x"]\ndown = ["x"]\n'); r = subprocess.run([BIN, "config"], env={"PATH": "/usr/bin:/bin", "HOME": str(self.env.home), "XDG_CONFIG_HOME": str(self.cfg), "LANG": "en_US.UTF-8"}, capture_output=True, text=True, timeout=10)
        self.assertNotEqual(r.returncode, 0); self.assertNotEqual(r.stderr, ""); self.assertEqual(r.stdout, "", "no half template")


if __name__ == "__main__":
    unittest.main()
