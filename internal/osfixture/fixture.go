//go:build e2e

// Package osfixture exists only in isolated desktop end-to-end builds.
package osfixture

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"sync"
)

var mu sync.Mutex

func Update(fn func(map[string]json.RawMessage) error) error {
	mu.Lock()
	defer mu.Unlock()
	path := os.Getenv("AICAT_E2E_OS_STATE")
	if !filepath.IsAbs(path) {
		return fmt.Errorf("AICAT_E2E_OS_STATE must be an absolute isolated fixture path")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var value map[string]json.RawMessage
	if err = json.Unmarshal(b, &value); err != nil {
		return err
	}
	if err = fn(value); err != nil {
		return err
	}
	b, err = json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".connection-fixture-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(b); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func Bool(value map[string]json.RawMessage, key string) bool {
	var b bool
	_ = json.Unmarshal(value[key], &b)
	return b
}
