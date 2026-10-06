package tasima

// arsiv_aktar.go — arşiv kaynaklı işte HesapAktar'ın adımları (dosya/DB/DNS/posta/cron).
//
// HesapAktar (aktar.go) aynen çalışır: hedef kontrolü, kapsam/plan/kota, ad-alanı denetimi
// (domains.AdAlaniDenetle, hesap oluşturulmadan ÖNCE; bayide Kapsam{RID}), sağlama, geri alma.
// Yalnız "kaynaktan çekme" adımları k.arsiv != nil iken arşivden beslenir:
//   - dosyalar: arsiv.Cikar (kiracı kimliğinde, süzülmüş akış; kiracı bütçesi = plan disk
//     kotası ∧ bayi disk havuzu kalanı);
//   - veritabanı: SQL tarayıcıdan (iki kez: ayrıştırma/TOCTOU savunması) + dumpFiltre ile
//     kısıtlı içe aktarma (DEFINER söker; bayide --sandbox zorunlu: veritabanlariniAktar);
//   - DNS: cPanel bölge dosyası (arşivin üst verisi) → zoneAyristir → mevcut dnsAktar süzgeci;
//   - posta: kutular eklentide açılır; maildir vmail kimliğinde süzülmüş akışla çıkarılır;
//   - cron: arşivden çevrilir ve panel cron deposuna eklenir (arsiv_cron.go).
// Ad çakışmaları (kullanıcı/DB adı) HesapAktar'ın mevcut sayaç ekleriyle çözülür
// (provisioner.BenzersizKullanici, benzersizHedefDB, tasimaKullanicisiSec: <ad>_2, _3 …).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"

	"girginospanel/internal/efektiflimit"
	"girginospanel/internal/tasima/arsiv"
)

// mailVmailKul — maildir çıkarma kimliği (testler ayrıcalıksız kullanıcıya çevirir).
var mailVmailKul = "vmail"

// arsivEvDisiYasak — docroot-dışı bağımlılık olarak çıkarılmayacak ev alt dizinleri
// (posta/parola özetleri/günlük/sertifika kiracıya "uygulama dizini" diye geçmesin).
var arsivEvDisiYasak = map[string]bool{"mail": true, "etc": true, "ssl": true, "logs": true, "access-logs": true, "tmp": true}

func arsivAlan(m *arsiv.Manifest, ad string) (arsiv.Alan, bool) {
	for _, a := range m.Alanlar {
		if a.Ad == ad {
			return a, true
		}
	}
	return arsiv.Alan{}, false
}

// arsivButce — yeni hesabın dosyaları için kiracı bütçesi: plan disk kotası (efektif,
// 0 = sınırsız) ile bayi disk havuzunun kalanından küçüğü. Bayi havuzu okunamazsa 0
// (boş olmayan seçime izin vermez: fail-closed).
func (h *Handlers) arsivButce(ctx context.Context, kp Kapsam, domainID int64) int64 {
	butce := arsiv.ButceSinirsiz
	var mb int64
	if err := h.DB.QueryRowContext(ctx,
		`SELECT `+efektiflimit.IfadeVarsayilan("disk_kota_mb", 0)+` FROM domains d`+efektiflimit.Join+`WHERE d.id=?`,
		domainID).Scan(&mb); err == nil && mb > 0 {
		butce = mb << 20
	}
	if kp.Bayi() {
		kalan, sinirli, err := h.bayiDiskKalan(ctx, kp.RID)
		if err != nil {
			return 0
		}
		if sinirli && (butce == arsiv.ButceSinirsiz || kalan < butce) {
			butce = kalan
		}
	}
	return butce
}

// atlananOzet — Cikar raporundaki atlanan üyelerin neden → sayı özeti (adlar arşivden gelir,
// düşmandır: günlüğe/uyarıya YAZILMAZ).
func atlananOzet(r *arsiv.Rapor) string {
	say := map[string]int{}
	for _, a := range r.Atlanan {
		say[a.Neden]++
	}
	var nedenler []string
	for n, c := range say {
		nedenler = append(nedenler, fmt.Sprintf("%s ×%d", n, c))
	}
	sort.Strings(nedenler)
	s := strings.Join(nedenler, ", ")
	if r.AtlananSayisi > len(r.Atlanan) {
		s += fmt.Sprintf(" (+%d daha)", r.AtlananSayisi-len(r.Atlanan))
	}
	return s
}

