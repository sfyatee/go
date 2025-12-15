package main

import (
	"flag"
	"fmt"
	"os"

	"9fans.net/go/plan9"
	"9fans.net/go/plan9/client"
	"github.com/jacobsa/fuse"
	"github.com/mdlayher/vsock"
)

func usage() {
	fmt.Fprint(os.Stderr, "usage: 9pfuse [-D] [-A attrtimeout [-a aname] address mtpt]")
}

func main() {
	flag.Usage = usage
}

func Getattr() {

}

func Setattr() {

}

func Mkdir() {

}

func Create() {

}

func Read() {

}

func Readdir() {

}
