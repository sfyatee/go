package main

import (
	"log"

	"9fans.net/go/draw"
	"9fans.net/go/draw/memdraw"
	// "github.com/go-text/typesetting/"
	"github.com/go-text/typesetting/fontscan"
)

func loadfonts() {
	sysfonts, err := fontscan.SystemFonts(nil, "")
	if err != nil {
		log.Fatal("font initialization failed")
	}
}

func load(f *XFont) {
	if f == nil || f.loaded {
		return
	}
	f.loaded = true
}

var lines = []string{
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	"abcdefghijklmnopqrstuvwxyz",
	"g",
	"ÁĂÇÂÄĊÀČĀĄÅÃĥľƒ",
	"ὕαλον ϕαγεῖν δύναμαι· τοῦτο οὔ με βλάπτει.",
	"私はガラスを食べられます。それは私を傷つけません。",
	"Aš galiu valgyti stiklą ir jis manęs nežeidžia",
	"Môžem jesť sklo. Nezraní ma.",
}

func mksubfont(f *XFont, name string, lo, hi, size int, antialias bool) *memdraw.Subfont {
	var w, x, y, y0 int
	var sf *memdraw.Subfont

	m, _ := memdraw.AllocImage(draw.Rect(0, 0, x*(hi+1-lo)+1, y+1), draw.GREY8)
	mc, err := memdraw.AllocImage(draw.Rect(0, 0, x+1, y+1), draw.GREY8)
	if err != nil {
		memdraw.Free(m)
	}
	memdraw.FillColor(m, draw.Black)
	memdraw.FillColor(mc, draw.Black)
	fc := make([]draw.Fontchar, hi+2-lo)
	fc0 := fc

	x = 0
	for i := lo; i <= hi; i++ {
		memdraw.FillColor(m, draw.Black)
	}

	// round up to 32-bit boundary
	// so that in-memory data is same
	// layout as in-file data.
	if x == 0 {
		x = 1
	}
	if y == 0 {
		y = 1
	}
	if antialias {
		x += -x & 3
	} else {
		x += -x & 31
	}
	pix := draw.GREY8
	if !antialias {
		pix = draw.GREY1
	}
	m1, _ := memdraw.AllocImage(draw.Rect(0, 0, x, y), pix)
	memdraw.Draw(m1, m1.R, m, m.R.Min, memdraw.Opaque, draw.ZP, draw.S)
	memdraw.Free(m)
	memdraw.Free(mc)

	sf.Name = ""
	sf.N = hi + 1 - lo
	sf.Height = uint8(m1.R.Dx())
	sf.Ascent = int8(m1.R.Dy() - y0)
	sf.Info = fc0
	sf.Bits = m1

	return sf
}
