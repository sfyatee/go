package main

import (
	"flag"
	"fmt"
	"os"

	"9fans.net/go/draw/memdraw"
	"9fans.net/go/plan9/srv9p"
)

func usage() {
	fmt.Fprint(os.Stderr, "usage: fontsrv [-m mtpt]")
	os.Exit(2)
}

func main() {
	mtpt := ""
	srvname := "font"

	// flag
	// flag
	flag.StringVar(&mtpt, "m", mtpt, "mtpt")
	flag.StringVar(&srvname, "s", srvname, "srvname")
	// flag
	flag.Usage = usage

	memdraw.Init()
	loadfonts()

	srv9p.PostMountServe(*srvname, *mtpt, syscall.MBEFORE, args, rot13Server)
}
