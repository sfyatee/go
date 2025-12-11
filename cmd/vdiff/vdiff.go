package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"9fans.net/go/draw"
	"9fans.net/go/plan9"
	"9fans.net/go/plumb"
)

type Block struct {
	r     draw.Rectangle
	sr    draw.Rectangle
	v     bool
	f     string
	lines []*Line
}

type Line struct {
	t int
	n int
	s string
}

type Col struct {
	bg *draw.Image
	fg *draw.Image
}

type Patch struct {
	name   string
	blocks []*Block
}

const (
	Lfile = iota
	Lsep
	Ladd
	Ldel
	Lnone
	Ncols

	Lterm = -1
	Lhash = -2
)

const (
	Mcollapse = iota
	Mexpand
	Nmenu
)

const (
	Scrollwidth = 12
	Scrollgap   = 2
	Margin      = 8
	Hpadding    = 4
	Vpadding    = 2
)

var (
	display    *draw.Display
	screen     *draw.Image
	font       *draw.Font
	mctl       *draw.Mousectl
	kctl       *draw.Keyboardctl
	sr         draw.Rectangle
	scrollr    draw.Rectangle
	scrposr    draw.Rectangle
	viewr      draw.Rectangle
	cols       [Ncols]Col
	scrlcol    Col
	trlcol     *draw.Image
	bord       *draw.Image
	expander   [2]*draw.Image
	fb         *draw.Image
	totalh     int
	viewh      int
	scrollsize int
	offset     int
	lineh      int
	scrolling  bool
	oldbuttons int
	patches    []*Patch
	cur        *Patch
	maxlength  int
	Δpan       int
	nstrip     int
	ellipsis   = "..."
	ellipsisw  int
	spacew     int
	black      bool
)

func eallocimage(r draw.Rectangle, repl bool, c draw.Color) *draw.Image {
	i, err := display.AllocImage(r, screen.Pix, repl, c)
	if err != nil {
		log.Fatalf("allocimage: %v", err)
	}
	return i
}

func plumber(f string, l int) {
	for i := 0; i < nstrip; i++ {
		if p := strings.IndexRune(f, '/'); p >= 0 {
			f = f[p+1:]
		}
	}
	fd, err := plumb.Open("send", plan9.OWRITE)
	if err != nil {
		return
	}
	defer fd.Close()
	wd, _ := os.Getwd()
	addr := fmt.Sprintf("%s:%d", f, l)
	msg := &plumb.Message{
		Src:  "vdiff",
		Dst:  "edit",
		Dir:  wd,
		Type: "text",
		Data: []byte(addr),
	}
	_ = msg.Send(fd)
}

func renderline(b *draw.Image, r draw.Rectangle, pad int, lt int, ls string) {
	b.Draw(r, cols[lt].bg, nil, draw.ZP)
	p := draw.Pt(r.Min.X+pad+Hpadding, r.Min.Y+(r.Dy()-font.Height)/2)
	off := Δpan / spacew
	hastrl := false
	var trlr draw.Rectangle
	nc := -1
	tab := 0
	for i := 0; i < len(ls) || tab > 0; {
		if tab <= 0 && i < len(ls) && ls[i] == '\t' {
			tab = 4 - (nc+1)%4
			i++
		}
		if tab > 0 {
			p0 := p
			if off <= 0 {
				p = b.String(p, cols[lt].bg, draw.ZP, font, "█")
			}
			if hastrl {
				trlr.Max = draw.Pt(p.X, p.Y+font.Height)
			} else {
				trlr.Min = p0
				hastrl = true
			}
			nc++
			off--
			tab--
			continue
		}
		if p.X+Hpadding+spacew+ellipsisw >= b.R.Max.X {
			b.String(p, cols[lt].fg, draw.ZP, font, ellipsis)
			break
		}
		rn, sz := utf8.DecodeRuneInString(ls[i:])
		if rn == utf8.RuneError && sz == 1 {
			// just show as replacement char
		}
		i += sz
		p0 := p
		if off <= 0 {
			p = b.Runes(p, cols[lt].fg, draw.ZP, font, []rune{rn})
		}
		if unicode.IsSpace(rn) {
			if hastrl {
				trlr.Max = draw.Pt(p.X, p.Y+font.Height)
			} else {
				trlr.Min = p0
				hastrl = true
			}
		} else if hastrl {
			hastrl = false
		}
		nc++
		off--
	}

	if hastrl {
		b.Draw(trlr, trlcol, nil, draw.ZP)
	}
}

