package dbgovernor

// eylem.go — kısma/bırakma eylemleri: MAX_USER_CONNECTIONS ayarı, KILL QUERY, doğrulama,
// kalıcı kayıt güncelleme ve userstat hazırlığı.

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// kisitDegeri: kısıtta uygulanacak MAX_USER_CONNECTIONS. Kısıt ASLA gevşetmez: kullanıcının
// mevcut sınırı (>0) zaten kısıttan düşükse o korunur.
func kisitDegeri(kisit, onceki int) int {
	if onceki > 0 && onceki <= kisit {
		return onceki
	}
	return kisit
}

// kisitKapsam: bir birim eşiği aşınca, tüketimin bu oranını karşılayan en büyük tüketici kullanıcılar
// kısılır. Gerekçe: (1) boşta/ihmal edilebilir kardeş hesaplar (ör. %2 pay) kısılmaz ve açık
// bağlantıları kesilmez; (2) yük birkaç hesaba bölünse bile (ör. 2 x %50, 20 x %5) kısılmayan kuyruk
// en çok %10 olur: hesap bölerek atlatma yok; (3) tek tüketicili hostingte yalnız o kısılır.
const kisitKapsam = 0.9

// kisitHedefleri: kullanıcılardan, penceredeki tüketimi (pay) büyükten küçüğe sıralı, kümülatif
// payı kisitKapsam'a ulaşana dek olanları döner. Tüketimi olmayan kullanıcı HİÇ seçilmez.
func kisitHedefleri(pay map[string]float64, users []string) []string {
	type kp struct {
		u string
		p float64
	}
	var l []kp
	toplam := 0.0
	for _, u := range users {
		if !kullaniciGecerli(u) {
			continue
		}
		if p := pay[u]; p > 0 {
			l = append(l, kp{u, p})
			toplam += p
		}
	}
	sort.Slice(l, func(i, j int) bool {
		if l[i].p != l[j].p {
			return l[i].p > l[j].p
		}
		return l[i].u < l[j].u
	})
	var out []string
	kum := 0.0
	for _, e := range l {
		if kum >= kisitKapsam*toplam-1e-9 {
			break
		}
		out = append(out, e.u)
		kum += e.p
	}
	return out
}

// birimKisitla: eşik aşan bir birimin (hosting ya da bayinin hostingi) tüketimin büyük kısmını
// oluşturan DB kullanıcılarını kısar (kisitHedefleri); boşta kardeşlere dokunulmaz.
func (m *Motor) birimKisitla(ctx context.Context, now time.Time, ayar Ayar, a asimBirim) {
	users, err := m.Depo.DomainKullanicilari(ctx, a.dom.id)
	if err != nil {
		m.hata(fmt.Sprintf("hosting %d DB kullanıcıları okunamadı: %v", a.dom.id, err))
		return
	}
	var yeni, yukselen, kesenler, surekliler []string
	kesilen := 0
	kim := Kimlik{DomainID: a.dom.id, ResellerID: a.dom.rid, AlanAdi: a.dom.alan}
	for _, u := range kisitHedefleri(a.dom.pay, users) { // kullaniciGecerli süzgeci kisitHedefleri'nde
		s := m.kullaniciKisitla(ctx, now, ayar, u, kim, a.ref, a.olc)
		if s.yeni {
			yeni = append(yeni, u)
		}
		if s.yukselen {
			yukselen = append(yukselen, u)
		}
		if s.kesti {
			kesenler = append(kesenler, u)
			kesilen += s.kesilen
		}
		if s.surekli {
			surekliler = append(surekliler, u)
		}
	}
	sort.Strings(yeni)
	sort.Strings(yukselen)
	sort.Strings(kesenler)
	sort.Strings(surekliler)
	if len(yeni) > 0 {
		m.kisitBildir(ayar, a, yeni)
	}
	if len(yukselen) > 0 {
		m.yukseltBildir(ayar, a, yukselen)
	}
	if len(kesenler) > 0 {
		m.kesBildir(ayar, a, kesenler, kesilen)
	}
	if len(surekliler) > 0 {
		m.surekliBildir(ayar, a, surekliler)
	}
}

// sureklilikAraligi: iki aşım turu arasındaki bu süreden uzun boşluk aşımın KESİLDİĞİ anlamına gelir
// (Tick'in boşluk eşiğiyle aynı: 2,5 x max(Poll, son tur süresi)).
func (m *Motor) sureklilikAraligi(a Ayar) time.Duration {
	ref := a.Poll
	if m.sonTur > ref {
		ref = m.sonTur
	}
	return time.Duration(2.5 * float64(ref))
}

