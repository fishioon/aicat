package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/agent"
	"github.com/fishioon/aicat/internal/config"
)

type Task struct {
	ID        string  `json:"id"`
	Account   string  `json:"account"`
	Source    string  `json:"source"`
	Room      string  `json:"room"`
	Log       string  `json:"log"`
	Status    string  `json:"status"`
	Error     string  `json:"error,omitempty"`
	CreatedAt string  `json:"createdAt"`
	Trigger   Message `json:"trigger"`
}

type dispatcher struct {
	ctx      context.Context
	channel  *Channel
	runner   *agent.Runner
	settings config.MessageAgent
	mu       sync.Mutex
	active   map[string]chan struct{}
	blocked  map[string]bool
	wg       sync.WaitGroup
	done     chan struct{}
	wake     chan struct{}
	source   string
}

func newDispatcher(ctx context.Context, c *Channel) *dispatcher {
	cfg, _ := config.Load(c.home)
	credentials, _ := c.credentials()
	return &dispatcher{ctx: ctx, channel: c, runner: agent.New(), settings: cfg.Extensions[c.id].Agent, active: map[string]chan struct{}{}, blocked: map[string]bool{}, done: make(chan struct{}), wake: make(chan struct{}, 1), source: c.source(credentials.AccountID)}
}

func (d *dispatcher) enqueue(source string, record Record) error {
	message := record.Event.Data
	if !d.settings.Enabled || message.Type != "text" || strings.TrimSpace(message.Text.Content) == "" {
		return nil
	}
	var timestamp time.Time
	if message.Time != nil {
		timestamp = time.UnixMilli(*message.Time)
	} else {
		var err error
		timestamp, err = time.Parse(time.RFC3339Nano, record.Event.Time)
		if err != nil {
			return nil
		}
	}
	age := time.Since(timestamp)
	if age > 10*time.Minute || age < -time.Minute {
		return nil
	}
	dir := d.channel.rooms.directory(source, message.RoomID)
	log := filepath.Join(dir, "context", "messages.log")
	task := Task{ID: record.ID, Account: d.channel.id, Source: source, Room: record.Event.Data.RoomID, Log: log, Status: "pending", CreatedAt: record.RecordedAt, Trigger: record.Event.Data}
	if err := d.channel.rooms.tasks.save(queuedTask{task: task, file: filepath.Join(dir, "tasks", task.CreatedAt+"-"+task.ID+".json")}); err != nil {
		return err
	}
	d.signal()
	return nil
}

func (d *dispatcher) signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *dispatcher) run() {
	defer close(d.done)
	if !d.settings.Enabled {
		<-d.ctx.Done()
		return
	}
	d.scan(true)
	d.scan(false)
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.wake:
			d.scan(false)
		}
	}
}

func (d *dispatcher) connected() bool {
	d.channel.mu.Lock()
	defer d.channel.mu.Unlock()
	return d.channel.account != nil && d.channel.state.Phase == "connected"
}

func (d *dispatcher) scan(recover bool) {
	store := d.channel.rooms.tasks
	if recover {
		for _, item := range store.selectTasks(d.source, "", "running") {
			item.task.Status = "unknown"
			item.task.Error = "应用退出前任务未完成，未自动重跑"
			if err := store.save(item); err != nil {
				d.block(filepath.Dir(filepath.Dir(item.file)))
			}
		}
		return
	}
	if !d.connected() {
		return
	}
	for _, item := range store.selectTasks(d.source, "", "pending") {
		dir := filepath.Dir(filepath.Dir(item.file))
		d.mu.Lock()
		if d.active[dir] != nil {
			select {
			case d.active[dir] <- struct{}{}:
			default:
			}
		} else if !d.blocked[dir] && d.ctx.Err() == nil {
			d.active[dir] = make(chan struct{}, 1)
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				defer func() { d.mu.Lock(); delete(d.active, dir); d.mu.Unlock(); d.signal() }()
				d.process(dir)
			}()
		}
		d.mu.Unlock()
	}
}

func (d *dispatcher) pending(dir string) []queuedTask {
	return d.channel.rooms.tasks.selectTasks(d.source, dir, "pending")
}

func triggerPrompt(tasks []queuedTask) string {
	messages := []Message{}
	requesters := []map[string]string{}
	positions := map[string]int{}
	for _, item := range tasks {
		message := item.task.Trigger
		messages = append(messages, message)
		requester := map[string]string{"from": message.From, "msgid": message.ID, "roomid": message.RoomID}
		if pos, ok := positions[message.From]; ok {
			requesters[pos] = requester
		} else {
			positions[message.From] = len(requesters)
			requesters = append(requesters, requester)
		}
	}
	raw, _ := json.Marshal(messages)
	people, _ := json.Marshal(requesters)
	return "trigger_messages:\n" + string(raw) + "\nrequesters:\n" + string(people)
}

