# Wayveil — Plan 2 of 3: Client Engine (Linux) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go client engine + `wayveil` CLI for Linux that imports a `vless://` link or QR image, generates a sing-box config with system-wide TUN split tunnelling (blocked-in-RU → proxy, everything else → direct, no DNS leaks), runs and supervises sing-box, keeps rule lists fresh, and needs elevated rights at most once.

**Architecture:** Pure config generation (`internal/singbox`) from a stored profile + settings + rule-list files; a process controller (`internal/engine`) that runs the pinned sing-box binary and talks to its Clash API on 127.0.0.1; a list updater (`internal/lists`) with bundled fallbacks; Linux file capabilities granted once via `pkexec setcap`. Everything the Plan 3 GUI needs is a Go API; the CLI is a thin wrapper. Routing is proven in Docker (client container with TUN ↔ Plan 1 test server) and end-to-end against the user's disposable test VPS.

**Tech Stack:** Go 1.27, sing-box **1.14.4** (pinned binary), itdoginfo/allow-domains `.srs` rule-sets, `github.com/makiuchi-d/gozxing` (QR), `golang.org/x/sys/unix`, Docker (`--cap-add NET_ADMIN --device /dev/net/tun`, never `--privileged`).

**Spec:** `docs/superpowers/specs/2026-11-07-wayveil-design.md` — sections 4.5, 4.6 (rules, mode, lists), 4.8, acceptance 5–8. Windows parts of 4.5 (service helper, wintun) move to Plan 3 (user decision 2026-11-07).

## ⚠ Amendment 2026-11-07: client core is mihomo, not sing-box (user decision)

Found during Task 8: xray-core ≥ v26.9.8 rejects sing-box's REALITY ClientHello (no X25519MLKEM768 key share; sing-box#4520, unfixed). Verified on the test server: sing-box 1.14.4 → no connection with any fingerprint; mihomo v1.19.33 → HTTP 200. Tasks 1–3 and 5–7 were done against sing-box; this amendment overrides the sing-box specifics everywhere below.

- **Binary:** mihomo **v1.19.33** (`MihomoVersion`), `build/mihomo` via `make mihomo` (gz from MetaCubeX/mihomo releases, `mihomo-linux-amd64-v1.19.33.gz`). Validate with `mihomo -t -d <home> -f <config>`; run with `mihomo -d <home> -f <config>`. `<home>` = `CacheDir()` (mihomo only accepts rule files under its home), config at `run/config.yaml`, lists under `lists/`.
- **Config:** JSON (valid YAML) generated from Go maps by `internal/mihomo` (replaces `internal/singbox`, which is deleted). Keys: `mode: rule`, `ipv6: false`, `geo-auto-update: false`, `external-controller` 127.0.0.1:port + `secret`; `tun{enable, device: wayveil0, stack: mixed, auto-route, auto-detect-interface, strict-route, dns-hijack: [any:53], inet4-address: [172.19.0.1/30], mtu: 1500}` or `mixed-port` (tests); `dns{enable, ipv6: false, enhanced-mode: redir-host, default-nameserver: [77.88.8.8], nameserver: [https://77.88.8.8/dns-query], proxy-server-nameserver: same, nameserver-policy: {"rule-set:<domain sets>": [https://1.1.1.1/dns-query#proxy], "+.<always-proxy>": same, "+.<always-direct>": local}}` (smart) — all-mode: `nameserver: [https://1.1.1.1/dns-query#proxy]`; one VLESS proxy named `proxy` (`flow`, `servername`, `client-fingerprint: chrome`, `reality-opts`), dialled by IP.
- **Rules (smart, exact order):** private CIDRs `DIRECT,no-resolve` → server IPs `IP-CIDR(6),…,DIRECT,no-resolve` (+ `DOMAIN,<host>,DIRECT` when the host is a name) → `DOMAIN-SUFFIX,<d>,DIRECT` → `DOMAIN-SUFFIX,<d>,proxy` → `RULE-SET,<domain set>,proxy` → `RULE-SET,<ip set>,proxy,no-resolve` → `MATCH,DIRECT`. All-mode: same up to always-direct, then `MATCH,proxy`.
- **Lists (Task 3 rework):** `.mrs` (zstd, magic `28 b5 2f fd`). Tags/behaviors: `ru-blocked` domain (`russia_inside_domain.mrs`), `telegram-domain`/`telegram-ip`, `meta-domain`/`meta-ip`, `twitter-domain`/`twitter-ip`, `discord-domain`/`discord-ip` (`*_domain.mrs` / `*_ipcidr.mrs`). `rules.Source` gains `Behavior` ("domain"|"ipcidr"). Files `lists/<tag>.mrs`. `ru-blocked` required in smart mode.
- **Engine/caps/CLI (Tasks 5–7 rework):** `mihomo.Find()` ($WAYVEIL_MIHOMO, next to exe, `build/mihomo`); Clash API unchanged except chains are `DIRECT`/`proxy` (compare case-insensitively); capability grant targets the mihomo binary.
- **Task 8:** client container mounts `build/mihomo`; everything else as written.
- **Dropped:** sing-box's DNS "legacy address filter" warning no longer applies.

