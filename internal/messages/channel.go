package messages

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/agent"
	"github.com/fishioon/aicat/internal/channelhost"
	"github.com/fishioon/aicat/internal/channelplugin"
)

type Channel struct {
	home, id, endpoint string
	host               *channelhost.Manager
	mu                 sync.Mutex
	sendMu             sync.Mutex
	state              State
	account            *channelhost.Account
	onChange           func()
	dispatcher         *Dispatcher
	rooms              *roomStore
	runner             *agent.Runner
	failures           chan channelFailure
	inbox              *Inbox
	manifest           *channelplugin.Manifest
	observations       chan any
}

func New(home, id, endpoint string, host *channelhost.Manager) *Channel {
	rooms := newRoomStore(home, id)
	return &Channel{home: home, id: id, endpoint: endpoint, host: host, rooms: rooms, inbox: &Inbox{rooms: rooms}, observations: make(chan any, 128), state: State{Phase: "stopped", Records: []Record{}, RoomDir: filepath.Join(home, "rooms")}}
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
	if os.IsNotExist(err) && c.nativePackage() {
		return AccountConfig{AccountID: "local"}, nil
	}
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
	if c.nativePackage() {
		_, e := c.nativeConfiguration()
		s.Configured = e == nil
		if account := c.selfID(); account != "" {
			s.AccountID = account
		}
	}
	s.Validated = c.Validated()
	s.Tasks = c.rooms.tasks.list(c.source(s.AccountID))
	return s
}

func (c *Channel) Configure(account AccountConfig) error {
	if c.nativePackage() {
		return c.configureNative(account.Values)
	}
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
	if err != "" {
		slog.Error("message channel status", "channel", c.id, "phase", phase)
	} else {
		slog.Info("message channel status", "channel", c.id, "phase", phase)
	}

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
		dispatcher.Signal()
	}
	c.changed()
}

func (c *Channel) Run(ctx context.Context) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	dispatcher := newDispatcher(ctx, c)
	c.mu.Lock()
	c.dispatcher = dispatcher
	c.failures = make(chan channelFailure, 1)
	c.mu.Unlock()
	defer func() {
		stop()
		dispatcher.Close()
		c.mu.Lock()
		c.dispatcher = nil
		c.failures = nil
		c.mu.Unlock()
	}()
	if c.nativePackage() {
		done := make(chan struct{})
		go func() { defer close(done); c.observeLoop(ctx) }()
		defer func() { stop(); <-done }()
	}
	go dispatcher.Run()

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
		if err != nil || credentials.AccountID == "" || (credentials.Secret == "" && !c.nativePackage()) {
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
		configuration := map[string]any{}
		if c.nativePackage() {
			configuration, err = c.nativeConfiguration()
			if err != nil {
				c.status("unconfigured", err.Error())
				return
			}
		}
		nativeContext, err := c.nativeContext()
		if err != nil {
			c.status("error", err.Error())
			return
		}
		openCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		a, err := c.host.Open(openCtx, channelhost.Binding{NativeContext: nativeContext, Configuration: configuration, AccountID: c.id, Session: newID(), BotID: credentials.AccountID, Secret: credentials.Secret, Source: source, Endpoint: c.endpoint},
			func(raw json.RawMessage) (any, error) { return c.receive(source, raw) }, c.status)
		cancel()
		if err == nil {
			c.mu.Lock()
			c.account = a
			c.state.PID = a.PID()
			c.mu.Unlock()
			dispatcher.Signal()
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
	if c.nativePackage() {
		return c.nativeSource()
	}
	return "aicat-channel://" + c.id + "/" + digest([]byte(identity))
}

func (c *Channel) receive(source string, raw json.RawMessage) (any, error) {
	var event Event
	if len(raw) > 256<<10 || json.Unmarshal(raw, &event) != nil || event.Source != source {
		return nil, errors.New("无效渠道消息")
	}
	if event.Type == PresentationUpdated {
		if err := c.inbox.updatePresentation(source, raw); err != nil {
			return nil, err
		}
		c.changed()
		return map[string]string{"source": source, "id": event.ID, "status": "accepted"}, nil
	}
	record, err := c.Receive(context.Background(), event)
	if err != nil {
		c.mu.Lock()
		c.state.Error = "Room 消息保存失败，等待重试"
		c.mu.Unlock()
		c.changed()
		return nil, err
	}
	c.mu.Lock()
	recovered := c.state.Error == "Room 消息保存失败，等待重试"
	if recovered {
		c.state.Error = ""
	}
	c.mu.Unlock()
	if recovered {
		c.changed()
	}
	return map[string]string{"source": record.Event.Source, "id": record.Event.ID, "status": "accepted"}, nil
}
func (c *Channel) Receive(ctx context.Context, event Event) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	credentials, err := c.credentials()
	if err != nil {
		return Record{}, err
	}
	source := c.source(credentials.AccountID)
	raw, err := json.Marshal(event)
	if err != nil {
		return Record{}, err
	}
	c.mu.Lock()
	d := c.dispatcher
	c.mu.Unlock()
	record, err := c.inbox.ReceiveAndDispatch(source, raw, d)
	if err != nil {
		return Record{}, err
	}
	c.mu.Lock()
	c.state.Records = append(c.state.Records, record)
	if len(c.state.Records) > 50 {
		c.state.Records = c.state.Records[len(c.state.Records)-50:]
	}
	c.mu.Unlock()
	c.changed()
	return record, nil
}
func (c *Channel) ReadRoom(ctx context.Context, room string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	credentials, err := c.credentials()
	if err != nil {
		return nil, err
	}
	source := c.source(credentials.AccountID)
	return c.inbox.Room(source, room)
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
	installed, err := channelplugin.New(c.home).Current(c.id)
	if err != nil {
		return ""
	}
	return digest(append(raw, []byte("\x00"+installed.Version+"\x00"+c.endpoint)...))
}
func (c *Channel) Validated() bool {
	if c.nativePackage() {
		_, err := c.nativeConfiguration()
		return err == nil
	}
	if !c.Installed() {
		return false
	}
	raw, err := os.ReadFile(c.credentialPath() + ".validated")
	return err == nil && string(raw) == c.validationKey()
}
func (c *Channel) Validate(ctx context.Context) error {
	if c.nativePackage() {
		_, err := c.nativeConfiguration()
		return err
	}
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

func (c *Channel) SetRunner(runner *agent.Runner) { c.runner = runner }
