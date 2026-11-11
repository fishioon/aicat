//go:build !nogui && e2e

package app

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/fishioon/aicat/internal/uihost"
	"net"
	"net/http"
	"os"
	"strings"
)

// Only test builds expose an authenticated loopback preview of the same assets.
func desktopPreview(host *uihost.Host) (func(), error) {
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
	handler := host.Preview()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("X-Aicat-Preview") != token {
			http.Error(w, "unauthorized", 403)
			return
		}
		handler.ServeHTTP(w, r)
	})}
	if err = os.WriteFile(path, []byte("http://"+listener.Addr().String()+"/?token="+token), 0600); err != nil {
		listener.Close()
		return nil, err
	}
	go server.Serve(listener)
	return func() { _ = server.Close() }, nil
}
