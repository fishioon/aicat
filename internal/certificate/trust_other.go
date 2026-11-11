//go:build !darwin && !e2e

package certificate

import (
	"context"
	"errors"
)

const TrustSupported = false

func Trusted(context.Context, string) (bool, error) {
	return false, errors.New("当前平台不支持钥匙串信任检测")
}
