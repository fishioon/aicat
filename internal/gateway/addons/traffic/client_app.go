package traffic

import (
	"net"
	"net/netip"
	"sync"

	store "github.com/fishioon/aicat/internal/traffic"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

type socketPair struct {
	client, listener netip.AddrPort
}

type clientState struct {
	id   string
	pair socketPair
	done chan struct{}

	// Protected by clientAppLookup.mu.
	queued, resolved bool
	app              store.ClientApp
}

func newClientState(c *proxy.ClientConn) *clientState {
	client := &clientState{done: make(chan struct{})}
	if !clientAppSupported {
		return client
	}
	peer, peerOK := c.Conn.RemoteAddr().(*net.TCPAddr)
	local, localOK := c.Conn.LocalAddr().(*net.TCPAddr)
	if peerOK && localOK && peer.AddrPort().Addr().Unmap().IsLoopback() {
		client.id = c.Id.String()
		client.pair = socketPair{normalizeEndpoint(peer.AddrPort()), normalizeEndpoint(local.AddrPort())}
	}
	return client
}

func normalizeEndpoint(endpoint netip.AddrPort) netip.AddrPort {
	return netip.AddrPortFrom(endpoint.Addr().Unmap(), endpoint.Port())
}

func (c *clientState) connected() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

const maxPendingClients = 128

type clientAppLookup struct {
	mu      sync.Mutex
	pending []*clientState
	running bool
}

func (a *Addon) begin(conn *proxy.ClientConn, start store.Start) (store.ID, error) {
	value, ok := a.clients.Load(conn)
	if !ok || value.(*clientState).id == "" {
		return a.r.Begin(start)
	}
	client := value.(*clientState)
	lookup := &a.clientApps
	lookup.mu.Lock()
	defer lookup.mu.Unlock()
	if client.resolved {
		start.ClientApp = client.app
	} else if client.queued || (len(lookup.pending) < maxPendingClients && client.connected()) {
		start.PendingClient = client.id
		if !client.queued {
			client.queued = true
			lookup.pending = append(lookup.pending, client)
		}
		if !lookup.running {
			lookup.running = true
			a.wg.Go(a.resolveClientApps)
		}
	}
	return a.r.Begin(start)
}

func (a *Addon) resolveClientApps() {
	lookup := &a.clientApps
	for {
		lookup.mu.Lock()
		batch := lookup.pending
		lookup.pending = nil
		if len(batch) == 0 {
			lookup.running = false
			lookup.mu.Unlock()
			return
		}
		lookup.mu.Unlock()

		apps := lookupClientApps(batch)
		resolved := make(map[string]store.ClientApp, len(batch))
		lookup.mu.Lock()
		for _, client := range batch {
			// The endpoint pair can be reused once this connection closes.
			if client.connected() {
				client.app = apps[client.pair]
			}
			client.resolved = true
			resolved[client.id] = client.app
		}
		lookup.mu.Unlock()
		a.r.ResolveClientApps(resolved)
	}
}
