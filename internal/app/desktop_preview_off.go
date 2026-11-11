//go:build !nogui && !e2e

package app

func desktopPreview(*Desktop) (func(), error) { return func() {}, nil }
