//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/fishioon/aicat/internal/channelplugin"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	store := channelplugin.New(os.Args[1])
	id := "wecom"
	if len(os.Args) > 2 {
		id = os.Args[2]
	}
	if err := store.Install(ctx, id); err != nil {
		panic(err)
	}
	entry, err := store.Entry(id)
	if err != nil {
		panic(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"entry": entry})
}
