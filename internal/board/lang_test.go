package board

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kackerx/agmux/internal/config"
	"github.com/kackerx/agmux/internal/i18n"
)

// zh switches the words to Chinese for the test; the footer is built from them at start, so it is
// built again, as Run does after the config is read.
func zh(t *testing.T) {
	t.Helper()
	i18n.Set("zh")
	footerKeys = footerFor(config.Default())
	t.Cleanup(func() {
		i18n.Set("en")
		footerKeys = footerFor(config.Default())
	})
}

func footerOf(v string) string { l := strings.Split(ansi.Strip(v), "\n"); return l[len(l)-1] }

// T74: in zh the top bar, footer, message line, kill prompt, picker footer and the state groups speak
// Chinese, and no English footer word is left.
func TestChineseBoard(t *testing.T) {
	zh(t)
	m := wide(&fakeRunner{}, 232, 50)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "3 个会话") || strings.Contains(v, "sessions") {
		t.Errorf("top bar:\n%s", strings.SplitN(v, "\n", 2)[0])
	}
	for _, w := range []string{"enter 跳转", "/ 搜索", "s 闪跳", "f 折叠", "g 分组:目录", "p 预览", "q 退出"} {
		if !strings.Contains(footerOf(v), w) {
			t.Errorf("footer lacks %q: %s", w, footerOf(v))
		}
	}
	for _, w := range []string{"jump", "search", "fold", "quit"} {
		if strings.Contains(footerOf(v), w) {
			t.Errorf("footer still says %q: %s", w, footerOf(v))
		}
	}
	if v := ansi.Strip(press1(t, wide(&fakeRunner{}, 99, 40), "p").View()); !strings.Contains(v, "预览需要至少 100 列宽的终端") {
		t.Errorf("message line:\n%s", v)
	}
	if v := ansi.Strip(press1(t, m, "x").View()); !strings.Contains(v, "终止 one？按 y 确认，其它键取消") {
		t.Errorf("kill prompt:\n%s", v)
	}
	if l := footerOf(press1(t, m, "a").View()); !strings.Contains(l, "移动") || !strings.Contains(l, "绑定") || !strings.Contains(l, "取消") {
		t.Errorf("picker footer: %s", l)
	}
	if v := ansi.Strip(press1(t, m, "g", "g", "g").View()); !strings.Contains(v, "空闲 (3)") || !strings.Contains(v, "分组:状态") {
		t.Errorf("state grouping:\n%s", v)
	}
	if v := ansi.Strip(press1(t, m, "g").View()); !strings.Contains(v, "(无项目) (3)") { // the fixtures have no project
		t.Errorf("project grouping:\n%s", v)
	}
	if v := ansi.Strip(press1(t, m, "g", "g").View()); !strings.Contains(v, "(无窗格) (3)") { // the fixtures have no tmux session
		t.Errorf("session grouping:\n%s", v)
	}
	if v := ansi.Strip(press1(t, m, "j").View()); !strings.Contains(v, "没有 tmux 窗格") { // a2 has no pane
		t.Errorf("no-pane note:\n%s", v)
	}
	if v := ansi.Strip(press1(t, m, "/").View()); !strings.Contains(v, "enter 保留过滤 · esc 清除") {
		t.Errorf("search prompt: %s", footerOf(v))
	}
}

func press1(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	m, _ = press(t, m, keys...)
	return m
}

// T74: the same session in en reads as it always did (nothing is translated by accident).
func TestEnglishBoardIsDefault(t *testing.T) {
	v := ansi.Strip(wide(&fakeRunner{}, 232, 50).View())
	if !strings.Contains(v, "3 sessions") || !strings.Contains(footerOf(v), "enter jump · / search") {
		t.Errorf("en board:\n%s", v)
	}
}

