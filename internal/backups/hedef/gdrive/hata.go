package gdrive

// hata.go — Google API hata gövdesi çözümü, kategori eşleme ve geri çekilme.
//
// Hata metinlerine YALNIZ durum kodu ve Google'ın makine okunur "reason" kodu
// (sanitize edilmiş) girer: gövdedeki "message" geri yansıyan istek içeriği
// taşıyabilir, hiç alınmaz.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"girginospanel/internal/agkapi"
	"girginospanel/internal/backups/hedef"
)

const (
	// azamiDeneme — bir istek için 429/5xx/ağ hatasında en çok yeniden deneme.
	azamiDeneme = 6
	// geriCekilmeTaban/Tavan — üstel geri çekilme (1,2,4,8,16,32 sn; tavan 64 sn).
	geriCekilmeTaban = time.Second
	geriCekilmeTavan = 64 * time.Second
	// azamiRetryAfter — sunucunun istediği bekleme bu süreyi aşamaz.
	azamiRetryAfter = 5 * time.Minute
	// govdeSiniri — hata gövdesinden okunan en çok bayt.
	govdeSiniri = 64 << 10
)

var reasonRe = regexp.MustCompile(`[^A-Za-z0-9_]`)

// apiHatasi — Google hata gövdesinin sırsız özeti.
type apiHatasi struct {
	Kod    int
	Reason string // sanitize edilmiş, en çok 40 karakter
	OAuth  string // OAuth "error" alanı (invalid_grant ...), sanitize edilmiş
}

func (a apiHatasi) metin() string {
	s := "http " + strconv.Itoa(a.Kod)
	if a.Reason != "" {
		s += " " + a.Reason
	}
	if a.OAuth != "" {
		s += " " + a.OAuth
	}
	return s
}

func temizReason(s string) string {
	s = reasonRe.ReplaceAllString(s, "")
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// govdeCoz — hata gövdesinden reason/OAuth kodunu çıkarır (bozuk gövde sorun değil).
func govdeCoz(kod int, govde []byte) apiHatasi {
	a := apiHatasi{Kod: kod}
	var g struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(govde, &g) != nil || len(g.Error) == 0 {
		return a
	}
	// Drive: {"error":{"code":403,"errors":[{"reason":"..."}]}}
	var d struct {
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
		Status string `json:"status"`
	}
	if json.Unmarshal(g.Error, &d) == nil {
		if len(d.Errors) > 0 {
			a.Reason = temizReason(d.Errors[0].Reason)
		}
		if a.Reason == "" {
			a.Reason = temizReason(d.Status)
		}
		return a
	}
	// OAuth: {"error":"invalid_grant","error_description":"..."}
	var s string
	if json.Unmarshal(g.Error, &s) == nil {
		a.OAuth = temizReason(s)
	}
	return a
}

var oranReasonlari = map[string]bool{
	"ratelimitexceeded": true, "userratelimitexceeded": true, "quotaexceeded": true,
	"dailylimitexceeded": true, "sharingratelimitexceeded": true, "concurrentlimitexceeded": true,
}

// yenidenDenenir — bu yanıt geçici mi (üstel geri çekilmeyle tekrar denenir)?
func (a apiHatasi) yenidenDenenir() bool {
	switch {
	case a.Kod == 429, a.Kod == 408:
		return true
	case a.Kod >= 500 && a.Kod != 501:
		return true
	case a.Kod == 403 && oranReasonlari[strings.ToLower(a.Reason)]:
		return true
	}
	return false
}

// kategori — hata kategorisi (Google reason'ı HTTP kodundan önce gelir).
func (a apiHatasi) kategori() hedef.Kategori {
	r := strings.ToLower(a.Reason)
	switch {
	case r == "storagequotaexceeded", r == "teamdrivefilelimitexceeded", r == "teamdrivestoragequotaexceeded":
		return hedef.KategoriKota
	case oranReasonlari[r]:
		return hedef.KategoriOran
	case r == "autherror", r == "unauthorized":
		return hedef.KategoriKimlik
	case r == "insufficientpermissions", r == "forbidden", r == "appnotauthorizedtofile",
		r == "cannotdownloadabusivefile", r == "domainpolicy", r == "accessnotconfigured":
		return hedef.KategoriErisim
	case r == "notfound", r == "filenotfound":
		return hedef.KategoriBulunamadi
	case r == "backenderror", r == "internalerror":
		return hedef.KategoriAg
	}
	switch strings.ToLower(a.OAuth) {
	case "invalid_grant", "invalid_client", "unauthorized_client", "invalid_request", "access_denied":
		return hedef.KategoriKimlik
	}
	return hedef.HTTPKategori(a.Kod)
}

// hata — işlem adıyla *hedef.Hata; sırlar ayrıntıdan maskelenir.
func (a apiHatasi) hata(islem string, sirlar []string) *hedef.Hata {
	return hedef.YeniHata(a.kategori(), islem, errors.New("drive: "+a.metin()), sirlar...)
}

// agHatasi — taşıma hatasından istek URL'sini ayıklar: *url.Error metni tam URL
// (yükleme oturumu adresi = taşıyıcı kimlik) içerir.
func agHatasi(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return fmt.Errorf("drive ağ hatası: %w", ue.Err)
	}
	return err
}

// govdeOku — yanıt gövdesini sınırlı okur ve kapatır.
func govdeOku(r *http.Response) []byte {
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, govdeSiniri))
	return b
}

// retryAfter — Retry-After başlığı (saniye ya da HTTP tarihi); yoksa/geçersizse ok=false.
func retryAfter(h http.Header, simdi time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	var d time.Duration
	if sn, err := strconv.Atoi(v); err == nil {
		if sn < 0 {
			return 0, false
		}
		d = time.Duration(sn) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(simdi)
		if d < 0 {
			d = 0
		}
	} else {
		return 0, false
	}
	if d > azamiRetryAfter {
		d = azamiRetryAfter
	}
	return d, true
}

// geriCekilme — deneme (0'dan) için bekleme: taban*2^deneme, tavanlı, %0-25 jitter.
func geriCekilme(deneme int) time.Duration {
	d := geriCekilmeTaban
	for i := 0; i < deneme && d < geriCekilmeTavan; i++ {
		d *= 2
	}
	if d > geriCekilmeTavan {
		d = geriCekilmeTavan
	}
	return d + time.Duration(rand.Int64N(int64(d)/4+1))
}

func uykuGercek(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// reddiMi — SSRF kapısı/alan adı izin listesi reddi: yeniden denenmez.
func reddiMi(err error) bool { return errors.Is(err, agkapi.ErrReddedildi) }
