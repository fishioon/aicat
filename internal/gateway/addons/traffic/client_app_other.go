//go:build !darwin

package traffic

import store "github.com/fishioon/aicat/internal/traffic"

const clientAppSupported = false

func lookupClientApps([]*clientState) map[socketPair]store.ClientApp { return nil }
