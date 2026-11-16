package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var ErrTurnEnded = errors.New("Codex turn 已结束")

var ErrUnknown = errors.New("Codex 任务结果不明")

type Runner struct {
	mu           sync.Mutex
	client       *codexClient
	sessions     map[string]*turnSession
	closed       bool
	loaded       map[string]bool
	mcpURL       string
	mcpTokenFile string
}

type turnSession struct {
	events chan rpcMessage
	id     string
}

type Options struct {
	MCPURL       string
	MCPTokenFile string
}

func New() *Runner { return NewWithMCP(Options{}) }

func NewWithMCP(options Options) *Runner {
	return &Runner{sessions: make(map[string]*turnSession), loaded: map[string]bool{}, mcpURL: options.MCPURL, mcpTokenFile: options.MCPTokenFile}
}

func executable(binary string) (string, error) {
	if binary != "" {
		return exec.LookPath(binary)
	}
	if p, err := exec.LookPath("codex"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/Applications/Codex.app/Contents/Resources/codex", "/Applications/ChatGPT.app/Contents/Resources/codex-cli/CodexCLI.app/Contents/MacOS/codex"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return p, nil
		}
	}
	return "", errors.New("未找到 Codex，请安装 Codex 或配置可执行文件路径")
}

func (r *Runner) connect(binary, workspace string) (*codexClient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("Codex 驱动已停止")
	}
	if r.client != nil && !r.client.isDone() {
		return r.client, nil
	}
	path, err := executable(binary)
	if err != nil {
		return nil, err
	}
	client, err := startCodexClient(path, workspace, r.mcpURL, r.mcpTokenFile)
	if err != nil {
		return nil, err
	}
	r.client = client
	r.loaded = map[string]bool{}
	go r.route(client)
	return client, nil
}

func (r *Runner) route(client *codexClient) {
	for {
		select {
		case <-client.done:
			return
		case event := <-client.events:
			var p struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(event.Params, &p)
			r.mu.Lock()
			session := r.sessions[p.ThreadID]
			r.mu.Unlock()
			if session != nil {
				select {
				case session.events <- event:
				case <-client.done:
					return
				}
			} else if len(event.ID) > 0 {
				_ = client.rejectServerRequest(event)
			}
		}
	}
}

type ThreadOptions struct {
	Instructions string
	Ephemeral    bool
	Model        string
}