func sonraki(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// seviye3Baglanti: 3. seviyede kullanıcı başına izin verilen bağlantı. ASLA 0: MariaDB'de
// MAX_USER_CONNECTIONS 0 = SINIRSIZ demektir.
const seviye3Baglanti = 1

// seviye3Sinir: 3. seviyeye geçerken kullanıcının TÜM hesaplarına MAX_USER_CONNECTIONS=1 yazar.
// MariaDB yeni sınırı hesabın açık bağlantı sayısı 0'a inince uygular; bu yüzden sınır
// bağlantıları KESMEDEN ÖNCE yazılır (kesim anında yeniden bağlanan istemci eski sınırı alırdı).
// Hiçbir hesaba yazılamazsa hata döner ve kısıt eski değerinde kalır (bağlantı kesilmez).
func (m *Motor) seviye3Sinir(ctx context.Context, k *Kisit) error {
	m.mu.Lock()
	eski := k.KisitDeger
	k.KisitDeger = seviye3Baglanti // plan kancası ve doğrulama hemen bu değeri kullanır
	m.mu.Unlock()
	hs, err := m.Yon.Hesaplar(ctx, []string{k.Kullanici})
	if err == nil {
		hedef, tamam := 0, 0
		for _, h := range hs[k.Kullanici] {
			hedef++
			if h.Baglanti == seviye3Baglanti {
				tamam++
				continue
			}
			if e := m.Yon.BaglantiAyarla(ctx, k.Kullanici, h.Host, seviye3Baglanti); e != nil {
				log.Printf("DB kısıcı: ALTER %s@%s başarısız (3. seviye): %v", k.Kullanici, h.Host, e)
				continue
			}
			tamam++
		}
		switch {
		case hedef == 0:
			err = fmt.Errorf("mysql.user'da hesap yok")
		case tamam == 0:
			err = fmt.Errorf("%d hesaptan hiçbirine ALTER yazılamadı", hedef)
		}
	}
	if err != nil {
		m.mu.Lock()
		k.KisitDeger = eski
		m.mu.Unlock()
		return err
	}
	return nil
}

// kisitSonuc: kullaniciKisitla'nın bu turda yaptığı.
type kisitSonuc struct {
	yeni     bool // yeni kısıt kaydı/uygulaması
	yukselen bool // 1 → 2. seviye
	kesti    bool // 2 → 3. seviye: bağlantılar kesildi
	kesilen  int  // kesilen bağlantı sayısı
	surekli  bool // 3. seviyede kesintisiz aşım Soguma'dan uzun sürdü (yöneticiye bildirim)
}

// kullaniciKisitla: tek bir DB kullanıcısını kısar (ya da var olan kısıtı tazeler/yükseltir).
func (m *Motor) kullaniciKisitla(ctx context.Context, now time.Time, ayar Ayar, u string, kim Kimlik, ref BirimRef, olc float64) (sonuc kisitSonuc) {
	m.mu.Lock()
	if k := m.aktif[u]; k != nil {
		if k.geriYazildi {
			m.mu.Unlock()
			return sonuc // kayıt kapanışı bekliyor: yeniden kısma/tırmanma yok
		}
		k.birimEkle(ref)
		if k.sonEsikUstu.IsZero() || now.Sub(k.sonEsikUstu) > m.sureklilikAraligi(ayar) {
			k.asimBasi = now // aşım kesintiye uğradı: tırmanma saati baştan
		}
		k.sonEsikUstu = now
		k.SonAsim = now
		k.Tepe = maxf(k.Tepe, olc)
		kes := false
		// Tırmanma YALNIZ uygula modunda ve gerçekten uygulanmış kısıtta: gözlem modunda KILL/ALTER yok
		// (gözlem kaydı hiç tırmanmaz; önceki uygula döneminden kalan kısıt da yeni eylem almaz).
		if ayar.Mod == ModUygula && k.Uygulandi {
			switch {
			case k.Seviye == 1 && now.Sub(k.asimBasi) >= ayar.Yukselme:
				k.Seviye = 2
				k.seviye2T = now
				k.sonYaz = time.Time{}
				sonuc.yukselen = true
			case k.Seviye == 2 && !k.seviye2T.IsZero() && now.Sub(sonraki(k.seviye2T, k.asimBasi)) >= ayar.BaglantiKes:
				kes = true
			case k.Seviye == 3 && !k.seviye3T.IsZero() && now.Sub(sonraki(k.seviye3T, k.asimBasi)) >= ayar.Soguma &&
				now.Sub(k.surekliT) >= bildirimBastir:
				// En sert kısıtta bile kesintisiz aşım Soguma'dan uzun sürdü: otomatik kilit/askı YOK; yalnız
				// yöneticiye "izole kopya/askı önerilir" bildirimi (en çok 10 dk'da bir).
				k.surekliT = now
				sonuc.surekli = true
			}
		}
		m.mu.Unlock()
		if kes {
			if err := m.seviye3Sinir(ctx, k); err != nil {
				// Sınır yazılamadı: bağlantı KESİLMEZ (eski sınırla yeniden bağlanmak işe yaramaz); sonraki turda yeniden denenir.
				m.hata(fmt.Sprintf("%s 3. seviye bağlantı sınırı yazılamadı: %v", u, err))
				return sonuc
			}
			n, err := m.baglantiKes(ctx, u)
			if err != nil {
				// Seviye 2'de kalır; sonraki turda (aşım sürüyorsa) yeniden denenir.
				m.hata(fmt.Sprintf("%s bağlantıları kesilemedi: %v", u, err))
				return sonuc
			}
			m.mu.Lock()
			k.Seviye = 3
			k.seviye3T = now
			k.Kesilen += n
			k.sonYaz = time.Time{}
			m.mu.Unlock()
			sonuc.kesti, sonuc.kesilen = true, n
		}
		return sonuc
	}
	m.mu.Unlock()

	k := &Kisit{Kullanici: u, Kimlik: kim, Birimler: []BirimRef{ref}, Seviye: 1, Baslangic: now, SonAsim: now,
		asimBasi: now, sonEsikUstu: now,
		Tepe: olc, Uygulandi: ayar.Mod == ModUygula, KisitDeger: ayar.KisitBaglanti}
	var hesaplar []Hesap
	if k.Uygulandi {
		hs, err := m.Yon.Hesaplar(ctx, []string{u})
		if err != nil {
			m.hata(fmt.Sprintf("%s hesapları okunamadı: %v", u, err))
			return kisitSonuc{}
		}
		hesaplar = hs[u]
		if len(hesaplar) == 0 {
			log.Printf("DB kısıcı: %s mysql.user'da yok — atlandı", u)
			return kisitSonuc{}
		}
		k.Onceki = hesaplar[0].Baglanti
		k.KisitDeger = kisitDegeri(ayar.KisitBaglanti, k.Onceki)
	}
	// Önce KAYIT, sonra MariaDB değişikliği: kayıt yazılamazsa kısıt konmaz (panel çökerse geri
	// alınamayacak bir kısıt bırakmak, kısıtlamamaktan kötüdür).
	id, err := m.Depo.Ac(ctx, k)
	if err != nil {
		m.hata(fmt.Sprintf("%s kısıt kaydı yazılamadı (kısıt konmadı): %v", u, err))
		return kisitSonuc{}
	}
	k.KayitID = id
	k.sonYaz = now
	if k.Uygulandi {
		basarili := 0
		for _, h := range hesaplar {
			if h.Baglanti == k.KisitDeger {
				basarili++
				continue
			}
			if err := m.Yon.BaglantiAyarla(ctx, u, h.Host, k.KisitDeger); err != nil {
				log.Printf("DB kısıcı: ALTER %s@%s başarısız: %v", u, h.Host, err)
				continue
			}
			basarili++
		}
		if basarili == 0 {
			_ = m.Depo.Kapat(ctx, id, now.Unix(), "hata")
			return kisitSonuc{}
		}
	}
	m.mu.Lock()
	m.aktif[u] = k
	m.mu.Unlock()
	return kisitSonuc{yeni: true}
}

// killPass: 2. ve 3. seviyedeki kullanıcıların KillEsigi'nden uzun süredir çalışan CPU yakan OKUMA sorgularını
// sonlandırır (KILL QUERY ID — bağlantıyı kesmez).
func (m *Motor) killPass(ctx context.Context, ayar Ayar, calisan []Sorgu) {
	m.mu.Lock()
	hedef := map[string]*Kisit{}
	for u, k := range m.aktif {
		if k.Seviye >= 2 && k.Uygulandi && !k.geriYazildi {
			hedef[u] = k
		}
	}
	m.mu.Unlock()
	if len(hedef) == 0 {
		return
	}
	for _, q := range calisan {
		k := hedef[q.Kullanici]
		// Yalnız CPU YAKAN okuma: uyuyan/kilit bekleyen deyim (SLEEP, lock wait) CPU harcamaz; öldürmek
		// kısıtın amacına hizmet etmez, yalnız masum işi keser.
		if k == nil || q.QueryID <= 0 || q.Sure < ayar.KillEsigi || !cpuYakiyorMu(q.Durum) || !okumaSorgusuMu(q.Metin) {
			continue
		}
		if err := m.Yon.SorguOldur(ctx, q.QueryID); err != nil {
			continue // deyim bu arada bitmiş olabilir; sorun değil
		}
		m.mu.Lock()
		k.Oldurulen++
		m.mu.Unlock()
		log.Printf("DB kısıcı: %s sorgusu %s > %s → KILL QUERY (query_id=%d)", k.Kullanici, q.Sure.Round(time.Second), ayar.KillEsigi, q.QueryID)
	}
}

// baglantiKes: 3. seviye — kullanıcının AÇIK bağlantılarını KILL CONNECTION ile keser; kesilen
// sayıyı döner. Güvenlik katmanları: (1) kullaniciGecerli (sistem/servis hesabı ASLA), (2) Yonetici
// yalnız o kullanıcının bağlantılarını listeler ve kısıcının KENDİ bağlantısını hariç tutar,
// (3) burada listedeki her satırın kullanıcısı yeniden doğrulanır. Tek tek kesme hatası (bağlantı
// bu arada kapanmış olabilir) toplam başarıyı bozmaz.
func (m *Motor) baglantiKes(ctx context.Context, u string) (int, error) {
	if !kullaniciGecerli(u) {
		return 0, fmt.Errorf("güvenlik: %q kullanıcısının bağlantıları kesilemez", u)
	}
	bl, err := m.Yon.Baglantilar(ctx, u)
	if err != nil {
		return 0, err
	}
	n, hedef := 0, 0
	for _, b := range bl {
		if b.ID <= 0 || b.Kullanici != u {
			continue
		}
		hedef++
		if err := m.Yon.BaglantiKes(ctx, b.ID); err != nil {
			continue
		}
		n++
	}
	if hedef > 0 && n == 0 {
		return 0, fmt.Errorf("%d bağlantıdan hiçbiri kesilemedi", hedef)
	}
	log.Printf("DB kısıcı: %s için %d bağlantı KILL CONNECTION ile kesildi (3. seviye)", u, n)
	return n, nil
}

// beklenenBaglanti: kısıtlı kullanıcıda mysql.user'da görmek istediğimiz değer.
func (m *Motor) beklenenBaglanti(k *Kisit) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.planAnlik[k.Kullanici]; ok && p > 0 && p < k.KisitDeger {
		return p
	}
	return k.KisitDeger
}

