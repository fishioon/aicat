//go:build !darwin && !e2e

package certificate

import (
	"context"
	"errors"
)

func Remove(context.Context, string) error {
	return errors.New("自动移除证书需要 macOS 桌面版")
}
