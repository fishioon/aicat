package systemproxy

import (
	"errors"
	"fmt"
	"strconv"
)

type proxyCommand struct {
	args  []string
	after map[string]any
}

func enabled(value any) bool { return value == float64(1) || value == true }

func endpoint(d map[string]any, prefix string) (string, int) {
	host, _ := d[prefix+"Proxy"].(string)
	port, _ := d[prefix+"Port"].(float64)
	return host, int(port)
}

// Only change fields that networksetup can restore without credentials or
// rewriting unrelated proxy settings. Validate the whole batch before writing.
func validateChanges(changes []change) error {
	for _, c := range changes {
		if _, err := proxyCommands(c); err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
	}
	return nil
}

func proxyCommands(c change) ([]proxyCommand, error) {
	from, to := clone(c.Before), clone(c.After)
	for _, prefix := range []string{"HTTP", "HTTPS"} {
		if enabled(from[prefix+"ProxyAuthenticated"]) || from[prefix+"User"] != nil || from[prefix+"Password"] != nil {
			return nil, errors.New("暂不接管带认证的系统代理，避免凭据进入命令参数；请使用应用专用代理接入")
		}
		for _, suffix := range []string{"Enable", "Proxy", "Port"} {
			delete(from, prefix+suffix)
			delete(to, prefix+suffix)
		}
	}
	for _, key := range []string{"SOCKSEnable", "ProxyAutoConfigEnable", "ProxyAutoDiscoveryEnable"} {
		delete(from, key)
		delete(to, key)
	}
	if !equal(from, to) {
		return nil, errors.New("networksetup 不支持此项代理字段变更，已保留原设置")
	}
	var commands []proxyCommand
	state := clone(c.Before)
	appendCommand := func(args []string, updates map[string]any) {
		for key, value := range updates {
			state[key] = value
		}
		commands = append(commands, proxyCommand{args: args, after: clone(state)})
	}
	for _, p := range []struct{ prefix, set, toggle string }{
		{"HTTP", "-setwebproxy", "-setwebproxystate"},
		{"HTTPS", "-setsecurewebproxy", "-setsecurewebproxystate"},
	} {
		oldHost, oldPort := endpoint(state, p.prefix)
		host, port := endpoint(c.After, p.prefix)
		wantOn := enabled(c.After[p.prefix+"Enable"])
		if port < 0 || port > 65535 || wantOn && (host == "" || port == 0) {
			return nil, errors.New("代理地址或端口无效")
		}
		if host != oldHost || port != oldPort {
			appendCommand([]string{p.set, c.Name, host, strconv.Itoa(port)}, map[string]any{
				p.prefix + "Proxy": host, p.prefix + "Port": float64(port), p.prefix + "Enable": float64(1),
			})
		}
		if enabled(state[p.prefix+"Enable"]) != wantOn {
			word, value := "off", float64(0)
			if wantOn {
				word, value = "on", 1
			}
			appendCommand([]string{p.toggle, c.Name, word}, map[string]any{p.prefix + "Enable": value})
		}
	}
	for _, p := range []struct{ key, toggle string }{
		{"ProxyAutoConfigEnable", "-setautoproxystate"},
		{"ProxyAutoDiscoveryEnable", "-setproxyautodiscovery"},
		{"SOCKSEnable", "-setsocksfirewallproxystate"},
	} {
		wantOn := enabled(c.After[p.key])
		if enabled(state[p.key]) != wantOn {
			word, value := "off", float64(0)
			if wantOn {
				word, value = "on", 1
			}
			appendCommand([]string{p.toggle, c.Name, word}, map[string]any{p.key: value})
		}
	}
	if !equal(state, c.After) {
		return nil, errors.New("无法无损规划代理设置，未执行命令")
	}
	return commands, nil
}

func commandService(c change, expected map[string]any) (service, error) {
	services, err := readServices()
	if err != nil {
		return service{}, err
	}
	var found *service
	names := 0
	for _, s := range services {
		if !s.Unlisted && s.Name == c.Name {
			names++
		}
		if s.ID == c.ID {
			copy := s
			found = &copy
		}
	}
	if found == nil || found.Unlisted || found.Name != c.Name || names != 1 {
		return service{}, errors.New("网络服务或位置已变化，未覆盖设置；请回到原网络位置后重试恢复")
	}
	if !equal(found.Proxies, expected) {
		return service{}, errors.New("网络设置已在 aicat 外变化，保留现状与恢复记录")
	}
	return *found, nil
}

// networksetup commits each command separately. Journal both possible outcomes
// before every call; re-read by stable service ID, not localized command output.
func applyChanges(changes []change, checkpoint func(string, *writeProgress) error) error {
	if err := validateChanges(changes); err != nil {
		return err
	}
	for _, c := range changes {
		if _, err := commandService(c, c.Before); err != nil {
			return err
		}
	}
	for _, c := range changes {
		commands, _ := proxyCommands(c)
		expected := c.Before
		for _, command := range commands {
			current, err := commandService(c, expected)
			if err != nil {
				return err
			}
			if err := checkpoint(c.ID, &writeProgress{Current: current.Proxies, Pending: command.after}); err != nil {
				return err
			}
			// Check again after the durable write, before invoking the OS command.
			if _, err := commandService(c, current.Proxies); err != nil {
				return err
			}
			commandErr := runNetworksetup(command.args)
			after, readErr := commandService(c, command.after)
			if readErr != nil {
				return errors.Join(commandErr, fmt.Errorf("networksetup %s 写入后核验失败: %w", command.args[0], readErr))
			}
			if err := checkpoint(c.ID, &writeProgress{Current: after.Proxies}); err != nil {
				return errors.Join(commandErr, err)
			}
			if commandErr != nil {
				return commandErr
			}
			expected = after.Proxies
		}
	}
	return nil
}
