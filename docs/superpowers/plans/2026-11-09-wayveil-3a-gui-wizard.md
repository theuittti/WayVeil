# Wayveil 3a: GUI wizard + connect (Linux) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Linux desktop app (Wails v2) where the owner sets up a VPS through a 5-screen wizard and connects with one button; a friend pastes a link and connects.

**Architecture:** All logic stays in the existing Go packages (`internal/server`, `engine`, `ownerstore`, `clientstore`, `profile`, `sshx`). A new UI-free package `internal/gui` exposes one `App` struct whose exported methods the frontend calls and which pushes progress through an `Emitter` interface; it is fully testable without a window. `cmd/wayveil-gui` (build tag `desktop`) only wires `App` into Wails. The frontend is a thin Svelte + TS app; screen flow lives in a pure TS module with vitest tests.

**Tech Stack:** Go 1.27, Wails v2 (latest v2.x, `github.com/wailsapp/wails/v2`), WebKitGTK 4.1 (`webkit2_41` tag), Svelte 5 + TypeScript + Vite, vitest, gozxing (already a dependency) for QR encode.

**Spec:** `docs/superpowers/specs/2026-11-09-wayveil-3a-gui-wizard-design.md` (and the main spec `docs/superpowers/specs/2026-11-07-wayveil-design.md` for style A). Approved wizard mockup: `.superpowers/brainstorm/651397-1794240238/content/wizard.html` (gitignored; local only — style values are repeated below).

## Global Constraints

- UI copy is **English only**. No i18n dictionary.
- Neutral wording: first screen "Paste a connection link" (placeholder `vless://…`), bottom link "I have my own server → set up". Never "friend" in the UI; the owner's first device is named **"Me"**.
- Window ~380×640, not maximised by default, fixed size (`DisableResize: true`).
- Style A: background `radial-gradient(circle at 50% 0%, #1e2a4a 0%, #0b0f1a 65%)`; primary button/round button gradient `#3b82f6 → #8b5cf6` with glow `0 0 18px rgba(99,102,241,.45)`; cards `rgba(255,255,255,.06)`; text `#e8ecf5`; ok `#4ade80`, warning `#facc15`, accent link `#93c5fd`; system font; no UI libraries.
- The SSH password is never stored, logged, emitted in events or included in error strings. It lives only for the duration of `ServerConnect`.
- `internal/gui` must not import Wails (so `go test ./...` runs on machines without WebKitGTK).
- Every file under `cmd/wayveil-gui/` carries `//go:build desktop` (Wails needs cgo + WebKitGTK headers; plain `go build/vet/test ./...` must skip it).
- Elevation (`pkexec setcap`) at most once, via existing `engine.GrantCaps`.
- The server never depends on GitHub during install: upload the zip matching `HostInfo.Arch` (`Xray-linux-64.zip` for amd64, `Xray-linux-arm64-v8a.zip` for arm64) shipped next to the GUI binary; if the file is missing, pass no zip (server downloads itself — existing behaviour).
- One owner server per app in 3a.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Go is at `~/.local/go/bin` (prepend to PATH). Docker only via `sg docker -c "<cmd>"` in Claude's environment. Integration tests: `WAYVEIL_IT=1 go test -tags integration`.

## Review Focus

1. **Messy host input** (`" 1.2.3.4 "`, `root@1.2.3.4`, `ssh://1.2.3.4:2222`, `[2001:db8::1]`, empty) — trimmed/parsed where unambiguous, otherwise a clear "Enter the server's IP address or hostname" error; never a raw Go error. Test in Task 4.
2. **Double click on Install / Connect** — a second call while one is running returns the "busy" error and starts nothing. Test in Tasks 4 and 5.
3. **Connection lost mid-install** (SSH drop, context cancelled) — human message plus the screen offers "Clean up and install again" (next precheck reports partial install). Test in Task 4 (fake runner returning an error mid-stream) and Task 9 (integration with `WV_FAIL_AT`).
4. **Pasted link with whitespace/newlines or not a vless link** — trimmed; non-vless gets "This is not a vless:// connection link". Test in Task 5.
5. **Leaving the target screen while the scan runs** (Back, or Install pressed early) — scan context cancelled, scanner removed from server, late suggestions from the old scan never appear on screen. Test in Task 4 (cancel → no emits after `ServerDisconnect`/`Install` begins).