func (r *Runner) Thread(ctx context.Context, binary, workspace, threadID string, options ThreadOptions) (string, error) {
	client, err := r.connect(binary, filepath.Dir(workspace))
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	loaded := r.loaded[threadID]
	r.mu.Unlock()
	if threadID != "" && loaded {
		return threadID, nil
	}
	params := map[string]any{"cwd": workspace, "approvalPolicy": "never", "permissions": "aicat-room", "runtimeWorkspaceRoots": []string{workspace}, "developerInstructions": options.Instructions}
	if options.Model != "" {
		params["model"] = options.Model
	}
	method := "thread/start"
	if threadID == "" {
		params["ephemeral"] = options.Ephemeral
		params["serviceName"] = "aicat_messages"
	} else {
		method = "thread/resume"
		params["threadId"] = threadID
		params["excludeTurns"] = true
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := client.request(callCtx, method, params)
	if err != nil {
		return "", err
	}
	var response struct {
		Model  string `json:"model"`
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(result, &response) != nil || response.Thread.ID == "" {
		return "", errors.New("Codex 未返回 thread.id")
	}
	if options.Model != "" && response.Model != "" && response.Model != options.Model {
		return "", fmt.Errorf("Codex 返回的模型 %s 与 Room 选择的 %s 不一致", response.Model, options.Model)
	}
	if threadID != "" && threadID != response.Thread.ID {
		return "", errors.New("Codex 恢复了不同的会话")
	}
	r.mu.Lock()
	r.loaded[response.Thread.ID] = true
	r.mu.Unlock()
	return response.Thread.ID, nil
}

func (r *Runner) Unsubscribe(ctx context.Context, threadID string) error {
	r.mu.Lock()
	client := r.client
	delete(r.loaded, threadID)
	r.mu.Unlock()
	if client == nil || client.isDone() {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := client.request(callCtx, "thread/unsubscribe", map[string]string{"threadId": threadID})
	return err
}

func (r *Runner) UpdateInstructions(ctx context.Context, threadID, instructions string) error {
	r.mu.Lock()
	client := r.client
	r.mu.Unlock()
	if client == nil || client.isDone() {
		return errors.New("Codex 连接已关闭")
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := client.request(callCtx, "thread/inject_items", map[string]any{"threadId": threadID, "items": []any{map[string]any{"type": "message", "role": "developer", "content": []any{map[string]string{"type": "input_text", "text": instructions}}}}})
	return err
}

func (r *Runner) Turn(ctx context.Context, threadID, taskID, prompt string, started chan<- struct{}) error {
	return r.TurnWithModel(ctx, threadID, taskID, prompt, "", started)
}

func (r *Runner) TurnWithModel(ctx context.Context, threadID, taskID, prompt, model string, started chan<- struct{}) error {
	r.mu.Lock()
	client := r.client
	if client == nil || client.isDone() || r.closed {
		r.mu.Unlock()
		return errors.New("Codex 连接已关闭")
	}
	if r.sessions[threadID] != nil {
		r.mu.Unlock()
		return errors.New("Room 已有任务运行")
	}
	events := make(chan rpcMessage, 1024)
	session := &turnSession{events: events}
	r.sessions[threadID] = session
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.sessions, threadID); r.mu.Unlock() }()
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	params := map[string]any{"threadId": threadID, "clientUserMessageId": taskID, "approvalPolicy": "never", "input": []any{map[string]any{"type": "text", "text": prompt}}}
	if model != "" {
		params["model"] = model
	}
	result, err := client.request(callCtx, "turn/start", params)
	cancel()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnknown, err)
	}
	var response struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(result, &response) != nil || response.Turn.ID == "" {
		return fmt.Errorf("%w: 未返回 turn.id", ErrUnknown)
	}
	turnID := response.Turn.ID
	r.mu.Lock()
	session.id = turnID
	r.mu.Unlock()
	if started != nil {
		close(started)
	}
	for {
		select {
		case <-ctx.Done():
			stopCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			_, _ = client.request(stopCtx, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID})
			stop()
			return fmt.Errorf("%w: %v", ErrUnknown, ctx.Err())
		case <-client.done:
			return fmt.Errorf("%w: 进程退出", ErrUnknown)
		case event := <-events:
			if event.Method == "model/rerouted" && model != "" {
				var changed struct {
					TurnID  string `json:"turnId"`
					ToModel string `json:"toModel"`
				}
				if json.Unmarshal(event.Params, &changed) == nil && changed.TurnID == turnID && changed.ToModel != model {
					interruptCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
					_, _ = client.request(interruptCtx, "turn/interrupt", map[string]string{"threadId": threadID, "turnId": turnID})
					stop()
					return fmt.Errorf("Codex 将 Room 模型更改为 %s，本次处理已停止", changed.ToModel)
				}
			}
			if len(event.ID) > 0 {
				_ = client.rejectServerRequest(event)
			}
			if event.Method == "turn/completed" {
				var p struct {
					Turn struct {
						ID     string `json:"id"`
						Status string `json:"status"`
						Error  *struct {
							Message string `json:"message"`
						} `json:"error"`
					} `json:"turn"`
				}
				if json.Unmarshal(event.Params, &p) != nil || p.Turn.ID != turnID {
					continue
				}
				if p.Turn.Status == "completed" {
					return nil
				}
				if p.Turn.Error != nil {
					return fmt.Errorf("Codex %s: %s", p.Turn.Status, p.Turn.Error.Message)
				}
				return fmt.Errorf("Codex %s", p.Turn.Status)
			}
		}
	}
}

func (r *Runner) Steer(ctx context.Context, threadID, prompt string) error {
	r.mu.Lock()
	client := r.client
	turn := ""
	if session := r.sessions[threadID]; session != nil {
		turn = session.id
	}
	r.mu.Unlock()
	if client == nil || turn == "" {
		return ErrTurnEnded
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err := client.request(callCtx, "turn/steer", map[string]any{"threadId": threadID, "expectedTurnId": turn, "input": []any{map[string]string{"type": "text", "text": prompt}}})
	if err != nil && (strings.Contains(err.Error(), "not active") || strings.Contains(err.Error(), "expectedTurnId") || strings.Contains(err.Error(), "no active turn")) {
		return ErrTurnEnded
	}
	return err
}

func (r *Runner) Close() {
	r.mu.Lock()
	r.closed = true
	client := r.client
	r.mu.Unlock()
	if client != nil {
		client.stop()
	}
}
