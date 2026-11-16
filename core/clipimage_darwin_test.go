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

// Runs the whole flow on a separate pasteboard: copying an image sends it, images written in by ourselves are not sent again, text behaves as usual
func TestClipboardImageMac(t *testing.T) {
	useTestPasteboard() // does not touch the user's system clipboard
	a := testApp(t, func(c *Config) { c.Watch = true })
	c := a.clip
	count := func() int { return len(a.store.List()) }

	// 1. Copy a PNG: it is sent as image content, and the stored file is the PNG from the clipboard
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
	// 2. No change: not sent again
	c.poll()
	if count() != 1 {
		t.Fatal("same image sent twice")
	}
	// 3. An image (JPEG) received from another device is written into the clipboard: it can be read back, but is not sent out again as a new copy
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
	// 4. Copy text afterwards: the text is sent as usual
	writeClipboard("复制的文字")
	c.poll()
	if count() != 2 || a.store.List()[0].Text != "复制的文字" {
		t.Fatalf("text not sent: %d", count())
	}
	// 5. Nothing is sent while auto copy is off; manually send the image in the clipboard
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

// An image received from another device is put into the clipboard while auto paste is on, and is not sent back as a new copy; when it is off it is not put in
func TestReceivedImageAutoPaste(t *testing.T) {
	useTestPasteboard()
	a := testApp(t, func(c *Config) { c.Watch = true; c.AutoApply = true })
	writeClipboard("原来的内容")
	a.clip.resync()
	recv := func(w int) *Item {
		var b bytes.Buffer
		png.Encode(&b, testImage(w, 3))
		id := randHex(8)
		os.WriteFile(a.store.blobPath(id), b.Bytes(), 0o600) // the file has already been synced to this machine
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
	// Auto paste off: a received image is not put into the clipboard
	a.updateCfg(func(c *Config) { c.AutoApply = false })
	recv(9)
	time.Sleep(200 * time.Millisecond)
	if waitImg(9) {
		t.Fatal("image applied with auto paste off")
	}
}
