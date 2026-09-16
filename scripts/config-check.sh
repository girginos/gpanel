#!/usr/bin/env bash
# config-check — statik yapılandırma doğrulama kapısı (DB/panel GEREKTİRMEZ).
# (a) YAML parse (python yaml.safe_load): .corgea.yaml · .github/workflows/ci.yml · .golangci.yml (varsa)
# (b) JSON parse: .claude/launch.json (varsa) · frontend/package.json · tsconfig.json (JSONC toleranslı)
# (c) migrations/ dosya adı numaraları monoton-artan mı (tekrar/atlanan numara → UYARI, bloklamaz)
# (d) go.mod/go.sum tutarlılığı (go mod verify)
# Çıkış kodu: yalnız GERÇEK bozuklukta 1 (geçersiz YAML/JSON, go mod verify hatası).
#            migration boşluk/tekrarı TARİHSEL kabul edilir → UYARI (bloklamaz).
set -uo pipefail
cd "$(dirname "$0")/.."

FAIL=0
WARN=0
GO="${GO:-go}"
PY=python3
command -v "$PY" >/dev/null 2>&1 || PY=python
command -v "$PY" >/dev/null 2>&1 || { echo "🔴 python bulunamadı (YAML/JSON parse için gerekli)"; exit 2; }

# --- ortak parse yardımcısı: _parse <yaml|json|jsonc> <dosya> ---
_parse() {
  "$PY" - "$1" "$2" <<'PY'
import sys, json
mode, path = sys.argv[1], sys.argv[2]
try:
    raw = open(path, 'rb').read().decode('utf-8', 'replace')
except Exception as e:
    print("okunamadi: %s" % e); sys.exit(2)
if mode == 'yaml':
    import yaml
    yaml.safe_load(raw)
elif mode == 'json':
    json.loads(raw)
elif mode == 'jsonc':
    # tsconfig JSONC olabilir: önce katı JSON dene; olmazsa string'e saygılı
    # yorum (// ve /* */) + sondaki virgül temizliğiyle tekrar dene.
    try:
        json.loads(raw)
    except Exception:
        out = []; i = 0; n = len(raw); instr = False; esc = False; q = ''
        while i < n:
            c = raw[i]
            if instr:
                out.append(c)
                if esc: esc = False
                elif c == '\\': esc = True
                elif c == q: instr = False
                i += 1; continue
            if c == '"' or c == "'":
                instr = True; q = c; out.append(c); i += 1; continue
            if c == '/' and i + 1 < n and raw[i+1] == '/':
                i += 2
                while i < n and raw[i] not in '\r\n': i += 1
                continue
            if c == '/' and i + 1 < n and raw[i+1] == '*':
                i += 2
                while i + 1 < n and not (raw[i] == '*' and raw[i+1] == '/'): i += 1
                i += 2; continue
            out.append(c); i += 1
        import re
        s = ''.join(out)
        s = re.sub(r',(\s*[}\]])', r'\1', s)
        json.loads(s)
else:
    print("bilinmeyen mod: %s" % mode); sys.exit(2)
PY
}

_yaml() { # _yaml <etiket> <dosya> <zorunlu 1|0>
  local label="$1" f="$2" req="$3" err
  if [ ! -f "$f" ]; then
    if [ "$req" = 1 ]; then echo "  🔴 $label: DOSYA YOK ($f)"; FAIL=1
    else echo "  ⤷ $label: yok, atlandı"; fi
    return
  fi
  if err=$(_parse yaml "$f" 2>&1); then
    echo "  ✓ $label: geçerli YAML"
  else
    echo "  🔴 $label: GEÇERSİZ YAML"; printf '%s\n' "$err" | head -3 | sed 's/^/       /'; FAIL=1
  fi
}

_json() { # _json <etiket> <dosya> <zorunlu 1|0> [mod json|jsonc]
  local label="$1" f="$2" req="$3" mode="${4:-json}" err
  if [ ! -f "$f" ]; then
    if [ "$req" = 1 ]; then echo "  🔴 $label: DOSYA YOK ($f)"; FAIL=1
    else echo "  ⤷ $label: yok, atlandı"; fi
    return
  fi
  if err=$(_parse "$mode" "$f" 2>&1); then
    echo "  ✓ $label: geçerli JSON${mode:+ ($mode)}"
  else
    echo "  🔴 $label: GEÇERSİZ JSON"; printf '%s\n' "$err" | head -3 | sed 's/^/       /'; FAIL=1
  fi
}

