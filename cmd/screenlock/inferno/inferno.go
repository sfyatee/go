package inferno

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"strconv"
	"strings"
	"unicode"
)

var (
	// ErrUnsupportedFormat is returned when the image uses a channel or layout
	// this decoder does not (yet) support.
	ErrUnsupportedFormat = errors.New("infernoimage: unsupported image format")
)

// Decode reads an Inferno image(6) file from r and returns it as image.NRGBA.
func Decode(r io.Reader) (image.Image, error) {
	br := bufio.NewReader(r)

	// Detect optional compressed prefix.
	peek, err := br.Peek(11)
	if err != nil {
		return nil, fmt.Errorf("infernoimage: peek: %w", err)
	}

	compressed := string(peek) == "compressed\n"
	if compressed {
		// Consume prefix and then read header as normal.
		if _, err := br.Discard(11); err != nil {
			return nil, fmt.Errorf("infernoimage: discard compressed prefix: %w", err)
		}
	}

	chanStr, minx, miny, maxx, maxy, err := readHeader(br)
	if err != nil {
		return nil, err
	}

	channels, depth, err := parseChanOrLDepth(chanStr)
	if err != nil {
		return nil, err
	}

	if depth <= 0 {
		return nil, fmt.Errorf("infernoimage: invalid depth %d", depth)
	}

	width := maxx - minx
	height := maxy - miny
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("infernoimage: non-positive bounds (%d,%d)-(%d,%d)", minx, miny, maxx, maxy)
	}

	bytesPerRow := (width*depth + 7) / 8
	totalBytes := bytesPerRow * height

	var pixelBytes []byte
	if compressed {
		pixelBytes, err = readCompressedPixels(br, totalBytes)
		if err != nil {
			return nil, err
		}
		if len(pixelBytes) < totalBytes {
			return nil, fmt.Errorf("infernoimage: compressed data too short: have %d, want %d", len(pixelBytes), totalBytes)
		}
		// Ignore any extra decompressed bytes beyond totalBytes, if present.
		pixelBytes = pixelBytes[:totalBytes]
	} else {
		// Uncompressed: just read raw bytes.
		pixelBytes = make([]byte, totalBytes)
		if _, err := io.ReadFull(br, pixelBytes); err != nil {
			return nil, fmt.Errorf("infernoimage: read pixel data: %w", err)
		}
	}

	// Create output image.
	rect := image.Rect(minx, miny, maxx, maxy)
	out := image.NewNRGBA(rect)

	if depth < 8 {
		if len(channels) != 1 || (channels[0].Name != 'k' && channels[0].Name != 'm') {
			return nil, ErrUnsupportedFormat
		}
		// 1/2/4-bit greyscale/colour-mapped (treated as greyscale).
		maxVal := (1 << depth) - 1
		for y := 0; y < height; y++ {
			row := pixelBytes[y*bytesPerRow : (y+1)*bytesPerRow]
			for x := 0; x < width; x++ {
				bitOff := x * depth
				val := getBits(row, bitOff, depth)
				grey := uint8(int(val) * 255 / maxVal)
				i := out.PixOffset(minx+x, miny+y)
				out.Pix[i+0] = grey
				out.Pix[i+1] = grey
				out.Pix[i+2] = grey
				out.Pix[i+3] = 255
			}
		}
		return out, nil
	}

	// depth >= 8
	if depth%8 != 0 {
		return nil, ErrUnsupportedFormat
	}
	bytesPerPixel := depth / 8

	// Ensure we only handle 1,2,4,8-bit channels and that all 8-bit channels
	// are indeed 8-bit.
	for _, ch := range channels {
		if ch.Bits != 1 && ch.Bits != 2 && ch.Bits != 4 && ch.Bits != 8 {
			return nil, ErrUnsupportedFormat
		}
	}

	// Optimised paths for simple 8-bit greyscale.
	if len(channels) == 1 && channels[0].Bits == 8 &&
		(channels[0].Name == 'k' || channels[0].Name == 'm') {
		for y := 0; y < height; y++ {
			row := pixelBytes[y*bytesPerRow : (y+1)*bytesPerRow]
			for x := 0; x < width; x++ {
				grey := row[x]
				i := out.PixOffset(minx+x, miny+y)
				out.Pix[i+0] = grey
				out.Pix[i+1] = grey
				out.Pix[i+2] = grey
				out.Pix[i+3] = 255
			}
		}
		return out, nil
	}

	// General 8-bit-per-channel path using little-endian byte order:
	// For 8-bit channels, bytes are stored in reverse channel order.
	// Example from spec: "r8g8b8" pixels have bytes B,G,R in the file.
	// So we build a reversed slice of 8-bit channels and map each byte.
	var eightBitChans []channelDesc
	for _, ch := range channels {
		if ch.Bits == 8 {
			eightBitChans = append(eightBitChans, ch)
		}
	}
	if len(eightBitChans) == 0 || len(eightBitChans) != bytesPerPixel {
		// We don't support mixed-width channels at depth >=8.
		return nil, ErrUnsupportedFormat
	}

	// Reverse channel order to map bytes.
	reversed := make([]channelDesc, len(eightBitChans))
	for i := range eightBitChans {
		reversed[i] = eightBitChans[len(eightBitChans)-1-i]
	}

	for y := 0; y < height; y++ {
		row := pixelBytes[y*bytesPerRow : (y+1)*bytesPerRow]
		for x := 0; x < width; x++ {
			offset := x * bytesPerPixel
			if offset+bytesPerPixel > len(row) {
				return nil, fmt.Errorf("infernoimage: truncated row data")
			}
			pix := row[offset : offset+bytesPerPixel]

			var r, g, b, a uint8
			a = 255 // default opaque; may be overridden by 'a' channel.

			for i, ch := range reversed {
				val := pix[i]
				switch ch.Name {
				case 'r':
					r = val
				case 'g':
					g = val
				case 'b':
					b = val
				case 'a':
					a = val
				case 'k', 'm':
					// 8-bit greyscale, but those should have been handled above;
					// if we get here, just map into RGB.
					r, g, b = val, val, val
				case 'x':
					// don't care, ignore
				default:
					return nil, ErrUnsupportedFormat
				}
			}

			i := out.PixOffset(minx+x, miny+y)
			out.Pix[i+0] = r
			out.Pix[i+1] = g
			out.Pix[i+2] = b
			out.Pix[i+3] = a
		}
	}

	return out, nil
}

