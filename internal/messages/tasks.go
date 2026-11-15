package messages

import (
	"sort"
	"sync"
)

type queuedTask struct {
	task Task
	dir  string
}

type taskStore struct {
	mu     sync.RWMutex
	active map[string]queuedTask
	recent map[string][]Task
}

func newTaskStore() *taskStore {
	return &taskStore{active: map[string]queuedTask{}, recent: map[string][]Task{}}
}

func (s *taskStore) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = map[string]queuedTask{}
	s.recent = map[string][]Task{}
}

func (s *taskStore) remember(item queuedTask) {
	task := item.task
	if task.Status == "pending" || task.Status == "running" {
		s.active[item.task.ID] = item
	} else {
		delete(s.active, item.task.ID)
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

func (s *taskStore) save(item queuedTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.remember(item)
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
		if item.task.Source == source && item.task.Status == status && (dir == "" || item.dir == dir) {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].task.CreatedAt < result[j].task.CreatedAt || (result[i].task.CreatedAt == result[j].task.CreatedAt && result[i].task.ID < result[j].task.ID)
	})
	return result
}
