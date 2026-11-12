package usage

import (
	"errors"
	"sort"
	"time"
)

// Group sums the calls sharing a key: a model, a provider ID or an agent.
type Group struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Requests   int    `json:"requests"`
	Failures   int    `json:"failures"`
	Input      int64  `json:"input"`
	Output     int64  `json:"output"`
	CacheRead  int64  `json:"cacheRead"`
	CacheWrite int64  `json:"cacheWrite"`
	// AvgMs and TTFTMs average the answered calls; Speed is output tokens
	// per second after the first token, over streamed answers that have
	// one. Each is 0 when no call counts.
	AvgMs  float64 `json:"avgMs"`
	TTFTMs float64 `json:"ttftMs"`
	Speed  float64 `json:"speed"`

	ms, ttft, ttftN, decodeOut, decodeMs int64
}

// Bucket is an hour of today or a day of a longer period.
type Bucket struct {
	Start    time.Time `json:"start"`
	Requests int       `json:"requests"`
	// Tokens per model.
	Tokens map[string]int64 `json:"tokens"`
}

type Report struct {
	Period    string   `json:"period"`
	Total     Group    `json:"total"`
	Buckets   []Bucket `json:"buckets"`
	Models    []Group  `json:"models"`
	Providers []Group  `json:"providers"`
	Agents    []Group  `json:"agents"`
}

// Periods a summary covers: today by hour, or the last 7 or 30 days, today
// included, by day.
var Periods = []string{"today", "7d", "30d"}

// minDecodeMs leaves out replies that arrived nearly at once: their speed
// says nothing about the model.
const minDecodeMs = 100

func (g *Group) add(r Record) {
	g.Requests++
	g.Input += r.Input
	g.Output += r.Output
	g.CacheRead += r.CacheRead
	g.CacheWrite += r.CacheWrite
	if r.Failed() {
		g.Failures++
		return
	}
	g.ms += r.Ms
	if r.TTFTMs > 0 {
		g.ttft += r.TTFTMs
		g.ttftN++
		if d := r.Ms - r.TTFTMs; d >= minDecodeMs && r.Output > 0 {
			g.decodeOut += r.Output
			g.decodeMs += d
		}
	}
}

func (g *Group) finish() {
	if n := g.Requests - g.Failures; n > 0 {
		g.AvgMs = float64(g.ms) / float64(n)
	}
	if g.ttftN > 0 {
		g.TTFTMs = float64(g.ttft) / float64(g.ttftN)
	}
	if g.decodeMs > 0 {
		g.Speed = float64(g.decodeOut) / (float64(g.decodeMs) / 1000)
	}
}

func (g Group) tokens() int64 { return g.Input + g.Output + g.CacheRead + g.CacheWrite }

// Summary sums the period ending now, in now's location.
func (s *Store) Summary(period string, now time.Time) (Report, error) {
	days := map[string]int{"today": 1, "7d": 7, "30d": 30}[period]
	if days == 0 {
		return Report{}, errors.New("period 只能是 today、7d 或 30d")
	}
	y, m, d := now.Date()
	from := time.Date(y, m, d-days+1, 0, 0, 0, 0, now.Location())
	out := Report{Period: period, Models: []Group{}, Providers: []Group{}, Agents: []Group{}}
	hourly := period == "today"
	n := days
	if hourly {
		n = 24
	}
	for i := range n {
		start := time.Date(y, m, d-days+1+i, 0, 0, 0, 0, now.Location())
		if hourly {
			start = time.Date(y, m, d, i, 0, 0, 0, now.Location())
		}
		out.Buckets = append(out.Buckets, Bucket{Start: start, Tokens: map[string]int64{}})
	}
	models, providers, agents := map[string]*Group{}, map[string]*Group{}, map[string]*Group{}
	into := func(groups map[string]*Group, key string, r Record) {
		g := groups[key]
		if g == nil {
			g = &Group{Key: key, Name: key}
			groups[key] = g
		}
		g.add(r)
	}
	for _, r := range s.since(from) {
		at := r.At.In(now.Location())
		var i int
		if hourly {
			i = at.Hour()
		} else {
			// count calendar days, which a DST change makes 23 or 25 hours
			ry, rm, rd := at.Date()
			i = int(time.Date(ry, rm, rd, 12, 0, 0, 0, time.UTC).Sub(time.Date(from.Year(), from.Month(), from.Day(), 12, 0, 0, 0, time.UTC)) / (24 * time.Hour))
		}
		if i < 0 || i >= len(out.Buckets) {
			continue
		}
		model := r.Model
		if model == "" {
			model = r.Served
		}
		out.Total.add(r)
		b := &out.Buckets[i]
		b.Requests++
		b.Tokens[model] += r.Input + r.Output + r.CacheRead + r.CacheWrite
		into(models, model, r)
		into(providers, r.Provider, r)
		agent := r.Agent
		if agent == "" {
			agent = "other"
		}
		into(agents, agent, r)
	}
	out.Total.finish()
	list := func(groups map[string]*Group) []Group {
		res := make([]Group, 0, len(groups))
		for _, g := range groups {
			g.finish()
			res = append(res, *g)
		}
		sort.Slice(res, func(i, j int) bool {
			if a, b := res[i].tokens(), res[j].tokens(); a != b {
				return a > b
			}
			if res[i].Requests != res[j].Requests {
				return res[i].Requests > res[j].Requests
			}
			return res[i].Key < res[j].Key
		})
		return res
	}
	out.Models, out.Providers, out.Agents = list(models), list(providers), list(agents)
	return out, nil
}
