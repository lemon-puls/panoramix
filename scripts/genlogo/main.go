// 生成 Panoramix 应用图标（512×512）：
// 深色天幕 + 升出地平线的渐变太阳（呼应"全景/日出"的一体化运维定位）。
// 一次性工具：go run ./scripts/genlogo [输出路径]（默认 build/appicon.png）
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const S = 512

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

type rgba struct{ R, G, B, A uint8 }

func (c rgba) toColor() color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 255} }

func mix(c1, c2 rgba, t float64) rgba {
	return rgba{
		uint8(lerp(float64(c1.R), float64(c2.R), t)),
		uint8(lerp(float64(c1.G), float64(c2.G), t)),
		uint8(lerp(float64(c1.B), float64(c2.B), t)),
		255,
	}
}

// over 把前景按 alpha 叠加到背景
func over(bg rgba, fg rgba, alpha float64) rgba {
	return rgba{
		uint8(lerp(float64(bg.R), float64(fg.R), alpha)),
		uint8(lerp(float64(bg.G), float64(fg.G), alpha)),
		uint8(lerp(float64(bg.B), float64(fg.B), alpha)),
		255,
	}
}

func main() {
	out := "build/appicon.png"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	img := image.NewRGBA(image.Rect(0, 0, S, S))

	// 天幕：垂直渐变 #262c40 → #12141d
	skyTop := rgba{0x26, 0x2c, 0x40, 255}
	skyBottom := rgba{0x12, 0x14, 0x1d, 255}
	horizonY := float64(S) * 0.60
	horizonH := 30.0

	// 太阳：径向 #ffd97a → #ff9f43，圆心略高于地平线
	cx, cy := float64(S)*0.5, horizonY-float64(S)*0.10
	radius := float64(S) * 0.17
	sunIn := rgba{0xff, 0xd9, 0x7a, 255}
	sunOut := rgba{0xff, 0x9f, 0x43, 255}

	// 地平线：水平渐变 #63e2b7 → #4a9ffd
	h1 := rgba{0x63, 0xe2, 0xb7, 255}
	h2 := rgba{0x4a, 0x9f, 0xfd, 255}

	for y := 0; y < S; y++ {
		sky := mix(skyTop, skyBottom, float64(y)/S)
		for x := 0; x < S; x++ {
			c := sky
			// 太阳本体
			dx, dy := float64(x)-cx, float64(y)-cy
			if d := math.Sqrt(dx*dx + dy*dy); d <= radius {
				c = mix(sunIn, sunOut, d/radius)
			}
			// 地平线渐变条（覆盖太阳下缘，形成"日出"剪影）
			if y >= int(horizonY) && float64(y) < horizonY+horizonH {
				c = mix(h1, h2, (float64(y)-horizonY)/horizonH)
			}
			// 太阳在地平线下方的淡倒影
			if y >= int(horizonY+horizonH) && y < int(horizonY+horizonH*6) {
				ry := horizonY + horizonH + (float64(y)-horizonY-horizonH)*3
				dx2, dy2 := float64(x)-cx, ry-cy
				if d := math.Sqrt(dx2*dx2 + dy2*dy2); d <= radius {
					alpha := 0.35 * (1 - (float64(y)-horizonY-horizonH)/(horizonH*5))
					c = over(c, mix(sunIn, sunOut, d/radius), alpha)
				}
			}
			img.SetRGBA(x, y, c.toColor())
		}
	}

	// 点缀几颗星（固定伪随机位置，保证产物可复现）
	stars := [][2]int{{70, 70}, {150, 46}, {410, 88}, {452, 170}, {96, 190}, {330, 60}}
	for _, s := range stars {
		x, y := s[0], s[1]
		if float64(y) < horizonY-20 {
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if math.Abs(float64(dx))+math.Abs(float64(dy)) <= 1 {
						img.SetRGBA(x+dx, y+dy, (rgba{230, 235, 245, 255}).toColor())
					}
				}
			}
		}
	}

	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}
