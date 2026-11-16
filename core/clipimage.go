package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

// Images on the clipboard (screenshots, images copied in a browser or an image viewer): they are sent as image files after copying;
// with "active paste" enabled, images sent by other devices are put into the local clipboard as well.
// The platform specific reading and writing lives in clipboard_*.go; this is the platform independent part plus the conversion between the Windows bitmap format (DIB) and PNG.

const maxClipImage = 20 << 20 // images larger than this are neither sent automatically nor put into the clipboard automatically

var errNoImage = errors.New("clipboard has no image")

func isClipImageMime(m string) bool {
	switch strings.ToLower(m) {
	case "image/png", "image/jpeg", "image/jpg", "image/gif":
		return true
	}
	return false
}

// newClipImage saves the image on the clipboard (PNG) as an image entry and sends it to the other devices.
func (a *App) newClipImage(data []byte) (*Item, error) {
	c := a.cfgSnapshot()
	id := randHex(8)
	final := a.store.blobPath(id)
	if err := writeAtomic(final, data); err != nil {
		return nil, err
	}
	name := L("图片 ", "Image ") + time.Now().Format("2006-01-02 15.04.05") + ".png"
	it := &Item{ID: id, Kind: "file", Name: name, Mime: "image/png", Size: int64(len(data)), From: c.Name, Origin: c.ID, TS: nowMs()}
	if !a.ingest(it, "", false) {
		os.Remove(final)
		return nil, errors.New("保存失败")
	}
	return it, nil
}

// applyImage puts an image sent by another device into the local clipboard (fetching it from the peer first when this machine does not have the file).
func (a *App) applyImage(it *Item) {
	rc, err := a.openBlob(it)
	if err != nil {
		return
	}
	data, err := io.ReadAll(io.LimitReader(rc, maxClipImage+1))
	rc.Close()
	if err != nil || len(data) > maxClipImage {
		return
	}
	// new content may have arrived during the download, or the user changed the settings: then do not overwrite the clipboard
	if c := a.cfgSnapshot(); !c.AutoApply || a.Paused() || !a.store.isLatest(it.ID) {
		return
	}
	if err := a.clip.ApplyImage(data); err != nil {
		log.Printf(L("写入剪贴板失败: %v", "Writing to clipboard failed: %v"), err)
	}
}

// ---- conversion between Windows bitmaps (CF_DIB / CF_DIBV5) and PNG ----

// dibToPNG converts a bitmap from the Windows clipboard (BITMAPINFOHEADER and the pixels after it) into PNG. It supports 24 and 32 bit color.
func dibToPNG(b []byte) ([]byte, error) {
	if len(b) < 40 {
		return nil, errNoImage
	}
	le := binary.LittleEndian
	hdr := int(le.Uint32(b[0:]))
	w, h := int(int32(le.Uint32(b[4:]))), int(int32(le.Uint32(b[8:])))
	bpp, comp := int(le.Uint16(b[14:])), le.Uint32(b[16:])
	topDown := h < 0
	if topDown {
		h = -h
	}
	if hdr < 40 || w <= 0 || h <= 0 || w*h > thumbMaxPixels || (bpp != 24 && bpp != 32) || (comp != 0 && comp != 3) {
		return nil, errNoImage
	}
	off := hdr
	if comp == 3 && hdr == 40 {
		off += 12 // BI_BITFIELDS: the three color masks follow the 40 byte header (they are already included in V4/V5 headers)
	}
	stride := (w*bpp + 31) / 32 * 4
	if len(b) < off+stride*h {
		return nil, errNoImage
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	step, hasAlpha := bpp/8, false
	for y := 0; y < h; y++ {
		sy := h - 1 - y // stored bottom-up by default
		if topDown {
			sy = y
		}
		src, dst := b[off+sy*stride:], img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			p := src[x*step:]
			d := dst[x*4:]
			d[0], d[1], d[2], d[3] = p[2], p[1], p[0], 255
			if step == 4 {
				d[3] = p[3]
				hasAlpha = hasAlpha || p[3] != 0
			}
		}
	}
	if step == 4 && !hasAlpha { // many programs set alpha to 0 for 32 bit color, which means opaque
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// imageToClip converts a received image (PNG / JPEG / GIF) into the bitmap the Windows clipboard expects (32 bit color, bottom-up),
// and at the same time returns a PNG (which programs that support transparency prefer).
func imageToClip(data []byte) (dib, pngData []byte, err error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > thumbMaxPixels {
		return nil, nil, errNoImage
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, errNoImage
	}
	bnd := src.Bounds()
	nrgba := image.NewNRGBA(image.Rect(0, 0, bnd.Dx(), bnd.Dy()))
	draw.Draw(nrgba, nrgba.Bounds(), src, bnd.Min, draw.Src)
	w, h := bnd.Dx(), bnd.Dy()
	dib = make([]byte, 40+w*h*4)
	le := binary.LittleEndian
	le.PutUint32(dib[0:], 40)
	le.PutUint32(dib[4:], uint32(w))
	le.PutUint32(dib[8:], uint32(h))
	le.PutUint16(dib[12:], 1)
	le.PutUint16(dib[14:], 32)
	le.PutUint32(dib[20:], uint32(w*h*4))
	for y := 0; y < h; y++ {
		row := dib[40+(h-1-y)*w*4:]
		for x := 0; x < w; x++ {
			c := nrgba.NRGBAAt(x, y)
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = c.B, c.G, c.R, c.A
		}
	}
	if format == "png" {
		return dib, data, nil
	}
	var out bytes.Buffer
	png.Encode(&out, nrgba)
	return dib, out.Bytes(), nil
}
