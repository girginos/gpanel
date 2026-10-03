// Package onyuzkapsam — ön yüz (frontend/src) kaynak kapsam kilitleri (yalnız test).
package onyuzkapsam

// uzun_istek_test.go — 2026-10-03 181: "Yeni Domain" axios'un genel 30 sn
// sınırına takıldı; tarayıcı bağlantıyı kapattı, kullanıcı yanıltıcı hata gördü.
// Arka uç bu işleri artık istemciden KOPUK tamamlıyor (httpx.KopukBaglam); ön yüz
// de uzun çağrılarda sunucu üst sınırı kadar beklemeli (UZUN_ISTEK) ve hosting
// oluşturmada yanıt alınamazsa "başarısız" demeden durumu yoklamalı.
// Kök/bayi/müşteri aynı sayfaları kullanır (App.tsx abonelikler/:id/*).

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const kok = "../../frontend/src/"

func oku(t *testing.T, yol string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(kok, yol))
	if err != nil {
		t.Fatalf("%s: %v", yol, err)
	}
	return string(b)
}

// cagri: uc metnini içeren api.post/put/delete çağrısının tam metni (açan
// "api."dan kapanan paranteze). Uç bulunamazsa "".
func cagrilar(src, uc string) []string {
	var out []string
	for i := 0; ; {
		j := strings.Index(src[i:], uc)
		if j < 0 {
			return out
		}
		j += i
		a := strings.LastIndex(src[:j], "api.")
		if a < 0 {
			i = j + len(uc)
			continue
		}
		p := strings.Index(src[a:], "(")
		if p < 0 {
			return out
		}
		k, derin := a+p+1, 1
		for k < len(src) && derin > 0 {
			switch src[k] {
			case '(', '[', '{':
				derin++
			case ')', ']', '}':
				derin--
			}
			k++
		}
		if m := src[a:k]; regexp.MustCompile(`^api\.(post|put|delete)\b`).MatchString(m) {
			out = append(out, m)
		}
		i = j + len(uc)
	}
}

var uzunCagrilar = map[string][]string{
	"pages/DomainsPage.tsx":              {"'/domains', body", "api.delete(g.url"},
	"components/bayi/BayiDomainler.tsx":  {"`/domains/${h.id}`"},
	"pages/DomainSubdomainlerPage.tsx":   {"`/domains/${id}/subdomain`", "`/domains/${id}/subdomain/${s.id}`", "`/domains/${id}/subdomain/${s.id}/ssl`"},
	"pages/DomainSubdomainYonetPage.tsx": {"`/domains/${id}/subdomain/${sid}/ssl`", "`/domains/${id}/subdomain/${sid}/php`", "`/domains/${id}/subdomain/${sid}`"},
	"pages/DomainPHPPage.tsx":            {"/php-settings`"},
	"components/SubPHPAyarlari.tsx":      {"/php-settings`"},
	"pages/DomainWebSunucuPage.tsx":      {"/web-backend`"},
	"pages/DomainSSLPage.tsx":            {"/ssl/dns-yenile`"},
	"pages/DomainDatabasesPage.tsx":      {"/databases`"},
	"pages/DomainWordPressPage.tsx":      {"`${base}/guncelle`", "`${base}/onar`", "`${base}/arac`", "`${base}/${tur}`", "`${base}/eklenti`", "`${base}/tema`"},
	"pages/WordPressPage.tsx":            {"/wordpress`", "/wordpress/guncelle`"},
	"pages/DomainLaravelPage.tsx":        {"/laravel/kur`", "/laravel/deploy`", "/laravel/queue`", "/laravel/otomatik-hazirla`", "(url, body"},
	"pages/DomainComposerPage.tsx":       {"`${base}/composer`"},
	"pages/RedisPage.tsx":                {"/redis`"},
	"pages/DomainKopyaPage.tsx":          {"/kopya`"},
	"pages/DomainBackupsPage.tsx":        {"/geriyukle`", "/backups/${silinecek.id}`"},
	"pages/DomainGitPage.tsx":            {"/git`", "/git/klonla`", "/git/pull`", "/github/use`"},
	"components/UygulamaKatalogu.tsx":    {"/uygulamalar`", "/uygulamalar/${kayitID}`"},
	"pages/DomainCronPage.tsx":           {"/calistir`"},
	"pages/DomainDNSPage.tsx":            {"/dns/ice-aktar", "/dns/dnssec`", "/dns/sablon`"},
	"pages/DomainMailKutularPage.tsx":    {"'/eklenti/mail/hesaplar'"},
	"pages/DomainSifreKorumaPage.tsx":    {"`${base}/koruma`"},
}

func TestOnYuz_UzunCagrilar30snSinirinaTakilmaz(t *testing.T) {
	api := oku(t, "lib/api.ts")
	if !strings.Contains(api, "timeout: 30_000") {
		t.Fatal("genel axios sınırı (30 sn) beklenirdi — kısa çağrılar için korunur")
	}
	m := regexp.MustCompile(`export const UZUN_ISTEK = \{ timeout: (\d+) \* 60_000 \}`).FindStringSubmatch(api)
	if m == nil {
		t.Fatal("UZUN_ISTEK tanımı yok")
	}
	if dk, _ := strconv.Atoi(m[1]); dk < 10 {
		t.Fatalf("UZUN_ISTEK %d dk — sunucu kopuk üst süresinden (≥10 dk) kısa", dk)
	}
	if !strings.Contains(api, "export function sonucBelirsiz(") {
		t.Fatal("ortak sonucBelirsiz yardımcısı yok")
	}
	for dosya, uclar := range uzunCagrilar {
		src := oku(t, dosya)
		for _, uc := range uclar {
			cs := cagrilar(src, uc)
			if len(cs) == 0 {
				t.Errorf("%s: %q çağrısı bulunamadı", dosya, uc)
				continue
			}
			for _, c := range cs {
				if !strings.Contains(c, "UZUN_ISTEK") {
					t.Errorf("%s: uzun çağrı genel 30 sn sınırında: %s", dosya, strings.Join(strings.Fields(c), " "))
				}
			}
		}
	}
}

func TestOnYuz_HostingOlusturmaZamanAsimindaDurumYoklar(t *testing.T) {
	src := oku(t, "pages/DomainsPage.tsx")
	i := strings.Index(src, "async function olusturGonder(")
	if i < 0 {
		t.Fatal("olusturGonder yok")
	}
	govde := src[i:]
	if j := strings.Index(govde, "\n  }\n"); j > 0 {
		govde = govde[:j]
	}
	for _, gerek := range []string{"sonucBelirsiz(e)", "olusturmaDurumuBekle(alanAdi)", "UZUN_ISTEK"} {
		if !strings.Contains(govde, gerek) {
			t.Errorf("hosting oluşturma: %q yok — zaman aşımında yanıltıcı hata/erken kesme", gerek)
		}
	}
	if !strings.Contains(src, `"Kuruluyor… ({0} sn)"`) {
		t.Error("oluşturma sürerken ilerleme (kuruluyor + süre) gösterilmeli")
	}
}
