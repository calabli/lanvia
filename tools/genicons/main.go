// genicons 生成桌面客户端用到的图标（与 core/web/icon.svg 同一个设计）。
//
//	go run ./tools/genicons
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// 设计稿坐标系为 64×64，见 core/web/icon.svg。
type shape func(x, y float64) float64 // 有符号距离，<0 表示在内部

func roundRect(x0, y0, w, h, r float64) shape {
	cx, cy, hx, hy := x0+w/2, y0+h/2, w/2-r, h/2-r
	return func(x, y float64) float64 {
		qx, qy := math.Abs(x-cx)-hx, math.Abs(y-cy)-hy
		return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
	}
}

func stroke(s shape, w float64) shape {
	return func(x, y float64) float64 { return math.Abs(s(x, y)) - w/2 }
}

func capsule(ax, ay, bx, by, w float64) shape {
	return func(x, y float64) float64 {
		px, py, dx, dy := x-ax, y-ay, bx-ax, by-ay
		t := math.Max(0, math.Min(1, (px*dx+py*dy)/(dx*dx+dy*dy)))
		return math.Hypot(px-dx*t, py-dy*t) - w/2
	}
}

type layer struct {
	s     shape
	c     color.NRGBA
	erase bool // 抠掉下面的像素（模板图标里的线条）
	paint func(x, y float64) color.NRGBA
}

var (
	blue  = color.NRGBA{0x39, 0x7e, 0xe4, 255}
	white = color.NRGBA{255, 255, 255, 255}
	black = color.NRGBA{0, 0, 0, 255}

	back  = roundRect(14, 22, 23, 27, 6)
	front = roundRect(27, 14, 23, 29, 6)
	line1 = capsule(33, 25, 44, 25, 3)
	line2 = capsule(33, 31, 40, 31, 3)
)

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	channel := func(x, y uint8) uint8 { return uint8(float64(x)*(1-t) + float64(y)*t) }
	return color.NRGBA{channel(a.R, b.R), channel(a.G, b.G), channel(a.B, b.B), 255}
}

func shellColor(x, y float64) color.NRGBA {
	t := (x + y) / 128
	top, middle, bottom := color.NRGBA{0x70, 0xb5, 0xff, 255}, color.NRGBA{0x34, 0x7d, 0xeb, 255}, color.NRGBA{0x20, 0x57, 0xc6, 255}
	if t < .52 {
		return mix(top, middle, t/.52)
	}
	return mix(middle, bottom, (t-.52)/.48)
}

// render 把 64×64 设计稿中 [vx, vx+vw] 的区域画到 size×size 的图上，4×4 超采样抗锯齿。
func render(size int, vx, vy, vw float64, layers []layer) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := vx + (float64(px)+(float64(sx)+0.5)/ss)*vw/float64(size)
					y := vy + (float64(py)+(float64(sy)+0.5)/ss)*vw/float64(size)
					var cr, cg, cb, ca float64
					for _, l := range layers {
						if l.s(x, y) >= 0 {
							continue
						}
						if l.erase {
							cr, cg, cb, ca = 0, 0, 0, 0
						} else {
							c := l.c
							if l.paint != nil {
								c = l.paint(x, y)
							}
							cr, cg, cb, ca = float64(c.R), float64(c.G), float64(c.B), 255
						}
					}
					r, g, b, a = r+cr*ca, g+cg*ca, b+cb*ca, a+ca
				}
			}
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(a / ss / ss)})
			}
		}
	}
	return img
}

func save(path string, img image.Image) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

func main() {
	full := []layer{
		{s: roundRect(1, 1, 62, 62, 18), paint: shellColor},
		{s: stroke(back, 3), c: white},
		{s: front, paint: func(x, y float64) color.NRGBA { return mix(white, color.NRGBA{0xdc, 0xea, 0xff, 255}, (y-14)/29) }},
		{s: line1, c: blue},
		{s: line2, c: blue},
	}
	// macOS 菜单栏模板图标：只用 alpha，系统会按浅色/深色模式着色
	glyph := []layer{
		{s: stroke(back, 4.5), c: black},
		{s: roundRect(26, 12, 26, 32, 8), erase: true}, // 两张卡片之间留出缝隙
		{s: front, c: black},
		{s: line1, erase: true},
		{s: line2, erase: true},
	}
	// 1024 的 macOS 图标按 Apple 规范四周留白（内容约占 824px）
	save("cmd/lanvia-desktop/build/appicon.png", render(1024, -7.8, -7.8, 79.6, full))
	save("cmd/lanvia-desktop/build/appicon-windows.png", render(256, 0, 0, 64, full))
	save("cmd/lanvia-desktop/assets/tray-template.png", render(44, 8.4, 8.6, 45, glyph))
	save("cmd/lanvia-desktop/assets/tray.png", render(64, 0, 0, 64, full))
}