---

### Task 1: Build skeleton (Wails v2 app, frontend scaffold, Makefile, docker builder)

**Files:**
- Create: `cmd/wayveil-gui/main.go` (`//go:build desktop`), `cmd/wayveil-gui/embed.go` (`//go:build desktop`, `//go:embed all:frontend_dist` — see below)
- Create: `frontend/` (Vite + Svelte 5 + TS): `package.json`, `package-lock.json`, `vite.config.ts`, `tsconfig.json`, `index.html`, `src/main.ts`, `src/App.svelte`, `src/styles/tokens.css`, `src/lib/smoke.test.ts`
- Create: `tools/gui-builder/Dockerfile`
- Modify: `Makefile`, `.gitignore`, `go.mod`/`go.sum`

**Interfaces:**
- Produces: `make frontend` → `frontend/dist`; `make app` → `build/wayveil-gui` plus `build/mihomo`, `build/xray`, `build/Xray-linux-64.zip`, `build/Xray-linux-arm64-v8a.zip`; `make ui` → Vite dev server on a mock backend (Task 6 adds the mock); `make app-smoke` → runs the built binary under Xvfb in the builder image for 5 s and fails if it exits early.
- Embedding: `make app` copies `frontend/dist` to `cmd/wayveil-gui/frontend_dist` (gitignored) before `go build`, so the embed path is inside the package.

- [ ] **Step 1:** Scaffold `frontend/` with Svelte 5 + TS + Vite + vitest (`npm create vite@latest` template `svelte-ts`, then `npm i -D vitest`), scripts `dev`, `build`, `test` (`vitest run`). `tokens.css` defines the style-A CSS variables from Global Constraints. `App.svelte` renders the style-A background and the text "Wayveil". `src/lib/smoke.test.ts`: `expect(1+1).toBe(2)` placeholder that Task 6 replaces.
- [ ] **Step 2:** Run `cd frontend && npm test && npm run build` → PASS, `frontend/dist/index.html` exists.
- [ ] **Step 3:** `go get github.com/wailsapp/wails/v2@latest` (must resolve to v2.x). `main.go`: `wails.Run(&options.App{Title: "Wayveil", Width: 380, Height: 640, DisableResize: true, BackgroundColour: &options.RGBA{R: 11, G: 15, B: 26, A: 255}, AssetServer: &assetserver.Options{Assets: assets}, OnStartup: …})` with nothing bound yet.
- [ ] **Step 4:** `tools/gui-builder/Dockerfile`: `FROM golang:1.27-bookworm`, apt `libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config xvfb xauth`, Node ≥ 20 copied from `node:22-bookworm` (`/usr/local/bin/node`, `/usr/local/lib/node_modules/npm`, npm symlink). Makefile `app`: if `pkg-config --exists webkit2gtk-4.1` and `npm` exist, build natively; otherwise build the image `wayveil-gui-builder` and run the same build inside it with the repo mounted, `--user $(id -u):$(id -g)`, `HOME=/tmp`, `GOCACHE=/tmp/gocache`, `GOMODCACHE` mounted from the host's `$(go env GOMODCACHE)`. The Go build command: `CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 -trimpath -ldflags "-s -w" -o build/wayveil-gui ./cmd/wayveil-gui`. `app` depends on `build` (mihomo, xray) and the new `build/Xray-linux-arm64-v8a.zip` target (same pattern as the amd64 zip target: curl with retries, gh fallback, `sha256sum -c` against `1811c8f1da0c3b6dc707ebaf1c8390e987ba39859a8f0a7aadd8526157f2ec8d`). `.gitignore`: `frontend/node_modules/`, `frontend/dist/`, `cmd/wayveil-gui/frontend_dist/`.
- [ ] **Step 5:** Run `sg docker -c "make app"` → `build/wayveil-gui` exists and `file build/wayveil-gui` says ELF 64-bit. Run `sg docker -c "make app-smoke"` → exits 0 (process alive after 5 s under `xvfb-run`). Run `go vet ./... && go test ./...` on the host → still green (cmd/wayveil-gui skipped by its tag).
- [ ] **Step 6:** Commit `build: Wails v2 GUI skeleton, frontend scaffold, docker builder`.

