package channelhost

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/config"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type EventHandler func(json.RawMessage) (any, error)
type StatusHandler func(string, string)

type Binding struct {
	Driver    string                `json:"driver,omitempty"`
	Local     *config.LocalMessages `json:"local,omitempty"`
	AccountID string                `json:"accountId"`
	Session   string                `json:"session"`
	BotID     string                `json:"botId"`
	Secret    string                `json:"secret"`
	Source    string                `json:"source"`
	Endpoint  string                `json:"endpoint,omitempty"`
}

type Manager struct {
	home    string
	mu      sync.Mutex
	process *process
}

type process struct {
	cmd      *exec.Cmd
	in       io.WriteCloser
	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]chan response
	accounts map[string]*Account
	done     chan struct{}
	next     atomic.Uint64
}

type response struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type Account struct {
	Binding
	p        *process
	onEvent  EventHandler
	onStatus StatusHandler
}

func New(home string) *Manager { return &Manager{home: home} }

func (m *Manager) Open(ctx context.Context, binding Binding, onEvent EventHandler, onStatus StatusHandler) (*Account, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.process != nil {
		select {
		case <-m.process.done:
			m.process = nil
		default:
		}
	}
	if m.process == nil {
		dir, err := channelplugin.RuntimeDir()
		if err != nil {
			return nil, err
		}
		script := "host.mjs"
		if binding.Driver == "wecom-local" {
			script = "wecom-local/host.ts"
		} else if binding.Driver != "" {
			return nil, errors.New("未知渠道宿主")
		}
		for _, file := range []string{"bun", script} {
			if _, err = os.Stat(filepath.Join(dir, file)); err != nil {
				return nil, errors.New("应用渠道运行资源不完整，请重新构建或修复应用")
			}
		}
		stateName := "channel-host"
		if binding.Driver != "" {
			stateName = binding.Driver
		}
		state := filepath.Join(m.home, "state", stateName)
		if err = os.MkdirAll(state, 0700); err != nil {
			return nil, err
		}
		entry := ""
		if binding.Driver == "" {
			entry, err = channelplugin.New(m.home).Entry(binding.AccountID)
			if err != nil {
				return nil, err
			}
		}
		cmd := exec.Command(filepath.Join(dir, "bun"), filepath.Join(dir, script), state, entry)
		cmd.Dir = dir
		cmd.Env = []string{"HOME=" + state, "TMPDIR=" + filepath.Join(state, "tmp"), "PATH=" + dir, "LANG=en_US.UTF-8"}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		in, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		cmd.Stderr = io.Discard
		p := &process{cmd: cmd, in: in, pending: make(map[string]chan response), accounts: make(map[string]*Account), done: make(chan struct{})}
		if err = cmd.Start(); err != nil {
			return nil, errors.New("应用内置 Bun 无法启动，请修复应用")
		}
		m.process = p
		go p.read(stdout)
	}
	p := m.process
	a := &Account{Binding: binding, p: p, onEvent: onEvent, onStatus: onStatus}
	p.mu.Lock()
	if _, exists := p.accounts[binding.AccountID]; exists {
		p.mu.Unlock()
		return nil, errors.New("账号已打开")
	}
	p.accounts[binding.AccountID] = a
	p.mu.Unlock()
	if _, err := p.call(ctx, "channel_open", binding); err != nil {
		p.mu.Lock()
		delete(p.accounts, binding.AccountID)
		empty := len(p.accounts) == 0
		p.mu.Unlock()
		if empty {
			p.stop()
			m.process = nil
		}
		return nil, err
	}
	return a, nil
}

func (p *process) write(value any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return json.NewEncoder(p.in).Encode(value)
}

func (p *process) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := "go-" + strconv.FormatUint(p.next.Add(1), 10)
	ch := make(chan response, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
	if err := p.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case reply := <-ch:
		if reply.Error != nil {
			return nil, fmt.Errorf("渠道请求失败：%s", reply.Error.Message)
		}
		return reply.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.done:
		return nil, errors.New("渠道宿主已退出")
	}
}

func (p *process) read(stdout io.ReadCloser) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		var frame struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				AccountID string          `json:"accountId"`
				Session   string          `json:"session"`
				Event     json.RawMessage `json:"event"`
				Phase     string          `json:"phase"`
				Error     string          `json:"error"`
			} `json:"params"`
			response
		}
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			break
		}
		p.mu.Lock()
		ch := p.pending[frame.ID]
		a := p.accounts[frame.Params.AccountID]
		p.mu.Unlock()
		if frame.Method == "" {
			if ch != nil {
				select {
				case ch <- frame.response:
				default:
				}
			}
			continue
		}
		if a == nil || a.Session != frame.Params.Session {
			continue
		}
		if frame.Method == "channel_status" {
			a.onStatus(frame.Params.Phase, frame.Params.Error)
		}
		if frame.Method == "message_push" {
			result, err := a.onEvent(frame.Params.Event)
			if err == nil {
				_ = p.write(map[string]any{"jsonrpc": "2.0", "id": frame.ID, "result": result})
			}
		}
	}
	_ = stdout.Close()
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
	close(p.done)
}

func (p *process) stop() {
	_ = p.in.Close()
	select {
	case <-p.done:
		return
	case <-time.After(5 * time.Second):
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-p.done:
		return
	case <-time.After(2 * time.Second):
	}
	_ = p.cmd.Process.Kill()
	<-p.done
}

func (m *Manager) Close(a *Account) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := a.p
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = p.call(ctx, "channel_close", map[string]any{"accountId": a.AccountID, "session": a.Session})
	p.mu.Lock()
	delete(p.accounts, a.AccountID)
	empty := len(p.accounts) == 0
	p.mu.Unlock()
	if empty {
		p.stop()
		if m.process == p {
			m.process = nil
		}
	}
}

func (a *Account) Done() <-chan struct{} { return a.p.done }
func (a *Account) PID() int              { return a.p.cmd.Process.Pid }
func (a *Account) Send(ctx context.Context, event any) (json.RawMessage, error) {
	return a.p.call(ctx, "channel_send", map[string]any{"accountId": a.AccountID, "session": a.Session, "event": event})
}

func (m *Manager) Validate(ctx context.Context, id, botID, secret, endpoint string) error {
	entry, err := channelplugin.New(m.home).Entry(id)
	if err != nil {
		return err
	}
	dir, err := channelplugin.RuntimeDir()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	raw, _ := json.Marshal(map[string]string{"botId": botID, "secret": secret, "endpoint": endpoint})
	cmd := exec.CommandContext(ctx, filepath.Join(dir, "bun"), filepath.Join(dir, "validate.mjs"), entry)
	cmd.Stdin = strings.NewReader(string(raw) + "\n")
	cmd.Env = []string{"HOME=" + m.home, "PATH=" + dir, "TMPDIR=" + os.TempDir()}
	output, err := cmd.Output()
	if err != nil || !bytes.Contains(output, []byte(`"validated":true`)) {
		return errors.New("机器人鉴权验证失败，请检查 Bot ID、Secret 和网络连接")
	}
	return nil
}
