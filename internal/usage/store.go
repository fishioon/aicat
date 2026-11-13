// Package usage keeps one record per model call aicat forwards to a provider
// and sums them for the overview and the HTTP API. Records are appended to
// <Home>/usage.jsonl (mode 0600); the last month is kept in memory for
// queries. A record that cannot be written is dropped: usage never gets in
// the way of the call itself.
package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is one forwarded call. Input excludes cached tokens: the four
// token fields add up to the prompt and the reply.
type Record struct {
	At       time.Time `json:"t"`
	Agent    string    `json:"agent,omitempty"`
	Provider string    `json:"provider"`
	// Model is the name the provider was asked for, without aicat's
	// "<provider ID>/" prefix; Served is the one its reply named.
	Model      string `json:"model"`
	Served     string `json:"served,omitempty"`
	API        string `json:"api"`
	Status     int    `json:"status,omitempty"`
	Error      string `json:"err,omitempty"`
	Input      int64  `json:"in,omitempty"`
	Output     int64  `json:"out,omitempty"`
	CacheRead  int64  `json:"cache_read,omitempty"`
	CacheWrite int64  `json:"cache_write,omitempty"`
	// Ms runs from the request reaching aicat to the reply's end; TTFTMs to
	// the first content event of a streamed reply, 0 when there was none.
	Ms     int64 `json:"ms"`
	TTFTMs int64 `json:"ttft_ms,omitempty"`
}

// Failed reports whether the call got no usable answer.
func (r Record) Failed() bool { return r.Error != "" || r.Status >= 400 || r.Status == 0 }

// keep is how far back the store holds records in memory; the longest
// period a summary covers is 30 days.
const keep = 31 * 24 * time.Hour

type Store struct {
	mu      sync.Mutex
	path    string
	file    *os.File
	records []Record
}

// Open reads the recent records of <home>/usage.jsonl and opens it for
// appending. Unreadable lines are skipped.
func Open(home string) (*Store, error) {
	path := filepath.Join(home, "usage.jsonl")
	s := &Store{path: path}
	if f, err := os.Open(path); err == nil {
		since := time.Now().Add(-keep)
		scan := bufio.NewScanner(f)
		scan.Buffer(make([]byte, 64<<10), 1<<20)
		for scan.Scan() {
			var r Record
			if json.Unmarshal(scan.Bytes(), &r) == nil && r.At.After(since) {
				s.records = append(s.records, r)
			}
		}
		f.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	s.file = f
	return s, nil
}

// Append adds a record. It is safe to call on a nil store.
func (s *Store) Append(r Record) {
	if s == nil {
		return
	}
	line, err := json.Marshal(r)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	if cut := time.Now().Add(-keep); len(s.records) > 0 && s.records[0].At.Before(cut) {
		i := 0
		for i < len(s.records) && s.records[i].At.Before(cut) {
			i++
		}
		s.records = append([]Record(nil), s.records[i:]...)
	}
	if s.file != nil {
		if _, err := s.file.Write(append(line, '\n')); err != nil {
			slog.Error("append usage failed", "err", err)
		}
	}
}

// since copies the records at or after t.
func (s *Store) since(t time.Time) []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Record
	for _, r := range s.records {
		if !r.At.Before(t) {
			out = append(out, r)
		}
	}
	return out
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}
