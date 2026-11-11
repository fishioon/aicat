package config

import (
	"fmt"
	"golang.org/x/net/idna"
	"net"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnableIntercept edits only gateway.interceptHosts, preserving other settings
// and comments. All config updates share the same atomic write path.
func EnableIntercept(home, host string) error {
	return SetInterceptEnabled(home, host, true)
}

// InterceptHost canonicalizes one decryption entry: an exact hostname or IP, or
// a leading wildcard such as *.example.com, which covers every subdomain of
// example.com at any depth but not example.com itself.
func InterceptHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.String(), nil
	}
	prefix := ""
	if strings.HasPrefix(host, "*.") {
		prefix, host = "*.", host[2:]
	}
	if strings.Contains(host, "*") || prefix != "" && (strings.Trim(host, ".") == "" || net.ParseIP(host) != nil) {
		return "", fmt.Errorf("通配符只支持 *.example.com 这一种写法，匹配所有子域名")
	}
	if strings.ContainsAny(host, ":/\\?#@ \t\n") {
		return "", fmt.Errorf("只填写域名或 IP，不含协议、端口或路径")
	}
	host, err := idna.Lookup.ToASCII(strings.ToLower(strings.TrimSuffix(host, ".")))
	if err != nil || len(host) == 0 || len(host) > 253 {
		return "", fmt.Errorf("请输入有效域名或 IP")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("请输入有效域名")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", fmt.Errorf("请输入有效域名")
			}
		}
	}
	return prefix + host, nil
}

func SetInterceptEnabled(home, host string, enabled bool) error {
	host, err := InterceptHost(host)
	if err != nil {
		return err
	}
	return update(home, func(root *yaml.Node) error {
		gateway := value(root, "gateway", yaml.MappingNode)
		if gateway.Kind != yaml.MappingNode {
			return fmt.Errorf("gateway must contain a mapping")
		}
		list := value(gateway, "interceptHosts", yaml.SequenceNode)
		var hosts []string
		if err := list.Decode(&hosts); err != nil {
			return fmt.Errorf("interceptHosts: %w", err)
		}
		kept := []string{}
		for _, existing := range hosts {
			if Host(existing) != host {
				kept = append(kept, existing)
			}
		}
		if enabled {
			kept = append(kept, host)
		}
		return list.Encode(kept)
	})
}

func value(mapping *yaml.Node, key string, kind yaml.Kind) *yaml.Node {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	n := &yaml.Node{Kind: kind}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, n)
	return n
}
