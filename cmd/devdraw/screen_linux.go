package main

import (
	"sync"

	"9fans.net/go/draw"
	"9fans.net/go/draw/memdraw"

	"github.com/neurlang/wayland/window"
	"github.com/neurlang/wayland/wl"
)

// ScreenPix is the pixel format used for the screen image.
// Only used by this file, but matches the usual 32-bit format.
var ScreenPix = draw.XRGB32

// rpcgfxlk is used by rpc_gfxdrawlock/rpc_gfxdrawunlock.
// devdraw.go calls these around operations that touch the real display.
var rpcgfxlk sync.Mutex

// snarfBuf is a trivial in-process clipboard implementation.
// It is wired into Trdsnarf/Twrsnarf in srv.go via rpc_getsnarf/rpc_putsnarf.
var snarfBuf []byte

// theImpl is the concrete implementation of ClientImpl for Linux/Wayland.
//
// It also implements window.WidgetHandler, so it can be used as the
// callback object for a Wayland window/widget if/when you wire that up.
type theImpl struct {
	// You can hang Wayland state here when you start using it:
	//   display *window.Display
	//   win     *window.Window
	//   widget  *window.Widget
}

// --- Ensure interface conformance at compile time ---

// ClientImpl (from devdraw.h.go).
var _ ClientImpl = (*theImpl)(nil)

// WidgetHandler (from github.com/neurlang/wayland/window).
var _ window.WidgetHandler = (*theImpl)(nil)

// --- Top-level entry points used by srv.go / devdraw.go ---

// gfx_main is called once from main (in srv.go) on the host/UI thread.
// For now we just tell the RPC side that graphics are "ready".
func gfx_main() {
	// In a real Wayland port, you'd:
	//   - create a window.Display
	//   - create a window.Window
	//   - start window.DisplayRun(display) in a goroutine
	//   - keep enough state to blit memdraw.Image into the Wayland surface.
	//
	// For now, do the minimal thing so the rest of devdraw can run.
	gfx_started()
}

// rpc_attach creates the initial screen image for the client and
// installs a theImpl as its ClientImpl.
//
// This is called in response to Tinit in srv.go.
func rpc_attach(c *Client, label, winsize string) (*memdraw.Image, error) {
	// TODO: parse winsize like "widthxheight" if you want.
	const (
		defaultW = 1024
		defaultH = 768
	)

	r := draw.Rect(0, 0, defaultW, defaultH)

	img, err := memdraw.AllocImage(r, ScreenPix)
	if err != nil {
		return nil, err
	}

	// Hook our implementation into the client.
	impl := &theImpl{}
	c.impl = impl

	// Install the memdraw.Image as the "screen" image.
	gfx_replacescreenimage(c, img)

	return img, nil
}

// rpc_gfxdrawlock / rpc_gfxdrawunlock wrap access to the real display.
// devdraw.go calls these around code that ultimately blits to the window.
func rpc_gfxdrawlock() {
	rpcgfxlk.Lock()
}

func rpc_gfxdrawunlock() {
	rpcgfxlk.Unlock()
}

// rpc_shutdown is called when client0 exits.
func rpc_shutdown() {
	// Nothing special yet. You can shut down Wayland here later.
}

// rpc_getsnarf and rpc_putsnarf back the snarf buffer used by Trdsnarf/Twrsnarf.
func rpc_getsnarf() []byte {
	// Return a copy so callers can't mutate our internal buffer accidentally.
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

// --- ClientImpl methods (called from devdraw.go via c.impl.…) ---

// These are intentionally stubbed out so the file compiles.
// You can fill them in with real Wayland behavior later.

func (impl *theImpl) rpc_resizeimg(c *Client) {
	// Called when the screen image changes size.
	// Once you have a Wayland window, you should update its buffer here.
}

func (impl *theImpl) rpc_resizewindow(c *Client, r draw.Rectangle) {
	// Called when the Plan 9 window wants to change size.
	// Typically you'd call win.ScheduleResize(...) on your Wayland window.
}

func (impl *theImpl) rpc_setcursor(c *Client, cur *draw.Cursor, cur2 *draw.Cursor2) {
	// TODO: map Plan 9 cursors onto Wayland cursors.
}

func (impl *theImpl) rpc_setlabel(c *Client, label string) {
	// TODO: set the Wayland window title: win.SetTitle(label).
}

func (impl *theImpl) rpc_setmouse(c *Client, p draw.Point) {
	// TODO: warp the pointer (if supported) or update internal mouse state.
}

func (impl *theImpl) rpc_topwin(c *Client) {
	// TODO: raise the window if the window API exposes that.
}

func (impl *theImpl) rpc_bouncemouse(c *Client, m draw.Mouse) {
	// Used when the mouse is outside the draw rectangle.
	// You could synthesize a Wayland motion event back into the widget here.
}

func (impl *theImpl) rpc_flush(c *Client, r draw.Rectangle) {
	// Called when a region of the memdraw screen image has changed
	// and needs to be pushed to the actual window.
	//
	// In a real implementation:
	//   - lock the memdraw.Image
	//   - copy the pixels in r into the Wayland surface buffer
	//   - commit the surface.
}

// --- window.WidgetHandler methods (Wayland event callbacks) ---

// The signatures are taken directly from github.com/neurlang/wayland/window v0.3.0.

func (impl *theImpl) Resize(
	w *window.Widget,
	width, height, pwidth, pheight int32,
) {
	// TODO: trigger a redraw and maybe tell devdraw about the new size.
}

func (impl *theImpl) Redraw(w *window.Widget) {
	// TODO: copy c.screenimage into the Wayland surface here.
}

func (impl *theImpl) Enter(
	w *window.Widget,
	in *window.Input,
	x, y float32,
) {
	// TODO: send a mouse-enter event into devdraw if needed.
}

func (impl *theImpl) Leave(
	w *window.Widget,
	in *window.Input,
) {
	// TODO: send a mouse-leave event into devdraw if needed.
}

func (impl *theImpl) Motion(
	w *window.Widget,
	in *window.Input,
	time uint32,
	x, y float32,
) int {
	// TODO: translate into a draw.Mouse and call gfx_mousetrack / gfx_keystroke.
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
	// TODO: forward pointer button events to devdraw.
}

func (impl *theImpl) TouchUp(
	w *window.Widget,
	in *window.Input,
	serial, time uint32,
	id int32,
) {
}

func (impl *theImpl) TouchDown(
	w *window.Widget,
	in *window.Input,
	serial, time uint32,
	id int32,
	x, y float32,
) {
}

func (impl *theImpl) TouchMotion(
	w *window.Widget,
	in *window.Input,
	time uint32,
	id int32,
	x, y float32,
) {
}

func (impl *theImpl) TouchFrame(
	w *window.Widget,
	in *window.Input,
) {
}

func (impl *theImpl) TouchCancel(
	w *window.Widget,
	width, height int32,
) {
}

func (impl *theImpl) Axis(
	w *window.Widget,
	in *window.Input,
	time, axis uint32,
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
	time, axis uint32,
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
