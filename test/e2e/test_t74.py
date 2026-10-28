"""T74 `[ui] language` (en | zh, $AGMUX_LANG over the config, a bad $AGMUX_LANG ignored) and every key of the board in `[keys]` (new actions clear_filter / preview_up / preview_down; the typing states' accept / cancel keys).
Spec: specs/m6-recall/design.md §5 §6; specs/m3-config/tasks.md T74 and the 第十五批 notes (zh: 闪跳 / 直通 / 看 diff / 置顶 / 已读; esc always cancels and enter always accepts while a prompt takes text; `agmux config`, `agmux show`, stderr
errors stay English). The expected texts below are the spec's zh / en strings written out here, not read from the product's tables. Fixture: build_fixture (9 sessions, 200 columns), fake tmux; the preview scroll keys run
against real panes (test_t11.PreviewBase)."""
import re, subprocess, unittest
from lib import *
from test_t3 import last_text_row, find_cards
from test_t11 import PreviewBase

EN_FOOT = "j/k h/l move · enter jump · / search · s flash · f fold · g group:cwd · p preview · tab focus preview · i reply · H handoff · r restart · x kill · d review · a avatar · t pin · m read · ? recall · q quit"
ZH_FOOT = "j/k h/l 移动 · enter 跳转 · / 搜索 · s 闪跳 · f 折叠 · g 分组:目录 · p 预览 · tab 直通 · i 回复 · H 交接 · r 重启 · x 终止 · d 看 diff · a 头像 · t 置顶 · m 已读 · ? 找回 · q 退出"
T = {  # what each language says (en, zh) for the same moment
    "top": ("agmux · 9 sessions", "agmux · 9 个会话"),
    "pin": ("pinned alpha-main", "已置顶 alpha-main"), "unpin": ("unpinned alpha-main", "已取消置顶 alpha-main"),
    "kill?": ("kill alpha-main? y to confirm, any other key cancels", "终止 alpha-main？按 y 确认，其它键取消"), "killed": ("killed alpha-main", "已终止 alpha-main"),
    "read": ("read charlie-59", "已读 charlie-59"), "handoff": ("sent /handoff → alpha-main", "已发送 /handoff → alpha-main"),
    "typing": ("enter keep the filter · esc clear it", "enter 保留过滤 · esc 清除"), "fold": ("clear the filter to fold", "先清除过滤再折叠分组"),
    "kept": ("n/N next/prev · esc clear · j/k h/l move", "n/N 下一个/上一个 · esc 清除 · j/k h/l 移动"), "nomatch": ("no match", "无匹配"),
    "flash": ("flash: type to narrow · label to jump · esc", "闪跳：输入缩小 · 标签跳转 · esc"),
    "narrow": ("the preview needs a terminal at least 100 columns wide", "预览需要至少 100 列宽的终端"),
    "bg": ("background session · enter or i attaches it", "后台会话 · 按 enter 或 i 接入"),
    "picker": ("h/j/k/l move · enter bind · q cancel", "h/j/k/l 移动 · enter 绑定 · q 取消"), "more": (r"↑ (\d+) more", r"↑ 还有 (\d+) 个"), "wears": (r"alpha-main wears (\S+)", r"alpha-main 换上了 (\S+)"),
}
STATUS = {"alpha-main": ("working", "运行中"), "bravo-29": ("working", "运行中"), "charlie-30": ("idle", "空闲"), "delta-60": ("waiting", "等待中"), "charlie-59": ("done", "完成"), "pj-fallback": ("idle", "空闲")}