// DecodeConfig reads only the header and returns image.Config.
func DecodeConfig(r io.Reader) (image.Config, error) {
	br := bufio.NewReader(r)

	peek, err := br.Peek(11)
	if err != nil {
		return image.Config{}, fmt.Errorf("infernoimage: peek: %w", err)
	}

	compressed := string(peek) == "compressed\n"
	if compressed {
		if _, err := br.Discard(11); err != nil {
			return image.Config{}, fmt.Errorf("infernoimage: discard compressed prefix: %w", err)
		}
	}

	chanStr, minx, miny, maxx, maxy, err := readHeader(br)
	if err != nil {
		return image.Config{}, err
	}

	_, _, err = parseChanOrLDepth(chanStr)
	if err != nil {
		return image.Config{}, err
	}

	width := maxx - minx
	height := maxy - miny
	if width <= 0 || height <= 0 {
		return image.Config{}, fmt.Errorf("infernoimage: non-positive bounds")
	}

	return image.Config{
		ColorModel: color.NRGBAModel,
		Width:      width,
		Height:     height,
	}, nil
}

// ---- helpers ----

type channelDesc struct {
	Name byte
	Bits int
}

// readHeader reads the 5 header fields after an optional "compressed\n":
// chan, r.min.x, r.min.y, r.max.x, r.max.y.
// Each is right-justified and blank padded in 11 characters, followed by a blank.
// We read 12 bytes and trim spaces.
func readHeader(br *bufio.Reader) (chanStr string, minx, miny, maxx, maxy int, err error) {
	fields := make([]string, 5)
	for i := 0; i < 5; i++ {
		buf := make([]byte, 12)
		if _, err := io.ReadFull(br, buf); err != nil {
			return "", 0, 0, 0, 0, fmt.Errorf("infernoimage: read header field %d: %w", i, err)
		}
		fields[i] = strings.TrimSpace(string(buf))
	}

	chanStr = fields[0]

	parse := func(s string) (int, error) {
		return strconv.Atoi(strings.TrimSpace(s))
	}

	if minx, err = parse(fields[1]); err != nil {
		return "", 0, 0, 0, 0, fmt.Errorf("infernoimage: parse minx: %w", err)
	}
	if miny, err = parse(fields[2]); err != nil {
		return "", 0, 0, 0, 0, fmt.Errorf("infernoimage: parse miny: %w", err)
	}
	if maxx, err = parse(fields[3]); err != nil {
		return "", 0, 0, 0, 0, fmt.Errorf("infernoimage: parse maxx: %w", err)
	}
	if maxy, err = parse(fields[4]); err != nil {
		return "", 0, 0, 0, 0, fmt.Errorf("infernoimage: parse maxy: %w", err)
	}

	return chanStr, minx, miny, maxx, maxy, nil
}

// parseChanOrLDepth handles both modern channel strings and the older ldepth
// numeric header, mapping ldepth 0-3 to k1, k2, k4, m8 respectively.
func parseChanOrLDepth(s string) ([]channelDesc, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, 0, fmt.Errorf("infernoimage: empty channel string")
	}

	// If it's purely digits, treat it as old ldepth.
	isDigits := true
	for _, r := range s {
		if !unicode.IsDigit(r) {
			isDigits = false
			break
		}
	}

	if isDigits {
		ld, err := strconv.Atoi(s)
		if err != nil {
			return nil, 0, fmt.Errorf("infernoimage: invalid ldepth %q: %w", s, err)
		}
		switch ld {
		case 0:
			return []channelDesc{{Name: 'k', Bits: 1}}, 1, nil // k1
		case 1:
			return []channelDesc{{Name: 'k', Bits: 2}}, 2, nil // k2
		case 2:
			return []channelDesc{{Name: 'k', Bits: 4}}, 4, nil // k4
		case 3:
			return []channelDesc{{Name: 'm', Bits: 8}}, 8, nil // m8
		default:
			return nil, 0, ErrUnsupportedFormat
		}
	}

	return parseChan(s)
}

