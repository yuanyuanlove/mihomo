package outbound

import (
	"github.com/metacubex/mihomo/common/structure"
	"math"
	"strconv"
	"testing"
	"time"
)

func TestXHTTPPingTimeoutSettings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int
		want  time.Duration
		fail  bool
	}{
		{"default", 0, 0, false}, {"30 seconds", 30, 30 * time.Second, false},
		{"negative", -1, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rs := &XHTTPReuseSettings{HPingTimeout: tc.value}
			_, got, _, err := rs.transportSettings()
			if (err != nil) != tc.fail {
				t.Fatalf("err=%v", err)
			}
			if err == nil && got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
	if strconv.IntSize == 64 {
		overflow := int64(math.MaxInt64)/int64(time.Second) + 1
		_, _, _, err := (&XHTTPReuseSettings{HPingTimeout: int(overflow)}).transportSettings()
		if err == nil {
			t.Fatal("overflow accepted")
		}
	}
	var rs *XHTTPReuseSettings
	keep, ping, diagnostics, err := rs.transportSettings()
	if keep != 0 || ping != 0 || diagnostics || err != nil {
		t.Fatal("nil settings changed default")
	}
}

func TestXHTTPSettingsDecodeAndRejectInvalid(t *testing.T) {
	mapping := map[string]any{
		"name": "offline-test", "server": "127.0.0.1", "port": 443,
		"uuid": "00000000-0000-4000-8000-000000000001", "network": "xhttp",
		"xhttp-opts": map[string]any{"reuse-settings": map[string]any{"h-ping-timeout": 30, "h2-diagnostics": true}},
	}
	var option VlessOption
	decoder := structure.NewDecoder(structure.Option{TagName: "proxy", WeaklyTypedInput: true, KeyReplacer: structure.DefaultKeyReplacer})
	if err := decoder.Decode(mapping, &option); err != nil {
		t.Fatal(err)
	}
	if option.XHTTPOpts.ReuseSettings.HPingTimeout != 30 || !option.XHTTPOpts.ReuseSettings.H2Diagnostics {
		t.Fatal("YAML mapping did not reach options")
	}
	v, err := NewVless(option)
	if err != nil {
		t.Fatal(err)
	}
	v.Close()
	option.XHTTPOpts.ReuseSettings.HPingTimeout = -1
	if _, err := NewVless(option); err == nil {
		t.Fatal("negative timeout accepted")
	}
	option.XHTTPOpts.ReuseSettings.HPingTimeout = 0
	option.XHTTPOpts.DownloadSettings = &XHTTPDownloadSettings{ReuseSettings: &XHTTPReuseSettings{HPingTimeout: -1}}
	if _, err := NewVless(option); err == nil {
		t.Fatal("negative download timeout accepted")
	}
}
