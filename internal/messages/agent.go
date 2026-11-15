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
	"github.com/fishioon/aicat/internal/config"
)

type Task struct {
	Settings             config.RoomSettings `json:"settings"`
	ConfigurationVersion string              `json:"configurationVersion"`
	ID                   string              `json:"id"`
	Account              string              `json:"account"`
	Source               string              `json:"source"`
	Room                 string              `json:"room"`
	Log                  string              `json:"log"`
	Status               string              `json:"status"`
	Error                string              `json:"error,omitempty"`
	CreatedAt            string              `json:"createdAt"`
	Trigger              Message             `json:"trigger"`
	TriggerReason        string              `json:"triggerReason,omitempty"`
}

type Dispatcher struct {
	ctx         context.Context
	rooms       *roomStore
	id          string
	notify      func()
	isConnected func() bool
	options     DispatcherOptions
	runner      *agent.Runner
	mu          sync.Mutex
	active      map[string]chan struct{}
	wg          sync.WaitGroup
	done        chan struct{}
	wake        chan struct{}
	source      string
}

func newDispatcher(ctx context.Context, c *Channel) *Dispatcher {
	credentials, _ := c.credentials()
	return dispatcherForRooms(ctx, c.rooms, DispatcherOptions{CanSend: c.CanSend, SelfID: c.selfID(), Source: c.source(credentials.AccountID), Connected: func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.account != nil && c.state.Phase == "connected" }, Changed: c.changed, Execute: MCPExecutor(MCPExecutorOptions{Channel: c.id, Send: c.mcp, Schedule: c.schedule})})
}

func (d *Dispatcher) Enqueue(source string, record Record) {
	message := record.Event.Data
	if message.Outgoing || message.Historical || (message.Schedule == nil && d.options.Accept != nil && !d.options.Accept(message)) || message.Type != "text" || strings.TrimSpace(message.Text.Content) == "" {
		return
	}
	var timestamp time.Time
	if message.Time != nil {
		timestamp = time.UnixMilli(*message.Time)
	} else {
		var err error
		timestamp, err = time.Parse(time.RFC3339Nano, record.Event.Time)
		if err != nil {
			return
		}
	}
	age := time.Since(timestamp)
	if age > 10*time.Minute || age < -time.Minute {
		return
	}
	settings, settingsErr := config.ReadRoomSettings(d.rooms.home, roomID(d.id, source, message.RoomID))
	if settingsErr == nil && (!settings.Automatic || roomTriggerReason(settings.Trigger, message, d.options.SelfID) == "") {
		return
	}
	dir := d.rooms.directory(source, message.RoomID)
	log := filepath.Join(dir, "context", "messages.log")
	task := Task{ID: record.ID, Account: d.id, Source: source, Room: record.Event.Data.RoomID, Log: log, Status: "pending", CreatedAt: record.RecordedAt, Trigger: record.Event.Data}
	task.Settings = taskSettings(settings, message)
	raw, _ := json.Marshal(task.Settings)
	task.ConfigurationVersion = digest(raw)
	task.TriggerReason = roomTriggerReason(settings.Trigger, message, d.options.SelfID)
	if settingsErr != nil {
		task.Status, task.Error = "failed", settingsErr.Error()
	}
	d.rooms.tasks.save(queuedTask{task: task, dir: dir})

	d.Signal()
	return
}

func (d *Dispatcher) Signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dispatcher) Run() {
	defer close(d.done)
	d.scan()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.wake:
			d.scan()
		}
	}
}

func (d *Dispatcher) connected() bool { return d.isConnected() }

func (d *Dispatcher) scan() {
	store := d.rooms.tasks
	if !d.connected() {
		return
	}
	for _, item := range store.selectTasks(d.source, "", "pending") {
		dir := item.dir
		d.mu.Lock()
		if d.active[dir] != nil {
			select {
			case d.active[dir] <- struct{}{}:
			default:
			}
		} else if d.ctx.Err() == nil {
			d.active[dir] = make(chan struct{}, 1)
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				defer func() { d.mu.Lock(); delete(d.active, dir); d.mu.Unlock(); d.Signal() }()
				d.process(dir)
			}()
		}
		d.mu.Unlock()
	}
}