// dogrulaPass: etkin kısıtların MariaDB'de GERÇEKTEN uygulandığını doğrular; sapmayı (ör. plan
// yeniden uygulaması kısıtı ezmişse) düzeltir. İdempotent.
func (m *Motor) dogrulaPass(ctx context.Context) {
	m.mu.Lock()
	var liste []*Kisit
	var users []string
	for _, k := range m.aktif {
		if k.Uygulandi && !k.Kurtar && !k.geriYazildi {
			liste = append(liste, k)
			users = append(users, k.Kullanici)
		}
	}
	m.mu.Unlock()
	if len(liste) == 0 {
		return
	}
	hs, err := m.Yon.Hesaplar(ctx, users)
	if err != nil {
		m.hata("kısıt doğrulaması için hesaplar okunamadı: " + err.Error())
		return
	}
	for _, k := range liste {
		beklenen := m.beklenenBaglanti(k)
		for _, h := range hs[k.Kullanici] {
			if h.Baglanti == beklenen {
				continue
			}
			if err := m.Yon.BaglantiAyarla(ctx, k.Kullanici, h.Host, beklenen); err != nil {
				log.Printf("DB kısıcı: kısıt yeniden uygulanamadı %s@%s: %v", k.Kullanici, h.Host, err)
			}
		}
	}
}

