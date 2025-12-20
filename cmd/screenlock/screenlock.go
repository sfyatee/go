package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/draw"
	"os"
	"os/signal"
	"syscall"
	"unsafe"

	"9fans.net/go/cmd/screenlock/inferno"
	sys "github.com/neurlang/wayland/os"
	"github.com/neurlang/wayland/wl"
	"github.com/neurlang/wayland/wlclient"
	ext "github.com/tuxx/wayland-ext-session-lock-go"
)

//go:embed bunny.bit
var pic []byte

func convertToRGBA(path string) (*image.RGBA, int, int, error) {
	src, _ := inferno.Decode(bytes.NewReader(pic))
	b := src.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)
	return rgba, b.Dx(), b.Dy(), nil
}

type LockClient struct {
	display    *wl.Display
	registry   *wl.Registry
	compositor *wl.Compositor
	shm        *wl.Shm

	lockManager *ext.SessionLockManager
	lock        *ext.SessionLock

	outputs  map[uint32]*wl.Output
	surfaces map[*wl.Output]*LockSurfaceState

	lockedReceived bool
	done           chan struct{}

	img *image.RGBA
	iw  int
	ih  int
}

type LockSurfaceState struct {
	lockSurface *ext.SessionLockSurface
	wlSurface   *wl.Surface
	width       uint32
	height      uint32
	serial      uint32

	// shm bits for latest frame
	pool   *wl.ShmPool
	buf    *wl.Buffer
	mapped []byte
	size   int
	fd     *os.File
}

func NewLockClient(imgPath string) (*LockClient, error) {
	img, iw, ih, err := convertToRGBA(imgPath)
	if err != nil {
		return nil, err
	}
	return &LockClient{
		outputs:  make(map[uint32]*wl.Output),
		surfaces: make(map[*wl.Output]*LockSurfaceState),
		done:     make(chan struct{}),
		img:      img,
		iw:       iw,
		ih:       ih,
	}, nil
}

func (c *LockClient) HandleRegistryGlobal(ev wl.RegistryGlobalEvent) {
	switch ev.Interface {
	case "wl_compositor":
		c.compositor = wlclient.RegistryBindCompositorInterface(c.registry, ev.Name, 4)
	case "wl_shm":
		c.shm = wlclient.RegistryBindShmInterface(c.registry, ev.Name, 1)
	case "wl_output":
		out := wlclient.RegistryBindOutputInterface(c.registry, ev.Name, 3)
		c.outputs[ev.Name] = out
	case "ext_session_lock_manager_v1":
		if ev.Version >= 1 {
			c.lockManager = ext.BindSessionLockManager(c.registry, ev.Name, 1)
		}
	}
}

func (c *LockClient) HandleRegistryGlobalRemove(ev wl.RegistryGlobalRemoveEvent) {
	if out, ok := c.outputs[ev.Name]; ok {
		// destroy per-output lock surface if present
		if st, ok := c.surfaces[out]; ok {
			_ = st.lockSurface.Destroy()
			c.destroyShm(st)
			delete(c.surfaces, out)
		}
		delete(c.outputs, ev.Name)
	}
}

func (c *LockClient) Connect() error {
	var err error
	c.display, err = wlclient.DisplayConnect(nil)
	if err != nil {
		return fmt.Errorf("DisplayConnect: %w", err)
	}

	c.registry, err = c.display.GetRegistry()
	if err != nil {
		return fmt.Errorf("GetRegistry: %w", err)
	}

	c.registry.AddGlobalHandler(c)
	c.registry.AddGlobalRemoveHandler(c)

	if err := wlclient.DisplayRoundtrip(c.display); err != nil {
		return fmt.Errorf("Roundtrip: %w", err)
	}

	if c.compositor == nil || c.shm == nil {
		return fmt.Errorf("missing wl_compositor or wl_shm")
	}
	if c.lockManager == nil {
		return fmt.Errorf("ext_session_lock_manager_v1 not available")
	}
	return nil
}

func (c *LockClient) LockSession() error {
	// lock
	lock, err := c.lockManager.Lock()
	if err != nil {
		return err
	}
	c.lock = lock
	ext.SessionLockAddListener(c.lock, c) // locked/finished events

	// create per-output lock surfaces :contentReference[oaicite:6]{index=6}
	for _, out := range c.outputs {
		if err := c.createLockSurface(out); err != nil {
			fmt.Println("warning:", err)
		}
	}

	return nil
}

func (c *LockClient) createLockSurface(output *wl.Output) error {
	wlsurf, err := c.compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("CreateSurface: %w", err)
	}

	ls, err := c.lock.GetLockSurface(wlsurf, output) // protocol request
	if err != nil {
		_ = wlsurf.Destroy()
		return fmt.Errorf("GetLockSurface: %w", err)
	}

	st := &LockSurfaceState{
		lockSurface: ls,
		wlSurface:   wlsurf,
	}
	c.surfaces[output] = st

	ext.SessionLockSurfaceAddListener(ls, &LockSurfaceHandler{client: c, st: st}) // configure event
	return nil
}

// ---- ext_session_lock handlers ----

func (c *LockClient) HandleSessionLockLocked(ev ext.SessionLockLockedEvent) {
	c.lockedReceived = true
	fmt.Println("Session is now locked.")
}

