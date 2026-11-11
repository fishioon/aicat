//go:build !nogui && !e2e

package app

import "github.com/fishioon/aicat/internal/uihost"

func desktopPreview(*uihost.Host) (func(), error) { return func() {}, nil }
