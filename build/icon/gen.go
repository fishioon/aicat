// Command gen draws aicat's icons from the private cloud's logo, which has one
// source: netcat's site/public/logo.svg, kept in the window's assets as
// logo.svg. The mark is never redrawn by hand — this reads the paths out of
// that SVG, curves included, and rasterizes them itself: the icons are the
// logo's own outline at any size, drawn the same on any machine, and the tray
// glyph is black and alpha alone, which the menu bar tints.
//
//	go run ./build/icon/gen.go tray assets/logo.svg assets/tray.png
//	go run ./build/icon/gen.go app assets/logo.svg 256 assets/icon.png
package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// mark is one <path> of the logo: its subpaths as closed rings of points, the
// curves cut into short segments, and the colour it is filled with.
type mark struct {
	rings [][][2]float64
	fill  color.NRGBA
}

// paper is the logo's own background, from its source: the app icon's field.
var paper = color.NRGBA{R: 0xf5, G: 0xf2, B: 0xed, A: 0xff}

func main() {
	switch {
	case len(os.Args) == 4 && os.Args[1] == "tray":
		picture, err := tray(os.Args[2])
		if err != nil {
			fail(err)
		}
		if err := write(os.Args[3], picture); err != nil {
			fail(err)
		}
	case len(os.Args) == 5 && os.Args[1] == "app":
		size, err := strconv.Atoi(os.Args[3])
		if err != nil || size < 16 {
			fmt.Fprintln(os.Stderr, "size must be a number of at least 16")
			os.Exit(2)
		}
		picture, err := app(os.Args[2], size)
		if err != nil {
			fail(err)
		}
		if err := write(os.Args[4], picture); err != nil {
			fail(err)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: gen tray <logo.svg> <out.png> | gen app <logo.svg> <size> <out.png>")
		os.Exit(2)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}

// tray is the menu bar glyph: the mark's own outline in black, which the bar
// tints. The final canvas follows the artwork's aspect ratio: MyGo fits the
// whole canvas to 18pt high, so square padding makes this wide mark look tiny.
func tray(svgPath string) (image.Image, error) {
	const (
		size   = 44
		margin = 4
	)
	marks, err := read(svgPath)
	if err != nil {
		return nil, err
	}
	k, ox, oy := fit(box(marks), size, size-2*margin)
	glyph := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i, cover := range coverage(marks, size, 8, k, ox, oy) {
		glyph.SetNRGBA(i%size, i/size, color.NRGBA{A: channel(cover * 255)})
	}
	ink := image.Rectangle{}
	for y := range size {
		for x := range size {
			if glyph.NRGBAAt(x, y).A != 0 {
				ink = ink.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if ink.Empty() {
		return nil, errors.New("the tray mark is empty")
	}
	// Two pixels of breathing room keep the visible mark near 15.5pt when
	// MyGo fits the canvas to 18pt, a little quieter than the tight 1px crop.
	return glyph.SubImage(ink.Inset(-2).Intersect(glyph.Bounds())), nil
}

// app is the application icon: the mark in its own colours on the logo's
// paper field, rounded the way macOS draws an app.
func app(svgPath string, size int) (image.Image, error) {
	marks, err := read(svgPath)
	if err != nil {
		return nil, err
	}
	k, ox, oy := fit(box(marks), float64(size), 0.72*float64(size))
	icon := image.NewNRGBA(image.Rect(0, 0, size, size))
	corner := 0.22 * float64(size)
	for y := range size {
		for x := range size {
			if insideRoundedSquare(float64(x)+0.5, float64(y)+0.5, float64(size), corner) {
				icon.SetNRGBA(x, y, paper)
			}
		}
	}
	// Each path in the colour the SVG fills it with, one over the other as
	// the SVG draws them.
	for _, m := range marks {
		for i, cover := range coverage([]mark{m}, size, 8, k, ox, oy) {
			ink := m.fill
			ink.A = channel(cover * 255)
			blend(icon, i%size, i/size, ink)
		}
	}
	return icon, nil
}

// read is the logo's marks, in the order the SVG draws them.
func read(svgPath string) ([]mark, error) {
	svg, err := os.ReadFile(svgPath)
	if err != nil {
		return nil, err
	}
	var marks []mark
	for rest := string(svg); ; {
		at := strings.Index(rest, "<path")
		if at < 0 {
			break
		}
		rest = rest[at+len("<path"):]
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			return nil, errors.New("a <path> is not closed")
		}
		element := rest[:end]
		rest = rest[end+1:]
		rings, err := rings(attribute(element, "d"))
		if err != nil {
			return nil, err
		}
		colour, err := fill(attribute(element, "fill"))
		if err != nil {
			return nil, err
		}
		marks = append(marks, mark{rings: rings, fill: colour})
	}
	if len(marks) == 0 {
		return nil, errors.New("the logo has no <path>")
	}
	return marks, nil
}

// attribute is one name="value" of a <path> element, or "" when it has none,
// which the callers read as the SVG's own default.
func attribute(element, name string) string {
	at := strings.Index(element, name+`="`)
	if at < 0 {
		return ""
	}
	value := element[at+len(name)+2:]
	end := strings.IndexByte(value, '"')
	if end < 0 {
		return ""
	}
	return value[:end]
}

// rings reads one path's d into rings of points: absolute M, L, H, V, C and Z
// only, with every curve cut into short segments. Anything else is an error,
// so a logo using a command this doesn't know fails the build rather than
// quietly drawing part of itself.
func rings(d string) ([][][2]float64, error) {
	if strings.TrimSpace(d) == "" {
		return nil, errors.New("a <path> has no d")
	}
	// A command may sit against its numbers (M134), so give every letter a
	// field of its own.
	fields := strings.Fields(strings.NewReplacer(
		"M", " M ", "L", " L ", "H", " H ", "V", " V ", "C", " C ", "Z", " Z ",
	).Replace(d))
	var (
		rings  [][][2]float64
		points [][2]float64
		at     [2]float64
		cmd    byte
	)
	number := func(i int) (float64, error) {
		if i >= len(fields) {
			return 0, fmt.Errorf("%q: %c wants more numbers", d, cmd)
		}
		n, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return 0, fmt.Errorf("%q: %w", d, err)
		}
		return n, nil
	}
	for i := 0; i < len(fields); {
		if f := fields[i]; len(f) == 1 && strings.ContainsAny(f, "MLHVCZ") {
			cmd = f[0]
			i++
			if cmd == 'Z' && len(points) > 0 {
				rings, points = append(rings, points), nil
			}
			continue
		}
		switch cmd {
		case 'M', 'L':
			if cmd == 'M' && len(points) > 0 {
				rings, points = append(rings, points), nil
			}
			x, err := number(i)
			if err != nil {
				return nil, err
			}
			y, err := number(i + 1)
			if err != nil {
				return nil, err
			}
			at = [2]float64{x, y}
			points, i, cmd = append(points, at), i+2, 'L'
		case 'H':
			x, err := number(i)
			if err != nil {
				return nil, err
			}
			at[0] = x
			points, i = append(points, at), i+1
		case 'V':
			y, err := number(i)
			if err != nil {
				return nil, err
			}
			at[1] = y
			points, i = append(points, at), i+1
		case 'C':
			from := at
			var control [6]float64
			for c := range control {
				n, err := number(i + c)
				if err != nil {
					return nil, err
				}
				control[c] = n
			}
			for s := 1; s <= 8; s++ {
				points = append(points, cubic(from, control, float64(s)/8))
			}
			at = [2]float64{control[4], control[5]}
			i += 6
		default:
			return nil, fmt.Errorf("%q: %q, want an absolute M, L, H, V, C or Z",
				d, fields[i])
		}
	}
	if len(points) > 0 {
		rings = append(rings, points)
	}
	return rings, nil
}

// cubic is the point at t of a cubic from p0 through the two control points
// and the end, which is what c holds.
func cubic(p0 [2]float64, c [6]float64, t float64) [2]float64 {
	m := 1 - t
	return [2]float64{
		m*m*m*p0[0] + 3*m*m*t*c[0] + 3*m*t*t*c[2] + t*t*t*c[4],
		m*m*m*p0[1] + 3*m*m*t*c[1] + 3*m*t*t*c[3] + t*t*t*c[5],
	}
}

// fill reads a path's fill="#rrggbb"; a path without one is black, as SVG's
// own default is.
func fill(value string) (color.NRGBA, error) {
	if value == "" {
		return color.NRGBA{A: 0xff}, nil
	}
	hex := strings.TrimPrefix(value, "#")
	if len(hex) == len(value) {
		return color.NRGBA{}, fmt.Errorf("fill %q is not #rgb or #rrggbb", value)
	}
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	shade, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("fill %q is not #rgb or #rrggbb", value)
	}
	return color.NRGBA{
		R: uint8(shade >> 16), G: uint8(shade >> 8), B: uint8(shade), A: 0xff,
	}, nil
}

// box is the marks' own box: what the shapes draw in, in their coordinates.
func box(marks []mark) [4]float64 {
	b := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, m := range marks {
		for _, r := range m.rings {
			for _, p := range r {
				b[0], b[1] = math.Min(b[0], p[0]), math.Min(b[1], p[1])
				b[2], b[3] = math.Max(b[2], p[0]), math.Max(b[3], p[1])
			}
		}
	}
	return b
}

// fit is where the marks' box lands on an icon: k, the icon's pixels per logo
// unit, and (ox, oy), the box's own corner as the box is drawn extent long —
// its longer side — and centred in a square of side size.
func fit(b [4]float64, size, extent float64) (k, ox, oy float64) {
	k = extent / math.Max(b[2]-b[0], b[3]-b[1])
	ox = (size-(b[2]-b[0])*k)/2 - b[0]*k
	oy = (size-(b[3]-b[1])*k)/2 - b[1]*k
	return k, ox, oy
}

// coverage is how much of each pixel of a size×size square is inside the
// marks: each mark's subpaths filled even-odd, the marks together filling
// their union, every pixel sampled samples×samples times. k and (ox, oy) are
// the fit the shapes are drawn with.
func coverage(marks []mark, size, samples int, k, ox, oy float64) []float64 {
	cover := make([]float64, size*size)
	union, one := make([]float64, size), make([]float64, size)
	per := 1 / float64(samples*samples)
	var crossings []float64
	for y := range size * samples {
		// The shapes' own line this sample row reads them along.
		v := (float64(y)/float64(samples) + 0.5 - oy) / k
		icon := (y / samples) * size
		for i := range union {
			union[i] = 0
		}
		for _, m := range marks {
			for i := range one {
				one[i] = 0
			}
			crossings = crossings[:0]
			for _, r := range m.rings {
				for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
					a, b := r[j], r[i]
					if (a[1] > v) != (b[1] > v) {
						crossings = append(crossings,
							(a[0]+(v-a[1])*(b[0]-a[0])/(b[1]-a[1]))*k+ox)
					}
				}
			}
			sort.Float64s(crossings)
			for i := 0; i+1 < len(crossings); i += 2 {
				// The supersampled samples whose centre falls between the
				// two crossings, x+0.5 of the grid.
				lo := int(math.Max(0, math.Ceil(crossings[i]*float64(samples)-0.5)))
				hi := int(math.Min(float64(size*samples),
					math.Ceil(crossings[i+1]*float64(samples)-0.5)))
				for x := lo; x < hi; x++ {
					one[x/samples] += per
				}
			}
			for i, c := range one {
				union[i] = math.Max(union[i], c)
			}
		}
		for i, c := range union {
			cover[icon+i] += c
		}
	}
	return cover
}

// blend puts overlay over what target already holds at (x, y).
func blend(target *image.NRGBA, x, y int, overlay color.NRGBA) {
	if !image.Pt(x, y).In(target.Bounds()) {
		return
	}
	under := target.NRGBAAt(x, y)
	over := float64(overlay.A) / 255
	keep := 1 - over
	target.SetNRGBA(x, y, color.NRGBA{
		R: channel(float64(overlay.R)*over + float64(under.R)*keep),
		G: channel(float64(overlay.G)*over + float64(under.G)*keep),
		B: channel(float64(overlay.B)*over + float64(under.B)*keep),
		A: channel(255*over + float64(under.A)*keep),
	})
}

func insideRoundedSquare(x, y, size, corner float64) bool {
	extent := corner
	dx, dy := math.Abs(x-size/2), math.Abs(y-size/2)
	if dx > size/2 || dy > size/2 {
		return false
	}
	if dx <= size/2-extent || dy <= size/2-extent {
		return true
	}
	return math.Hypot(dx-(size/2-extent), dy-(size/2-extent)) <= extent
}

func write(path string, picture image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, picture); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func channel(value float64) uint8 {
	switch {
	case value <= 0:
		return 0
	case value >= 255:
		return 255
	}
	return uint8(math.Round(value))
}
