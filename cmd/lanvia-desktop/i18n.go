package main

import "strings"

// 托盘菜单的中英文。界面语言在设置里选，留空时跟随系统。

var trayEN = map[string]string{
	"还没有内容":          "Nothing yet",
	"收藏":             "Favorites",
	"已复制":            "Copied",
	"已发送":            "Sent",
	"主动复制 · 复制后自动发送": "Auto copy · send what you copy",
	"主动粘贴 · 收到后放进剪贴板": "Auto paste · put received text on the clipboard",
	"发送当前剪贴板":         "Send current clipboard",
	"开机时启动":           "Launch at login",
	"打开快捷面板":          "Open quick panel",
	"打开主窗口…":          "Open main window…",
	"配对码  ":           "Pairing code  ",
	"退出 Lanvia":       "Quit Lanvia",
	"Lanvia 预览":       "Lanvia Preview",
	"（空白）":            "(blank)",
	"Lanvia 无法启动":     "Lanvia can't start",
	"这台电脑不支持读取剪贴板":    "This computer can't read the clipboard",
	"剪贴板里是文件或敏感内容，不会发送": "File or sensitive content, not sent",
	"读取剪贴板失败":           "Couldn't read the clipboard",
	"剪贴板里没有文字":          "No text on the clipboard",
	"内容太长":              "Too long",
	"复制":                "Copy",
	"在文件夹中显示":           "Show in folder",
	"收到 %d 条新内容":        "%d new items",
	"最新来自 %s":           "Latest from %s",
	"来自 %s":             "From %s",
	"已写入剪贴板":            "Copied to your clipboard",
	"暂停同步":              "Pause syncing",
	"15 分钟":             "15 minutes",
	"1 小时":              "1 hour",
	"直到手动恢复":            "Until I resume",
	"恢复同步（已暂停）":         "Resume syncing (paused)",
	"恢复同步（暂停到 %s）":      "Resume syncing (paused until %s)",
	"更新到 %s":            "Update to %s",
	"正在更新…":             "Updating…",
	"检查更新":              "Check for updates",
	"已是最新版本":            "You're up to date",
	"无法检查更新":            "Couldn't check for updates",
	"有新版本 %s":           "Lanvia %s is available",
	"点击托盘菜单里的“更新到 %s”即可安装": "Choose “Update to %s” in the tray menu to install",
	"更新失败：%s": "Update failed: %s",
}

func (d *desktop) lang() string {
	if l := d.node.Settings().Lang; l != "" {
		return l
	}
	return sysLang()
}

// tr 翻译托盘里的文字；中文是源文本。
func (d *desktop) tr(s string) string {
	if d.lang() != "en" {
		return s
	}
	if v, ok := trayEN[s]; ok {
		return v
	}
	return s
}

func isZh(locale string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "zh")
}