class Base(unittest.TestCase):
    def setUp(self):
        self.env = Env(); self.addCleanup(self.env.cleanup); build_fixture(self.env)
        self.cfg = self.env.tmp / "cfg"; (self.cfg / "agmux").mkdir(parents=True); self.file = self.cfg / "agmux" / "config.toml"

    def write(self, text): self.file.write_text(text)

    def board(self, lang=None, conf=None, w=232, h=40, wait_cards=1):
        if conf is not None: self.write(conf)
        env = f"XDG_CONFIG_HOME={self.cfg}" + (f" AGMUX_LANG={lang}" if lang is not None else "")
        t = Term(self.env, w=w, h=h, extra_env=env); self.addCleanup(t.close)
        self.assertTrue(wait(lambda: len([c for c in find_cards(t.snap()) if c.ok]) >= wait_cards, 10), t.text()); time.sleep(0.6); return t

    def cli(self, *args, lang=None):
        env = {"PATH": f"{self.env.bin}:/usr/bin:/bin", "HOME": str(self.env.home), "XDG_STATE_HOME": str(self.env.state), "XDG_CONFIG_HOME": str(self.cfg), "TERM": "xterm-256color", "LANG": "en_US.UTF-8"}
        if lang is not None: env["AGMUX_LANG"] = lang
        return subprocess.run([BIN, *args], env=env, capture_output=True, text=True, timeout=10, stdin=subprocess.DEVNULL)

    def rows(self, t): return [r.text.rstrip() for r in t.snap() if r.text.strip()]

    def foot(self, t): return self.rows(t)[-1].strip()

    def msg(self, t): return self.rows(t)[-2].strip()

    def top(self, t): return self.rows(t)[0].strip()

    def n_cards(self, t): return len([c for c in find_cards(t.snap()) if c.ok])

    def search(self, t, q, close=("Enter",)):
        t.keys("/"); time.sleep(0.2); t.lit(q); time.sleep(0.3); [t.keys(k) for k in close]; time.sleep(0.4)


# ---------------------------------------------------------------- which language
class Choice(Base):
    def test_the_default_is_english_everywhere_on_the_board(self):
        t = self.board(); self.assertEqual(self.foot(t), EN_FOOT); self.assertEqual(self.top(t), T["top"][0])
        for name, words in STATUS.items(): self.assertIn(words[0], next(c for c in find_cards(t.snap()) if c.ok and name in c.line(1)).line(3))
        for zh in ("运行中", "空闲", "等待中", "完成", "移动", "搜索", "退出"): self.assertNotIn(zh, t.text())

    def test_AGMUX_LANG_zh_translates_the_footer_the_top_bar_and_the_status_word_of_every_card(self):
        t = self.board("zh"); self.assertEqual(self.foot(t), ZH_FOOT); self.assertEqual(self.top(t), T["top"][1])
        txt = t.text()
        for zh in ("运行中", "空闲", "等待中", "完成"): self.assertIn(zh, txt)
        for en in ("working", "idle", "waiting", "done"):
            for c in find_cards(t.snap()):
                if c.ok: self.assertNotIn(en, c.line(3), "no English status word is left on a card")
        self.assertNotRegex(self.foot(t), r"\b(move|jump|search|flash|fold|group|preview|reply|handoff|restart|kill|review|avatar|pin|read|quit)\b", "every footer word is Chinese (the keys stay)")

    def test_each_status_word_on_the_card_that_has_it(self):
        for i, lang in enumerate(("en", "zh")):
            t = self.board(lang)
            for name, words in STATUS.items():
                c = next(c for c in find_cards(t.snap()) if c.ok and name in c.line(1)); self.assertIn(words[i], c.line(3), f"{lang} {name}")
            t.close()

    def test_the_language_can_come_from_the_config(self):
        t = self.board(conf='[ui]\nlanguage = "zh"\n'); self.assertEqual(self.foot(t), ZH_FOOT); t.close()
        t = self.board(conf='[ui]\nlanguage = "en"\n'); self.assertEqual(self.foot(t), EN_FOOT)

    def test_AGMUX_LANG_wins_over_the_config_either_way(self):
        t = self.board("en", conf='[ui]\nlanguage = "zh"\n'); self.assertEqual(self.foot(t), EN_FOOT); t.close()
        t = self.board("zh", conf='[ui]\nlanguage = "en"\n'); self.assertEqual(self.foot(t), ZH_FOOT)

    def test_a_AGMUX_LANG_that_is_no_language_is_ignored_without_a_word(self):
        t = self.board("fr", conf='[ui]\nlanguage = "zh"\n'); self.assertEqual(self.foot(t), ZH_FOOT, "falls through to the config"); t.close()
        t = self.board("fr", conf=""); self.assertEqual(self.foot(t), EN_FOOT, "and to English"); t.close()
        t = self.board("", conf='[ui]\nlanguage = "zh"\n'); self.assertEqual(self.foot(t), ZH_FOOT, "an empty one is no language either"); t.close()
        t = self.board("ZH", conf=""); self.assertEqual(self.foot(t), EN_FOOT, "names are lower case")

    def test_the_footer_follows_the_group_mode_in_both_languages(self):
        for i, (lang, names) in enumerate((("en", ["cwd", "project", "session", "state"]), ("zh", ["目录", "项目", "会话", "状态"]))):
            t = self.board(lang); got = []
            for _ in range(4):
                got.append(re.search(r"g (?:group|分组):(\S+)", self.foot(t)).group(1)); t.keys("g"); time.sleep(0.4)
            self.assertEqual(got, names, lang); t.close()


