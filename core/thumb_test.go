package core

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

// 在 JPEG 的 SOI 之后插入一段只含方向标记的 EXIF
func withOrientation(jpg []byte, o uint16) []byte {
	tiff := []byte("II*\x00\x08\x00\x00\x00") // 小端，IFD0 在偏移 8
	tiff = append(tiff, 1, 0)                 // 1 个条目
	e := make([]byte, 12)
	binary.LittleEndian.PutUint16(e[0:], 0x0112) // Orientation
	binary.LittleEndian.PutUint16(e[2:], 3)      // SHORT
	binary.LittleEndian.PutUint32(e[4:], 1)
	binary.LittleEndian.PutUint16(e[8:], o)
	tiff = append(append(tiff, e...), 0, 0, 0, 0)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, app1...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func TestThumbnail(t *testing.T) {
	// 2000×1000 的横图，左半红右半蓝
	src := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 2000; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 1000 {
				c = color.RGBA{0, 0, 255, 255}
			}
			src.Set(x, y, c)
		}
	}
	var jb bytes.Buffer
	jpeg.Encode(&jb, src, &jpeg.Options{Quality: 90})

	out, err := makeThumb(jb.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	img, _ := jpeg.Decode(bytes.NewReader(out))
	if b := img.Bounds(); b.Dx() != 480 || b.Dy() != 240 {
		t.Fatalf("size %v, want 480x240", b)
	}

	// EXIF 方向 6（顺时针转 90°）：缩略图应变成竖的，原来的左边（红）到上边
	out, _ = makeThumb(withOrientation(jb.Bytes(), 6))
	img, _ = jpeg.Decode(bytes.NewReader(out))
	if b := img.Bounds(); b.Dx() != 240 || b.Dy() != 480 {
		t.Fatalf("rotated size %v, want 240x480", b)
	}
	if r, _, bl, _ := img.At(120, 40).RGBA(); r < bl {
		t.Fatal("orientation not applied: top should be red")
	}

	// 透明 PNG：透明部分垫白
	pn := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	var pb bytes.Buffer
	png.Encode(&pb, pn)
	out, err = makeThumb(pb.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	img, _ = jpeg.Decode(bytes.NewReader(out))
	if r, g, b, _ := img.At(50, 50).RGBA(); r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Fatal("transparent area should be white")
	}

	if _, err := makeThumb([]byte("not an image")); err == nil {
		t.Fatal("non-image accepted")
	}
}

func TestThumbnailCachedAndCleaned(t *testing.T) {
	a := testApp(t, nil)
	var jb bytes.Buffer
	jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 800, 600)), nil)
	it := &Item{ID: "a000000000000001", Kind: "file", Name: "p.jpg", Mime: "image/jpeg", Size: int64(jb.Len()), From: "x", Origin: a.cfgSnapshot().ID, TS: nowMs()}
	a.store.Add(it)
	os.WriteFile(a.store.blobPath(it.ID), jb.Bytes(), 0o600)
	p, err := a.Thumbnail(it)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("thumbnail not cached")
	}
	a.store.Delete(it.ID)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("thumbnail kept after delete")
	}
	if _, err := a.Thumbnail(&Item{Kind: "file", Mime: "application/pdf"}); err == nil {
		t.Fatal("thumbnail for non-image")
	}
}
