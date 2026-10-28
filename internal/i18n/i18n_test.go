package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestTablesHaveTheSameKeys(t *testing.T) {
	for k := range en {
		if _, ok := zh[k]; !ok {
			t.Errorf("zh lacks %q", k)
		}
	}
	for k := range zh {
		if _, ok := en[k]; !ok {
			t.Errorf("en lacks %q (only zh has it)", k)
		}
	}
}

// A zh text takes the same arguments as its en text: same verbs, same count (a reordering is %[2]s).
func TestTextsTakeTheSameArguments(t *testing.T) {
	verbs := regexp.MustCompile(`%(?:\[\d+\])?[sdqv]`)
	letters := func(s string) []string {
		var out []string
		for _, v := range verbs.FindAllString(s, -1) {
			out = append(out, v[len(v)-1:])
		}
		slices.Sort(out)
		return out
	}
	for k, e := range en {
		if !slices.Equal(letters(e), letters(zh[k])) {
			t.Errorf("%q: en %q and zh %q take different arguments", k, e, zh[k])
		}
	}
}

func TestMissingZhFallsBackToEnglish(t *testing.T) {
	en["test.only_en"] = "only %s"
	defer delete(en, "test.only_en")
	defer Set("en")
	Set("zh")
	if got := T("test.only_en", "x"); got != "only x" {
		t.Errorf("T of a key zh lacks = %q, want the English text", got)
	}
	if got := T("no.such.key"); got != "no.such.key" {
		t.Errorf("T of an unknown key = %q, want the key", got)
	}
}

func TestSetAndResolve(t *testing.T) {
	defer Set("en")
	Set("zh")
	if got := T("status.idle"); got != zh["status.idle"] || got == en["status.idle"] {
		t.Errorf("zh: T(status.idle) = %q", got)
	}
	Set("fr") // not a language: the default
	if got := T("status.idle"); got != "idle" {
		t.Errorf("Set(fr): T(status.idle) = %q, want the English text", got)
	}
	for _, c := range []struct{ cfg, env, want string }{
		{"", "", "en"}, {"zh", "", "zh"}, {"en", "zh", "zh"}, {"zh", "en", "en"}, {"zh", "fr", "zh"}, {"xx", "yy", "en"},
	} {
		if got := Resolve(c.cfg, c.env); got != c.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c.cfg, c.env, got, c.want)
		}
	}
}

// Every T("key") in the program is a key of the tables, and every key is asked for somewhere: a typo
// in either place would show the key itself (or nothing) on the board.
func TestKeysInTheSourceAndTheTablesMatch(t *testing.T) {
	asked := regexp.MustCompile(`\bT\("([a-z_.]*[a-z_])"`)
	var src strings.Builder
	used := map[string]bool{}
	err := filepath.Walk("../..", func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "internal/i18n/") {
			return err
		}
		b, err := os.ReadFile(p)
		src.Write(b)
		for _, m := range asked.FindAllSubmatch(b, -1) {
			used[string(m[1])] = true
			if _, ok := en[string(m[1])]; !ok {
				t.Errorf("%s asks for %q, which the tables lack", p, m[1])
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for k := range en {
		// status.* and mode.* are built from the state / grouping name
		if !used[k] && !strings.HasPrefix(k, "status.") && !strings.HasPrefix(k, "mode.") && !strings.Contains(src.String(), `"`+k+`"`) {
			t.Errorf("nothing asks for %q", k)
		}
	}
}
