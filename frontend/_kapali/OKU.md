# Geçici olarak kapatılan sayfalar

Host Uygulamaları (2026-08-17, kullanıcı talebi).

Geri açmak için:
1. Bu iki .tsx dosyasını `src/pages/` altına geri taşı
2. `src/App.tsx`e importları + iki Route satırını geri ekle
   (`araclar/host-uygulamalari` ve `araclar/host-uygulamalari/:id`)
3. `src/pages/AraclarAyarlarPage.tsx`e "Genel Ayarlar" grubuna menü girdisini geri ekle
4. `cmd/server/main.go` içinde `hostUygulamalariAcik = true` yap
5. `girginospanel-paketle` içindeki "Host Uygulamalari kapali" kapısını kaldır

Backend paketi (`internal/hostuyg/`) hiç silinmedi, olduğu gibi duruyor.
