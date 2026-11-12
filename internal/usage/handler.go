package usage

import (
	"encoding/json"
	"net/http"
	"time"
)

// Path is where the HTTP API serves summaries on aicat's listener.
const Path = "/api/usage"

// Handler answers GET /api/usage?period=today|7d|30d with the summary;
// name, when set, turns provider IDs into their names. The caller restricts
// it to local peers.
func Handler(s *Store, name func(id string) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		period := r.URL.Query().Get("period")
		if period == "" {
			period = "today"
		}
		sum, err := s.Summary(period, time.Now())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if name != nil {
			sum.Name(name)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sum)
	})
}

// Name sets each provider group's name; an ID name does not know keeps
// itself, as a deleted provider's does.
func (s *Report) Name(name func(id string) string) {
	for i := range s.Providers {
		if n := name(s.Providers[i].Key); n != "" {
			s.Providers[i].Name = n
		}
	}
}
