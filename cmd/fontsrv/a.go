package main

import (
	"unicode/utf8"
)

var xfont *XFont

const (
	SubfontSize = 32
	MaxSubfont  = utf8.MaxRune / SubfontSize
)

type XFont struct {
	name    *string
	loaded  bool
	unit    int
	height  float64
	originY float64
}
