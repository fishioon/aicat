//go:build e2e

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lqqyt2423/go-mitmproxy/cert"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
	log "github.com/sirupsen/logrus"
)

const messageCount = 384

type collector struct {
	proxy.BaseAddon
	mu    sync.Mutex
	seen  map[string]int
	wrong int
	flow  *proxy.Flow
	done  chan struct{}
	once  sync.Once
}

func (c *collector) WebSocketMessage(f *proxy.Flow) {
	runtime.Gosched()
	time.Sleep(100 * time.Microsecond)
	msg := f.WebSocket.Messages[len(f.WebSocket.Messages)-1]
	c.record(f, msg)
}

func (c *collector) record(f *proxy.Flow, msg *proxy.WebSocketMessage) {
	text := string(msg.Content[:min(len(msg.Content), 20)])
	key := strings.SplitN(text, ":", 3)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flow = f
	if len(key) < 2 || msg.FromClient != (key[0] == "client") {
		c.wrong++
		return
	}
	c.seen[key[0]+":"+key[1]]++
}

func (c *collector) WebSocketEnd(f *proxy.Flow) {
	c.once.Do(func() { close(c.done) })
}

type observer struct{ *collector }

func (c *observer) WebSocketMessageReceived(f *proxy.Flow, msg *proxy.WebSocketMessage) {
	c.record(f, msg)
}

type snapshotter interface {
	Snapshot() []*proxy.WebSocketMessage
	MessageCount() uint64
}

func payload(prefix string, index int) []byte {
	size := 8192
	if index == messageCount/2 {
		size = 2 << 20
	}
	head := []byte(prefix + ":" + strconv.Itoa(index) + ":")
	return append(head, bytes.Repeat([]byte{byte(index % 251)}, size-len(head))...)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func run(secure bool) map[string]any {
	legacy := &collector{seen: map[string]int{}, done: make(chan struct{})}
	typed := &observer{&collector{seen: map[string]int{}, done: make(chan struct{})}}
	serverDone := make(chan error, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		must(conn.SetReadDeadline(time.Now().Add(30 * time.Second)))
		writer := make(chan error, 1)
		go func() {
			for i := 0; i < messageCount; i++ {
				if err := conn.WriteMessage(websocket.BinaryMessage, payload("server", i)); err != nil {
					writer <- err
					return
				}
			}
			writer <- nil
		}()
		for i := 0; i < messageCount; i++ {
			_, b, err := conn.ReadMessage()
			if err != nil {
				serverDone <- err
				return
			}
			if !bytes.Equal(b, payload("client", i)) {
				serverDone <- fmt.Errorf("upstream payload mismatch %d", i)
				return
			}
		}
		if err := <-writer; err != nil {
			serverDone <- err
			return
		}
		_, _, err = conn.ReadMessage()
		if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
			err = nil
		}
		serverDone <- err
	})
	var upstream *httptest.Server
	if secure {
		upstream = httptest.NewTLSServer(handler)
	} else {
		upstream = httptest.NewServer(handler)
	}
	defer upstream.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	must(err)
	addr := l.Addr().String()
	must(l.Close())
	ca, err := cert.NewSelfSignCAMemory()
	must(err)
	p, err := proxy.NewProxy(&proxy.Options{Addr: addr, SslInsecure: true, NewCaFunc: func() (cert.CA, error) { return ca, nil }})
	must(err)
	p.SetShouldInterceptRule(func(*http.Request) bool { return true })
	p.SetUpstreamProxy(func(*http.Request) (*url.URL, error) { return nil, nil })
	p.AddAddon(legacy)
	p.AddAddon(typed)
	go func() { _ = p.Start() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
		_ = p.Close()
	}()
	for deadline := time.Now().Add(5 * time.Second); ; {
		c, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			panic("proxy not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	proxyURL, _ := url.Parse("http://" + addr)
	dialer := websocket.Dialer{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, HandshakeTimeout: 5 * time.Second}
	endpoint := strings.Replace(upstream.URL, "http", "ws", 1) + "/ws"
	client, _, err := dialer.Dial(endpoint, nil)
	must(err)
	must(client.SetReadDeadline(time.Now().Add(30 * time.Second)))
	writer := make(chan error, 1)
	go func() {
		for i := 0; i < messageCount; i++ {
			if err := client.WriteMessage(websocket.BinaryMessage, payload("client", i)); err != nil {
				writer <- err
				return
			}
		}
		writer <- nil
	}()
	for i := 0; i < messageCount; i++ {
		_, b, err := client.ReadMessage()
		must(err)
		if !bytes.Equal(b, payload("server", i)) {
			panic("downstream payload mismatch")
		}
	}
	must(<-writer)
	must(client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
	must(<-serverDone)
	client.Close()
	select {
	case <-legacy.done:
	case <-time.After(5 * time.Second):
		panic("missing end callback")
	}
	legacy.mu.Lock()
	defer legacy.mu.Unlock()
	typed.mu.Lock()
	defer typed.mu.Unlock()
	result := map[string]any{"secure": secure, "forwardedEachDirection": messageCount, "legacyUnique": len(legacy.seen), "legacyWrongDirection": legacy.wrong, "typedUnique": len(typed.seen), "typedWrongDirection": typed.wrong}
	check := func(c *collector) bool {
		if len(c.seen) != messageCount*2 || c.wrong != 0 {
			return false
		}
		for _, n := range c.seen {
			if n != 1 {
				return false
			}
		}
		return true
	}
	result["legacyExact"] = check(legacy)
	result["typedExact"] = check(typed.collector)
	data := legacy.flow.WebSocket
	count, size := 0, 0
	if s, ok := any(data).(snapshotter); ok {
		for _, msg := range s.Snapshot() {
			count++
			size += len(msg.Content)
		}
		result["totalMessages"] = s.MessageCount()
		result["snapshotAPI"] = true
	} else {
		for _, msg := range data.Messages {
			count++
			size += len(msg.Content)
		}
		result["snapshotAPI"] = false
	}
	result["retainedMessages"] = count
	result["retainedBytes"] = size
	result["bounded"] = count <= 256 && size <= 1<<20
	return result
}

func main() {
	log.SetLevel(log.ErrorLevel)
	output := os.Args[1]
	must(os.MkdirAll(output, 0700))
	results := []map[string]any{run(false), run(true)}
	passed := true
	for _, r := range results {
		if r["legacyExact"] != true || r["typedExact"] != true || r["bounded"] != true || r["snapshotAPI"] != true {
			passed = false
		}
	}
	b, err := json.MarshalIndent(map[string]any{"passed": passed, "results": results, "command": os.Args}, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(output, "report.json"), append(b, '\n'), 0600))
	fmt.Println(string(b))
	if !passed {
		os.Exit(1)
	}
}