### Task 2: `server.Adopt` — take over an existing Wayveil server

**Files:**
- Modify: `internal/server/scripts/ctl.sh` (new subcommand `adopt <user> <base64 key line>`), `internal/server/clients.go`
- Test: `internal/server/adopt_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Produces: `func Adopt(ctx context.Context, r remote.Runner, user, keyLine string) error` — appends `keyLine` to `user`'s `authorized_keys` exactly like install.sh's `authkey` step (create `.ssh` 0700 owned by user, newline guard, 0600, idempotent via `grep -qxF`) and records `line <path> <base64 key>` in `/etc/xray/wayveil-manifest` **before** writing, only when the line was not already present.

- [ ] **Step 1: Failing test** `TestAdoptKeyRemovedByUninstall`: install on a fresh `testenv` container (key A, user root), `Adopt(…, "root", keyB)` → `grep -cxF keyB /root/.ssh/authorized_keys` == `1`; `Adopt` again → still `1` and manifest has exactly one entry for keyB; `Uninstall(…, "", nil)` → `grep -c` for keyB == `0`. Second test `TestAdoptNotInstalled`: fresh container, `Adopt` → `errors.Is(err, &ScriptError{Code: "not_installed"})`.
- [ ] **Step 2:** `WAYVEIL_IT=1 go test -tags integration -run TestAdopt ./internal/server/` → FAIL (undefined Adopt).
- [ ] **Step 3:** Implement (ctl.sh already fails with `not_installed` when `wayveil.json` is absent and holds the flock).
- [ ] **Step 4:** Re-run → PASS. Also `go test ./internal/server/` (unit) green.
- [ ] **Step 5:** Commit `feat(server): Adopt — authorize the app key on an existing install (in manifest)`.

### Task 3: `internal/client` — shared connect logic

**Files:**
- Create: `internal/client/client.go`, `internal/client/client_test.go`
- Modify: `cmd/wayveil/main.go` (use the package; its tests must keep passing unchanged in behaviour)

**Interfaces:**
- Produces:
  ```go
  type Engine interface {
      Connect(context.Context, clientstore.Settings) error
      Disconnect(context.Context) error
      Status(context.Context) engine.Status
  }
  type Deps struct {
      Engine Engine
      Grant  func(ctx context.Context, bin string) error            // engine.GrantCaps
      Update func(context.Context, lists.Options) (lists.UpdateResult, error) // lists.Update
      Mihomo func() (string, error)                                  // mihomo.Find
      Warn   func(msg string)                                        // may be nil
  }
  const ListsMaxAge = 24 * time.Hour
  var ErrNoProfile = errors.New("no_profile")
  func Connect(ctx context.Context, s *clientstore.Settings, d Deps) error   // refresh lists if older than ListsMaxAge (failure → Warn, continue), engine.Connect, on engine.ErrNeedsElevation: Grant once then retry once
  func SaveAndApply(ctx context.Context, s *clientstore.Settings, d Deps) (reconnected bool, err error)
  func UpdateLists(ctx context.Context, s *clientstore.Settings, d Deps) (lists.UpdateResult, error)
  ```
- [ ] **Step 1: Failing tests** (fakes, `t.Setenv("XDG_CONFIG_HOME", t.TempDir())`): `TestConnectGrantsOnceOnElevation` (engine returns `ErrNeedsElevation` first then nil → Grant called exactly 1×, Connect 2×); `TestConnectElevationDeniedNoLoop` (Grant returns error → returned error wraps it, Connect called 1×); `TestConnectNoProfile` → `ErrNoProfile`; `TestConnectStaleListsWarnsAndContinues` (Update errors → Warn called, engine.Connect still called); `TestSaveAndApplyReconnectsOnlyWhenConnected`.
- [ ] **Step 2:** `go test ./internal/client/` → FAIL.
- [ ] **Step 3:** Move the logic out of `cmd/wayveil/main.go` (`connect`, `saveAndApply`, `updateLists`) into the package; `cmd/wayveil` keeps its output strings by printing around the calls.
- [ ] **Step 4:** `go test ./internal/client/ ./cmd/wayveil/` → PASS.
- [ ] **Step 5:** Commit `refactor(client): shared connect/apply logic for CLI and GUI`.

### Task 4: `internal/gui` — owner wizard backend

**Files:**
- Create: `internal/gui/app.go` (App, Emitter, deps, busy guard), `internal/gui/wizard.go`, `internal/gui/errors.go`, `internal/gui/qr.go`, `internal/gui/wizard_test.go`, `internal/gui/errors_test.go`

**Interfaces:**
- Consumes: `server.Precheck/SuggestTargets/CheckTarget/Install/Uninstall/Adopt/AddClient/Info/ClientLink`, `sshx.Connect/GenerateKey`, `ownerstore.Save/Load`, `clientstore.Load/Save`, `profile.FromLink`, `rules.DefaultTargets`.
- Produces (exported methods are what the frontend calls; JSON field names are camelCase via tags):
  ```go
  type Emitter interface{ Emit(event string, data any) }
  func New(e Emitter, d Deps) *App          // Deps: function fields for every external call above + XrayZip func(arch string) []byte + Now
  func NewDefault(e Emitter, exeDir string) *App // real deps; zips read from exeDir
  func (a *App) Shutdown()                  // cancels running work, closes SSH

  type AccessForm struct{ Host string; Port int; User, Password, PrivateKey string }
  type HostCheck struct {
      Fingerprint string `json:"fingerprint"` // SHA256:…
      Pinned      bool   `json:"pinned"`      // matches the saved host key (no need to confirm)
      OS, Arch    string                      // json "os","arch"
      State       string `json:"state"`       // "fresh" | "wayveil" | "partial" | "foreign" | "unsupported" | "port-busy"
      Detail      string `json:"detail"`      // e.g. OS name or the process on :443, English
  }
  func (a *App) ServerConnect(f AccessForm) (HostCheck, error) // parse host, connect (pinned key from ownerstore if saved; key auth if PrivateKey given or saved profile and empty Password), Precheck; when State is fresh/wayveil/partial start the background target scan
  func (a *App) ServerDisconnect()                              // cancels scan, closes SSH
  // events while scanning: "target" {domain, kind ("nearby"|"country"|"russian"), recommended, handshakeMs}; "target-done" {}
  type DomainCheck struct{ OK bool; Problems []string; Alternatives []string } // json ok, problems, alternatives
  func (a *App) CheckDomain(domain string) (DomainCheck, error) // problems as English sentences; up to 3 alternatives from suggestions seen so far (recommended first)
  type Done struct{ Link string; QR string } // json link, qr ("data:image/png;base64,…", 256 px)
  func (a *App) Install(sni string) (Done, error)   // cancels scan; GenerateKey; ownerstore.Save BEFORE install; Install with XrayZip(arch); AddClient("Me"); Info; save the "Me" link as the client profile (clientstore)
  func (a *App) Adopt() (Done, error)              // State "wayveil": GenerateKey, ownerstore.Save, server.Adopt, AddClient("Me"), …
  func (a *App) Reinstall(sni string) (Done, error) // State "wayveil"/"partial": Uninstall (steps emitted with id prefix "uninstall-"), then Install
  // events during install: "step" {id, status ("start"|"ok"|"warn"|"fail"), title, message, at (unix ms)}
  func Human(err error) string                      // errors.go
  var ErrBusy = errors.New("busy")
  ```
- Step titles (exact copy): uninstaller "Preparing clean removal", authkey "App access key", deps "Packages", xray "Xray", keys "REALITY keys", config "Configuration", service "Service", bbr "BBR", logrotate "Log rotation", verify "Final check".
- `Human` copy (exact): `sshx.ErrAuth` → "Could not log in: check the password or key."; `sshx.ErrHostKeyMismatch` → "The server's identity changed since last time. This may be an attack — not continuing."; `remote.ErrSudoPassword` → "This user needs a password for sudo. Use root or a user with passwordless sudo."; dial timeout / refused → "Cannot reach the server. Check the IP and port, and that the hosting firewall allows SSH."; `ErrUnsupportedOS` → "Unsupported system: <detail>. Wayveil supports Ubuntu 22.04/24.04/26.04 and Debian 12/13 — reinstall the server with Ubuntu 24.04 in your hosting panel."; `ErrPortBusy` → "Port 443 is used by <detail>. Free it and try again."; `ErrForeignXray` → "Another Xray installation is on this server. Remove it first: sudo systemctl disable --now xray && sudo rm -f /usr/local/bin/xray /etc/systemd/system/xray.service"; `ErrPartialInstall` → "A previous installation did not finish."; `ErrBusy` → "Already working on it…"; bad host → "Enter the server's IP address or hostname."; any other ScriptError → "Installation failed at step <step>: <message>"; anything else → "Something went wrong: <err>" (never containing the password).

- [ ] **Step 1: Failing tests** (fake runner/deps, temp XDG dirs):
  - `TestServerConnectStates` table: precheck flags → State (`Supported=false`→unsupported, `PartialInstall`→partial, `HasWayveil`→wayveil, `HasForeignXray`→foreign, `Port443Busy`→port-busy, else fresh); scan started only for fresh/wayveil/partial.
  - `TestParseHost`: `" 1.2.3.4 "`→`1.2.3.4`/22/root; `root@1.2.3.4`→user root; `ssh://admin@1.2.3.4:2222`→admin/2222; `[2001:db8::1]:22`; `""` and `"a b"` → error whose `Human` is "Enter the server's IP address or hostname."
  - `TestPasswordNeverLeaks`: password `"S3cr3t-pw"`; run ServerConnect with an auth failure and a success path, Install with a failing step; assert no emitted event payload (`fmt.Sprintf("%+v")`), no returned error string, no file under the temp config dir contains it.
  - `TestInstallOrder`: fake records calls: `GenerateKey` → `ownerstore.Save` → `Install` (with zip for the precheck arch: amd64 → bytes of fake `Xray-linux-64.zip`, arm64 → arm64 zip) → `AddClient("Me")` → `Info`; returned `Done.Link` starts with `vless://`, `Done.QR` starts with `data:image/png;base64,`; clientstore profile saved.
  - `TestInstallBusy`: second `Install` while first blocks → `ErrBusy`, Install dep called once.
  - `TestInstallStreamDrop`: fake Install emits two steps then returns `io.ErrUnexpectedEOF` → error returned, `Human` non-empty, steps were emitted with titles from the table.
  - `TestScanCancelledNoLateEvents`: fake SuggestTargets emits slowly; call `Install` → no `target` events emitted after Install started; scan ctx cancelled.
  - `TestCheckDomainAlternatives`: failed report with problems → `OK=false`, English problems, alternatives = recommended first, max 3.
  - `TestAdoptAndReinstall`: Adopt calls `server.Adopt` with the generated key line and user; Reinstall calls Uninstall then Install, uninstall steps emitted with `uninstall-` prefix.
  - `errors_test.go`: one case per `Human` mapping above.
