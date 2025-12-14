package main

import (
	"fmt"

	"github.com/neurlang/wayland/wl"
	client "github.com/neurlang/wayland/wlclient"
	ext "github.com/tuxx/wayland-ext-session-lock-go"
)

type Client struct {
	display     *wl.Display
	registry    *wl.Registry
	compositor  *wl.Compositor
	lockManager *ext.SessionLockManager
	lock        *ext.SessionLock
	surfaces    map[*wl.Output]*ext.SessionLockSurface
	outputs     map[uint32]*wl.Output
	locked      bool
	done        chan struct{}
}

type Surface struct {
	client  *Client
	lock    *ext.SessionLockSurface
	surface *wl.Surface
	serial  uint32
	width   uint32
	height  uint32
}

func NewClient() *Client {
	return &Client{
		surfaces: make(map[*wl.Output]*ext.SessionLockSurface),
		outputs:  make(map[uint32]*wl.Output),
		done:     make(chan struct{}),
	}
}

func (c *Client) Connect() error {
	var err error
	c.display, err = client.DisplayConnect(nil)
	if err != nil {
		return fmt.Errorf("Unable to connect to the compositor: %w", err)
	}
	c.registry, _ = c.display.GetRegistry()
	c.registry.AddGlobalHandler(c)
	c.registry.AddGlobalRemoveHandler(c)
	if err := client.DisplayRoundtrip(c.display); err != nil {
		return fmt.Errorf("DisplayRoundtrip() failed: %w", err)
	}
	if c.lockManager == nil {
		return fmt.Errorf("Missing ext-session-lock_manager-v1")
	}

	return nil
}

func (c *Client) HandleRegistryGlobal(ev wl.RegistryGlobalEvent) {
	if ev.Interface == "wl_compositor" {
		c.compositor = client.RegistryBindCompositorInterface(c.registry, ev.Name, 4)
	} else if ev.Interface == "ext_session_lock_manager_v1" && ev.Version >= 1 {
		c.lockManager = ext.BindSessionLockManager(c.registry, ev.Name, 1)
	} else if ev.Interface == "wl_output" {
		output := client.RegistryBindOutputInterface(c.registry, ev.Name, 3)
		c.outputs[ev.Name] = output
	}
}

func (c *Client) HandleRegistryGlobalRemove(ev wl.RegistryGlobalRemoveEvent) {
	if output, exists := c.outputs[ev.Name]; exists {
		fmt.Printf("Output removed (name: %d)\n", ev.Name)
		// If we have a lock surface for this output, destroy it
		if lockSurface, ok := c.surfaces[output]; ok {
			lockSurface.Destroy()
			delete(c.surfaces, output)
		}

		delete(c.outputs, ev.Name)
	}
}

func (c *Client) HandleSessionLockLocked(ev ext.SessionLockLockedEvent) {
	fmt.Println("Session is now locked!")
	c.locked = true
}

func (c *Client) HandleSessionLockFinished(ev ext.SessionLockFinishedEvent) {
	fmt.Println("Lock manager finished the session lock")
	if c.locked {
		c.lock.UnlockAndDestroy()
	} else {
		c.lock.Destroy()
	}
	close(c.done)
}

func (s *Surface) HandleSessionLockSurfaceConfigure(ev ext.SessionLockSurfaceConfigureEvent) {
	fmt.Printf("Configure: serial=%d, width=%d, height=%d\n", ev.Serial, ev.Width, ev.Height)
	s.serial = ev.Serial
	s.width = ev.Width
	s.height = ev.Height
	s.lock.AckConfigure(ev.Serial)
	createSolidColorBuffer(s.surface, s.width, s.height, 64, 0, 0) // Dark red
}

// Helper function to create a solid color buffer for a surface
func createSolidColorBuffer(surface *wl.Surface, width, height uint32, r, g, b uint8) {
	// In a real implementation, you would:
	// 1. Create a shared memory buffer
	// 2. Fill it with the solid color
	// 3. Attach it to the surface
	// 4. Commit the surface

	// This is a simplified version for the example
	fmt.Printf("Would create %dx%d buffer with color #%02x%02x%02x\n", width, height, r, g, b)
}