// hedefDeger: kısıt kalkarken geri yazılacak MAX_USER_CONNECTIONS.
// Öncelik: kısıt sırasında plan yeniden uygulandıysa onun değeri → hosting planlıysa GÜNCEL plan
// değeri (kaynaklimit planlı hostinge uygular) → plansızsa kısıttan önce okunan değer.
func (m *Motor) hedefDeger(ctx context.Context, k *Kisit) int {
	m.mu.Lock()
	if p, ok := m.planAnlik[k.Kullanici]; ok {
		m.mu.Unlock()
		return p
	}
	m.mu.Unlock()
	if n, planli, err := m.Depo.PlanBaglanti(ctx, k.Kimlik.DomainID); err == nil && planli {
		return n
	}
	return k.Onceki
}

// birakPass: bırakılması gereken kısıtları bırakır (açılış kurtarma, mod kapalı, askı, silinme, soğuma).
func (m *Motor) birakPass(ctx context.Context, now time.Time, ayar Ayar, kim map[string]Kimlik) {
	m.mu.Lock()
	liste := make([]*Kisit, 0, len(m.aktif))
	for _, k := range m.aktif {
		liste = append(liste, k)
	}
	m.mu.Unlock()
	sort.Slice(liste, func(i, j int) bool { return liste[i].Kullanici < liste[j].Kullanici })
	for _, k := range liste {
		neden := ""
		c, bilinen := kim[k.Kullanici]
		switch {
		case k.geriYazildi:
			neden = k.birakNeden // bırakma başladı: koşullar değişse de KAYIT kapanışı tamamlanır
		case k.Kurtar:
			neden = "acilis"
		case ayar.Mod == ModKapali:
			neden = "kapali"
		case ayar.Mod == ModUygula && !k.Uygulandi:
			neden = "mod" // gözlem kaydı: uygula moduna geçildi; yeni değerlendirme gerçekten kısar
		case bilinen && c.Askida:
			neden = "askida"
		case bilinen && c.Yok:
			neden = "silindi"
		case now.Sub(k.SonAsim) >= ayar.Soguma:
			neden = "soguma"
		}
		if neden != "" {
			m.birak(ctx, now, k, neden)
		}
	}
}

