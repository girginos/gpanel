package denetim

import (
	"encoding/json"
	"strings"
	"testing"

	"girginospanel/internal/nginxset"
)

// notJSON: httpx.denetimYaz'ın yazdığı biçim — {"not": "<metin>"}.
func notJSON(s string) string {
	b, _ := json.Marshal(map[string]string{"not": s})
	return string(b)
}

// TestAciklamaKur: her bilinen biçim + bilinmeyen + boş + bozuk girdi.
// ayr: beklenen genişletme ayrıntıları "Etiket=değer" (TR) sırayla.
func TestAciklamaKur(t *testing.T) {
	type c struct {
		ad, eylem, detail string
		tr, en            string
		ayr               []string
		yok               bool // nil beklenir
	}
	n := notJSON
	for _, x := range []c{
		{ad: "sayaç tam", eylem: "hosting.toplu_durum", detail: n("istenen=3 uygulanan=3 atlanan=0"),
			tr: "3 işlemden 3'ü uygulandı", en: "3 of 3 applied"},
		{ad: "sayaç atlanan", eylem: "hosting.toplu_durum", detail: n("istenen=3 uygulanan=2 atlanan=1"),
			tr: "3 işlemden 2'si uygulandı, 1 atlandı", en: "2 of 3 applied, 1 skipped"},
		{ad: "sayaç onlar", eylem: "hosting.toplu_durum", detail: n("istenen=40 uygulanan=40 atlanan=0"),
			tr: "40 işlemden 40'ı uygulandı", en: "40 of 40 applied"},
		{ad: "websec sırada", eylem: "websec.tara", detail: n("sirada=2"),
			tr: "2 alan adı tarama sırasına alındı", en: "2 domains queued for scanning"},
		{ad: "websec sırada 0", eylem: "websec.tara", detail: n("sirada=0"),
			tr: "Sıraya yeni alan adı alınmadı", en: "No new domain was queued"},
		{ad: "bayi güncelle", eylem: "bayi.guncelle",
			detail: n("paket 1→2, paketle gelen: domain=10 disk=2048MB trafik=0MB asim=disk_trafik fazla_satis=0, max_disk_mb 100→200"),
			tr:     "Değişen: paket (#1 → #2), paketle gelen sınırlar (alan adı sınırı 10, disk kotası 2 GB, trafik kotası sınırsız, aşım ilkesi disk ve trafik, fazla satış kapalı), disk kotası (100 MB → 200 MB)",
			en:     "Changed: package (#1 → #2), package limits (domain limit 10, disk quota 2 GB, traffic quota unlimited, overage policy disk and traffic, overselling off), disk quota (100 MB → 200 MB)"},
		{ad: "bayi durum", eylem: "bayi.guncelle", detail: n("durum active→suspended, max_domain 5→0"),
			tr: "Değişen: durum (etkin → askıda), alan adı sınırı (5 → sınırsız)",
			en: "Changed: status (active → suspended), domain limit (5 → unlimited)"},
		{ad: "bayi güncelle serbest parça", eylem: "bayi.guncelle", detail: n("ad, e-posta"),
			tr: "Değişen: ad, e-posta", en: "Changed: ad, e-posta"},
		{ad: "bayi oluştur", eylem: "bayi.olustur", detail: n("paket=3 domain=10 disk=1000MB"),
			tr: "Paket: #3 · Alan adı sınırı: 10 · Disk kotası: 1000 MB",
			en: "Package: #3 · Domain limit: 10 · Disk quota: 1000 MB"},
		{ad: "bayi askı", eylem: "bayi.askiya_al", detail: n("etkilenen hosting=4 basarisiz=1"),
			tr: "4 hosting hesabı etkilendi, 1'i uygulanamadı", en: "4 hosting accounts affected, 1 failed"},
		{ad: "bayi sil red", eylem: "bayi.sil", detail: n("reddedildi: 2 hosting hesabı var"),
			tr: "Reddedildi: bayinin 2 hosting hesabı var", en: "Rejected: reseller still has 2 hosting accounts"},
		{ad: "bayi sil id teknik", eylem: "bayi.sil", detail: n("silinen bayi id=17"),
			ayr: []string{"Bayi kimliği=17"}},
		{ad: "plan", eylem: "hosting.olustur", detail: n("plan=4"), tr: "Plan: #4", en: "Plan: #4"},
		{ad: "plan yok", eylem: "hosting.olustur", detail: n("plan=0"), tr: "Plan: yok", en: "Plan: none"},
		{ad: "yeni plan", eylem: "hosting.plan", detail: n("yeni plan=7"), tr: "Yeni plan: #7", en: "New plan: #7"},
		{ad: "db kullanıcı", eylem: "db.olustur", detail: n("kullanici=site_wp"), tr: "Kullanıcı: site_wp", en: "User: site_wp"},
		{ad: "sistem kullanıcısı teknik", eylem: "hosting.sil", detail: n("sistem_kullanici=gs_ornek"),
			ayr: []string{"Sistem kullanıcısı=gs_ornek"}},
		{ad: "geri yükle", eylem: "yedek.geriyukle", detail: n("mod=tam dosya=/home/yedek/a b.tar.zst"),
			tr: "Tam yedek", en: "Full backup", ayr: []string{"Dosya=/home/yedek/a b.tar.zst"}},
		{ad: "ip değiş", eylem: "domain.ip_degistir", detail: n("1.1.1.1 → 2.2.2.2 (A:2 SPF:1)"),
			tr: "1.1.1.1 → 2.2.2.2 · 3 DNS kaydı güncellendi", en: "1.1.1.1 → 2.2.2.2 · 3 DNS records updated",
			ayr: []string{"DNS kayıtları=A: 2, SPF: 1"}},
		{ad: "ip toplu hata", eylem: "domain.ip_degistir", detail: n("toplu #12: 1.1.1.1 → 2.2.2.2 (A:0 SPF:0): nginx -t failed"),
			tr: "Toplu işlemle · 1.1.1.1 → 2.2.2.2 · Uygulanamadı", en: "Bulk action · 1.1.1.1 → 2.2.2.2 · Failed",
			ayr: []string{"Toplu iş=#12", "Hata=nginx -t failed"}},
		{ad: "sahip devri", eylem: "hosting.sahip_devir", detail: n("bayi ana hesap→#5, müşteri #3→değişmedi, ftp parolası döndürüldü=true"),
			tr: "Bayi: ana hesap → #5 · FTP parolası yenilendi", en: "Reseller: main account → #5 · FTP password rotated"},
		{ad: "taşıma", eylem: "tasima_baslat", detail: n("3 site"), tr: "3 site taşınıyor", en: "3 sites being migrated"},
		{ad: "erişim ayar", eylem: "erisim.ayar", detail: n("aktif=true varsayilan=red"),
			tr: "Erişim kısıtlaması açık · Kurala uymayanlar engellenir", en: "Access restriction on · Unmatched visitors are blocked"},
		{ad: "alt alan erişim ayar", eylem: "altalan.erisim.ayar", detail: n("aktif=false varsayilan=izin"),
			tr: "Erişim kısıtlaması kapalı · Kurala uymayanlar izinli", en: "Access restriction off · Unmatched visitors are allowed"},
		{ad: "kural izin IP", eylem: "erisim.kural.ekle", detail: n("izin 1.2.3.4"),
			tr: "İzin verilen IP eklendi: 1.2.3.4", en: "Allowed IP added: 1.2.3.4"},
		{ad: "kural red aralık", eylem: "altalan.erisim.kural.ekle", detail: n("red 10.0.0.0/8"),
			tr: "Engellenen IP aralığı eklendi: 10.0.0.0/8", en: "Blocked IP range added: 10.0.0.0/8"},
		{ad: "kural geri alındı", eylem: "erisim.kural.ekle", detail: n("render basarisiz, GERI ALINDI: izin 1.2.3.4"),
			tr: "Uygulanamadı, eklenmedi: izin verilen IP 1.2.3.4", en: "Could not be applied, not added: allowed IP 1.2.3.4"},
		{ad: "kural sil teknik", eylem: "erisim.kural.sil", detail: n("kural_id=9"), ayr: []string{"Kural kimliği=9"}},
		{ad: "kural sil geri", eylem: "altalan.erisim.kural.sil", detail: n("render basarisiz, GERI ALINDI: kural_id=9"),
			tr: "Uygulanamadı, kural geri yüklendi", en: "Could not be applied, rule restored", ayr: []string{"Kural kimliği=9"}},
		{ad: "ayar geri genel hata", eylem: "erisim.ayar", detail: n("render basarisiz, GERI ALINDI: " + sunucuHatasiGenel),
			tr: "Uygulanamadı, önceki ayar geri yüklendi: Sunucu yapılandırması uygulanamadı; ayrıntı sunucu günlüğünde",
			en: "Could not be applied, previous setting restored: Server configuration could not be applied; details are in the server log"},
		{ad: "ayar geri ham hata", eylem: "erisim.ayar", detail: n("render basarisiz, GERI ALINDI: exit status 1"),
			tr: "Uygulanamadı, önceki ayar geri yüklendi", en: "Could not be applied, previous setting restored", ayr: []string{"Hata=exit status 1"}},
		{ad: "vekil", eylem: "guvenilir_vekil.ekle", detail: n("aralik_sayisi=3"),
			tr: "Toplam 3 güvenilir vekil aralığı", en: "3 trusted proxy ranges in total"},
		{ad: "port", eylem: "kapi.rol_port", detail: n("8443 → 9443"), tr: "Port: 8443 → 9443", en: "Port: 8443 → 9443"},
		{ad: "port uzat", eylem: "kapi.eski_uzat", detail: n("1 gün"), tr: "1 gün uzatıldı", en: "Extended by 1 day"},
		{ad: "port zorla", eylem: "kapi.eski_kapat", detail: n("zorla (admin yeni kapıyı doğrulamadan)"),
			tr: "Zorla kapatıldı (yeni port doğrulanmadan)", en: "Forced (new port not verified)"},
		{ad: "win ajan", eylem: "winajan.ekle", detail: n("windows ajanı eklendi (10.0.0.5:8765, sürüm=1.4.2, parmak izi=ab12cd34ef56ab78)"),
			tr: "Adres: 10.0.0.5:8765 · Sürüm: 1.4.2", en: "Address: 10.0.0.5:8765 · Version: 1.4.2", ayr: []string{"Parmak izi=ab12cd34ef56ab78"}},
		{ad: "toplu işlem sabit", eylem: "hosting.askiya_al", detail: n("toplu işlem"), tr: "Toplu işlemle yapıldı", en: "Done as part of a bulk action"},
		{ad: "admin onayı sabit", eylem: "hosting.sil.uzak-yedek-birakildi", detail: n("uzak yedekler BIRAKILDI (admin onayı)"),
			tr: "Yönetici onayıyla", en: "With admin approval"},
		{ad: "izolasyon json", eylem: "guvenlik.izolasyon_kaybi",
			detail: `{"sistem_kullanici":"gs_x","domain_id":42,"sebep":"unit başlamadı"}`,
			tr:     "Hosting paylaşılan PHP havuzuna düştü; komşu hesaplardan yalıtımı zayıfladı. Sorun giderilince planı yeniden uygulayın.",
			en:     "Hosting fell back to the shared PHP pool; its isolation from neighbouring accounts is weakened. Re-apply the plan once the cause is fixed.",
			ayr:    []string{"Hosting kimliği=42", "Sebep=unit başlamadı", "Sistem kullanıcısı=gs_x"}},
		{ad: "bilinmeyen anahtar", eylem: "yeni.eylem", detail: n("ftp_parola_suresi=30 is=7"),
			tr: "Ftp parola suresi: 30", en: "Ftp parola suresi: 30", ayr: []string{"İş=7"}},
		{ad: "bilinmeyen json", eylem: "yeni.eylem", detail: `{"yeni_alan":true}`, tr: "Yeni alan: evet", en: "Yeni alan: yes"},
		{ad: "serbest metin", eylem: "kapi.rol_port", detail: n("port 80 kullanımda"), tr: "Port 80 kullanımda", en: "Port 80 kullanımda"},
		{ad: "boş", eylem: "hosting.sil", detail: "", yok: true},
		{ad: "boş not", eylem: "hosting.sil", detail: n("   "), yok: true},
		{ad: "etiketi tekrarlayan", eylem: "winajan.sil", detail: n("windows ajanı silindi"), yok: true},
		{ad: "bozuk json", eylem: "hosting.olustur", detail: `{"not": "kırık`, tr: `{"not": "kırık`, en: `{"not": "kırık`},
		{ad: "bozuk sayaç", eylem: "hosting.toplu_durum", detail: n("istenen=x uygulanan= atlanan"),
			tr: "0 işlemden 0'ı uygulandı", en: "0 of 0 applied"},
		{ad: "bozuk ip", eylem: "domain.ip_degistir", detail: n("→ (A: SPF:)"), tr: "→ (A: SPF:)", en: "→ (A: SPF:)"},
	} {
		t.Run(x.ad, func(t *testing.T) {
			a := aciklamaKur(x.eylem, x.detail)
			if x.yok {
				if a != nil {
					t.Fatalf("nil beklenirdi: %+v", a)
				}
				return
			}
			if a == nil {
				t.Fatalf("açıklama beklenirdi, nil geldi")
			}
			if a.TR != x.tr || a.EN != x.en {
				t.Errorf("\n TR=%q\nbek=%q\n EN=%q\nbek=%q", a.TR, x.tr, a.EN, x.en)
			}
			var ayr []string
			for _, y := range a.Ayrinti {
				ayr = append(ayr, y.TR+"="+y.Deger)
				if y.EN == "" {
					t.Errorf("ayrıntının EN etiketi boş: %+v", y)
				}
			}
			if strings.Join(ayr, "|") != strings.Join(x.ayr, "|") {
				t.Errorf("ayrıntı %q, beklenen %q", ayr, x.ayr)
			}
			// Teknik ayrıntı (dosya yolu, iş/kural kimliği, parmak izi) cümlede GÖRÜNMEZ.
			for _, y := range a.Ayrinti {
				if y.Deger != "" && len(y.Deger) > 3 && (strings.Contains(a.TR, y.Deger) || strings.Contains(a.EN, y.Deger)) {
					t.Errorf("teknik ayrıntı cümlede görünüyor: %q in %q", y.Deger, a.TR)
				}
			}
		})
	}
}