## Global Constraints

- sing-box version pinned: `SingBoxVersion = "1.14.4"`; config format = sing-box 1.14 (typed DNS servers, rule actions `sniff` / `hijack-dns`, `route.default_domain_resolver`). Every generated config must pass `sing-box check`.
- Outbound: `vless` + `flow: xtls-rprx-vision` + `tls.reality` + `utls.fingerprint: chrome`, built from `link.Params` (Plan 1 `internal/link`).
- TUN: interface `wayveil0`, addresses `172.19.0.1/30` and `fdfe:dcba:9876::1/126`, `auto_route: true`, `strict_route: true`, `stack: "mixed"`, `mtu: 1500`.
- Smart-mode route order (exact): sniff → hijack-dns → private IPs direct → server IPs/domain direct → user "always direct" → user "always proxy" → rule-sets (`ru-blocked`, `telegram`, `meta`, `twitter`, `discord`) proxy → `final: direct`.
- All-mode: sniff → hijack-dns → private direct → server direct → user "always direct" → `final: proxy`.
- DNS: `remote` = DoH `1.1.1.1` via `proxy`; `local` = DoH `77.88.8.8` direct. Smart: rule-set/always-proxy domains → `remote`, `final: local`. All: `final: remote`. `strategy: "ipv4_only"` (blocked sites must not bypass the tunnel over IPv6).
- Rule-set sources (itdoginfo/allow-domains latest release): `russia_inside.srs` → tag `ru-blocked` (blocked + geo-blocked domains), `telegram.srs`, `meta.srs`, `twitter.srs`, `discord.srs` (domains + IP CIDRs). URLs live in `rules/sources.json` (embedded, overridable by the copy on `main` at `https://raw.githubusercontent.com/theuittti/WayVeil/main/rules/sources.json`).
- Lists refresh: on connect when older than 24 h; bundled copies under `rules/lists/*.srs` are used when no valid cache exists. A downloaded file is accepted only if it starts with the `SRS` magic bytes and is < 4 MiB.
- Clash API: `127.0.0.1:<random free port>` with a random 32-hex secret per run; never on 0.0.0.0.
- App dirs: config `os.UserConfigDir()/wayveil` (settings, profile — 0600 files, 0700 dir); cache `os.UserCacheDir()/wayveil` (lists, generated config, pid, log).
- Elevation on Linux: at most once — `pkexec setcap cap_net_admin,cap_net_bind_service,cap_net_raw+ep <sing-box path>`; nothing else ever runs as root.
- Integration containers never use `--privileged` (protects the developer's host network).
- Real-VPS credentials come only from `~/.config/wayveil-test/vps.env` (`WV_TEST_HOST`, `WV_TEST_PORT`, `WV_TEST_USER`, `WV_TEST_PASS`); never printed, logged or committed.

## Review Focus

1. **Server address is a domain, not an IP** — must be resolved before start and excluded from the TUN, otherwise the proxy connection loops into the tunnel. (Task 4)
2. **Broken or partial list download** (HTML error page, truncated file, GitHub unreachable) — previous cache or bundled copy stays in use; sing-box never starts with a missing rule-set file. (Task 3)
3. **sing-box dies after start** (bad link, unreachable server, `kill -9`) — controller reports the failure, status shows disconnected, and the user's internet works again after `disconnect`/next `connect` (no stale routes). (Tasks 5, 8)
4. **User rule input like `https://Example.COM/path`, ` youtube.com `, `*.site.ru`** — normalised to `example.com`, `youtube.com`, `site.ru`; garbage rejected with an error. (Task 1)
5. **Host with IPv6** — blocked domains resolve to A records only and are proxied; config captures IPv6 in the TUN. (Task 4)

---

## File Structure

```
internal/appdir/appdir.go            ConfigDir, CacheDir (moved out of ownerstore)
internal/ownerstore/store.go         (modify) use appdir.ConfigDir
internal/clientstore/store.go        Settings, Mode, Load, Save, NormalizeDomain
internal/profile/profile.go          Profile, FromLink, FromQRImage
internal/lists/lists.go              Source, Sources, Ensure, Update, Status
rules/sources.json, rules/lists/*.srs (bundled), rules/rules.go (modify: embed both)
internal/singbox/config.go           Input, Inbound kinds, Build
internal/singbox/binary.go           Version, Find (next to exe / build/ / $WAYVEIL_SINGBOX)
internal/engine/engine.go            Engine: Connect, Disconnect, Status; process + pid; log
internal/engine/clash.go             Clash API client: Version, Connections, Traffic
internal/engine/caps_linux.go        HasCaps, GrantCaps (pkexec setcap)
cmd/wayveil/main.go                  client CLI
Makefile                             (modify) singbox, lists, e2e targets
test/client/Dockerfile               client test image (curl, tcpdump, iproute2, nftables)
internal/testenv/client.go           StartClient(t, network) helper
internal/engine/routing_integration_test.go
internal/engine/e2e_vps_test.go      //go:build e2e — real VPS
```

---

### Task 1: App dirs, client settings, domain normalisation

**Files:**
- Create: `internal/appdir/appdir.go`, `internal/clientstore/store.go`
- Modify: `internal/ownerstore/store.go` (use `appdir.ConfigDir`)
- Test: `internal/appdir/appdir_test.go`, `internal/clientstore/store_test.go`

**Interfaces:**
- Produces:
  ```go
  // appdir
  func ConfigDir() (string, error) // os.UserConfigDir()/wayveil ("Wayveil" on windows), created 0700
  func CacheDir() (string, error)  // os.UserCacheDir()/wayveil, created 0700
  // clientstore
  type Mode string // "smart" | "all"
  type Settings struct {
      Mode         Mode     `json:"mode"`          // default smart
      AlwaysProxy  []string `json:"alwaysProxy"`
      AlwaysDirect []string `json:"alwaysDirect"`
      Profile      *profile.Profile `json:"profile,omitempty"`
      ListsUpdated time.Time `json:"listsUpdated"`
  }
  func Load() (Settings, error)              // missing file → defaults, no error
  func Save(s Settings) error                // settings.json 0600, atomic
  func NormalizeDomain(s string) (string, error) // lower, trim, strip scheme/path/port/"*."/trailing dot; requires a dot; ErrBadDomain
  func (s *Settings) AddRule(list *[]string, domain string) error // normalised, deduped, removed from the opposite list
  ```
  `profile.Profile` is defined in Task 2; for Task 1 declare `Profile` field after Task 2 lands — **order: implement Task 2's type first** (Task 1 step 1 creates `internal/profile` with just the `Profile` struct).

- [ ] **Step 1:** Create `internal/profile/profile.go` with only `type Profile struct { Name string; Link string; Params link.Params }` (json tags `name`, `link`, `params`).
- [ ] **Step 2: Failing tests**
  - `TestDirs` (temp `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`): dirs end in `/wayveil`, exist, mode 0700; `ownerstore.Dir()` equals `appdir.ConfigDir()`.
  - `TestNormalizeDomain` table: `https://Example.COM/path?q` → `example.com`; ` youtube.com ` → `youtube.com`; `*.site.ru` → `site.ru`; `site.ru.` → `site.ru`; `site.ru:8443` → `site.ru`; `localhost`, ``, `http://`, `a b.com` → `ErrBadDomain`; `пример.рф` → `xn--e1afmkfd.xn--p1ai` (IDNA via `golang.org/x/net/idna` Lookup profile).
  - `TestSettingsDefaultsAndRoundTrip`: `Load()` on empty dir → `Mode == "smart"`, empty lists; `Save` then `Load` equal; file mode 0600.
  - `TestAddRuleMovesBetweenLists`: add `youtube.com` to AlwaysProxy, then to AlwaysDirect → only in AlwaysDirect; adding twice keeps one entry.
- [ ] **Step 3: Run** `go test ./internal/appdir/ ./internal/clientstore/ ./internal/ownerstore/` → FAIL.
- [ ] **Step 4: Implement**; make `ownerstore.Dir` delegate to `appdir.ConfigDir`.
- [ ] **Step 5: Run** → PASS (ownerstore tests still green).
- [ ] **Step 6: Commit** `feat(clientstore): app dirs, client settings, domain normalisation`
- [ ] **Step 7 (Plan 1 bug found 2026-11-07):** `sshx.Connect` dials TCP with no timeout, so an unreachable host hangs ~2 min. Failing test `TestConnectUnreachableTimesOut` in `internal/sshx/client_test.go` (target `192.0.2.1:22`, TEST-NET, `Target.DialTimeout: 2*time.Second` → error within 3 s); add `DialTimeout time.Duration` to `sshx.Target` (default 15 s) and use `net.Dialer{Timeout: …}`. Commit `fix(sshx): dial timeout`.

---

### Task 2: Profile import (link and QR image)

**Files:**
- Modify: `internal/profile/profile.go`
- Test: `internal/profile/profile_test.go`, `internal/profile/testdata/` (generated in test, not committed)

**Interfaces:**
- Consumes: `link.Parse`, `link.Params`.
- Produces:
  ```go
  func FromLink(s string) (Profile, error)          // trims spaces/newlines; Name = Params.Name or Host
  func FromQRImage(path string) (Profile, error)    // PNG/JPEG; ErrNoQR when none; then FromLink
  var ErrNoQR = errors.New("no_qr_code")
  ```
- [ ] **Step 1: Failing tests:** `TestFromLink` (link from `link.Build` with name `Вася` wrapped in `"\n  "` → `Name == "Вася"`, Params equal); `TestFromLinkInvalid` (`vmess://…` → `errors.Is(err, link.ErrNotVless)`); `TestFromQRImage` (encode a link with gozxing `qrcode.NewQRCodeWriter()` to 300×300, save PNG in `t.TempDir()`, decode → same Params); `TestFromQRImageNoCode` (blank PNG → `ErrNoQR`).
- [ ] **Step 2: Run** → FAIL. **Step 3:** `go get github.com/makiuchi-d/gozxing`; implement with `image.Decode` (+`_ "image/jpeg"`, `_ "image/png"`) and `qrcode.NewQRCodeReader().Decode` with `TRY_HARDER` hint. **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(profile): import from vless link and QR image`

---

### Task 3: Rule lists — sources, bundled copies, updater

**Files:**
- Create: `rules/sources.json`, `rules/lists/{russia_inside,telegram,meta,twitter,discord}.srs` (via `make lists`), `internal/lists/lists.go`
- Modify: `rules/rules.go` (embed `sources.json`, `lists/*.srs`), `Makefile` (target `lists`)
- Test: `internal/lists/lists_test.go`

**Interfaces:**
- Produces:
  ```go
  type Source struct { Tag string `json:"tag"`; URL string `json:"url"` }
  // package rules
  func DefaultSources() []Source            // from sources.json
  func Bundled(tag string) ([]byte, bool)   // embedded .srs by tag
  // package lists
  type Options struct { Dir string; HTTP *http.Client; SourcesURL string /* "" = default raw URL */ }
  func Ensure(o Options) (map[string]string, error)  // tag → file path; writes bundled copy for any tag lacking a valid cached file
  func Update(ctx context.Context, o Options) (UpdateResult, error) // downloads all; per-tag atomic replace only when valid
  type UpdateResult struct { Updated, Kept []string; Errors map[string]string }
  func Valid(b []byte) bool                  // len>3 && b[:3]=="SRS" && len < 4<<20
  ```
  Tags/URLs in `sources.json`: `ru-blocked`→`…/releases/latest/download/russia_inside.srs`, `telegram`, `meta`, `twitter`, `discord` → same pattern. `Update` first fetches `SourcesURL` (JSON array of Source); on failure or invalid JSON uses `DefaultSources()`.

- [ ] **Step 1:** `make lists` downloads the five files into `rules/lists/` (curl -fsSL, fails on HTTP errors) and is run once; commit the files (total ≈ 15 KB).
- [ ] **Step 2: Failing tests** (httptest server):
  - `TestEnsureWritesBundled`: empty dir → 5 paths, each file equals `rules.Bundled(tag)`.
  - `TestUpdateReplacesValid`: server serves `SRS…` bytes for all → `Updated` has 5 tags, files equal served bytes.
  - `TestUpdateRejectsHTML`: server returns 200 `<html>` for `telegram` → `Kept` contains `telegram`, file unchanged, `Errors["telegram"]` non-empty.
  - `TestUpdateServerDown`: closed server → all kept, `Ensure` still returns 5 existing paths.
  - `TestSourcesOverride`: `SourcesURL` serves `[{"tag":"ru-blocked","url":"<srv>/x.srs"}]` → only that tag downloaded from the override URL; the other four still present from bundled.
- [ ] **Step 3: Run** → FAIL. **Step 4: Implement** (write to `name.tmp`, `os.Rename`). **Step 5: Run** → PASS.
- [ ] **Step 6: Commit** `feat(lists): rule-set sources, bundled copies and safe updater`

---

### Task 4: sing-box config builder

**Files:**
- Create: `internal/singbox/config.go`, `internal/singbox/binary.go`
- Modify: `Makefile` (target `singbox`: download pinned `sing-box-1.14.4-linux-amd64.tar.gz` to `build/sing-box`; `test` depends on it)
- Test: `internal/singbox/config_test.go`

**Interfaces:**
- Consumes: `link.Params`, `clientstore.Mode`.
- Produces:
  ```go
  const Version = "1.14.4"
  type InboundKind int
  const ( TUN InboundKind = iota; Mixed )        // Mixed = socks/http on 127.0.0.1:MixedPort (tests, debugging)
  type Input struct {
      Server       link.Params
      ServerIPs    []netip.Addr      // resolved by caller; must be non-empty
      Mode         clientstore.Mode
      AlwaysProxy, AlwaysDirect []string
      RuleSets     map[string]string // tag → .srs path (from lists.Ensure)
      Inbound      InboundKind
      MixedPort    int
      ClashAddr, ClashSecret string
      LogPath      string
  }
  func Build(in Input) ([]byte, error)          // indented JSON; error if ServerIPs empty or a required tag is missing
  func Find() (string, error)                    // $WAYVEIL_SINGBOX, then dir of os.Executable(), then <repo>/build/sing-box
  ```
- [ ] **Step 1: Failing tests** (decode output into `map[string]any`, assert structurally):
  - `TestSmartRouteOrder`: route.rules actions/outbounds in exactly the Global Constraints order; `final == "direct"`; rule-set rule lists all five tags.
  - `TestAllMode`: no rule-set rule in route; `final == "proxy"`; dns `final == "remote"`.
  - `TestServerExcluded`: `Server.Host = "vpn.example.com"`, `ServerIPs = [198.51.100.7, 2001:db8::7]` → a direct rule with `ip_cidr` `["198.51.100.7/32","2001:db8::7/128"]` and `domain: ["vpn.example.com"]` appears before any proxy rule; outbound `server == "198.51.100.7"` (dial by IP, no DNS loop).
  - `TestUserRules`: AlwaysDirect `["youtube.com"]` → `domain_suffix` direct rule before the rule-set rule; AlwaysProxy `["example.org"]` → proxy rule and a DNS rule to `remote`.
  - `TestDNSNoLeak`: dns.servers `remote` has `detour: "proxy"`, type `https`, server `1.1.1.1`; `local` server `77.88.8.8`; `strategy == "ipv4_only"`; a `hijack-dns` route rule exists.
  - `TestTUNShape`: inbound `wayveil0`, both addresses, `auto_route`/`strict_route` true; `Mixed` kind → `{"type":"mixed","listen":"127.0.0.1","listen_port":MixedPort}` and no tun.
  - `TestClashLocalOnly`: `experimental.clash_api.external_controller` starts with `127.0.0.1:`; secret equals input.
  - `TestMissingRuleSet`: smart mode without `ru-blocked` path → error.
  - `TestSingBoxCheck`: for smart/all × TUN/Mixed write config + real bundled `.srs` paths, run `<Find()> check -c file` → exit 0 (skip only if `Find()` fails, with message `run make singbox`).
- [ ] **Step 2: Run** `make singbox && go test ./internal/singbox/` → FAIL. **Step 3: Implement** using typed Go structs marshalled to JSON (no templates). **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(singbox): split-tunnel config builder for sing-box 1.14`

