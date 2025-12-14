// #include <u.h>
// #include <libc.h>
// #include <draw.h>
// #include <memdraw.h>

package memdraw

import "9fans.net/go/draw"

func allocmemsubfont(name string, n int, height int, ascent int, info []draw.Fontchar, i *Image) *Subfont {
	f := new(Subfont)
	f.N = n
	f.Height = uint8(height)
	f.Ascent = int8(ascent)
	f.Info = info
	f.Bits = i
	f.Name = name
	return f
}

func freememsubfont(f *Subfont) {
	if f == nil {
		return
	}
	Free(f.Bits)
}
