# Mihomo XHTTP cloud checks — no deployment

This experimental branch intentionally starts from the fixed Go proxy-core commit
`9f053c49075de076d83d3e5918241e410f7e9adf`, not this fork's Python `main`.
Do not merge `main`, update dependencies, commit, push, open a PR, publish a release,
install the core, or change any real proxy profile while performing these checks.

The three inherited GitHub Actions workflows are intentionally omitted from this
cloud-only experiment branch. Checks run in the selected Codex cloud environment;
there is no inherited release/update pipeline on this branch.

## Experiment

- Optional XHTTP/H2 `h-ping-timeout`: integer seconds; omitted/0 keeps the 15-second default; positive values override it. Negative/overflowing values are rejected.
- Optional `h2-diagnostics`: false by default; sanitized, bounded-per-connection diagnostic events.
- Existing `h-keep-alive-period` / 45-second default sending interval is unchanged.
- H1/H3 behavior and existing connection-count/reuse settings are not changed.
- No real credentials, node profiles, runtime logs or captures are included. Public upstream test fixtures are retained.

## Verified before publishing this exact source

Local macOS ARM64: Go 1.20.14 targeted normal/race tests passed; six local H2
scenarios passed 20 repetitions. Go 1.27.2 targeted normal tests, vet and final
macOS ARM64 `with_gvisor` build passed. The full two-package Go 1.27.2 race check
did not finish because the local disk filled during dependency compilation.
Do not report this incomplete check as passed.

## Cloud environment and pending checks

Select `yuanyuanlove/mihomo` and checkout branch
`feature-20261011-xhttp-cloud-checks` before runtime/dependency detection.
Use Go **1.27.2**, a native C compiler for race, and sufficient disk/RAM.
Record the actual OS/architecture, toolchain/compiler versions and disk space.
Limit build parallelism to 2. Package-manager access is needed for public modules;
no private network access or proxy secrets are required.

Run from the repository root, preserving exit codes and full logs:

```sh
export GOMAXPROCS=2
export GOTOOLCHAIN=local
go version
go env GOVERSION GOOS GOARCH CGO_ENABLED CC
go test -p 2 -tags with_gvisor ./transport/xhttp ./adapter/outbound -count=1
go vet -p 2 -tags with_gvisor ./transport/xhttp ./adapter/outbound
CGO_ENABLED=1 go test -p 2 -race -tags with_gvisor ./transport/xhttp ./adapter/outbound -count=1
```

Optional cloud cross-build (do not execute a macOS binary on Linux):

```sh
mkdir -p outputs
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -p 2 -tags with_gvisor -trimpath \
  -ldflags '-X github.com/metacubex/mihomo/constant.Version=alpha-9f053c4-xhttp-exp-20261011-cloudcheck -X github.com/metacubex/mihomo/constant.BuildTime=2026-10-11 -w -s -buildid=' \
  -o outputs/mihomo-darwin-arm64-cloudcheck .
file outputs/mihomo-darwin-arm64-cloudcheck
go version -m outputs/mihomo-darwin-arm64-cloudcheck
sha256sum outputs/mihomo-darwin-arm64-cloudcheck
```

Tests use synthetic inputs/local loopback only. No real node connections, model
probes, history replay or packet capture. If version/network/resources block a
check, record that limitation rather than silently changing the toolchain or tests.
If tests expose a source bug, retain the failing case and report it before expanding
the task into behavior changes.

## Expected source file hashes

| File | SHA256 |
| --- | --- |
| `adapter/outbound/vless.go` | `38563ff65815a3fc1f31e89d281d4c0598507bf2157fe85a5bb67c5f718cad68` |
| `adapter/outbound/vless_xhttp_test.go` | `bf464bb3622c643adb6bb4ee57396e35a01834df451d96d64bc4cec2e486706e` |
| `transport/xhttp/client.go` | `7fae6974e21db4f3a82ab4bc017261ea87d95622e22901d076417c03055c3a35` |
| `transport/xhttp/client_transport_test.go` | `816b7ad094182d295cff80cd7d4932045f1b8d387c3925acc6bb64eec557597c` |
| `transport/xhttp/diagnostics.go` | `da45eed485d8badea114cf11c16a944eed6b7577f02f73da44fa9b848467f49e` |
| `transport/xhttp/diagnostics_test.go` | `39c5a456d1d07d3395b50bd70fa4654374d4c8fddbbdc2bb5642e9195eb725cc` |

Original input patch SHA256: `4356f4ce024570c6ff5d18c39b4e701702121b26bfd2e83498ba2efeb8d8bdeb`.

## Delivery boundary

Report each command, environment, exit code and result separately. Linux-native
race success is not macOS-native race success. Cross-compilation is not a real
REALITY handshake or evidence that intermittent WebSocket/502 failures are fixed.
Do not deploy. Save test logs and candidate hashes, then stop.
