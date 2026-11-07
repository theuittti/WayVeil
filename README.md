# Wayveil

Desktop app (Windows / Linux) that sets up your own VLESS + REALITY + Vision
server over SSH and connects you and your friends with automatic split
tunnelling. **Work in progress** — this repository currently contains the
server core (Plan 1 of 3). See `docs/superpowers/specs/2026-11-07-wayveil-design.md`.

## Developer quick start

Requirements: Go ≥ 1.24, Docker (for integration tests), `make`.

```bash
make test        # unit tests (also builds the wv-scan scanner assets)
make it          # integration tests against a throwaway systemd+sshd container
go build -o build/wayveil-dev ./cmd/wayveil-dev
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

### Removing Wayveil from a server without the app

```bash
sudo bash /etc/xray/wayveil-uninstall.sh
```

## Credits

The server installer is derived from
[ndatg/xray-vless-reality-installer](https://github.com/ndatg/xray-vless-reality-installer) (MIT).
See `THIRD_PARTY_LICENSES`.
