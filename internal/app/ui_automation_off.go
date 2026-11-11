//go:build !nogui && !e2e

package app

import "github.com/egoist/mygo"

func automationMenus(*application) []*mygo.MenuItem { return nil }
