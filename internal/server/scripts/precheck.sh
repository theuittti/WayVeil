# wayveil-script: precheck
# Inspects the server; changes nothing. Prints one ##RESULT line.
set -u
jstr() { local s=${1//\\/\\\\}; s=${s//\"/\\\"}; printf '"%s"' "$s"; }
jbool() { if [ "$1" = 1 ]; then printf true; else printf false; fi; }

os_id=""; os_ver=""
if [ -r /etc/os-release ]; then . /etc/os-release; os_id=${ID:-}; os_ver=${VERSION_ID:-}; fi
supported=0
case "$os_id:$os_ver" in ubuntu:22.04|ubuntu:24.04|debian:11|debian:12) supported=1 ;; esac

case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) arch=$(uname -m) ;; esac

busy=0; proc=""
line=$(ss -ltnpH 'sport = :443' 2>/dev/null | head -1)
if [ -n "$line" ]; then
  busy=1
  proc=$(printf '%s' "$line" | sed -n 's/.*users:(("\([^"]*\)".*/\1/p')
fi

has_wv=0; [ -f /etc/xray/wayveil.json ] && has_wv=1
foreign=0
if [ $has_wv = 0 ] && { [ -e /usr/local/bin/xray ] || [ -e /etc/systemd/system/xray.service ] || systemctl cat xray >/dev/null 2>&1; }; then foreign=1; fi

ip=$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p' | head -1)

printf '##RESULT {"osId":%s,"osVersion":%s,"arch":%s,"supported":%s,"port443Busy":%s,"port443Process":%s,"hasWayveil":%s,"hasForeignXray":%s,"publicIp":%s}\n' \
  "$(jstr "$os_id")" "$(jstr "$os_ver")" "$(jstr "$arch")" "$(jbool $supported)" "$(jbool $busy)" \
  "$(jstr "$proc")" "$(jbool $has_wv)" "$(jbool $foreign)" "$(jstr "$ip")"
