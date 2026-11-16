package core

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// The lower-cased version of a long text is cached, so that it is not computed again for every search (invalidated by content ID and revision).
type lowerCache struct {
	mu sync.Mutex
	m  map[string]lowerEntry
}

type lowerEntry struct {
	rev, size int
	lower     string
}

func (c *lowerCache) get(it *Item) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]lowerEntry{}
	}
	if e, ok := c.m[it.ID]; ok && e.rev == it.Rev && e.size == len(it.Text) {
		return e.lower
	}
	l := strings.ToLower(it.Text)
	c.m[it.ID] = lowerEntry{it.Rev, len(it.Text), l}
	return l
}

// prune drops the cache entries of content that is no longer in the list.
func (c *lowerCache) prune(alive map[string]bool) {
	c.mu.Lock()
	for id := range c.m {
		if !alive[id] {
			delete(c.m, id)
		}
	}
	c.mu.Unlock()
}

// Long texts in the web list only contain the first 16 KB; searching the rest is done by the node on the full text.

type SearchHit struct {
	ID      string `json:"id"`
	Line    int    `json:"line"`    // line the match is on (starting at 1)
	Snippet string `json:"snippet"` // a short piece of text around the match
}

// SearchFull finds the content in truncated long texts that contains all the keywords (case-insensitive).
func (a *App) SearchFull(query string) []SearchHit {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return []SearchHit{}
	}
	out := []SearchHit{}
	alive := map[string]bool{}
	defer func() { a.lower.prune(alive) }()
	for _, it := range a.store.List() {
		if it.Kind != "text" || len(it.Text) <= previewBytes {
			continue // the web page can search short content on its own
		}
		alive[it.ID] = true
		lower := a.lower.get(it)
		// locate the keyword that appears latest: it is usually the one outside the snippet that only a full text search can find
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
		// the case conversion may change the byte length (for a few characters), so the snippet is taken from the converted text in that case
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

// snippet returns about width bytes of text around pos, without cutting multi-byte characters; a line break is shown as ⏎.
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
