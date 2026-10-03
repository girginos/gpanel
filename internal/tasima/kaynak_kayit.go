package tasima

// kaynak_kayit.go — taşıma KAYNAK KAYIT DEFTERİ (tek kaynak).
//
// Kaynak türleri (Plesk, GirginOSPanel, cPanel, DirectAdmin …) YALNIZ burada
// tanımlanır; doğrulama (Dogrula), keşif, sahip keşfi, dump kimliği, DNS okuma,
// posta keşfi ve kaynağa özel sertifika okuma kayıttaki sürücüden çalışır. Ön yüz
// listeyi GET /tasima/kaynaklar ile sunucudan alır: kök yeni kaynak açınca bayi
// aynı listeyi derleme olmadan görür, kökün kapattığı kaynak kimseye gitmez.
//
// Durum (kökün anahtarı) DB'de: tasima_kaynak_ayar(kod, etkin). Satır yoksa
// VarsayilanEtkin geçerlidir (doğrulanmış kaynak açık, deneysel kapalı).
//
// 🔴 Uzak komutlar KODDA kalır (sabit şablon ilkesi, tasima.go başı); DB'deki tek
// bilgi bir açık/kapalı bitidir — DB yazma yetkisi uzak kabuk komutuna dönüşmez.

import (
	"context"
	"fmt"
	"sort"
)

// Yetenek — kaynağın taşıyabildiği bileşenler (ön yüz anahtarları buradan çizer).
type Yetenek struct {
	Dosya      bool `json:"dosya"`
	Veritabani bool `json:"veritabani"`
	DNS        bool `json:"dns"`
	SSL        bool `json:"ssl"`
	Posta      bool `json:"posta"`
	Sahipler   bool `json:"sahipler"`
}

// Olgunluk değerleri.
const (
	olgunlukDogrulandi = "dogrulandi" // gerçek sunucuda uçtan uca denendi
	olgunlukDeneysel   = "deneysel"   // kodda var, gerçek sunucuda doğrulanmadı
)

// kaynakSurucu — bir kaynak türünün tanımı. Fonksiyon alanları nil olabilir
// (yetenek yok / ortak yol kullanılır).
type kaynakSurucu struct {
	Kod             string
	Etiket          string
	Olgunluk        string
	Sira            int // listeleme sırası (küçük önce)
	VarsayilanEtkin bool
	Yetenekler      Yetenek

	// kesif — kaynaktaki siteleri listeler (uzak çıktı DÜŞMANDIR: ayrıştırıcı süzer).
	kesif func(ctx context.Context, k *Kaynak) ([]Hesap, error)
	// sahipKomutu — reseller/müşteri keşif komutu ("" = desteklenmez).
	sahipKomutu string
	// dumpKimlikOnek / dumpKul — kaynak MySQL kimliği (mysqldump önüne eklenir).
	dumpKimlikOnek string
	dumpKul        string
	// dnsOnce — zone dosyasından ÖNCE denenen kaynak deposu (Plesk). nil = yok.
	dnsOnce func(ctx context.Context, k *Kaynak, alanAdi string) []zoneKayit
	// dnsKomutu — BIND zone dosyası okuma komutu (q = shQuote(alan)). nil = ortak liste.
	dnsKomutu func(q string) string
	// postaKesfet — domainin gerçek kutuları + her kutunun uzak maildir yolu. nil =
	// posta desteklenmez.
	postaKesfet func(ctx context.Context, k *Kaynak, hs Hesap) ([]mailHesap, error)
	// sertifikaOku — kaynağa özel sertifika deposu (cPanel apache_tls, DA kullanıcı
	// dizini). Boş dönerse ortak nginx taraması (kaynakCertOku) denenir.
	sertifikaOku func(ctx context.Context, k *Kaynak, hs Hesap) (cert, key string, err error)
}

// kaynakKayit — kod → sürücü. init() sırasında doldurulur; çalışma anında salt okunur.
var kaynakKayit = map[string]*kaynakSurucu{}

// kaynakKaydet — sürücüyü deftere ekler. Aynı kod iki kez kaydedilemez (paket
// başlatılırken panik: kayıt hatası derleme sonrası ilk testte yakalanır).
func kaynakKaydet(s *kaynakSurucu) {
	if s == nil || s.Kod == "" || s.kesif == nil {
		panic("tasima: eksik kaynak sürücüsü")
	}
	if _, var_ := kaynakKayit[s.Kod]; var_ {
		panic("tasima: kaynak kodu iki kez kaydedildi: " + s.Kod)
	}
	if s.Olgunluk != olgunlukDogrulandi && s.Olgunluk != olgunlukDeneysel {
		panic("tasima: geçersiz olgunluk: " + s.Kod)
	}
	kaynakKayit[s.Kod] = s
}