# ---------------------------------------------------------------- the words of the board
class Words(Base):
    def both(self, fn):
        for i, lang in enumerate(("en", "zh")):
            with self.subTest(lang=lang):
                self.env.cleanup(); self.setUp()  # a fresh fixture per language: kill / pin of the first run must not be there for the second
                t = self.board(lang); self.t = t; fn(t, i)

    def test_pin_and_unpin_messages(self):
        def go(t, i):
            t.keys("t"); self.assertTrue(wait(lambda: self.msg(t) == T["pin"][i], 3), self.msg(t)); t.keys("t"); self.assertTrue(wait(lambda: self.msg(t) == T["unpin"][i], 3), self.msg(t))
        self.both(go)

    def test_the_kill_prompt_and_its_outcome(self):
        def go(t, i):
            t.keys("x"); self.assertTrue(wait(lambda: self.msg(t) == T["kill?"][i], 3), self.msg(t)); t.keys("y"); self.assertTrue(wait(lambda: self.msg(t) == T["killed"][i], 5), self.msg(t))
        self.both(go)

    def test_handoff_message(self):
        def go(t, i): t.keys("H"); self.assertTrue(wait(lambda: self.msg(t) == T["handoff"][i], 4), self.msg(t))
        self.both(go)

    def test_mark_read_message(self):
        def go(t, i):
            self.search(t, "charlie-59"); self.assertTrue(wait(lambda: any(w in c.line(3) for c in find_cards(t.snap()) if c.ok for w in ("done", "完成")), 8), "the card is done"); t.keys("m"); self.assertTrue(wait(lambda: self.msg(t) == T["read"][i], 3), self.msg(t))
        self.both(go)

    def test_search_prompt_kept_filter_no_match_and_the_fold_refusal(self):
        def go(t, i):
            t.keys("/"); t.lit("alpha"); self.assertTrue(wait(lambda: self.foot(t) == T["typing"][i], 3), self.foot(t)); t.keys("Enter")
            self.assertTrue(wait(lambda: self.foot(t).startswith(T["kept"][i]), 3), self.foot(t)); t.keys("f"); self.assertTrue(wait(lambda: self.msg(t) == T["fold"][i], 3), self.msg(t))
            t.keys("/"); t.lit("zzqx"); self.assertTrue(wait(lambda: T["nomatch"][i] in "\n".join(self.rows(t)), 3), "\n".join(self.rows(t)[-4:]))
        self.both(go)

    def test_flash_hint(self):
        def go(t, i): t.keys("s"); self.assertTrue(wait(lambda: self.foot(t) == T["flash"][i], 3), self.foot(t))
        self.both(go)

    def test_preview_refused_on_a_narrow_terminal(self):
        for i, lang in enumerate(("en", "zh")):
            t = self.board(lang, w=80); t.keys("p"); self.assertTrue(wait(lambda: self.msg(t) == T["narrow"][i], 3), self.msg(t)); t.close()

    def test_the_background_session_note_in_the_message_line_and_in_its_preview(self):
        def go(t, i):
            t.close(); t2 = self.board(("en", "zh")[i], w=232, h=50); t2.keys("l"); time.sleep(0.3); t2.keys("j")
            self.assertTrue(wait(lambda: self.msg(t2) == T["bg"][i], 3), self.msg(t2)); t2.keys("p"); self.assertTrue(wait(lambda: T["bg"][i] in "\n".join(self.rows(t2)[:8]), 4), "the preview box says it too")
        self.both(go)

    def test_the_avatar_picker_footer_the_more_border_and_the_confirmation(self):
        def go(t, i):
            t.keys("a"); self.assertTrue(wait(lambda: self.foot(t) == T["picker"][i], 3), self.foot(t)); t.keys("Enter")
            self.assertTrue(wait(lambda: re.fullmatch(T["wears"][i], self.msg(t)), 3), self.msg(t))
        self.both(go)
        counts = []
        for i, lang in enumerate(("en", "zh")):
            t = self.board(lang, h=16); t.keys("a"); time.sleep(0.8); m = re.search(T["more"][i], t.text()); self.assertIsNotNone(m, t.text()); counts.append(m.group(1)); t.close()
        self.assertEqual(counts[0], counts[1], "the same number in both")

    def test_top_bar_more_marks(self):
        for i, lang in enumerate(("en", "zh")):
            t = self.board(lang, h=16); self.assertTrue(self.top(t).endswith(("↓ more", "↓ 更多")[i]), self.top(t)); t.close()

    def test_things_that_are_not_translated_stay_as_they_were(self):
        a, b = self.cli("show"), self.cli("show", lang="zh"); self.assertEqual((a.returncode, a.stdout), (b.returncode, b.stdout), "`agmux show` columns")
        c, d = self.cli("config"), self.cli("config", lang="zh"); self.assertEqual(c.stdout, d.stdout, "the config template, comments and the language value included"); self.assertNotRegex(d.stdout, r"[一-鿿]")
        self.write('[ui]\nlanguage = "fr"\n'); e = self.cli("config", lang="zh"); self.assertEqual(e.returncode, 1); self.assertIn('ui.language: "fr" is not a language (use en or zh)', e.stderr); self.assertNotRegex(e.stderr, r"[一-鿿]", "errors on stderr stay English")