func renderblock(b *Block, sr draw.Rectangle) {
	r := sr.Inset(1)
	pad := 0
	if b.f != "" {
		pad = Margin
		lr := r
		lr.Max.Y = lr.Min.Y + lineh
		br := expander[0].R.Add(draw.Pt(lr.Min.X+Hpadding, lr.Min.Y+Vpadding))
		fb.Border(sr, 1, bord, draw.ZP)
		renderline(fb, lr, expander[0].R.Dx()+Hpadding, Lfile, b.f)
		fb.Draw(br, expander[boolToInt(b.v)], nil, draw.ZP)
		r.Min.Y += lineh
	}
	if !b.v {
		return
	}
	for i, l := range b.lines {
		lr := draw.Rect(r.Min.X, r.Min.Y+i*lineh, r.Max.X, r.Min.Y+(i+1)*lineh)
		renderline(fb, lr, pad, l.t, l.s)
	}
}

func redraw() {
	fb.Draw(fb.R, cols[Lnone].bg, nil, draw.ZP)
	fb.Draw(scrollr, scrlcol.bg, nil, draw.ZP)
	if viewh < totalh {
		h := int(float64(viewh) / float64(totalh) * float64(scrollr.Dy()))
		y := int(float64(offset) / float64(totalh) * float64(scrollr.Dy()))
		ye := scrollr.Min.Y + y + h
		if ye >= scrollr.Max.Y {
			ye = scrollr.Max.Y
		}
		scrposr = draw.Rect(scrollr.Min.X, scrollr.Min.Y+y+1, scrollr.Max.X-1, ye)
	} else {
		scrposr = draw.Rect(scrollr.Min.X, scrollr.Min.Y, scrollr.Max.X-1, scrollr.Max.Y)
	}
	fb.Draw(scrposr, scrlcol.fg, nil, draw.ZP)
	vmin := viewr.Min.Y + offset
	vmax := viewr.Max.Y + offset
	clipr := fb.R
	fb.ReplClipr(false, viewr)
	for _, b := range cur.blocks {
		if b.sr.Min.Y <= vmax && b.sr.Max.Y >= vmin {
			renderblock(b, b.sr.Add(draw.Pt(0, -offset)))
		}
	}
	fb.ReplClipr(false, clipr)
	screen.Draw(screen.R, fb, nil, fb.R.Min)
	_ = display.Flush()
}

func pan(off int) {
	max := scrollr.Dx() + Margin + Hpadding + maxlength*spacew + 2*ellipsisw + Hpadding + Margin - cur.blocks[0].r.Dx()/2
	Δpan += off * spacew
	if Δpan < 0 || max <= 0 {
		Δpan = 0
	} else if Δpan > max {
		Δpan = max
	}
	redraw()
}

func clampoffset(off int) {
	if offset < 0 {
		offset = 0
	}
	if offset+viewh > totalh {
		if off > 0 {
			offset = totalh - viewh
		} else {
			offset = 0
		}
	}
}

func scroll(off int) {
	if off < 0 && offset <= 0 {
		return
	}
	if off > 0 && offset+viewh > totalh {
		return
	}
	offset += off
	clampoffset(off)
	redraw()
}

func blockresize(b *Block) {
	w := viewr.Dx() - 2 /* add 2 for border */
	h := 2
	if b.f != "" {
		h += lineh
	}
	if b.v {
		h += len(b.lines) * lineh
	}
	b.r = draw.Rect(0, 0, w, h)
}

