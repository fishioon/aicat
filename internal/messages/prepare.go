package messages

import (
	"github.com/fishioon/aicat/internal/channelplugin"
)

func (c *Channel) BoundSource() (string, error) {
	credentials, err := c.credentials()
	if err != nil {
		return "", err
	}
	return c.source(credentials.AccountID), nil
}

func (c *Channel) CanSend() bool {
	if c.nativePackage() {
		manifest, err := channelplugin.New(c.home).Manifest(c.id)
		return err == nil && manifest.Capabilities.Send
	}
	return true
}

func (c *Channel) PrepareRooms() error {
	if !c.nativePackage() || !c.Installed() {
		return nil
	}
	manifest, err := channelplugin.New(c.home).Manifest(c.id)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.manifest = &manifest
	c.mu.Unlock()
	return c.rooms.applyTemplate(manifest.Room)
}
