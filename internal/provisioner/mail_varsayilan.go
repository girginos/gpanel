package provisioner

// mail_varsayilan.go — Mail sunucusunun VARSAYILAN (SNI-siz) TLS sertifikası.
//
// 🔴 SORUN (üretimde ölçüldü): mail eklentisi per-domain sertifikayı SNI ile
// sunuyor (mail.<d> → Let's Encrypt). Ama istemci TLS el sıkışmasında SNI
// GÖNDERMEZSE (Outlook sık sık göndermez, bazı Android istemcileri hiç) veya
// sunucuya farklı bir adla bağlanırsa, dovecot/postfix VARSAYILAN sertifikayı
// sunar — ve o, kurulumda üretilen KENDİNDEN İMZALI sertifikadır. İstemci
// güvenmez, bağlantıyı kabul etmez ve KULLANICIYA SÜREKLİ ŞİFRE SORAR.
//
// ÇÖZÜM: ilk geçerli LE mail sertifikası kurulduğunda, varsayılan
// /etc/pki/mail/mail.crt+key hâlâ self-signed ise onu da bu LE ile değiştir.
// Böylece SNI gönderen de göndermeyen de geçerli bir sertifika görür.

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"os/exec"
	"time"
)

const (
	mailVarsayilanCrt = "/etc/pki/mail/mail.crt"
	mailVarsayilanKey = "/etc/pki/mail/mail.key"
)

// MailVarsayilanKur — varsayılan mail sertifikası HÂLÂ self-signed ise, verilen
// geçerli LE cert+key ile değiştirir ve dovecot+postfix'i reload eder.
// Döndürdüğü bool: değişiklik yapıldı mı. Zaten geçerli (CA imzalı) bir
// varsayılan varsa dokunmaz (false, nil) — böylece çok-domainli sunucuda her
// mail SSL kurulumu varsayılanı gereksiz yere ezmez.
func MailVarsayilanKur(certPath, keyPath string) (bool, error) {
	if !mailVarsayilanSelfSigned() {
		return false, nil // varsayılan zaten geçerli — dokunma
	}
	cert, err := os.ReadFile(certPath)
	if err != nil {
		return false, err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return false, err
	}
	// Kaynak da GEÇERLİ (CA imzalı + süresi dolmamış) olmalı: self-signed bir
	// kaynağı varsayılan yapmak Outlook'u yine reddettirir. Kurulum akışında
	// panel host LE henüz alınmamışsa bu dal self-signed'ı korur.
	if !certGecerliLE(cert) {
		return false, nil
	}
	// Yedekle (ilk sefer), sonra değiştir.
	if _, e := os.Stat(mailVarsayilanCrt + ".selfsigned.yedek"); e != nil {
		if b, e2 := os.ReadFile(mailVarsayilanCrt); e2 == nil {
			_ = os.WriteFile(mailVarsayilanCrt+".selfsigned.yedek", b, 0o644)
		}
		if b, e2 := os.ReadFile(mailVarsayilanKey); e2 == nil {
			_ = os.WriteFile(mailVarsayilanKey+".selfsigned.yedek", b, 0o600)
		}
	}
	if err := os.WriteFile(mailVarsayilanCrt, cert, 0o644); err != nil {
		return false, err
	}
	if err := os.WriteFile(mailVarsayilanKey, key, 0o600); err != nil {
		return false, err
	}
	// Reload (restart değil — kuyruk/oturum kesintisiz). Eklenti servisleri
	// yönetir ama varsayılan cert dosyaları core'un sorumluluğunda; reload'u
	// da core tetikler.
	_, _ = exec.Command("systemctl", "reload", "dovecot").CombinedOutput()
	if o, e := exec.Command("systemctl", "reload", "postfix").CombinedOutput(); e != nil {
		_, _ = exec.Command("systemctl", "restart", "postfix").CombinedOutput()
		_ = o
	}
	return true, nil
}

// mailVarsayilanSelfSigned — /etc/pki/mail/mail.crt kendinden imzalı mı
// (issuer == subject) ya da hiç yok mu? Yoksa "değiştirilmeli" sayılır.
func mailVarsayilanSelfSigned() bool {
	b, err := os.ReadFile(mailVarsayilanCrt)
	if err != nil {
		return true // dosya yok → geçerli LE ile doldurulmalı
	}
	blok, _ := pem.Decode(b)
	if blok == nil {
		return true
	}
	c, err := x509.ParseCertificate(blok.Bytes)
	if err != nil {
		return true
	}
	// Süresi geçmiş de "geçersiz" sayılır (yenilenmeli).
	if time.Now().After(c.NotAfter) {
		return true
	}
	return c.Issuer.CommonName == c.Subject.CommonName
}

// certGecerliLE — PEM cert CA-imzalı (issuer!=subject) VE süresi dolmamış mı?
func certGecerliLE(pemBytes []byte) bool {
	blok, _ := pem.Decode(pemBytes)
	if blok == nil {
		return false
	}
	c, err := x509.ParseCertificate(blok.Bytes)
	if err != nil {
		return false
	}
	if time.Now().After(c.NotAfter) {
		return false
	}
	return c.Issuer.CommonName != c.Subject.CommonName
}
