package main

import (
	"9fans.net/go/draw"
	"9fans.net/go/memdraw"
	"github.com/neurlang/wayland/libdecor"
	"github.com/neurlang/wayland/wl"
	"github.com/neurlang/wayland/wlclient"
	"github.com/neurlang/wayland/wlcursor"
	"github.com/neurlang/wayland/xdg"
	xkb "github.com/neurlang/wayland/xkbcommon"
)

func rpc_attach(client *Client, label, winsize string) (*memdraw.Image, error) {
}

func (*theImpl) rpc_setlabel(client *Client, label string) {
}

func rpc_shutdown() {
}

func (impl *theImpl) rpc_flush(client *Client, r draw.Rectangle) {
}

func (*theImpl) rpc_resizeimg(client *Client) {
	// TODO
}

var rpcgfxlk sync.Mutex

func rpc_gfxdrawlock() {
	rpcgfxlk.Lock()
}

func rpc_gfxdrawunlock() {
	rpcgfxlk.Unlock()
}

func (*theImpl) rpc_topwin(client *Client) {
}

func (*theImpl) rpc_resizewindow(client *Client, r draw.Rectangle) {
}

func (*theImpl) rpc_setmouse(client *Client, p draw.Point) {
}

func (*theImpl) rpc_setcursor(client *Client, c *draw.Cursor, c2 *draw.Cursor2) {
}

func rpc_getsnarf() []byte {
	return nil
}

func rpc_putsnarf(data []byte) {
}

func (*theImpl) rpc_bouncemouse(client *Client, m draw.Mouse) {
}
