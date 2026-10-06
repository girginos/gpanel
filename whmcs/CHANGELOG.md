# Değişiklik günlüğü — GirginOSPanel WHMCS modülü

## 1.0.0
İlk sürüm (WHMCS 8.x, PHP 8.1+).

- Sağlama: CreateAccount (idempotent), SuspendAccount, UnsuspendAccount, TerminateAccount,
  ChangePackage, ChangePassword, UsageUpdate, TestConnection.
- Tek tıkla panele giriş: ServiceSingleSignOn + AdminSingleSignOn (`POST /api/v1/domains/{id}/sso`),
  uç yoksa/izinsizse müşteri alanında düz panel bağlantısına geri düşme.
- Bayi (reseller) ürünü: bayi hesabı oluştur/askı/aç/sil, paket değiştir, kullanım.
- Müşteri alanı: hesap bilgisi, disk/trafik çubukları, parola değiştirme formu, kullanım yenileme.
- Yönetici hizmet sekmesi: panel hesap kimliği (doğrulamalı elle bağlama).
- Güvenlik: varsayılan TLS doğrulaması, anahtar/parola/SSO kodu log maskeleme, başka müşterinin
  hesabını devralmaya karşı "mevcut hesabı bağla" kapısı, istekten serviceid okunmaz.
- Testler: `php whmcs/tests/run.php` (sahte panel + WHMCS saplamaları), `bash whmcs/tests/mutasyon.sh`.