---

### Task 5: Engine — run, supervise and query sing-box

**Files:**
- Create: `internal/engine/engine.go`, `internal/engine/clash.go`
- Test: `internal/engine/engine_test.go` (uses real `build/sing-box` with `Mixed` inbound; no root needed)

**Interfaces:**
- Consumes: `singbox.Build`, `singbox.Find`, `lists.Ensure`, `clientstore.Settings`, `appdir.CacheDir`.
- Produces:
  ```go
  type State string // "disconnected" | "connecting" | "connected" | "failed"
  type Status struct { State State; Since time.Time; Error string; Up, Down int64; Server string; Mode clientstore.Mode }
  type Options struct { Inbound singbox.InboundKind; MixedPort int; Resolve func(ctx context.Context, host string) ([]netip.Addr, error) }
  type Engine struct{ /* cache dir, pid file, clash addr/secret */ }
  func New(o Options) (*Engine, error)
  func (e *Engine) Connect(ctx context.Context, s clientstore.Settings) error   // resolve server, Ensure lists, Build, `sing-box check`, start detached, wait ≤10s for Clash API; stale pid/process from a previous run is stopped first
  func (e *Engine) Disconnect(ctx context.Context) error                      // SIGTERM, wait ≤5s, then SIGKILL; removes pid file; idempotent
  func (e *Engine) Status(ctx context.Context) Status                         // pid alive + Clash API reachable → connected; pid file but dead → failed with last 5 log lines
  // clash.go
  type Clash struct{ Addr, Secret string; HTTP *http.Client }
  func (c Clash) Version(ctx context.Context) error
  func (c Clash) Connections(ctx context.Context) ([]Conn, Traffic, error) // Conn{Host string; DestIP string; Chains []string}; Traffic{Up, Down int64}
  ```
  Runtime files in `CacheDir()/run/`: `config.json` (0600), `singbox.pid`, `singbox.log`, `clash.json` (addr+secret, 0600) so a separate CLI invocation can query/stop.