echo "[config-check] (a) YAML doğrulama (yaml.safe_load)"
_yaml ".corgea.yaml"              ".corgea.yaml"               1
_yaml ".github/workflows/ci.yml" ".github/workflows/ci.yml"   1
if   [ -f .golangci.yml ];  then _yaml ".golangci.yml"  ".golangci.yml"  1
elif [ -f .golangci.yaml ]; then _yaml ".golangci.yaml" ".golangci.yaml" 1
else echo "  ⤷ .golangci.yml: yok, atlandı"; fi

echo "[config-check] (b) JSON doğrulama"
_json ".claude/launch.json"      ".claude/launch.json"        0 json
_json "frontend/package.json"    "frontend/package.json"      1 json
[ -f tsconfig.json ] && _json "tsconfig.json" "tsconfig.json" 1 jsonc
_json "frontend/tsconfig.json"   "frontend/tsconfig.json"     1 jsonc

echo "[config-check] (c) migrations sıra denetimi (monoton-artan?)"
if [ -d migrations ]; then
  mig=$("$PY" - <<'PY'
import os, re, glob
d = 'migrations'
nums = {}; bad = []
for f in sorted(glob.glob(os.path.join(d, '*.sql'))):
    b = os.path.basename(f)
    m = re.match(r'^(\d+)_', b)
    if not m: bad.append(b); continue
    nums.setdefault(int(m.group(1)), []).append(b)
if not nums:
    print("EMPTY"); raise SystemExit
lo, hi = min(nums), max(nums)
total = sum(len(v) for v in nums.values())
print("INFO %04d..%04d  (%d dosya, %d benzersiz numara)" % (lo, hi, total, len(nums)))
for b in bad: print("BADNAME %s" % b)
for k in sorted(nums):
    if len(nums[k]) > 1: print("DUP %04d -> %s" % (k, ", ".join(nums[k])))
gaps = [k for k in range(lo, hi+1) if k not in nums]
if gaps: print("GAP " + ", ".join("%04d" % g for g in gaps))
if not bad and not gaps and all(len(v) == 1 for v in nums.values()): print("OK")
PY
)
  printf '%s\n' "$mig" | while IFS= read -r line; do
    case "$line" in
      INFO*)    echo "  ⤷ ${line#INFO }" ;;
      OK)       echo "  ✓ sıra temiz (tekrar/boşluk yok)" ;;
      DUP*)     echo "  ⚠ TEKRAR numara: ${line#DUP }" ;;
      GAP*)     echo "  ⚠ ATLANAN numara: ${line#GAP }" ;;
      BADNAME*) echo "  ⚠ NNNN_ öneki yok: ${line#BADNAME }" ;;
      EMPTY)    echo "  ⤷ migrations boş" ;;
    esac
  done
  if printf '%s\n' "$mig" | grep -qE '^(DUP|GAP|BADNAME) '; then WARN=1; fi
else
  echo "  ⤷ migrations/ yok, atlandı"
fi

echo "[config-check] (d) go.mod/go.sum tutarlılığı (go mod verify)"
if command -v "$GO" >/dev/null 2>&1; then
  if out=$("$GO" mod verify 2>&1); then
    echo "  ✓ ${out}"
  else
    echo "  🔴 go mod verify BAŞARISIZ"; printf '%s\n' "$out" | head -5 | sed 's/^/       /'; FAIL=1
  fi
else
  echo "  🔴 '$GO' bulunamadı — go mod verify atlandı"; FAIL=1
fi

echo ""
if [ "$FAIL" != 0 ]; then
  echo "[config-check] 🔴 BAŞARISIZ — geçersiz config veya go.mod tutarsızlığı"; exit 1
fi
if [ "$WARN" != 0 ]; then
  echo "[config-check] ⚠ GEÇTİ (config sağlam) — migration sıra uyarıları var (tarihsel; bloklamaz)"; exit 0
fi
echo "[config-check] ✓ TEMİZ — tüm config geçerli, migration sırası tutarlı"; exit 0
