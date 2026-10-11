package xhttp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/metacubex/mihomo/log"
)

func TestDiagnosticClassificationDoesNotEchoSecrets(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "none"}, {io.EOF, "eof"}, {io.ErrUnexpectedEOF, "unexpected_eof"},
		{fmt.Errorf("secret.example/Authorization: %w", syscall.ECONNRESET), "connection_reset"},
		{syscall.EPIPE, "broken_pipe"}, {net.ErrClosed, "closed"}, {context.Canceled, "canceled"},
		{context.DeadlineExceeded, "deadline"}, {errors.New("Authorization secret UUID url"), "other"},
	} {
		if got := diagnosticErrorClass(tc.err); got != tc.want {
			t.Fatalf("got=%s want=%s", got, tc.want)
		}
	}
	for _, tc := range []struct{ code, want string }{
		{"conn_close_lost_ping", "conn_close_lost_ping"}, {"recv_goaway_INTERNAL_ERROR", "recv_goaway_internal_error"},
		{"recv_rststream_CANCEL", "recv_rststream_cancel"}, {"read_frame_eof", "read_frame_eof"},
		{"read_frame_conn_error_protocol_error", "read_frame_conn_error_protocol_error"},
		{"secret_uuid_lowercase", "unknown"}, {"https://secret.example?token=secret", "unknown"},
		{"recv_goaway_secret", "recv_goaway_unknown"},
	} {
		if got := diagnosticH2Code(tc.code); got != tc.want {
			t.Fatalf("got=%s want=%s", got, tc.want)
		}
	}
}

func TestDiagnosticConnectionLogsBoundedAndPreservesIO(t *testing.T) {
	sub := log.Subscribe()
	defer log.UnSubscribe(sub)
	a, b := net.Pipe()
	defer b.Close()
	diag := newH2Diagnostics()
	c := diag.wrapConn(a)
	b.Close()
	for i := 0; i < 3; i++ {
		if _, err := c.Read(make([]byte, 1)); err != io.EOF {
			t.Fatalf("read err=%v", err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := c.Write([]byte("secret payload")); err == nil {
			t.Fatal("expected write error")
		}
	}
	c.Close()
	c.Close()
	diag.countErrorHook()("https://secret.example/Authorization")
	for _, event := range []string{"connected", "read_error", "write_error", "local_close", "h2_error"} {
		select {
		case e := <-sub:
			if !strings.Contains(e.Payload, "event="+event) {
				t.Fatalf("log=%s want event=%s", e.Payload, event)
			}
			for _, secret := range []string{"secret", "Authorization", "https://"} {
				if strings.Contains(e.Payload, secret) {
					t.Fatal("sensitive value logged")
				}
			}
		case <-time.After(time.Second):
			t.Fatal("missing diagnostic event")
		}
	}
	// A sentinel makes the no-duplicate assertion deterministic without sleeping.
	diag.countErrorHook()("read_frame_eof")
	select {
	case e := <-sub:
		if !strings.Contains(e.Payload, "code=read_frame_eof") {
			t.Fatalf("duplicate log: %s", e.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("missing sentinel")
	}
}