// arsivDosyaHatasi — çıkarma hatasını taşıma hatasına çevirir (kota → kalem 'atlandi').
func arsivDosyaHatasi(err error) error {
	if errors.Is(err, arsiv.ErrKota) {
		return &kotaHatasi{mesaj: "taşınan dosyalar planın/hesabın disk kotasını aşıyor — site geri alındı"}
	}
	return fmt.Errorf("dosya aktarimi: %w", err)
}

// arsivDosyalariAktar — belge kökünü (ve docroot-dışı uygulama dizinlerini) arşivden çıkarır.
func (h *Handlers) arsivDosyalariAktar(ctx context.Context, k *Kaynak, hs Hesap, ay Ayarlar, kp Kapsam,
	sk, webRoot, alanAdi string, sonuc *AktarSonuc, log func(string, ...any)) error {
	if !ay.Dosyalar {
		return nil
	}
	a, m := k.arsiv.A, k.arsiv.M
	al, ok := arsivAlan(m, alanAdi)
	if !ok {
		return fmt.Errorf("alan adı arşivde yok")
	}
	onek := m.DosyaOnek(al.BelgeKoku)
	if onek == "" || !a.Var(onek) {
		log("bu alan adinin arsivde belge koku yok — dosya tasinmadi")
		sonuc.Uyarilar = append(sonuc.Uyarilar, "Dosya yok — taşınmadı")
		return nil
	}
	ki, err := kiraciAl(sk)
	if err != nil {
		return err
	}
	kok, err := ki.Goreli(webRoot)
	if err != nil {
		return fmt.Errorf("hedef web kok dizini kiracinin ev dizininde degil")
	}
	uid, _, err := uidGid(ki.Kul)
	if err != nil {
		return fmt.Errorf("hedef sistem kullanicisi bulunamadi")
	}
	butce := h.arsivButce(ctx, kp, sonuc.DomainID)

	log("dosyalar arsivden cikariliyor → %s", webRoot)
	rap, err := a.Cikar(ctx, onek, arsiv.Hedef{SK: sk, Kok: kok, Butce: butce})
	if rap != nil && rap.TarCikti != "" {
		log("tar: %s", kisalt(temizHata(rap.TarCikti), 300))
	}
	if err != nil {
		return arsivDosyaHatasi(err)
	}
	sonuc.DosyaBayt = rap.Bayt
	if rap.AtlananSayisi > 0 {
		log("uyari: arsivden %d uye atlandi (%s)", rap.AtlananSayisi, atlananOzet(rap))
		sonuc.Uyarilar = append(sonuc.Uyarilar, fmt.Sprintf("%d dosya/bağ atlandı (güvenlik süzgeci): %s", rap.AtlananSayisi, atlananOzet(rap)))
	}
	log("dosyalar tamam (%.1f MB)", float64(sonuc.DosyaBayt)/(1024*1024))

	// Docroot-dışı bağımlılık (Laravel/Symfony): index.php'deki "../<dizin>/" başvuruları.
	kalanButce := butce
	if kalanButce != arsiv.ButceSinirsiz {
		kalanButce -= rap.Bayt
		if kalanButce < 0 {
			kalanButce = 0
		}
	}
	ustDizin := path.Dir(onek)
	for _, ust := range docrootDisiBagimlilik(ki.Ev, kok, uid) {
		if arsivEvDisiYasak[ust] {
			continue
		}
		onekUst := path.Join(ustDizin, ust)
		if !a.Var(onekUst) {
			continue
		}
		log("uygulama dizini (belge koku disi) arsivden cikariliyor: %s", ust)
		r2, err := a.Cikar(ctx, onekUst, arsiv.Hedef{SK: sk, Kok: ust, Butce: kalanButce})
		if err != nil {
			if errors.Is(err, arsiv.ErrKota) {
				return arsivDosyaHatasi(err)
			}
			log("uyari: %s cikarilamadi: %v", ust, kisalt(temizHata(err.Error()), 120))
			sonuc.Uyarilar = append(sonuc.Uyarilar, "Uygulama dizini eksik: "+ust)
			continue
		}
		sonuc.DosyaBayt += r2.Bayt
		if kalanButce != arsiv.ButceSinirsiz {
			kalanButce = max(0, kalanButce-r2.Bayt)
		}
		if r2.AtlananSayisi > 0 {
			sonuc.Uyarilar = append(sonuc.Uyarilar, fmt.Sprintf("%s: %d dosya/bağ atlandı (güvenlik süzgeci)", ust, r2.AtlananSayisi))
		}
		laravelSonrasiDuzelt(ki.Ev, ust, alanAdi, uid, log)
	}
	laravelSonrasiDuzelt(ki.Ev, kok, alanAdi, uid, log)
	return nil
}