// birak: kısıtı kaldırır (MAX_USER_CONNECTIONS geri yazılır, kayıt kapanır). Geri yazma
// başarısızsa kısıt AÇIK kalır ve sonraki turda yeniden denenir.
func (m *Motor) birak(ctx context.Context, now time.Time, k *Kisit, neden string) bool {
	m.mu.Lock()
	yazildi := k.geriYazildi
	if k.Uygulandi && !yazildi {
		// Geri yazma başlamadan işaretle: plan kancası artık planı kısıt değeriyle EZMEZ (aksi halde
		// tam bu pencerede gelen bir plan yeniden uygulaması kalıcı düşük sınır bırakırdı).
		k.geriYazildi = true
		k.birakNeden = neden
	}
	m.mu.Unlock()
	if k.Uygulandi && !yazildi {
		for deneme := 0; deneme < 2; deneme++ {
			hs, err := m.Yon.Hesaplar(ctx, []string{k.Kullanici})
			if err != nil {
				m.hata(fmt.Sprintf("%s kısıtı kaldırılamadı (hesaplar okunamadı): %v", k.Kullanici, err))
				m.geriYazmaIptal(k)
				return false
			}
			hedef := m.hedefDeger(ctx, k)
			basarisiz := false
			for _, h := range hs[k.Kullanici] {
				if h.Baglanti == hedef {
					continue
				}
				if err := m.Yon.BaglantiAyarla(ctx, k.Kullanici, h.Host, hedef); err != nil {
					log.Printf("DB kısıcı: geri yazma başarısız %s@%s: %v", k.Kullanici, h.Host, err)
					basarisiz = true
				}
			}
			if basarisiz {
				m.geriYazmaIptal(k)
				return false
			}
			if m.hedefDeger(ctx, k) == hedef {
				break // geri yazma sırasında plan değişmedi
			}
		}
	}
	if k.KayitID > 0 {
		if err := m.Depo.Kapat(ctx, k.KayitID, now.Unix(), neden); err != nil {
			// Kayıt AÇIK kalırsa UNIQUE(db_user, acik) bu kullanıcıya yeni kısıt kaydını engeller ve
			// panel yeniden başlayınca bayat önceki_baglanti geri yazılır: kısıtı bellekte tut, yeniden dene.
			log.Printf("DB kısıcı: kısıt kaydı kapatılamadı (%s), yeniden denenecek: %v", k.Kullanici, err)
			return false
		}
	}
	m.mu.Lock()
	delete(m.aktif, k.Kullanici)
	delete(m.planAnlik, k.Kullanici)
	kalan := 0
	for _, o := range m.aktif {
		if o.Kimlik.DomainID == k.Kimlik.DomainID {
			kalan++
		}
	}
	// Bildirim/denetim hosting başına son kullanıcı bırakılınca bir kez yazılır: sayaç hostingin TÜM kullanıcılarının toplamı olmalı.
	oldurulen := k.Oldurulen + m.kapananOldurulen[k.Kimlik.DomainID]
	if kalan > 0 {
		m.kapananOldurulen[k.Kimlik.DomainID] = oldurulen
	} else {
		delete(m.kapananOldurulen, k.Kimlik.DomainID)
	}
	m.mu.Unlock()
	if kalan == 0 {
		m.kaldirBildir(k, neden, now, oldurulen)
	}
	return true
}

// geriYazmaIptal: geri yazma tamamlanamadı — kısıt yeniden doğrulanır/yeniden denenir.
func (m *Motor) geriYazmaIptal(k *Kisit) {
	m.mu.Lock()
	k.geriYazildi = false
	m.mu.Unlock()
}

// yazPass: kalıcı kayıtları (son aşım, tepe, öldürülen, seviye) en çok dakikada bir günceller.
func (m *Motor) yazPass(ctx context.Context, now time.Time) {
	m.mu.Lock()
	var liste []*Kisit
	for _, k := range m.aktif {
		if k.KayitID > 0 && now.Sub(k.sonYaz) >= yazAralik {
			liste = append(liste, k)
		}
	}
	m.mu.Unlock()
	for _, k := range liste {
		if err := m.Depo.Guncelle(ctx, k); err != nil {
			log.Printf("DB kısıcı: kısıt kaydı güncellenemedi (%s): %v", k.Kullanici, err)
			continue
		}
		m.mu.Lock()
		k.sonYaz = now
		m.mu.Unlock()
	}
}

