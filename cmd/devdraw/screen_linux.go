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

// Plan 9-style screen pixel format: matches other backends.
var ScreenPix = draw.XBGR32

// Single Wayland display for this devdraw instance.
var wlDisplay *window.Display

// Used by rpc_gfxdrawlock/rpc_gfxdrawunlock.
var rpcgfxlk sync.Mutex

// Simple in-process snarf buffer for now.
var snarfBuf []byte

func (*theImpl) rpc_bouncemouse(client *Client, m draw.Mouse) {
}

// theImpl is the per-client backend state and implements ClientImpl
// plus window.WidgetHandler and window.CloseHandler.
type theImpl struct {
	client *Client

	win    *window.Window
	widget *window.Widget

	i    *memdraw.Image
	rgba *image.RGBA

	mu sync.Mutex
}

// Ensure we satisfy ClientImpl; WidgetHandler/CloseHandler are enforced by usage.
var _ ClientImpl = (*theImpl)(nil)

// memimageToRGBA builds an image.RGBA view over the memdraw pixels.
func memimageToRGBA(i *memdraw.Image) *image.RGBA {
	if i == nil {
		return nil
	}
	return &image.RGBA{
		Pix:    i.BytesAt(i.R.Min),
		Stride: int(i.Width) * 4,
		Rect:   i.R,
	}
}

// -----------------------------------------------------------------------------
// Top-level driver glue
// -----------------------------------------------------------------------------

// gfx_main is called once from srv.go main().
// It must not return until devdraw is really done.
func gfx_main() {
	d, err := window.DisplayCreate(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wayland: DisplayCreate failed: %v\n", err)
		os.Exit(1)
	}
	wlDisplay = d

	// Start the RPC server (serveproc(client0) in srv.go).
	gfx_started()

	// Run the Wayland event loop here and block until Exit().
	window.DisplayRun(d)
}

