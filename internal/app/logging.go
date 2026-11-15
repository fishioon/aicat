package app

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func openRuntimeLog(home string) (func(), error) {
	dir := filepath.Join(home, "logs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "aicat.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	previous := slog.Default()
	previousOutput := logrus.StandardLogger().Out
	logrus.SetOutput(io.MultiWriter(os.Stderr, file))
	slog.SetDefault(slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, file), nil)))
	return func() {
		slog.SetDefault(previous)
		logrus.SetOutput(previousOutput)
		_ = file.Close()
	}, nil
}