// T74: a background session names the keys the config gives jump and reply, not enter and i.
func TestBgNoteNamesTheConfiguredKeys(t *testing.T) {
	c, err := config.Parse("cfg", []byte("[keys]\njump = [\"o\"]\nreply = [\"ctrl+r\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	bg := append(staticSrc(nil), sessions...)
	bg[1].Background = true
	m := New(bg, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	m.cfg, m.keymap = c, c.KeyMap()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	m = tm.(Model)
	useConfig(m.cfg)
	t.Cleanup(func() { useConfig(config.Default()) })
	if v := ansi.Strip(press1(t, m, "j").View()); !strings.Contains(v, "background session · o or ctrl+r attaches it") {
		t.Errorf("bg note:\n%s", v)
	}
}

// T74: an action whose default key another action took has none to name: the note names the other.
func TestBgNoteLeavesOutAnUnboundKey(t *testing.T) {
	c, err := config.Parse("cfg", []byte("[keys]\nsearch = [\"i\"]\n")) // reply loses its i
	if err != nil {
		t.Fatal(err)
	}
	bg := append(staticSrc(nil), sessions...)
	bg[1].Background = true
	m := New(bg, &fakeRunner{})
	m.getenv = func(string) string { return "" }
	m.cfg, m.keymap = c, c.KeyMap()
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
	useConfig(c)
	t.Cleanup(func() { useConfig(config.Default()) })
	if v := ansi.Strip(press1(t, tm.(Model), "j").View()); !strings.Contains(v, "background session · enter attaches it") {
		t.Errorf("bg note:\n%s", v)
	}
}

// T74: the keys that were written into the code are in [keys] now. A kept filter is dropped by
// clear_filter only; esc no longer does once it is rebound.
func TestClearFilterIsConfigurable(t *testing.T) {
	if _, quit := press(t, withConfig(t, ""), "esc"); !quit {
		t.Error("esc with no filter kept is quit's key")
	}
	m := withConfig(t, "[keys]\nclear_filter = [\"ctrl+x\"]\n")
	m = typeKeys(t, press1(t, m, "/"), "thr")
	m = press1(t, m, "enter")
	if m.query != "thr" {
		t.Fatalf("query %q", m.query)
	}
	if m = press1(t, m, "esc"); m.query != "thr" {
		t.Error("esc no longer clears the kept filter")
	}
	if !strings.Contains(footerOf(m.View()), "ctrl+x clear") {
		t.Errorf("hint: %s", footerOf(m.View()))
	}
	if m = press1(t, m, "ctrl+x"); m.query != "" {
		t.Errorf("ctrl+x: query %q", m.query)
	}
}

// T74: while the search box takes text, quit's letters are text and only its other keys cancel; jump's
// non-character keys keep the filter.
func TestSearchPromptKeysFollowTheConfig(t *testing.T) {
	m := withConfig(t, "[keys]\nquit = [\"q\", \"ctrl+q\"]\njump = [\"l\", \"enter\"]\n")
	m = typeKeys(t, press1(t, m, "/"), "ql") // text, both
	if m.query != "ql" || !m.searching {
		t.Fatalf("q and l are text: query %q searching %v", m.query, m.searching)
	}
	if !strings.Contains(footerOf(m.View()), "enter keep the filter · ctrl+q clear it") {
		t.Errorf("prompt: %s", footerOf(m.View()))
	}
	if m = press1(t, m, "esc"); !m.searching || m.query != "ql" {
		t.Error("esc is not a cancel key any more")
	}
	if m = press1(t, m, "ctrl+q"); m.searching || m.query != "" {
		t.Errorf("ctrl+q cancels: searching %v query %q", m.searching, m.query)
	}
	m = typeKeys(t, press1(t, m, "/"), "thr")
	if m = press1(t, m, "enter"); m.searching || m.query != "thr" {
		t.Errorf("enter keeps: searching %v query %q", m.searching, m.query)
	}
	// jump moved off enter: enter is not "keep" any more, the new key is
	m = withConfig(t, "[keys]\njump = [\"ctrl+x\"]\n")
	m = typeKeys(t, press1(t, m, "/"), "thr")
	if m = press1(t, m, "enter"); !m.searching {
		t.Error("enter is not jump's key: the prompt goes on")
	}
	if m = press1(t, m, "ctrl+x"); m.searching || m.query != "thr" {
		t.Errorf("ctrl+x keeps: searching %v query %q", m.searching, m.query)
	}
	// a quit with only characters leaves nothing to name as the cancel key
	m = typeKeys(t, press1(t, withConfig(t, "[keys]\nquit = [\"q\"]\n"), "/"), "a")
	if f := strings.TrimSpace(footerOf(m.View())); f != "enter keep the filter" {
		t.Errorf("no cancel key: %q", f)
	}
}

// T74: flash ends on quit's non-character keys; its letters are labels / pattern.
func TestFlashEndsOnTheQuitKey(t *testing.T) {
	m := withConfig(t, "[keys]\nquit = [\"q\", \"ctrl+q\"]\n")
	if m = press1(t, m, "s"); m.mode != flashing {
		t.Fatal("s starts flash")
	}
	if m = press1(t, m, "esc"); m.mode != flashing {
		t.Error("esc is not bound to quit any more: flash goes on")
	}
	if !strings.Contains(footerOf(m.View()), "ctrl+q") {
		t.Errorf("flash footer: %s", footerOf(m.View()))
	}
	if m = press1(t, m, "ctrl+q"); m.mode != browsing {
		t.Error("ctrl+q ends flash")
	}
}

// T74: locked out of a dead pane, quit's non-character keys give the keys back, and so does leave_preview.
func TestLockedOutLeavesOnTheQuitKey(t *testing.T) {
	c, err := config.Parse("cfg", []byte("[keys]\nquit = [\"q\", \"ctrl+q\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ctrl+q", "tab"} {
		src := &mutSrc{ss: sessions}
		m := New(src, &fakeRunner{})
		m.getenv = func(string) string { return "" }
		m.cfg, m.keymap = c, c.KeyMap()
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 232, Height: 50})
		m = press1(t, tm.(Model), "i")
		src.ss = sessions[1:]
		if m = press1(t, m, "esc"); !m.pvFocus {
			t.Errorf("%s: esc is not a quit key here: still locked out", k)
		}
		if m = press1(t, m, k); m.pvFocus {
			t.Errorf("%s leaves the locked-out state", k)
		}
	}
}

// T74: pgup / pgdown scroll the preview like the wheel (three lines), up to what was captured; the
// keys are rebindable.
func TestPreviewScrollKeys(t *testing.T) {
	var hist strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&hist, "line %d\n", i)
	}
	for _, c := range []struct{ toml, up, down string }{{"", "pgup", "pgdown"}, {"[keys]\npreview_up = [\"ctrl+x\"]\npreview_down = [\"ctrl+q\"]\n", "ctrl+x", "ctrl+q"}} {
		m := press1(t, withConfig(t, c.toml), "p")
		tm, _ := m.Update(previewMsg{pane: "%1", text: hist.String()})
		m = tm.(Model)
		m.capturing = false
		if m = press1(t, m, c.up); m.scroll != 3 {
			t.Fatalf("%q: up: scroll %d", c.up, m.scroll)
		}
		m.capturing = false
		if m = press1(t, m, c.up); m.scroll != 6 {
			t.Fatalf("%q: up again: scroll %d", c.up, m.scroll)
		}
		m.capturing = false
		if m = press1(t, m, c.down); m.scroll != 3 {
			t.Fatalf("%q: down: scroll %d", c.down, m.scroll)
		}
	}
	m := press1(t, withConfig(t, ""), "pgup") // preview closed: nothing to scroll
	if m.scroll != 0 {
		t.Errorf("no preview, scroll %d", m.scroll)
	}
}