// surucuBul — kaynak türünün sürücüsü (bilinmeyen kod → false).
func surucuBul(kod string) (*kaynakSurucu, bool) {
	s, ok := kaynakKayit[kod]
	return s, ok
}

// kaynakSurucuSirali — tüm sürücüler, listeleme sırasıyla.
func kaynakSurucuSirali() []*kaynakSurucu {
	out := make([]*kaynakSurucu, 0, len(kaynakKayit))
	for _, s := range kaynakKayit {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sira != out[j].Sira {
			return out[i].Sira < out[j].Sira
		}
		return out[i].Kod < out[j].Kod
	})
	return out
}

// errDesteklenmeyenTip — kayıtta olmayan kaynak türü.
var errDesteklenmeyenTip = fmt.Errorf("desteklenmeyen panel tipi")

// ---------------------------------------------------------------------------
// Kayıtlar
// ---------------------------------------------------------------------------

func init() {
	kaynakKaydet(&kaynakSurucu{
		Kod: "plesk", Etiket: "Plesk", Olgunluk: olgunlukDogrulandi, Sira: 10, VarsayilanEtkin: true,
		Yetenekler: Yetenek{Dosya: true, Veritabani: true, DNS: true, SSL: true, Posta: true, Sahipler: true},
		kesif: func(ctx context.Context, k *Kaynak) ([]Hesap, error) {
			return k.ayristir(k.Calistir(ctx, komutPlesk))
		},
		sahipKomutu:    komutPleskSahipler,
		dumpKimlikOnek: `MYSQL_PWD="$(cat /etc/psa/.psa.shadow 2>/dev/null)" `,
		dumpKul:        "-uadmin ",
		dnsOnce: func(ctx context.Context, k *Kaynak, alanAdi string) []zoneKayit {
			if ham, err := k.Calistir(ctx, "plesk bin dns --info "+shQuote(alanAdi)+" 2>/dev/null"); err == nil {
				return pleskDNSAyristir(ham, alanAdi)
			}
			return nil
		},
		postaKesfet: func(ctx context.Context, k *Kaynak, hs Hesap) ([]mailHesap, error) {
			return pleskMailKesfet(ctx, k, hs.AlanAdi)
		},
	})
	kaynakKaydet(&kaynakSurucu{
		Kod: "gpanel", Etiket: "GirginOSPanel", Olgunluk: olgunlukDogrulandi, Sira: 20, VarsayilanEtkin: true,
		// Posta: kaynak gPanel'in kutu parolaları kaynakta şifreli ve eklenti DB'sinde;
		// bu yol henüz yazılmadı → yetenek kapalı (adım çalışmaz, uyarı bırakır).
		Yetenekler: Yetenek{Dosya: true, Veritabani: true, DNS: true, SSL: true, Sahipler: true},
		kesif: func(ctx context.Context, k *Kaynak) ([]Hesap, error) {
			return k.ayristir(k.Calistir(ctx, komutGpanel))
		},
		sahipKomutu: komutGpanelSahipler,
	})
	kaynakKaydet(&kaynakSurucu{
		Kod: "cpanel", Etiket: "cPanel", Olgunluk: olgunlukDeneysel, Sira: 30, VarsayilanEtkin: false,
		Yetenekler:   Yetenek{Dosya: true, Veritabani: true, DNS: true, SSL: true, Posta: true},
		kesif:        cpanelKesfet,
		postaKesfet:  cpanelMailKesfet,
		sertifikaOku: cpanelCertOku,
	})
	kaynakKaydet(&kaynakSurucu{
		Kod: "directadmin", Etiket: "DirectAdmin", Olgunluk: olgunlukDeneysel, Sira: 40, VarsayilanEtkin: false,
		Yetenekler: Yetenek{Dosya: true, Veritabani: true, DNS: true, SSL: true, Posta: true},
		kesif: func(ctx context.Context, k *Kaynak) ([]Hesap, error) {
			return k.ayristir(k.Calistir(ctx, komutDirectAdmin))
		},
		dumpKimlikOnek: daMySQLKimlikOnek,
		dumpKul:        daMySQLKul,
		dnsKomutu:      daZoneKomutu,
		postaKesfet:    daMailKesfet,
		sertifikaOku:   daCertOku,
	})
}
