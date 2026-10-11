package xhttp

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/metacubex/mihomo/log"
)

var diagnosticSequence atomic.Uint64

// CountError has transport scope, not connection scope. Do not associate a
// library error with the most recently dialed connection: retries can overlap.
type h2Diagnostics struct{ id uint64 }

func newH2Diagnostics() *h2Diagnostics { return &h2Diagnostics{id: diagnosticSequence.Add(1)} }

func (d *h2Diagnostics) countErrorHook() func(string) {
	if d == nil {
		return nil
	}
	return func(code string) {
		code = diagnosticH2Code(code)
		log.Debugln("[XHTTP H2] transport=%d event=h2_error code=%s scope=transport", d.id, code)
	}
}

// Keep only fixed library event names and protocol error-code tokens. Unknown
// values are not echoed, even if they happen to look syntactically safe.
func diagnosticH2Code(code string) string {
	// v0.1.8 emits uppercase HTTP/2 error tokens despite the hook docs.
	code = strings.ToLower(code)
	switch code {
	case "conn_close_lost_ping", "read_frame_eof", "read_frame_unexpected_eof", "read_frame_too_large", "read_frame_other":
		return code
	}
	for _, prefix := range []string{"recv_goaway_", "recv_rststream_", "read_frame_conn_error_"} {
		if suffix, ok := strings.CutPrefix(code, prefix); ok {
			switch suffix {
			case "no_error", "protocol_error", "internal_error", "flow_control_error", "settings_timeout", "stream_closed", "frame_size_error", "refused_stream", "cancel", "compression_error", "connect_error", "enhance_your_calm", "inadequate_security", "http_1_1_required":
				return code
			default:
				return prefix + "unknown"
			}
		}
	}
	return "unknown"
}

func (d *h2Diagnostics) wrapConn(conn net.Conn) net.Conn {
	c := &diagnosticConn{Conn: conn, transportID: d.id, id: diagnosticSequence.Add(1), created: time.Now()}
	c.report("connected", "none")
	return c
}

// This wrapper observes the I/O presented to HTTP/2 (after TLS/REALITY), not
// individual frames. EOF cannot identify the closing peer or middlebox.
// Successful reads/writes have no logging, clock calls, or counters.
type diagnosticConn struct {
	net.Conn
	transportID, id                uint64
	created                        time.Time
	localClosing                   atomic.Bool
	readOnce, writeOnce, closeOnce sync.Once
}

func (c *diagnosticConn) report(event, reason string) {
	log.Debugln("[XHTTP H2] transport=%d conn=%d age_ms=%d event=%s reason=%s local_close_started=%t", c.transportID, c.id, time.Since(c.created).Milliseconds(), event, reason, c.localClosing.Load())
}
func (c *diagnosticConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil {
		c.readOnce.Do(func() { c.report("read_error", diagnosticErrorClass(err)) })
	}
	return n, err
}
func (c *diagnosticConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err != nil {
		c.writeOnce.Do(func() { c.report("write_error", diagnosticErrorClass(err)) })
	}
	return n, err
}
func (c *diagnosticConn) Close() error {
	c.localClosing.Store(true)
	err := c.Conn.Close()
	c.closeOnce.Do(func() { c.report("local_close", diagnosticErrorClass(err)) })
	return err
}

// Never log err.Error(): a wrapped error can contain hostnames, URLs or data.
func diagnosticErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, syscall.EPIPE):
		return "broken_pipe"
	case errors.Is(err, net.ErrClosed):
		return "closed"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "other"
}