func (d *Dispatcher) pending(dir string) []queuedTask {
	return d.rooms.tasks.selectTasks(d.source, dir, "pending")
}

func triggerPrompt(tasks []queuedTask) string {
	messages := []Message{}
	requesters := []map[string]string{}
	positions := map[string]int{}
	for _, item := range tasks {
		message := item.task.Trigger
		if item.task.Settings.Mode == "snapshot" && message.AgentSnapshot != nil {
			message = Message{ID: message.ID, From: message.From, RoomID: message.RoomID, ContextKey: message.ContextKey, Type: "text", Text: *message.AgentSnapshot}
		}
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
	context, _ := json.Marshal(tasks[0].task.Settings.Context)
	return "trigger_messages:\n" + string(raw) + "\nrequesters:\n" + string(people) + "\nroom_context:\n" + string(context)
}

func (d *Dispatcher) saveTasks(tasks []queuedTask, status string, cause error) {
	for _, item := range tasks {
		item.task.Status = status
		item.task.Error = ""
		if cause != nil {
			item.task.Error = cause.Error()
		}
		d.rooms.tasks.save(item)
		if cause != nil {
			slog.Error("message task failed", "channel", d.id, "task", item.task.ID, "status", status, "message", item.task.Trigger.ID, "localID", item.task.Trigger.LocalID, "room", item.task.Room, "reason", item.task.TriggerReason)
		} else {
			slog.Info("message task updated", "channel", d.id, "task", item.task.ID, "status", status, "message", item.task.Trigger.ID, "localID", item.task.Trigger.LocalID, "room", item.task.Room, "reason", item.task.TriggerReason)
		}
	}
	d.notify()
}

func (d *Dispatcher) process(dir string) {
	d.mu.Lock()
	wake := d.active[dir]
	d.mu.Unlock()
	for d.ctx.Err() == nil && d.connected() {
		tasks := d.pending(dir)
		settings, settingsErr := config.ReadRoomSettings(d.rooms.home, filepath.Base(dir))
		valid := []queuedTask{}
		for _, item := range tasks {
			if settingsErr != nil {
				d.saveTasks([]queuedTask{item}, "failed", settingsErr)
			} else if !settings.Automatic || roomTriggerReason(settings.Trigger, item.task.Trigger, d.options.SelfID) == "" || (item.task.Trigger.Schedule == nil && d.options.Accept != nil && !d.options.Accept(item.task.Trigger)) {
				d.saveTasks([]queuedTask{item}, "skipped", nil)
			} else if item.task.Trigger.ID == "" || item.task.Trigger.RoomID != item.task.Room {
				d.saveTasks([]queuedTask{item}, "failed", errors.New("旧格式任务缺少完整触发消息，请重新发送"))
			} else {
				item.task.Settings = taskSettings(settings, item.task.Trigger)
				raw, _ := json.Marshal(item.task.Settings)
				item.task.ConfigurationVersion = digest(raw)
				valid = append(valid, item)
			}
		}
		tasks = valid
		if len(tasks) == 0 {
			return
		}
		snapshot := tasks[0].task.Settings.Mode == "snapshot"
		end := 1
		for end < len(tasks) && tasks[end].task.ConfigurationVersion == tasks[0].task.ConfigurationVersion {
			end++
		}
		tasks = tasks[:end]
		if snapshot && len(tasks) > 1 {
			d.saveTasks(tasks[:len(tasks)-1], "superseded", nil)
			tasks = tasks[len(tasks)-1:]
		}
		d.saveTasks(tasks, "running", nil)
		thread, err := d.thread(dir, tasks[0].task)
		if err != nil {
			d.saveTasks(tasks, "failed", err)
			continue
		}
		ctx, cancel := context.WithTimeout(d.ctx, 30*time.Minute)
		done := make(chan error, 1)
		started := make(chan struct{})
		first := tasks[0].task
		go func() { done <- d.options.Execute(ctx, d.runner, thread, first, triggerPrompt(tasks), started) }()
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
				if snapshot {
					continue
				}
				end := 0
				for end < len(next) && next[end].task.ConfigurationVersion == first.ConfigurationVersion {
					end++
				}
				next = next[:end]
				if len(next) == 0 {
					continue
				}
				d.saveTasks(next, "running", nil)
				err = d.runner.Steer(ctx, thread, triggerPrompt(next))
				if err == nil {
					tasks = append(tasks, next...)
				} else if errors.Is(err, agent.ErrTurnEnded) {
					d.saveTasks(next, "pending", nil)
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
				d.saveTasks(tasks, status, err)
				finished = true
			}
		}
		cancel()
		if snapshot {
			if err := d.runner.Unsubscribe(d.ctx, thread); err != nil {
				slog.Warn("snapshot thread unsubscribe failed", "channel", d.id, "room", first.Room, "error", err)
			}
		}
	}
}

