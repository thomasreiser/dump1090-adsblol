package sbs

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pipeConn is a one-shot net.Conn whose Read drains a fixed string then returns
// io.EOF; close-on-cancel still works because we wrap with a real net.Pipe.
func pipeConnFrom(data string) net.Conn {
	server, client := net.Pipe()
	go func() {
		_, _ = io.Copy(server, strings.NewReader(data))
		_ = server.Close()
	}()
	return client
}

func TestClient_ReadsUntilEOFThenReconnects(t *testing.T) {
	lines := strings.Join([]string{
		"MSG,3,1,1,A1B2C3,1,2024/01/01,12:00:00.000,2024/01/01,12:00:00.000,,35000,,,40.0,-74.0,,,0,0,0,0",
		"MSG,1,1,1,A1B2C3,1,2024/01/01,12:00:01.000,2024/01/01,12:00:01.000,UAL999  ,,,,,,,,,,,",
	}, "\r\n") + "\r\n"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu       sync.Mutex
		seen     []Message
		dialN    atomic.Int32
	)

	c := &Client{
		Addr: "ignored",
		Dial: func(ctx context.Context) (net.Conn, error) {
			n := dialN.Add(1)
			if n > 2 {
				// Stop the test once we've completed two connect/read cycles.
				cancel()
				return nil, context.Canceled
			}
			return pipeConnFrom(lines), nil
		},
		OnMessage: func(m Message) {
			mu.Lock()
			seen = append(seen, m)
			mu.Unlock()
		},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinBackoff: time.Millisecond,
		MaxBackoff: 2 * time.Millisecond,
	}
	_ = c.Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 4 {
		t.Fatalf("got %d messages, want 4 (2 per connection × 2 connects)", len(seen))
	}
	if seen[0].HexIdent != "a1b2c3" || seen[1].Callsign == nil || *seen[1].Callsign != "UAL999" {
		t.Errorf("unexpected message contents: %+v %+v", seen[0], seen[1])
	}
}

func TestClient_BackoffOnDialError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var attempts atomic.Int32
	c := &Client{
		Addr: "ignored",
		Dial: func(ctx context.Context) (net.Conn, error) {
			attempts.Add(1)
			return nil, io.ErrUnexpectedEOF
		},
		OnMessage:  func(Message) {},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinBackoff: time.Millisecond,
		MaxBackoff: 5 * time.Millisecond,
	}
	_ = c.Run(ctx)
	if attempts.Load() < 2 {
		t.Errorf("attempts = %d, want at least 2 retries within window", attempts.Load())
	}
}
