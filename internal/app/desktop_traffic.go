//go:build !nogui

package app

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/fishioon/aicat/internal/traffic"
)

// TrafficBatch contains summary changes; body bytes never enter this stream.
type TrafficBatch struct {
	Reset   bool              `json:"reset"`
	Items   []traffic.Summary `json:"items"`
	Removed []traffic.ID      `json:"removed"`
	SentAt  int64             `json:"sentAt"`
}

// WatchTraffic owns one cancellable MyGo channel and atomic Watch snapshot.
func (d *Desktop) WatchTraffic(ctx context.Context, updates *mygo.Channel[TrafficBatch]) error {
	return d.watchTraffic(ctx, updates.Send)
}
func (d *Desktop) watchTraffic(ctx context.Context, send func(TrafficBatch) error) error {
	trafficViews.Add(1)
	defer trafficViews.Add(-1)
	page, sub, err := d.app.r.traffic.Watch(ctx, traffic.Query{Limit: 2000})
	if err != nil {
		return err
	}
	defer func() {
		if sub != nil {
			sub.Close()
		}
	}()
	previous := make(map[traffic.ID]uint64)
	publish := func(page traffic.Page, reset bool) error {
		batch := TrafficBatch{Reset: reset, Items: []traffic.Summary{}, Removed: []traffic.ID{}, SentAt: time.Now().UnixMilli()}
		next := make(map[traffic.ID]uint64, len(page.Items))
		for _, r := range page.Items {
			next[r.ID] = r.Revision
			if reset || previous[r.ID] != r.Revision {
				batch.Items = append(batch.Items, r)
			}
		}
		for id := range previous {
			if _, exists := next[id]; !exists {
				batch.Removed = append(batch.Removed, id)
			}
		}
		previous = next
		if !reset && len(batch.Items) == 0 && len(batch.Removed) == 0 {
			return nil
		}
		slices.SortFunc(batch.Items, func(a, b traffic.Summary) int { return cmp.Compare(a.ID, b.ID) })
		return send(batch)
	}
	if err = publish(page, true); err != nil {
		return err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	dirty := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, open := <-sub.Changes:
			if !open {
				if !errors.Is(sub.Err(), traffic.ErrSlowSubscriber) {
					return sub.Err()
				}
				page, sub, err = d.app.r.traffic.Watch(ctx, traffic.Query{Limit: 2000})
				if err != nil {
					return err
				}
				if err = publish(page, true); err != nil {
					return err
				}
				dirty = false
				continue
			}
			dirty = true
		case <-ticker.C:
			if !dirty {
				continue
			}
			page, err = d.app.r.traffic.List(ctx, traffic.Query{Limit: 2000})
			if err != nil {
				return err
			}
			if err = publish(page, false); err != nil {
				return err
			}
			dirty = false
		}
	}
}
