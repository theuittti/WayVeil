# Wayveil

Desktop app (Windows / Linux) that sets up your own VLESS + REALITY + Vision
server over SSH and connects you and your friends with automatic split
tunnelling. **Work in progress** — this repository contains the server core
(Plan 1) and the Linux client engine (Plan 2); the GUI comes in Plan 3.
See `docs/superpowers/specs/2026-11-07-wayveil-design.md`.

## Developer quick start

Requirements: Go ≥ 1.24, Docker (for integration tests), `make`.

```bash
make build       # build/wayveil, build/wayveil-dev + pinned mihomo and xray binaries
make test        # unit tests
make it          # integration tests: throwaway server + TUN client containers
make e2e         # end-to-end on your disposable test VPS (see below)
```

### `wayveil-dev` — server core from the terminal

```bash
wayveil-dev install -host 203.0.113.10 -sni www.nu.nl      # asks SSH password, confirms host key
wayveil-dev targets -host 203.0.113.10                      # suggested REALITY targets
wayveil-dev check-target -host 203.0.113.10 -domain example.com
wayveil-dev clients list -host 203.0.113.10
wayveil-dev clients add "Вася" -host 203.0.113.10           # prints the vless:// link
wayveil-dev clients rm c-1a2b3c4d -host 203.0.113.10
wayveil-dev clients reissue c-1a2b3c4d -host 203.0.113.10
wayveil-dev set-sni -host 203.0.113.10 -domain www.bol.com
wayveil-dev uninstall -host 203.0.113.10                    # removes everything Wayveil installed
```

The SSH password is used once and never stored: the tool authorises its own
ed25519 key and saves a profile (host, pinned host key, private key) under
`~/.config/wayveil/servers/` (mode 0600).

### `wayveil` — the Linux client

```bash
wayveil import 'vless://…'        # or: wayveil import qr.png
wayveil connect                   # asks once for permission to create the TUN interface
wayveil status
wayveil mode smart|all            # smart: only blocked-in-RU sites via VPN; all: everything
wayveil rules proxy example.org   # always via VPN
wayveil rules direct bank.ru      # always direct
wayveil rules list | rm <domain>
wayveil lists update              # block lists also refresh daily on connect
wayveil disconnect
```

How it works: [mihomo](https://github.com/MetaCubeX/mihomo) owns the TUN
interface, the routing rules and DNS (fake-ip: blocked names are never
resolved locally; direct ones go to encrypted DoH). Traffic that must go
through the VPN is handed to a local [xray](https://github.com/XTLS/Xray-core)
client, which speaks VLESS + REALITY + Vision to your server — the same
implementation as the server, so the handshake always matches.
Block lists: [itdoginfo/allow-domains](https://github.com/itdoginfo/allow-domains)
(`russia_inside` plus Telegram, Meta, Twitter, Discord IP ranges); a copy is
bundled, so the first connect works even if GitHub is unreachable.
Set `WAYVEIL_LOG_LEVEL=info` to log every routing decision to
`~/.cache/wayveil/run/mihomo.log`.

### End-to-end test on a real VPS

Create `~/.config/wayveil-test/vps.env` (mode 0600, outside the repo):

```
WV_TEST_HOST=203.0.113.10
WV_TEST_PORT=22
WV_TEST_USER=root
WV_TEST_PASS=…
```

`make e2e` wipes every trace of earlier Wayveil attempts from that server
(including the packages the installer adds, `jq` and `unzip`, and the apt
lists, so the install pays what a fresh VPS pays; set `WV_TEST_KEEP_DEPS=1`
to keep them), installs Wayveil, connects a TUN client container and checks
that proxied sites see the VPS IP and direct ones see yours. It ends with a
per-phase timing table. **Use a disposable server only.** The password is used
once to authorize a test key (`~/.config/wayveil-test/id_ed25519`); remove
`WV_TEST_PASS` afterwards.

### Removing Wayveil from a server without the app

```bash
sudo bash /etc/xray/wayveil-uninstall.sh
```

## Credits

The server installer is derived from
[ndatg/xray-vless-reality-installer](https://github.com/ndatg/xray-vless-reality-installer) (MIT).
See `THIRD_PARTY_LICENSES`.