- [ ] **Step 2:** `go test ./internal/gui/` → FAIL.
- [ ] **Step 3:** Implement. Notes: a single `sync.Mutex`-guarded `busy` flag shared by Install/Adopt/Reinstall/ServerConnect; scan runs in its own goroutine with its own cancel; emitted target events carry a scan generation number checked before Emit so a cancelled scan cannot emit; QR via `gozxing/qrcode.NewQRCodeWriter().Encode(link, gozxing.BarcodeFormat_QR_CODE, 256, 256, nil)` → PNG.
- [ ] **Step 4:** `go test -race ./internal/gui/` → PASS.
- [ ] **Step 5:** Commit `feat(gui): owner wizard backend (connect, targets, install, adopt, reinstall)`.

### Task 5: `internal/gui` — connection backend

**Files:**
- Create: `internal/gui/conn.go`, `internal/gui/conn_test.go`

**Interfaces:**
- Consumes: Task 3 `client.Connect/SaveAndApply`, `clientstore`, `profile.FromLink/FromQRImage`.
- Produces:
  ```go
  type UIState struct {
      HasProfile  bool   `json:"hasProfile"`
      ProfileName string `json:"profileName"`
      Server      string `json:"server"`
      HasOwner    bool   `json:"hasOwner"`  // an owner server is saved
      Mode        string `json:"mode"`      // "smart" | "all"
  }
  type ConnStatus struct {
      State string `json:"state"` // "disconnected" | "connecting" | "connected" | "failed"
      Since int64  `json:"since"` // unix ms, 0 when not connected
      Up, Down int64                // json "up","down"
      Error string `json:"error"`  // Human() text
  }
  func (a *App) State() UIState
  func (a *App) ImportLink(s string) (UIState, error)      // trims whitespace/newlines; non-vless → "This is not a vless:// connection link."
  func (a *App) ImportQRFile(path string) (UIState, error) // "" (dialog cancelled) → current state, nil
  func (a *App) Connect() (ConnStatus, error)              // busy guard; uses client.Connect (elevation once)
  func (a *App) Disconnect() (ConnStatus, error)
  func (a *App) Status() ConnStatus
  func (a *App) SetMode(mode string) (ConnStatus, error)   // "smart"|"all", else error; client.SaveAndApply
  ```
