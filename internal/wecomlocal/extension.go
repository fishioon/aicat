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

	"github.com/fishioon/aicat/internal/agent"
	"github.com/fishioon/aicat/internal/channelconfig"
	"github.com/fishioon/aicat/internal/channelhost"
	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/extension"
	"github.com/fishioon/aicat/internal/messages"
)

const ID = "wecom-local"

var Extension = extension.Extension{
	ID: ID, Name: "企业微信客户端", Summary: "读取本地消息到 Room，通过 MCP 调用客户端发送文本", Lede: "增量读取本地消息，按会话配置私聊、群聊提及与关键词触发。Agent 通过 MCP 回复，客户端脚本完成发送即返回成功。", Categories: []string{"消息渠道"},
	Build: func(env extension.Env) (extension.Parts, error) {
		c := &channel{home: env.Home, host: channelhost.New(env.Home), inbox: messages.NewInbox(env.Home, ID), mcp: env.MCPSend, mcpReady: env.MCPReady, state: messages.State{Phase: "stopped", Records: []messages.Record{}, Tasks: []messages.Task{}, RoomDir: filepath.Join(env.Home, "rooms")}}
		return extension.Parts{Messages: c, Configuration: c, Run: c.Run}, nil
	},
}

type channel struct {
	home          string
	host          *channelhost.Manager
	inbox         *messages.Inbox
	mu            sync.Mutex
	sendMu        sync.Mutex
	account       *channelhost.Account
	state         messages.State
	onChange      func()
	mcpReady      func() bool
	mcp           func(context.Context, string, messages.Event) (messages.Receipt, error)
	dispatcher    *messages.Dispatcher
	baseline      int64
	baselineReady bool
	agentError    string
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
func (c *channel) settings() (*channelconfig.LocalMessages, error) {
	cfg, err := channelconfig.Load(c.home, ID)
	if err != nil {
		return nil, err
	}

	local := cfg.Local
	if local == nil {
		return nil, errors.New("请配置本地账号、数据库目录和 Base64 密钥")
	}
	return local, local.Validate()
}
func (c *channel) Snapshot() messages.State {
	c.mu.Lock()
	state := c.state
	state.Records = append([]messages.Record{}, state.Records...)
	if c.agentError != "" {
		state.Error = c.agentError
	}
	c.mu.Unlock()
	local, err := c.settings()
	if err == nil {
		_, err = c.key(local)
	}
	state.Local, state.Configured, state.Validated = local, err == nil, err == nil
	if local != nil {
		state.AccountID = local.AccountID
	}
	if cfg, e := channelconfig.Load(c.home, ID); e == nil {
		state.Agent = cfg.Agent
	}
	if local != nil {
		state.Tasks = c.inbox.Tasks(c.source(local))
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
	cfg, err := channelconfig.Load(c.home, ID)
	if err != nil {
		return err
	}
	if cfg.Agent.Enabled && len(local.ReplyRooms) == 0 {
		return errors.New("停用 Codex 自动处理后才能移除全部自动处理会话")
	}
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
	return channelconfig.SetLocalMessages(c.home, ID, *local)
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
func (c *channel) validate(ctx context.Context, local *channelconfig.LocalMessages, key string) error {
	dir, err := channelplugin.RuntimeDir()
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(struct {
		*channelconfig.LocalMessages
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
	if phase == "connected" {
		c.mu.Lock()
		d := c.dispatcher
		c.mu.Unlock()
		if d != nil {
			d.Signal()
		}
	}
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
	source := c.source(local)
	cfg, _ := channelconfig.Load(c.home, ID)
	automatic := cfg.Agent.Enabled
	if automatic {
		d := c.inbox.Dispatcher(ctx, messages.DispatcherOptions{Source: source, Settings: cfg.Agent, Connected: c.connected, Changed: c.notify, Error: func(e string) { c.mu.Lock(); c.agentError = e; c.mu.Unlock(); c.notify() }, Accept: func(m messages.Message) bool { return c.accept(local, m) }, TriggerReason: func(m messages.Message) string { return triggerReason(local, m) }, Instructions: localAgentInstructions, UpdateInstructions: true, Execute: func(ctx context.Context, r *agent.Runner, thread string, task messages.Task, prompt string, started chan<- struct{}) error {
			return c.runAgent(ctx, r, local, thread, task, prompt, started)
		}})
		c.mu.Lock()
		c.dispatcher = d
		c.baselineReady = false
		c.mu.Unlock()
		go d.Run()
		defer func() { d.Close(); c.mu.Lock(); c.dispatcher = nil; c.mu.Unlock() }()
	}
	for attempt := 0; ctx.Err() == nil; attempt++ {
		c.status("connecting", "")
		openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		account, err := c.host.Open(openCtx, channelhost.Binding{Driver: ID, AccountID: ID, Session: hash(time.Now().String()), Source: source, Local: local, Secret: key, SendScript: sendScript()}, func(raw json.RawMessage) (any, error) {
			c.mu.Lock()
			ready := c.baselineReady
			d := c.dispatcher
			c.mu.Unlock()
			if automatic && !ready {
				return nil, errors.New("自动处理起点正在初始化")
			}
			record, err := c.inbox.Receive(source, raw)
			if err != nil {
				c.status("error", "Room 日志保存失败，等待重试")
				return nil, err
			}
			if d != nil {
				if err = d.Enqueue(source, record); err != nil {
					c.status("error", "Agent 任务保存失败，等待重试")
					return nil, err
				}
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
		if err == nil && automatic {
			if e := c.prepareBaseline(local, account.Position); e != nil {
				c.host.Close(account)
				err = e
			}
		}
		if err == nil {
			c.mu.Lock()
			c.account = account
			c.state.PID = account.PID()
			c.mu.Unlock()
			c.notify()
			c.mu.Lock()
			d := c.dispatcher
			c.mu.Unlock()
			if d != nil {
				d.Signal()
			}
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
