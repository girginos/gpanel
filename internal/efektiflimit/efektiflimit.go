package efektiflimit

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Sorgulayici — *sql.DB ve *sql.Tx ikisi de uyar (dönüşüm/devir işlem içinde okur).
type Sorgulayici interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Yazici — üst yazımı kaydetmek için (*sql.DB / *sql.Tx).
type Yazici interface {
	Sorgulayici
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Kaynak değerleri: efektif değerin nereden geldiği.
const (
	KaynakUst  = "ust"
	KaynakAlan = "alan" // domains.disk_kota_mb / inode_kota (0037)
	KaynakPlan = "plan"
	KaynakYok  = "yok" // plan yok, üst yazım yok → çağıranın varsayılanı
)

// Deger — tek alanın çözümü.
type Deger struct {
	Plan    *int   // planın değeri (plan yoksa nil)
	Ust     *int   // hosting_limit_ust değeri (yoksa nil)
	Alan    *int   // domains.<alan> > 0 (yalnız AlanMiras)
	Efektif *int   // nil = KaynakYok
	Kaynak  string // ust | alan | plan | yok
	Kok     bool   // üst yazımı kök kilitledi (bayi göremez/değiştiremez)
}

// Sonuc — bir hostingin tüm efektif limitleri.
type Sonuc struct {
	DomainID   int64
	ResellerID int64
	PlanID     *int64 // domains.plan_id (sarkık olabilir → PlanVar false)
	PlanVar    bool   // plan satırı gerçekten var
	PlanAd     string
	PlanSahip  int64  // service_plans.reseller_id
	PlanDomain *int64 // service_plans.domain_id — dolu: dönüştürülmemiş eski özel kopya
	UstVar     bool
	KokAlanlar map[string]bool
	Degerler   map[string]Deger
}

// Efektif — alanın efektif değeri; KaynakYok ise varsayılan.
func (s Sonuc) Efektif(sutun string, varsayilan int) int {
	if d, ok := s.Degerler[sutun]; ok && d.Efektif != nil {
		return *d.Efektif
	}
	return varsayilan
}

// UstHarita — mevcut üst yazım değerleri (yalnız dolu olanlar).
func (s Sonuc) UstHarita() map[string]*int {
	m := map[string]*int{}
	for k, d := range s.Degerler {
		if d.Ust != nil {
			v := *d.Ust
			m[k] = &v
		}
	}
	return m
}

var okuSorgu = func() string {
	var b strings.Builder
	b.WriteString("SELECT d.id, COALESCE(d.reseller_id,0), d.plan_id, p.id, COALESCE(p.ad,''), COALESCE(p.reseller_id,0), p.domain_id, ")
	b.WriteString(OzelVarIfade + ", COALESCE(u.kok_alanlar,'')")
	for _, a := range Alanlar {
		fmt.Fprintf(&b, ", p.`%[1]s`, u.`%[1]s`", a.Sutun)
		if a.AlanMiras {
			fmt.Fprintf(&b, ", d.`%s`", a.Sutun)
		}
	}
	b.WriteString(" FROM domains d" + Join + "WHERE d.id=?")
	return b.String()
}()

func ip(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// Oku — hostingin efektif limitleri. Hosting yoksa sql.ErrNoRows.
func Oku(ctx context.Context, q Sorgulayici, did int64) (Sonuc, error) {
	var s Sonuc
	var planID, pID, pDom sql.NullInt64
	var ustVar int
	var kok string
	type uclu struct{ p, u, d sql.NullInt64 }
	ham := make([]uclu, len(Alanlar))
	hedef := []any{&s.DomainID, &s.ResellerID, &planID, &pID, &s.PlanAd, &s.PlanSahip, &pDom, &ustVar, &kok}
	for i, a := range Alanlar {
		hedef = append(hedef, &ham[i].p, &ham[i].u)
		if a.AlanMiras {
			hedef = append(hedef, &ham[i].d)
		}
	}
	if err := q.QueryRowContext(ctx, okuSorgu, did).Scan(hedef...); err != nil {
		return Sonuc{}, err
	}
	if planID.Valid {
		v := planID.Int64
		s.PlanID = &v
	}
	s.PlanVar = pID.Valid
	if pDom.Valid {
		v := pDom.Int64
		s.PlanDomain = &v
	}
	s.UstVar = ustVar == 1
	s.KokAlanlar = kokCoz(kok)
	s.Degerler = make(map[string]Deger, len(Alanlar))
	for i, a := range Alanlar {
		d := Deger{Ust: ip(ham[i].u), Kok: s.KokAlanlar[a.Sutun] && ham[i].u.Valid}
		if s.PlanVar {
			pv := 0 // plan var ama sütun NULL (eski şema) → 0
			if ham[i].p.Valid {
				pv = int(ham[i].p.Int64)
			}
			d.Plan = &pv
		}
		if a.AlanMiras && ham[i].d.Valid && ham[i].d.Int64 > 0 {
			d.Alan = ip(ham[i].d)
		}
		switch {
		case d.Ust != nil:
			d.Efektif, d.Kaynak = d.Ust, KaynakUst
		case d.Alan != nil:
			d.Efektif, d.Kaynak = d.Alan, KaynakAlan
		case d.Plan != nil:
			d.Efektif, d.Kaynak = d.Plan, KaynakPlan
		default:
			d.Kaynak = KaynakYok
		}
		s.Degerler[a.Sutun] = d
	}
	return s, nil
}

// Dogrula — istemciden gelen üst yazım haritası (nil değer = plandan al).
// bayi=true: disk/trafik 0 (sınırsız) yazılamaz.
func Dogrula(ust map[string]*int, bayi bool) error {
	for k, v := range ust {
		a, ok := alanHarita[k]
		if !ok {
			return fmt.Errorf("bilinmeyen limit alanı: %q", k)
		}
		if v == nil {
			continue
		}
		n := *v
		if n < 0 {
			return fmt.Errorf("%s negatif olamaz", k)
		}
		if a.Sifir == SifirYasak && n == 0 {
			return fmt.Errorf("%s sıfır olamaz (en az %d)", k, a.Min)
		}
		if n != 0 && n < a.Min {
			return fmt.Errorf("%s en az %d olmalı", k, a.Min)
		}
		if a.Max > 0 && n > a.Max {
			return fmt.Errorf("%s en fazla %d olabilir", k, a.Max)
		}
		if bayi && a.BayiSifirYasak && n == 0 {
			return fmt.Errorf("%s sıfır (sınırsız) olamaz", k)
		}
	}
	return nil
}

// Kaydet — hostingin üst yazımını TAM olarak değiştirir: ust'ta olmayan ya da nil
// alan NULL olur. Hepsi nil → satır silinir. kok: kökün kilitlediği alanlar (yalnız
// dolu olanlar saklanır). kaynakPlan: dönüşümde eski kopya planın kimliği (nil = dokunma).
// Değerler Dogrula'dan geçmiş olmalıdır; sütun adları yalnız Alanlar'dan gelir.
func Kaydet(ctx context.Context, ex Yazici, did int64, ust map[string]*int, kok map[string]bool, uid int64, kaynakPlan *int64) error {
	for k := range ust {
		if _, ok := alanHarita[k]; !ok {
			return fmt.Errorf("bilinmeyen limit alanı: %q", k)
		}
	}
	dolu := false
	kokDolu := map[string]bool{}
	for _, a := range Alanlar {
		if v := ust[a.Sutun]; v != nil {
			dolu = true
			if kok[a.Sutun] {
				kokDolu[a.Sutun] = true
			}
		}
	}
	if !dolu {
		_, err := ex.ExecContext(ctx, `DELETE FROM hosting_limit_ust WHERE domain_id=?`, did)
		return err
	}
	sutun := []string{"domain_id"}
	yer := []string{"?"}
	guncel := []string{}
	args := []any{did}
	for _, a := range Alanlar {
		sutun = append(sutun, "`"+a.Sutun+"`")
		yer = append(yer, "?")
		guncel = append(guncel, fmt.Sprintf("`%[1]s`=VALUES(`%[1]s`)", a.Sutun))
		if v := ust[a.Sutun]; v != nil {
			args = append(args, *v)
		} else {
			args = append(args, nil)
		}
	}
	var uidV, kpV any
	if uid > 0 {
		uidV = uid
	}
	if kaynakPlan != nil {
		kpV = *kaynakPlan
	}
	sutun = append(sutun, "kok_alanlar", "guncelleyen_user_id", "kaynak_ozel_plan_id")
	yer = append(yer, "?", "?", "?")
	args = append(args, kokYaz(kokDolu), uidV, kpV)
	guncel = append(guncel, "kok_alanlar=VALUES(kok_alanlar)", "guncelleyen_user_id=VALUES(guncelleyen_user_id)",
		"kaynak_ozel_plan_id=COALESCE(VALUES(kaynak_ozel_plan_id), kaynak_ozel_plan_id)")
	q := "INSERT INTO hosting_limit_ust (" + strings.Join(sutun, ",") + ") VALUES (" + strings.Join(yer, ",") +
		") ON DUPLICATE KEY UPDATE " + strings.Join(guncel, ", ")
	_, err := ex.ExecContext(ctx, q, args...)
	return err
}

// Esit — iki çözümün 22 alanda aynı efektif değeri verip vermediği (dönüşüm
// değişmezi). haric: bilinçli olarak değişmesine izin verilen alanlar.
func Esit(a, b Sonuc, haric map[string]bool) []string {
	var fark []string
	for _, al := range Alanlar {
		if haric[al.Sutun] {
			continue
		}
		x, y := a.Degerler[al.Sutun].Efektif, b.Degerler[al.Sutun].Efektif
		if (x == nil) != (y == nil) || (x != nil && *x != *y) {
			fark = append(fark, al.Sutun)
		}
	}
	return fark
}
