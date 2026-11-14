package messages

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type queuedTask struct {
	task Task
	file string
}

type taskStore struct {
	mu     sync.RWMutex
	active map[string]queuedTask
	recent map[string][]Task
}

func newTaskStore(home, channel string) *taskStore {
	s := &taskStore{active: map[string]queuedTask{}, recent: map[string][]Task{}}
	files, _ := filepath.Glob(filepath.Join(home, "rooms", "*", "tasks", "*.json"))
	for _, file := range files {
		var task Task
		raw, err := os.ReadFile(file)
		if err != nil || json.Unmarshal(raw, &task) != nil || task.Account != channel {
			continue
		}
		dir := filepath.Dir(filepath.Dir(file))
		if task.Log != filepath.Join(dir, "context", "messages.log") || filepath.Base(dir) != digest([]byte(channel+"\x00"+task.Source+"\x00"+task.Room)) {
			continue
		}
		s.remember(queuedTask{task: task, file: file})
	}
	return s
}

func (s *taskStore) remember(item queuedTask) {
	task := item.task
	if task.Status == "pending" || task.Status == "running" {
		s.active[item.file] = item
	} else {
		delete(s.active, item.file)
	}
	recent := s.recent[task.Source]
	for i, t := range recent {
		if t.ID == task.ID {
			recent = append(recent[:i], recent[i+1:]...)
			break
		}
	}
	recent = append(recent, task)
	sort.Slice(recent, func(i, j int) bool {
		if recent[i].CreatedAt == recent[j].CreatedAt {
			return recent[i].ID < recent[j].ID
		}
		return recent[i].CreatedAt < recent[j].CreatedAt
	})
	if len(recent) > 50 {
		recent = recent[len(recent)-50:]
	}
	s.recent[task.Source] = recent
}

func (s *taskStore) save(item queuedTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeJSON(item.file, item.task); err != nil {
		return err
	}
	s.remember(item)
	return nil
}

func (s *taskStore) list(source string) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Task{}, s.recent[source]...)
}

func (s *taskStore) selectTasks(source, dir, status string) []queuedTask {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []queuedTask{}
	for _, item := range s.active {
		if item.task.Source == source && item.task.Status == status && (dir == "" || filepath.Dir(filepath.Dir(item.file)) == dir) {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].file < result[j].file })
	return result
}

func (s *taskStore) create(item queuedTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(item.file)
	if err == nil {
		var saved Task
		if json.Unmarshal(raw, &saved) != nil || saved.ID != item.task.ID || saved.Source != item.task.Source || saved.Account != item.task.Account || saved.Room != item.task.Room || saved.Log != item.task.Log || jsonDigest(saved.Trigger) != jsonDigest(item.task.Trigger) {
			return errors.New("Agent 任务标识或触发内容冲突")
		}
		s.remember(queuedTask{task: saved, file: item.file})
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	if err = writeJSON(item.file, item.task); err != nil {
		return err
	}
	s.remember(item)
	return nil
}
