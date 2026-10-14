package dns

// zone_islem.go — DNS yazma uçlarının ORTAK iskeleti: değişiklik + zone yazımı ATOMİK.
//
// Eskiden uçlar DB'yi değiştirip WriteZone'u çağırıyor, zone hatasını yalnız günlüğe
// yazıp BAŞARILI dönüyordu: bozuk kayıt DB'de kalıyor, zone güncellenmiyor ve o
// hesabın sonraki tüm değişiklikleri sessizce zone dışında kalıyordu.
//
// zoneliIslem şunu yapar (domain başına seri):
//  1. işlem (tx) açar, değişikliği tx içinde uygular;
//  2. zone'u tx'in GÖRDÜĞÜ (henüz commit edilmemiş) durumdan yazar:
//     named-checkzone → dosya → include (checkconf) → named yeniden yükleme;
//  3. herhangi bir adım başarısızsa tx GERİ ALINIR, zone dosyası önceki geçerli
//     hâline döner, include commit edilmiş DB'den yeniden üretilir (includeTazele),
//     çağırana *zoneIslemHatasi gider;
//  4. hepsi tamamsa commit eder (commit başarısızsa dosyalar da geri alınır). Commit
//     sonrası include yine commit edilmiş DB ile uzlaştırılır: başka bir domainin
//     eşzamanlı işlemi include'u bu işlemi görmeyen görünümüyle yazmış olabilir.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"girginospanel/internal/httpx"
	"girginospanel/internal/middleware"
)

// islemHatasi — tx içindeki DB adımının istemciye AYNEN dönecek hatası (404 yabancı
// kayıt, 400 doğrulama ...). Dönerse zone'a hiç dokunulmaz, tx geri alınır.
type islemHatasi struct {
	Kod   int
	Ileti string
}

func (e *islemHatasi) Error() string { return e.Ileti }

// zoneIslemHatasi — değişiklik uygulandı ama zone yazılamadı; HER ŞEY geri alındı.
type zoneIslemHatasi struct{ err error }

func (e *zoneIslemHatasi) Error() string { return e.err.Error() }
func (e *zoneIslemHatasi) Unwrap() error { return e.err }

// domainKilitleri — domain başına seri çalışma (aynı zone dosyasına eşzamanlı yazım
// .tmp dosyasını ezerdi; geri alma da başkasının değişikliğini ezmesin).
var domainKilitleri sync.Map

func domainKilidi(id int64) *sync.Mutex {
	if m, ok := domainKilitleri.Load(id); ok {
		return m.(*sync.Mutex)
	}
	m, _ := domainKilitleri.LoadOrStore(id, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// zoneliIslem — bkz. dosya başı. degistir tx içinde DB'yi değiştirir; *islemHatasi
// dönerse olduğu gibi yukarı çıkar (zone yazılmaz).
func (h *Handlers) zoneliIslem(ctx context.Context, domainID int64, degistir func(tx *sql.Tx) error) error {
	k := domainKilidi(domainID)
	k.Lock()
	defer k.Unlock()

	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // Commit başarılıysa no-op; hata/erken dönüşte geri alır.

	if err := degistir(tx); err != nil {
		return err
	}
	geri, tazele, err := zoneUygula(ctx, tx, domainID)
	if err != nil {
		_ = tx.Rollback() // includeTazele commit edilmiş (bu işlemsiz) durumu okusun
		if tazele {
			includeTazele(ctx, h.DB, true)
		}
		return &zoneIslemHatasi{err: err}
	}
	if err := tx.Commit(); err != nil {
		geri() // DB değişmedi → zone da önceki hâline dönsün
		if tazele {
			includeTazele(ctx, h.DB, true)
		}
		return fmt.Errorf("değişiklik kaydedilemedi: %w", err)
	}
	if tazele {
		includeTazele(ctx, h.DB, false)
	}
	return nil
}

// hataYanitla — zoneliIslem hatasını HTTP yanıtına çevirir. Zone yazımı yüzünden geri
// alındıysa true döner (çağıran başarısız denetim kaydı yazar).
//
//	onek:        zone hatasında iletinin başı
//	gecersizKod: kiracının KENDİ zone'u geçersizse (named-checkzone) durum kodu;
//	             sunucu tarafı hatalar (include/reload) her zaman 500.
func (h *Handlers) hataYanitla(w http.ResponseWriter, r *http.Request, domainID int64, err error, onek string, gecersizKod int) bool {
	var ih *islemHatasi
	if errors.As(err, &ih) {
		httpx.WriteError(w, ih.Kod, ih.Ileti)
		return false
	}
	var zh *zoneIslemHatasi
	if errors.As(err, &zh) {
		kod := http.StatusInternalServerError
		var zg *zoneGecersizHatasi
		if errors.As(zh.err, &zg) {
			kod = gecersizKod
		}
		httpx.WriteError(w, kod, onek+zoneHatasi(r, domainID, zh.err))
		return true
	}
	httpx.WriteError(w, http.StatusInternalServerError, err.Error())
	return false
}

// denetle — DNS işlemini audit_log'a yazar (kapsam domainin sahibinden çözülür).
// eylem kodları internal/denetim/eylemler.go'da etiketlidir.
func (h *Handlers) denetle(r *http.Request, domainID int64, eylem, hedef, detay string, basarili bool) {
	uid, aktor := middleware.Aktor(r)
	httpx.DenetimDomain(h.DB, r, uid, aktor, eylem, hedef, detay, domainID, basarili)
}

// kayitOzeti — denetim notu için tek satırlık kayıt özeti.
func kayitOzeti(k Kayit) string {
	deger := k.Deger
	if len(deger) > 120 {
		deger = deger[:120] + "…"
	}
	s := fmt.Sprintf("#%d %s %s %s ttl=%d", k.ID, k.Ad, k.Tip, strings.TrimSpace(deger), k.TTL)
	if k.Tip == "MX" || k.Tip == "SRV" {
		s += fmt.Sprintf(" oncelik=%d", k.Oncelik)
	}
	if !k.Aktif {
		s += " pasif"
	}
	return s
}
