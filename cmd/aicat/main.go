package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/fishioon/aicat/internal/app"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	args := os.Args[1:]
	serve := len(args) > 0 && args[0] == "serve"
	if serve {
		args = args[1:]
	}
	fs := flag.NewFlagSet("aicat", flag.ContinueOnError)
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	home := fs.String("home", filepath.Join(userHome, ".aicat"), "aicat data directory (use a separate directory for testing)")
	addr := fs.String("addr", "", "override proxy/API listen address (default 127.0.0.1:9981)")
	intercept := fs.String("intercept", "", "override exact HTTPS host allowlist, comma separated; empty passes through")
	showVersion := fs.Bool("version", false, "print version")
	if err = fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if *showVersion {
		fmt.Println("aicat", version)
		return nil
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	abs, err := filepath.Abs(*home)
	if err != nil {
		return err
	}
	o := app.Options{Home: abs, Addr: *addr}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "intercept" {
			o.Intercept = []string{}
			for _, h := range strings.Split(*intercept, ",") {
				if h = strings.TrimSpace(h); h != "" {
					o.Intercept = append(o.Intercept, h)
				}
			}
		}
	})
	if serve {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return app.Serve(ctx, o)
	}
	return app.Run(o)
}
