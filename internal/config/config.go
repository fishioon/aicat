package config

import (
	"fmt"
	"github.com/fishioon/aicat/internal/rewrite"
	"github.com/fishioon/aicat/internal/traffic"
	"gopkg.in/yaml.v3"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Rewrite    rewrite.Config `yaml:"rewrite,omitempty"`
	UI         Preferences    `yaml:"ui"`
	Navigation struct {
		Pinned []string `yaml:"pinned"`
	} `yaml:"navigation"`
	SystemProxy struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"systemProxy"`
	// Extensions holds each extension's section by its ID: its switch now,
	// its own settings later.
	Extensions map[string]Extension `yaml:"extensions"`
	Gateway    struct {
		Addr              string   `yaml:"addr"`
		InterceptHosts    []string `yaml:"interceptHosts"`
		BufferedBodyLimit int64    `yaml:"bufferedBodyLimit"`
	} `yaml:"gateway"`
	Traffic struct {
		PinnedDomains     []string `yaml:"pinnedDomains,omitempty"`
		MaxRecords        int      `yaml:"maxRecords"`
		MaxBodyBytes      int64    `yaml:"maxBodyBytes"`
		MaxTotalBodyBytes int64    `yaml:"maxTotalBodyBytes"`
	} `yaml:"traffic"`
}

// Extension is an extension's section in aicat.yaml.
type Extension struct {
	// Enabled is off unless the user turns it on; traffic alone is on by
	// default.
	Enabled bool `yaml:"enabled"`
}

// ExtensionEnabled reports whether the extension of this ID is turned on.
func (c Config) ExtensionEnabled(id string) bool { return c.Extensions[id].Enabled }

func Load(home string) (Config, error) {
	var c Config
	c.UI = Preferences{Language: "system", Theme: "system"}
	c.Navigation.Pinned = []string{"traffic"}
	c.Extensions = map[string]Extension{"traffic": {Enabled: true}}
	c.Gateway.Addr = "127.0.0.1:9981"
	c.Gateway.BufferedBodyLimit = 1 << 20
	c.Traffic.MaxRecords = 2000
	c.Traffic.MaxBodyBytes = 256 << 10
	c.Traffic.MaxTotalBodyBytes = 64 << 20
	if err := os.MkdirAll(home, 0700); err != nil {
		return c, err
	}
	path := filepath.Join(home, "aicat.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data, err = yaml.Marshal(c)
		if err == nil {
			err = os.WriteFile(path, data, 0600)
		}
	} else if err == nil {
		err = yaml.Unmarshal(data, &c)
	}
	if err != nil {
		return c, fmt.Errorf("read %s: %w", path, err)
	}
	if _, ok := c.Extensions["traffic"]; !ok {
		// an empty extensions section still leaves traffic on
		if c.Extensions == nil {
			c.Extensions = map[string]Extension{}
		}
		c.Extensions["traffic"] = Extension{Enabled: true}
	}
	if err := c.UI.Validate(); err != nil {
		return c, err
	}
	return c, nil
}
func (c Config) TrafficOptions() traffic.Options {
	return traffic.Options{MaxRecords: c.Traffic.MaxRecords, MaxBodyBytes: c.Traffic.MaxBodyBytes, MaxTotalBodyBytes: c.Traffic.MaxTotalBodyBytes}
}

// Host canonicalizes one exact hostname/IP, removing the CONNECT port.
func Host(authority string) string {
	if host, _, err := net.SplitHostPort(authority); err == nil {
		authority = host
	}
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(authority)), ".")
}
