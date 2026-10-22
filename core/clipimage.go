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

// 剪贴板里的图片（截图、在浏览器或看图软件里复制的图片）：复制后作为图片文件发送；
// 开着“主动粘贴”时，其他设备发来的图片也自动放进本机剪贴板。
// 各平台的读写见 clipboard_*.go；这里是与平台无关的部分，以及 Windows 位图格式（DIB）与 PNG 之间的转换。

const maxClipImage = 20 << 20 // 超过这么大的图片不自动发送、不自动放进剪贴板

var errNoImage = errors.New("clipboard has no image")

func isClipImageMime(m string) bool {
	switch strings.ToLower(m) {
	case "image/png", "image/jpeg", "image/jpg", "image/gif":
		return true
	}
	return false
}

// newClipImage 把剪贴板里的图片（PNG）保存为一条图片内容并发送给其他设备。
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

// applyImage 把其他设备发来的图片放进本机剪贴板（本机没有文件时先从对方取）。
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
	// 下载期间可能又来了新内容，或者用户改了设置：那就不再覆盖剪贴板
	if c := a.cfgSnapshot(); !c.AutoApply || a.Paused() || !a.store.isLatest(it.ID) {
		return
	}
	if err := a.clip.ApplyImage(data); err != nil {
		log.Printf(L("写入剪贴板失败: %v", "Writing to clipboard failed: %v"), err)
	}
}

// ---- Windows 位图（CF_DIB / CF_DIBV5）与 PNG 的转换 ----

// dibToPNG 把 Windows 剪贴板里的位图（BITMAPINFOHEADER 及其后的像素）转成 PNG。支持 24、32 位色。
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
		off += 12 // BI_BITFIELDS：三个颜色掩码跟在 40 字节的头后面（V4/V5 头里已经包含）
	}
	stride := (w*bpp + 31) / 32 * 4
	if len(b) < off+stride*h {
		return nil, errNoImage
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	step, hasAlpha := bpp/8, false
	for y := 0; y < h; y++ {
		sy := h - 1 - y // 默认从下往上存
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
	if step == 4 && !hasAlpha { // 很多程序 32 位色时 alpha 全是 0，表示不透明
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

// imageToClip 把收到的图片（PNG / JPEG / GIF）转成 Windows 剪贴板要的位图（32 位色、从下往上），
// 同时给出 PNG（支持透明的程序会优先用它）。
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