// parseChan parses a channel descriptor string like "r8g8b8" into components.
func parseChan(s string) ([]channelDesc, int, error) {
	var (
		chans []channelDesc
		i     int
	)
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}
		if !isValidChanLetter(c) {
			return nil, 0, fmt.Errorf("infernoimage: invalid channel letter %q", c)
		}
		i++
		if i >= len(s) || !unicode.IsDigit(rune(s[i])) {
			return nil, 0, fmt.Errorf("infernoimage: missing bit depth after %q", c)
		}
		bits := 0
		for i < len(s) && unicode.IsDigit(rune(s[i])) {
			bits = bits*10 + int(s[i]-'0')
			i++
		}
		if bits <= 0 {
			return nil, 0, fmt.Errorf("infernoimage: non-positive bits %d for channel %q", bits, c)
		}
		chans = append(chans, channelDesc{Name: c, Bits: bits})
	}
	if len(chans) == 0 {
		return nil, 0, fmt.Errorf("infernoimage: no channels")
	}
	depth := 0
	for _, ch := range chans {
		depth += ch.Bits
	}
	return chans, depth, nil
}

func isValidChanLetter(c byte) bool {
	switch c {
	case 'r', 'g', 'b', 'a', 'm', 'k', 'x':
		return true
	default:
		return false
	}
}

// getBits extracts `bits` bits starting at bit offset bitOff in row.
// Bits in each byte are numbered 0 (high-order) to 7 (low-order).
func getBits(row []byte, bitOff int, bits int) uint32 {
	var v uint32
	for i := 0; i < bits; i++ {
		bIndex := (bitOff + i) / 8
		bitInByte := (bitOff + i) % 8
		if bIndex >= len(row) {
			break
		}
		b := row[bIndex]
		// high bit is bit 0
		bit := (b >> (7 - bitInByte)) & 1
		v = (v << 1) | uint32(bit)
	}
	return v
}

// readCompressedPixels reads compressed pixel data blocks and returns the
// decompressed byte slice. It stops once it has decompressed at least totalBytes
// or EOF/format error.
func readCompressedPixels(br *bufio.Reader, totalBytes int) ([]byte, error) {
	out := make([]byte, 0, totalBytes)

	for len(out) < totalBytes {
		// Each compression block begins with two decimal strings of 12 bytes each:
		// y-limit and compressed length.
		header := make([]byte, 24)
		if _, err := io.ReadFull(br, header); err != nil {
			return nil, fmt.Errorf("infernoimage: read compression block header: %w", err)
		}

		yStr := strings.TrimSpace(string(header[:12]))
		lenStr := strings.TrimSpace(string(header[12:]))

		// We don't actually need yStr for decoding; it's for row alignment.
		if _, err := strconv.Atoi(yStr); err != nil {
			return nil, fmt.Errorf("infernoimage: invalid block y string %q: %w", yStr, err)
		}
		blockLen, err := strconv.Atoi(lenStr)
		if err != nil {
			return nil, fmt.Errorf("infernoimage: invalid block length %q: %w", lenStr, err)
		}
		if blockLen < 0 || blockLen > 6000 {
			return nil, fmt.Errorf("infernoimage: unreasonable block length %d", blockLen)
		}

		data := make([]byte, blockLen)
		if _, err := io.ReadFull(br, data); err != nil {
			return nil, fmt.Errorf("infernoimage: read compression block data: %w", err)
		}

		// LZ77-style decode.
		i := 0
		for i < len(data) && len(out) < totalBytes {
			b := data[i]
			i++
			if b&0x80 != 0 {
				// Literal run: low 7 bits + 1 is length.
				n := int(b&0x7F) + 1
				if i+n > len(data) {
					return nil, fmt.Errorf("infernoimage: literal run beyond block")
				}
				end := i + n
				if len(out)+(end-i) > totalBytes {
					end = i + (totalBytes - len(out))
				}
				out = append(out, data[i:end]...)
				i += n
			} else {
				// Copy from previous data.
				length := int((b>>2)&0x1F) + 3
				if i >= len(data) {
					return nil, fmt.Errorf("infernoimage: missing offset byte in copy")
				}
				offByte := data[i]
				i++
				offset := (int(b&0x03) << 8) | int(offByte)
				offset++ // stored 0..1023 => real offset 1..1024

				if offset <= 0 || offset > len(out) {
					return nil, fmt.Errorf("infernoimage: invalid copy offset %d (out len %d)", offset, len(out))
				}
				srcStart := len(out) - offset
				for j := 0; j < length && len(out) < totalBytes; j++ {
					out = append(out, out[srcStart+j])
				}
			}
		}
	}

	return out, nil
}