- [ ] **Step 1: Failing tests:** `TestImportLinkTrims` (`"  vless://…\n"` imports); `TestImportLinkRejects` (`"https://x"`, `""` → that exact message); `TestConnectBusy` (blocking fake engine → second Connect returns ErrBusy); `TestConnectElevationOnce` (via Task 3 deps fake: Grant called once over two Connect/Disconnect cycles when the first grant succeeds and HasCaps then reports true); `TestStatusMapping` (engine.Status → ConnStatus, Since in ms, Error through Human); `TestSetModeValidates`.
- [ ] **Step 2:** `go test ./internal/gui/` → FAIL.
- [ ] **Step 3:** Implement.
- [ ] **Step 4:** `go test -race ./internal/gui/` → PASS.
- [ ] **Step 5:** Commit `feat(gui): connection backend (import, connect, mode, status)`.

### Task 6: Frontend shell — backend bridge, flow state machine, Paste and Main screens

**Files:**
- Create: `frontend/src/lib/backend.ts`, `frontend/src/lib/mock.ts`, `frontend/src/lib/flow.ts`, `frontend/src/lib/flow.test.ts`, `frontend/src/lib/format.ts`, `frontend/src/lib/format.test.ts`, `frontend/src/screens/Paste.svelte`, `frontend/src/screens/Main.svelte`, `frontend/src/components/RoundButton.svelte`, `frontend/src/components/Card.svelte`
- Modify: `frontend/src/App.svelte`, `Makefile` (`ui` target: `cd frontend && VITE_MOCK=1 npm run dev`)
- Delete: `frontend/src/lib/smoke.test.ts`

