package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type point = [2]float64
type curve [4]point

type tailSection struct {
	middle, half point
}

// tailSections reads the actual SVG boundaries, rather than drawing a new cat.
// The logo has three outer cubics, two tip cubics, and three inner cubics.
func tailSections(d string) ([]tailSection, []curve, error) {
	fields := strings.Fields(strings.NewReplacer("M", " M ", "C", " C ", "Z", " Z ").Replace(d))
	var at point
	var curves []curve
	for i := 0; i < len(fields); {
		cmd := fields[i]
		i++
		if cmd == "Z" {
			break
		}
		count := 2
		if cmd == "C" {
			count = 6
		} else if cmd != "M" {
			return nil, nil, fmt.Errorf("unsupported tail command %q", cmd)
		}
		if i+count > len(fields) {
			return nil, nil, fmt.Errorf("incomplete tail %s", cmd)
		}
		numbers := make([]float64, count)
		for j := range numbers {
			value, err := strconv.ParseFloat(fields[i+j], 64)
			if err != nil {
				return nil, nil, err
			}
			numbers[j] = value
		}
		i += count
		if cmd == "M" {
			at = point{numbers[0], numbers[1]}
		} else {
			c := curve{at, {numbers[0], numbers[1]}, {numbers[2], numbers[3]}, {numbers[4], numbers[5]}}
			curves, at = append(curves, c), c[3]
		}
	}
	if len(curves) != 8 {
		return nil, nil, fmt.Errorf("logo tail has %d cubics, expected 8", len(curves))
	}
	var sections []tailSection
	for segment := range 3 {
		a, reverse := curves[segment], curves[7-segment]
		b := curve{reverse[3], reverse[2], reverse[1], reverse[0]}
		start := 0
		if segment > 0 {
			start = 1
		}
		for i := start; i <= 80; i++ {
			outer, inner := onCurve(a, float64(i)/80), onCurve(b, float64(i)/80)
			sections = append(sections, tailSection{
				middle: point{(outer[0] + inner[0]) / 2, (outer[1] + inner[1]) / 2},
				half:   point{(outer[0] - inner[0]) / 2, (outer[1] - inner[1]) / 2},
			})
		}
	}
	return sections, curves[3:5], nil
}

func onCurve(c curve, t float64) point {
	return cubic(c[0], [6]float64{c[1][0], c[1][1], c[2][0], c[2][1], c[3][0], c[3][1]}, t)
}

func rotate(p point, angle float64) point {
	return point{p[0]*math.Cos(angle) - p[1]*math.Sin(angle), p[0]*math.Sin(angle) + p[1]*math.Cos(angle)}
}

// uncurl keeps the root and each segment's length/cross-section fixed. Only
// the final two cubics lose curvature; the original rounded tip rotates intact.
// This is the same construction accepted in the manual SVG design preview.
func uncurl(sections []tailSection, cap []curve, percent float64) []point {
	count := len(sections)
	angles, lengths := make([]float64, count-1), make([]float64, count-1)
	for i := range angles {
		a, b := sections[i].middle, sections[i+1].middle
		angle := math.Atan2(b[1]-a[1], b[0]-a[0])
		if i > 0 {
			for angle-angles[i-1] > math.Pi {
				angle -= 2 * math.Pi
			}
			for angle-angles[i-1] < -math.Pi {
				angle += 2 * math.Pi
			}
		}
		angles[i], lengths[i] = angle, math.Hypot(b[0]-a[0], b[1]-a[1])
	}
	middle, changes := make([]point, count), make([]float64, count-1)
	middle[0] = sections[0].middle
	for i, angle := range angles {
		nextAngle := angle
		if i >= 80 {
			nextAngle = angles[80] + percent/100*(angle-angles[80])
		}
		changes[i] = nextAngle - angle
		delta := point{math.Cos(nextAngle) * lengths[i], math.Sin(nextAngle) * lengths[i]}
		if i < 80 || percent == 100 {
			delta = point{sections[i+1].middle[0] - sections[i].middle[0], sections[i+1].middle[1] - sections[i].middle[1]}
		}
		middle[i+1] = point{middle[i][0] + delta[0], middle[i][1] + delta[1]}
	}
	outer, inner := make([]point, count), make([]point, count)
	for i, section := range sections {
		change := changes[0]
		if i == count-1 {
			change = changes[len(changes)-1]
		} else if i > 0 {
			change = (changes[i-1] + changes[i]) / 2
		}
		offset := rotate(section.half, change)
		outer[i] = point{middle[i][0] + offset[0], middle[i][1] + offset[1]}
		inner[i] = point{middle[i][0] - offset[0], middle[i][1] - offset[1]}
	}
	result := append([]point(nil), outer...)
	end, oldEnd, angle := middle[count-1], sections[count-1].middle, changes[len(changes)-1]
	for _, c := range cap {
		for i := 1; i <= 32; i++ {
			p := onCurve(c, float64(i)/32)
			delta := rotate(point{p[0] - oldEnd[0], p[1] - oldEnd[1]}, angle)
			result = append(result, point{end[0] + delta[0], end[1] + delta[1]})
		}
	}
	for i := count - 2; i >= 0; i-- {
		result = append(result, inner[i])
	}
	return result
}