func (d *Dispatcher) thread(dir string, task Task) (string, error) {
	canSend := task.Settings.AllowOutbound && (d.options.CanSend == nil || d.options.CanSend())
	instructions := agentInstructions + "\nRoom prompt:\n" + task.Settings.Prompt
	cfg, err := config.Load(d.rooms.home)
	if err != nil {
		return "", err
	}
	binary := cfg.MessageExecution.CodexBinary
	if task.Settings.Mode == "snapshot" {
		return d.runner.Thread(d.ctx, binary, dir, "", agent.ThreadOptions{Instructions: instructions + "\n" + snapshotInstructions, Tools: agentTools(canSend), Ephemeral: true, Model: task.Settings.Model})
	}
	var state struct {
		ThreadID         string `json:"threadId"`
		ToolsHash        string `json:"toolsHash"`
		InstructionsHash string `json:"instructionsHash,omitempty"`
		Model            string `json:"model,omitempty"`
		AllowOutbound    bool   `json:"allowOutbound"`
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
	tools, _ := json.Marshal(agentTools(canSend))
	toolsHash := digest(tools)
	if state.ThreadID != "" && (state.Model != task.Settings.Model || state.AllowOutbound != canSend || state.ToolsHash != toolsHash) {
		if err := d.runner.Unsubscribe(d.ctx, state.ThreadID); err != nil {
			return "", err
		}
		state.ThreadID = ""
	}
	id, err := d.runner.Thread(d.ctx, binary, dir, state.ThreadID, agent.ThreadOptions{Instructions: instructions, Tools: agentTools(canSend), Model: task.Settings.Model})
	if err != nil {
		return "", err
	}
	instructionsHash := digest([]byte(instructions))
	update := state.ThreadID != "" && state.InstructionsHash != instructionsHash
	if update {
		if err = d.runner.UpdateInstructions(d.ctx, id, instructions); err != nil {
			return "", err
		}
	}
	if state.ThreadID == "" || update {
		state.ThreadID = id
		state.ToolsHash = toolsHash
		state.InstructionsHash = instructionsHash
		state.Model = task.Settings.Model
		state.AllowOutbound = canSend
		if err = writeJSON(file, state); err != nil {
			return "", err
		}
	}
	return id, nil
}

func (d *Dispatcher) Close() {
	<-d.done
	d.runner.Close()
	d.wg.Wait()
}

type DispatcherOptions struct {
	CanSend   func() bool
	SelfID    string
	Source    string
	Connected func() bool
	Changed   func()
	Accept    func(Message) bool
	Execute   func(context.Context, *agent.Runner, string, Task, string, chan<- struct{}) error
}

func (s *Inbox) Dispatcher(ctx context.Context, options DispatcherOptions) *Dispatcher {
	return dispatcherForRooms(ctx, s.rooms, options)
}
func dispatcherForRooms(ctx context.Context, rooms *roomStore, options DispatcherOptions) *Dispatcher {
	rooms.tasks.reset()
	return &Dispatcher{ctx: ctx, rooms: rooms, id: rooms.channel, runner: agent.New(), source: options.Source, notify: options.Changed, isConnected: options.Connected, options: options, active: map[string]chan struct{}{}, done: make(chan struct{}), wake: make(chan struct{}, 1)}
}
func (s *Inbox) Tasks(source string) []Task { return s.rooms.tasks.list(source) }

func taskSettings(settings config.RoomSettings, message Message) config.RoomSettings {
	if message.Schedule != nil {
		settings.Mode = "conversation"
	}
	return settings
}
