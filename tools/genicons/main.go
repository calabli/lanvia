// genicons 生成桌面客户端用到的图标（与 core/web/icon.svg 同一个设计）。
//
//	go run ./tools/genicons
//
// 设计：薄荷绿 → 天蓝 → 科技蓝的渐变底，两个设备节点由一条 S 形曲线相连，
// 曲线前半段实心、后半段半透明，表示内容正在设备之间流转。
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

func circle(cx, cy, r float64) shape {
	return func(x, y float64) float64 { return math.Hypot(x-cx, y-cy) - r }
}

// cubic 是三次贝塞尔曲线的描边（圆头），用折线近似。
func cubic(x0, y0, x1, y1, x2, y2, x3, y3, w float64) shape {
	const n = 64
	var px, py [n + 1]float64
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := 0; i <= n; i++ {
		t := float64(i) / n
		u := 1 - t
		px[i] = u*u*u*x0 + 3*u*u*t*x1 + 3*u*t*t*x2 + t*t*t*x3
		py[i] = u*u*u*y0 + 3*u*u*t*y1 + 3*u*t*t*y2 + t*t*t*y3
		minX, minY, maxX, maxY = math.Min(minX, px[i]), math.Min(minY, py[i]), math.Max(maxX, px[i]), math.Max(maxY, py[i])
	}
	return func(x, y float64) float64 {
		if x < minX-w || x > maxX+w || y < minY-w || y > maxY+w {
			return w // 远离曲线，省掉逐段计算
		}
		d := math.Inf(1)
		for i := 0; i < n; i++ {
			ax, ay, dx, dy := x-px[i], y-py[i], px[i+1]-px[i], py[i+1]-py[i]
			t := math.Max(0, math.Min(1, (ax*dx+ay*dy)/(dx*dx+dy*dy)))
			d = math.Min(d, math.Hypot(ax-dx*t, ay-dy*t))
		}
		return d - w/2
	}
}

type layer struct {
	s     shape
	c     color.NRGBA // A < 255 时半透明叠加
	erase bool        // 抠掉下面的像素（模板图标里的镂空）
	paint func(x, y float64) color.NRGBA
}

var (
	white = color.NRGBA{255, 255, 255, 255}
	black = color.NRGBA{0, 0, 0, 255}

	// 与 icon.svg 中的路径一致
	flow     = cubic(18, 46, 18, 25, 46, 39, 46, 18, 6)      // 整条曲线
	flowDone = cubic(18, 46, 18, 35.5, 25, 33.75, 32, 32, 6) // 前半段（实心）：整条曲线在 t=0.5 处精确截取，两段严丝合缝
	nodeA    = circle(18, 46, 7.5)
	nodeB    = circle(46, 18, 7.5)
	holeA    = circle(18, 46, 3)
	holeB    = circle(46, 18, 3)
)

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	channel := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return color.NRGBA{channel(a.R, b.R), channel(a.G, b.G), channel(a.B, b.B), 255}
}

// 底色：左上薄荷绿 → 中间天蓝 → 右下科技蓝，上半部分叠一层淡淡的高光
func shellColor(x, y float64) color.NRGBA {
	t := (x + y) / 128
	mint, sky, blue := color.NRGBA{0x5e, 0xf0, 0xcf, 255}, color.NRGBA{0x22, 0xb9, 0xf2, 255}, color.NRGBA{0x3a, 0x6f, 0xf5, 255}
	var c color.NRGBA
	if t < .5 {
		c = mix(mint, sky, t/.5)
	} else {
		c = mix(sky, blue, (t-.5)/.5)
	}
	if g := .26 * (1 - y/64/.55); g > 0 {
		c = mix(c, white, g)
	}
	return c
}

// render 把 64×64 设计稿中 [vx, vx+vw] 的区域画到 size×size 的图上，4×4 超采样抗锯齿。
func render(size int, vx, vy, vw float64, layers []layer) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64 // 预乘 alpha 累加
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := vx + (float64(px)+(float64(sx)+0.5)/ss)*vw/float64(size)
					y := vy + (float64(py)+(float64(sy)+0.5)/ss)*vw/float64(size)
					var cr, cg, cb, ca float64 // 当前子像素（预乘）
					for _, l := range layers {
						if l.s(x, y) >= 0 {
							continue
						}
						if l.erase {
							cr, cg, cb, ca = 0, 0, 0, 0
							continue
						}
						c := l.c
						if l.paint != nil {
							c = l.paint(x, y)
						}
						al := float64(c.A) / 255
						cr = float64(c.R)*al + cr*(1-al)
						cg = float64(c.G)*al + cg*(1-al)
						cb = float64(c.B)*al + cb*(1-al)
						ca = al + ca*(1-al)
					}
					r, g, b, a = r+cr, g+cg, b+cb, a+ca
				}
			}
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{uint8(math.Min(255, r/a)), uint8(math.Min(255, g/a)), uint8(math.Min(255, b/a)), uint8(math.Round(a / ss / ss * 255))})
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
		{s: roundRect(0, 0, 64, 64, 14.3), paint: shellColor},
		{s: flow, c: color.NRGBA{255, 255, 255, 140}}, // 后半段：半透明
		{s: flowDone, c: white},
		{s: nodeA, c: white},
		{s: holeA, c: color.NRGBA{0x27, 0xb6, 0xf1, 255}},
		{s: nodeB, c: white},
		{s: holeB, c: color.NRGBA{0x2f, 0xa6, 0xf3, 255}},
	}
	// macOS 菜单栏模板图标：只用 alpha，系统会按浅色/深色模式着色。节点做成圆环，线条稍粗以便小尺寸可读
	glyph := []layer{
		{s: cubic(18, 46, 18, 25, 46, 39, 46, 18, 6.5), c: black},
		{s: circle(18, 46, 8.5), erase: true}, // 节点和线之间留出缝隙
		{s: circle(46, 18, 8.5), erase: true},
		{s: circle(18, 46, 7), c: black},
		{s: circle(46, 18, 7), c: black},
		{s: circle(18, 46, 3), erase: true},
		{s: circle(46, 18, 3), erase: true},
	}
	// 1024 的 macOS 图标按 Apple 规范四周留白（内容约占 824px）
	save("cmd/lanvia-desktop/build/appicon.png", render(1024, -7.8, -7.8, 79.6, full))
	save("cmd/lanvia-desktop/build/appicon-windows.png", render(256, 0, 0, 64, full))
	save("cmd/lanvia-desktop/assets/tray-template.png", render(44, 7, 7, 50, glyph))
	save("cmd/lanvia-desktop/assets/tray.png", render(64, 0, 0, 64, full))
}
