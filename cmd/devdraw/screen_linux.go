package main

import (
	"fmt"
	"image"
	"os"
	"sync"

	"9fans.net/go/draw"
	"9fans.net/go/draw/memdraw"

	"github.com/neurlang/wayland/window"
	"github.com/neurlang/wayland/wl"
)

// ScreenPix is the pixel format used for the memdraw screen.
var ScreenPix = draw.XBGR32

// wlDisplay is the single Wayland display used by this devdraw instance.
var wlDisplay *window.Display

// rpcgfxlk is used by rpc_gfxdrawlock/rpc_gfxdrawunlock.
var rpcgfxlk sync.Mutex

// theImpl holds the per-client Wayland state and implements ClientImpl
// and window.WidgetHandler (via its methods).
type theImpl struct {
	client *Client

	win    *window.Window
	widget *window.Widget

	i    *memdraw.Image
	rgba *image.RGBA

	mu sync.Mutex
}

// Compile-time check only for ClientImpl; WidgetHandler is enforced when
// we pass *theImpl to AddWidget.
var _ ClientImpl = (*theImpl)(nil)

// memimageToRGBA creates an image.RGBA view over the memdraw.Image pixels.
func memimageToRGBA(i *memdraw.Image) *image.RGBA {
	return &image.RGBA{
		Pix:    i.BytesAt(i.R.Min),
		Stride: int(i.Width) * 4,
		Rect:   i.R,
	}
}

// gfx_main is called once from srv.go. Set up the Wayland Display and
// start its event loop in a goroutine, then tell the RPC side we’re ready.
func gfx_main() {
	d, err := window.DisplayCreate(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wayland: DisplayCreate failed: %v\n", err)
		os.Exit(1)
	}
	wlDisplay = d

	// Start the Wayland event loop.
	go window.DisplayRun(d)

	// Now start serving clients.
	gfx_started()
}

// rpc_attach is called for the first initdraw of a client.
// We create a Wayland window + widget and a memdraw.Image backing store.
func rpc_attach(c *Client, label, winsize string) (*memdraw.Image, error) {
	if wlDisplay == nil {
		return nil, fmt.Errorf("wayland: display not initialised")
	}

	// If we've already attached this client, just return its screen image.
	if c.impl != nil {
		if impl, ok := c.impl.(*theImpl); ok && impl.i != nil {
			return impl.i, nil
		}
	}

	// Decide an initial size. If winsize parses, use it, otherwise 1024x768.
	r := draw.Rect(0, 0, 1024, 768)
	if winsize != "" {
		var haveMin bool
		if err := parsewinsize(winsize, &r, &haveMin); err != nil {
			// ignore parse error; keep default
		}
	}

	// Create the memdraw backing store.
	img, err := memdraw.AllocImage(r, ScreenPix)
	if err != nil {
		return nil, err
	}

	impl := &theImpl{
		client: c,
		i:      img,
		rgba:   memimageToRGBA(img),
	}
	c.impl = impl
	c.mouserect = img.R
	if c.displaydpi == 0 {
		c.displaydpi = 100
	}

	// Create the Wayland toplevel window.
	win := window.Create(wlDisplay)
	if win == nil {
		return nil, fmt.Errorf("wayland: failed to create window")
	}
	impl.win = win

	if label != "" {
		win.SetTitle(label)
	}
	win.SetBufferType(window.BufferTypeShm)
	win.SetCloseHandler(impl) // theImpl has Close() below

	// Create a widget covering the main surface and let impl handle it.
	w := win.AddWidget(impl) // *theImpl must satisfy window.WidgetHandler
	impl.widget = w

	// Ask for an initial size and a first redraw.
	w.ScheduleResize(int32(r.Dx()), int32(r.Dy()))
	w.ScheduleRedraw()

	return img, nil
}

// rpc_shutdown is called when the last client exits.
func rpc_shutdown() {
	if wlDisplay != nil {
		wlDisplay.Exit()
	}
}

// rpc_gfxdrawlock / rpc_gfxdrawunlock wrap access to the real display.
func rpc_gfxdrawlock() {
	rpcgfxlk.Lock()
}