func (c *LockClient) HandleSessionLockFinished(ev ext.SessionLockFinishedEvent) {
	fmt.Println("Session lock finished.")
	if c.lock != nil {
		if c.lockedReceived {
			_ = c.lock.UnlockAndDestroy()
		} else {
			_ = c.lock.Destroy()
		}
	}
	close(c.done)
}

type LockSurfaceHandler struct {
	client *LockClient
	st     *LockSurfaceState
}

func (h *LockSurfaceHandler) HandleSessionLockSurfaceConfigure(ev ext.SessionLockSurfaceConfigureEvent) {
	// Must ack before committing content; protocol even defines an error for committing before first ack.
	h.st.serial = ev.Serial
	h.st.width = ev.Width
	h.st.height = ev.Height

	_ = h.st.lockSurface.AckConfigure(ev.Serial) // :contentReference[oaicite:10]{index=10}

	// draw one frame (jpg centered)
	if err := h.client.drawCenteredJPG(h.st); err != nil {
		fmt.Println("draw error:", err)
	}
}

// ---- shm drawing ----

func (c *LockClient) drawCenteredJPG(st *LockSurfaceState) error {
	w := int(st.width)
	h := int(st.height)
	if w <= 0 || h <= 0 {
		return nil
	}

	stride := w * 4
	size := stride * h

	// recreate shm if size changed
	if st.pool == nil || st.size != size {
		c.destroyShm(st)

		fd, err := sys.CreateAnonymousFile(int64(size))
		if err != nil {
			return fmt.Errorf("CreateAnonymousFile: %w", err)
		}
		st.fd = fd

		m, err := sys.Mmap(int(fd.Fd()), 0, size, sys.ProtRead|sys.ProtWrite, sys.MapShared)
		if err != nil {
			_ = fd.Close()
			return fmt.Errorf("Mmap: %w", err)
		}
		st.mapped = m
		st.size = size

		pool, err := c.shm.CreatePool(fd.Fd(), int32(size))
		if err != nil {
			_ = sys.Munmap(m)
			_ = fd.Close()
			return fmt.Errorf("CreatePool: %w", err)
		}
		st.pool = pool

		buf, err := pool.CreateBuffer(
			0,
			int32(w), int32(h),
			int32(stride),
			wl.ShmFormatXrgb8888,
		)
		if err != nil {
			_ = pool.Destroy()
			_ = sys.Munmap(m)
			_ = fd.Close()
			return fmt.Errorf("CreateBuffer: %w", err)
		}
		st.buf = buf
	}

	// clear black (XRGB: bytes are B,G,R,ignored in little-endian clients; we just fill BGRA-ish and use opaque alpha)
	for y := 0; y < h; y++ {
		row := st.mapped[y*stride : y*stride+stride]
		for i := 0; i < len(row); i += 4 {
			row[i+0] = 0
			row[i+1] = 0
			row[i+2] = 0
			row[i+3] = 0 // ignored by XRGB, but ok
		}
	}

	// center copy with clipping
	x0 := (w - c.iw) / 2
	y0 := (h - c.ih) / 2

	srcMinX, srcMinY := 0, 0
	srcMaxX, srcMaxY := c.iw, c.ih

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
		goto commit
	}

	for sy := srcMinY; sy < srcMaxY; sy++ {
		dy := y0 + (sy - srcMinY)
		dstRow := st.mapped[dy*stride : dy*stride+stride]
		srcRow := c.img.Pix[sy*c.img.Stride : sy*c.img.Stride+c.iw*4]
		for sx := srcMinX; sx < srcMaxX; sx++ {
			dx := x0 + (sx - srcMinX)
			si := sx * 4
			di := dx * 4
			r := srcRow[si+0]
			g := srcRow[si+1]
			b := srcRow[si+2]
			dstRow[di+0] = b
			dstRow[di+1] = g
			dstRow[di+2] = r
			dstRow[di+3] = 0
		}
	}

commit:
	_ = st.wlSurface.Attach(st.buf, 0, 0)
	_ = st.wlSurface.Damage(0, 0, int32(w), int32(h))
	_ = st.wlSurface.Commit()
	return nil
}

func (c *LockClient) destroyShm(st *LockSurfaceState) {
	if st.buf != nil {
		_ = st.buf.Destroy()
		st.buf = nil
	}
	if st.pool != nil {
		_ = st.pool.Destroy()
		st.pool = nil
	}
	if st.mapped != nil {
		_ = sys.Munmap(st.mapped)
		st.mapped = nil
	}
	if st.fd != nil {
		_ = st.fd.Close()
		st.fd = nil
	}
	st.size = 0
	_ = unsafe.Pointer(nil)
}

func main() {
	client, err := NewLockClient("dummy.jpg")
	if err != nil {
		fmt.Fprintln(os.Stderr, "jpeg:", err)
		os.Exit(1)
	}

	if err := client.Connect(); err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}

	// Ctrl+C unlock
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nReceived signal, unlocking...")
		if client.lock != nil && client.lockedReceived {
			_ = client.lock.UnlockAndDestroy() // :contentReference[oaicite:11]{index=11}
		}
	}()

	if err := client.LockSession(); err != nil {
		fmt.Fprintln(os.Stderr, "lock:", err)
		os.Exit(1)
	}

	// event loop (like the example)
	for {
		select {
		case <-client.done:
			return
		default:
			if err := wlclient.DisplayDispatch(client.display); err != nil {
				fmt.Fprintln(os.Stderr, "dispatch:", err)
				return
			}
		}
	}
}
