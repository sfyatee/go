package main

import (
	"unicode/utf8"
)

var xfont []*XFont
var nxfont int

const (
	SubfontSize = 32
	MaxSubfont  = utf8.MaxRune / SubfontSize
)

type XFont struct {
	name       *string
	loaded     bool
	Range      [MaxSubfont]bool
	File       [MaxSubfont]uint16
	NFile      int
	unit       int
	height     float64
	originY    float64
	loadheight func(f *XFont, size int, height, ascent *int)
	FontText   []byte
	NFontText  int
}
