package core

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func testImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 10), uint8(y * 20), 77, uint8(255 - x)})
		}
	}
	return img
}

// PNG → Windows 位图 → PNG，像素一致（含透明度）
func TestDIBRoundTrip(t *testing.T) {
	src := testImage(13, 7) // 奇数宽度：行尾不需要补齐（32 位色），但高度方向从下往上
	var pb bytes.Buffer
	png.Encode(&pb, src)
	dib, pngOut, err := imageToClip(pb.Bytes())
	if err != nil || !bytes.Equal(pngOut, pb.Bytes()) {
		t.Fatalf("imageToClip: %v", err)
	}
	back, err := dibToPNG(dib)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := png.Decode(bytes.NewReader(back))
	for y := 0; y < 7; y++ {
		for x := 0; x < 13; x++ {
			if g, w := color.NRGBAModel.Convert(got.At(x, y)), src.NRGBAAt(x, y); g != w {
				t.Fatalf("pixel %d,%d = %v, want %v", x, y, g, w)
			}
		}
	}
}

// 24 位色、从上往下存、每行要补齐到 4 字节的位图；以及 32 位色但 alpha 全是 0（应当按不透明处理）
func TestDIBVariants(t *testing.T) {
	w, h := 3, 2
	stride := (w*24 + 31) / 32 * 4 // 12
	dib := make([]byte, 40+stride*h)
	le := binary.LittleEndian
	le.PutUint32(dib[0:], 40)
	le.PutUint32(dib[4:], uint32(w))
	le.PutUint32(dib[8:], uint32(int32(-h))) // 负数：从上往下
	le.PutUint16(dib[12:], 1)
	le.PutUint16(dib[14:], 24)
	dib[40+0], dib[40+1], dib[40+2] = 1, 2, 3 // 第一行第一个像素 BGR
	out, err := dibToPNG(dib)
	if err != nil {
		t.Fatal(err)
	}
	img, _ := png.Decode(bytes.NewReader(out))
	if c := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); c != (color.NRGBA{3, 2, 1, 255}) {
		t.Fatalf("top-down 24-bit pixel = %v", c)
	}

	dib32, _, _ := imageToClip(func() []byte { var b bytes.Buffer; png.Encode(&b, testImage(4, 4)); return b.Bytes() }())
	for i := 40 + 3; i < len(dib32); i += 4 {
		dib32[i] = 0
	}
	out, _ = dibToPNG(dib32)
	img, _ = png.Decode(bytes.NewReader(out))
	if _, _, _, a := img.At(1, 1).RGBA(); a != 0xffff {
		t.Fatal("all-zero alpha should be treated as opaque")
	}
	if _, err := dibToPNG([]byte("short")); err == nil {
		t.Fatal("garbage accepted")
	}
}

// 收到 JPEG 时也给出 PNG
func TestImageToClipJPEG(t *testing.T) {
	var jb bytes.Buffer
	jpeg.Encode(&jb, testImage(8, 8), nil)
	dib, p, err := imageToClip(jb.Bytes())
	if err != nil || len(dib) != 40+8*8*4 || !bytes.HasPrefix(p, []byte("\x89PNG")) {
		t.Fatalf("jpeg: %v dib=%d", err, len(dib))
	}
}
