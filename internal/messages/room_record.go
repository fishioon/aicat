package messages

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/fishioon/aicat/internal/config"
)

func readLegacyRoomParts(home, id string, record *RoomRecord) error {
	dir := filepath.Join(home, "rooms", id)
	settingsFile := filepath.Join(dir, "settings.json")
	if raw, err := os.ReadFile(settingsFile); err == nil {
		var settings roomOverrides
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&settings); err != nil || settings.Version != roomSettingsVersion {
			return errors.New("Room 设置文件无效")
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			return errors.New("Room 设置文件无效")
		}
		record.Settings = &settings
	} else if !os.IsNotExist(err) {
		return err
	}

	runtimeFile := filepath.Join(dir, "agent.json")
	if raw, err := os.ReadFile(runtimeFile); err == nil {
		var runtime roomRuntimeState
		if json.Unmarshal(raw, &runtime) != nil {
			return errors.New("Room Agent 状态无效")
		}
		record.Runtime = &runtime
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *roomStore) writeRecord(record RoomRecord) error {
	record.Version = roomRecordVersion
	if record.ID == "" || record.Channel == "" || record.Source == "" || record.Conversation == "" || roomID(record.Channel, record.Source, record.Conversation) != record.ID {
		return errors.New("Room 记录无效")
	}
	if record.Settings != nil {
		record.Settings.Version = 0
		settings := s.defaults
		applyRoomOverrides(&settings, *record.Settings)
		if err := settings.Validate(); err != nil {
			return err
		}
	}
	return writeJSON(filepath.Join(s.home, "rooms", record.ID, "room.json"), record)
}

func (s *roomStore) updateRecord(id string, edit func(*RoomRecord) error) (RoomRecord, error) {
	roomCatalogMu.Lock()
	defer roomCatalogMu.Unlock()
	record, err := readRoomRecord(s.home, id)
	if err != nil {
		return RoomRecord{}, err
	}
	if err = edit(&record); err != nil {
		return RoomRecord{}, err
	}
	if err = s.writeRecord(record); err != nil {
		return RoomRecord{}, err
	}
	if err = removeLegacyRoomParts(s.home, id); err != nil {
		return RoomRecord{}, err
	}
	return record, nil
}

func removeLegacyRoomParts(home, id string) error {
	dir := filepath.Join(home, "rooms", id)
	removed := false
	for _, name := range []string{"settings.json", "agent.json"} {
		err := os.Remove(filepath.Join(dir, name))
		if err == nil {
			removed = true
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if removed {
		return syncDir(dir)
	}
	return nil
}

func InitializeRoomStorage(home string) error {
	entries, err := os.ReadDir(filepath.Join(home, "rooms"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		roomCatalogMu.Lock()
		record, readErr := readRoomRecord(home, id)
		if readErr != nil {
			roomCatalogMu.Unlock()
			if os.IsNotExist(readErr) {
				continue
			}
			return readErr
		}
		legacy := record.Version == 0 || fileExists(filepath.Join(home, "rooms", id, "settings.json")) || fileExists(filepath.Join(home, "rooms", id, "agent.json"))
		if legacy {
			record.Version = roomRecordVersion
			err = (&roomStore{home: home, defaults: config.DefaultRoomSettings()}).writeRecord(record)
		}
		roomCatalogMu.Unlock()
		if err != nil {
			return err
		}
		if legacy {
			if err = removeLegacyRoomParts(home, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