func eresize(new bool) {
	if new {
		if err := display.Attach(draw.RefNone); err != nil {
			log.Fatalf("cannot reattach: %v", err)
		}
		screen = display.ScreenImage
	}
	sr = screen.R
	scrollr = sr
	scrollr.Max.X = scrollr.Min.X + Scrollwidth + Scrollgap
	listr := sr
	listr.Min.X = scrollr.Max.X
	viewr = listr.Inset(Margin)
	viewh = viewr.Dy()
	lineh = Vpadding + font.Height + Vpadding
	totalh = -Margin + Vpadding + 1
	p := viewr.Min.Add(draw.Pt(0, totalh))
	for _, b := range cur.blocks {
		blockresize(b)
		b.sr = b.r.Add(p)
		p.Y += Margin + b.r.Dy()
		totalh += Margin + b.r.Dy()
	}
	totalh = totalh - Margin + Vpadding
	scrollsize = viewh / 2
	if viewh <= totalh {
		clampoffset(1)
	} else {
		clampoffset(0)
	}
	fb = eallocimage(screen.R, false, draw.Black)
	redraw()
}

func ekeyboard(k rune) {
	switch k {
	case 'q', 0x7f:
		os.Exit(0)
	case draw.KeyHome:
		scroll(-totalh)
	case draw.KeyEnd:
		scroll(totalh)
	case draw.KeyPageUp:
		scroll(-viewh)
	case draw.KeyPageDown:
		scroll(viewh)
	case draw.KeyUp:
		scroll(-scrollsize)
	case draw.KeyDown:
		scroll(scrollsize)
	case draw.KeyLeft:
		pan(-4)
	case draw.KeyRight:
		pan(4)
	}
}

func genmenu(i int, buf []byte) ([]byte, bool) {
	switch i {
	case Mcollapse:
		return []byte("collapse"), true
	case Mexpand:
		return []byte("expand"), true
	default:
		i -= Nmenu
		if i >= len(patches) || len(patches) == 1 {
			return nil, false
		}
		if patches[i].name == "" {
			patches[i].name = fmt.Sprintf("%d", i)
		}
		return []byte(patches[i].name), true
	}
}

func blockmouse(b *Block, m draw.Mouse) {
	n := (m.Y + offset - b.sr.Min.Y) / lineh
	if n == 0 && b.f != "" && (m.Buttons&1) != 0 {
		b.v = !b.v
		eresize(false)
		return
	}
	if n > 0 && (m.Buttons&4) != 0 {
		if idx := n - 1; idx >= 0 && idx < len(b.lines) {
			l := b.lines[idx]
			if l.t != Lsep {
				plumber(b.f, l.n)
			}
		}
	}
}

func collapse(v int) {
	for _, b := range cur.blocks {
		if b.f != "" {
			b.v = v != 0
		}
	}
	eresize(false)
}

func emouse(m draw.Mouse) {
	if oldbuttons == 0 && m.Buttons != 0 && m.Point.In(scrollr) {
		scrolling = true
	} else if m.Buttons == 0 {
		scrolling = false
	}

	n := (m.Y - viewr.Min.Y - Margin) / lineh * lineh
	if scrolling {
		if (m.Buttons & 1) != 0 {
			scroll(-n)
			oldbuttons = m.Buttons
			return
		} else if (m.Buttons & 2) != 0 {
			offset = (m.Y - scrollr.Min.Y) * totalh / scrollr.Dy()
			offset = offset / lineh * lineh
			if viewh <= totalh {
				clampoffset(1)
			} else {
				clampoffset(0)
			}
			redraw()
		} else if (m.Buttons & 4) != 0 {
			scroll(n)
			oldbuttons = m.Buttons
			return
		}
	} else if !scrolling && (m.Buttons&2) != 0 {
		menu := &draw.Menu{Item: nil, Gen: genmenu}
		hit := draw.MenuHit(2, mctl, menu, nil)
		switch hit {
		case -1:
		case Mexpand, Mcollapse:
			collapse(hit)
		default:
			hit -= Nmenu
			if hit >= 0 && hit < len(patches) && cur != patches[hit] {
				cur = patches[hit]
				eresize(false)
			}
		}
	} else if (m.Buttons & 8) != 0 {
		scroll(-n)
	} else if (m.Buttons & 16) != 0 {
		scroll(n)
	} else if (oldbuttons^m.Buttons) != 0 && m.Point.In(viewr) {
		for _, b := range cur.blocks {
			if m.Point.Add(draw.Pt(0, offset)).In(b.sr) {
				blockmouse(b, m)
				break
			}
		}
	}
	oldbuttons = m.Buttons
}

