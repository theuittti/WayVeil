# wayveil-script: install
# Non-interactive VLESS + REALITY + Vision install. Derived from
# ndatg/xray-vless-reality-installer (MIT), see THIRD_PARTY_LICENSES.
# Inputs: WV_ADDRESS WV_SNI WV_DNS WV_AUTH_USER WV_AUTH_KEY WV_XRAY_VERSION
#         WV_XRAY_SHA256_64 WV_XRAY_SHA256_ARM64 (official digests of the zip)
#         WV_UNINSTALLER (base64 of uninstall.sh)
# Test hooks: WV_FAIL_AT=<step>, WV_APT_LOCK_TIMEOUT=<seconds>
# Every created item is recorded in the manifest *before* it is created.
# The uninstaller and the app key go first, so a failed install can always
# be cleaned up; /etc/xray/wayveil-installing marks an unfinished install.
set -Eeuo pipefail
export DEBIAN_FRONTEND=noninteractive
umask 022

MF=/etc/xray/wayveil-manifest
LOG=$(mktemp /tmp/wayveil-install.XXXXXX)
CUR=init
step() {
  CUR=$1; echo "##STEP $1 start"
  if [ "${WV_FAIL_AT:-}" = "$1" ]; then echo "test hook: failing at $1" >&2; false; fi
}
APT_OPTS=(-o "DPkg::Lock::Timeout=${WV_APT_LOCK_TIMEOUT:-300}")
# apt-get update takes the lists lock, which DPkg::Lock::Timeout does not cover
apt_update() { local i; for i in 1 2 3 4 5 6 7 8 9 10; do apt-get update -qq "${APT_OPTS[@]}" >/dev/null && return 0; sleep 15; done; return 1; }
ok()   { echo "##STEP $CUR ok${1:+ $1}"; }
warn() { echo "##STEP $CUR warn $1"; }
mf()   { echo "$*" >> "$MF"; }
fail() {
  local rc=$?
  echo "##STEP $CUR fail"
  echo "##ERROR install_failed step $CUR failed (exit $rc): $(tail -n 3 "$LOG" 2>/dev/null | tr '\n' ' ')"
  exit "$rc"
}
trap fail ERR
exec 2>>"$LOG"

if [ -f "$MF" ] || [ -f /etc/xray/wayveil.json ]; then echo "##ERROR already_installed"; exit 1; fi

if [ ! -d /etc/xray ]; then
  install -d -m 0750 /etc/xray
  mf dir /etc/xray        # only directories we create are ours to delete
fi
mf file /etc/xray/wayveil-installing
: > /etc/xray/wayveil-installing

# ---- uninstaller ----
step uninstaller
mf file /etc/xray/wayveil-uninstall.sh
printf '%s' "$WV_UNINSTALLER" | base64 -d > /etc/xray/wayveil-uninstall.sh
chmod 0700 /etc/xray/wayveil-uninstall.sh
ok

# ---- authkey ----
step authkey
H=$(getent passwd "$WV_AUTH_USER" | cut -d: -f6)
G=$(id -gn "$WV_AUTH_USER")
[ -n "$H" ]
install -d -m 0700 -o "$WV_AUTH_USER" -g "$G" "$H/.ssh"
AK="$H/.ssh/authorized_keys"
if ! grep -qxF "$WV_AUTH_KEY" "$AK" 2>/dev/null; then
  mf line "$AK" "$(printf '%s' "$WV_AUTH_KEY" | base64 -w0)"
  # never glue our key onto a last line that lacks a newline
  if [ -s "$AK" ] && [ -n "$(tail -c 1 "$AK")" ]; then echo >> "$AK"; fi
  printf '%s\n' "$WV_AUTH_KEY" >> "$AK"
fi
chown "$WV_AUTH_USER:$G" "$AK"
chmod 0600 "$AK"
ok

