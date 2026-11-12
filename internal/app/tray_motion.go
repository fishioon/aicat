//go:build !nogui

package app

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
)

// trayIconMotion owns only presentation. A click never changes proxy intent;
// the newest real proxy state always wins over an in-flight round trip.
type trayIconMotion struct {
	tray       *mygo.Tray
	frames     [][]byte
	duration   time.Duration
	generation atomic.Uint64
	running    atomic.Uint64
	frame      atomic.Int32
	observe    func(int, uint64)
}

func (m *trayIconMotion) endpoint(active bool) int {
	if active {
		return len(m.frames) - 1
	}
	return 0
}

func (m *trayIconMotion) stop() {
	m.generation.Add(1)
	m.running.Store(0)
}

func (m *trayIconMotion) rest(active bool) {
	m.stop()
	m.display(m.endpoint(active), m.generation.Load())
}

func (m *trayIconMotion) display(frame int, generation uint64) {
	mygo.RunOnMain(func() {
		if m.generation.Load() != generation {
			return
		}
		if int(m.frame.Load()) != frame {
			if err := m.tray.SetIcon(m.frames[frame], true); err != nil {
				m.running.CompareAndSwap(generation, 0)
				return
			}
			m.frame.Store(int32(frame))
		}
		if m.observe != nil {
			m.observe(frame, generation)
		}
	})
}

func (m *trayIconMotion) play(active bool) {
	generation := m.generation.Add(1)
	m.running.Store(generation)
	from := m.endpoint(active)
	to := len(m.frames) - 1 - from
	m.display(from, generation)
	go func() {
		ticker := time.NewTicker(time.Second / 60)
		defer ticker.Stop()
		start := time.Now()
		for now := range ticker.C {
			if m.generation.Load() != generation {
				return
			}
			progress := min(1, float64(now.Sub(start))/float64(m.duration))
			amount := math.Pow(math.Sin(math.Pi*progress), 2)
			frame := int(math.Round(float64(from) + float64(to-from)*amount))
			m.display(frame, generation)
			if progress == 1 {
				m.running.CompareAndSwap(generation, 0)
				return
			}
		}
	}()
}