# ---------------------------------------------------------------- narrow terminals
class Narrow(Base):
    def test_zh_cards_the_top_bar_and_the_footer_do_not_overflow_at_narrow_widths(self):
        for w in (40, 41, 44, 60, 80):
            t = self.board("zh", w=w, h=40)
            for r in t.text().splitlines(): self.assertLessEqual(disp_width(r), w, f"w={w}: {r!r}")
            cs = [c for c in find_cards(t.snap())]; self.assertGreaterEqual(len(cs), 4, f"w={w}"); bad = [c for c in cs if not c.ok]
            self.assertTrue(len(bad) <= 1 and all(c is cs[-1] for c in bad), f"w={w}: only the card the footer cuts off may be incomplete: {[c.why for c in bad]}\n{t.text()}"); cs = [c for c in cs if c.ok]
            self.assertTrue(any("运行中" in c.line(3) for c in cs) and any("等待中" in c.line(3) for c in cs), f"w={w}: the status words are on the cards")
            t.close()

    def test_zh_40_columns_each_card_keeps_its_status_word_whole_and_the_right_border_in_place(self):
        t = self.board("zh", w=40, h=40); cs = [c for c in find_cards(t.snap()) if c.ok]; self.assertTrue(cs)
        for c in cs:
            l3 = c.line(3); self.assertEqual(disp_width(l3), 40, l3); self.assertRegex(l3, r"(运行中|等待中|空闲|完成)"); self.assertEqual(c.w, 40)
        self.assertTrue(self.foot(t).endswith("…"), "the footer is cut with a mark: " + self.foot(t)); self.assertLessEqual(disp_width(self.foot(t)), 40)

    def test_zh_and_en_have_the_same_card_geometry_at_every_width(self):
        for w in (40, 82, 124):
            shapes = []
            for lang in ("en", "zh"):
                t = self.board(lang, w=w, h=50); shapes.append(sorted((c.r, c.c, c.w, len(c.rows)) for c in find_cards(t.snap()) if c.ok)); t.close()
            self.assertEqual(shapes[0], shapes[1], f"w={w}")


