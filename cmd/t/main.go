package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"os"

	cairo "github.com/neurlang/wayland/cairoshim"
	"github.com/neurlang/wayland/window"
	"github.com/neurlang/wayland/wl"
)

// Minimal WidgetHandler: draw dummy.jpg centered each frame.
type jpegCenter struct {
	win  *window.Window
	img  *image.RGBA
	imgW int
	imgH int
	bgA  byte // background alpha (255 opaque)
}

func loadJPEG(path string) (*image.RGBA, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()

	src, err := jpeg.Decode(f)
	if err != nil {
		return nil, 0, 0, err
	}

	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	return rgba, b.Dx(), b.Dy(), nil
}

func (j *jpegCenter) Resize(_ *window.Widget, _ int32, _ int32, _ int32, _ int32) {
	// nothing; centering happens in Redraw using current surface size
}

func clear(surface cairo.Surface) {
	dst := surface.ImageSurfaceGetData()
	w := surface.ImageSurfaceGetWidth()
	h := surface.ImageSurfaceGetHeight()
	stride := surface.ImageSurfaceGetStride()
	if dst == nil {
		return
	}
	for y := 0; y < h; y++ {
		row := dst[y*stride : y*stride+w*4]
		for i := 0; i < len(row); i += 4 {
			row[i+0] = 0   // B
			row[i+1] = 0   // G
			row[i+2] = 0   // R
			row[i+3] = 255 // A
		}
	}
}

func blitCentered(j *jpegCenter, surface cairo.Surface) {
	dst := surface.ImageSurfaceGetData()
	w := surface.ImageSurfaceGetWidth()
	h := surface.ImageSurfaceGetHeight()
	stride := surface.ImageSurfaceGetStride()
	if dst == nil || j.img == nil {
		return
	}

	// center position in window pixels
	x0 := (w - j.imgW) / 2
	y0 := (h - j.imgH) / 2

	// clip against window
	srcMinX, srcMinY := 0, 0
	srcMaxX, srcMaxY := j.imgW, j.imgH

	if x0 < 0 {
		srcMinX = -x0
		x0 = 0
	}
	if y0 < 0 {
		srcMinY = -y0
		y0 = 0
	}
	if x0+(srcMaxX-srcMinX) > w {
		srcMaxX = srcMinX + (w - x0)
	}
	if y0+(srcMaxY-srcMinY) > h {
		srcMaxY = srcMinY + (h - y0)
	}
	if srcMinX >= srcMaxX || srcMinY >= srcMaxY {
		return
	}

	// cairoshim ARGB32 on little-endian is stored as BGRA bytes.
	for sy := srcMinY; sy < srcMaxY; sy++ {
		dy := y0 + (sy - srcMinY)

		dstRow := dst[dy*stride : dy*stride+w*4]
		srcRow := j.img.Pix[sy*j.img.Stride : sy*j.img.Stride+j.imgW*4]

		for sx := srcMinX; sx < srcMaxX; sx++ {
			dx := x0 + (sx - srcMinX)

			si := sx * 4
			di := dx * 4

			r := srcRow[si+0]
			g := srcRow[si+1]
			b := srcRow[si+2]
			// JPEG has no alpha; draw opaque
			dstRow[di+0] = b
			dstRow[di+1] = g
			dstRow[di+2] = r
			dstRow[di+3] = 255
		}
	}
}

func (j *jpegCenter) Redraw(widget *window.Widget) {
	surface := j.win.WindowGetSurface()
	if surface == nil {
		return
	}
	defer surface.Destroy()

	clear(surface)
	blitCentered(j, surface)

	// keep drawing (optional; if you only want redraw on resize, remove this)
	widget.ScheduleRedraw()
}

func (*jpegCenter) Enter(*window.Widget, *window.Input, float32, float32) {}
func (*jpegCenter) Leave(*window.Widget, *window.Input)                   {}
func (*jpegCenter) Motion(*window.Widget, *window.Input, uint32, float32, float32) int {
	return window.CursorLeftPtr
}
func (*jpegCenter) Button(*window.Widget, *window.Input, uint32, uint32, wl.PointerButtonState, window.WidgetHandler) {
}
func (*jpegCenter) TouchUp(*window.Widget, *window.Input, uint32, uint32, int32) {}
func (*jpegCenter) TouchDown(*window.Widget, *window.Input, uint32, uint32, int32, float32, float32) {
}
func (*jpegCenter) TouchMotion(*window.Widget, *window.Input, uint32, int32, float32, float32) {}
func (*jpegCenter) TouchFrame(*window.Widget, *window.Input)                                   {}
func (*jpegCenter) TouchCancel(*window.Widget, int32, int32)                                   {}
func (*jpegCenter) Axis(*window.Widget, *window.Input, uint32, uint32, float32)                {}
func (*jpegCenter) AxisSource(*window.Widget, *window.Input, uint32)                           {}
func (*jpegCenter) AxisStop(*window.Widget, *window.Input, uint32, uint32)                     {}
func (*jpegCenter) AxisDiscrete(*window.Widget, *window.Input, uint32, int32)                  {}
func (*jpegCenter) PointerFrame(*window.Widget, *window.Input)                                 {}

func main() {
	d, err := window.DisplayCreate([]string{})
	if err != nil {
		fmt.Println(err)
		return
	}
	defer d.Destroy()

	win := window.Create(d)
	if win == nil {
		fmt.Println("failed to create window")
		return
	}
	defer win.Destroy()

	// Load dummy.jpg from project root
	img, iw, ih, err := loadJPEG("dummy.jpg")
	if err != nil {
		fmt.Println("load dummy.jpg:", err)
		return
	}

	handler := &jpegCenter{
		win:  win,
		img:  img,
		imgW: iw,
		imgH: ih,
	}

	widget := win.AddWidget(handler)
	widget.SetUserDataWidgetHandler(handler)

	win.SetTitle("centered jpeg")
	win.SetBufferType(window.BufferTypeShm)

	// Pick an initial size (can be anything)
	widget.ScheduleResize(900, 700)

	// Ensure we actually paint at least once
	widget.ScheduleRedraw()

	window.DisplayRun(d)
}
