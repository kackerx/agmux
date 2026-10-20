package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kackerx/agmux/internal/store"
)

func asst(m string) string { return `{"type":"assistant","message":{"model":"` + m + `"}}` }
func bigAttachment() string {
	return `{"type":"attachment","content":"` + strings.Repeat("A", 200<<10) + `"}`
}
func write(t *testing.T, p string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
func newP(t *testing.T, raw *[]store.Raw) *store.Poller {
	return &store.Poller{Sources: []store.Source{store.SourceFunc(func() ([]store.Raw, error) { return *raw, nil })}, StateDir: t.TempDir()}
}
func model(p *store.Poller, id string) string {
	for _, s := range p.Sessions() {
		if s.ID == id {
			return s.Model
		}
	}
	return "<missing>"
}

func TestExtModelCache(t *testing.T) {
	d := t.TempDir()
	la, lb := filepath.Join(d, "a.jsonl"), filepath.Join(d, "b.jsonl")
	raw := []store.Raw{{ID: "a", Transcript: la}, {ID: "b", Transcript: lb}}
	p := newP(t, &raw)
	write(t, la, `{"type":"user"}`, asst("m-a1"), `{"type":"user"}`)
	write(t, lb, bigAttachment()) // b never had a model
	p.Refresh()
	if model(p, "a") != "m-a1" || model(p, "b") != "" {
		t.Fatalf("poll1: a=%q b=%q", model(p, "a"), model(p, "b"))
	}
	write(t, la, asst("m-a1"), bigAttachment(), bigAttachment()) // assistant scrolled out of the 64KB tail
	p.Refresh()
	if model(p, "a") != "m-a1" || model(p, "b") != "" {
		t.Fatalf("poll2 (cache): a=%q b=%q (b must not borrow a's model)", model(p, "a"), model(p, "b"))
	}
	write(t, la, asst("m-a1"), asst("m-a2"), `{"type":"user"}`) // model switched mid-session
	p.Refresh()
	if model(p, "a") != "m-a2" {
		t.Fatalf("poll3: new model not picked up: %q", model(p, "a"))
	}
	os.Remove(la) // transcript vanished: keep last known
	p.Refresh()
	if model(p, "a") != "m-a2" {
		t.Fatalf("poll4 (missing file): %q", model(p, "a"))
	}
	write(t, lb, asst("m-b"))
	p.Refresh()
	if model(p, "b") != "m-b" {
		t.Fatalf("poll5: b=%q", model(p, "b"))
	}
}

func TestExtBoundary(t *testing.T) {
	const win = 64 << 10
	for _, c := range []struct {
		back int // assistant line starts `back` bytes before EOF
		want string
	}{{win, "edge"}, {win - 1, "edge"}, {win + 1, ""}} {
		d := t.TempDir()
		lg := filepath.Join(d, "s.jsonl")
		a := asst("edge")
		// file = junk lines + assistant line + pad line, sized so the assistant line starts exactly c.back bytes before EOF
		tail := c.back - len(a) - 1 // bytes of the pad line incl. its newline
		pad := `{"type":"user","pad":"` + strings.Repeat("x", tail-1-len(`{"type":"user","pad":""}`)) + `"}`
		write(t, lg, `{"type":"user"}`, `{"type":"user"}`, a, pad)
		fi, _ := os.Stat(lg)
		raw := []store.Raw{{ID: "x", Transcript: lg}}
		p := newP(t, &raw)
		p.Refresh()
		off := fi.Size() - int64(len(pad)+1) - int64(len(a)+1)
		t.Logf("back=%d: assistant starts %d bytes before EOF (size %d, off %d)", c.back, fi.Size()-off, fi.Size(), off)
		if got := model(p, "x"); got != c.want {
			t.Errorf("assistant line starting %d bytes before EOF: got %q want %q", c.back, got, c.want)
		}
	}
}

func TestExtConcurrentRefresh(t *testing.T) {
	d := t.TempDir()
	lg := filepath.Join(d, "s.jsonl")
	write(t, lg, asst("m"))
	raw := []store.Raw{{ID: "a", Transcript: lg}, {ID: "b", Transcript: lg}}
	p := newP(t, &raw)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				p.Refresh()
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = p.Sessions()
				_ = p.Err()
			}
		}()
	}
	wg.Wait()
	if model(p, "a") != "m" {
		t.Fatal(model(p, "a"))
	}
}
