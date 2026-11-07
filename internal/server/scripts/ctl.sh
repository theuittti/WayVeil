# wayveil-script: ctl
# Client management on a Wayveil server. Usage: ctl.sh <command> [args]
#   info | list | add <base64 name> | remove <id> | reissue <id> | stats | lastseen | set-sni <domain>
# Prints ##RESULT <json> or ##ERROR <code> <message>.
set -Eeuo pipefail
XRAY=/usr/local/bin/xray
CFG=/etc/xray/config.json
WV=/etc/xray/wayveil.json
TR=/etc/xray/wayveil-traffic.json
NCFG=/etc/xray/config.new.json   # xray picks the config format from the extension
NWV=/etc/xray/wayveil.new.json
err() { echo "##ERROR $1 ${2:-}"; exit 1; }
trap 'err ctl_failed "line $LINENO"' ERR
[ -f "$WV" ] || err not_installed
exec 9>/run/wayveil.lock
flock -w 30 9 || err busy

result() { printf '##RESULT %s\n' "$(jq -c . <<<"$1")"; }
rand_id() { echo "c-$(head -c 4 /dev/urandom | od -An -tx1 | tr -d ' \n')"; }

# Add xray's counters to the persistent totals and reset them, so totals
# survive restarts. Must run before every restart.
stats_flush() {
  local q
  q=$("$XRAY" api statsquery --server=127.0.0.1:10085 -reset 2>/dev/null) || q='{}'
  [ -n "$q" ] || q='{}'
  [ -f "$TR" ] || echo '{}' > "$TR"
  jq --argjson q "$q" '
    reduce (($q.stat // [])[] | select(.name | startswith("user>>>"))) as $s (.;
      ($s.name | split(">>>")) as $p
      | .[$p[1]][(if $p[3] == "uplink" then "up" else "down" end)] += ($s.value // 0 | tonumber))
    | map_values({up: (.up // 0), down: (.down // 0)})' "$TR" > "$TR.tmp"
  mv "$TR.tmp" "$TR"
  chmod 0600 "$TR"
}

# apply <new config> <new wayveil.json>: validate, swap atomically, restart.
apply() {
  local out
  out=$("$XRAY" run -test -config "$1" 2>&1) || { rm -f "$1" "$2"; err invalid_config "$(tail -n 2 <<<"$out" | tr '\n' ' ')"; }
  chown root:xray "$1"; chmod 0640 "$1"; chmod 0600 "$2"
  mv "$1" "$CFG"
  mv "$2" "$WV"
  systemctl restart xray
}

exists() { jq -e --arg id "$1" '.clients[$id] != null' "$WV" >/dev/null || err no_such_client "$1"; }

client_json() { # id
  jq -c --arg id "$1" --slurpfile c "$CFG" '
    {client: {id: $id, name: .clients[$id].name, created: .clients[$id].created,
      uuid: ($c[0].inbounds[] | select(.tag == "vless-in") | .settings.clients[] | select(.email == $id) | .id)}}' "$WV"
}

cmd=${1:-}
case "$cmd" in
info)
  result "$(jq '{address, sni, publicKey, shortId, xrayVersion}' "$WV")" ;;
list)
  result "$(jq --slurpfile c "$CFG" '. as $w | {clients: [
      $c[0].inbounds[] | select(.tag == "vless-in") | .settings.clients[]
      | {id: .email, uuid: .id, name: ($w.clients[.email].name // .email),
         created: ($w.clients[.email].created // "1970-01-01T00:00:00Z")}]}' "$WV")" ;;
add)
  name=$(printf '%s' "${2:?}" | base64 -d)
  id=$(rand_id); uuid=$(cat /proc/sys/kernel/random/uuid); now=$(date -u +%FT%TZ)
  stats_flush
  jq --arg id "$id" --arg u "$uuid" '(.inbounds[] | select(.tag == "vless-in") | .settings.clients) += [{id: $u, email: $id, flow: "xtls-rprx-vision", level: 0}]' "$CFG" > "$NCFG"
  jq --arg id "$id" --arg n "$name" --arg t "$now" '.clients[$id] = {name: $n, created: $t}' "$WV" > "$NWV"
  apply "$NCFG" "$NWV"
  result "$(client_json "$id")" ;;
remove)
  id=${2:?}; exists "$id"
  stats_flush
  jq --arg id "$id" '(.inbounds[] | select(.tag == "vless-in") | .settings.clients) |= map(select(.email != $id))' "$CFG" > "$NCFG"
  jq --arg id "$id" 'del(.clients[$id])' "$WV" > "$NWV"
  apply "$NCFG" "$NWV"
  jq --arg id "$id" 'del(.[$id])' "$TR" > "$TR.tmp" && mv "$TR.tmp" "$TR"
  result '{"ok":true}' ;;
reissue)
  id=${2:?}; exists "$id"
  uuid=$(cat /proc/sys/kernel/random/uuid)
  stats_flush
  jq --arg id "$id" --arg u "$uuid" '(.inbounds[] | select(.tag == "vless-in") | .settings.clients[] | select(.email == $id) | .id) = $u' "$CFG" > "$NCFG"
  cp "$WV" "$NWV"
  apply "$NCFG" "$NWV"
  result "$(client_json "$id")" ;;
stats)
  stats_flush
  result "$(jq '{traffic: .}' "$TR")" ;;
lastseen)
  # Access log lines: "2026/11/07 16:45:32.123456 from [tcp:]IP:PORT accepted ... email: ID".
  # Files are read oldest first, so the last line per ID wins.
  files=$(ls -tr /var/log/xray/access.log* 2>/dev/null || true)
  tsv=$(for f in $files; do zcat -f "$f"; done | awk '
    / accepted / && / email: / {
      ip = ""
      for (i = 1; i < NF; i++) if ($i == "from") { ip = $(i + 1); break }
      sub(/^(tcp|udp):/, "", ip); sub(/:[0-9]+$/, "", ip); gsub(/[][]/, "", ip)
      t = $2; sub(/\..*/, "", t); d = $1; gsub("/", "-", d)
      last[$NF] = d " " t "\t" ip
    }
    END { for (id in last) print id "\t" last[id] }')
  json='{}'
  while IFS=$'\t' read -r id ts ip; do
    [ -n "$id" ] || continue
    at=$(date -u -d "$ts" +%FT%TZ)
    json=$(jq --arg id "$id" --arg ip "$ip" --arg at "$at" '.[$id] = {ip: $ip, at: $at}' <<<"$json")
  done <<<"$tsv"
  result "$(jq '{lastSeen: .}' <<<"$json")" ;;
set-sni)
  d=${2:?}
  stats_flush
  jq --arg d "$d" '(.inbounds[] | select(.tag == "vless-in") | .streamSettings.realitySettings) |= (.target = ($d + ":443") | .serverNames = [$d])' "$CFG" > "$NCFG"
  jq --arg d "$d" '.sni = $d' "$WV" > "$NWV"
  apply "$NCFG" "$NWV"
  result '{"ok":true}' ;;
*)
  err bad_command "$cmd" ;;
esac