func templateGlyph(marks []mark, size int, k, ox, oy float64) *image.NRGBA {
	glyph := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i, cover := range coverage(marks, size, 8, k, ox, oy) {
		glyph.SetNRGBA(i%size, i/size, color.NRGBA{A: channel(cover * 255)})
	}
	return glyph
}

func trayMotion(svgPath, output, svgFrames string) error {
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	if svgFrames != "" {
		if err := os.MkdirAll(svgFrames, 0755); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(svgPath)
	if err != nil {
		return err
	}
	var paths []string
	for rest := string(data); ; {
		start := strings.Index(rest, "<path")
		if start < 0 {
			break
		}
		rest = rest[start+len("<path"):]
		end := strings.IndexByte(rest, '>')
		if end < 0 {
			return fmt.Errorf("unclosed SVG path")
		}
		paths = append(paths, attribute(rest[:end], "d"))
		rest = rest[end+1:]
	}
	if len(paths) != 2 {
		return fmt.Errorf("logo needs one body and one tail, got %d paths", len(paths))
	}
	sections, cap, err := tailSections(paths[1])
	if err != nil {
		return err
	}
	marks, err := read(svgPath)
	if err != nil {
		return err
	}
	k, ox, oy := fit(box(marks), 44, 36)
	// All frames share this single canonical viewport/crop, so the cat body
	// stays pixel-for-pixel fixed instead of being refitted on every frame.
	first := []mark{marks[0], {rings: [][]point{uncurl(sections, cap, 100)}}}
	glyph := templateGlyph(first, 44, k, ox, oy)
	ink := image.Rectangle{}
	for y := range 44 {
		for x := range 44 {
			if glyph.NRGBAAt(x, y).A != 0 {
				ink = ink.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if ink.Empty() {
		return fmt.Errorf("empty tray artwork")
	}
	crop := ink.Inset(-2).Intersect(glyph.Bounds())
	// MyGo presents the canvas at 18pt high. Render 36 physical pixels for
	// Retina (2x), directly from SVG geometry; do not upscale the old PNG.
	const height = 36
	scale := float64(height) / float64(crop.Dy())
	width := int(math.Round(float64(crop.Dx()) * scale))
	k, ox, oy = k*scale, (ox-float64(crop.Min.X))*scale, (oy-float64(crop.Min.Y))*scale
	const frames = 25
	sprite := image.NewNRGBA(image.Rect(0, 0, width*frames, height))
	var last []point
	for i := range frames {
		percent := math.Round((100-70*float64(i)/(frames-1))*10) / 10
		last = uncurl(sections, cap, percent)
		if svgFrames != "" {
			if err := os.WriteFile(filepath.Join(svgFrames, fmt.Sprintf("frame-%02d.svg", i)), []byte(tailSVG(paths[0], last)), 0644); err != nil {
				return err
			}
		}
		frame := templateGlyph([]mark{marks[0], {rings: [][]point{last}}}, width, k, ox, oy).SubImage(image.Rect(0, 0, width, height))
		draw.Draw(sprite, image.Rect(i*width, 0, (i+1)*width, height), frame, image.Point{}, draw.Src)
		if i == 0 || i == frames-1 {
			name := "tray.png"
			if i != 0 {
				name = "tray-active.png"
			}
			if err := write(filepath.Join(output, name), frame); err != nil {
				return err
			}
		}
	}
	if err := write(filepath.Join(output, "tray-motion.png"), sprite); err != nil {
		return err
	}
	manifest := struct {
		FrameCount   int    `json:"frameCount"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		DurationMS   int    `json:"durationMs"`
		FromCurl     int    `json:"fromCurl"`
		ToCurl       int    `json:"toCurl"`
		SourceSHA256 string `json:"sourceSHA256"`
	}{frames, width, height, 600, 100, 30, fmt.Sprintf("%x", sha256.Sum256(data))}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "tray-motion.json"), append(encoded, '\n'), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "tray-active.svg"), []byte(tailSVG(paths[0], last)), 0644)
}

func tailSVG(body string, ring []point) string {
	var d strings.Builder
	for i, p := range ring {
		command := "L"
		if i == 0 {
			command = "M"
		}
		fmt.Fprintf(&d, "%s%.6f %.6f", command, p[0], p[1])
	}
	d.WriteByte('Z')
	return fmt.Sprintf("<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 256 256\"><g fill=\"currentColor\"><path d=\"%s\"/><path d=\"%s\"/></g></svg>\n", body, d.String())
}
