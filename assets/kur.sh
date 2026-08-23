#!/usr/bin/env bash
# GirginOSPanel — public install bootstrap
#
#   curl -fsSL https://get.girginos.io | sudo bash
#   curl -fsSL https://get.girginos.io | sudo bash -s -- --waf
#
# What it does: downloads the published installer package, VERIFIES its sha256
# against latest.txt, extracts it and runs girginospanel-install.sh with the given args.
#
# 🔴 sha256 verification is MANDATORY: this script runs via `curl | bash`,
# so the downloaded tarball executes directly as root. If the checksum does not
# match we exit without running anything — no "downloaded it, probably fine".
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
c_g=$'\e[32m'; c_r=$'\e[31m'; c_0=$'\e[0m'
bilgi(){ echo -e "  $*"; }
dur(){ echo -e "${c_r}✗ $*${c_0}" >&2; exit 1; }

[ "$(id -u)" = "0" ] || dur "root required:  curl -fsSL https://get.girginos.io | sudo bash"

for k in curl tar sha256sum; do
  command -v "$k" >/dev/null || dur "'$k' not found — install it first"
done

echo
echo "  GirginOSPanel installer"
echo "  ───────────────────────"

# 1) Version info
LATEST=$(curl -fsSL --max-time 30 "$TABAN/latest.txt") || dur "could not fetch version info ($TABAN/latest.txt)"
PAKET=$(echo "$LATEST"  | awk -F': ' '/^paket:/{print $2}')
BEK_SHA=$(echo "$LATEST" | awk -F': ' '/^sha256:/{print $2}')
SURUM=$(echo "$LATEST"  | awk -F': ' '/^surum:/{print $2}')
[ -n "$PAKET" ]   || dur "'paket:' missing in latest.txt"
[ -n "$BEK_SHA" ] || dur "'sha256:' missing in latest.txt — an unverifiable package is not installed"
bilgi "version: ${SURUM:-?}   package: $PAKET"

# 2) Download
GEC=$(mktemp -d /tmp/gosp-kur.XXXXXX) || dur "could not create temp dir"
temizle(){ rm -rf "$GEC"; }
trap temizle EXIT
bilgi "downloading…"
curl -fSL --max-time 900 -o "$GEC/paket.tar.gz" "$TABAN/$PAKET" || dur "download failed"

# 3) VERIFY (BEFORE install)
GER_SHA=$(sha256sum "$GEC/paket.tar.gz" | cut -d' ' -f1)
if [ "$GER_SHA" != "$BEK_SHA" ]; then
  dur "sha256 MISMATCH — install aborted
     expected: $BEK_SHA
     got     : $GER_SHA"
fi
bilgi "${c_g}✓${c_0} sha256 verified"

# 4) Extract
tar xzf "$GEC/paket.tar.gz" -C "$GEC" || dur "could not extract archive"
KUR=$(find "$GEC" -maxdepth 2 -name girginospanel-install.sh -type f | head -1)
[ -n "$KUR" ] || dur "girginospanel-install.sh not found in package"
chmod +x "$KUR"

# 5) Run — pass args through verbatim
bilgi "starting install…"
echo
cd "$(dirname "$KUR")" || dur "could not enter directory"
exec "$KUR" "$@"
