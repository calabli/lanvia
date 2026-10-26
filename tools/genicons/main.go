// genicons prepares the approved sapphire/ice-blue artwork for each platform.
// Run from the repository root: go run ./tools/genicons
// source.png retains the approved presentation artwork. Resource preparation
// crops its presentation margin and applies the app tile's antialiased corners.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

func main() {
	f, err := os.Open("tools/genicons/source.png")
	if err != nil {
		panic(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		panic(err)
	}
	b := src.Bounds()
	// These normalized bounds describe the tile, excluding its presentation
	// margin and ground shadow. Artwork inside the tile is kept unchanged.
	art := image.Rect(b.Min.X+int(float64(b.Dx())*.04), b.Min.Y+int(float64(b.Dy())*.034), b.Min.X+int(float64(b.Dx())*.96), b.Min.Y+int(float64(b.Dy())*.94))
	icon := resize(src, art, 1024)
	roundTile(icon)
	save("cmd/lanvia-desktop/build/appicon-windows.png", resize(icon, icon.Bounds(), 256))
	for _, size := range []int{16, 20, 24, 32, 48} {
		save(filepath.Join("cmd/lanvia-desktop/build", fmt.Sprintf("icon-small-%d.png", size)), smallMark(size, false))
	}
	save("cmd/lanvia-desktop/assets/tray.png", smallMark(32, false))
	save("core/web/icon.png", resize(icon, icon.Bounds(), 512))
	save("core/web/icon-192.png", resize(icon, icon.Bounds(), 192))
	save("core/web/icon-180.png", resize(icon, icon.Bounds(), 180))
	mac := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	small := resize(icon, icon.Bounds(), 824)
	draw.Draw(mac, image.Rect(100, 100, 924, 924), small, image.Point{}, draw.Src)
	save("cmd/lanvia-desktop/build/appicon.png", mac)
	// macOS uses a monochrome template, tinted automatically by the system.
	save("cmd/lanvia-desktop/assets/tray-template.png", smallMark(44, true))
}

// Draw the small mark independently of the glass tile. Thick open links remain
// legible at 16 px, with a cool rim for contrast on light and dark title bars.
func smallMark(size int, mono bool) *image.NRGBA {
	const scale = 8
	n := size * scale
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	roundRect := func(x, y, hx, hy, r float64) float64 {
		qx, qy := math.Abs(x)-hx+r, math.Abs(y)-hy+r
		return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			px, py := (float64(x)+.5)/float64(n)-.5, (float64(y)+.5)/float64(n)-.5
			u, v := (px-py)/math.Sqrt2, (px+py)/math.Sqrt2
			inside, rim := false, false
			for _, shift := range []float64{-.17, .17} {
				a, b := u-shift, v
				outer := roundRect(a, b, .235, .155, .115)
				inner := roundRect(a, b, .135, .055, .045)
				gap := (shift < 0 && a > .11 && b < -.005) || (shift > 0 && a < -.11 && b > .005)
				if outer <= 0 && inner >= 0 && !gap {
					inside = true
				}
				if outer <= .018 && inner >= -.018 && !gap {
					rim = true
				}
			}
			if inside {
				c := color.NRGBA{R: 30, G: 135, B: 238, A: 255}
				if mono {
					c = color.NRGBA{A: 255}
				}
				dst.SetNRGBA(x, y, c)
			} else if rim && !mono {
				dst.SetNRGBA(x, y, color.NRGBA{R: 185, G: 225, B: 255, A: 255})
			}
		}
	}
	return resize(dst, dst.Bounds(), size)
}

// Average premultiplied colors to avoid dark fringes at transparent edges.
func resize(src image.Image, region image.Rectangle, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	sx, sy := float64(region.Dx())/float64(size), float64(region.Dy())/float64(size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			x0, y0 := float64(region.Min.X)+float64(x)*sx, float64(region.Min.Y)+float64(y)*sy
			x1, y1 := x0+sx, y0+sy
			var rr, gg, bb, aa, weight float64
			for iy := int(math.Floor(y0)); iy < int(math.Ceil(y1)); iy++ {
				wy := math.Min(y1, float64(iy+1)) - math.Max(y0, float64(iy))
				for ix := int(math.Floor(x0)); ix < int(math.Ceil(x1)); ix++ {
					w := (math.Min(x1, float64(ix+1)) - math.Max(x0, float64(ix))) * wy
					r, g, b, a := src.At(ix, iy).RGBA()
					rr += float64(r) * w
					gg += float64(g) * w
					bb += float64(b) * w
					aa += float64(a) * w
					weight += w
				}
			}
			if aa > 0 && weight > 0 {
				dst.SetNRGBA(x, y, color.NRGBA{R: channel(rr / aa * 255), G: channel(gg / aa * 255), B: channel(bb / aa * 255), A: channel(aa / weight / 257)})
			}
		}
	}
	return dst
}

func channel(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(255, v)))) }

func roundTile(icon *image.NRGBA) {
	n := float64(icon.Bounds().Dx())
	radius := n * .23
	for y := 0; y < icon.Bounds().Dy(); y++ {
		for x := 0; x < icon.Bounds().Dx(); x++ {
			coverage := 0.0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px, py := float64(x)+(float64(sx)+.5)/4, float64(y)+(float64(sy)+.5)/4
					qx := math.Max(math.Abs(px-n/2)-(n/2-radius), 0)
					qy := math.Max(math.Abs(py-n/2)-(n/2-radius), 0)
					if math.Hypot(qx, qy) <= radius {
						coverage++
					}
				}
			}
			c := icon.NRGBAAt(x, y)
			c.A = channel(float64(c.A) * coverage / 16)
			icon.SetNRGBA(x, y, c)
		}
	}
}

func save(path string, img image.Image) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		panic(err)
	}
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		panic(err)
	}
	if err := f.Close(); err != nil {
		panic(err)
	}
}
