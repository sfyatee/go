package main

import (
	"log"

	"9fans.net/go/draw"
	"9fans.net/go/draw/memdraw"
	"github.com/go-text/typesetting/fontscan"
)

func loadfonts() {
	sysfonts, err := fontscan.SystemFonts(nil, "")
	if err != nil {
		log.Fatal("font initialization failed")
	}
}

func load(f *XFont) {
}

func mksubfont(f *XFont, name string, lo, hi, size int, antialias bool) {
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
	m1, _ := memdraw.AllocImage(draw.Rect(0, 0, x, y))
	memdraw.Draw(m1, m1.r, m, m.r.Min, memdraw.Opaque, draw.ZP, draw.S)
	memdraw.Free(m)
	memdraw.Free(mc)

	sf.name = nil
	sf.n = hi + 1 - lo
	sf.height = m1.r.Dy()
	sf.ascent = m1.r.Dy() - y0
	sf.info = fc0
	sf.bits = m1

	return sf
}
