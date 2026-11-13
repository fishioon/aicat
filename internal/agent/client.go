package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type codexClient struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	events  chan rpcMessage
	done    chan struct{}
	wait    chan error

	writeMu sync.Mutex
	stateMu sync.Mutex
	pending map[uint64]chan rpcMessage
	endErr  error
	endOnce sync.Once
	nextID  atomic.Uint64
}

func startCodexClient(binary, workspace string) (*codexClient, error) {
	args := []string{"app-server", "--stdio", "-c", `permissions.aicat-room={filesystem={":project_roots"="write",":root"="read",":tmpdir"="write",":slash_tmp"="write"},network={enabled=true}}`, "-c", `default_permissions="aicat-room"`}
	command := exec.Command(binary, args...)
	command.Dir = workspace
	command.Stdin = nil
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex app-server stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open codex app-server stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start %s app-server --stdio: %w", binary, err)
	}
	client := &codexClient{
		command: command,
		stdin:   stdin,
		events:  make(chan rpcMessage, 1024),
		done:    make(chan struct{}),
		wait:    make(chan error, 1),
		pending: make(map[uint64]chan rpcMessage),
	}
	client.nextID.Store(1)
	go client.readLoop(stdout)
	go func() {
		client.wait <- command.Wait()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := client.request(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "aicat_messages",
			"title":   "aicat Messages",
			"version": "1",
		},
		"capabilities": map[string]bool{
			"experimentalApi": true,
		},
	}); err != nil {
		client.stop()
		return nil, err
	}
	if err := client.notify("initialized", map[string]any{}); err != nil {
		client.stop()
		return nil, err
	}
	return client, nil
}

func (client *codexClient) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := client.nextID.Add(1) - 1
	response := make(chan rpcMessage, 1)
	client.stateMu.Lock()
	client.pending[id] = response
	client.stateMu.Unlock()

	if err := client.write(map[string]any{"method": method, "id": id, "params": params}); err != nil {
		client.removePending(id)
		return nil, err
	}
	select {
	case message := <-response:
		return rpcResult(method, message)
	case <-ctx.Done():
		client.removePending(id)
		return nil, fmt.Errorf("codex %s: %w", method, ctx.Err())
	case <-client.done:
		client.removePending(id)
		select {
		case message := <-response:
			return rpcResult(method, message)
		default:
		}
		return nil, fmt.Errorf("codex %s: %w", method, client.terminalError())
	}
}

func rpcResult(method string, message rpcMessage) (json.RawMessage, error) {
	if message.Error != nil {
		return nil, fmt.Errorf("codex %s failed (%d): %s", method, message.Error.Code, message.Error.Message)
	}
	return message.Result, nil
}

func (client *codexClient) notify(method string, params any) error {
	return client.write(map[string]any{"method": method, "params": params})
}

func (client *codexClient) write(message any) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode codex app-server message: %w", err)
	}
	encoded = append(encoded, '\n')
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	select {
	case <-client.done:
		return client.terminalError()
	default:
	}
	if _, err := client.stdin.Write(encoded); err != nil {
		return fmt.Errorf("write codex app-server message: %w", err)
	}
	return nil
}

func (client *codexClient) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	var readErr error
	for scanner.Scan() {
		var message rpcMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			readErr = fmt.Errorf("decode codex app-server JSONL: %w", err)
			break
		}
		if message.Method != "" {

			select {
			case client.events <- message:
			case <-client.done:
				return
			}
			continue
		}
		id, ok := rawUint64(message.ID)
		if !ok {
			continue
		}
		client.stateMu.Lock()
		pending := client.pending[id]
		delete(client.pending, id)
		client.stateMu.Unlock()
		if pending != nil {
			pending <- message
		}
	}
	if readErr == nil && scanner.Err() != nil {
		readErr = fmt.Errorf("read codex app-server stdout: %w", scanner.Err())
	}
	if readErr != nil {
		_ = syscall.Kill(-client.command.Process.Pid, syscall.SIGKILL)
	}
	processErr := <-client.wait
	if readErr != nil {
		client.finish(readErr)
	} else if processErr != nil {
		client.finish(processErr)
	} else {
		client.finish(io.EOF)
	}
}

func (client *codexClient) rejectServerRequest(message rpcMessage) error {
	var id any
	if err := json.Unmarshal(message.ID, &id); err != nil {
		return err
	}
	return client.write(map[string]any{
		"id": id,
		"error": map[string]any{
			"code":    -32601,
			"message": "aicat Claw does not support interactive server requests",
		},
	})
}

func (client *codexClient) removePending(id uint64) {
	client.stateMu.Lock()
	delete(client.pending, id)
	client.stateMu.Unlock()
}

func (client *codexClient) finish(err error) {
	client.endOnce.Do(func() {
		if err == nil {
			err = io.EOF
		}
		client.stateMu.Lock()
		client.endErr = err
		client.stateMu.Unlock()
		close(client.done)
	})
}

func (client *codexClient) terminalError() error {
	client.stateMu.Lock()
	defer client.stateMu.Unlock()
	if client.endErr == nil {
		return errors.New("codex app-server stopped")
	}
	return client.endErr
}

func (client *codexClient) stop() {
	if client == nil || client.command == nil || client.command.Process == nil {
		return
	}
	_ = client.stdin.Close()
	_ = syscall.Kill(-client.command.Process.Pid, syscall.SIGINT)
	select {
	case <-client.done:
		return
	case <-time.After(5 * time.Second):
	}
	_ = syscall.Kill(-client.command.Process.Pid, syscall.SIGKILL)
	select {
	case <-client.done:
	case <-time.After(time.Second):
	}
}

func (client *codexClient) isDone() bool {
	select {
	case <-client.done:
		return true
	default:
		return false
	}
}

func rawUint64(value json.RawMessage) (uint64, bool) {
	if len(value) == 0 {
		return 0, false
	}
	var id uint64
	if err := json.Unmarshal(value, &id); err != nil {
		return 0, false
	}
	return id, true
}
