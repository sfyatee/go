package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"time"

	"9fans.net/go/draw"
)

//go:embed bunny.bit
var pic []byte

var (
	debug int
	blank int64

	display *draw.Display
	screen  *draw.Image
)

func getuser() string {
	u, err := user.Current()
	if err != nil {
		return "unknown"
	}
	return u.Username
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: screenlock [-d]\n")
	os.Exit(1)
}

// ^D, Delete, Enter, Backspace, ^U
func readline(buf []byte, nbuf int) string {
	if nbuf <= 1 {
		return ""
	}
	r := bufio.NewReader(os.Stdin)
	line, err := r.ReadString('\n')
	if err != nil {
		return ""
	}
	if len(line) >= nbuf {
		line = line[:nbuf-1]
	}
	copy(buf, line)
	if len(line) < nbuf {
		buf[len(line)] = 0
	}
	blank = time.Now().Unix()
	return line
}

func checkpassword() {
	fmt.Print("Press Enter to unlock (auth stubbed)...")
	_ = readline(make([]byte, 256), 256)
}

// func blanker(_ any) {
// 	if display == nil || screen == nil {
// 		return
// 	}

// 	for {
// 		if blank != 0 && time.Now().Unix()-blank >= 5 {
// 			blank = 0

// 			screen.Draw(screen.Bounds(), display.Black, nil, draw.ZP)
// 			if err := display.Flush(); err != nil && debug != 0 {
// 				log.Printf("blanker Flush: %v", err)
// 			}
// 		}
// 		time.Sleep(time.Second)
// 	}
// }

func grabmouse(_ any) {
	if display == nil || screen == nil {
		return
	}

	mc := display.InitMouse()
	if mc == nil {
		if debug != 0 {
			log.Printf("InitMouse failed in grabmouse")
		}
		return
	}
	r := screen.Bounds()

	center := draw.Pt(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2)
	display.MoveCursor(center)
	for {
		select {
		case m, ok := <-mc.C:
			if !ok {
				return
			}
			_ = m
			blank = time.Now().Unix()
			display.MoveCursor(center)
		case <-mc.Resize:
			display.MoveCursor(center)
		}
	}
}

func top(_ any) {
	for {
		display.Top()
		time.Sleep(time.Second)
	}
}

func lockscreen() {
	var err error

	display, err = draw.Init(nil, "", "screenlock", "")
	if err != nil {
		log.Fatalf("initdraw failed: %v", err)
	}
	screen = display.ScreenImage

	var i *draw.Image
	if len(pic) > 0 {
		i, err = display.ReadImage(bytes.NewReader(pic))
		if err != nil {
			i = nil
		}
	}
	if i != nil {
		r := screen.Bounds()
		p := draw.Pt(r.Max.X/2, r.Max.Y*2/3)
		dx := (screen.Bounds().Dx() - i.Bounds().Dx()) / 2
		r.Min.X += dx
		r.Max.X -= dx
		dy := (screen.Bounds().Dy() - i.Bounds().Dy()) / 2
		r.Min.Y += dy
		r.Max.Y -= dy
		screen.Draw(screen.Bounds(), display.Black, nil, draw.ZP)
		screen.Draw(r, i, nil, i.Bounds().Min)

		// identify the user on screen, centered
		s := fmt.Sprintf("user %s at %d:%02.2d", getuser(), time.Now().Hour(), time.Now().Minute())
		// mimic `subpt(p, Pt(stringwidth(font,"m")*strlen(s)/2, 0));`
		// textWidth := display.Font.StringWidth("m") * len(s) / 2
		p = draw.Pt(p.X-display.Font.StringWidth("m")*len(s)/2, p.Y)
		screen.StringBg(p, display.White, draw.ZP, display.Font, s, display.Black, draw.ZP)
	}
	if err := display.Flush(); err != nil && debug != 0 {
		log.Printf("display.Flush: %v", err)
	}

	go top(nil)
	go grabmouse(nil)
	// go blanker(nil)
}

func main() {
	d := flag.Bool("d", false, "debug")
	flag.Usage = usage
	flag.Parse()

	if *d {
		debug++
	}
	if flag.NArg() != 0 {
		usage()
	}

	lockscreen()
	checkpassword()
	if display != nil {
		_ = display.Close()
	}
}
