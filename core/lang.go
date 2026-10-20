package core

import (
	"os"
	"strings"
	"sync/atomic"
)

// 终端输出和日志的语言（网页界面的语言另见 core/web/i18n.js）。

var english atomic.Bool

// SetLanguage 设置终端和日志的语言："en" 为英文，其他为中文。
func SetLanguage(lang string) { english.Store(lang == "en") }

// L 按当前语言选择文字。
func L(zh, en string) string {
	if english.Load() {
		return en
	}
	return zh
}

// SystemLanguage 根据 LC_ALL / LC_MESSAGES / LANG 判断系统语言；都没有时按平台判断（见 lang_*.go）。
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