func TestSayiIyelik(t *testing.T) {
	for n, bek := range map[int]string{0: "'ı", 1: "'i", 2: "'si", 3: "'ü", 4: "'ü", 5: "'i", 6: "'sı", 7: "'si", 8: "'i", 9: "'u",
		10: "'u", 20: "'si", 30: "'u", 40: "'ı", 50: "'si", 60: "'ı", 70: "'i", 80: "'i", 90: "'ı", 100: "'ü", 1000: "'i",
		23: "'ü", 110: "'u", 2000000: "'u", 3000000000: "'ı"} {
		if g := sayiIyelik(n); g != bek {
			t.Errorf("sayiIyelik(%d) = %q, beklenen %q", n, g, bek)
		}
	}
}

// Bayiye gösterilen genel hata metninin kopyası kaynağıyla aynı kalmalı; yoksa
// sade çeviri sessizce devre dışı kalır.
func TestSunucuHatasiGenelEsit(t *testing.T) {
	if sunucuHatasiGenel != nginxset.SunucuHatasiGenel {
		t.Fatalf("sunucuHatasiGenel kopyası nginxset.SunucuHatasiGenel ile aynı değil")
	}
}

// bosIseNil: cümlesi de ayrıntısı da olmayan açıklama yanıta "boş kutu" olarak
// girmesin (ön yüz boş satır/düğme çizerdi). Bugünkü biçimlerde aciklamaKur
// üzerinden ulaşılamayan savunma yolu; doğrudan sınanır.
func TestBosIseNil(t *testing.T) {
	if bosIseNil(&Aciklama{}) != nil || bosIseNil(nil) != nil {
		t.Fatal("boş açıklama nil olmalı")
	}
	if bosIseNil(&Aciklama{Ayrinti: []Ayrinti{{TR: "İş", EN: "Job", Deger: "1"}}}) == nil {
		t.Fatal("yalnız ayrıntısı olan açıklama korunmalı")
	}
}
