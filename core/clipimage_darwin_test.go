//go:build darwin && cgo

package core

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
	"time"
)

// 在独立的剪贴板上走一遍：复制图片会发送，自己写进去的图片不会再发，文字照常
func TestClipboardImageMac(t *testing.T) {
	useTestPasteboard() // 不碰用户的系统剪贴板
	a := testApp(t, func(c *Config) { c.Watch = true })
	c := a.clip
	count := func() int { return len(a.store.List()) }

	// 1. 复制一张 PNG：发送为图片内容，文件内容就是剪贴板里的 PNG
	var pb bytes.Buffer
	png.Encode(&pb, testImage(20, 10))
	if err := writeClipboardImage(pb.Bytes()); err != nil {
		t.Fatal(err)
	}
	c.poll()
	if count() != 1 {
		t.Fatalf("image not sent, %d items", count())
	}
	it := a.store.List()[0]
	data, _ := os.ReadFile(a.store.blobPath(it.ID))
	img, err := png.Decode(bytes.NewReader(data))
	if it.Kind != "file" || it.Mime != "image/png" || err != nil || img.Bounds() != image.Rect(0, 0, 20, 10) {
		t.Fatalf("sent item: %+v err=%v", it, err)
	}
	// 2. 没有变化：不重复发送
	c.poll()
	if count() != 1 {
		t.Fatal("same image sent twice")
	}
	// 3. 其他设备发来的图片（JPEG）写进剪贴板：能读出来，但不会被当成新复制的再发出去
	var jb bytes.Buffer
	jpeg.Encode(&jb, testImage(8, 8), nil)
	if err := c.ApplyImage(jb.Bytes()); err != nil {
		t.Fatal(err)
	}
	if got, err := readClipboardImage(); err != nil || !bytes.HasPrefix(got, []byte("\x89PNG")) {
		t.Fatalf("read back applied image: %v", err)
	}
	c.poll()
	if count() != 1 {
		t.Fatal("applied image echoed back")
	}
	// 4. 再复制文字：照常发送文字
	writeClipboard("复制的文字")
	c.poll()
	if count() != 2 || a.store.List()[0].Text != "复制的文字" {
		t.Fatalf("text not sent: %d", count())
	}
	// 5. 主动复制关闭时不发送；手动发送剪贴板里的图片
	a.updateCfg(func(cf *Config) { cf.Watch = false })
	var pb2 bytes.Buffer
	png.Encode(&pb2, testImage(5, 5))
	writeClipboardImage(pb2.Bytes())
	c.poll()
	if count() != 2 {
		t.Fatal("sent while auto copy is off")
	}
	if st := a.ClipboardState(); st.Image == 0 || st.Sent {
		t.Fatalf("clipboard state: %+v", st)
	}
	if _, err := a.SendClipboard(); err != nil || count() != 3 || a.store.List()[0].Mime != "image/png" {
		t.Fatalf("manual send: %v, %d items", err, count())
	}
	if st := a.ClipboardState(); !st.Sent {
		t.Fatalf("after manual send, state = %+v", st)
	}
}

// 收到其他设备的图片：开着主动粘贴时放进剪贴板，且不会被当成新复制的再发回去；关着时不放
func TestReceivedImageAutoPaste(t *testing.T) {
	useTestPasteboard()
	a := testApp(t, func(c *Config) { c.Watch = true; c.AutoApply = true })
	writeClipboard("原来的内容")
	a.clip.resync()
	recv := func(w int) *Item {
		var b bytes.Buffer
		png.Encode(&b, testImage(w, 3))
		id := randHex(8)
		os.WriteFile(a.store.blobPath(id), b.Bytes(), 0o600) // 文件已经同步到本机
		it := &Item{ID: id, Kind: "file", Name: "x.png", Mime: "image/png", Size: int64(b.Len()), From: "PC", Origin: "b2b2b2b2b2b2b2b2", TS: nowMs()}
		a.ingest(it, "b2b2b2b2b2b2b2b2", true)
		return it
	}
	waitImg := func(w int) bool {
		for i := 0; i < 100; i++ {
			if got, err := readClipboardImage(); err == nil {
				if img, err := png.Decode(bytes.NewReader(got)); err == nil && img.Bounds().Dx() == w {
					return true
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	recv(7)
	if !waitImg(7) {
		t.Fatal("received image not on clipboard")
	}
	n := len(a.store.List())
	a.clip.poll()
	if len(a.store.List()) != n {
		t.Fatal("pasted image echoed back")
	}
	// 关掉主动粘贴：收到的图片不放进剪贴板
	a.updateCfg(func(c *Config) { c.AutoApply = false })
	recv(9)
	time.Sleep(200 * time.Millisecond)
	if waitImg(9) {
		t.Fatal("image applied with auto paste off")
	}
}