**Interfaces:**
- Consumes: Task 4/5 method names and JSON shapes. In Wails v2 bound methods are `window.go.gui.App.<Method>(...)` returning Promises; events via `window.runtime.EventsOn(name, cb)` (returns an unsubscribe fn).
- Produces:
  - `backend.ts`: `export const api: Api` with one async function per Go method (same names, same JSON types as TS interfaces `UIState, ConnStatus, AccessForm, HostCheck, DomainCheck, Done, TargetEvent, StepEvent`) and `on(event, cb): () => void`. When `window.go` is absent (or `import.meta.env.VITE_MOCK`), `api` is `mock.ts` (scripted: a fresh server, targets arriving over ~3 s, install steps every ~400 ms, connect after 1 s) so `make ui` shows every screen in a browser.
  - `flow.ts`: pure reducer `next(state: Screen, ev: FlowEvent): Screen` with `Screen = 'paste'|'main'|'access'|'target'|'installing'|'done'|'existing'|'refused'` and events `start(UIState)`, `ownServer`, `back`, `connected(HostCheck)`, `chooseTarget`, `installFinished`, `installFailed`, `adopt`, `reinstall`, `imported`, `openWizard`, `replaceLink`. Rules: start → `main` if hasProfile else `paste`; `connected` with state fresh → `target`, wayveil/partial → `existing`, foreign/unsupported/port-busy → `refused`; `back` from target → access (backend `ServerDisconnect` called by the screen); `installFinished` → `done`; done + `connect` → `main`.
  - `format.ts`: `bytes(n): string` ("0 B", "1.5 KB", "2.3 GB"), `duration(ms): string` ("0:41", "1:02:03").
