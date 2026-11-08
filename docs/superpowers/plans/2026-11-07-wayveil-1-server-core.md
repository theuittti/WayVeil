# Wayveil — Plan 1 of 3: Server Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go library + developer CLI that installs, manages and fully removes a VLESS + REALITY + Vision server over SSH, checks and suggests REALITY targets, and produces client links — everything the GUI (Plan 3) will call for the "owner" role.

**Architecture:** Pure-Go core in `internal/`. Server work is done by bash scripts embedded in the binary and piped to `bash -s` over SSH (never left on the server, except the uninstaller copy). A small static Go scanner (`wv-scan`) is uploaded temporarily for target checks/scans. A `Runner` interface separates SSH from logic so logic is unit-tested with fakes; real behaviour is integration-tested against a systemd Ubuntu container over SSH.

**Tech Stack:** Go (latest stable, ≥1.24), `golang.org/x/crypto/ssh`, stdlib `flag`/`net/http`/`crypto/tls`, bash + jq on the server, xray-core (pinned), Docker for integration tests.

**Spec:** `docs/superpowers/specs/2026-11-07-wayveil-design.md` (sections 4.2–4.4, 4.8, 7). Reference installer (MIT): https://github.com/ndatg/xray-vless-reality-installer — `xray-install.sh`.

**Roadmap:** Plan 1 (this) — server core. Plan 2 — client engine: link import, sing-box config generation, rule-list updater, privileged helper (Windows service / Linux caps). Plan 3 — Wails GUI, i18n, tray, settings store with DPAPI/keyring, packaging & CI. Plans 2 and 3 are written after Plan 1 lands.

## Global Constraints

- Module path: `github.com/theuittti/WayVeil` (rename later with one `go mod edit` if the GitHub owner differs).
- Supported server OS: Ubuntu 22.04 / 24.04 / 26.04, Debian 12 / 13 (updated 2026-11-08; Debian 11 is EOL). Anything else → error code `unsupported_os`.
- Inbound: VLESS, port 443, TCP only, `flow: "xtls-rprx-vision"`, `security: reality`. No XHTTP.
- Client link: `vless://UUID@host:443?security=reality&sni=…&fp=chrome&pbk=…&sid=…&flow=xtls-rprx-vision&type=tcp#Name`.
- The SSH password is never written to disk or logs. After first login the app authorises its own ed25519 key and uses only that.
- SSH user: `root`, or a user with passwordless sudo. Sudo that asks for a password → error code `sudo_password_required`.
- Every config change: write temp → `xray run -test` → atomic `mv` → `systemctl restart xray`. On failure the old config stays.
- Target requirements (same as reference `check_sni_target`): TLS 1.3, HTTP/2, no redirect to another host.
- Scan: own /24 first, then the rest of the enclosing /22; per-address timeout 1500 ms; concurrency 128; stop after 5 verified candidates or 30 s; scanner deleted afterwards.
- Access log kept ≤ 7 days (logrotate daily, rotate 7).
- Server files: `/usr/local/bin/xray`, `/etc/xray/config.json`, `/etc/xray/wayveil.json`, `/etc/xray/wayveil-manifest`, `/etc/xray/wayveil-traffic.json`, `/etc/xray/wayveil-uninstall.sh`, `/etc/systemd/system/xray.service`, `/etc/sysctl.d/99-xray-bbr.conf`, `/etc/logrotate.d/xray`, `/var/log/xray/`, system user `xray`.
- Manifest is a line file (not JSON — easier and safer from bash): spec's `wayveil-manifest.json` becomes `/etc/xray/wayveil-manifest`. *(Planner's call.)*
- Keep the reference installer's MIT copyright in `THIRD_PARTY_LICENSES`.
- Remote script output protocol: progress lines `##STEP <id> <start|ok|warn|fail> [message]`; final line `##RESULT <json>`; errors `##ERROR <code> <message>`. ctl commands print exactly one JSON line.

## Review Focus

1. **Non-root sudo user** — `deploy` with NOPASSWD sudo must install and manage exactly like root; a user whose sudo needs a password gets `sudo_password_required` before anything is changed. (Task 4)
2. **sysctl not permitted (containers, OpenVZ)** — BBR step must end `warn`, not `fail`; install succeeds. (Task 6)
3. **Interrupted / repeated runs** — uninstall run twice succeeds both times; install on a server that already has `wayveil.json` returns `already_installed` instead of clobbering keys. (Tasks 6, 8)
4. **Client names with Cyrillic, spaces, quotes** (`Вася ноутбук`, `O'Brien "x"`) — stored and listed byte-for-byte; link fragment percent-encoded. (Tasks 1, 7)
5. **Port 443 already taken** (nginx etc.) — precheck reports `port443Busy: true` and install refuses with `port_busy` before touching anything. (Tasks 5, 6)

---

## File Structure

