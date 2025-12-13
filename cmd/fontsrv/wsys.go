//go:build ignore

package main

import (
	"9fans.net/go/draw"
	"9fans.net/go/memdraw"
)

func loadfonts() {
}

func load(f *XFont) {
}

func mksubfont(f *Xfont) memdraw.Subfont {
	var sf *memdraw.Subfont

	m := memdraw.AllocImage(draw.Rect(0, 0, x*(hi+1-lo)+1, y+1), draw.GREY8)
	mc, err := memdraw.AllocImage(draw.Rect(0, 0, x+1, y+1), draw.GREY8)
	if err != nil {
		memdraw.Free(m)
	}
	memdraw.FillColor(m, draw.Black)
	memdraw.FillColor(mc, draw.Black)

	for {
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
	sf.height = Dy(m1.r)
	sf.ascent = Dy(m1.r) - y0
	sf.info = fc0
	sf.bits = m1

	return sf
}
