package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

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
			name = *xfont[f].name
		}

	case Qsizedir:
		q.Type = plan9.QTDIR
		sz := 0
		if QSIZE(path) >= 0 && QSIZE(path) < len(sizes) {
			sz = sizes[QSIZE(path)]
		}
		if QANTIALIAS(path) == 1 {
			name = fmt.Sprintf("%da", sz)
		} else {
			name = fmt.Sprintf("%d", sz)
		}

	case Qfontfile:
		f := xfont[QFONT(path)]
		load(f)
		length = 11 + 1 + 11 + 1 + uint64(f.NFile)*(8+1+8+1+11+1)
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

func walk1(path uint64, name string) (plan9.Qid, bool) {
	var (
		q  plan9.Qid
		ok bool
	)

	switch QTYPE(path) {
	case Qroot:
		if name == ".." {
			path = qpath(Qroot, 0, 0, 0, 0)
			goto Found
		}
		for i := 0; i < nxfont; i++ {
			pp := qpath(Qfontdir, i, 0, 0, 0)
			qq := dostat(pp, nil)
			if qq.Path == 0 {
				continue
			}
			if strings.EqualFold(*xfont[i].name, name) {
				path = pp
				goto Found
			}
		}
		goto NotFound

	case Qfontdir:
		if name == ".." {
			path = qpath(Qroot, 0, 0, 0, 0)
			goto Found
		}

		szStr := name
		aa := 0
		if strings.HasSuffix(szStr, "a") {
			aa = 1
			szStr = strings.TrimSuffix(szStr, "a")
		}
		var szVal int
		if _, err := fmt.Sscanf(szStr, "%d", &szVal); err != nil {
			goto NotFound
		}
		szIdx := -1
		for i, v := range sizes {
			if v == szVal {
				szIdx = i
				break
			}
		}
		if szIdx < 0 {
			goto NotFound
		}
		path = qpath(Qsizedir, QFONT(path), szIdx, aa, 0)
		goto Found

	case Qsizedir:
		if name == ".." {
			path = qpath(Qfontdir, QFONT(path), 0, 0, 0)
			goto Found
		}
		if name == "font" {
			path = qpath(Qfontfile, QFONT(path), QSIZE(path), QANTIALIAS(path), 0)
			goto Found
		}
		if strings.HasPrefix(name, "x") && strings.HasSuffix(name, ".bit") {
			var lo int
			if _, err := fmt.Sscanf(name, "x%06x.bit", &lo); err != nil {
				goto NotFound
			}
			path = qpath(Qsubfontfile, QFONT(path), QSIZE(path), QANTIALIAS(path), lo/SubfontSize)
			goto Found
		}
		goto NotFound

	default:
		goto NotFound
	}

NotFound:
	q = dostat(path, nil)
	ok = false
	return q, ok

Found:
	q = dostat(path, nil)
	ok = true
	return q, ok
}

func rootgen(i int, d *plan9.Dir) int {
	if i >= nxfont {
		return -1
	}
	_ = dostat(qpath(Qfontdir, i, 0, 0, 0), d)
	return 0
}

func fontgen(fid *srv9p.Fid, i int, d *plan9.Dir) int {
	path := fid.Qid().Path
	if i >= 2*len(sizes) {
		return -1
	}
	_ = dostat(qpath(Qsizedir, QFONT(path), sizes[i/2], i&1, 0), d)
	return 0
}

func sizegen(fid *srv9p.Fid, i int, d *plan9.Dir) int {
	var f *XFont
	var path uint64

	path = fid.Qid().Path
	if i == 0 {
		path += Qfontfile - Qsizedir
		goto Done
	}
	i--
	f = xfont[QFONT(path)]
	load(f)
	if i < f.unit {
		path += Qsubfontfile - Qsizedir
		goto Done
	}
	return -1

Done:
	dostat(path, d)
	return 0
}

type font struct {
	srv srv9p.Server
}

func (srv *font) Attach(ctx context.Context, fid, afid *srv9p.Fid, user, aname string) (plan9.Qid, error) {
	q := dostat(qpath(Qroot, 0, 0, 0, 0), nil)
	fid.SetQid(q)
	return q, nil
}