- [ ] **Step 1: Failing tests** (Mixed inbound on a free port; server params point at a closed local port so no real proxy is needed):
  - `TestConnectStatusDisconnect`: Connect → Status `connected`; `clash.json` mode 0600 and addr on 127.0.0.1; Disconnect → `disconnected`, pid file gone, process gone.
  - `TestRoutingDecisionViaClash`: with Mixed inbound, `curl`-equivalent Go HTTP request through the socks proxy to `http://youtube.com/` (will fail upstream) and to an httptest server on 127.0.0.1 → Connections (polled ≤2s) shows youtube.com with chain containing `proxy`, the local one `direct`.
  - `TestProcessDiedReported`: Connect, then `kill -9` the pid → Status `failed`, `Error` non-empty; Disconnect returns nil; next Connect succeeds.
  - `TestStaleProcessReplaced`: two `New()` engines sharing the cache dir; second Connect stops the first's process (old pid no longer alive).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** (start with `exec.Command(bin, "run", "-c", cfg)`, `SysProcAttr{Setsid: true}`, stdout/stderr → log file; random secret `crypto/rand` 16 bytes hex; free port via `net.Listen("127.0.0.1:0")`). **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(engine): run and supervise sing-box, Clash API status`

---

### Task 6: One-time elevation on Linux (file capabilities)