```
go.mod / go.sum
Makefile                              build scanner assets, test, it
.gitignore                            .superpowers/, build/, internal/server/assets/wv-scan-*
LICENSE (MIT), THIRD_PARTY_LICENSES
internal/link/vless.go                Params, Build, Parse
internal/targetcheck/check.go         Report, Problem codes, Check
internal/targetscan/scan.go           Candidate, Options, Scan, ScanOrder
cmd/wv-scan/main.go                   `wv-scan check|scan` — JSON on stdout
internal/remote/remote.go             Runner interface, ExitError, WithEnv, ShellQuote
internal/sshx/client.go               SSH Runner implementation (+sudo), Connect, host key TOFU
internal/sshx/keys.go                 GenerateKey
internal/server/scripts/{precheck,install,ctl,uninstall}.sh   embedded
internal/server/assets/               wv-scan-linux-{amd64,arm64} (built by make, gitignored) + .keep
internal/server/protocol.go           ParseLine → StepEvent / result / error
internal/server/precheck.go           HostInfo, Precheck
internal/server/install.go            InstallOptions, InstallResult, Install
internal/server/clients.go            Client, ServerInfo, Traffic, Seen, ctl wrappers, ClientLink
internal/server/targets.go            Presets, CheckTarget, SuggestTargets, scanner upload/cleanup
internal/server/uninstall.go          Uninstall
rules/targets.json                    preset targets by country + Russian ones (embedded via rules/rules.go)
internal/ownerstore/store.go          ServerProfile, Save, Load (0600 file; encryption in Plan 3)
cmd/wayveil-dev/main.go               developer CLI over everything above
test/server/Dockerfile                systemd + sshd Ubuntu 24.04 test server
internal/testenv/testenv.go           Start(t) → running container, integration tag
```

---

### Task 1: Repository skeleton + VLESS link

**Files:**
- Create: `go.mod`, `Makefile`, `.gitignore`, `LICENSE`, `THIRD_PARTY_LICENSES`, `internal/link/vless.go`
- Test: `internal/link/vless_test.go`

**Interfaces:**
- Produces:
  ```go
  type Params struct { UUID, Host string; Port int; SNI, PublicKey, ShortID, Fingerprint, Flow, Name string }
  func Build(p Params) string            // Fingerprint defaults "chrome", Flow defaults "xtls-rprx-vision", Port defaults 443
  func Parse(s string) (Params, error)   // errors: ErrNotVless, ErrMissingField{Field string}
  ```

- [ ] **Step 1: Prerequisites.** Install Go (latest stable from go.dev, ≥1.24) and confirm `go version`. Run `git init`, `go mod init github.com/theuittti/WayVeil`. `.gitignore`: `.superpowers/`, `build/`, `internal/server/assets/wv-scan-*`. Copy reference `LICENSE` text into `THIRD_PARTY_LICENSES` under a heading naming the repo URL. Makefile targets: `test` (`go test ./...`), `scanner` (Task 3), `it` (`WAYVEIL_IT=1 go test -tags integration -count=1 -timeout 20m ./...`).

- [ ] **Step 2: Write failing tests** in `vless_test.go`:
  - `TestBuildExact`: `Build(Params{UUID:"11111111-2222-3333-4444-555555555555",Host:"203.0.113.10",SNI:"www.example.nl",PublicKey:"PBK",ShortID:"ab12",Name:"Вася ноутбук"})` == `vless://11111111-2222-3333-4444-555555555555@203.0.113.10:443?flow=xtls-rprx-vision&fp=chrome&pbk=PBK&security=reality&sid=ab12&sni=www.example.nl&type=tcp#%D0%92%D0%B0%D1%81%D1%8F%20%D0%BD%D0%BE%D1%83%D1%82%D0%B1%D1%83%D0%BA` (query keys sorted — `url.Values.Encode`; fragment via `url.PathEscape`).
  - `TestRoundTripQuotes`: name `O'Brien "x"` → `Parse(Build(p)) == p` (with defaults filled).
  - `TestParseIPv6Host`: `vless://u@[2001:db8::1]:443?security=reality&sni=a&pbk=b&sid=c&type=tcp&flow=xtls-rprx-vision#n` → `Host == "2001:db8::1"`; `Build` of that brackets it again.
  - `TestParseErrors`: `"vmess://x"` → `errors.Is(err, ErrNotVless)`; missing `pbk` → `ErrMissingField{Field:"pbk"}`.

- [ ] **Step 3: Run** `go test ./internal/link/` → FAIL (undefined).
- [ ] **Step 4: Implement** `Build`/`Parse` with `net/url`; `Parse` requires uuid, host, port, `security=reality`, `sni`, `pbk`, `type=tcp`.
- [ ] **Step 5: Run** `go test ./internal/link/` → PASS.
- [ ] **Step 6: Commit** (also commit `docs/`): `git add -A && git commit -m "chore: skeleton; feat(link): VLESS reality link build/parse"`

---

### Task 2: Target check

**Files:**
- Create: `internal/targetcheck/check.go`
- Test: `internal/targetcheck/check_test.go`

**Interfaces:**
- Produces:
  ```go
  type Problem string
  const ( Unreachable Problem = "unreachable"; NoTLS13 = "no_tls13"; NoHTTP2 = "no_http2"; RedirectOther = "redirect_other_domain"; BadCert = "bad_certificate"; Slow = "slow" )
  type Report struct {
      Domain string `json:"domain"`; OK bool `json:"ok"`; TLS13 bool `json:"tls13"`; HTTP2 bool `json:"http2"`
      RedirectHost string `json:"redirectHost,omitempty"`; HandshakeMs int `json:"handshakeMs"`; Problems []Problem `json:"problems"`
  }
  type Options struct {
      Timeout time.Duration               // default 5s
      Port int                            // default 443
      RootCAs *x509.CertPool              // nil = system roots
      DialContext func(ctx context.Context, network, addr string) (net.Conn, error) // nil = net.Dialer
      SlowThreshold time.Duration         // default 800ms
  }
  func Check(ctx context.Context, domain string, o Options) Report
  ```
  `OK` = no problems other than `Slow` (slow is a warning).

