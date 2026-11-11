//go:build nogui

package app

import "fmt"

func Run(Options) error { return fmt.Errorf("this CLI build has no UI; use aicat serve") }