// Kurtar: panel (yeniden) başlarken çağrılır. Önceki çalışmadan kalan AÇIK kısıtları bulur ve geri
// alır: panel çökmesi/yeniden başlaması MariaDB'de kalıcı kısıt bırakmaz. Geri yazılamayan
// kısıt bellekte Kurtar işaretiyle kalır ve her turda yeniden denenir.
func (m *Motor) Kurtar(ctx context.Context) {
	m.hazirla()
	acik, err := m.Depo.AcikKayitlar(ctx)
	if err != nil {
		m.hata("açık kısıtlar okunamadı (açılış kurtarma): " + err.Error())
		return
	}
	now := m.simdi()
	for _, k := range acik {
		k.Kurtar = true
		m.mu.Lock()
		m.aktif[k.Kullanici] = k
		m.mu.Unlock()
		if m.birak(ctx, now, k, "acilis") {
			log.Printf("DB kısıcı: açılışta kısıt geri alındı: %s (önceki MAX_USER_CONNECTIONS=%d)", k.Kullanici, k.Onceki)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// userstat

// UserstatCnfYolu: userstat'ı yeniden başlatmalarda da açık tutan yapılandırma parçası.
const UserstatCnfYolu = "/etc/my.cnf.d/zz-girginospanel-userstat.cnf"

const userstatCnfIcerik = "# GirginOSPanel DB CPU kısıcısı (internal/dbgovernor) tarafından yazıldı.\n" +
	"# CPU_TIME muhasebesi için userstat gerekir; MariaDB yeniden başlayınca da açık kalsın.\n" +
	"[mysqld]\nuserstat = 1\n"

// userstatHazirla: userstat kapalıysa AÇIK konumdaki sunucuda dinamik açar (SET GLOBAL; yeniden
// başlatma GEREKMEZ) ve yapılandırma parçasını idempotent yazar. Açılamazsa durum "bekliyor":
// parça yazıldığı için bir sonraki yeniden başlatmada etkin olur.
func (m *Motor) userstatHazirla(ctx context.Context) {
	m.userstatTur++
	m.mu.Lock()
	ds := m.durum.Userstat
	m.mu.Unlock()
	// Açıkken HER turda bakılır (tek ucuz sorgu): userstat dışarıdan kapatılırsa CPU sayaçları donar,
	// kısıcı bunu "yük bitti" sanıp kısıtı soğumayla kaldırırdı ve durum hâlâ "acik" görünürdü
	// (VM-B ölçümü: 40 turda bir bakılıyordu → ~10 dk kör). Kapalı/hatalıyken açma denemesi 10 turda bir.
	if ds != "" && ds != "acik" && m.userstatTur%10 != 0 {
		return
	}
	set := func(s string) {
		m.mu.Lock()
		m.durum.Userstat = s
		m.mu.Unlock()
	}
	acik, err := m.Yon.UserstatAcikMi(ctx)
	if err != nil {
		set("hata")
		return
	}
	if !acik {
		if ds == "acik" {
			log.Printf("DB kısıcı: userstat KAPALI bulundu (dışarıdan kapatılmış); yeniden açılıyor — kapalı kaldığı sürede CPU sayılmadı")
		}
		if err := m.Yon.UserstatAc(ctx); err != nil {
			log.Printf("DB kısıcı: userstat açılamadı (bekliyor): %v", err)
			set("bekliyor")
			m.cnfYaz()
			return
		}
		acik, err = m.Yon.UserstatAcikMi(ctx)
		if err != nil {
			set("hata")
			return
		}
	}
	if acik {
		set("acik")
	} else {
		set("bekliyor")
	}
	m.cnfYaz()
}

func (m *Motor) cnfYaz() {
	kalici := false
	defer func() {
		m.mu.Lock()
		m.durum.UserstatKalici = kalici
		m.mu.Unlock()
	}()
	yol := m.UserstatCnf
	if yol == "" {
		return
	}
	if b, err := os.ReadFile(yol); err == nil && string(b) == userstatCnfIcerik {
		kalici = true
		return
	}
	dir := filepath.Dir(yol)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return // MariaDB yapılandırma dizini yok: kalıcılaştırılamaz
	}
	tmp, err := os.CreateTemp(dir, ".userstat-*.tmp")
	if err != nil {
		log.Printf("DB kısıcı: userstat yapılandırması yazılamadı: %v", err)
		return
	}
	adi := tmp.Name()
	_, werr := tmp.WriteString(userstatCnfIcerik)
	cerr := tmp.Close()
	if werr != nil || cerr != nil || os.Chmod(adi, 0o644) != nil || os.Rename(adi, yol) != nil {
		_ = os.Remove(adi)
		log.Printf("DB kısıcı: userstat yapılandırması yazılamadı (%s)", yol)
		return
	}
	kalici = true
}

// ---------------------------------------------------------------------------------------------
// bildirim / denetim metinleri

func cekirdek(v float64) string { return strings.Replace(fmt.Sprintf("%.1f", v), ".", ",", 1) }

func (m *Motor) kisitBildir(ayar Ayar, a asimBirim, kullanicilar []string) {
	if ayar.Mod != ModUygula {
		m.gozlemBildir(ayar, a, kullanicilar)
		return
	}
	dk := int(ayar.Soguma.Minutes())
	detay := fmt.Sprintf("kullanıcılar: %s; ölçülen %s çekirdek (eşik %s, pencere %d sn); MAX_USER_CONNECTIONS %d",
		strings.Join(kullanicilar, ","), cekirdek(a.olc), cekirdek(a.ref.Esik), int(ayar.Pencere.Seconds()), ayar.KisitBaglanti,
	)
	if a.ref.Tur == "bayi" {
		m.denetimKisit(a.dom.alan, "bayi sınırı: "+detay, a.dom.rid)
		m.bildirimYaz("uyari", "Veritabanı CPU sınırı aşıldı",
			fmt.Sprintf("Hesabınızın toplam veritabanı CPU kullanımı %s çekirdek (sınır %s). En çok kullanan hostinglere (ör. %s) geçici kısıt uygulandı: veritabanı bağlantı sınırı %d'ye indi. Kullanım düşünce %d dk içinde kendiliğinden kalkar.",
				cekirdek(a.olc), cekirdek(a.ref.Esik), a.dom.alan, ayar.KisitBaglanti, dk), 0, a.dom.rid, ayar)
		return
	}
	m.denetimKisit(a.dom.alan, detay, a.dom.rid)
	m.bildirimYaz("uyari", "Veritabanı CPU kısıtı uygulandı",
		fmt.Sprintf("%s hostingi son %d sn'de ortalama %s çekirdek veritabanı CPU'su kullandı (eşik %s). Veritabanı bağlantı sınırı geçici olarak %d'ye düşürüldü; sürerse uzun süren okuma sorguları sonlandırılır, daha da sürerse veritabanı bağlantıları kesilir. Kullanım düşünce %d dk içinde kendiliğinden kalkar. Genellikle sitedeki bir eklenti/yazılım hatası ağır sorgu üretir.",
			a.dom.alan, int(ayar.Pencere.Seconds()), cekirdek(a.olc), cekirdek(a.ref.Esik), ayar.KisitBaglanti, dk), a.dom.id, 0, ayar)
}

// gozlemBildir: GÖZLEM modunda eşik aşımı — hiçbir şey uygulanmaz; sahibe "uygula modunda
// kısılırdı" bildirilir (eşik ayarını gerçek trafikle sınamak için).
func (m *Motor) gozlemBildir(ayar Ayar, a asimBirim, kullanicilar []string) {
	kul := strings.Join(kullanicilar, ", ")
	detay := fmt.Sprintf("GÖZLEM MODU — hiçbir kısıt uygulanmadı; uygula modunda kısılırdı. kullanıcılar: %s; ölçülen %s çekirdek (eşik %s, pencere %d sn); uygula modunda MAX_USER_CONNECTIONS %d'ye indirilirdi",
		kul, cekirdek(a.olc), cekirdek(a.ref.Esik), int(ayar.Pencere.Seconds()), ayar.KisitBaglanti)
	if a.ref.Tur == "bayi" {
		m.denetimGozlem(a.dom.alan, "bayi sınırı: "+detay, a.dom.rid)
		m.bildirimYaz("bilgi", "Veritabanı CPU sınırı aşıldı (gözlem modu)",
			fmt.Sprintf("Hesabınızın toplam veritabanı CPU kullanımı %s çekirdek (sınır %s). Veritabanı kullanıcıları (%s; ör. %s hostingi) uygula modunda kısılırdı; şu an yalnızca gözlem yapılıyor, hiçbir kısıt uygulanmadı.",
				cekirdek(a.olc), cekirdek(a.ref.Esik), kul, a.dom.alan), 0, a.dom.rid, ayar)
		return
	}
	m.denetimGozlem(a.dom.alan, detay, a.dom.rid)
	m.bildirimYaz("bilgi", "Veritabanı CPU eşiği aşıldı (gözlem modu)",
		fmt.Sprintf("%s hostingindeki veritabanı kullanıcıları (%s) son %d sn'de ortalama %s çekirdek veritabanı CPU'su kullandı (eşik %s). Uygula modunda kısılırdı; şu an gözlem modunda olduğu için hiçbir kısıt uygulanmadı.",
			a.dom.alan, kul, int(ayar.Pencere.Seconds()), cekirdek(a.olc), cekirdek(a.ref.Esik)), a.dom.id, 0, ayar)
}

// kesBildir: 3. seviye — bağlantılar kesildi.
func (m *Motor) kesBildir(ayar Ayar, a asimBirim, kullanicilar []string, adet int) {
	dk := int(ayar.BaglantiKes.Minutes())
	detay := fmt.Sprintf("kullanıcılar: %s; 2. seviye %d dk sürdü, hâlâ eşik üstünde (%s çekirdek, eşik %s); %d bağlantı KILL CONNECTION ile kesildi; MAX_USER_CONNECTIONS %d yapıldı, yeniden bağlanan istemci bu sınıra takılır",
		strings.Join(kullanicilar, ","), dk, cekirdek(a.olc), cekirdek(a.ref.Esik), adet, seviye3Baglanti)
	m.denetimKes(a.dom.alan, detay, a.dom.rid)
	msg := fmt.Sprintf("%s hostingindeki veritabanı CPU kullanımı bağlantı kısıtına ve sorgu sonlandırmaya rağmen %d dk daha sürdü (%s çekirdek). Veritabanı kullanıcılarının %d bağlantısı kesildi; bağlantı sınırı %d'e düşürüldü, yeniden bağlanan istemci bu sınıra takılır. Kullanım düşünce kısıt kendiliğinden kalkar.",
		a.dom.alan, dk, cekirdek(a.olc), adet, seviye3Baglanti)
	if a.ref.Tur == "bayi" {
		m.bildirimYaz("uyari", "Veritabanı bağlantıları kesildi", msg, 0, a.dom.rid, ayar)
		return
	}
	m.bildirimYaz("uyari", "Veritabanı bağlantıları kesildi", msg, a.dom.id, 0, ayar)
}

// surekliBildir: 3. seviyede kesintisiz aşım Soguma'dan uzun sürdü. Otomatik işlem YAPILMAZ.
func (m *Motor) surekliBildir(ayar Ayar, a asimBirim, kullanicilar []string) {
	dk := int(ayar.Soguma.Minutes())
	detay := fmt.Sprintf("kullanıcılar: %s; en sert kısıtta (kullanıcı başına %d bağlantı, okuma sorgusu sonlandırma, bağlantı kesme) %d dk'dan uzun süredir eşik üstünde (%s çekirdek, eşik %s); otomatik kilit/askı uygulanmadı, izole kopya ya da askı önerilir",
		strings.Join(kullanicilar, ","), seviye3Baglanti, dk, cekirdek(a.olc), cekirdek(a.ref.Esik))
	m.denetimYukselt(a.dom.alan, detay, a.dom.rid)
	msg := fmt.Sprintf("%s hostingindeki veritabanı CPU kullanımı en sert kısıta (kullanıcı başına %d bağlantı, okuma sorgusu sonlandırma, bağlantı kesme) rağmen %d dk'dan uzun süredir eşik üstünde (%s çekirdek, eşik %s; kullanıcılar: %s). Tek bağlantı bile bir çekirdeği tüketebilir; otomatik bir işlem yapılmadı. Veritabanını izole bir kopyaya almak ya da hostingi askıya almak önerilir.",
		a.dom.alan, seviye3Baglanti, dk, cekirdek(a.olc), cekirdek(a.ref.Esik), strings.Join(kullanicilar, ", "))
	// İzole kopya kararı yalnız yöneticide: sahip bayiyse yöneticiye de yazılır (admin sahipli hostingde
	// hosting bildirimi zaten yöneticiye gider).
	if a.ref.Tur == "bayi" {
		m.bildirimYaz("uyari", surekliBaslik, msg, 0, a.dom.rid, ayar)
		m.yoneticiyeBildir("uyari", surekliBaslik, msg, a.dom.id)
		return
	}
	m.bildirimYaz("uyari", surekliBaslik, msg, a.dom.id, 0, ayar)
	if a.dom.rid > 0 {
		m.yoneticiyeBildir("uyari", surekliBaslik, msg, a.dom.id)
	}
}

func (m *Motor) yukseltBildir(ayar Ayar, a asimBirim, kullanicilar []string) {
	detay := fmt.Sprintf("kullanıcılar: %s; kısıt %d sn sürdü, hâlâ eşik üstünde (%s çekirdek, eşik %s); %d sn'den uzun okuma sorguları sonlandırılıyor",
		strings.Join(kullanicilar, ","), int(ayar.Yukselme.Seconds()), cekirdek(a.olc), cekirdek(a.ref.Esik), int(ayar.KillEsigi.Seconds()))
	m.denetimYukselt(a.dom.alan, detay, a.dom.rid)
	if a.ref.Tur == "hosting" {
		m.bildirimYaz("uyari", "Veritabanı CPU kısıtı sertleşti",
			fmt.Sprintf("%s hostingindeki veritabanı CPU kullanımı kısıta rağmen sürüyor (%s çekirdek). %d sn'den uzun süren okuma sorguları sonlandırılıyor.",
				a.dom.alan, cekirdek(a.olc), int(ayar.KillEsigi.Seconds())), a.dom.id, 0, ayar)
	}
}

func (m *Motor) kaldirBildir(k *Kisit, neden string, now time.Time, oldurulen int) {
	detay := fmt.Sprintf("neden: %s; süre %s; tepe %s çekirdek; sonlandırılan okuma sorgusu %d; MAX_USER_CONNECTIONS geri yazıldı",
		neden, now.Sub(k.Baslangic).Round(time.Second), cekirdek(k.Tepe), oldurulen)
	if !k.Uygulandi {
		detay = strings.Replace(detay, "; MAX_USER_CONNECTIONS geri yazıldı", " (gözlem modu)", 1)
	}
	m.denetimKaldir(k.Kimlik.AlanAdi, detay, k.Kimlik.ResellerID)
	if neden == "soguma" && k.Uygulandi {
		m.bildirimYaz("bilgi", "Veritabanı CPU kısıtı kaldırıldı",
			fmt.Sprintf("%s hostingindeki veritabanı CPU kısıtı kaldırıldı; bağlantı sınırı eski değerine döndü.", k.Kimlik.AlanAdi),
			k.Kimlik.DomainID, 0, VarsayilanAyar())
	}
}