- [ ] **Step 1: Write failing tests** using `httptest.NewUnstartedServer` with `TLS = &tls.Config{...}`, `EnableHTTP2 = true`, `StartTLS()`; pass `RootCAs` from `srv.Certificate()` and a `DialContext` that sends any address to `srv.Listener.Addr()`. Domain `example.com` (httptest cert SAN).
  - `TestCheckGood`: TLS1.3+h2, 200 → `OK`, `TLS13`, `HTTP2`, `Problems` empty.
  - `TestCheckNoTLS13`: server `MaxVersion: tls.VersionTLS12` → `Problems` contains `NoTLS13`, `OK == false`.
  - `TestCheckNoHTTP2`: `EnableHTTP2=false` → contains `NoHTTP2`.
  - `TestCheckRedirectOther`: handler 301 `Location: https://other.example/` → `RedirectHost == "other.example"`, contains `RedirectOther`.
  - `TestCheckRedirectSameHost`: 301 to `https://example.com/ru` → `OK`.
  - `TestCheckBadCert`: no `RootCAs` → contains `BadCert`.
  - `TestCheckUnreachable`: dialer returning error → `[]Problem{Unreachable}` only.
- [ ] **Step 2: Run** `go test ./internal/targetcheck/` → FAIL.
- [ ] **Step 3: Implement** `Check`: one `tls.Dial` with `MinVersion: tls.VersionTLS13`, `NextProtos: ["h2","http/1.1"]` to time the handshake and read `NegotiatedProtocol`; on handshake version failure retry with TLS1.2 only to distinguish `NoTLS13` from `Unreachable`; then `http.Client` with `CheckRedirect: return http.ErrUseLastResponse`, GET `https://domain/`, compare `Location` host case-insensitively.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(targetcheck): REALITY target requirements check`

---

### Task 3: Neighbour scan + `wv-scan` binary

**Files:**
- Create: `internal/targetscan/scan.go`, `cmd/wv-scan/main.go`, `internal/server/assets/.keep`
- Modify: `Makefile` (target `scanner`)
- Test: `internal/targetscan/scan_test.go`, `cmd/wv-scan/main_test.go`

**Interfaces:**
- Consumes: `targetcheck.Check`, `targetcheck.Options`, `targetcheck.Report`.
- Produces:
  ```go
  type Candidate struct { Domain string `json:"domain"`; IP string `json:"ip"`; Report targetcheck.Report `json:"report"` }
  type Options struct {
      Self netip.Addr; Port int /*443*/; PerAddrTimeout time.Duration /*1500ms*/; Concurrency int /*128*/
      Limit int /*5*/; Deadline time.Duration /*30s*/; Check targetcheck.Options
      Resolve func(ctx context.Context, host string) ([]netip.Addr, error) // nil = net.DefaultResolver
  }
  func ScanOrder(self netip.Addr) []netip.Addr // own /24 by |distance| from self (self excluded), then rest of /22 by distance; IPv4 only, IPv6 → nil
  func Scan(ctx context.Context, o Options, found func(Candidate)) (scanned int, err error)
  ```
  `wv-scan` CLI: `wv-scan check -domain D [-timeout 5s]` → one JSON `Report`. `wv-scan scan -self IP [-limit 5 -deadline 30s -concurrency 128 -timeout 1500ms]` → JSON lines `{"type":"found",...Candidate}` then `{"type":"done","scanned":N}`.

- [ ] **Step 1: Write failing tests**
  - `TestScanOrder`: self `10.0.1.5` → first three `10.0.1.4, 10.0.1.6, 10.0.1.3`; len == 1023; no `10.0.1.5`; element 254 is in `10.0.0.0/24`,`10.0.2.0/24` or `10.0.3.0/24` (first outside own /24); all within `10.0.0.0/22`.
  - `TestScanFindsAndVerifies` (loopback): start 2 TLS1.3+h2 httptest servers with cert SAN `good.test`, one TLS1.2 server with SAN `old.test`; Options with injected per-IP dial (map IP → listener addr) and `Resolve` returning the server IP; expect exactly one `Candidate{Domain:"good.test"}` (dedup by domain), `old.test` absent.
  - `TestScanSkipsWildcardAndIPSANs`: cert with only `*.wild.test` and an IP SAN → no candidates.
  - `TestScanLimitAndDeadline`: 10 good servers, `Limit:5` → `found` called exactly 5 times; with a server that hangs and `Deadline: 300ms`, `Scan` returns within 1s.
  - `main_test.go` `TestCheckJSON`: run `run([]string{"check","-domain","x"}, stdout, injectedCheck)` → valid JSON with key `"domain":"x"`.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement.** Scan: worker pool over `ScanOrder`; per IP TLS dial with `InsecureSkipVerify` + SNI = IP string to read the leaf cert; names = CN + DNS SANs minus wildcards/IPs; dedupe; verify each new name with `targetcheck.Check` (real verification, system roots); emit only `Report.OK`; stop on `Limit` or `Deadline`. Expose a test hook for the dialer via an unexported field set in tests (`o.dial`). `main.go` keeps a `run(args, w, deps)` function for testability.
- [ ] **Step 4: Makefile `scanner`:** `CGO_ENABLED=0 GOOS=linux GOARCH={amd64,arm64} go build -trimpath -ldflags "-s -w" -o internal/server/assets/wv-scan-linux-$ARCH ./cmd/wv-scan`. Make `test` and `it` depend on `scanner`.
- [ ] **Step 5: Run** `make test` → PASS; `ls -la internal/server/assets/` shows both binaries (each < 8 MB).
- [ ] **Step 6: Commit** `feat(targetscan): neighbour TLS scan and wv-scan binary`

---

### Task 4: Remote runner, SSH client, test server container

**Files:**
- Create: `internal/remote/remote.go`, `internal/sshx/client.go`, `internal/sshx/keys.go`, `test/server/Dockerfile`, `internal/testenv/testenv.go`
- Test: `internal/remote/remote_test.go`, `internal/sshx/keys_test.go`, `internal/sshx/client_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Produces:
  ```go
  // package remote
  type Runner interface {
      Run(ctx context.Context, cmd string, stdin []byte) (stdout []byte, err error)        // non-zero exit → *ExitError
      Stream(ctx context.Context, cmd string, stdin []byte, onLine func(string)) error      // stdout lines; stderr collected into ExitError
  }
  type ExitError struct { Code int; Stderr string }
  func ShellQuote(s string) string                         // POSIX single-quote quoting
  func WithEnv(script []byte, env map[string]string) []byte // prepends sorted `export K='v'` lines
  var ErrSudoPassword = errors.New("sudo_password_required")

  // package sshx
  type Target struct { Host string; Port int /*22*/; User string /*root*/; Password string; PrivateKeyPEM []byte; HostKey string /* authorized_keys format; empty = trust on first use */ }
  type Client struct{ /* implements remote.Runner */ }
  func Connect(ctx context.Context, t Target) (*Client, error)  // mismatch → ErrHostKeyMismatch; auth failure → ErrAuth
  func (c *Client) HostKey() string             // key seen during handshake (authorized_keys format)
  func (c *Client) Fingerprint() string         // SHA256:... for display
  func (c *Client) Close() error
  func GenerateKey() (privatePEM []byte, authorizedLine string, err error) // ed25519, comment "wayveil"
  ```
  When `User != "root"` every command is wrapped as `sudo -n bash -c <quoted cmd>`; a `sudo: a password is required` stderr maps to `ErrSudoPassword`.