**Files:**
- Create: `internal/engine/caps_linux.go`, `internal/engine/caps_other.go` (`//go:build !linux`, stubs returning `nil`/`true`)
- Test: `internal/engine/caps_linux_test.go`

**Interfaces:**
- Produces:
  ```go
  func HasCaps(bin string) (bool, error)        // reads xattr "security.capability"; true iff CAP_NET_ADMIN is in the permitted set
  func GrantCaps(ctx context.Context, bin string) error // runs exactly: pkexec setcap cap_net_admin,cap_net_bind_service,cap_net_raw+ep <bin>; ErrElevationDenied on exit 126/127
  var ErrElevationDenied = errors.New("elevation_denied")
  ```
  `Engine.Connect` with `Inbound == TUN` returns `ErrNeedsElevation` (new exported error) when `HasCaps` is false — the CLI/GUI then calls `GrantCaps` once and retries; Connect never calls pkexec itself.
- [ ] **Step 1: Failing tests:** `TestHasCapsFalse` (fresh copy of `build/sing-box` in temp dir → false); `TestHasCapsParsesXattr` (unit-test the parser `parseCapXattr([]byte)` with a VFS_CAP_REVISION_2 blob that has bit 12 set → true, with bit 12 clear → false); `TestGrantCapsCommand` (inject a fake `pkexec` via `PATH` that records argv and exits 126 → `ErrElevationDenied`, argv exactly `setcap cap_net_admin,cap_net_bind_service,cap_net_raw+ep <bin>`); `TestConnectNeedsElevation` (TUN inbound, uncapped binary → `errors.Is(err, ErrNeedsElevation)` and no process started).
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement** with `unix.Getxattr`. **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat(engine): one-time capability grant for TUN on Linux`

---

### Task 7: `wayveil` client CLI

**Files:**
- Create: `cmd/wayveil/main.go`
- Test: `cmd/wayveil/main_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces commands (human output; exit 0/1):
  `wayveil import <vless-link | path/to/qr.png>` ·
  `wayveil connect` (Ensure lists, refresh if `ListsUpdated` older than 24 h — refresh failure is a warning, not fatal; on `ErrNeedsElevation` print one line explaining why and call `GrantCaps`, then retry once) ·
  `wayveil disconnect` · `wayveil status` (state, since, server, mode, ↓/↑) ·
  `wayveil mode smart|all` (reconnects if connected) ·
  `wayveil rules list | proxy <domain> | direct <domain> | rm <domain>` (reconnects if connected) ·
  `wayveil lists update` · `wayveil setup` (GrantCaps only).
  Internals: `run(args []string, d deps) int` with `deps{out io.Writer; engine engineAPI; grant func(ctx, bin) error; update func(ctx, lists.Options) (lists.UpdateResult, error)}`.