# ---- deps ----
step deps
installed_pkgs() { dpkg-query -W -f='${db:Status-Abbrev} ${Package}\n' | awk '$1=="ii"{print $2}' | sort; }
need=()
for p in curl unzip jq logrotate; do dpkg -s "$p" >/dev/null 2>&1 || need+=("$p"); done
if [ ${#need[@]} -gt 0 ]; then
  before=$(installed_pkgs)
  for p in "${need[@]}"; do mf pkg "$p"; done   # recorded first in case apt is interrupted
  apt_update
  apt-get install -y -qq --no-install-recommends "${APT_OPTS[@]}" "${need[@]}" >/dev/null
  # also record dependencies apt pulled in, so uninstall restores the exact package set
  comm -13 <(echo "$before") <(installed_pkgs) | while read -r p; do
    case " ${need[*]} " in *" $p "*) ;; *) mf pkg "$p" ;; esac
  done
fi
ok

# ---- xray ----
step xray
case "$(uname -m)" in
  x86_64) A=64; SUM=$WV_XRAY_SHA256_64 ;;
  aarch64|arm64) A=arm64-v8a; SUM=$WV_XRAY_SHA256_ARM64 ;;
  *) echo "unsupported arch $(uname -m)" >&2; false ;;
esac
T=$(mktemp -d)
curl -fsSL --retry 5 --retry-all-errors --retry-delay 3 --connect-timeout 10 --max-time 300 \
  "https://github.com/XTLS/Xray-core/releases/download/${WV_XRAY_VERSION}/Xray-linux-${A}.zip" -o "$T/x.zip"
echo "$SUM  $T/x.zip" | sha256sum -c --quiet - || { echo "xray download does not match the official SHA-256" >&2; false; }
unzip -qo "$T/x.zip" xray -d "$T"
mf file /usr/local/bin/xray
install -m 0755 "$T/xray" /usr/local/bin/xray
rm -rf "$T"
ok

# ---- keys ----
step keys
KO=$(/usr/local/bin/xray x25519)
PRIV=$(awk '/PrivateKey/{print $NF}' <<<"$KO")
[ -n "$PRIV" ] || PRIV=$(awk '/Private key:/{print $3}' <<<"$KO")
PUB=$(awk '/Password/{print $NF}' <<<"$KO")
[ -n "$PUB" ] || PUB=$(awk '/Public key:/{print $3}' <<<"$KO")
[ -n "$PRIV" ] && [ -n "$PUB" ]
SID=$(head -c 4 /dev/urandom | od -An -tx1 | tr -d ' \n')
mf file /etc/xray/wayveil.json
jq -n --arg a "$WV_ADDRESS" --arg s "$WV_SNI" --arg p "$PUB" --arg i "$SID" --arg v "$WV_XRAY_VERSION" \
  '{schema:1, address:$a, sni:$s, publicKey:$p, shortId:$i, xrayVersion:$v, clients:{}}' > /etc/xray/wayveil.json
chmod 0600 /etc/xray/wayveil.json
ok

# ---- config ----
step config
if ! getent passwd xray >/dev/null; then
  mf user xray
  useradd --system --no-create-home --shell /usr/sbin/nologin xray
fi
[ -d /var/log/xray ] || mf dir /var/log/xray
install -d -m 0750 -o xray -g xray /var/log/xray
# create the log as xray: `xray run -test` below runs as root and would otherwise own it
install -m 0640 -o xray -g xray /dev/null /var/log/xray/access.log
# IPv6 only when the server really has it; otherwise xray would pick AAAA
# records at random and some connections would fail ("works every other time").
if curl -6 -fsS --max-time 5 -o /dev/null https://www.cloudflare.com/cdn-cgi/trace 2>/dev/null; then
  STRAT=ForceIP; QSTRAT=UseIP
else
  STRAT=ForceIPv4; QSTRAT=UseIPv4
