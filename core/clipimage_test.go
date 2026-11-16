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

// PNG → Windows bitmap → PNG, with identical pixels (alpha included)
func TestDIBRoundTrip(t *testing.T) {
	src := testImage(13, 7) // odd width: rows need no padding (32-bit color), but the height direction is bottom-up
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

// 24-bit bitmaps stored top-down with every row padded to 4 bytes; and 32-bit images whose alpha is all 0 (should be treated as opaque)
func TestDIBVariants(t *testing.T) {
	w, h := 3, 2
	stride := (w*24 + 31) / 32 * 4 // 12
	dib := make([]byte, 40+stride*h)
	le := binary.LittleEndian
	le.PutUint32(dib[0:], 40)
	le.PutUint32(dib[4:], uint32(w))
	le.PutUint32(dib[8:], uint32(int32(-h))) // negative: top-down
	le.PutUint16(dib[12:], 1)
	le.PutUint16(dib[14:], 24)
	dib[40+0], dib[40+1], dib[40+2] = 1, 2, 3 // first pixel of the first row in BGR
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

// Also produce a PNG when the input is a JPEG
func TestImageToClipJPEG(t *testing.T) {
	var jb bytes.Buffer
	jpeg.Encode(&jb, testImage(8, 8), nil)
	dib, p, err := imageToClip(jb.Bytes())
	if err != nil || len(dib) != 40+8*8*4 || !bytes.HasPrefix(p, []byte("\x89PNG")) {
		t.Fatalf("jpeg: %v dib=%d", err, len(dib))
	}
}