- [ ] **Step 1: Failing tests** (fake engine + temp config dir): `TestImportThenStatus` (import a built link → settings profile saved; status prints `disconnected`); `TestConnectGrantsOnce` (fake engine returns `ErrNeedsElevation` first, nil second → `grant` called exactly once, exit 0); `TestConnectGrantDenied` (grant returns `ErrElevationDenied` → exit 1, message contains `TUN`); `TestRulesNormalised` (`rules proxy https://Example.COM/x` → settings AlwaysProxy `["example.com"]`); `TestModeReconnects` (engine connected → `mode all` calls Disconnect then Connect with Mode all); `TestConnectWithoutProfile` → exit 1, message says to run `wayveil import`.
- [ ] **Step 2: Run** → FAIL. **Step 3: Implement.** **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `feat: wayveil client CLI`

---

### Task 8: Routing integration tests in Docker (TUN ↔ test server)

**Files:**
- Create: `test/client/Dockerfile`, `internal/testenv/client.go`, `internal/engine/routing_integration_test.go`
- Modify: `internal/testenv/testenv.go` (optional `Network string` so server and client share a user-defined Docker network; server container gets alias `vpn-server`)

**Interfaces:**
- Consumes: `testenv.Start`, `server.Install`, `server.AddClient`, `server.Info`, `server.ClientLink`, the built `build/wayveil` + `build/sing-box` (mounted read-only at `/opt/wayveil/`).
- Produces: `testenv.StartClient(t, network string) *Client` with `Exec(t, cmd) string` (runs `docker exec` as non-root user `app`; `sing-box` copied to `/home/app/` and given caps inside the container by root during setup — this models the one-time grant). Client container: `--cap-add NET_ADMIN --device /dev/net/tun`, user `app`, not privileged.

