package messages

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/fishioon/aicat/internal/channelplugin"
)

func (c *Channel) GatewayCapabilities() channelplugin.Capabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.manifest == nil {
		return channelplugin.Capabilities{}
	}
	return c.manifest.Capabilities
}

func (c *Channel) Gateway(ctx context.Context, event any) (json.RawMessage, error) {
	c.mu.Lock()
	account := c.account
	c.mu.Unlock()
	if account == nil {
		return nil, errors.New("渠道宿主尚未就绪")
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return account.Observe(callCtx, event)
}

func (c *Channel) Observe(event any) {
	select {
	case c.observations <- event:
	default:
		c.mu.Lock()
		c.state.Dropped++
		c.mu.Unlock()
	}
}

func (c *Channel) observeLoop(ctx context.Context) {
	for {
		select {
		case event := <-c.observations:
			if _, err := c.Gateway(ctx, event); err != nil {
				c.mu.Lock()
				c.state.Dropped++
				c.mu.Unlock()
			}
		case <-ctx.Done():
			return
		}
	}
}
