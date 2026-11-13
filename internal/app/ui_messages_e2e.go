//go:build !nogui && e2e

package app

import (
	"encoding/json"
	"github.com/fishioon/aicat/internal/osfixture"
)

func openMessageLogs(path string) error {
	return osfixture.Update(func(state map[string]json.RawMessage) error {
		value, err := json.Marshal(path)
		if err == nil {
			state["messageLogsOpened"] = value
		}
		return err
	})
}
