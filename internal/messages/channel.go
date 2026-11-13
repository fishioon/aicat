package messages

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/channelhost"
	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/config"
)

type Channel struct {
	home, id, endpoint string
	host               *channelhost.Manager
	mu                 sync.Mutex
	sendMu             sync.Mutex
	state              State
	account            *channelhost.Account
	onChange           func()
	dispatcher         *dispatcher
	rooms              *roomStore
	failures           chan channelFailure
}

func New(home, id, endpoint string, host *channelhost.Manager) *Channel {
	return &Channel{home: home, id: id, endpoint: endpoint, host: host, rooms: newRoomStore(home, id), state: State{Phase: "stopped", Records: []Record{}, RoomDir: filepath.Join(home, "rooms")}}
}

func (c *Channel) OnChange(fn func()) { c.mu.Lock(); c.onChange = fn; c.mu.Unlock() }
func (c *Channel) changed() {
	c.mu.Lock()
	fn := c.onChange
	c.mu.Unlock()
	if fn != nil {
		fn()
	}
}
func (c *Channel) credentialPath() string { return filepath.Join(c.home, "credentials", c.id+".json") }
func (c *Channel) credentials() (AccountConfig, error) {
	var config AccountConfig
	b, err := os.ReadFile(c.credentialPath())
	if err == nil {
		err = json.Unmarshal(b, &config)
	}
	return config, err
}

func (c *Channel) Snapshot() State {
	c.mu.Lock()
	s := c.state
	s.Records = append([]Record{}, s.Records...)
	c.mu.Unlock()
	account, err := c.credentials()
	if err == nil {
		s.AccountID = account.AccountID
		s.Configured = account.AccountID != "" && account.Secret != ""
	}
	cfg, configErr := config.Load(c.home)
	if configErr == nil {
		s.Agent = cfg.Extensions[c.id].Agent
	}
	s.Validated = c.Validated()
	s.Tasks = c.rooms.tasks.list(c.source(s.AccountID))
	return s
}

func (c *Channel) Configure(account AccountConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.account != nil || c.state.Phase == "connecting" {
		return errors.New("请先停用扩展再修改账号")
	}
	account.AccountID = strings.TrimSpace(account.AccountID)
	account.Secret = strings.TrimSpace(account.Secret)
	if account.AccountID == "" || len(account.AccountID) > 256 || strings.ContainsAny(account.AccountID, "\r\n") || len(account.Secret) > 4096 {
		return errors.New("请填写有效的 Bot ID 和 Secret")
	}
	if account.Secret == "" {
		old, err := c.credentials()
		if err != nil || old.AccountID != account.AccountID {
			return errors.New("首次配置或更换 Bot ID 时需要填写 Secret")
		}
		account.Secret = old.Secret
	}
	if account.Secret == "" {
		return errors.New("请填写 Secret")
	}
	if err := writeJSON(c.credentialPath(), account); err != nil {
		return err
	}
	c.state.Phase = "stopped"
	c.state.Error = ""
	c.state.Records = []Record{}
	return nil
}

type channelFailure struct {
	retry   bool
	message string
}

func (c *Channel) status(phase, err string) {
	c.mu.Lock()
	if phase == "failed" || phase == "blocked" {
		select {
		case c.failures <- channelFailure{retry: phase == "failed", message: err}:
		default:
		}
		phase = "error"
	}
	c.state.Phase = phase
	c.state.Error = err
	dispatcher := c.dispatcher
	c.mu.Unlock()
	if phase == "connected" && dispatcher != nil {
		dispatcher.signal()
	}
	c.changed()
}

func (c *Channel) ConfigureAgent(settings config.MessageAgent) error {
	return config.SetMessageAgent(c.home, c.id, settings)
}