- [ ] **Step 1: Write failing unit tests**
  - `TestShellQuote`: `O'Brien` → `'O'\''Brien'`; empty → `''`.
  - `TestWithEnv`: `WithEnv([]byte("echo hi\n"), {"B":"2","A":"x'y"})` == `"export A='x'\\''y'\nexport B='2'\necho hi\n"`.
  - `TestGenerateKey`: line starts `ssh-ed25519 ` and ends ` wayveil`; PEM parses with `ssh.ParsePrivateKey`.
- [ ] **Step 2: Run** `go test ./internal/remote/ ./internal/sshx/` → FAIL; implement; → PASS.
- [ ] **Step 3: Test server image** `test/server/Dockerfile` (FROM `ubuntu:24.04`): install `systemd systemd-sysv openssh-server sudo python3 curl ca-certificates iproute2`; root password `wayveil`; `PermitRootLogin yes`, `PasswordAuthentication yes`; user `deploy` (password `deploy`) with `/etc/sudoers.d/deploy: deploy ALL=(ALL) NOPASSWD:ALL`; user `nopw` (password `nopw`) in group `sudo` without NOPASSWD; `systemctl enable ssh`; `CMD ["/sbin/init"]`.
- [ ] **Step 4: `testenv.Start(t *testing.T) *Env`** (`Env{Host string; Port int}`): skips unless `WAYVEIL_IT=1`; `docker build -t wayveil-it test/server` (cached), `docker run -d --privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw -p 127.0.0.1::22 wayveil-it`, reads mapped port via `docker port`, waits until SSH answers (≤60 s), `t.Cleanup` removes the container. Each test gets a fresh container.
- [ ] **Step 5: Integration tests** (`client_integration_test.go`):
  - `TestRootPasswordRun`: Connect root/`wayveil`, `Run("id -u")` → `"0\n"`; `Fingerprint()` starts `SHA256:`.
  - `TestDeploySudo`: user `deploy` → `Run("id -u")` → `"0\n"` (wrapped with sudo).
  - `TestNopwSudo`: user `nopw` → `errors.Is(err, remote.ErrSudoPassword)`.
  - `TestHostKeyMismatch`: Connect with `HostKey` from a different generated key → `ErrHostKeyMismatch`.
  - `TestStreamLines`: `Stream("printf 'a\\nb\\n'; echo err >&2; exit 3")` → lines `[a b]`, `*ExitError{Code:3, Stderr:"err\n"}`.
  - `TestStdin`: `Run("cat", []byte("x'y"))` → `"x'y"`.
- [ ] **Step 6: Run** `make it` → PASS.
- [ ] **Step 7: Commit** `feat(sshx): SSH runner with sudo, TOFU host keys; integration test server`

---

### Task 5: Script protocol + precheck

**Files:**
- Create: `internal/server/protocol.go`, `internal/server/precheck.go`, `internal/server/scripts/precheck.sh`, `internal/server/embed.go` (`//go:embed scripts/*.sh assets/*`)
- Test: `internal/server/protocol_test.go`, `internal/server/precheck_test.go`, `internal/server/precheck_integration_test.go`

