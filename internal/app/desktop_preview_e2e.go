//go:build !nogui && e2e

package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/fishioon/aicat/ui"
)

// Only test builds expose an authenticated loopback preview of the same assets.
func desktopPreview(desktop *Desktop) (func(), error) {
	path := os.Getenv("AICAT_E2E_PREVIEW")
	if path == "" {
		return func() {}, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	var b [24]byte
	_, err = rand.Read(b[:])
	if err != nil {
		listener.Close()
		return nil, err
	}
	token := hex.EncodeToString(b[:])
	var mu sync.Mutex
	listeners := make(map[chan StateChange]struct{})
	desktop.app.previewNotify = func(change StateChange) {
		mu.Lock()
		defer mu.Unlock()
		for ch := range listeners {
			select {
			case ch <- change:
			default:
			}
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/", mygo.FileServer(ui.Files()))
	mux.HandleFunc("/diagnostics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = previewEncode(w, map[string]int32{"trafficSubscriptions": trafficViews.Load()})
	})
	// Reflect the actual bound service, rather than maintain another API registry.
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", 405)
			return
		}
		if !desktop.app.ready.Load() {
			http.Error(w, "not ready", 503)
			return
		}
		var request struct {
			Method string            `json:"method"`
			Args   []json.RawMessage `json:"args"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&request); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if request.Method == "Desktop.WatchTraffic" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_ = desktop.watchTraffic(r.Context(), func(batch TrafficBatch) error {
				if err := previewEncode(w, batch); err != nil {
					return err
				}
				return http.NewResponseController(w).Flush()
			})
			return
		}
		value, err := previewCall(r.Context(), desktop, request.Method, request.Args)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(400)
			_ = previewEncode(w, map[string]string{"error": err.Error()})
			return
		}
		_ = previewEncode(w, value)
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		ch := make(chan StateChange, 1)
		mu.Lock()
		listeners[ch] = struct{}{}
		mu.Unlock()
		defer func() { mu.Lock(); delete(listeners, ch); mu.Unlock() }()
		// Re-read after reconnecting too; a change may have happened while offline.
		select {
		case ch <- StateChange{Revision: desktop.app.revision.Load()}:
		default:
		}
		for {
			select {
			case <-r.Context().Done():
				return
			case change := <-ch:
				if err := previewEncode(w, change); err != nil {
					return
				}
				if err := http.NewResponseController(w).Flush(); err != nil {
					return
				}
			}
		}
	})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.URL.Path == "/rpc" || r.URL.Path == "/events" || r.URL.Path == "/diagnostics") && (r.Header.Get("X-Aicat-Preview") != token || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+r.Host)) {
			http.Error(w, "unauthorized", 403)
			return
		}
		mux.ServeHTTP(w, r)
	})}
	if err = os.WriteFile(path, []byte("http://"+listener.Addr().String()+"/?token="+token), 0600); err != nil {
		listener.Close()
		return nil, err
	}
	go server.Serve(listener)
	return func() { _ = server.Close() }, nil
}

func previewCall(ctx context.Context, desktop *Desktop, name string, raw []json.RawMessage) (result any, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("preview call: %v", value)
		}
	}()
	if !strings.HasPrefix(name, "Desktop.") {
		return nil, fmt.Errorf("unknown service")
	}
	method := reflect.ValueOf(desktop).MethodByName(strings.TrimPrefix(name, "Desktop."))
	if !method.IsValid() {
		return nil, fmt.Errorf("unknown method")
	}
	typ := method.Type()
	if typ.NumIn() != len(raw)+1 {
		return nil, fmt.Errorf("wrong argument count")
	}
	args := []reflect.Value{reflect.ValueOf(ctx)}
	for i, data := range raw {
		arg := reflect.New(typ.In(i + 1))
		if err := jsonv2.Unmarshal(data, arg.Interface(), json.FormatDurationAsNano(true)); err != nil {
			return nil, err
		}
		args = append(args, arg.Elem())
	}
	values := method.Call(args)
	if len(values) > 0 {
		last := values[len(values)-1]
		if last.Type().Implements(reflect.TypeFor[error]()) {
			if !last.IsNil() {
				return nil, last.Interface().(error)
			}
			values = values[:len(values)-1]
		}
	}
	if len(values) != 0 {
		return values[0].Interface(), nil
	}
	return nil, nil
}

// Match MyGo's JSON v2 wire semantics, especially nil slices as [] rather than null.
func previewEncode(w http.ResponseWriter, value any) error {
	if err := jsonv2.MarshalWrite(w, value, json.FormatDurationAsNano(true), jsontext.EscapeForJS(true)); err != nil {
		return err
	}
	_, err := w.Write([]byte{'\n'})
	return err
}