func rpc_gfxdrawunlock() {
	rpcgfxlk.Unlock()
}

// Simple in-process snarf buffer for now.
var snarfBuf []byte

func rpc_getsnarf() []byte {
	if len(snarfBuf) == 0 {
		return nil
	}
	cp := make([]byte, len(snarfBuf))
	copy(cp, snarfBuf)
	return cp
}

func rpc_putsnarf(b []byte) {
	if len(b) == 0 {
		snarfBuf = nil
		return
	}
	snarfBuf = make([]byte, len(b))
	copy(snarfBuf, b)
}

// --- ClientImpl methods (called from devdraw.go) ---

func (impl *theImpl) rpc_resizeimg(c *Client) {
	// devdraw wants to recreate the root image. We'll just treat the next
	// Resize from Wayland as authoritative and reallocate there.
}

func (impl *theImpl) rpc_resizewindow(c *Client, r draw.Rectangle) {
	// Request a resize of the Wayland window. We map the requested
	// pixels directly to surface coordinates.
	if impl == nil || impl.widget == nil {
		return
	}
	impl.widget.ScheduleResize(int32(r.Dx()), int32(r.Dy()))
}

func (impl *theImpl) rpc_setcursor(c *Client, cur *draw.Cursor, cur2 *draw.Cursor2) {
	// TODO: map Plan 9 cursors onto Wayland cursors.
}

func (impl *theImpl) rpc_setlabel(c *Client, label string) {
	if impl == nil || impl.win == nil {
		return
	}
	impl.win.SetTitle(label)
}

func (impl *theImpl) rpc_setmouse(c *Client, p draw.Point) {
	// We don't try to warp the host pointer.
}

func (impl *theImpl) rpc_topwin(c *Client) {
	// TODO: if the window API gets a "raise" call, use it here.
}

func (impl *theImpl) rpc_bouncemouse(c *Client, m draw.Mouse) {
	// Not implemented; mouse input is not hooked up yet.
}

// rpc_flush is called when some rectangle of the memdraw screen changed.
// For now we ignore 'r' and repaint the whole window.
func (impl *theImpl) rpc_flush(c *Client, r draw.Rectangle) {
	if impl == nil || impl.widget == nil {
		return
	}
	impl.widget.ScheduleRedraw()
}

// --- window.CloseHandler ---

func (impl *theImpl) Close() {
	// Close button on the Wayland window -> shut down devdraw.
	rpc_shutdown()
}

// --- window.WidgetHandler implementation ---
// Signatures MUST EXACTLY match the WidgetHandler interface in window/window.go.

// Resize(Widget *Widget, width int32, height int32, pwidth int32, pheight int32)
func (impl *theImpl) Resize(
	w *window.Widget,
	width int32,
	height int32,
	pwidth int32,
	pheight int32,
) {
	if width <= 0 || height <= 0 {
		return
	}

	r := draw.Rect(0, 0, int(width), int(height))

	impl.mu.Lock()
	defer impl.mu.Unlock()

	// If the size didn't change, keep the existing image.
	if impl.i != nil && impl.i.R == r {
		return
	}

	img, err := memdraw.AllocImage(r, ScreenPix)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wayland: AllocImage(%v) failed: %v\n", r, err)
		return
	}

	impl.i = img
	impl.rgba = memimageToRGBA(img)

	if impl.client != nil {
		impl.client.mouserect = img.R
		if impl.client.displaydpi == 0 {
			impl.client.displaydpi = 100
		}
		gfx_replacescreenimage(impl.client, img)
	}
}

