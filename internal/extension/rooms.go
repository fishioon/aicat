package extension

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/messages"
)

type roomReceiver interface {
	Receive(context.Context, messages.Event) (messages.Record, error)
}
type roomReader interface {
	ReadRoom(context.Context, string) (map[string]any, error)
}

func (h *Host) ListRooms(ctx context.Context) ([]messages.RoomSummary, error) {
	rooms, err := messages.ListRooms(ctx, h.home)
	if err != nil {
		return nil, err
	}
	tasks := map[string][]messages.Task{}
	for i := range rooms {
		channel := rooms[i].Channel
		if _, loaded := tasks[channel]; !loaded {
			tasks[channel] = nil
			if it := h.find(channel); it != nil && it.parts.Messages != nil {
				tasks[channel] = it.parts.Messages.Snapshot().Tasks
			}
		}
		rooms[i].AddTaskIssues(tasks[channel])
	}
	return rooms, nil
}

func (h *Host) ReadRoomByID(ctx context.Context, id string) (messages.RoomView, error) {
	view, err := messages.ReadRoom(ctx, h.home, id)
	if err != nil {
		return messages.RoomView{}, err
	}
	if it := h.find(view.Channel); it != nil && it.parts.Messages != nil {
		for _, task := range it.parts.Messages.Snapshot().Tasks {
			if task.Source == view.Source && task.Room == view.Conversation {
				view.Tasks = append(view.Tasks, task)
			}
		}
	}
	view.AddTaskIssues(view.Tasks)
	return view, nil
}

func (h *Host) ReceiveMessage(ctx context.Context, id string, event messages.Event) (messages.Record, error) {
	it := h.find(id)
	if it == nil || !it.on.Load() {
		return messages.Record{}, errors.New("消息扩展未启用")
	}
	receiver, ok := it.parts.Messages.(roomReceiver)
	if !ok {
		return messages.Record{}, errors.New("扩展没有入站能力")
	}
	return receiver.Receive(ctx, event)
}
func (h *Host) ReadRoom(ctx context.Context, id, room string) (map[string]any, error) {
	it := h.find(id)
	if it == nil {
		return nil, errors.New("没有这个扩展")
	}
	reader, ok := it.parts.Messages.(roomReader)
	if !ok {
		return nil, errors.New("扩展没有 Room 读取能力")
	}
	return reader.ReadRoom(ctx, room)
}
func (h *Host) mountRooms(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/rooms", func(w http.ResponseWriter, r *http.Request) {
		data, err := h.ListRooms(r.Context())
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	})
	mux.HandleFunc("GET /api/rooms/{id}", func(w http.ResponseWriter, r *http.Request) {
		data, err := h.ReadRoomByID(r.Context(), r.PathValue("id"))
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	})
	mux.HandleFunc("PUT /api/rooms/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var settings config.RoomSettings
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
		decoder.DisallowUnknownFields()
		var extra any
		if decoder.Decode(&settings) != nil || decoder.Decode(&extra) != io.EOF {
			http.Error(w, "无效 Room 配置", http.StatusBadRequest)
			return
		}
		if err := messages.UpdateRoomSettings(r.Context(), h.home, r.PathValue("id"), settings); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if h.changed != nil {
			h.changed()
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/messages/receive/{channel}", func(w http.ResponseWriter, r *http.Request) {
		var event messages.Event
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&event) != nil {
			http.Error(w, "无效入站消息", 400)
			return
		}
		record, err := h.ReceiveMessage(r.Context(), r.PathValue("channel"), event)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		_ = json.NewEncoder(w).Encode(record)
	})
	mux.HandleFunc("GET /api/rooms/{channel}/{room}", func(w http.ResponseWriter, r *http.Request) {
		data, err := h.ReadRoom(r.Context(), r.PathValue("channel"), r.PathValue("room"))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		_ = json.NewEncoder(w).Encode(data)
	})
}
