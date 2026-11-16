package core

import (
	"reflect"
	"testing"
)

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		"  #工作 ":  "工作",
		"＃家里":     "家里",
		"a  b\tc": "a b c",
		"x,y，z":   "x y z",
		"#":       "",
		"一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十多出来": "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十",
	}
	for in, want := range cases {
		if got := normalizeTag(in); got != want {
			t.Errorf("normalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

// Two devices change tags at the same time; merging them in different orders gives the same result
func TestTagsConverge(t *testing.T) {
	const id = "a000000000000001"
	mk := func() *Store {
		s := openStore(t.TempDir())
		s.Add(&Item{ID: id, Kind: "text", Text: "x", From: "x", Origin: "o", TS: nowMs(), Tags: map[string]int64{"工作": 10, "旧": 10}})
		return s
	}
	changes := []map[string]int64{
		{"家里": 20},          // A adds "家里"
		{"旧": -21},          // B deletes "旧" at the same time
		{"工作": -30},         // A deletes "工作"
		{"工作": 30},          // B adds "工作" again in the same millisecond: the addition wins
		{"家里": -15},         // an earlier deletion, does not take effect
		{"#坏的": 40, "": 40}, // invalid tags are ignored
	}
	a, b := mk(), mk()
	for _, c := range changes {
		a.applyTags(id, c)
	}
	for i := len(changes) - 1; i >= 0; i-- {
		b.applyTags(id, changes[i])
	}
	ga, gb := a.Get(id).Tags, b.Get(id).Tags
	if !reflect.DeepEqual(ga, gb) {
		t.Fatalf("diverged:\n %v\n %v", ga, gb)
	}
	if got := a.Get(id).LiveTags(); !reflect.DeepEqual(got, []string{"家里", "工作"}) {
		t.Fatalf("live tags = %v", got)
	}
	if a.applyTags(id, changes[0]) != nil {
		t.Fatal("re-applying should be a no-op")
	}
}

func TestSetTags(t *testing.T) {
	a := testApp(t, nil)
	x := a.newText("第一条", "Mac", false)
	y := a.newText("第二条", "Mac", false)
	if _, err := a.SetTags(x.ID, []string{"Work", "#家里", "  "}, nil); err != nil {
		t.Fatal(err)
	}
	// When only the case differs, the existing spelling is kept
	it, _ := a.SetTags(y.ID, []string{"work"}, nil)
	if got := it.LiveTags(); !reflect.DeepEqual(got, []string{"Work"}) {
		t.Fatalf("case reuse: %v", got)
	}
	it, _ = a.SetTags(x.ID, nil, []string{"家里"})
	if got := it.LiveTags(); !reflect.DeepEqual(got, []string{"Work"}) || it.Tags["家里"] >= 0 {
		t.Fatalf("after remove: %v %v", got, it.Tags)
	}
	// Delete it and add it back
	it, _ = a.SetTags(x.ID, []string{"家里"}, nil)
	if it.Tags["家里"] <= 0 {
		t.Fatalf("re-add: %v", it.Tags)
	}
	if all := a.AllTags(); len(all) != 2 || all[0] != (TagCount{"Work", 2}) {
		t.Fatalf("AllTags = %v", all)
	}
	var many []string
	for i := 0; i < maxTags+1; i++ {
		many = append(many, "t"+string(rune('a'+i)))
	}
	if _, err := a.SetTags(y.ID, many, nil); err == nil {
		t.Fatal("tag limit not enforced")
	}
	if _, err := a.SetTags("ffffffffffffffff", []string{"x"}, nil); err == nil {
		t.Fatal("unknown item")
	}
}

func TestTagsPersist(t *testing.T) {
	dir := t.TempDir()
	s := openStore(dir)
	s.Add(&Item{ID: "a000000000000001", Kind: "text", Text: "x", From: "x", Origin: "o", TS: nowMs(), Tags: map[string]int64{"工作": 5, "旧": -6}})
	s.Flush()
	if got := openStore(dir).Get("a000000000000001").Tags; !reflect.DeepEqual(got, map[string]int64{"工作": 5, "旧": -6}) {
		t.Fatalf("after reload: %v", got)
	}
}
