# -*- coding: utf-8 -*-
# E2E test jetonu — panele PAROLA GIRMEDEN API dogrulamasi icin.
# Sunucunun KENDI JWT sirriyla imzalar (root erisimi zaten var); hicbir sir
# disari cikmaz, hicbir kimlik bilgisi bir giris formuna girilmez.
import base64, hashlib, hmac, json, re, subprocess, sys, time

def b64u(b):
    return base64.urlsafe_b64encode(b).rstrip(b"=")

sir = None
try:
    for satir in open("/etc/girginospanel/env", encoding="utf-8"):
        m = re.match(r"\s*PANEL_JWT_SECRET\s*=\s*(.+?)\s*$", satir)
        if m:
            sir = m.group(1).strip().strip('"').strip("'")
            break
except OSError as e:
    sys.exit("env okunamadi: %s" % e)
if not sir:
    sys.exit("PANEL_JWT_SECRET bulunamadi")

def mysql(q):
    return subprocess.run(["mysql", "-N", "-B", "panel", "-e", q],
                          capture_output=True, text=True).stdout.strip()

satir = mysql("SELECT id, username FROM users WHERE role='admin' ORDER BY id LIMIT 1;")
if not satir:
    sys.exit("admin kullanici yok. users semasi:\n" + mysql("DESCRIBE users;"))
uid, kadi = satir.split("\t")[0], satir.split("\t")[1]

# token_gecersiz_ts iat'tan buyukse jeton reddedilir -> guvenli iat sec
gecersiz = int(mysql("SELECT COALESCE(token_gecersiz_ts,0) FROM users WHERE id=%s;" % uid) or 0)
iat = max(int(time.time()), gecersiz + 1)

basl = {"alg": "HS256", "typ": "JWT"}
govde = {"uid": int(uid), "usr": kadi, "rol": "admin",
         "iat": iat, "exp": iat + 7200, "iss": "girginospanel"}
imzasiz = b64u(json.dumps(basl, separators=(",", ":")).encode()) + b"." + \
          b64u(json.dumps(govde, separators=(",", ":")).encode())
imza = hmac.new(sir.encode(), imzasiz, hashlib.sha256).digest()
print((imzasiz + b"." + b64u(imza)).decode())