func initcol(c *Col, fg, bg draw.Color) {
	r := draw.Rect(0, 0, 1, 1)
	c.fg = eallocimage(r, true, fg)
	c.bg = eallocimage(r, true, bg)
}

func initcols(black bool) {
	r := draw.Rect(0, 0, 1, 1)
	if black {
		mask := ^uint32(0xff)
		bord = eallocimage(r, true, draw.Color(0x888888FF^mask))
		initcol(&scrlcol, draw.Black, draw.Color(0x999999FF^mask))
		initcol(&cols[Lfile], draw.White, 0x333333FF)
		initcol(&cols[Lsep], draw.Black, draw.PurpleBlue)
		initcol(&cols[Ladd], draw.White, 0x002800FF)
		initcol(&cols[Ldel], draw.White, 0x3F0000FF)
		initcol(&cols[Lnone], draw.White, draw.Black)
		trlcol = eallocimage(r, true, 0x9F0000FF)
	} else {
		bord = eallocimage(r, true, 0x888888FF)
		initcol(&scrlcol, draw.White, 0x999999FF)
		initcol(&cols[Lfile], draw.Black, 0xEFEFEFFF)
		initcol(&cols[Lsep], draw.Black, 0xEAFFFFFF)
		initcol(&cols[Ladd], draw.Black, 0xE6FFEDFF)
		initcol(&cols[Ldel], draw.Black, 0xFFEEF0FF)
		initcol(&cols[Lnone], draw.Black, draw.White)
		trlcol = eallocimage(r, true, 0xFF8890FF)
	}
}

func initicons() {
	h := font.Height
	w := h
	expander[0] = eallocimage(draw.Rect(0, 0, w, h), false, draw.NoFill)
	expander[0].Draw(expander[0].R, cols[Lfile].bg, nil, draw.ZP)
	p := []draw.Point{
		{X: w / 4, Y: h / 4},
		{X: w / 4, Y: 3 * h / 4},
		{X: 3 * w / 4, Y: h / 2},
		{X: w / 4, Y: h / 4},
	}
	expander[0].FillPoly(p, -1, bord, draw.ZP)
	expander[1] = eallocimage(draw.Rect(0, 0, w, h), false, draw.NoFill)
	expander[1].Draw(expander[1].R, cols[Lfile].bg, nil, draw.ZP)
	p = []draw.Point{
		{X: w / 4, Y: h / 4},
		{X: 3 * w / 4, Y: h / 4},
		{X: w / 2, Y: 3 * h / 4},
		{X: w / 4, Y: h / 4},
	}
	expander[1].FillPoly(p, -1, bord, draw.ZP)
	display.Flush()
}

func addblock() *Block {
	b := &Block{v: true}
	cur.blocks = append(cur.blocks, b)
	return b
}

func addline(b *Block, t, n int, s string) {
	l := &Line{t: t, n: n, s: s}
	b.lines = append(b.lines, l)
	if len(s) > maxlength {
		maxlength = len(s)
	}
}

func linetype(text string) int {
	if strings.HasPrefix(text, "⑨") {
		return Lterm
	}
	if strings.HasPrefix(text, "diff") {
		return Lhash
	}
	if strings.HasPrefix(text, "+++") {
		return Lfile
	}
	if strings.HasPrefix(text, "---") {
		if len(text) > 4 {
			return Lfile
		}
	}
	if strings.HasPrefix(text, "@@") {
		return Lsep
	}
	if strings.HasPrefix(text, "+") {
		return Ladd
	}
	if strings.HasPrefix(text, "-") {
		return Ldel
	}
	return Lnone
}

