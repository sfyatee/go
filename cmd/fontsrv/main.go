package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"9fans.net/go/draw/memdraw"
	"9fans.net/go/plan9"
	"9fans.net/go/plan9/srv9p"
)

func usage() {
	fmt.Fprint(os.Stderr, "usage: fontsrv [-m mtpt]")
	os.Exit(2)
}

const (
	Qroot = iota
	Qfontdir
	Qsizedir
	Qfontfile
	Qsubfontfile
)

func QTYPE(path uint64) int      { return int(path & 0xF) }
func QFONT(path uint64) int      { return int((path >> 4) & 0xFFFF) }
func QSIZE(path uint64) int      { return int((path >> 20) & 0xFF) }
func QANTIALIAS(path uint64) int { return int((path >> 28) & 0x1) }
func QRANGE(path uint64) int     { return int((path >> 29) & 0xFFFFFF) }

var sizes = []int{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 22, 24, 28}

func qpath(qtype, font, size, antialias, rng int) uint64 {
	return uint64(qtype) | uint64(font)<<4 | uint64(size)<<20 | uint64(antialias)<<28 | uint64(rng)<<29
}

func dostat(path uint64, d *plan9.Dir) plan9.Qid {
	var (
		name   string
		mode   plan9.Perm
		length uint64
	)

	q := plan9.Qid{
		Type: 0,
		Vers: 0,
		Path: path,
	}
	mode = plan9.DMDIR | 0555
	length = 0
	name = "???"

	switch QTYPE(path) {
	default:
		panic(fmt.Sprintf("dostat: bad QTYPE %#x", QTYPE(path)))

	case Qroot:
		q.Type = plan9.QTDIR
		name = "/"

	case Qfontdir:
		q.Type = plan9.QTDIR
		f := QFONT(path)
		if f < 0 || f >= nxfont {
			name = "?"
		} else {
			name = xfont[f].Name
		}

	case Qsizedir:
		q.Type = plan9.QTDIR
		si := QSIZE(path)
		aa := QANTIALIAS(path)
		sz := 0
		if si >= 0 && si < len(sizes) {
			sz = sizes[si]
		}
		if aa == 1 {
			name = fmt.Sprintf("%da", sz)
		} else {
			name = fmt.Sprintf("%d", sz)
		}

	case Qfontfile:
		// f =:w
		load(f)
		// length
		name = "font"

	case Qsubfontfile:
		rng := QRANGE(path)
		name = fmt.Sprintf("x%06x.bit", rng*SubfontSize)

	}

	if d != nil {
		d.Type = 0
		d.Dev = 0
		d.Qid = q
		d.Mode = mode
		d.Length = length
		d.Name = name
		d.Uid = "fontsrv"
		d.Gid = "fontsrv"
		d.Muid = ""
	}
	return q
}

type font struct {
	srv srv9p.Server
}

func (srv *font) Attach(ctx context.Context, fid, afid *srv9p.Fid, user, aname string) (plan9.Qid, error) {
}

func main() {
	mtpt := ""
	srvname := "font"

	// flag
	// flag
	flag.StringVar(&mtpt, "m", mtpt, "mount at `mtpt`")
	flag.StringVar(&srvname, "s", srvname, "post service at /srv/`name`")
	// flag
	flag.Usage = usage

	memdraw.Init()
	loadfonts()

	srv9p.PostMountServe(srvname, mtpt, syscall.MBEFORE, args, r)
}