**Interfaces:**
- Consumes: `remote.Runner`, `remote.ExitError`.
- Produces:
  ```go
  type StepStatus string // "start" | "ok" | "warn" | "fail"
  type StepEvent struct { ID string; Status StepStatus; Message string }
  type ScriptError struct { Code, Message string } // from ##ERROR; implements error, Error() == Code
  type Line struct { Step *StepEvent; Result json.RawMessage; Err *ScriptError; Raw string }
  func ParseLine(s string) Line
  func runScript(ctx context.Context, r remote.Runner, name string, env map[string]string, onStep func(StepEvent), onRaw func(string)) (json.RawMessage, error) // streams `bash -s` with WithEnv(embedded script)

  type HostInfo struct {
      OSID string `json:"osId"`; OSVersion string `json:"osVersion"`; Arch string `json:"arch"` // "amd64"|"arm64"
      Supported bool `json:"supported"`; Port443Busy bool `json:"port443Busy"`; Port443Process string `json:"port443Process"`
      HasWayveil bool `json:"hasWayveil"`; HasForeignXray bool `json:"hasForeignXray"`; PublicIP string `json:"publicIp"`
  }
  func Precheck(ctx context.Context, r remote.Runner) (HostInfo, error)
  ```

- [ ] **Step 1: Failing unit tests**
  - `TestParseLine`: `##STEP xray ok` → Step{xray, ok, ""}; `##STEP bbr warn sysctl not permitted` → Message `sysctl not permitted`; `##RESULT {"a":1}` → Result `{"a":1}`; `##ERROR port_busy nginx on 443` → Err{port_busy, "nginx on 443"}; `hello` → only Raw.
  - `TestPrecheckParsesResult`: fake Runner streaming `##RESULT {"osId":"ubuntu","osVersion":"24.04","arch":"amd64","supported":true,...}` → HostInfo fields equal.
- [ ] **Step 2: Run** → FAIL; implement `ParseLine`, `runScript`, `Precheck`; → PASS.
- [ ] **Step 3: `precheck.sh`** reads `/etc/os-release`; `supported` true only for ubuntu 22.04/24.04, debian 11/12; `uname -m` x86_64→amd64, aarch64→arm64; `ss -ltnpH 'sport = :443'` for busy + process name; `HasWayveil` = `/etc/xray/wayveil.json` exists; `HasForeignXray` = xray binary or `xray.service` exists without marker; `PublicIP` = `ip -4 route get 1.1.1.1` src. Print one `##RESULT` (build JSON with `printf` + jq-free escaping; jq may not be installed yet).
- [ ] **Step 4: Integration tests:** `TestPrecheckFresh` (root) → `{ubuntu, 24.04, supported, Port443Busy:false, HasWayveil:false}`; `TestPrecheckPortBusy`: first `Run("nohup python3 -m http.server 443 >/dev/null 2>&1 &")` → `Port443Busy:true`, `Port443Process` contains `python3`.
- [ ] **Step 5: Run** `make test && make it` → PASS.
- [ ] **Step 6: Commit** `feat(server): script protocol and server precheck`

---

### Task 6: Install (with manifest)

**Files:**
- Create: `internal/server/scripts/install.sh`, `internal/server/install.go`
- Test: `internal/server/install_test.go`, `internal/server/install_integration_test.go`

**Interfaces:**
- Consumes: `Precheck`, `runScript`, `HostInfo`, `remote.Runner`.
- Produces:
  ```go
  const XrayVersion = "v<latest stable at implementation time, pinned>"
  type InstallOptions struct { Address, SNI, AuthUser, AuthKeyLine string; DNS string /* default "1.1.1.1" */ }
  type InstallResult struct { PublicKey string `json:"publicKey"`; ShortID string `json:"shortId"`; XrayVersion string `json:"xrayVersion"`; Warnings []StepEvent `json:"-"` }
  var ErrAlreadyInstalled = &ScriptError{Code: "already_installed"} // also: unsupported_os, port_busy, foreign_xray
  func Install(ctx context.Context, r remote.Runner, o InstallOptions, onStep func(StepEvent)) (InstallResult, error)
  ```
  `Install` runs `Precheck` first and refuses (`unsupported_os` / `port_busy` / `already_installed` / `foreign_xray`) before changing anything. Step IDs in order: `deps xray keys config service bbr logrotate authkey uninstaller verify`.

- [ ] **Step 1: Failing unit tests** (fake Runner): Precheck says `Port443Busy` → `Install` returns error with Code `port_busy` and the install script was never sent; `HasWayveil` → `already_installed`; happy path collects StepEvents in the order above and parses `InstallResult`; a `warn` step lands in `Warnings`.
- [ ] **Step 2: Run** → FAIL; implement `install.go`; → PASS.
- [ ] **Step 3: `install.sh`** — port the reference `new_install` non-interactively, inputs `WV_ADDRESS WV_SNI WV_DNS WV_AUTH_USER WV_AUTH_KEY WV_XRAY_VERSION`. `set -euo pipefail`; trap ERR → `##STEP <current> fail` + `##ERROR install_failed <last line>`. Manifest helpers append to `/etc/xray/wayveil-manifest`: `mf file <path>`, `mf dir <path>`, `mf pkg <name>`, `mf user <name>`, `mf service <name>`, `mf sysctl <path>`, `mf line <file> <base64-of-line>`. Write `mf` entries **before** creating each item, so a partial install can be uninstalled.
  - `deps`: `apt-get install -y curl unzip jq`, `mf pkg` only for packages `dpkg -s` reported missing before.
  - `xray`: download `https://github.com/XTLS/Xray-core/releases/download/$WV_XRAY_VERSION/Xray-linux-{64|arm64-v8a}.zip`, install to `/usr/local/bin/xray`; also geo files not needed.
  - `keys`: `xray x25519`, 8-hex short id, write `/etc/xray/wayveil.json` = `{"schema":1,"address":…,"sni":…,"publicKey":…,"shortId":…,"xrayVersion":…,"clients":{}}`.
  - `config`: reference config shape + inbound tag `vless-in` with empty `clients`, reality `dest "$SNI:443"`, `serverNames [$SNI]`; private-address block from reference `add_private_block`; `"api":{"tag":"api","services":["StatsService"]}`, `"stats":{}`, `"policy":{"levels":{"0":{"statsUserUplink":true,"statsUserDownlink":true}}}`, dokodemo-door inbound `127.0.0.1:10085` tag `api` routed to `api`; `"log":{"loglevel":"warning","access":"/var/log/xray/access.log"}`. Validate with `xray run -test`.
  - `service`: reference systemd unit (user `xray`, `CAP_NET_BIND_SERVICE`), enable + start.
  - `bbr`: write sysctl file, `sysctl -p` file; on failure `##STEP bbr warn …` and continue.
  - `logrotate`: `/var/log/xray/*.log { daily rotate 7 missingok notifempty copytruncate }`.
  - `authkey`: append `WV_AUTH_KEY` to `~WV_AUTH_USER/.ssh/authorized_keys` (create dir 0700/file 0600 owned by user) only if absent; `mf line`.
  - `uninstaller`: placeholder step — Go uploads `uninstall.sh` to `/etc/xray/wayveil-uninstall.sh` right after (Task 8 adds the upload); script emits `ok`.
  - `verify`: `systemctl is-active xray` and `ss -ltnH 'sport = :443'`; then `##RESULT`.