func lineno(s string) int {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return -1
	}
	p := strings.TrimPrefix(fields[2], "+")
	if p == "" {
		return -1
	}
	if i := strings.IndexByte(p, ','); i >= 0 {
		p = p[:i]
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return -1
	}
	return n
}

func parse(r io.Reader, name string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		b       *Block
		n       int
		ab      bool
		gotterm bool
	)

	patch := func() {
		p := &Patch{name: name}
		patches = append(patches, p)
		cur = p
		b = addblock()
		n = 0
		ab = false
	}
	patch()

	for sc.Scan() {
		s := sc.Text()
		t := linetype(s)

		switch t {
		case Lterm:
			gotterm = true
			patch()

		case Lfile:
			if s[0] == '-' {
				b = addblock()
				f := s[4:]
				if strings.HasPrefix(f, "a/") {
					ab = true
					f = f[1:]
				}
				b.f = f
			} else if s[0] == '+' {
				f := s[4:]
				if ab && strings.HasPrefix(f, "b/") {
					f = f[1:]
					if _, err := os.Stat(f); err != nil && len(f) > 0 {
						f = f[1:]
					}
				}
				if i := strings.IndexByte(f, '\t'); i >= 0 {
					f = f[:i]
				}
				if f != "/dev/null" {
					b.f = f
				}
			}

		case Lsep:
			n = lineno(s) - 1 /* -1 as the separator is not an actual line */
			addline(b, Lsep, n, s)

		case Lhash:
			addline(b, Lnone, n, s)
			fields := strings.Fields(s)
			if len(fields) >= 3 {
				id := fields[len(fields)-1]
				if name != "" {
					cur.name = fmt.Sprintf("%s %.*s", name, 9, id)
				} else {
					cur.name = fmt.Sprintf("%.*s", 9, id)
				}
			}

		case Ladd:
			n++
			addline(b, Ladd, n, s)
		case Ldel:
			addline(b, Ldel, n, s)
		case Lnone:
			n++
			addline(b, Lnone, n, s)
		default:
			// nothing
		}
	}

	if gotterm && len(patches) > 0 {
		patches = patches[:len(patches)-1]
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-b] [-p nstrip] [patches...]\n", filepath.Base(os.Args[0]))
	os.Exit(2)
}

func main() {
	flag.BoolVar(&black, "b", false, "white text on a black background")
	flag.IntVar(&nstrip, "p", 0, "remove elements from the path before plumbing")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() == 0 {
		parse(os.Stdin, "")
	} else {
		for _, name := range flag.Args() {
			f, err := os.Open(name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "open: %v\n", err)
				os.Exit(1)
			}
			parse(f, name)
			f.Close()
		}
	}

	if len(patches) == 0 || (len(patches) == 1 && len(patches[0].blocks) == 1 && len(patches[0].blocks[0].lines) == 0) {
		fmt.Fprintln(os.Stderr, "no diff")
		return
	}
	cur = patches[0]

	d, err := draw.Init(nil, "", "vdiff", "")
	if err != nil {
		log.Fatal("initdraw: ", err)
	}
	display = d
	screen = display.ScreenImage
	font = display.Font
	mctl = display.InitMouse()
	if mctl == nil {
		log.Fatal("initmouse: failed")
	}
	kctl = display.InitKeyboard()
	if kctl == nil {
		log.Fatal("initkeyboard: failed")
	}
	initcols(black)
	initicons()
	spacew = font.StringWidth(" ")
	ellipsisw = font.StringWidth(ellipsis)
	eresize(false)
	for {
		select {
		case m := <-mctl.C:
			emouse(m)
		case <-mctl.Resize:
			eresize(true)
		case k := <-kctl.C:
			ekeyboard(k)
		}
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
