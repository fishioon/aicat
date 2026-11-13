package messages

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func digest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func prepareDir(dir string) error {
	if info, err := os.Stat(dir); err == nil {
		if !info.IsDir() {
			return errors.New("Room 存储目录不可用")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := prepareDir(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	return syncDir(filepath.Dir(dir))
}

func writeJSON(file string, value any) error {
	dir := filepath.Dir(file)
	if err := prepareDir(dir); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".save-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(value)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), file); err != nil {
		return err
	}
	return syncDir(dir)
}

func appendJSONL(file string, value func(int64) any) error {
	dir := filepath.Dir(file)
	if err := prepareDir(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(file); err == nil && !info.Mode().IsRegular() {
		return errors.New("Room 日志必须是普通文件")
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	b := make([]byte, 4096)
	for end > 0 {
		start := max(int64(0), end-int64(len(b)))
		block := b[:end-start]
		if _, err = f.ReadAt(block, start); err != nil {
			return err
		}
		if i := bytes.LastIndexByte(block, '\n'); i >= 0 {
			end = start + int64(i) + 1
			break
		}
		end = start
	}
	if err = f.Truncate(end); err != nil {
		return err
	}
	if _, err = f.Seek(end, io.SeekStart); err != nil {
		return err
	}
	if err = json.NewEncoder(f).Encode(value(end)); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	return syncDir(dir)
}
