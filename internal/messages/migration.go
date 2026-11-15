package messages

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/fishioon/aicat/internal/channelconfig"
	"github.com/fishioon/aicat/internal/channelplugin"
	"github.com/fishioon/aicat/internal/config"
)

func (s *Inbox) SetRoomDefaults(settings config.RoomSettings) {
	s.rooms.defaults = settings
}

func (c *Channel) PrepareRooms() error {
	if c.nativePackage() {
		if !c.Installed() {
			return nil
		}
		manifest, err := channelplugin.New(c.home).Manifest(c.id)
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.manifest = &manifest
		c.mu.Unlock()
		c.inbox.SetRoomDefaults(manifest.Room)
	}
	previous, err := channelconfig.Load(c.home, c.id)
	if err != nil {
		return err
	}
	if err := c.migrateNativeConfiguration(previous); err != nil {
		return err
	}
	account, err := c.credentials()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = c.inbox.MigrateRooms(c.source(account.AccountID), previous, c.CanSend()); err != nil {
		return err
	}
	values, err := config.ReadExtension(c.home, c.id)
	if err != nil {
		return err
	}
	if values["agent"] != nil || values["local"] != nil {
		return config.RemoveLegacyChannelSettings(c.home, c.id)
	}
	return nil
}

func (c *Channel) BoundSource() (string, error) {
	account, err := c.credentials()
	if err != nil {
		return "", err
	}
	return c.source(account.AccountID), nil
}

func (c *Channel) CanSend() bool {
	if c.nativePackage() {
		m, err := channelplugin.New(c.home).Manifest(c.id)
		return err == nil && m.Capabilities.Send
	}
	return true
}

func (s *Inbox) MigrateRooms(source string, previous channelconfig.Settings, canSend bool) error {
	if err := config.MigrateCodexBinary(s.rooms.home, previous.Agent.Binary); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(s.rooms.home, "rooms"))
	if os.IsNotExist(err) {
		entries, err = nil, nil
	}
	if err != nil {
		return err
	}
	if previous.Local != nil {
		for conversation, target := range previous.Local.ReplyRooms {
			settings := s.rooms.defaults
			settings.SendTarget = target
			settings.Automatic = previous.Agent.Enabled
			settings.AllowOutbound = previous.Agent.Enabled && canSend
			if trigger, exists := previous.Local.Triggers[conversation]; exists {
				settings.Trigger = config.RoomTrigger{All: trigger.All, Direct: trigger.Direct, Mention: trigger.Mention, MentionAll: trigger.MentionAll, Keywords: trigger.Keywords}
			}
			if err = config.EnsureRoomSettings(s.rooms.home, roomID(s.rooms.channel, source, conversation), settings); err != nil {
				return err
			}
			if err = s.rooms.register(source, conversation, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() || !config.ValidRoomID(entry.Name()) {
			continue
		}
		_, identityErr := readRoomIdentity(s.rooms.home, entry.Name())
		if identityErr == nil {
			continue
		}
		if identityErr != nil && !os.IsNotExist(identityErr) {
			return identityErr
		}
		file, err := os.Open(filepath.Join(s.rooms.home, "rooms", entry.Name(), "context", "messages.log"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			if err != nil {
				return err
			}
			continue
		}
		scan := bufio.NewScanner(file)
		scan.Buffer(make([]byte, 4096), 2<<20)
		var message Message
		for scan.Scan() {
			if json.Unmarshal(scan.Bytes(), &message) == nil && message.RoomID != "" {
				break
			}
		}
		err = errors.Join(scan.Err(), file.Close())
		if err != nil {
			return err
		}
		if message.RoomID == "" || roomID(s.rooms.channel, source, message.RoomID) != entry.Name() {
			continue
		}
		settings := s.rooms.defaults
		settings.Automatic = previous.Agent.Enabled
		settings.AllowOutbound = previous.Agent.Enabled && previous.Local == nil && canSend
		if previous.Local != nil {
			settings.SendTarget = previous.Local.ReplyRooms[message.RoomID]
			_, configured := previous.Local.ReplyRooms[message.RoomID]
			settings.Automatic = previous.Agent.Enabled && configured
			settings.AllowOutbound = settings.Automatic
			if trigger, exists := previous.Local.Triggers[message.RoomID]; exists {
				settings.Trigger = config.RoomTrigger{All: trigger.All, Direct: trigger.Direct, Mention: trigger.Mention, MentionAll: trigger.MentionAll, Keywords: trigger.Keywords}
			}
		}
		if err = config.EnsureRoomSettings(s.rooms.home, entry.Name(), settings); err != nil {
			return err
		}
		if err = s.rooms.register(source, message.RoomID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return nil
}
