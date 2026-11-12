package assets

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"time"
)

// TrayIcon is the canonical logo rendered as a monochrome template icon.
//
//go:embed tray.png
var TrayIcon []byte

// These PNG frames are rendered from logo.svg at build time. The native tray
// accepts PNGs; the SVG controls the shape and each frame has a fixed canvas.
//
//go:embed tray-motion.png tray-motion.json
var trayMotion embed.FS

func LoadTrayAnimation() ([][]byte, time.Duration, error) {
	data, err := trayMotion.ReadFile("tray-motion.json")
	if err != nil {
		return nil, 0, err
	}
	var metadata struct {
		FrameCount int `json:"frameCount"`
		Width      int `json:"width"`
		Height     int `json:"height"`
		DurationMS int `json:"durationMs"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, 0, err
	}
	data, err = trayMotion.ReadFile("tray-motion.png")
	if err != nil {
		return nil, 0, err
	}
	sprite, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	if metadata.FrameCount < 2 || metadata.Width < 1 || metadata.Height < 1 || metadata.DurationMS < 1 || sprite.Bounds().Dx() != metadata.Width*metadata.FrameCount || sprite.Bounds().Dy() != metadata.Height {
		return nil, 0, fmt.Errorf("invalid tray animation dimensions")
	}
	frames := make([][]byte, metadata.FrameCount)
	for i := range frames {
		frame := image.NewNRGBA(image.Rect(0, 0, metadata.Width, metadata.Height))
		draw.Draw(frame, frame.Bounds(), sprite, image.Pt(i*metadata.Width, 0), draw.Src)
		var out bytes.Buffer
		if err := png.Encode(&out, frame); err != nil {
			return nil, 0, err
		}
		frames[i] = out.Bytes()
	}
	return frames, time.Duration(metadata.DurationMS) * time.Millisecond, nil
}