// arsivDBAktar — arşivdeki dökümü iki geçişle (tara → aktar) hedef veritabanına yükler.
// 🔴 İlk geçiş SQL tarayıcıdır (allowlist; yönetim/dosya/istemci komutu yasak); ikinci
// geçiş içe aktarmadır ve dumpFiltre'den (DEFINER söker, istemci komutu reddi, bitiş
// damgası) geçer. Tarama atlanmış (SQLAtla) ya da reddedilmiş dökümler İÇE AKTARILMAZ.
func (h *Handlers) arsivDBAktar(ctx context.Context, k *Kaynak, kaynakDB, hedefDB string, log func(string, ...any)) error {
	b := k.arsiv
	var v *arsiv.Veritabani
	for i := range b.M.Veritabanlari {
		if b.M.Veritabanlari[i].Ad == kaynakDB {
			v = &b.M.Veritabanlari[i]
			break
		}
	}
	if v == nil {
		return fmt.Errorf("veritabani arsivde yok")
	}
	if v.Reddedildi != "" {
		return &arsiv.SQLHata{Kural: v.Reddedildi}
	}
	sinir := b.DumpSinir
	if sinir <= 0 {
		sinir = 32 << 30
	}
	rc, err := b.A.DumpAc(ctx, v.DumpYol, sinir)
	if err != nil {
		return fmt.Errorf("dump acilamadi: %w", err)
	}
	rap, err := arsiv.SQLTara(ctx, rc, arsiv.SQLSinir{ToplamBayt: sinir})
	rc.Close()
	if err != nil {
		return err
	}
	if !rap.Tamam {
		return fmt.Errorf("dump yarim kaldi (arsivde bitis damgasi yok)")
	}
	if rap.Definer > 0 {
		log("veritabani %s: %d ifadede DEFINER var (suzuluyor)", kaynakDB, rap.Definer)
	}
	rc2, err := b.A.DumpAc(ctx, v.DumpYol, sinir)
	if err != nil {
		return fmt.Errorf("dump acilamadi: %w", err)
	}
	defer rc2.Close()
	return h.dbIceAktar(ctx, io.LimitReader(rc2, sinir), hedefDB)
}

// arsivZoneOku — cPanel bölge dosyası (arşivin üst verisi). DirectAdmin'de bölge yoktur.
func arsivZoneOku(k *Kaynak, alanAdi string) []zoneKayit {
	uye := k.arsiv.M.DNSUye(alanAdi)
	if uye == "" {
		return nil
	}
	b, ok := k.arsiv.A.UstVeriAl(uye)
	if !ok {
		return nil
	}
	return zoneAyristir(string(b), alanAdi)
}

// mailDovecotDosyasi — maildir'de taşınmayanlar (mailRsyncHaric ile AYNI küme): dovecot iç
// dosyaları yeniden üretilir; sieve betikleri kaynağın denetimindeki kurallardır
// (yönlendirme/program çalıştırma) → taşınırsa hedefte vmail kimliğiyle yürürdü.
func mailDovecotDosyasi(rel string) bool {
	for _, p := range strings.Split(rel, "/") {
		if strings.HasPrefix(p, "dovecot") || p == "maildirsize" || p == ".dovecot.sieve" || p == ".dovecot.svbin" || p == "sieve" {
			return true
		}
	}
	return false
}

