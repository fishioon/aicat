//go:build darwin && !cgo

package traffic

import store "github.com/fishioon/aicat/internal/traffic"

func appForProcess(owner socketOwner) store.ClientApp {
	return store.ClientApp{Name: owner.command}
}
