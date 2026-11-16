package core

import (
	"os"
	"strings"
	"sync/atomic"
)

// Language for terminal output and logs (the web UI language is in core/web/i18n.js).

var english atomic.Bool

// SetLanguage sets the language of the terminal and the logs: "en" for English, anything else for Chinese.
func SetLanguage(lang string) { english.Store(lang == "en") }

// L picks the text for the current language.
func L(zh, en string) string {
	if english.Load() {
		return en
	}
	return zh
}

// SystemLanguage derives the system language from LC_ALL / LC_MESSAGES / LANG; without any of them it falls back to the platform (see lang_*.go).
func SystemLanguage() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := strings.ToLower(os.Getenv(k)); v != "" && v != "c" && v != "posix" {
			if strings.HasPrefix(v, "zh") {
				return "zh"
			}
			return "en"
		}
	}
	return platformLanguage()
}
