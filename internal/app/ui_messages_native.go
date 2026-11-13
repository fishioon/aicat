//go:build !nogui && !e2e

package app

import "github.com/egoist/mygo"

func openMessageLogs(path string) error { return mygo.Shell.OpenPath(path) }
