package kaynaklimit

// Bayi kaynak havuzu ↔ hosting plan dilimi köprüsü.
//
// Havuz üyeliği değişince (hosting havuza girdi / çıktı / başka bayiye geçti) hostingin plan
// dilimi dosyası YENİ adla (girginos-bayi<N>-<sk>.slice ya da eski düz ad) yeniden yazılmalı;
// hosting planının limitleri havuzun İÇİNDE aynen çalışmayı sürsün. Havuz paketi (bayikaynak)
// kaynaklimit'i içe aktaramadığı için bu iş bir kancayla buraya bağlanır.

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"

	"girginospanel/internal/bayidilim"
	"girginospanel/internal/bayikaynak"
	"girginospanel/internal/provisioner"
)

func init() {
	bayikaynak.HookKur(bayikaynak.Hooklar{UyelikDegisti: uyelikDegisti})
}

// Dikiş noktaları (testler değiştirir): hostingin efektif plan limitleri ve "söküm sürüyor" işareti.
var (
	planLimitleri = PlanLimitleriGetir
	tenantSokumde = provisioner.TenantSokumde
)

// planDilimiYazHafif: plan dilimi DOSYASINI yazar (içerik aynıysa dokunmaz); daemon-reload ve
// canlı uygulama çağırana aittir. true = dosya değişti.
func planDilimiYazHafif(sk string, l Limitler) (bool, error) {
	yol := slicePath(sk)
	want := sliceIcerik(sk, l)
	if cur, err := os.ReadFile(yol); err == nil && string(cur) == want {
		return false, nil
	}
	if err := os.WriteFile(yol, []byte(want), 0o644); err != nil {
		return false, err
	}
	// Aynı yolda önceki bir `set-property --runtime` düşürücüsü yeni dosyayı gölgelemesin.
	_ = os.RemoveAll(filepath.Join(runKontrolDizini, sliceName(sk)+".d"))
	return true, nil
}

// uyelikDegisti: havuza giren / çıkan hostinglerin plan dilimlerini yeni adla yeniden yazar.
// Planı olmayan hostingin dilim dosyası yoktur (örtük dilim) → yazılacak bir şey de yok.
// Hata bir hostingi atlatır; sonraki uzlaştırma turu yeniden dener.
func uyelikDegisti(ctx context.Context, db *sql.DB, r bayikaynak.Rapor) {
	skDomain := map[string]int64{}
	for _, u := range r.UyelikEklenen {
		skDomain[u.SK] = u.DomainID
	}
	for _, sk := range r.UyelikKalkan {
		var id int64
		if err := db.QueryRowContext(ctx, `SELECT id FROM domains WHERE sistem_kullanici=? LIMIT 1`, sk).Scan(&id); err == nil {
			skDomain[sk] = id
		}
	}
	degisen := 0
	for sk, id := range skDomain {
		if ctx.Err() != nil {
			return
		}
		if !bayidilim.GecerliSK(sk) || tenantSokumde(sk) {
			continue
		}
		l, err := planLimitleri(ctx, db, id)
		if err != nil {
			log.Printf("bayi havuz: %s plan limitleri okunamadı: %v", sk, err)
			continue
		}
		if l.CPUYuzde == 0 && l.RAMMB == 0 && l.MaxProcess == 0 {
			continue // plansiz: dilim dosyası yok
		}
		d, err := planDilimiYazHafif(sk, l)
		if err != nil {
			log.Printf("bayi havuz: %s plan dilimi yazılamadı: %v", sk, err)
			continue
		}
		if d {
			degisen++
		}
	}
	if degisen > 0 {
		if out, err := systemctlCalistir("daemon-reload"); err != nil {
			log.Printf("bayi havuz: daemon-reload: %s: %v", strings.TrimSpace(string(out)), err)
		}
		log.Printf("bayi havuz: %d hostingin plan dilimi yeni adıyla yazıldı", degisen)
	}
}
