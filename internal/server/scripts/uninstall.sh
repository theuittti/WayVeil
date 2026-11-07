#!/usr/bin/env bash
# wayveil-script: uninstall
# Removes everything Wayveil installed, driven by /etc/xray/wayveil-manifest.
# Idempotent: safe to re-run after an interruption. Manual use:
#   sudo bash /etc/xray/wayveil-uninstall.sh
# Optional input: WV_AUTH_KEY (the app's key line, removed even without manifest).
set -uo pipefail
export DEBIAN_FRONTEND=noninteractive
MF=/etc/xray/wayveil-manifest
CUR=init
step() { CUR=$1; echo "##STEP $1 start"; }
ok()   { echo "##STEP $CUR ok${1:+ $1}"; }
warn() { echo "##STEP $CUR warn $1"; }

# Only these paths may ever be deleted, whatever the manifest says.
allowed_file() {
  case "$1" in
    /usr/local/bin/xray|/etc/systemd/system/xray.service|/etc/sysctl.d/99-xray-bbr.conf|/etc/logrotate.d/xray|/etc/xray/*|/var/log/xray/*) return 0 ;;
  esac
  return 1
}
allowed_dir() { case "$1" in /etc/xray|/var/log/xray) return 0 ;; esac; return 1; }

entries=()
[ -f "$MF" ] && mapfile -t entries < "$MF"
list() { local e; for e in "${entries[@]}"; do [ "${e%% *}" = "$1" ] && echo "${e#* }"; done; return 0; }
has() { local e; for e in "${entries[@]}"; do [ "$e" = "$1" ] && return 0; done; return 1; }

step service
for s in $(list service); do
  [ "$s" = xray ] || continue
  systemctl disable --now "$s" >/dev/null 2>&1 || true
done
ok

step files
while read -r f; do
  [ -n "$f" ] || continue
  if allowed_file "$f"; then rm -f -- "$f"; else warn "skipped $f"; fi
done < <(list file)
systemctl daemon-reload >/dev/null 2>&1 || true
systemctl reset-failed xray >/dev/null 2>&1 || true
while read -r d; do
  [ -n "$d" ] || continue
  [ "$d" = /etc/xray ] && continue   # removed last: it holds the manifest
  if allowed_dir "$d"; then rm -rf -- "$d"; else warn "skipped $d"; fi
done < <(list dir | awk '{print length, $0}' | sort -rn | cut -d' ' -f2-)
ok

step sysctl
removed=0
while read -r f; do
  [ -n "$f" ] || continue
  if allowed_file "$f"; then rm -f -- "$f"; removed=1; fi
done < <(list sysctl)
if [ $removed = 1 ]; then
  if sysctl --system >/dev/null 2>&1; then ok; else warn "sysctl reload not permitted"; fi
else
  ok
fi

step packages
pkgs=$(list pkg | tr '\n' ' ')
if [ -n "${pkgs// /}" ]; then
  # shellcheck disable=SC2086
  if apt-get purge -y -qq $pkgs >/dev/null 2>&1; then ok; else warn "apt-get purge failed: $pkgs"; fi
else
  ok
fi

step user
if has "user xray" && getent passwd xray >/dev/null; then
  if out=$(userdel xray 2>&1); then ok; else warn "userdel: $out"; fi
else
  ok
fi

rm -rf -- /etc/xray

step authkey
remove_line() { # file line
  local f=$1 l=$2 tmp
  case "$f" in */.ssh/authorized_keys) ;; *) return 0 ;; esac
  [ -f "$f" ] && grep -qxF -- "$l" "$f" || return 0
  tmp=$(mktemp)
  grep -vxF -- "$l" "$f" > "$tmp" || true
  cat "$tmp" > "$f"   # keep owner and mode
  rm -f "$tmp"
}
for e in "${entries[@]}"; do
  [ "${e%% *}" = line ] || continue
  rest=${e#line }
  remove_line "${rest% *}" "$(printf '%s' "${rest##* }" | base64 -d)"
done
if [ -n "${WV_AUTH_KEY:-}" ]; then
  getent passwd | cut -d: -f6 | sort -u | while read -r h; do
    remove_line "$h/.ssh/authorized_keys" "$WV_AUTH_KEY"
  done
fi
ok
echo '##RESULT {"ok":true}'