// rpc_attach is called when the client does initdraw.
// We create a memdraw screen image and a Wayland window/widget wrapping it.
func rpc_attach(c *Client, label, winsize string) (*memdraw.Image, error) {
	if wlDisplay == nil {
		return nil, fmt.Errorf("wayland: display not initialised")
	}

	// If we already have a window for this client, just return its screen.
	if c.impl != nil {
		if impl, ok := c.impl.(*theImpl); ok && impl.i != nil {
			return impl.i, nil
		}
	}

	// Initial window rectangle – default 1024x768, optionally from winsize.
	r := draw.Rect(0, 0, 1024, 768)
	if winsize != "" {
		var haveMin bool
		if err := parsewinsize(winsize, &r, &haveMin); err != nil {
			// ignore parse error; keep default
		}
	}

	// Allocate memdraw backing store for the screen.
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

	// Create a Wayland toplevel window.
	win := window.Create(wlDisplay)
	if win == nil {
		return nil, fmt.Errorf("wayland: failed to create Wayland window")
	}
	impl.win = win

	if label != "" {
		win.SetTitle(label)
	}
	win.SetBufferType(window.BufferTypeShm)
	win.SetCloseHandler(impl) // implement Close() below
	win.SetKeyboardHandler(impl)
	// Attach our handler as the main widget for the window content.
	w := win.AddWidget(impl) // *theImpl must satisfy window.WidgetHandler
	impl.widget = w

	// Ask for an initial size and redraw; Wayland's internal resize logic
	// will call our Resize() and Redraw().
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

// -----------------------------------------------------------------------------
// ClientImpl hooks used from devdraw.go
// -----------------------------------------------------------------------------

// Called when the draw library has recreated the root memdraw image but the
// window size is unchanged. For the Wayland shm path, we just update our view.
func (impl *theImpl) rpc_resizeimg(c *Client) {
	if impl == nil || c == nil {
		return
	}
	impl.mu.Lock()
	defer impl.mu.Unlock()

	img := c.screenimage
	impl.i = img
	impl.rgba = memimageToRGBA(img)
	if img != nil {
		c.mouserect = img.R
	}
}

// Called when the client requests a resize (e.g. drawresizewindow()).
func (impl *theImpl) rpc_resizewindow(c *Client, r draw.Rectangle) {
	if impl == nil || impl.widget == nil {
		return
	}
	// Let the Wayland library drive the mechanics; this mirrors wayland.c:
	// - ScheduleResize -> pendingAllocation
	// - later Window.Run -> idleResize -> windowDoResize -> surfaceResize
	// - surfaceResize -> Widget.Resize(...) (our Resize below)
	impl.widget.ScheduleResize(int32(r.Dx()), int32(r.Dy()))
}

// Cursor, label, mouse, topwin: mostly stubs for now.

func (impl *theImpl) rpc_setcursor(c *Client, cur *draw.Cursor, cur2 *draw.Cursor2) {
	// TODO: hook to window cursors if you want Plan 9's fat cursors.
}

func (impl *theImpl) rpc_setlabel(c *Client, label string) {
	if impl == nil || impl.win == nil {
		return
	}
	impl.win.SetTitle(label)
}

func (impl *theImpl) rpc_setmouse(c *Client, p draw.Point) {
	// Wayland doesn’t let us warp the pointer, so ignore for now.
}

func (impl *theImpl) rpc_topwin(c *Client) {
	// Could be used to raise the window if the API ever exposes it.
}

// Called when some portion of the memdraw screen changed.
// We just schedule a redraw; Wayland will coalesce and call Redraw.
func (impl *theImpl) rpc_flush(c *Client, r draw.Rectangle) {
	if impl == nil || impl.widget == nil {
		return
	}
	impl.widget.ScheduleRedraw()
}

// rpc_gfxdrawlock / rpc_gfxdrawunlock are used around draw ops.
func rpc_gfxdrawlock() {
	rpcgfxlk.Lock()
}

func rpc_gfxdrawunlock() {
	rpcgfxlk.Unlock()
}

// Snarfing: for now, just keep a local buffer.
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

// -----------------------------------------------------------------------------
// window.CloseHandler
// -----------------------------------------------------------------------------

func (impl *theImpl) Close() {
	// Window close -> exit the display loop.
	rpc_shutdown()
}

// -----------------------------------------------------------------------------
// window.WidgetHandler implementation
// This is where we match the wayland.c resize mechanics.
// -----------------------------------------------------------------------------

// Resize is called when Wayland has decided on a new allocation for our widget.
// This is the point where we:
//   - allocate a new memdraw screen image of the new size
//   - call gfx_replacescreenimage so the Plan 9 side sees the resize
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

	// If nothing actually changed, keep the current image.
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

	// Tell devdraw that the root screen image changed, exactly like the
	// C backends do. This will:
	//   - swap c.screenimage
	//   - free the old one when no longer referenced
	//   - call gfx_mouseresized(c), which triggers drawrefreshscreen().
	if impl.client != nil {
		gfx_replacescreenimage(impl.client, img)
		impl.client.mouserect = img.R
		if impl.client.displaydpi == 0 {
			impl.client.displaydpi = 100
		}
	}
}

// Redraw is called when Wayland wants the window contents.
// We copy from the memdraw backing image into the current shm buffer.
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

	// Copy the overlapping region.
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

// Input-related methods: stubs for now, since you said we can skip
// keyboard/mouse for the moment. Signatures must match exactly.

func (impl *theImpl) Enter(
	w *window.Widget,
	in *window.Input,
	x float32,
	y float32,
) {
}

func (impl *theImpl) Leave(
	w *window.Widget,
	in *window.Input,
) {
}

func (impl *theImpl) Motion(
	w *window.Widget,
	in *window.Input,
	time uint32,
	x float32,
	y float32,
) int {
	return 0
}

func (impl *theImpl) Button(
	w *window.Widget,
	in *window.Input,
	time uint32,
	button uint32,
	state wl.PointerButtonState,
	data window.WidgetHandler,
) {
}

func (impl *theImpl) TouchUp(
	w *window.Widget,
	in *window.Input,
	serial uint32,
	time uint32,
	id int32,
) {
}

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

func (impl *theImpl) TouchMotion(
	w *window.Widget,
	in *window.Input,
	time uint32,
	id int32,
	x float32,
	y float32,
) {
}

func (impl *theImpl) TouchFrame(
	w *window.Widget,
	in *window.Input,
) {
}

// NOTE: TouchCancel in the window.WidgetHandler interface has *no* Input param.
func (impl *theImpl) TouchCancel(
	w *window.Widget,
	width int32,
	height int32,
) {
}

