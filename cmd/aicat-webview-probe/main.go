//go:build !nogui && webviewprobe

package main

import (
	"flag"
	"log"
	"path/filepath"

	_ "github.com/egoist/mygo/plugins/automation"
	"github.com/fishioon/aicat/internal/app"
)

func main() {
	home := flag.String("home", "", "isolated aicat Home for the WebView experiment")
	addr := flag.String("addr", "", "proxy listen address")
	preview := flag.String("preview", "", "optional loopback HTTP preview address")
	flag.Parse()
	if *home == "" {
		log.Fatal("--home is required")
	}
	root, err := filepath.Abs(*home)
	if err != nil {
		log.Fatal(err)
	}
	if err := app.RunWebViewProbe(app.Options{Home: root, Addr: *addr}, *preview); err != nil {
		log.Fatal(err)
	}
}