// Redraw(Widget *Widget)
func (impl *theImpl) Redraw(w *window.Widget) {
	impl.mu.Lock()
	defer impl.mu.Unlock()

	if impl.i == nil || impl.rgba == nil || impl.win == nil {
		return
	}

	surf := impl.win.WindowGetSurface()
	if surf == nil {
		return
	}
	defer surf.Destroy()

	dst := surf.ImageSurfaceGetData()
	if dst == nil {
		return
	}

	dstStride := surf.ImageSurfaceGetStride()
	dstW := surf.ImageSurfaceGetWidth()
	dstH := surf.ImageSurfaceGetHeight()

	src := impl.rgba.Pix
	if src == nil {
		return
	}
	srcStride := impl.rgba.Stride
	srcW := impl.i.R.Dx()
	srcH := impl.i.R.Dy()

	// Clamp copy region to the smaller of the two images.
	wCopy := dstW
	if wCopy > srcW {
		wCopy = srcW
	}
	hCopy := dstH
	if hCopy > srcH {
		hCopy = srcH
	}
	if wCopy <= 0 || hCopy <= 0 {
		return
	}
	rowBytes := wCopy * 4

	for y := 0; y < hCopy; y++ {
		copy(
			dst[y*dstStride:y*dstStride+rowBytes],
			src[y*srcStride:y*srcStride+rowBytes],
		)
	}
}

// Enter(Widget *Widget, Input *Input, x float32, y float32)
func (impl *theImpl) Enter(
	w *window.Widget,
	in *window.Input,
	x float32,
	y float32,
) {
}

// Leave(Widget *Widget, Input *Input)
func (impl *theImpl) Leave(
	w *window.Widget,
	in *window.Input,
) {
}

// Motion(Widget *Widget, Input *Input, time uint32, x float32, y float32) int
func (impl *theImpl) Motion(
	w *window.Widget,
	in *window.Input,
	time uint32,
	x float32,
	y float32,
) int {
	return 0
}

// Button(Widget *Widget, Input *Input, time uint32, button uint32,
//
//	state wl.PointerButtonState, data WidgetHandler)
func (impl *theImpl) Button(
	w *window.Widget,
	in *window.Input,
	time uint32,
	button uint32,
	state wl.PointerButtonState,
	data window.WidgetHandler,
) {
}

// TouchUp(Widget *Widget, Input *Input, serial uint32, time uint32, id int32)
func (impl *theImpl) TouchUp(
	w *window.Widget,
	in *window.Input,
	serial uint32,
	time uint32,
	id int32,
) {
}

// TouchDown(Widget *Widget, Input *Input,
//
//	serial uint32, time uint32, id int32,
//	x float32, y float32)
func (impl *theImpl) TouchDown(
	w *window.Widget,
	in *window.Input,
	serial uint32,
	time uint32,
	id int32,
	x float32,
	y float32,
) {
}

// TouchMotion(Widget *Widget, Input *Input, time uint32, id int32, x float32, y float32)
func (impl *theImpl) TouchMotion(
	w *window.Widget,
	in *window.Input,
	time uint32,
	id int32,
	x float32,
	y float32,
) {
}

// TouchFrame(Widget *Widget, Input *Input)
func (impl *theImpl) TouchFrame(
	w *window.Widget,
	in *window.Input,
) {
}

// TouchCancel(Widget *Widget, Input *Input, width int32, height int32)
func (impl *theImpl) TouchCancel(
	w *window.Widget,
	in *window.Input,
	width int32,
	height int32,
) {
}

// Axis(Widget *Widget, Input *Input, time uint32, axis uint32, value float32)
func (impl *theImpl) Axis(
	w *window.Widget,
	in *window.Input,
	time uint32,
	axis uint32,
	value float32,
) {
}

// AxisSource(Widget *Widget, Input *Input, source uint32)
func (impl *theImpl) AxisSource(
	w *window.Widget,
	in *window.Input,
	source uint32,
) {
}

// AxisStop(Widget *Widget, Input *Input, time uint32, axis uint32)
func (impl *theImpl) AxisStop(
	w *window.Widget,
	in *window.Input,
	time uint32,
	axis uint32,
) {
}

// AxisDiscrete(Widget *Widget, Input *Input, axis uint32, discrete int32)
func (impl *theImpl) AxisDiscrete(
	w *window.Widget,
	in *window.Input,
	axis uint32,
	discrete int32,
) {
}

// PointerFrame(Widget *Widget, Input *Input)
func (impl *theImpl) PointerFrame(
	w *window.Widget,
	in *window.Input,
) {
}
