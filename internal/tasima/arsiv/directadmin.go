package arsiv

// directadmin.go — DirectAdmin kullanıcı yedeği (backup-*.tar.gz / user.*.tar.gz).
//
// Yerleşim (kök YOK; üst dizinler doğrudan):
//   backup/user.conf            KEY=VALUE (username=, domain=, …); package/ip/reseller iddiaları OKUNMAZ
//   backup/<db>.sql, <db>.conf  döküm; .conf: "<dbkullanıcı>=<yetkiler>&passwd=<özet>" (YALNIZ ad okunur)
//   backup/<alan>/email/passwd  "<kutu>:<özet>" satırları
//   backup/crontab.conf         crontab
//   domains/<alan>/             public_html, private_html …
//   imap/<alan>/<kutu>/         maildir
// Gerçek bir DirectAdmin yedeğiyle DOĞRULANMADI (olgunluk: deneysel); okuyucu yalnız
// yukarıdaki yapıya dayanır, bilinmeyeni uyarıyla atlar. DNS bölgesi/alt alan listesi
// okunmaz (yedek biçimi doğrulanmadı) → ana alan için varsayılan bölge faz 2'de.

import (
	"context"
	"fmt"
	"strings"
)

var daTasinmayacak = []string{
	"hesap/e-posta/veritabanı kullanıcı PAROLALARI ve özetleri (yeniden belirlenir)",
	"veritabanı kullanıcı yetki ayrıntıları (<db>.conf) ve GRANT/CREATE USER ifadeleri",
	"DNS bölgeleri ve alt alan adı listesi (yedek biçimi doğrulanmadı; ana alan varsayılanı oluşturulur)",
	"e-posta yönlendiricileri, otomatik yanıtlayıcılar, posta listeleri, spam ayarları",
	"SSL sertifikaları (yeniden düzenlenir), FTP alt hesapları, kota/plan/bayi bilgisi",
	"cron satırları (yalnız bildirilir, otomatik kurulmaz)",
	"sembolik bağlar kök dışına çıkıyorsa, aygıt/FIFO düğümleri, setuid/setgid bitleri",
}

func directadminOku(ctx context.Context, a *Arsiv, o Secenek, m *Manifest) error {
	uc := anahtarDeger(a.UstVeri["backup/user.conf"])
	kul := uc["username"]
	if !reKullanici.MatchString(kul) {
		return fmt.Errorf("%w: hesap adı geçersiz", ErrHesap)
	}
	m.Kullanici = kul
	for _, iddia := range []string{"creator", "package", "ip", "usertype"} {
		if uc[iddia] != "" {
			m.uyari("yedekteki %s iddiası yok sayıldı (sahip/plan hedef panelde belirlenir)", iddia)
		}
	}
	ana := strings.ToLower(uc["domain"])
	if !AlanAdiGecerli(ana) {
		return fmt.Errorf("%w: ana alan adı geçersiz", ErrHesap)
	}
	m.AnaAlan = ana

	// Alan adları: domains/<alan> dizinleri.
	alanSet := map[string]bool{}
	for _, u := range a.Uyeler {
		rel, ok := altinda(u.Ad, "domains")
		if !ok || rel == "" || strings.Contains(rel, "/") {
			continue
		}
		ad := strings.ToLower(rel)
		if u.Tur != '5' || !AlanAdiGecerli(ad) || ad != rel {
			m.uyari("domains/ altında geçersiz alan girdisi atlandı: %q", kisalt(rel))
			continue
		}
		if len(m.Alanlar) >= maxAlan {
			m.uyari("alan adı sayısı sınırı aşıldı (%d)", maxAlan)
			break
		}
		tur := "ek"
		if ad == ana {
			tur = "ana"
		}
		al := Alan{Ad: ad, Tur: tur}
		if uyeVar(a, "domains/"+ad+"/public_html") {
			al.BelgeKoku = "domains/" + ad + "/public_html"
		} else {
			m.uyari("alan %q için public_html yedekte yok", kisalt(ad))
		}
		alanSet[ad] = true
		m.Alanlar = append(m.Alanlar, al)
	}
	if !alanSet[ana] {
		m.uyari("ana alan %q domains/ altında yok; yalnız hesap kaydı kullanılır", ana)
		alanSet[ana] = true
		m.Alanlar = append(m.Alanlar, Alan{Ad: ana, Tur: "ana"})
	}
	m.sayim(a, "domains")

	// Veritabanları: backup/<db>.sql (+ <db>.conf'tan kullanıcı adları).
	for _, u := range a.Uyeler {
		rel, ok := altinda(u.Ad, "backup")
		if !ok || rel == "" || strings.Contains(rel, "/") || !strings.HasSuffix(rel, ".sql") {
			continue
		}
		ad := strings.TrimSuffix(rel, ".sql")
		switch {
		case u.Tur != '0':
			m.uyari("veritabanı üyesi düzenli dosya değil, atlandı: %q", kisalt(rel))
		case !reDBAdi.MatchString(ad):
			m.uyari("geçersiz veritabanı adı atlandı: %q", kisalt(ad))
		case dbSistemMi(ad):
			m.uyari("sistem veritabanı atlandı: %q", kisalt(ad))
		default:
			v := dumpTara(ctx, a, o, ad, u.Ad, u.Boyut, m)
			v.Kullanicilar = daDBKullanicilari(a.UstVeri["backup/"+ad+".conf"], m)
			m.DBKullanicilari = append(m.DBKullanicilari, v.Kullanicilar...)
			m.Veritabanlari = append(m.Veritabanlari, v)
		}
	}
	if len(m.DBKullanicilari) > 0 {
		m.uyari("%d veritabanı kullanıcısı bulundu: parolalar taşınmaz, yeniden oluşturulur", len(m.DBKullanicilari))
	}

	// Posta: backup/<alan>/email/passwd + imap/<alan>/<kutu>.
	mail := maildirOzetle(a, "imap")
	for ad, b := range a.UstVeri {
		p := bol(ad)
		if len(p) == 4 && p[0] == "backup" && p[2] == "email" && p[3] == "passwd" {
			m.postaEkle(p[1], b, alanSet, mail)
		}
	}
	m.cronOku(a.UstVeri["backup/crontab.conf"])
	return nil
}

// daDBKullanicilari — "<kullanıcı>=<yetkiler>&passwd=<özet>" satırlarından YALNIZ adlar.
func daDBKullanicilari(b []byte, m *Manifest) []string {
	var out []string
	gor := map[string]bool{}
	for i, s := range strings.Split(string(b), "\n") {
		if i > 2000 {
			break
		}
		k, v, ok := strings.Cut(strings.TrimRight(s, "\r"), "=")
		if !ok || k == "accesshosts" || !strings.Contains(v, "&") {
			continue
		}
		if !reDBKul.MatchString(k) {
			m.uyari("geçersiz veritabanı kullanıcı adı atlandı: %q", kisalt(k))
			continue
		}
		if !gor[k] {
			gor[k] = true
			out = append(out, k)
		}
	}
	return out
}