func (c *Channel) Run(ctx context.Context) {
	dispatcher := newDispatcher(ctx, c)
	c.mu.Lock()
	c.dispatcher = dispatcher
	c.failures = make(chan channelFailure, 1)
	c.mu.Unlock()
	defer func() { dispatcher.close(); c.mu.Lock(); c.dispatcher = nil; c.failures = nil; c.mu.Unlock() }()
	go dispatcher.run()

	defer func() {
		c.mu.Lock()
		c.account = nil
		c.state.PID = 0
		c.state.Phase = "stopped"
		c.mu.Unlock()
		c.changed()
	}()
	for attempt := 0; ctx.Err() == nil; attempt++ {
		credentials, err := c.credentials()
		if err != nil || credentials.AccountID == "" || credentials.Secret == "" {
			c.status("unconfigured", "请填写 Bot ID 和 Secret")
			<-ctx.Done()
			return
		}
		source := c.source(credentials.AccountID)
		select {
		case <-c.failures:
		default:
		}
		c.status("connecting", "")
		openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		a, err := c.host.Open(openCtx, channelhost.Binding{AccountID: c.id, Session: newID(), BotID: credentials.AccountID, Secret: credentials.Secret, Source: source, Endpoint: c.endpoint},
			func(raw json.RawMessage) (any, error) { return c.receive(source, raw) }, c.status)
		cancel()
		if err == nil {
			c.mu.Lock()
			c.account = a
			c.state.PID = a.PID()
			c.mu.Unlock()
			dispatcher.signal()
			c.changed()
			failure := channelFailure{retry: true, message: "渠道宿主异常退出"}
			select {
			case <-ctx.Done():
			case <-a.Done():
			case failure = <-c.failures:
			}
			c.host.Close(a)
			c.mu.Lock()
			c.account = nil
			c.state.PID = 0
			c.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
			err = errors.New(failure.message)
			if !failure.retry {
				c.status("error", failure.message)
				<-ctx.Done()
				return
			}
		}
		c.status("error", err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(1<<min(attempt, 5)) * time.Second):
		}
	}
}

func (c *Channel) source(identity string) string {
	return "aicat-channel://" + c.id + "/" + digest([]byte(identity))
}

func (c *Channel) receive(source string, raw json.RawMessage) (any, error) {
	var event Event
	if len(raw) > 256<<10 || json.Unmarshal(raw, &event) != nil || event.SpecVersion != "1.0" || event.ContentType != "application/json" || event.Type != Received ||
		event.Source != source || event.ID == "" || event.Data.RoomID == "" || event.Data.ID == "" {
		return nil, errors.New("无效渠道消息")
	}
	record, err := c.appendRecord(source, "", event, "")
	if err != nil {
		c.mu.Lock()
		c.state.Error = "Room 日志保存失败，等待重试"
		c.mu.Unlock()
		c.changed()
		return nil, err
	}
	c.mu.Lock()
	dispatcher := c.dispatcher
	c.mu.Unlock()
	if dispatcher != nil {
		if err := dispatcher.enqueue(source, record); err != nil {
			c.mu.Lock()
			c.state.Error = "Agent 任务保存失败，等待重试"
			c.mu.Unlock()
			c.changed()
			return nil, err
		}
	}
	c.mu.Lock()
	if c.state.Error == "Room 日志保存失败，等待重试" || c.state.Error == "Agent 任务保存失败，等待重试" {
		c.state.Error = ""
	}
	c.mu.Unlock()
	c.changed()
	return map[string]string{"source": event.Source, "id": event.ID, "status": "accepted"}, nil
}

func (c *Channel) appendRecord(identity, botID string, event Event, status string) (Record, error) {
	record, err := c.rooms.append(identity, botID, event, status)
	if err != nil {
		return Record{}, err
	}
	c.mu.Lock()
	c.state.Records = append(c.state.Records, record)
	if len(c.state.Records) > 50 {
		c.state.Records = c.state.Records[len(c.state.Records)-50:]
	}
	c.mu.Unlock()
	return record, nil
}

func (c *Channel) Installed() bool {
	_, err := channelplugin.New(c.home).Entry(c.id)
	return err == nil
}
func (c *Channel) validationKey() string {
	raw, err := os.ReadFile(c.credentialPath())
	if err != nil {
		return ""
	}
	descriptor, ok := channelplugin.Find(c.id)
	if !ok {
		return ""
	}
	return digest(append(raw, []byte("\x00"+descriptor.Version+"\x00"+c.endpoint)...))
}
func (c *Channel) Validated() bool {
	if !c.Installed() {
		return false
	}
	raw, err := os.ReadFile(c.credentialPath() + ".validated")
	return err == nil && string(raw) == c.validationKey()
}
func (c *Channel) Validate(ctx context.Context) error {
	account, err := c.credentials()
	if err != nil {
		return err
	}
	key := c.validationKey()
	if err = c.host.Validate(ctx, c.id, account.AccountID, account.Secret, c.endpoint); err != nil {
		_ = os.Remove(c.credentialPath() + ".validated")
		c.mu.Lock()
		c.state.Error = err.Error()
		c.mu.Unlock()
		return err
	}
	if err = os.WriteFile(c.credentialPath()+".validated", []byte(key), 0600); err != nil {
		return err
	}
	c.mu.Lock()
	c.state.Error = ""
	c.mu.Unlock()
	return nil
}