- [ ] **Step 4: Integration tests**
  - `TestInstallRoot`: install with SNI `www.microsoft.com` (only needs to be syntactically fine for install) → no error; `systemctl is-active xray` = `active`; `/etc/xray/wayveil-manifest` contains `file /usr/local/bin/xray` and `service xray`; `jq .publicKey /etc/xray/wayveil.json` == result PublicKey; `authorized_keys` of root contains the line once.
  - `TestInstallDeploy`: same via `deploy` user; key lands in `/home/deploy/.ssh/authorized_keys`, owned by deploy.
  - `TestInstallBBRWarnOK`: in the container BBR may be refused — assert install succeeded and `bbr` step status ∈ {ok, warn}.
  - `TestInstallTwice`: second `Install` → `already_installed`, `wayveil.json` unchanged (same publicKey).
  - `TestInstallPortBusy`: python on 443 → `port_busy`, `/usr/local/bin/xray` absent.
- [ ] **Step 5: Run** `make it` → PASS.
- [ ] **Step 6: Commit** `feat(server): non-interactive xray install with manifest`

---

### Task 7: Client management, traffic, last seen, target change

**Files:**
- Create: `internal/server/scripts/ctl.sh`, `internal/server/clients.go`
- Test: `internal/server/clients_test.go`, `internal/server/clients_integration_test.go`

**Interfaces:**
- Consumes: `remote.Runner`, `link.Build`, `link.Params`.
- Produces:
  ```go
  type Client struct { ID string `json:"id"` /* xray email, "c-" + 8 hex */; UUID string `json:"uuid"`; Name string `json:"name"`; Created time.Time `json:"created"` }
  type ServerInfo struct { Address, SNI, PublicKey, ShortID, XrayVersion string } // json tags as in wayveil.json
  type Traffic struct { Up int64 `json:"up"`; Down int64 `json:"down"` }
  type Seen struct { IP string `json:"ip"`; At time.Time `json:"at"` }
  func Info(ctx context.Context, r remote.Runner) (ServerInfo, error)
  func ListClients(ctx context.Context, r remote.Runner) ([]Client, error)          // sorted by Created
  func AddClient(ctx context.Context, r remote.Runner, name string) (Client, error)
  func RemoveClient(ctx context.Context, r remote.Runner, id string) error          // unknown id → ScriptError{Code:"no_such_client"}
  func ReissueClient(ctx context.Context, r remote.Runner, id string) (Client, error) // same ID/name, new UUID; traffic kept
  func TrafficAll(ctx context.Context, r remote.Runner) (map[string]Traffic, error)
  func LastSeen(ctx context.Context, r remote.Runner) (map[string]Seen, error)
  func SetSNI(ctx context.Context, r remote.Runner, domain string) error            // updates dest, serverNames, wayveil.json
  func ClientLink(info ServerInfo, c Client) string
  ```
  ctl invocation: `bash -s -- <cmd> [args]` with the embedded `ctl.sh`; names passed base64 (`add <b64name>`); output one JSON line, error `{"error":code,"message":…}` + exit 1 → `*ScriptError`.

