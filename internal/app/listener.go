package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/gateway"
	"github.com/fishioon/aicat/internal/listenaddr"
)

// Reconfiguration shares the OS/settings/quit lock. Traffic storage, the CA,
// rewrite runtime and subscriptions keep their identities across the switch.
func (c *connection) rebind(ctx context.Context, g *gateway.Gateway, address string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return errors.New("应用正在退出")
	}
	status := c.proxy.Status()
	if status.Recovery && (!status.Enabled || status.Problem != "") {
		return errors.New("请先恢复系统代理原设置")
	}
	oldClient, nextClient := listenaddr.ClientAddress(c.addr), listenaddr.ClientAddress(address)
	rollbackProxy := func() error {
		if err := c.proxy.Restore(); err != nil {
			return err
		}
		return c.proxy.Enable(oldClient)
	}
	err := g.Rebind(ctx, address, func() error {
		switched := status.Enabled && oldClient != nextClient
		if switched {
			if err := c.proxy.Restore(); err != nil {
				return err
			}
			if err := c.proxy.Enable(nextClient); err != nil {
				if rollback := rollbackProxy(); rollback != nil {
					return errors.Join(err, rollback, gateway.ErrKeepListener)
				}
				return err
			}
		}
		if err := config.SetGatewayAddress(c.home, address); err != nil {
			if switched {
				if rollback := rollbackProxy(); rollback != nil {
					return errors.Join(err, rollback, gateway.ErrKeepListener)
				}
			}
			return fmt.Errorf("保存监听设置失败: %w", err)
		}
		return nil
	})
	c.addr = g.Status().Addr
	if err == nil {
		c.problem = ""
	} else if errors.Is(err, gateway.ErrKeepListener) {
		c.problem = err.Error()
	}
	return err
}
