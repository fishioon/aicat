// Package listenaddr describes local proxy listeners and reachable client endpoints.
package listenaddr

import (
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strconv"
)

func Addresses() ([]string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{"127.0.0.1": true, "0.0.0.0": true}
	var local []string
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, raw := range addrs {
			prefix, err := netip.ParsePrefix(raw.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr().Unmap()
			if ip.IsUnspecified() || ip.IsMulticast() {
				continue
			}
			if ip.Is6() && ip.IsLinkLocalUnicast() {
				ip = ip.WithZone(iface.Name)
			}
			value := ip.String()
			if !seen[value] {
				seen[value] = true
				local = append(local, value)
			}
		}
	}
	sort.Slice(local, func(i, j int) bool {
		a, _ := netip.ParseAddr(local[i])
		b, _ := netip.ParseAddr(local[j])
		if a.Is4() != b.Is4() {
			return a.Is4()
		}
		return local[i] < local[j]
	})
	return append([]string{"127.0.0.1", "0.0.0.0"}, local...), nil
}

func Parse(address string) (netip.Addr, int, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return netip.Addr{}, 0, fmt.Errorf("invalid listener address: %w", err)
	}
	ip, err := netip.ParseAddr(host)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || n < 1 || n > 65535 {
		return netip.Addr{}, 0, fmt.Errorf("an IP address and port from 1 to 65535 are required")
	}
	return ip.Unmap(), n, nil
}

func IsLocal(ip netip.Addr) bool {
	if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	addresses, err := Addresses()
	if err != nil {
		return false
	}
	for _, value := range addresses {
		other, _ := netip.ParseAddr(value)
		if other == ip.Unmap() {
			return true
		}
	}
	return false
}

func Validate(address string) error {
	ip, _, err := Parse(address)
	if err != nil {
		return err
	}
	if !ip.IsUnspecified() && !IsLocal(ip) {
		return fmt.Errorf("listener IP is not assigned to this computer: %s", ip)
	}
	return nil
}

// ClientAddress never uses an unspecified IP as an outbound destination.
func ClientAddress(address string) string {
	ip, port, err := Parse(address)
	if err != nil {
		return address
	}
	if ip.IsUnspecified() {
		if ip.Is4() {
			ip = netip.MustParseAddr("127.0.0.1")
		} else {
			ip = netip.IPv6Loopback()
		}
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(port))
}

func Overlaps(a, b string) bool {
	x, xp, xe := Parse(a)
	y, yp, ye := Parse(b)
	return xe == nil && ye == nil && xp == yp && (x == y || x.IsUnspecified() || y.IsUnspecified())
}