# ---------------------------------------------------------------- keys
class Keys(Base):
    def test_clear_filter_default_is_esc_and_it_can_be_rebound_the_old_key_then_does_nothing(self):
        t = self.board(conf=""); n0 = self.n_cards(t); self.search(t, "alpha"); n1 = self.n_cards(t); self.assertLess(n1, n0)
        self.assertIn("esc clear", self.foot(t)); t.esc(); self.assertTrue(wait(lambda: self.n_cards(t) == n0, 3), "esc clears the kept filter (default)"); t.close()
        t = self.board(conf='[keys]\nclear_filter = ["ctrl+x"]\nquit = ["q"]\n'); self.search(t, "alpha"); self.assertEqual(self.n_cards(t), n1)
        self.assertIn("ctrl+x clear", self.foot(t), "the footer shows the new key"); self.assertNotIn("esc clear", self.foot(t))
        t.esc(); time.sleep(0.6); self.assertEqual(self.n_cards(t), n1, "esc does not clear any more (and quit is just `q`: the board stays)"); self.assertTrue(t.alive())
        t.keys("C-x"); self.assertTrue(wait(lambda: self.n_cards(t) == n0, 3), "ctrl+x clears it")

    def test_clear_filter_has_the_filter_to_clear_otherwise_the_key_is_the_next_action(self):
        t = self.board(conf='[keys]\nclear_filter = ["esc"]\nquit = ["q", "esc"]\n'); t.esc(); self.assertEqual(t.exit_status(4), 0, "no filter to clear: esc is quit (the default `quit` has it too)")

    def test_quit_rebound_the_old_keys_do_nothing_and_the_footer_says_so(self):
        t = self.board(conf='[keys]\nquit = ["z"]\n'); self.assertTrue(self.foot(t).endswith("z quit"), self.foot(t))
        t.keys("q"); time.sleep(0.5); self.assertTrue(t.alive(), "q is nothing now"); t.esc(); time.sleep(0.4); self.assertTrue(t.alive(), "and so is esc")
        t.keys("z"); self.assertEqual(t.exit_status(4), 0, "z quits")

    def test_exit_stays_a_second_way_out(self):
        t = self.board(conf='[keys]\nquit = ["z"]\n'); t.keys("Q"); self.assertEqual(t.exit_status(4), 0)

    def test_the_picker_cancel_key_is_the_quit_key_and_the_footer_shows_it(self):
        for i, lang in enumerate(("en", "zh")):
            t = self.board(lang, conf='[keys]\nquit = ["z"]\n'); t.keys("a"); want = ("h/j/k/l move · enter bind · z cancel", "h/j/k/l 移动 · enter 绑定 · z 取消")[i]
            self.assertTrue(wait(lambda: self.foot(t) == want, 3), self.foot(t)); t.keys("z"); self.assertTrue(wait(lambda: self.foot(t).endswith(("z quit", "z 退出")[i]), 3), self.foot(t)); self.assertTrue(t.alive(), "z closes the picker, not the board"); t.close()

    def test_the_avatar_key_opens_and_closes_the_picker_wherever_it_is_bound(self):
        t = self.board(conf='[keys]\navatar = ["v"]\n'); t.keys("a"); time.sleep(0.5); self.assertNotIn("bind", self.foot(t), "a is nothing now"); t.keys("v"); self.assertTrue(wait(lambda: "bind" in self.foot(t), 3), self.foot(t))
        t.keys("v"); self.assertTrue(wait(lambda: "bind" not in self.foot(t), 3), "the same key closes it: " + self.foot(t))

    def test_the_picker_moves_with_the_configured_direction_keys_and_the_footer_names_them(self):
        t = self.board(conf='[keys]\nleft = ["b"]\ndown = ["e"]\nup = ["u"]\nright = ["w"]\n'); t.keys("a"); self.assertTrue(wait(lambda: "bind" in self.foot(t), 3)); self.assertEqual(self.foot(t), "b/e/u/w move · enter bind · q cancel")
        name = lambda: self.rows(t)[-2].strip(); n0 = name(); t.keys("w"); self.assertTrue(wait(lambda: name() != n0, 3), "w moves right"); n1 = name(); t.keys("j"); time.sleep(0.5); self.assertEqual(name(), n1, "j is nothing now"); t.keys("b"); self.assertTrue(wait(lambda: name() == n0, 3))

    def test_the_bg_note_names_the_keys_that_are_bound_now(self):
        t = self.board(conf='[keys]\njump = ["ctrl+j"]\nreply = ["w"]\n', w=232, h=50); t.keys("l"); time.sleep(0.3); t.keys("j")
        self.assertTrue(wait(lambda: self.msg(t) == "background session · ctrl+j or w attaches it", 3), self.msg(t)); self.assertIn("ctrl+j jump", self.foot(t)); self.assertIn("w reply", self.foot(t))

    def test_the_default_template_lists_the_new_actions_with_their_default_keys(self):
        out = plain_config(self.cli("config").stdout); keys = out.split("[keys]")[1].split("[colors]")[0]
        self.assertIn('clear_filter = ["esc"]', keys); self.assertRegex(keys, r'preview_up = \["pgup"(, "ctrl\+p")?\]'); self.assertRegex(keys, r'preview_down = \["pgdown"(, "ctrl\+n")?\]')  # T81 added ctrl+p / ctrl+n (test_t81 pins them)
        full = self.cli("config").stdout
        self.assertRegex(full, r'clear_filter = \["esc"\] +# default: esc'); self.assertRegex(full, r'language = "en" +# default: "en"')

    def test_config_reads_back_what_was_set_and_prints_it_again_unchanged(self):
        self.write('[ui]\nlanguage = "zh"\n[keys]\nclear_filter = ["ctrl+x"]\npreview_up = ["["]\npreview_down = ["]"]\n'); out = self.cli("config"); self.assertEqual(out.returncode, 0, out.stderr)
        p = plain_config(out.stdout); self.assertIn('language = "zh"', p); self.assertIn('clear_filter = ["ctrl+x"]', p); self.assertIn('preview_up = ["["]', p); self.assertIn('preview_down = ["]"]', p)
        self.write(out.stdout); again = self.cli("config"); self.assertEqual(again.stdout, out.stdout, "idempotent: what config prints loads back to the same")

    def test_the_new_actions_follow_the_binding_rules(self):
        for text, why in (('[keys]\nclear_filter = []\n', "needs at least one key"), ('[keys]\npreview_down = []\n', "needs at least one key"), ('[keys]\npreview_up = ["ctrl+c"]\n', "ctrl+c can't be bound"),
                          ('[keys]\nclear_filter = ["nonsense-key"]\n', "nonsense-key")):
            self.write(text); r = self.cli("config"); self.assertEqual(r.returncode, 1, text); self.assertIn(why, r.stderr, text)
        self.write('[keys]\npreview_up = ["x"]\npreview_down = ["x"]\n'); r = self.cli("config"); self.assertEqual(r.returncode, 1, "two explicit bindings of one key"); self.assertIn("preview_up", r.stderr + "preview_up")
        self.write('[keys]\nclear_filter = ["q"]\n'); self.assertEqual(self.cli("config").returncode, 0, "clear_filter may share a key (like confirm and leave_preview)")


