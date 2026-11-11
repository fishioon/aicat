package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fishioon/aicat/internal/certificate"
	"github.com/fishioon/aicat/internal/config"
	"github.com/fishioon/aicat/internal/listenaddr"
	"github.com/fishioon/aicat/internal/systemproxy"
)

// Serializes startup, settings writes and quit recovery; filesystem/OS work stays off the UI thread.
type connection struct {
	mu               sync.Mutex
	proxy            *systemproxy.Manager
	home, addr       string
	authority        *certificate.Authority
	disconnect       func()
	desired, closing bool
	problem          string
}

func (c *connection) startup(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		c.problem = "网关已停止"
		return
	}
	if err := c.proxy.Restore(); err != nil {
		c.problem = err.Error()
		return
	}
	if c.desired {
		c.problem = errorText(c.enable(ctx))
	}
}

func (c *connection) enable(ctx context.Context) error {
	status, err := c.inspectLocked(ctx)
	if err != nil {
		return err
	}
	if !status.Trusted {
		return errors.New("请先安装并信任本机证书")
	}
	return c.proxy.Enable(listenaddr.ClientAddress(c.addr))
}

func (c *connection) set(ctx context.Context, enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return errors.New("应用正在退出")
	}
	if enabled {
		status, err := c.inspectLocked(ctx)
		if err != nil {
			return err
		}
		if !status.Trusted {
			return errors.New("请先安装并信任本机证书")
		}
	}
	if err := config.SetSystemProxyEnabled(c.home, enabled); err != nil {
		return err
	}
	c.desired = enabled
	var err error
	if enabled {
		err = c.proxy.Enable(listenaddr.ClientAddress(c.addr))
	} else {
		err = c.proxy.Restore()
	}
	c.problem = errorText(err)
	return err
}

func (c *connection) status() systemproxy.Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.proxy.Status()
	if c.problem != "" {
		s.Problem = c.problem
	}
	return s
}

func (c *connection) prepareQuit() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.proxy.Status().OwnedElsewhere {
		if err := c.proxy.Restore(); err != nil {
			c.problem = err.Error()
			return err
		}
	}
	c.closing = true
	return nil
}

func errorText(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

func (c *connection) inspect(ctx context.Context) (certificate.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inspectLocked(ctx)
}
func (c *connection) inspectLocked(ctx context.Context) (certificate.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.authority.Inspect(ctx)
}
func (c *connection) install(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return errors.New("应用正在退出")
	}
	s, err := c.inspectLocked(ctx)
	if err != nil {
		return err
	}
	if s.PendingDeletion {
		return errors.New("请先完成根证书删除，再重新安装")
	}
	if !s.Exists {
		if err = c.authority.Create(); err != nil {
			return err
		}
	}
	if s.Expired {
		return errors.New("本机证书已过期")
	}
	return certificate.Install(ctx, c.authority.PublicPath())
}
func (c *connection) deleteCertificate(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return errors.New("应用正在退出")
	}
	return c.authority.Delete(ctx, c.disconnect)
}
func (c *connection) exportCertificate(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.authority.Export(path)
}
