package xdg

import "testing"

func TestStateDir(t *testing.T) {
	t.Setenv("HOME", "/h")
	for _, c := range []struct{ xdg, want string }{
		{"/x", "/x/agmux"},
		{"rel/x", "/h/.local/state/agmux"}, // relative: ignored
		{"", "/h/.local/state/agmux"},
	} {
		t.Setenv("XDG_STATE_HOME", c.xdg)
		if got, err := StateDir(); err != nil || got != c.want {
			t.Errorf("XDG_STATE_HOME=%q: %q %v, want %q", c.xdg, got, err, c.want)
		}
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := StateDir(); err == nil {
		t.Error("no HOME and no XDG_STATE_HOME should error, not fall back to the cwd")
	}
}

func TestCacheDir(t *testing.T) {
	t.Setenv("HOME", "/h")
	for _, c := range []struct{ xdg, want string }{
		{"/x", "/x/agmux"},
		{"rel/x", "/h/.cache/agmux"}, // relative: ignored
		{"", "/h/.cache/agmux"},
	} {
		t.Setenv("XDG_CACHE_HOME", c.xdg)
		if got, err := CacheDir(); err != nil || got != c.want {
			t.Errorf("XDG_CACHE_HOME=%q: %q %v, want %q", c.xdg, got, err, c.want)
		}
	}
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := CacheDir(); err == nil {
		t.Error("no HOME and no XDG_CACHE_HOME should error, not fall back to the cwd")
	}
}