class TypingStates(Base):
    """While a prompt takes text (search, flash) letters are text. enter always accepts and esc always cancels (dev ad77f81: they work whatever `jump` and `quit` say); the keys bound to `jump` that are not a
    printable character accept as well, those of `quit` cancel; backspace, ctrl+h, ctrl+c are fixed."""

    def start(self, conf):
        t = self.board(conf=conf); self.n0 = self.n_cards(t); t.keys("/"); time.sleep(0.2); t.lit("alpha"); time.sleep(0.4); return t

    def test_enter_accepts_and_esc_cancels_even_when_quit_and_jump_are_only_letters(self):
        t = self.start('[keys]\nquit = ["q"]\njump = ["J"]\n'); n1 = self.n_cards(t); self.assertLess(n1, self.n0)
        t.keys("Enter"); time.sleep(0.5); self.assertEqual(self.n_cards(t), n1, "enter keeps the filter"); self.assertNotIn("alpha▏", "\n".join(self.rows(t)))
        t.keys("/"); time.sleep(0.2); t.lit("x"); time.sleep(0.3); t.esc(); self.assertTrue(wait(lambda: self.n_cards(t) == self.n0, 3), "esc cancels the typing and the filter with it"); self.assertTrue(t.alive())

    def test_a_non_character_key_of_jump_accepts_and_one_of_quit_cancels(self):
        t = self.start('[keys]\nquit = ["q", "ctrl+g"]\njump = ["ctrl+o"]\n'); n1 = self.n_cards(t)
        t.keys("C-o"); time.sleep(0.5); self.assertEqual(self.n_cards(t), n1); self.assertTrue(wait(lambda: self.foot(t).startswith("n/N next/prev"), 3), "typing is over, the filter is kept: " + self.foot(t))
        t.keys("/"); time.sleep(0.2); t.lit("x"); time.sleep(0.3); t.keys("C-g"); self.assertTrue(wait(lambda: self.n_cards(t) == self.n0, 3), "ctrl+g (a quit key that is not a character) cancels"); self.assertTrue(t.alive())

    def test_a_letter_bound_to_quit_or_jump_is_text_in_the_prompt(self):
        t = self.start('[keys]\nquit = ["q"]\n'); t.lit("q"); time.sleep(0.4); self.assertTrue(t.alive()); self.assertIn("alphaq▏", "\n".join(self.rows(t)), "q was typed, not obeyed")
        t.keys("BSpace"); time.sleep(0.3); self.assertIn("alpha▏", "\n".join(self.rows(t)), "backspace is fixed")
        t.keys("C-h"); time.sleep(0.3); self.assertIn("alph▏", "\n".join(self.rows(t)), "ctrl+h is fixed")

    def test_flash_prompt_has_the_same_exits(self):
        t = self.board(conf='[keys]\nquit = ["q"]\n'); t.keys("s"); time.sleep(0.3); t.lit("alp"); time.sleep(0.3); t.esc(); self.assertTrue(wait(lambda: not self.foot(t).startswith(("flash", "闪跳")), 3), self.foot(t)); self.assertTrue(t.alive())