fi
mf file /etc/xray/config.json
jq -n --arg strat "$STRAT" --arg qstrat "$QSTRAT" --arg sni "$WV_SNI" --arg priv "$PRIV" --arg sid "$SID" --arg dns "$WV_DNS" '{
  log: {loglevel: "warning", access: "/var/log/xray/access.log"},
  dns: {servers: [$dns], tag: "dns-internal", queryStrategy: $qstrat},
  api: {tag: "api", services: ["StatsService"]},
  stats: {},
  policy: {levels: {"0": {statsUserUplink: true, statsUserDownlink: true}}},
  inbounds: [
    {tag: "vless-in", port: 443, listen: "0.0.0.0", protocol: "vless",
     settings: {clients: [], decryption: "none"},
     streamSettings: {network: "tcp", security: "reality",
       realitySettings: {show: false, target: ($sni + ":443"), xver: 0,
         serverNames: [$sni], privateKey: $priv, shortIds: [$sid]}},
     sniffing: {enabled: true, destOverride: ["http", "tls", "quic"], routeOnly: true}},
    {tag: "api", listen: "127.0.0.1", port: 10085, protocol: "dokodemo-door", settings: {address: "127.0.0.1"}}
  ],
  routing: {domainStrategy: "IPIfNonMatch", rules: [
    {type: "field", inboundTag: ["api"], outboundTag: "api"},
    {type: "field", inboundTag: ["dns-internal"], outboundTag: "direct"},
    {type: "field", ip: ["0.0.0.0/8","10.0.0.0/8","100.64.0.0/10","127.0.0.0/8","169.254.0.0/16",
                         "172.16.0.0/12","192.168.0.0/16","::1/128","fc00::/7","fe80::/10"], outboundTag: "block"}
  ]},
  outbounds: [
    {tag: "direct", protocol: "freedom", streamSettings: {sockopt: {domainStrategy: $strat}}},
    {tag: "block", protocol: "blackhole"}
  ]
}' > /etc/xray/config.json
chown root:xray /etc/xray /etc/xray/config.json
chmod 0640 /etc/xray/config.json
/usr/local/bin/xray run -test -config /etc/xray/config.json >/dev/null
ok

# ---- service ----
step service
mf file /etc/systemd/system/xray.service
mf service xray
cat > /etc/systemd/system/xray.service <<'UNIT'
[Unit]
Description=Xray Service (VLESS + REALITY, managed by Wayveil)
Documentation=https://github.com/XTLS/Xray-core
After=network.target nss-lookup.target

[Service]
Type=simple
User=xray
Group=xray
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_BIND_SERVICE
ExecStart=/usr/local/bin/xray run -config /etc/xray/config.json
Restart=on-failure
LimitNOFILE=51200

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now xray >/dev/null 2>&1
sleep 2
systemctl is-active --quiet xray
[ "$(systemctl show -p NRestarts --value xray)" = 0 ] || { journalctl -u xray -n 3 --no-pager >&2; false; }
ok

# ---- bbr (best effort: not permitted on OpenVZ/LXC/containers) ----
step bbr
mf sysctl /etc/sysctl.d/99-xray-bbr.conf
printf 'net.core.default_qdisc=fq\nnet.ipv4.tcp_congestion_control=bbr\n' > /etc/sysctl.d/99-xray-bbr.conf
if sysctl -p /etc/sysctl.d/99-xray-bbr.conf >/dev/null 2>&1; then ok; else warn "sysctl not permitted"; fi

# ---- logrotate ----
step logrotate
mf file /etc/logrotate.d/xray
cat > /etc/logrotate.d/xray <<'ROT'
/var/log/xray/*.log {
    daily
    rotate 7
    missingok
    notifempty
    copytruncate
    compress
    delaycompress
}
ROT
ok

# ---- verify ----
step verify
systemctl is-active --quiet xray
[ -n "$(ss -ltnH 'sport = :443')" ]
ok
rm -f "$LOG" /etc/xray/wayveil-installing
printf '##RESULT {"publicKey":"%s","shortId":"%s","xrayVersion":"%s"}\n' "$PUB" "$SID" "$WV_XRAY_VERSION"
