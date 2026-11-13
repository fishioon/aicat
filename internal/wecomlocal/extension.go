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
	ID: ID, Name: "企业微信客户端", Summary: "读取本地消息到 Room，通过 MCP 调用客户端发送文本", Lede: "使用 Base64 密钥增量读取消息。Agent 通过 MCP 请求回复，客户端脚本发送并回读本地数据库确认。", Categories: []string{"消息"},
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
	sendMu   sync.Mutex
	account  *channelhost.Account
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
func (c *channel) remember(record messages.Record) {
	c.mu.Lock()
	found := false
	for index, prior := range c.state.Records {
		if prior.Event.Source == record.Event.Source && prior.Event.ID == record.Event.ID {
			c.state.Records[index] = record
			found = true
			break
		}
	}
	if !found {
		c.state.Records = append(c.state.Records, record)
		if len(c.state.Records) > 50 {
			c.state.Records = c.state.Records[len(c.state.Records)-50:]
		}
	}
	c.mu.Unlock()
	c.notify()
}
func (c *channel) settings() (*config.LocalMessages, error) {
	cfg, err := config.Load(c.home)
	if err != nil {
		return nil, err
	}
	if cfg.Extensions[ID].Agent.Enabled {
		return nil, errors.New("客户端扩展通过 MCP 接收发送请求，不自动执行 Agent")
	}
	local := cfg.Extensions[ID].Local
	if local == nil {
		return nil, errors.New("请配置本地账号、数据库目录和 Base64 密钥")
	}
	return local, local.Validate()
}
func (c *channel) Snapshot() messages.State {
	c.mu.Lock()
	state := c.state
	state.Records = append([]messages.Record{}, state.Records...)
	c.mu.Unlock()
	local, err := c.settings()
	if err == nil {
		_, err = c.key(local)
	}
	state.Local, state.Configured, state.Validated = local, err == nil, err == nil
	if local != nil {
		state.AccountID = local.AccountID
	}
	return state
}
func (c *channel) Installed() bool { return true }
func (c *channel) Validated() bool {
	local, err := c.settings()
	if err == nil {
		_, err = c.key(local)
	}
	return err == nil
}
func (c *channel) Configure(account messages.AccountConfig) error {
	if account.Local == nil {
		return errors.New("请填写本地数据库配置")
	}
	local := account.Local
	if err := local.Validate(); err != nil {
		return err
	}
	key := normalizeKey(account.Secret)
	if key == "" {
		var err error
		key, err = c.key(local)
		if err != nil {
			return err
		}
	}
	if !validKey(key) {
		return errors.New("请输入编码 16 字节原始输入的有效 Base64 密钥")
	}
	if err := c.validate(context.Background(), local, key); err != nil {
		return err
	}
	if err := c.saveKey(local, key); err != nil {
		return err
	}
	return config.SetLocalMessages(c.home, ID, *local)
}
func (c *channel) ConfigureAgent(config.MessageAgent) error {
	return errors.New("客户端扩展通过 MCP 接收发送请求，不自动执行 Agent")
}
func (c *channel) Validate(ctx context.Context) error {
	local, err := c.settings()
	if err != nil {
		return err
	}
	key, err := c.key(local)
	if err != nil {
		return err
	}
	return c.validate(ctx, local, key)
}
func (c *channel) validate(ctx context.Context, local *config.LocalMessages, key string) error {
	dir, err := channelplugin.RuntimeDir()
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(struct {
		*config.LocalMessages
		Key string `json:"key"`
	}{local, key})
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
	defer func() {
		c.mu.Lock()
		c.state.PID = 0
		c.account = nil
		c.mu.Unlock()
		c.status("stopped", "")
		c.notify()
	}()
	local, err := c.settings()
	if err != nil {
		c.status("unconfigured", err.Error())
		<-ctx.Done()
		return
	}
	key, err := c.key(local)
	if err != nil {
		c.status("unconfigured", err.Error())
		<-ctx.Done()
		return
	}
	source := "aicat-channel://" + ID + "/" + hash(local.AccountID+"\x00"+filepath.Clean(local.DataDir))
	for attempt := 0; ctx.Err() == nil; attempt++ {
		c.status("connecting", "")
		openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		account, err := c.host.Open(openCtx, channelhost.Binding{Driver: ID, AccountID: ID, Session: hash(time.Now().String()), Source: source, Local: local, Secret: key, SendScript: sendScript()}, func(raw json.RawMessage) (any, error) {
			record, err := c.inbox.Receive(source, raw)
			if err != nil {
				c.status("error", "Room 日志保存失败，等待重试")
				return nil, err
			}
			c.remember(record)
			return map[string]string{"source": record.Event.Source, "id": record.Event.ID, "status": "accepted"}, nil
		}, c.status)
		cancel()
		if err == nil {
			c.mu.Lock()
			c.account = account
			c.state.PID = account.PID()
			c.mu.Unlock()
			c.notify()
			select {
			case <-ctx.Done():
			case <-account.Done():
			}
			c.host.Close(account)
			c.mu.Lock()
			c.account = nil
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