- [ ] **Step 1: Failing unit tests** (fake Runner returning canned JSON): `AddClient` sends args `add 0JLQsNGB0Y8g0L3QvtGD0YLQsdGD0Lo=` for `Вася ноутбук`; `{"error":"no_such_client"}` → `ScriptError.Code == "no_such_client"`; `LastSeen` parses `{"lastSeen":{"c-1a2b3c4d":{"ip":"95.24.1.2","at":"2026-11-07T12:00:00Z"}}}`; `ClientLink` equals `link.Build` with fields mapped and `Name` = client name.
- [ ] **Step 2: Run** → FAIL; implement `clients.go`; → PASS.
- [ ] **Step 3: `ctl.sh`** — commands `info list add remove reissue stats lastseen set-sni`. All mutating commands: `flock /run/wayveil.lock`; **call `stats` accumulation first** (so counters survive the restart); jq edit to temp of both `config.json` (`.inbounds[]|select(.tag=="vless-in").settings.clients`) and `wayveil.json` (`.clients[id] = {name, created}`); `xray run -test -config tmp`; `mv`; `systemctl restart xray`. `stats`: `xray api statsquery --server=127.0.0.1:10085 -reset`, sum `user>>>ID>>>traffic>>>uplink|downlink` into `/etc/xray/wayveil-traffic.json` (create `{}` if missing), print totals. `remove` also drops the id from traffic. `lastseen`: one `awk` pass over `/var/log/xray/access.log*` (`zcat -f`), lines of form `YYYY/MM/DD HH:MM:SS.ffffff from [tcp:]IP:PORT accepted … email: ID` → keep latest per ID; output RFC3339 UTC (log time is server local time — convert with `date -d`).
- [ ] **Step 4: Integration tests** (container after `Install` with SNI `www.microsoft.com`):
  - `TestClientsCRUD`: add `Вася ноутбук` and `O'Brien "x"` → `ListClients` returns both names byte-exact; config.json clients length 2 with `flow == "xtls-rprx-vision"`; reissue changes UUID only; remove → length 1; remove again → `no_such_client`.
  - `TestTrafficSurvivesRestart` (needs internet from container): add client; inside container start the installed `xray` as a client (socks inbound 127.0.0.1:1080, vless outbound to `127.0.0.1:443` with the client's link params) and `curl -x socks5h://127.0.0.1:1080 https://www.microsoft.com -o /dev/null`; `TrafficAll[id].Down > 0`; `systemctl restart xray`; `TrafficAll[id].Down` ≥ previous value. This also proves the link works end-to-end.
  - `TestLastSeen`: append two synthetic lines for the same id with different times/IPs to access.log → `LastSeen[id].IP` is the later one's.
  - `TestSetSNI`: `SetSNI("www.apple.com")` → `Info().SNI == "www.apple.com"`, xray active.
- [ ] **Step 5: Run** `make test && make it` → PASS.
- [ ] **Step 6: Commit** `feat(server): client management, traffic accounting, last seen, SNI change`

---

### Task 8: Uninstall

**Files:**
- Create: `internal/server/scripts/uninstall.sh`, `internal/server/uninstall.go`
- Modify: `internal/server/install.go` (after `##RESULT`, upload `uninstall.sh` to `/etc/xray/wayveil-uninstall.sh` via `install -m 0700 /dev/stdin <path>` and record `mf file`)
- Test: `internal/server/uninstall_integration_test.go`

**Interfaces:**
- Consumes: `runScript`, `remote.Runner`.
- Produces: `func Uninstall(ctx context.Context, r remote.Runner, authKeyLine string, onStep func(StepEvent)) error`
  Steps: `service files sysctl packages user authkey`. `WV_AUTH_KEY` (the app's own line) is passed so the key is removed even if the manifest is already gone. Exit 0 with all steps `ok` when there is nothing left to do.

- [ ] **Step 1: `uninstall.sh`:** read manifest into memory (if present); stop/disable/remove services; remove `file` entries then `dir` entries deepest-first (`tac`), all `rm -f`/`rm -rf` tolerant; remove sysctl files and `sysctl --system` (warn on failure); `apt-get remove -y` recorded packages; `userdel xray` if recorded; `systemctl daemon-reload`; remove `/etc/xray` entirely; **last**: delete the exact authorized_keys line (from manifest `line` entries and from `WV_AUTH_KEY`) in every home that has it, using `grep -vxF`. Works when run manually as `sudo bash /etc/xray/wayveil-uninstall.sh` (no env → only manifest lines).
- [ ] **Step 2: Integration tests**
  - `TestUninstallClean`: snapshot `dpkg --get-selections`, `getent passwd` before install; install; add a client; `Uninstall` → no error; afterwards: none of the Global Constraints server paths exist; `systemctl cat xray` fails; `getent passwd xray` empty; package list equals snapshot; root `authorized_keys` no longer contains the line; `ss -ltnH 'sport = :443'` empty; then `Install` again succeeds.
  - `TestUninstallTwice`: second `Uninstall` → nil error.
  - `TestUninstallManualCopy`: install, then `Run("bash /etc/xray/wayveil-uninstall.sh")` → exit 0 and `/usr/local/bin/xray` gone.
- [ ] **Step 3: Run** `make it` → PASS.
- [ ] **Step 4: Commit** `feat(server): manifest-driven full uninstall`

---

### Task 9: Target presets and suggestions on the server

**Files:**
- Create: `rules/targets.json`, `rules/rules.go` (`//go:embed targets.json`), `internal/server/targets.go`
- Test: `rules/rules_test.go`, `internal/server/targets_test.go`, `internal/server/targets_integration_test.go`

**Interfaces:**
- Consumes: embedded `assets/wv-scan-linux-*`, `HostInfo.Arch`, `targetcheck.Report`, `targetscan.Candidate`.
- Produces:
  ```go
  // package rules
  type Targets struct { Version int `json:"version"`; ByCountry map[string][]string `json:"byCountry"`; Russian []string `json:"russian"` }
  func DefaultTargets() Targets
  // package server
  type SuggestionKind string // "nearby" | "country" | "russian"
  type Suggestion struct { Kind SuggestionKind `json:"kind"`; Domain string `json:"domain"`; Report targetcheck.Report `json:"report"`; Recommended bool `json:"recommended"` }
  func CheckTarget(ctx context.Context, r remote.Runner, arch, domain string) (targetcheck.Report, error)
  func SuggestTargets(ctx context.Context, r remote.Runner, h HostInfo, t rules.Targets, on func(Suggestion)) error
  func CleanupScanner(ctx context.Context, r remote.Runner) error
  ```
  Order of emission: verified `country` presets for the server country and verified `russian` presets first (checked in parallel, ~5 s), then `nearby` candidates as `wv-scan scan` streams them. `Recommended = true` for every `nearby`; if the scan finishes with none, the fastest `country` preset becomes recommended (emitted again with the flag). Server country: `curl -fsS --max-time 5 https://ipinfo.io/country` run on the server (empty → skip country presets). Scanner uploaded once to `/tmp/wv-scan` (0755); `SuggestTargets` ends with `CleanupScanner`; results written to `wayveil.json .scanCache` when the server is installed.

- [ ] **Step 1: `targets.json` seed:** `russian: ["ozon.ru","vk.com","mail.ru"]`; `byCountry` with keys `NL, DE, FI, FR, GB, US, SE, PL` — 3–5 popular local HTTPS sites each (examples: NL `www.nu.nl`, `www.bol.com`, `tweakers.net`). Entries need not be perfect: every one is re-checked from the server before display.
- [ ] **Step 2: Failing tests:** `TestTargetsValid`: every domain matches `^[a-z0-9.-]+\.[a-z]{2,}$`, no duplicates, every country key is 2 upper-case letters. `TestSuggestOrder` (fake Runner: `ipinfo` → `NL`, `wv-scan check` → ok for `www.nu.nl` and `vk.com` only, `wv-scan scan` streams one candidate `nearby.example`): emitted sequence has the presets before `nearby.example`, failed presets absent, `nearby.example` has `Recommended`. `TestSuggestFallbackRecommend`: scan finds nothing → a `country` suggestion re-emitted with `Recommended:true`. `TestCleanupCalled`: last command contains `rm -f /tmp/wv-scan`.
- [ ] **Step 3: Run** → FAIL; implement; → PASS.
- [ ] **Step 4: Integration test** `TestCheckTargetReal` (needs internet): `CheckTarget(…, "www.microsoft.com")` → `TLS13 == true`; `CheckTarget(…, "tls-v1-2.badssl.com")` → contains `no_tls13`; `/tmp/wv-scan` absent after `CleanupScanner`.
- [ ] **Step 5: Run** `make test && make it` → PASS.
- [ ] **Step 6: Commit** `feat(server): target presets and streaming suggestions`

---

### Task 10: Owner store + developer CLI

**Files:**
- Create: `internal/ownerstore/store.go`, `cmd/wayveil-dev/main.go`, `README.md` (dev section only)
- Test: `internal/ownerstore/store_test.go`, `cmd/wayveil-dev/main_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces:
  ```go
  type ServerProfile struct { Host string; Port int; User string; HostKey string; PrivateKeyPEM []byte; AuthorizedLine string }
  func Dir() (string, error)                       // os.UserConfigDir()/wayveil  (Windows %APPDATA%\Wayveil)
  func Save(p ServerProfile) error                 // servers/<host>_<port>.json, file mode 0600, dir 0700; never has a password field
  func Load(host string, port int) (ServerProfile, error)
  func Delete(host string, port int) error
  ```
  CLI (`flag` subcommands, human output; `-json` for raw):
  `wayveil-dev install -host H [-port 22 -user root] -sni D` (password read from stdin/terminal without echo, prints fingerprint and asks y/N on first connect, generates key, runs `Install`, `Save`s profile, adds client `Я`, prints link);
  `targets -host H` (streams `SuggestTargets`); `check-target -host H -domain D`;
  `clients list|add NAME|rm ID|reissue ID|link ID -host H` (list shows name, traffic, last seen);
  `set-sni -host H -domain D`; `uninstall -host H` (confirm, then `Uninstall`, `Delete`).

- [ ] **Step 1: Failing tests:** `TestSaveLoad` (temp `XDG_CONFIG_HOME`/`APPDATA`): round-trip equal; file mode `0600` on unix; serialized JSON contains no `"password"` key. `TestCLINoPasswordInProfile`: run `install` against a fake installer dependency and check the saved file does not contain the provided password string.
- [ ] **Step 2: Run** → FAIL; implement; → PASS.
- [ ] **Step 3: Manual smoke test on a real VPS** (user-run, Ubuntu 24.04, fresh): `go run ./cmd/wayveil-dev install -host <IP> -sni <pick from targets>`; time from Enter to link < 3 min (spec acceptance 1); import the printed link into v2rayNG or Hiddify on a phone and open youtube.com; `clients add Тест`, use it, `clients list` shows traffic; `clients rm` → that phone loses access within seconds (acceptance 3); `uninstall` → `ssh root@IP 'ls /etc/xray; systemctl status xray'` shows nothing (acceptance 9 of spec numbering).
- [ ] **Step 4: Commit** `feat: owner profile store and wayveil-dev CLI`

---

## Self-Review Notes

- Spec coverage for this plan's scope: 4.2 (Tasks 4–6, 8), 4.3 (Tasks 2, 3, 9), 4.4 (Task 7), 4.8 partially (Task 10; encryption deferred to Plan 3 — stated in Roadmap), acceptance 1–4 and 9 (Tasks 6–8, 10). Client/sing-box (4.5), settings (4.6), window/tray (4.7), packaging (4.9) belong to Plans 2–3.
- "Connect to existing server" (spec 4.2, 4.4) needs no extra server code: `Precheck.HasWayveil` + `Info` + `ListClients` over a password login, then `GenerateKey` + authorized-key append — wired in Plan 3's wizard; the CLI's `install` on an existing server returns `already_installed`.
