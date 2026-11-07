#!/usr/bin/env bash
# wayveil-script: uninstall
# Removes everything Wayveil installed, driven by /etc/xray/wayveil-manifest.
# Idempotent: safe to re-run after an interruption. Manual use:
#   sudo bash /etc/xray/wayveil-uninstall.sh
# Optional input: WV_AUTH_KEY (the app's key line, removed even without manifest).
# Test hook: WV_APT_LOCK_TIMEOUT=<seconds> (default 300).
set -uo pipefail
export DEBIAN_FRONTEND=noninteractive
MF=/etc/xray/wayveil-manifest
UNI=/etc/xray/wayveil-uninstall.sh
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
has()  { local e; for e in "${entries[@]}"; do [ "$e" = "$1" ] && return 0; done; return 1; }
inset() { local x=$1 y; shift; for y in "$@"; do [ "$x" = "$y" ] && return 0; done; return 1; }

step service
for s in $(list service); do
  [ "$s" = xray ] || continue
  systemctl disable --now "$s" >/dev/null 2>&1 || true
done
ok

step files
while read -r f; do
  [ -n "$f" ] || continue
  [ "$f" = "$UNI" ] && continue      # removed at the very end
  if allowed_file "$f"; then rm -f -- "$f"; else warn "skipped $f"; fi
done < <(list file)
rm -rf -- /root/.cache/wayveil      # target scanner, if a session was interrupted
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

# Purge only packages whose removal takes nothing but other recorded
# packages with it: a library we installed may since have gained
# dependents the owner relies on. Those are kept (and forgotten).
step packages
pending=()
kept=()
cand=()
for p in $(list pkg); do
  st=$(dpkg-query -W -f='${db:Status-Abbrev}' "$p" 2>/dev/null)
  [[ $st == ii* ]] && cand+=("$p")
done
removes() { apt-get -s purge "$@" 2>/dev/null | awk '$1=="Purg"||$1=="Remv"{print $2}'; }
changed=1
while [ $changed = 1 ] && [ ${#cand[@]} -gt 0 ]; do
  changed=0
  for p in "${cand[@]}"; do
    for x in $(removes "$p"); do
      if ! inset "$x" "${cand[@]}"; then
        kept+=("$p")
        next=(); for q in "${cand[@]}"; do [ "$q" = "$p" ] || next+=("$q"); done
        cand=("${next[@]}")
        changed=1
        break 2
      fi
    done
  done
done
if [ ${#cand[@]} -gt 0 ]; then
  apt-get purge -y -qq -o "DPkg::Lock::Timeout=${WV_APT_LOCK_TIMEOUT:-300}" "${cand[@]}" >/dev/null 2>&1 || pending=("${cand[@]}")
fi
msg=""
[ ${#kept[@]} -gt 0 ] && msg="kept ${kept[*]} (other packages need them)"
[ ${#pending[@]} -gt 0 ] && msg="${msg:+$msg; }apt is busy, run uninstall again later to remove: ${pending[*]}"
if [ -n "$msg" ]; then warn "$msg"; else ok; fi

step user
if has "user xray" && getent passwd xray >/dev/null; then
  if out=$(userdel xray 2>&1); then ok; else warn "userdel: $out"; fi
else
  ok
fi

if [ ${#pending[@]} -gt 0 ]; then
  # Keep a residual manifest, the uninstaller and the app key, so a rerun
  # (by the app or by hand) can finish the job.
  {
    has "dir /etc/xray" && echo "dir /etc/xray"
    echo "file $UNI"
    for p in "${pending[@]}"; do echo "pkg $p"; done
  } > "$MF.tmp" && mv "$MF.tmp" "$MF"
  step authkey
  warn "app key kept until packages are removed"
  echo '##RESULT {"ok":true,"pending":true}'
  exit 0
fi

if has "dir /etc/xray"; then
  rm -rf -- /etc/xray
else
  rm -f -- "$MF" "$UNI"   # /etc/xray predates Wayveil: leave everything else
fi

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