// arsivMailAktar — kutuları eklentide açar; maildir'i arşivden vmail kimliğinde çıkarır.
// Parolalar arşivde YOKTUR (yalnız özet): kutular yeni parolayla açılır, uyarı bırakılır.
func (h *Handlers) arsivMailAktar(ctx context.Context, k *Kaynak, hs0 Hesap, log func(string, ...any)) (int, []string, error) {
	var uyarilar []string
	alanAdi := hs0.AlanAdi
	b := k.arsiv
	var kutular []arsiv.PostaHesabi
	for _, p := range b.M.Posta {
		if p.Alan == alanAdi {
			kutular = append(kutular, p)
		}
	}
	if len(kutular) == 0 {
		return 0, []string{"Posta: arşivde kutu yok — taşınmadı"}, nil
	}
	mh, err := h.mailHedefHazirla(ctx, alanAdi)
	if err != nil {
		return 0, nil, err
	}
	sayi := 0
	var domainDir string
	grup := map[string]bool{} // alan dizininden tek geçişte çıkarılacak kutular
	for _, p := range kutular {
		if !mailYerelGecerli(p.Yerel) {
			uyarilar = append(uyarilar, fmt.Sprintf("Posta: geçersiz kutu adı atlandı: %.64q", p.Yerel))
			continue
		}
		email := p.Yerel + "@" + alanAdi
		dd, hedef, err := mailHedefDizin(alanAdi, p.Yerel)
		if err != nil {
			log("Posta: %s atlandı: %v", email, err)
			uyarilar = append(uyarilar, "Posta: "+email+" hedef dizini güvenli değil — atlandı")
			continue
		}
		if !h.mailKutuOlustur(ctx, mh, email, "", &uyarilar) {
			continue
		}
		sayi++
		if !p.MaildirVar {
			continue
		}
		if err := os.MkdirAll(hedef, 0o700); err != nil {
			uyarilar = append(uyarilar, "Posta: "+email+" hedef maildir hazırlanamadı")
			continue
		}
		// Alan dizini + kutu dizini vmail:vmail (bkz. mailAktar: vmail geçemezse teslim düşer).
		_ = exec.CommandContext(ctx, "chown", mailVmailKul+":"+mailVmailKul, dd).Run()
		_ = exec.CommandContext(ctx, "chown", mailVmailKul+":"+mailVmailKul, hedef).Run()
		domainDir = dd
		onek := b.M.PostaOnek(b.A, p)
		if onek == "" {
			continue
		}
		if onek != b.M.PostaAlanOnek(alanAdi)+"/"+p.Yerel {
			// İç içe yerleşim (ör. DirectAdmin <kutu>/Maildir): kutuyu tek başına çıkar.
			rap, err := b.A.CikarDizin(ctx, onek, hedef, mailVmailKul, arsiv.ButceSinirsiz,
				func(rel string) bool { return !mailDovecotDosyasi(rel) })
			uyarilar = append(uyarilar, mailRaporUyari(email, rap, err, log)...)
			continue
		}
		grup[p.Yerel] = true
	}
	if len(grup) > 0 && domainDir != "" {
		rap, err := b.A.CikarDizin(ctx, b.M.PostaAlanOnek(alanAdi), domainDir, mailVmailKul, arsiv.ButceSinirsiz,
			func(rel string) bool {
				kutu, kalan, _ := strings.Cut(rel, "/")
				return grup[kutu] && !mailDovecotDosyasi(kalan)
			})
		uyarilar = append(uyarilar, mailRaporUyari(alanAdi, rap, err, log)...)
	}
	uyarilar = append(uyarilar, "Posta: kutu parolaları arşivde bulunmuyor — yeni parola belirlendi; kullanıcılar panelden parolayı sıfırlamalı")
	log("Posta: %d kutu arşivden işlendi", sayi)
	return sayi, uyarilar, nil
}

// mailRaporUyari — maildir çıkarma sonucunu kalem uyarısına çevirir (ham hata günlüğe).
func mailRaporUyari(ad string, rap *arsiv.Rapor, err error, log func(string, ...any)) []string {
	var u []string
	if err != nil {
		log("Posta: %s maildir cikarma hatasi: %v", ad, kisalt(temizHata(err.Error()), 200))
		u = append(u, "Posta: "+ad+" mesajları kısmen/hiç taşınamadı")
	}
	if rap != nil && rap.AtlananSayisi > 0 {
		// Dovecot/sieve süzgeci beklenen atlamadır; asıl sayı yine de bildirilir.
		u = append(u, fmt.Sprintf("Posta: %s — %d girdi atlandı (güvenlik/dovecot süzgeci)", ad, rap.AtlananSayisi))
	}
	return u
}

