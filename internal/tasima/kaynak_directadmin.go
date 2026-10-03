package tasima

// kaynak_directadmin.go — DirectAdmin kaynak bağdaştırıcısı (canlı SSH, root).
// Keşif komutu kesif.go'da (komutDirectAdmin); burada dump kimliği, zone yolu,
// posta ve sertifika okuma.
//
// Doğrulanmalı (gerçek DA sunucusunda denenmedi — olgunluk "deneysel"):
//   - Posta: /etc/virtual/<alan>/passwd ilk alanı kutu adı; maildir
//     ~/imap/<alan>/<kutu>/Maildir/.
//   - SSL: /usr/local/directadmin/data/users/<u>/domains/<alan>.cert|.key.
//   - Zone: /var/named/<alan>.db (RHEL) ya da /etc/bind/<alan>.db (Debian).

import (
	"context"
	"fmt"
)

// DA MySQL kimliği (dump): /usr/local/directadmin/conf/mysql.conf. Parola
// $(sed …) ile alınır — ps'te parolanın kendisi görünmez.
const (
	daMySQLKimlikOnek = `MYSQL_PWD="$(sed -n 's/^passwd=//p' /usr/local/directadmin/conf/mysql.conf 2>/dev/null)" `
	daMySQLKul        = `-u"$(sed -n 's/^user=//p' /usr/local/directadmin/conf/mysql.conf 2>/dev/null)" `
)

// daZoneKomutu — ortak BIND yollarına ek olarak Debian DA düzeni (/etc/bind/<alan>.db).
func daZoneKomutu(q string) string {
	return ortakZoneKomutu(q) + " || cat /etc/bind/" + q + ".db 2>/dev/null"
}

// daMailKesfet — /etc/virtual/<alan>/passwd'deki kutular. Parolalar crypt özeti →
// kutulara yeni parola üretilir.
func daMailKesfet(ctx context.Context, k *Kaynak, hs Hesap) ([]mailHesap, error) {
	if !reAlanAdi.MatchString(hs.AlanAdi) || !reHesap.MatchString(hs.KaynakHesap) {
		return nil, fmt.Errorf("gecersiz hesap/alan adi")
	}
	uzak := uzakEvKomutu(hs.KaynakHesap) + "d=" + shQuote(hs.AlanAdi) + "\n" +
		`[ -f "/etc/virtual/$d/passwd" ] && cut -d: -f1 "/etc/virtual/$d/passwd" 2>/dev/null | head -n 2000`
	cikti, err := k.Calistir(ctx, uzak)
	if err != nil {
		return nil, err
	}
	ev, adlar := uzakEvAyristir(cikti)
	if ev == "" {
		return nil, fmt.Errorf("kaynak hesabin ev dizini okunamadi")
	}
	alan := hs.AlanAdi
	var out []mailHesap
	gorulen := map[string]bool{}
	for _, y := range adlar {
		if gorulen[y] {
			continue
		}
		gorulen[y] = true
		out = append(out, mailHesap{Yerel: y, uzakYol: func(yerel string) string {
			return ev + "/imap/" + alan + "/" + yerel + "/Maildir/"
		}})
	}
	return out, nil
}

// daCertOku — kullanıcının alan adı sertifikası (.cert + .key).
func daCertOku(ctx context.Context, k *Kaynak, hs Hesap) (string, string, error) {
	if !reAlanAdi.MatchString(hs.AlanAdi) || !reHesap.MatchString(hs.KaynakHesap) {
		return "", "", fmt.Errorf("gecersiz hesap/alan adi")
	}
	uzak := "u=" + shQuote(hs.KaynakHesap) + "\nd=" + shQuote(hs.AlanAdi) + "\n" +
		`b="/usr/local/directadmin/data/users/$u/domains/$d"
if [ -f "$b.cert" ] && [ -f "$b.key" ]; then head -c 262144 "$b.cert"; printf '\n'; head -c 65536 "$b.key"; fi`
	cikti, err := k.Calistir(ctx, uzak)
	if err != nil {
		return "", "", err
	}
	cert, key := pemAyir(cikti)
	return cert, key, nil
}
