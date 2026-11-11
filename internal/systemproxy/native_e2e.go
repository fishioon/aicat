//go:build e2e

package systemproxy

import (
	"encoding/json"
	"errors"
	"github.com/fishioon/aicat/internal/osfixture"
	"net"
	"os"
	"strconv"
	"time"
)

const supported = true

func readServices() ([]service, error) {
	var services []service
	err := osfixture.Update(func(state map[string]json.RawMessage) error { return json.Unmarshal(state["services"], &services) })
	sortServices(services)
	return services, err
}

// Replace only the OS command, not the planner, WAL or recovery controller.
func runNetworksetup(args []string) error {
	var commandErr error
	crash := false
	err := osfixture.Update(func(state map[string]json.RawMessage) error {
		var count int
		_ = json.Unmarshal(state["proxyCommandCount"], &count)
		count++
		state["proxyCommandCount"], _ = json.Marshal(count)
		at := func(key string) bool { var n int; _ = json.Unmarshal(state[key], &n); return n > 0 && n == count }
		var failFrom int
		_ = json.Unmarshal(state["failProxyFrom"], &failFrom)
		if osfixture.Bool(state, "failApply") || at("failProxyAt") || failFrom > 0 && count >= failFrom {
			commandErr = errors.New("test apply failed")
			return nil
		}
		var services []service
		if err := json.Unmarshal(state["services"], &services); err != nil {
			return err
		}
		for i := range services {
			s := &services[i]
			if s.Name != args[1] || s.Unlisted {
				continue
			}
			before := clone(s.Proxies)
			if s.Proxies == nil {
				s.Proxies = map[string]any{}
			}
			p := s.Proxies
			switch args[0] {
			case "-setwebproxy", "-setsecurewebproxy":
				prefix := "HTTP"
				if args[0] == "-setsecurewebproxy" {
					prefix = "HTTPS"
				}
				port, _ := strconv.Atoi(args[3])
				p[prefix+"Proxy"], p[prefix+"Port"], p[prefix+"Enable"] = args[2], float64(port), float64(1)
			default:
				key := map[string]string{"-setwebproxystate": "HTTPEnable", "-setsecurewebproxystate": "HTTPSEnable", "-setautoproxystate": "ProxyAutoConfigEnable", "-setproxyautodiscovery": "ProxyAutoDiscoveryEnable", "-setsocksfirewallproxystate": "SOCKSEnable"}[args[0]]
				if key == "" {
					return errors.New("unexpected networksetup command")
				}
				p[key] = float64(0)
				if args[2] == "on" {
					p[key] = float64(1)
				}
			}
			if at("externalProxyAt") {
				p["HTTPProxy"] = "external-edit.invalid"
			}
			if osfixture.Bool(state, "recordProxyWrites") {
				var history []map[string]any
				_ = json.Unmarshal(state["proxyWrites"], &history)
				var addr string
				_ = json.Unmarshal(state["probeGateway"], &addr)
				conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
				if conn != nil {
					_ = conn.Close()
				}
				history = append(history, map[string]any{"command": args, "changes": []change{{ID: s.ID, Name: s.Name, Before: before, After: p}}, "gatewayReachable": dialErr == nil})
				state["proxyWrites"], _ = json.Marshal(history)
			}
			state["services"], _ = json.Marshal(services)
			if at("failProxyAfterWriteAt") {
				commandErr = errors.New("test command failed after write")
			}
			crash = at("crashProxyAfterWriteAt")
			return nil
		}
		return errors.New("test network service not found")
	})
	if crash {
		os.Exit(77)
	}
	return errors.Join(err, commandErr)
}
