#!/bin/bash
# OTOMATİK ÜRETİLDİ — ipyonetim.PersistYaz
# GirginOSPanel'in cp_server_ips tablosundaki IP'leri sunucuya geri yükler.
# Elle düzenleme; bir sonraki IP değişikliğinde üzerine yazılır.
set -u

ip addr add 203.0.113.97/24 dev pub0 label "panel-i0001" 2>/dev/null || true

exit 0