// arsivCronAktar — arşivden çevrilen cron satırlarını hedef hesabın panel cron deposuna
// ekler (mevcut cron altyapısı: kiracı kimliği + kaynak dilimi; idempotent). Çevrilemeyen
// ya da reddedilen her satır NEDENİYLE kalem uyarısında raporlanır (sessiz atma yok).
func (h *Handlers) arsivCronAktar(k *Kaynak, alanAdi, sk string, sonuc *AktarSonuc, log func(string, ...any)) {
	b := k.arsiv
	satirlar := b.CronAtama[alanAdi]
	var redler []cronRed
	if !b.cronRaporlandi {
		b.cronRaporlandi = true
		redler = append(redler, b.CronRed...)
	}
	if len(satirlar) == 0 && len(redler) == 0 {
		return
	}
	eklenen, yinelenen := 0, 0
	var notlar []string
	if len(satirlar) > 0 {
		e, y, r, n, err := cronKur(sk, satirlar, b.M.Kullanici)
		if err != nil {
			log("uyari: cron eklenemedi: %v", kisalt(temizHata(err.Error()), 160))
			redler = append(redler, cronRed{Satir: fmt.Sprintf("(%d satır)", len(satirlar)), Neden: "hesabın cron deposu şu an yazılamıyor (askıda olabilir)"})
		} else {
			eklenen, yinelenen = e, y
			redler = append(redler, r...)
			notlar = n
		}
	}
	sonuc.Uyarilar = append(sonuc.Uyarilar, cronRaporMetni(eklenen, yinelenen, redler)...)
	if b.cronNot == nil {
		b.cronNot = map[string]bool{}
	}
	for _, n := range notlar {
		if !b.cronNot[n] {
			b.cronNot[n] = true
			sonuc.Uyarilar = append(sonuc.Uyarilar, "Cron: "+n)
		}
	}
	log("Cron: %d eklendi, %d zaten vardi, %d tasinamadi", eklenen, yinelenen, len(redler))
}

// arsivIsHazirla — iş başlarken arşivi açar (kapsam + parmak izi + disk önkontrolü yeniden).
// Seçilen her site arşivde bulunmalı. ay.Cron ise satırlar alan adlarına atanır; seçilmeyen
// alana düşenler CronRed'e (ilk kalemin raporu) eklenir.
func (h *Handlers) arsivIsHazirla(ctx context.Context, kp Kapsam, k *Kaynak, ay Ayarlar, hesaplar []Hesap) error {
	g := k.arsivGirdi
	if g == nil {
		return fmt.Errorf("arşiv kaynağı iş kaydında yok")
	}
	bilgi, err := h.arsivKaynakCoz(ctx, kp, g.Yol, g.YuklemeID)
	if err != nil {
		return err
	}
	if !bilgi.parmakIziEslesir(g.ParmakIzi) {
		return errArsivDegisti
	}
	if err := arsivDiskOnkontrol(bilgi.Boyut); err != nil {
		return err
	}
	a, m, err := arsivAc(ctx, bilgi.Yol, bilgi.Secenek)
	if err != nil {
		return err
	}
	if err := arsivTuruUyar(k.Tip, m); err != nil {
		return err
	}
	// Okuma süresince dosya değiştiyse sonuç güvenilmez.
	if yeni, err := h.arsivKaynakCoz(ctx, kp, g.Yol, g.YuklemeID); err != nil || !yeni.parmakIziEslesir(g.ParmakIzi) {
		return errArsivDegisti
	}
	for _, hs := range hesaplar {
		if _, ok := arsivAlan(m, hs.AlanAdi); !ok {
			return fmt.Errorf("seçilen alan adı arşivde yok")
		}
	}
	b := &arsivBaglam{A: a, M: m, Yol: bilgi.Yol, Boyut: bilgi.Boyut, DumpSinir: bilgi.Secenek.DumpSinir}
	if ay.Cron {
		atama, red := cronAta(m.Cron, cronBaglam{EskiKullanici: m.Kullanici, AnaAlan: m.AnaAlan, Alanlar: m.Alanlar})
		secili := map[string]bool{}
		for _, hs := range hesaplar {
			secili[hs.AlanAdi] = true
		}
		alanlar := make([]string, 0, len(atama))
		for al := range atama {
			alanlar = append(alanlar, al)
		}
		sort.Strings(alanlar)
		for _, al := range alanlar {
			if !secili[al] {
				for _, s := range atama[al] {
					red = append(red, cronRed{Satir: cronTemizle(s), Neden: "ait olduğu alan adı (" + kisalt(al, 80) + ") bu taşımada seçilmedi"})
				}
				delete(atama, al)
			}
		}
		b.CronAtama, b.CronRed = atama, red
	}
	k.arsiv = b
	return nil
}