func (impl *theImpl) Axis(
	w *window.Widget,
	in *window.Input,
	time uint32,
	axis uint32,
	value float32,
) {
}

func (impl *theImpl) AxisSource(
	w *window.Widget,
	in *window.Input,
	source uint32,
) {
}

func (impl *theImpl) AxisStop(
	w *window.Widget,
	in *window.Input,
	time uint32,
	axis uint32,
) {
}

func (impl *theImpl) AxisDiscrete(
	w *window.Widget,
	in *window.Input,
	axis uint32,
	discrete int32,
) {
}

func (impl *theImpl) PointerFrame(
	w *window.Widget,
	in *window.Input,
) {
}

// KeyboardHandler implementation.
// This is called from the Wayland input layer when a key changes state.
func (impl *theImpl) Key(
	win *window.Window,
	in *window.Input,
	time uint32,
	key uint32,
	sym uint32,
	state wl.KeyboardKeyState,
	data window.WidgetHandler,
) {
	if impl == nil || impl.client == nil {
		return
	}
	// Only act on key press, like the shiny backend.
	if state != wl.KeyboardKeyStatePressed {
		return
	}

	// First try to turn the keysym into a Unicode rune.
	ch := in.GetRune(&sym, 0)

	if ch == 0 {
		// Non-printable; map special keys (F-keys, arrows, etc.).
		ch = symToRune(sym)
	} else if ch == '\r' {
		// Normalise CR to NL for Plan 9.
		ch = '\n'
	}

	if ch == 0 {
		return
	}

	gfx_keystroke(impl.client, ch)
}

func (impl *theImpl) Focus(win *window.Window, in *window.Input) {
	// We don't need to do anything special on focus gain/loss for devdraw.
}

// Subset of XKB/X11 keysyms we care about for special keys.
const (
	xkbSymBackSpace = 0xff08
	xkbSymTab       = 0xff09
	xkbSymReturn    = 0xff0d
	xkbSymEscape    = 0xff1b
	xkbSymDelete    = 0xffff

	xkbSymHome     = 0xff50
	xkbSymLeft     = 0xff51
	xkbSymUp       = 0xff52
	xkbSymRight    = 0xff53
	xkbSymDown     = 0xff54
	xkbSymPageUp   = 0xff55
	xkbSymPageDown = 0xff56
	xkbSymEnd      = 0xff57
	xkbSymInsert   = 0xff63

	xkbSymF1  = 0xffbe
	xkbSymF12 = 0xffc9

	xkbSymShiftL   = 0xffe1
	xkbSymShiftR   = 0xffe2
	xkbSymControlL = 0xffe3
	xkbSymControlR = 0xffe4
	xkbSymAltL     = 0xffe9
	xkbSymAltR     = 0xffea
)

// Map non-Unicode XKB keysyms into the runes expected by devdraw
// (draw.KeyFn, draw.KeyHome, draw.KeyLeft, draw.KeyAlt, etc.).
func symToRune(sym uint32) rune {
	switch sym {
	case xkbSymReturn:
		// Make sure Return is always newline.
		return '\n'
	case xkbSymBackSpace:
		return '\b'
	case xkbSymTab:
		return '\t'
	case xkbSymEscape:
		return 0x1b

	case xkbSymDelete:
		return draw.KeyDelete
	case xkbSymInsert:
		return draw.KeyInsert
	case xkbSymHome:
		return draw.KeyHome
	case xkbSymEnd:
		return draw.KeyEnd
	case xkbSymPageUp:
		return draw.KeyPageUp
	case xkbSymPageDown:
		return draw.KeyPageDown

	case xkbSymLeft:
		return draw.KeyLeft
	case xkbSymRight:
		return draw.KeyRight
	case xkbSymUp:
		return draw.KeyUp
	case xkbSymDown:
		return draw.KeyDown

	case xkbSymShiftL, xkbSymShiftR:
		return draw.KeyShift
	case xkbSymControlL, xkbSymControlR:
		return draw.KeyCtl
	case xkbSymAltL, xkbSymAltR:
		return draw.KeyAlt
	}

	// F1–F12 map to draw.KeyFn | n, like screen.go does.
	if sym >= xkbSymF1 && sym <= xkbSymF12 {
		n := int(sym - xkbSymF1 + 1)
		return draw.KeyFn | rune(n)
	}

	return 0
}
