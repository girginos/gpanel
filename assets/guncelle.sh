#!/usr/bin/env bash
# GirginOSPanel — update bootstrap
#
#   curl -fsSL https://get.girginos.io/guncelle | sudo bash
#   curl -fsSL https://get.girginos.io/guncelle | sudo bash -s -- --dry-run
#
# WHY IT EXISTS: `girginospanel-update` updates itself. If the installed version
# is broken or stale it cannot repair itself — that is exactly what happened on
# 2026-08-17: the old updater pulled from GitHub, GitHub rate-limited it (429),
# and because the updater could not download the self-fixing new version the
# servers stayed wedged. This bootstrap re-downloads the tools fresh from the
# server ON EVERY RUN, so it is independent of the state of the installed tools.
#
# 🔴 sha256 verification is MANDATORY: this script runs as root via `curl | bash`
# and installs what it downloads into /usr/local/bin. If the checksum does not
# match nothing is installed.
set -uo pipefail

# 🔴 WE SET PATH OURSELVES — `sudo` DROPS /usr/local/bin.
# AlmaLinux/RHEL default:  Defaults secure_path = /sbin:/bin:/usr/sbin:/usr/bin
# Since the documented install/update command is `curl ... | sudo bash`, this
# path works ON EVERY CUSTOMER and our own tools (girginospanel-*, composer,
# wp) give "command not found" when called bare-name. Measured: the tool existed
# as a file, it just wasn't on PATH; the install still finished green.
case ":$PATH:" in
  *:/usr/local/bin:*) : ;;
  *) export PATH="/usr/local/sbin:/usr/local/bin:$PATH" ;;
esac


TABAN=${GOSP_TABAN:-https://surum.girginos.io/panel}
c_g=$'\e[32m'; c_r=$'\e[31m'; c_y=$'\e[33m'; c_0=$'\e[0m'
bilgi(){ echo "  $*"; }
ok(){    echo -e "  ${c_g}✓${c_0} $*"; }
uyar(){  echo -e "  ${c_y}!${c_0} $*"; }
dur(){   echo -e "${c_r}✗ $*${c_0}" >&2; exit 1; }

[ "$(id -u)" = "0" ] || dur "root required:  curl -fsSL https://get.girginos.io/guncelle | sudo bash"
for k in curl tar sha256sum install; do
  command -v "$k" >/dev/null || dur "'$k' not found — install it first"
done
[ -d /opt/girginospanel ] || dur "/opt/girginospanel missing — GirginOSPanel is not installed on this server.
     To install:  curl -fsSL https://get.girginos.io | sudo bash"

echo
echo "  GirginOSPanel update bootstrap"
echo "  ──────────────────────────────"

# ── 1) Version info ─────────────────────────────────────────────────
LATEST=$(curl -fsSL --max-time 60 "$TABAN/latest.txt") \
  || dur "could not fetch version info ($TABAN/latest.txt) — check network/DNS"
PAKET=$(printf '%s' "$LATEST"  | awk -F': ' '/^paket:/{print $2}')
BEK=$(printf '%s'   "$LATEST"  | awk -F': ' '/^sha256:/{print $2}')
SURUM=$(printf '%s' "$LATEST"  | awk -F': ' '/^surum:/{print $2}')
[ -n "$PAKET" ] || dur "'paket:' missing in latest.txt"
[ -n "$BEK" ]   || dur "'sha256:' missing in latest.txt — an unverifiable package is not installed"
bilgi "published version: ${SURUM:-?}"

# ── 2) Download + VERIFY ────────────────────────────────────────────
GEC=$(mktemp -d /tmp/gosp-guncelle.XXXXXX) || dur "could not create temp dir"
trap 'rm -rf "$GEC"' EXIT
bilgi "downloading…"
curl -fsSL --max-time 900 -o "$GEC/paket.tar.gz" "$TABAN/$PAKET" || dur "download failed"
GER=$(sha256sum "$GEC/paket.tar.gz" | cut -d' ' -f1)
[ "$GER" = "$BEK" ] || dur "sha256 MISMATCH — nothing was installed
     expected: $BEK
     got     : $GER"
ok "sha256 verified"

tar xzf "$GEC/paket.tar.gz" -C "$GEC" || dur "could not extract archive"
OPS=$(find "$GEC" -maxdepth 3 -type d -name ops -path '*/assets/*' | head -1)
[ -n "$OPS" ] || dur "assets/ops missing in package (structure differs from expected)"

# ── 3) Install tools FRESH ──────────────────────────────────────────
# The state of the installed version is irrelevant: on every run we write what
# comes from the package. This permanently breaks the "tool cannot update itself" wedge.
KURULAN=0
for f in "$OPS"/*; do
  [ -f "$f" ] || continue
  bn=$(basename "$f")
  # 🔴 Leftover/backup files are NOT installed. These become STALE code and if
  # installed to /usr/local/bin with 0755 as root they could be run by accident
  # (girginospanel-repair.bak.<time> was found in the package and was being installed).
  case "$bn" in *.bak|*.bak.*|*.orig|*.rej|*~) continue ;; esac
  case "$bn" in
    *.conf|*.service|*.timer) continue ;;   # configuration files do not go here
  esac
  hedef="/usr/local/bin/${bn%.sh}"
  if ! cmp -s "$f" "$hedef" 2>/dev/null; then
    install -m 0755 "$f" "$hedef" || dur "could not install: $hedef"
    KURULAN=$((KURULAN+1))
  fi
done
# The panel helper looks for it at EXACTLY this path (see internal/portyonetim).
if [ -f "$OPS/girginospanel-port-swap.sh" ]; then
  install -D -m 0700 "$OPS/girginospanel-port-swap.sh" \
    /usr/local/sbin/girginospanel-port-swap.sh || dur "could not install port-swap"
fi
ok "ops tools refreshed ($KURULAN files updated)"

command -v girginospanel-update >/dev/null || dur "girginospanel-update could not be installed — missing from package?"

# ── 4) Run the actual update ────────────────────────────────────────
echo
bilgi "starting update…"
echo
exec girginospanel-update "$@"