func (srv *font) Walk(ctx context.Context, fid, nfid *srv9p.Fid, names []string) ([]plan9.Qid, error) {
	if len(names) == 0 {
		nfid.SetQid(fid.Qid())
		return []plan9.Qid{fid.Qid()}, nil
	}

	qids := make([]plan9.Qid, 0, len(names))
	cur := fid.Qid()
	for _, name := range names {
		next, ok := walk1(cur.Path, name)
		if !ok {
			return qids, errors.New("file not found")
		}
		qids = append(qids, next)
		cur = next
	}
	nfid.SetQid(cur)

	return qids, nil
}

func (srv *font) Open(ctx context.Context, fid *srv9p.Fid, mode uint8) error {
	if mode&plan9.OWRITE != 0 || mode&plan9.ORDWR != 0 {
		return errors.New("permission denied")
	}
	return nil
}

func (srv *font) Read(ctx context.Context, fid *srv9p.Fid, data []byte, offset int64) (int, error) {
	path := fid.Qid().Path
	switch QTYPE(path) {
	case Qroot:
		return dirpackage(data, offset, rootgen)
	case Qfontdir:
		return dirpackage(data, offset, func(i int, d *plan9.Dir) int {
			return fontgen(fid, i, d)
		})
	case Qsizedir:
		return dirpackage(data, offset, func(i int, d *plan9.Dir) int {
			return sizegen(fid, i, d)
		})
	case Qfontfile:
		if QFONT(path) < 0 || QFONT(path) >= nxfont || QSIZE(path) < 0 || QSIZE(path) >= len(sizes) {
			return 0, errors.New("bad font index")
		}
		f := *xfont[QFONT(path)]
		load(&f) // XXX: set ranges, File[], LoadHeight, etc
		if f.FontText == nil {
			var h, a int
			if f.loadheight != nil {
				f.loadheight(&f, sizes[QSIZE(path)], &h, &a)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%11d %11d\n", h, a)
			for i := 0; i < f.NFile; i++ {
				loRune := int(f.File[i]) * SubfontSize
				hiRune := loRune + SubfontSize - 1
				fmt.Fprintf(&b, "0x%06x 0x%06x x%06x.bit\n", loRune, hiRune, loRune)
			}
			f.FontText = []byte(b.String())
			f.NFontText = len(f.FontText)
		}
		if offset >= int64(f.NFontText) {
			return 0, io.EOF
		}
		n := copy(data, f.FontText[offset:])
		return n, nil

	case Qsubfontfile:
		// XXX(d)
		// build Memsubfont + Memimage and writes:
		//   - "chan minx miny maxx maxy "
		//   - raw image bytes
		//   - "n height ascent " + packed Fontchar info
		//
		//   * use go-text/render to rasterize glyphs into a bitmap,
		//   * copy bitmap in a memdraw.Image,
		//   * use Fontchar[] info,
		//   * stream header + image + Fontchar table here.
		//
		return 0, errors.New("subfont bitmaps not implemented yet")

	default:
		return 0, errors.New("invalid qid for read")
	}
}

func (srv *font) Stat(ctx context.Context, fid *srv9p.Fid) (*plan9.Dir, error) {
	var d plan9.Dir
	_ = dostat(fid.Qid().Path, &d)
	return &d, nil
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

	fs := &font{}
	fs.srv.Attach = fs.Attach
	fs.srv.Walk = fs.Walk
	fs.srv.Open = fs.Open
	fs.srv.Read = fs.Read
	fs.srv.Stat = fs.Stat

	memdraw.Init()
	loadfonts()

	srv9p.PostMountServe(srvname, mtpt, syscall.MBEFORE, args, r)
}

func dirpackage(buf []byte, offset int64, gen func(i int, d *plan9.Dir) int) (int, error) {
	var packed [][]byte
	for i := 0; ; i++ {
		var d plan9.Dir
		if gen(i, &d) < 0 {
			break
		}
		b, err := d.Bytes()
		if err != nil {
			return 0, err
		}
		packed = append(packed, b)
	}

	var total int
	for _, b := range packed {
		total += len(b)
	}
	if offset >= int64(total) {
		return 0, io.EOF
	}
	var all = make([]byte, total)
	var off int
	for _, b := range packed {
		copy(all[off:], b)
		off += len(b)
	}
	n := copy(buf, all[offset:])
	return n, nil
}