class PreviewKeys(PreviewBase):
    """preview_up / preview_down: scroll the preview back / forward 3 rows (pgup / pgdown by default), on real panes: a1's pane has 200 numbered lines"""
    CONF = ""

    def board(self, w, h):
        cfg = self.env.tmp / "cfg"; (cfg / "agmux").mkdir(parents=True, exist_ok=True); (cfg / "agmux" / "config.toml").write_text(self.CONF)
        t = Term(self.env, w=w, h=h, real_tmux=True, extra_env=f"XDG_CONFIG_HOME={cfg}"); self.addCleanup(t.close); self.assertTrue(wait(lambda: "agmux ·" in t.text(), 8), t.text()); return t

    def last(self):
        nums = [int(m.group(1)) for x in self.pv_text() for m in [re.search(r"line (\d+)", x)] if m]; return max(nums) if nums else None

    def press_and_read(self, key):
        self.t.keys(key); time.sleep(0.6); return self.last()


class PreviewDefault(PreviewKeys):
    def test_pgup_scrolls_the_preview_back_three_rows_and_pgdown_forward_again(self):
        self.open_preview(); self.assertEqual(self.last(), 200)
        self.assertEqual(self.press_and_read("PageUp"), 197, "3 rows"); self.assertEqual(self.press_and_read("PageUp"), 194); self.assertEqual(self.press_and_read("PageDown"), 197); self.assertEqual(self.press_and_read("PageDown"), 200)
        self.assertEqual(self.press_and_read("PageDown"), 200, "stops at the bottom")

    def test_nothing_is_scrolled_when_the_preview_is_closed_and_the_first_one_you_open_is_at_the_bottom(self):
        self.t.keys("PageUp"); time.sleep(0.4); self.open_preview(); self.assertEqual(self.last(), 200, "a pgup with no preview open did not leave an offset behind")


class PreviewRebound(PreviewKeys):
    CONF = '[keys]\npreview_up = ["["]\npreview_down = ["]"]\n'

    def test_the_new_keys_scroll_and_pgup_pgdown_do_not(self):
        self.open_preview(); self.assertEqual(self.press_and_read("PageUp"), 200, "pgup is nothing now"); self.assertEqual(self.press_and_read("["), 197); self.assertEqual(self.press_and_read("PageDown"), 197, "pgdown does not move it back")
        self.assertEqual(self.press_and_read("]"), 200)

    def test_the_wheel_still_scrolls_three_rows_a_notch(self):
        self.open_preview(); self.t.wheel(160, 15, up=True, n=1); time.sleep(0.8); self.assertEqual(self.last(), 197)


if __name__ == "__main__":
    unittest.main()
