package core

import (
	"strings"
	"unicode/utf8"
)

// 网页列表里的长正文只有前 16 KB，超出部分的搜索由节点在全文里完成。

type SearchHit struct {
	ID      string `json:"id"`
	Line    int    `json:"line"`    // 命中位置所在的行（从 1 开始）
	Snippet string `json:"snippet"` // 命中位置前后的一小段文字
}

// SearchFull 在被截断的长正文里查找同时包含所有关键词（不区分大小写）的内容。
func (a *App) SearchFull(query string) []SearchHit {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return []SearchHit{}
	}
	out := []SearchHit{}
	for _, it := range a.store.List() {
		if it.Kind != "text" || len(it.Text) <= previewBytes {
			continue // 短内容网页自己就能搜到
		}
		lower := strings.ToLower(it.Text)
		// 定位到“最晚才出现”的那个关键词：它往往就是在摘要之外、需要全文搜索才能找到的那个
		pos := -1
		for _, t := range terms {
			i := strings.Index(lower, t)
			if i < 0 {
				pos = -2
				break
			}
			if i > pos {
				pos = i
			}
		}
		if pos < 0 {
			continue
		}
		// 大小写转换可能改变字节长度（少数字符），这时从转换后的文本里截取片段
		src := it.Text
		if len(lower) != len(src) {
			src = lower
		}
		out = append(out, SearchHit{
			ID:      it.ID,
			Line:    strings.Count(src[:pos], "\n") + 1,
			Snippet: snippet(src, pos, 60),
		})
	}
	return out
}

// snippet 取 pos 前后约 width 字节的文字，不切断多字节字符，换行显示为 ⏎。
func snippet(s string, pos, width int) string {
	start, end := pos-width, pos+width*2
	if start < 0 {
		start = 0
	}
	if end > len(s) {
		end = len(s)
	}
	for start > 0 && !utf8.RuneStart(s[start]) {
		start--
	}
	for end < len(s) && !utf8.RuneStart(s[end]) {
		end++
	}
	out := strings.ReplaceAll(s[start:end], "\n", " ⏎ ")
	if start > 0 {
		out = "…" + out
	}
	if end < len(s) {
		out += "…"
	}
	return out
}
