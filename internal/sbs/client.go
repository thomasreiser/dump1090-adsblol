package sbs

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"time"
)

// Client reads SBS lines from a dump1090 BaseStation TCP endpoint and pushes
// parsed Messages to a sink. It reconnects with exponential backoff.
type Client struct {
	Addr       string        // host:port
	Dial       func(ctx context.Context) (net.Conn, error) // overridable for tests
	OnMessage  func(Message)
	Logger     *slog.Logger
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

// Run blocks until ctx is cancelled.
func (c *Client) Run(ctx context.Context) error {
	if c.OnMessage == nil {
		return errors.New("sbs: client has no OnMessage callback")
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.MinBackoff <= 0 {
		c.MinBackoff = 500 * time.Millisecond
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 30 * time.Second
	}
	dial := c.Dial
	if dial == nil {
		dial = func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", c.Addr)
		}
	}

	backoff := c.MinBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := dial(ctx)
		if err != nil {
			c.Logger.Warn("sbs: dial failed", "addr", c.Addr, "err", err, "retry_in", backoff)
			if !sleep(ctx, backoff) {
				return ctx.Err()
			}
			backoff = nextBackoff(backoff, c.MaxBackoff)
			continue
		}
		c.Logger.Info("sbs: connected", "addr", c.Addr)
		backoff = c.MinBackoff
		c.read(ctx, conn)
		_ = conn.Close()
		if !sleep(ctx, c.MinBackoff) {
			return ctx.Err()
		}
	}
}

func (c *Client) read(ctx context.Context, conn net.Conn) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	// dump1090 uses \r\n line endings; bufio's default ScanLines handles both.
	for sc.Scan() {
		m, err := Parse(sc.Text())
		if err != nil {
			if !errors.Is(err, ErrNotMSG) {
				c.Logger.Debug("sbs: parse error", "err", err, "line", sc.Text())
			}
			continue
		}
		c.OnMessage(*m)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) && ctx.Err() == nil {
		c.Logger.Warn("sbs: read error", "err", err)
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		return max
	}
	return next
}
