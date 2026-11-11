package traffic

import (
	"context"
	"maps"
	"net/netip"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	store "github.com/fishioon/aicat/internal/traffic"
)

const clientAppSupported = true

type socketOwner struct {
	pid     int
	command string
}

func lookupClientApps(batch []*clientState) map[socketPair]store.ClientApp {
	wanted := make(map[socketPair]bool, len(batch))
	ports := make(map[uint16]bool)
	for _, client := range batch {
		if client.connected() {
			wanted[client.pair] = true
			ports[client.pair.listener.Port()] = true
		}
	}
	if len(ports) == 0 {
		return nil
	}
	args := []string{"-nP", "+c", "0", "-F0pcn"}
	for _, port := range slices.Sorted(maps.Keys(ports)) {
		args = append(args, "-i", "TCP:"+strconv.Itoa(int(port)))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/sbin/lsof", args...).Output()
	if err != nil {
		return nil
	}
	owners := matchSocketOwners(string(output), wanted)
	apps := make(map[socketPair]store.ClientApp, len(owners))
	byPID := make(map[int]store.ClientApp)
	for pair, owner := range owners {
		if owner.pid == 0 {
			continue
		}
		app, ok := byPID[owner.pid]
		if !ok {
			app = appForProcess(owner)
			byPID[owner.pid] = app
		}
		apps[pair] = app
	}
	return apps
}

func matchSocketOwners(output string, wanted map[socketPair]bool) map[socketPair]socketOwner {
	owners := make(map[socketPair]socketOwner)
	var owner socketOwner
	for field := range strings.SplitSeq(output, "\x00") {
		field = strings.TrimLeft(field, "\n")
		if len(field) < 2 {
			continue
		}
		switch field[0] {
		case 'p':
			pid, _ := strconv.Atoi(field[1:])
			owner = socketOwner{pid: pid}
		case 'c':
			owner.command = strings.TrimSpace(field[1:])
		case 'n':
			source, dest, connected := strings.Cut(field[1:], "->")
			if !connected || owner.pid <= 0 {
				continue
			}
			client, clientErr := netip.ParseAddrPort(source)
			listener, listenerErr := netip.ParseAddrPort(dest)
			if clientErr != nil || listenerErr != nil {
				continue
			}
			pair := socketPair{normalizeEndpoint(client), normalizeEndpoint(listener)}
			if !wanted[pair] {
				continue
			}
			if previous, found := owners[pair]; found && previous.pid != owner.pid {
				owners[pair] = socketOwner{} // Ambiguous even if the same owner appears again.
			} else {
				owners[pair] = owner
			}
		}
	}
	return owners
}