Test flow (each test): start network, server container (install with SNI `www.microsoft.com`), add client, start client container, `wayveil import <link>` (server address rewritten to `vpn-server`'s container IP), `wayveil connect`, assertions via `wayveil status` and the Clash API (`/connections`):
- [ ] **Step 1: Write tests**
  - `TestTUNSmartRouting`: `curl -s -o /dev/null https://www.youtube.com` → connection chain `proxy`; `curl https://www.gosuslugi.ru` (may fail upstream; only the decision matters) → `direct`; `curl --connect-timeout 5 https://149.154.167.50` (Telegram IP, no domain) → `proxy`.
  - `TestTUNDNSNoLeak`: run `tcpdump -ni eth0 -c 50 'udp port 53'` (as root via docker exec) while `getent hosts www.youtube.com` → capture contains no `youtube`.
  - `TestTUNAllMode`: `wayveil mode all` → `gosuslugi.ru` chain `proxy`.
  - `TestTUNUserDirectOverride`: `wayveil rules direct youtube.com` → youtube chain `direct`.
  - `TestTUNKill9Recovers`: `kill -9` sing-box → `wayveil status` says `failed`; `wayveil disconnect`; `ip rule` has no sing-box rules and `curl -s https://www.gosuslugi.ru -o /dev/null -w '%{http_code}'` works (non-000); `wayveil connect` works again.
- [ ] **Step 2: Run** `make build it` (new Makefile target `build`: `go build -o build/wayveil ./cmd/wayveil` + `wayveil-dev`) → expect failures, fix code (systematic-debugging), → PASS. Any fix lands with the test that exposed it.
- [ ] **Step 3: Commit** `test: TUN routing integration tests`

---

### Task 9: End-to-end on the real test VPS + README

**Files:**
- Create: `internal/engine/e2e_vps_test.go` (`//go:build e2e`)
- Modify: `Makefile` (target `e2e`: `go test -tags e2e -count=1 -timeout 20m ./internal/engine/`), `README.md` (client section)

**Interfaces:**
- Consumes: `~/.config/wayveil-test/vps.env`; `sshx`, `server.*`, `testenv.StartClient`.

- [ ] **Step 1: Test** `TestE2EVPS` (skips with a clear message if `vps.env` missing):
  1. Connect with password once; if no key yet, generate one, authorize it (append to `authorized_keys` exactly like `server.Install` does), store key at `~/.config/wayveil-test/id_ed25519` (0600); later runs use the key. Password never printed.
  2. `Uninstall` (ignore "nothing to do"), `Install` with the best `SuggestTargets` result (fallback `www.microsoft.com`), `AddClient("e2e")`.
  3. Client container (default Docker network, real internet): import link, `wayveil rules proxy ipify.org`, `wayveil rules direct ifconfig.me`, connect.
  4. Assert: `curl -s https://api.ipify.org` == VPS public IP; `curl -s https://ifconfig.me/ip` == the host's own public IP (fetched before connecting); `curl -s -o /dev/null -w %{http_code} https://www.youtube.com` ∈ 2xx/3xx with chain `proxy`; `https://chatgpt.com` and `https://www.instagram.com` chain `proxy`; `https://www.gosuslugi.ru` chain `direct`.
  5. Leave the server installed (next run reinstalls).
- [ ] **Step 2: Run** `make build e2e` → PASS.
- [ ] **Step 3: README:** client quick start (`wayveil import`, `connect`, `rules`, `mode`, `setup`), what "smart" routes, where lists come from, how to update them.
- [ ] **Step 4: Commit** `test: end-to-end on real VPS; docs: client usage`

---

## Self-Review Notes

- Spec 4.5 (Linux part): import ✓ T2, engine ✓ T5, inbound/outbound/routing/all-mode ✓ T4, DNS ✓ T4/T8, lists + bundled + daily refresh ✓ T3/T7, elevation once ✓ T6/T7. 4.6 rules/mode/lists-now ✓ T1/T7. 4.8 client storage ✓ T1. Acceptance 5 ✓ T8/T9, 6 ✓ T8, 7 (Telegram by IP) ✓ T8, 8 ✓ T6/T7. Acceptance "2ip.ru shows Russian IP" is checked as "direct sites show the host's own IP" because the test host is not in Russia.
- Deferred to Plan 3: Windows helper service + wintun, GUI, tray, auto-start, GeoIP country label, settings encryption (DPAPI/keyring), packaging (`.deb` will install sing-box root-owned under `/usr/lib/wayveil` and setcap in postinst, making Task 6's prompt unnecessary for deb users).
- antifilter IP lists from the spec are replaced by itdoginfo per-service IP rule-sets (telegram/meta/twitter/discord) — one maintained source, sing-box native format. *(Planner's call.)*
