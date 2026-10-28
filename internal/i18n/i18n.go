// Package i18n holds the words the board and `agmux setup` show a user, in English and Chinese.
// A text is a Printf format under a key; the key is the same in both tables.
package i18n

import "fmt"

// Langs are the languages there are tables for; the first is the default and the fallback.
var Langs = []string{"en", "zh"}

var lang = "en"

// Set picks the language T speaks; anything but a language of Langs is the default. Call it once at
// start, before anything draws.
func Set(l string) {
	lang = "en"
	if Valid(l) {
		lang = l
	}
}

// Valid reports whether l names a language.
func Valid(l string) bool {
	for _, x := range Langs {
		if x == l {
			return true
		}
	}
	return false
}

// Resolve is the language in use: $AGMUX_LANG (env) if it names one, else the config's, else English.
func Resolve(cfg, env string) string {
	switch {
	case Valid(env):
		return env
	case Valid(cfg):
		return cfg
	}
	return Langs[0]
}

// T is the text under key in the current language, formatted with args if there are any. A key the
// language lacks falls back to English; one neither has comes back as itself.
func T(key string, args ...any) string {
	s, ok := tables[lang][key]
	if !ok {
		if s, ok = tables["en"][key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

var tables = map[string]map[string]string{"en": en, "zh": zh}
