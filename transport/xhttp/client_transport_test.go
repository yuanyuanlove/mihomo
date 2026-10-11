package xhttp

import (
	"bytes"
	"context"
	"fmt"
	"github.com/metacubex/http"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/quic-go/http3"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestH2PingTimeoutConfiguration(t *testing.T) {
	for _, timeout := range []time.Duration{0, 15 * time.Second, 30 * time.Second} {
		tr := NewTransport(nil, nil, nil, []string{"h2"}, 0, timeout, false).(*http.Transport)
		if tr.HTTP2.PingTimeout != timeout {
			t.Fatalf("PingTimeout=%v, want %v", tr.HTTP2.PingTimeout, timeout)
		}
		if tr.HTTP2.SendPingTimeout != 45*time.Second {
			t.Fatalf("SendPingTimeout changed: %v", tr.HTTP2.SendPingTimeout)
		}
		if tr.HTTP2.CountError != nil {
			t.Fatal("diagnostics enabled by default")
		}
	}
	tr := NewTransport(nil, nil, nil, []string{"h2"}, -time.Second, 30*time.Second, true).(*http.Transport)
	if tr.HTTP2.SendPingTimeout != 0 || tr.HTTP2.CountError == nil {
		t.Fatal("keepalive disable / diagnostics configuration failed")
	}
}

func TestH2OptionsDoNotAffectH1OrH3(t *testing.T) {
	h1 := NewTransport(nil, nil, nil, []string{"http/1.1"}, 0, 30*time.Second, true).(*http.Transport)
	if h1.HTTP2 != nil || h1.ForceAttemptHTTP2 {
		t.Fatal("H1 unexpectedly enables H2")
	}
	h3 := NewTransport(nil, nil, nil, []string{"h3"}, 0, 30*time.Second, true).(*http3.Transport)
	if h3.QUICConfig.KeepAlivePeriod != QuicgoH3KeepAlivePeriod {
		t.Fatal("H3 keepalive changed")
	}
}

// Exercise the public RoundTripper against a local H2 peer. The durations are
// scaled down to keep regression tests fast; the 15/30-second mapping is checked
// separately above. No external endpoint, REALITY credential, or model is used.
func TestH2TransportFailureEvents(t *testing.T) {

	for _, tc := range []struct {
		name        string
		timeout     time.Duration
		diagnostics bool
		action      string
		wantCode    string
	}{
		{"short_wait_no_diagnostics", 60 * time.Millisecond, false, "ping", "conn_close_lost_ping"},
		{"long_wait_no_diagnostics", 400 * time.Millisecond, false, "ping", ""},
		{"short_wait_logs_lost_ping", 60 * time.Millisecond, true, "ping", "conn_close_lost_ping"},
		{"long_wait_survives", 400 * time.Millisecond, true, "ping", ""},
		{"peer_reset", 400 * time.Millisecond, true, "reset", "recv_rststream_cancel"},
		{"peer_goaway", 400 * time.Millisecond, true, "goaway", "recv_goaway_internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			peerDone := make(chan error, 1)
			peerExited := make(chan struct{})
			defer func() {
				listener.Close()
				select {
				case <-peerExited:
				case <-time.After(6 * time.Second):
					t.Error("peer goroutine did not exit")
				}
			}()
			sawPing := make(chan struct{}, 1)
			go func() {
				defer close(peerExited)
				conn, err := listener.Accept()
				if err != nil {
					peerDone <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				preface := make([]byte, len(http2.ClientPreface))
				if _, err = io.ReadFull(conn, preface); err != nil {
					peerDone <- err
					return
				}
				if string(preface) != http2.ClientPreface {
					peerDone <- fmt.Errorf("invalid preface")
					return
				}
				fr := http2.NewFramer(conn, conn)
				if err = fr.WriteSettings(); err != nil {
					peerDone <- err
					return
				}
				var stream uint32
				for {
					frame, err := fr.ReadFrame()
					if err != nil {
						peerDone <- err
						return
					}
					switch f := frame.(type) {
					case *http2.SettingsFrame:
						if !f.IsAck() {
							if err = fr.WriteSettingsAck(); err != nil {
								peerDone <- err
								return
							}
						}
					case *http2.HeadersFrame:
						stream = f.StreamID
						var block bytes.Buffer
						enc := hpack.NewEncoder(&block)
						enc.WriteField(hpack.HeaderField{Name: ":status", Value: "200"})
						if err = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: stream, BlockFragment: block.Bytes(), EndHeaders: true}); err != nil {
							peerDone <- err
							return
						}

						if tc.action == "reset" {
							peerDone <- fr.WriteRSTStream(stream, http2.ErrCodeCancel)
							return
						}
						if tc.action == "goaway" {
							peerDone <- fr.WriteGoAway(stream, http2.ErrCodeInternal, nil)
							return
						}
					case *http2.PingFrame:
						if f.IsAck() {
							continue
						}
						sawPing <- struct{}{}
						time.Sleep(180 * time.Millisecond)
						err = fr.WritePing(true, f.Data)
						if err == nil {
							err = fr.WriteData(stream, true, []byte("ok"))
						}
						peerDone <- err
						return
					}
				}
			}()
			tr := NewTransport(func(ctx context.Context) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
			},
				func(_ context.Context, c net.Conn, _ bool) (net.Conn, error) { return c, nil }, nil, []string{"h2"}, 40*time.Millisecond, tc.timeout, tc.diagnostics).(*http.Transport)
			defer tr.CloseIdleConnections()
			sub := log.Subscribe()
			defer log.UnSubscribe(sub)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", "https://offline.invalid/", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := tr.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if tc.wantCode != "" {
				if readErr == nil {
					t.Fatalf("expected transport failure, body=%q", data)
				}
				found := !tc.diagnostics
				for !found {
					select {
					case event := <-sub:
						found = strings.Contains(event.Payload, "code="+tc.wantCode)
					case <-ctx.Done():
						t.Fatal("missing H2 failure hook")
					}
				}
			} else if readErr != nil || string(data) != "ok" {
				t.Fatalf("long wait failed: data=%q err=%v", data, readErr)
			}
			if tc.action == "ping" {
				select {
				case <-sawPing:
				default:
					t.Fatal("peer saw no PING")
				}
			}
			select {
			case peerErr := <-peerDone:
				if tc.wantCode == "" && peerErr != nil {
					t.Fatal(peerErr)
				}
			case <-ctx.Done():
				t.Fatal("peer did not finish")
			}
		})
	}
}