func (d *dispatcher) saveTasks(tasks []queuedTask, status string, cause error) bool {
	for _, item := range tasks {
		item.task.Status = status
		item.task.Error = ""
		if cause != nil {
			item.task.Error = cause.Error()
		}
		if err := d.channel.rooms.tasks.save(item); err != nil {
			d.block(filepath.Dir(filepath.Dir(item.file)))
			return false
		}
	}
	d.channel.changed()
	return true
}

func (d *dispatcher) process(dir string) {
	d.mu.Lock()
	wake := d.active[dir]
	d.mu.Unlock()
	for d.ctx.Err() == nil && d.connected() {
		tasks := d.pending(dir)
		valid := []queuedTask{}
		for _, item := range tasks {
			if item.task.Trigger.ID == "" || item.task.Trigger.RoomID != item.task.Room {
				d.saveTasks([]queuedTask{item}, "failed", errors.New("旧格式任务缺少完整触发消息，请重新发送"))
			} else {
				valid = append(valid, item)
			}
		}
		tasks = valid
		if len(tasks) == 0 {
			return
		}
		if !d.saveTasks(tasks, "running", nil) {
			return
		}
		thread, err := d.thread(dir)
		if err != nil {
			if !d.saveTasks(tasks, "failed", err) {
				return
			}
			continue
		}
		ctx, cancel := context.WithTimeout(d.ctx, 30*time.Minute)
		done := make(chan error, 1)
		started := make(chan struct{})
		first := tasks[0].task
		go func() { done <- d.runTurn(ctx, thread, first, triggerPrompt(tasks), started) }()
		ready := false
		finished := false
		for !finished {
			select {
			case <-started:
				ready = true
				started = nil
				select {
				case wake <- struct{}{}:
				default:
				}
			case <-wake:
				if !ready {
					continue
				}
				next := d.pending(dir)
				if len(next) == 0 {
					continue
				}
				if !d.saveTasks(next, "running", nil) {
					cancel()
					<-done
					return
				}
				err = d.runner.Steer(ctx, thread, triggerPrompt(next))
				if err == nil {
					tasks = append(tasks, next...)
				} else if errors.Is(err, agent.ErrTurnEnded) {
					if !d.saveTasks(next, "pending", nil) {
						cancel()
						<-done
						return
					}
				} else {
					d.saveTasks(next, "unknown", err)
				}
			case err = <-done:
				status := "completed"
				if err != nil {
					status = "failed"
				}
				if d.ctx.Err() != nil || errors.Is(err, agent.ErrUnknown) {
					status = "unknown"
				}
				if !d.saveTasks(tasks, status, err) {
					cancel()
					return
				}
				finished = true
			}
		}
		cancel()
	}
}

func (d *dispatcher) block(dir string) {
	d.mu.Lock()
	d.blocked[dir] = true
	d.mu.Unlock()
	d.channel.mu.Lock()
	d.channel.state.Error = "Agent 任务状态保存失败，请修复存储后重新启用"
	d.channel.mu.Unlock()
	d.channel.changed()
}

func (d *dispatcher) thread(dir string) (string, error) {
	var state struct {
		ThreadID string `json:"threadId"`
	}
	file := filepath.Join(dir, "agent.json")
	raw, err := os.ReadFile(file)
	if err == nil {
		if err = json.Unmarshal(raw, &state); err != nil {
			return "", err
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	id, err := d.runner.Thread(d.ctx, d.settings.Binary, dir, state.ThreadID, agent.ThreadOptions{Instructions: agentInstructions, Tools: []any{agentSendTool()}})
	if err != nil {
		return "", err
	}
	if state.ThreadID == "" {
		state.ThreadID = id
		if err = writeJSON(file, state); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (d *dispatcher) runTurn(ctx context.Context, thread string, task Task, prompt string, started chan<- struct{}) error {
	sent := map[string]bool{}
	return d.runner.Turn(ctx, thread, task.Trigger.ID, prompt, func(ctx context.Context, args json.RawMessage) (any, error) {
		var input struct {
			Event Event `json:"event"`
		}
		if json.Unmarshal(args, &input) != nil || input.Event.Source != "aicat-agent://codex" || input.Event.Data.RoomID != task.Room {
			return nil, errors.New("发送必须使用当前 Room 和允许的来源")
		}
		receipt, err := d.channel.Send(ctx, input.Event)
		if err == nil && receipt.Status == "sent" {
			sent[input.Event.Data.Text.Content] = true
		}
		return receipt, err
	}, func(ctx context.Context, itemID, text string) error {
		if sent[text] {
			return nil
		}
		receipt, err := d.channel.Send(ctx, Event{SpecVersion: "1.0", ID: "codex-" + digest([]byte(itemID+"\x00"+text)), Source: "aicat-agent://codex", Type: SendRequested, ContentType: "application/json", Data: Message{Action: "send", RoomID: task.Room, Type: "text", Text: Text{Content: text}}})
		if err != nil {
			return err
		}
		if receipt.Status != "sent" {
			return fmt.Errorf("%w: 回复发送结果不明", agent.ErrUnknown)
		}
		return nil
	}, started, "messages_send")
}

func (d *dispatcher) close() {
	<-d.done
	d.runner.Close()
	d.wg.Wait()
}
