package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/fishioon/aicat/internal/channelplugin"
)

func (c *Channel) PrepareNative(ctx context.Context) error {
	m, err := channelplugin.New(c.home).Manifest(c.id)
	if err != nil {
		return err
	}
	if m.Prepare == "" {
		_, err := c.nativeConfiguration()
		return err
	}
	locks := []*os.File{}
	defer func() {
		for _, lock := range locks {
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			_ = lock.Close()
		}
	}()
	for _, capability := range m.Capabilities.Native {
		file := filepath.Join(os.TempDir(), "aicat-native-"+capability+"-"+strconv.Itoa(os.Getuid())+".lock")
		lock, err := os.OpenFile(file, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			return err
		}
		locks = append(locks, lock)
		if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			return errors.New("渠道正在准备启用，请稍后重试")
		}
	}
	runtime, err := channelplugin.RuntimeDir()
	if err != nil {
		return err
	}
	entry, err := channelplugin.New(c.home).Entry(c.id)
	if err != nil {
		return err
	}
	values, err := c.nativeValues(false)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(values)
	userHome, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	_ = entry
	store := channelplugin.New(c.home)
	installed, err := store.Current(c.id)
	if err != nil {
		return err
	}
	script := filepath.Join(store.Directory(c.id), "node_modules", filepath.FromSlash(installed.Package), m.Prepare)
	cmd := exec.CommandContext(ctx, filepath.Join(runtime, "bun"), script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 3 * time.Second
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Env = append([]string{"HOME=" + userHome, "PATH=" + runtime, "TMPDIR=" + os.TempDir(), "AICAT_SQLITE3MC_LIBRARY=" + filepath.Join(runtime, "sqlite3mc", "libsqlite3mc.dylib")}, nativePreparationEnvironment()...)
	output, runErr := cmd.Output()
	var result struct {
		Values map[string]any `json:"values"`
		Error  string         `json:"error"`
	}
	if json.Unmarshal(output, &result) != nil {
		return errors.New("渠道准备失败，请检查平台登录状态和系统权限")
	}
	if runErr != nil || result.Error != "" || result.Values == nil {
		if result.Error != "" && len(result.Error) <= 512 {
			return errors.New(result.Error)
		}
		return errors.New("渠道准备未完成")
	}
	return c.configureNative(result.Values)
}

func (c *Channel) nativeContext() (map[string]string, error) {
	c.mu.Lock()
	m := c.manifest
	c.mu.Unlock()
	if m == nil || len(m.Capabilities.Native) == 0 {
		return nil, nil
	}
	runtime, err := channelplugin.RuntimeDir()
	if err != nil {
		return nil, err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	result := map[string]string{"sqliteLibrary": filepath.Join(runtime, "sqlite3mc", "libsqlite3mc.dylib"), "userHome": userHome}
	addNativeTestContext(result)
	return result, nil
}
