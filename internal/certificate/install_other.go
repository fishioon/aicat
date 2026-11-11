//go:build !darwin && !e2e

package certificate

import (
	"context"
	"errors"
)

func Install(context.Context, string) error {
	return errors.New("自动安装证书需要 macOS 桌面版")
}
