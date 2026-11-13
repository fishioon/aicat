package wecomlocal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/channelhost"
	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/extension"
	"github.com/fishioon/aicat/internal/messages"
)

const ID = "wecom-local"

var Extension = extension.Extension{
	ID: ID, Name: "企业微信客户端", Summary: "读取企业微信客户端本地数据库，增量保存消息到 Room", Lede: "使用已有密钥读取本地消息。原型仅保存到 Room，不发送消息或自动执行 Agent。", Categories: []string{"消息"},
	Build: func(env extension.Env) (extension.Parts, error) {
		c := &channel{home: env.Home, host: channelhost.New(env.Home), inbox: messages.NewInbox(env.Home, ID), state: messages.State{Phase: "stopped", Records: []messages.Record{}, Tasks: []messages.Task{}, RoomDir: filepath.Join(env.Home, "rooms")}}
		return extension.Parts{Messages: c, Run: c.Run}, nil
	},
}

type channel struct {
	home     string
	host     *channelhost.Manager
	inbox    *messages.Inbox
	mu       sync.Mutex
	state    messages.State
	onChange func()
}

func (c *channel) OnChange(fn func()) { c.mu.Lock(); c.onChange = fn; c.mu.Unlock() }
func (c *channel) notify() {
	c.mu.Lock()
	fn := c.onChange
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}
func (c *channel) settings() (*config.LocalMessages, error) {
	cfg, err := config.Load(c.home)
	if err != nil {
		return nil, err
	}
	if cfg.Extensions[ID].Agent.Enabled {
		return nil, errors.New("客户端原型仅保存消息到 Room，不支持自动执行 Agent")
	}
	local := cfg.Extensions[ID].Local
	if local == nil {
		return nil, errors.New("请配置本地账号、数据库目录和密钥文件")
	}
	return local, local.Validate()
}
func (c *channel) Snapshot() messages.State {
	c.mu.Lock()
	state := c.state
	state.Records = append([]messages.Record{}, state.Records...)
	c.mu.Unlock()
	local, err := c.settings()
	state.Local, state.Configured, state.Validated = local, err == nil, err == nil
	if local != nil {
		state.AccountID = local.AccountID
	}
	return state
}
func (c *channel) Installed() bool { return true }
func (c *channel) Validated() bool { _, err := c.settings(); return err == nil }
func (c *channel) Configure(account messages.AccountConfig) error {
	if account.Local == nil || account.Secret != "" {
		return errors.New("请填写本地数据库配置")
	}
	return config.SetLocalMessages(c.home, ID, *account.Local)
}
func (c *channel) ConfigureAgent(config.MessageAgent) error {
	return errors.New("客户端原型不支持自动执行 Agent")
}
func (c *channel) Send(context.Context, messages.Event) (messages.Receipt, error) {
	return messages.Receipt{}, errors.New("企业微信客户端原型只读，不支持发送")
}
func (c *channel) Validate(ctx context.Context) error {
	local, err := c.settings()
	if err != nil {
		return err
	}
	dir, err := channelplugin.RuntimeDir()
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(local)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "bun"), filepath.Join(dir, "wecom-local", "validate.ts"))
	cmd.Stdin = bytes.NewReader(raw)
	cmd.Env = []string{"HOME=" + c.home, "PATH=" + dir, "TMPDIR=" + os.TempDir()}
	output, err := cmd.Output()
	if err != nil || !bytes.Contains(output, []byte(`"validated":true`)) {
		return errors.New("本地数据库验证失败，请检查目录、账号、密钥和客户端版本")
	}
	return nil
}
func (c *channel) status(phase, message string) {
	c.mu.Lock()
	changed := c.state.Phase != phase || c.state.Error != message
	c.state.Phase, c.state.Error = phase, message
	c.mu.Unlock()
	if changed {
		c.notify()
	}
}
func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (c *channel) Run(ctx context.Context) {
	defer func() { c.mu.Lock(); c.state.PID = 0; c.mu.Unlock(); c.status("stopped", ""); c.notify() }()
	local, err := c.settings()
	if err != nil {
		c.status("unconfigured", err.Error())
		<-ctx.Done()
		return
	}
	source := "aicat-channel://" + ID + "/" + hash(local.AccountID+"\x00"+filepath.Clean(local.DataDir))
	for attempt := 0; ctx.Err() == nil; attempt++ {
		c.status("connecting", "")
		openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		account, err := c.host.Open(openCtx, channelhost.Binding{Driver: ID, AccountID: ID, Session: hash(time.Now().String()), Source: source, Local: local}, func(raw json.RawMessage) (any, error) {
			record, err := c.inbox.Receive(source, raw)
			if err != nil {
				c.status("error", "Room 日志保存失败，等待重试")
				return nil, err
			}
			c.mu.Lock()
			exists := false
			for _, prior := range c.state.Records {
				if prior.ID == record.ID {
					exists = true
					break
				}
			}
			if !exists {
				c.state.Records = append(c.state.Records, record)
				if len(c.state.Records) > 50 {
					c.state.Records = c.state.Records[len(c.state.Records)-50:]
				}
			}
			c.mu.Unlock()
			c.notify()
			return map[string]string{"source": record.Event.Source, "id": record.Event.ID, "status": "accepted"}, nil
		}, c.status)
		cancel()
		if err == nil {
			c.mu.Lock()
			c.state.PID = account.PID()
			c.mu.Unlock()
			c.notify()
			select {
			case <-ctx.Done():
			case <-account.Done():
			}
			c.host.Close(account)
			c.mu.Lock()
			c.state.PID = 0
			c.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
			err = errors.New("客户端读取宿主异常退出")
		}
		c.status("error", err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(1<<min(attempt, 5)) * time.Second):
		}
	}
}
