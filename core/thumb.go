package core

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // 注册解码器
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// 列表里的图片只加载缩略图（最长边 480 像素的 JPEG），原图在预览时才加载。
// 缩略图在电脑上生成一次后缓存在 blobs/thumbs/。

const (
	thumbMax       = 480
	thumbMaxBytes  = 60 << 20   // 更大的图片不生成缩略图
	thumbMaxPixels = 50_000_000 // 解码会占用约 4 字节/像素的内存
)

var (
	errNoThumb = errors.New("no thumbnail")
	thumbSem   = make(chan struct{}, 2) // 同时最多生成 2 张，打开一屏图片时不至于占满 CPU
)

func (s *Store) thumbPath(id string) string { return filepath.Join(s.blobDir, "thumbs", id+".jpg") }

func thumbable(it *Item) bool {
	if it == nil || it.Kind != "file" || it.Size > thumbMaxBytes {
		return false
	}
	switch strings.ToLower(it.Mime) {
	case "image/jpeg", "image/jpg", "image/png", "image/gif":
		return true
	}
	return false
}

// Thumbnail 返回缩略图文件的路径，没有就现在生成。
func (a *App) Thumbnail(it *Item) (string, error) {
	if !thumbable(it) {
		return "", errNoThumb
	}
	p := a.store.thumbPath(it.ID)
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	thumbSem <- struct{}{}
	defer func() { <-thumbSem }()
	if _, err := os.Stat(p); err == nil { // 排队期间别的请求已经生成了
		return p, nil
	}
	rc, err := a.openBlob(it) // 本机没有原图时会从其他设备取
	if err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(rc, thumbMaxBytes+1))
	rc.Close()
	if err != nil || len(data) > thumbMaxBytes {
		return "", errNoThumb
	}
	out, err := makeThumb(data)
	if err != nil {
		return "", err
	}
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := writeAtomic(p, out); err != nil {
		return "", err
	}
	return p, nil
}

func makeThumb(data []byte) ([]byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > thumbMaxPixels {
		return nil, errNoThumb
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errNoThumb
	}
	// 先转成 RGBA（标准库对 JPEG 的 YCbCr 等格式有快速路径），透明部分垫白底
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Over)

	w, h := b.Dx(), b.Dy()
	if w > thumbMax || h > thumbMax {
		if w >= h {
			w, h = thumbMax, max(1, h*thumbMax/b.Dx())
		} else {
			w, h = max(1, w*thumbMax/b.Dy()), thumbMax
		}
	}
	img := downscale(rgba, w, h)
	if format == "jpeg" {
		img = orient(img, exifOrientation(data))
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// downscale 用区域平均缩小（每个目标像素取对应源区域的平均值），缩小倍数大时也不会出现锯齿。
func downscale(src *image.RGBA, w, h int) *image.RGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if sw == w && sh == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, max((y+1)*sh/h, y*sh/h+1)
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, max((x+1)*sw/w, x*sw/w+1)
			var r, g, bl, n uint32
			for sy := y0; sy < y1; sy++ {
				row := src.Pix[sy*src.Stride:]
				for sx := x0; sx < x1; sx++ {
					p := row[sx*4:]
					r, g, bl, n = r+uint32(p[0]), g+uint32(p[1]), bl+uint32(p[2]), n+1
				}
			}
			d := dst.Pix[y*dst.Stride+x*4:]
			d[0], d[1], d[2], d[3] = uint8(r/n), uint8(g/n), uint8(bl/n), 255
		}
	}
	return dst
}

// exifOrientation 读取 JPEG 里 EXIF 的方向（1–8），读不到时返回 1。
// 手机拍的照片常常像素是横的，靠这个标记告诉看图软件要旋转。
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker, size := data[i+1], int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xDA || size < 2 || i+2+size > len(data) { // 到图像数据了
			return 1
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[ifd:]))
	for k := 0; k < n; k++ {
		e := ifd + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:]) == 0x0112 { // Orientation
			if v := int(bo.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orient 按 EXIF 方向把图片转正。
func orient(src *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 { // 5–8 需要宽高互换
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			copy(dst.Pix[dy*dst.Stride+dx*4:dy*dst.Stride+dx*4+4], src.Pix[y*src.Stride+x*4:y*src.Stride+x*4+4])
		}
	}
	return dst
}