- [ ] **Step 1: Failing tests** `flow.test.ts` (every rule above, one `it` each) and `format.test.ts` (the examples above plus 0 and large values).
- [ ] **Step 2:** `cd frontend && npm test` → FAIL.
- [ ] **Step 3:** Implement the modules and the two screens. Paste screen: title "Wayveil", "Paste a connection link", input with placeholder `vless://…`, buttons "From clipboard" (`navigator.clipboard.readText`, fallback: focus input) and "QR image" (calls `api.PickQRFile()` — see Task 8 — then `ImportQRFile`), errors in warning colour under the input, bottom link "I have my own server → set up". Main screen: round 160 px button (style A, glow when connected), status text "Disconnected" / "Connecting…" / "Connected" / error, timer from `since` (`duration`), cards "Downloaded" (`bytes(down)`) and "Mode: Smart / All traffic" toggle (`SetMode`), "⋯" menu with "Set up my own server" and "Replace link". Status polled with `api.Status()` every 1 s while Main is mounted.
- [ ] **Step 4:** `npm test && npm run build` → PASS. `make ui`, open the printed URL: Paste and Main render in style A at 380×640 (check with the browser's device toolbar).
- [ ] **Step 5:** Commit `feat(ui): backend bridge, flow, paste and main screens`.

### Task 7: Frontend wizard screens

**Files:**
- Create: `frontend/src/screens/Access.svelte`, `Target.svelte`, `Installing.svelte`, `Done.svelte`, `Existing.svelte`, `Refused.svelte`, `frontend/src/lib/steps.ts`, `frontend/src/lib/steps.test.ts`, `frontend/src/lib/targets.ts`, `frontend/src/lib/targets.test.ts`
- Modify: `frontend/src/App.svelte`

**Interfaces:**
- Consumes: Task 6 `api`, `on`, `flow`; event payloads from Task 4.
- Produces:
  - `targets.ts`: `merge(list: TargetEvent[], ev: TargetEvent): TargetEvent[]` — dedupe by domain (a later `recommended` replaces), order: recommended → nearby → country → russian, russian ones are **not** in the main list but returned by `russian(list)` for the caption line.
  - `steps.ts`: `apply(list: StepRow[], ev: StepEvent): StepRow[]` keeping install order (`uninstall-*` first if present, then uninstaller … verify), status per row, `fail` freezes later rows as pending.
- Screen copy (exact): Access — "Step 1 of 3", "Your server", "The details from your hosting provider's email. The password is used once and never saved.", fields "IP or address", "Port", "User", toggle "Password" / "Key", button "Connect". Host-key card after connect when `!pinned`: "Server fingerprint" + fingerprint + "Is this your server?" buttons "Yes, continue" / "No". Target — "Step 2 of 3", "Which site should the traffic look like?", "To your internet provider, your traffic will look like a normal visit to this site.", scanning line "Looking for sites near your server… <n> s" (disappears on `target-done`), "recommended" badge, caption "Also possible: <russian list> — Russian sites; on a foreign server the disguise may be easier to spot.", input "Your own domain…", warning box with problems and alternatives, button "Use anyway", "Next" always enabled once a target is selected (first recommended pre-selected, else first country one, else `www.microsoft.com`). Installing — "Step 3 of 3", "Installing", step rows with titles from events, "▸ Show detailed log" (expands step messages with times), elapsed timer; on failure the error (`Human`) and button "Clean up and install again" (→ `Reinstall(sni)`). Done — "Server is ready", "You are the first device, “Me”. You can add more devices later.", QR image, "Copy link", "Connect" (→ `ImportLink` is already done by backend; call `Connect` and go to main). Existing — "Wayveil is already installed on this server" (or for partial: "A previous installation did not finish") with buttons "Take over" (only for wayveil) and "Reinstall" + warning "Existing connection links will stop working." Refused — the `Human` text for the state and button "Back".
- [ ] **Step 1: Failing tests** `targets.test.ts` (dedupe, ordering, russian split, late recommended upgrade) and `steps.test.ts` (ordering, uninstall prefix rows first, fail freezes later rows).
- [ ] **Step 2:** `npm test` → FAIL.
- [ ] **Step 3:** Implement modules and screens; wire them into `App.svelte` through `flow`.
- [ ] **Step 4:** `npm test && npm run build` → PASS; `make ui` walks Paste → Access → Target (targets appear over time, Next enabled immediately) → Installing → Done → Main in the mock.
- [ ] **Step 5:** Commit `feat(ui): wizard screens`.

### Task 8: Wails wiring

**Files:**
- Modify: `cmd/wayveil-gui/main.go`; Create: `cmd/wayveil-gui/bridge.go` (`//go:build desktop`)

**Interfaces:**
- Consumes: `gui.NewDefault`, `gui.App` methods, Wails v2 `runtime.EventsEmit`, `runtime.OpenFileDialog`.
- Produces: bound struct `App` (package `gui`, so JS path `window.go.gui.App`) plus a tiny bound `Dialogs` struct with `PickQRFile() (string, error)` (filters `*.png;*.jpg;*.jpeg`) — frontend `api.PickQRFile` maps to `window.go.main.Dialogs.PickQRFile`. Emitter adapter wraps `runtime.EventsEmit(ctx, …)`. `OnShutdown` → `app.Shutdown()`. exeDir = `filepath.Dir(os.Executable())`; set `WAYVEIL_MIHOMO`/`WAYVEIL_XRAY` only if unset and the files exist next to the exe (existing `Find` already checks next to the exe — confirm and skip if so).
- [ ] **Step 1:** Implement.
- [ ] **Step 2:** `sg docker -c "make app app-smoke"` → binary builds, smoke passes. `go test ./...` on host still green.
- [ ] **Step 3:** Commit `feat(gui): wire App into Wails`.

### Task 9: Integration test and docs

**Files:**
- Create: `internal/gui/wizard_integration_test.go` (`//go:build integration`)
- Modify: `README.md` (section "Desktop app (Linux, preview)": `make app`, runtime package `libwebkit2gtk-4.1-0`, running `build/wayveil-gui`)

**Interfaces:**
- Consumes: `testenv.Start`, `gui.New` with real server/sshx deps but a recording Emitter and temp config/cache dirs.

- [ ] **Step 1: Tests:**
  - `TestWizardFreshInstall`: `ServerConnect` with the container's root password → State "fresh", fingerprint non-empty, `Pinned=false`; wait for ≥1 `target` event or `target-done` (≤60 s); `Install("www.microsoft.com")` → `Done.Link` starts with `vless://`; `systemctl is-active xray` = active; `ownerstore.Load` has the key; the "step" events include `verify` ok; recorded events never contain the password.
  - `TestWizardTakeOver`: after the above, a fresh `App` (new temp dirs) → `ServerConnect` → State "wayveil" → `Adopt()` → Done; `ListClients` has two "Me" clients.
  - `TestWizardReinstallAfterFailure`: build the App with a Deps.Install wrapper that calls the real `server.Install` but cancels its context when the `config` step starts (simulates a dropped connection) → `Install` returns an error with non-empty `Human`; a new `ServerConnect` → State "partial" → `Reinstall("www.microsoft.com")` → Done, xray active. No production API is added for this.
- [ ] **Step 2:** `sg docker -c "WAYVEIL_IT=1 go test -tags integration -count=1 -run TestWizard ./internal/gui/"` → PASS. Then the full suites: `go test ./...`, `cd frontend && npm test`, `sg docker -c "make it"` → all green.
- [ ] **Step 3:** README section; commit `test(gui): wizard against a real server container; docs`.
