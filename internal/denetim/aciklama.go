package denetim

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Aciklama: bir denetim kaydının açıklamasının SADE insan dili hâli.
//
// Denetim yazan çağrılar açıklamayı kısa teknik biçimde yazar ("istenen=3
// uygulanan=3 atlanan=0", "izin 1.2.3.4", "kural_id=5" …). Yazım biçimine
// DOKUNULMAZ (eski kayıtlar da aynı biçimde); çeviri burada, OKUMA anında yapılır.
//
//	TR/EN   : listede gösterilen cümle (boş olabilir — yalnız Ayrinti varsa)
//	Ayrinti : iç/teknik ayrıntılar (iş kimliği, dosya yolu, sistem kullanıcısı,
//	          parmak izi, ham hata). Varsayılan gösterimde GİZLİ; satır
//	          genişletilince görünür.
//
// Ham metin (Kayit.Detay) yalnız ipucunda (title) ve genişletmede durur.
type Aciklama struct {
	TR      string    `json:"tr"`
	EN      string    `json:"en"`
	Ayrinti []Ayrinti `json:"ayrinti,omitempty"`
}

// Ayrinti: genişletmede görünen tek bir teknik ayrıntı ("Dosya: /home/...").
type Ayrinti struct {
	TR    string `json:"tr"`
	EN    string `json:"en"`
	Deger string `json:"deger"`
}

// iki: TR/EN metin çifti.
type iki struct{ tr, en string }

// aciklamaKur: audit_log.detail ham değerinden (JSON) sade açıklama. Gösterecek
// bir şey yoksa nil. Bozuk/bilinmeyen biçim ASLA düşmez: okunur geri dönüşle
// (ya da olduğu gibi) gösterilir.
func aciklamaKur(eylem, detail string) *Aciklama {
	d := strings.TrimSpace(detail)
	if d == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(d), &m); err == nil {
		if not, ok := m["not"].(string); ok && len(m) == 1 {
			return metinAciklama(eylem, not)
		}
		return haritaAciklama(eylem, m)
	}
	// JSON olmayan eski/bozuk kayıt: düz metin say.
	return metinAciklama(eylem, d)
}

// ---------------------------------------------------------------------------
// Düz metin ({"not": "..."}) açıklamalar

var (
	reYeniPlan       = regexp.MustCompile(`^yeni plan=(\d+)$`)
	reBayiEtki       = regexp.MustCompile(`^etkilenen hosting=(\d+) basarisiz=(\d+)$`)
	reBayiRed        = regexp.MustCompile(`^reddedildi: (\d+) hosting hesabı var$`)
	reBayiSilinen    = regexp.MustCompile(`^silinen bayi id=(\d+)$`)
	reIPDegis        = regexp.MustCompile(`^(?:toplu #(\d+): )?(\S*) → (\S+) \(A:(\d+) SPF:(\d+)\)(?:: (.+))?$`)
	reSahipDevir     = regexp.MustCompile(`^bayi (.+?)→(.+?), müşteri (.+?)→(.+?), ftp parolası döndürüldü=(true|false)$`)
	reSiteSayisi     = regexp.MustCompile(`^(\d+) site$`)
	reErisimKural    = regexp.MustCompile(`^(izin|red) (\S+)$`)
	rePortDegis      = regexp.MustCompile(`^(\d+) → (\d+)$`)
	reGunSayisi      = regexp.MustCompile(`^(\d+) gün$`)
	reWinAjan        = regexp.MustCompile(`^windows ajanı eklendi \((.*), sürüm=(.*), parmak izi=(.*)\)$`)
	reDegisimOk      = regexp.MustCompile(`^(\S+) (\S+)→(\S+)$`)
	reAnahtarOlustur = regexp.MustCompile(`^ad=(.*) izinler=(\S*) ip=(\d+) gün=(\d+|süresiz)$`)
	reAnahtarYenile  = regexp.MustCompile(`^eski=(\S+) geçiş=(\d+) saat$`)
	reAnahtarIptal   = regexp.MustCompile(`^ad=(.*) neden=(\S+)$`)
	reAnahtarToplu   = regexp.MustCompile(`^neden=(\S+) (?:iptal edilen=(\d+)|HATA: anahtarlar iptal edilemedi)$`)
	reAnahtar        = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	geriAlindiOnek   = "render basarisiz, GERI ALINDI: "
	paketleGelenOnk  = "paketle gelen: "
)

// sunucuHatasiGenel: nginxset.SunucuHatasiGenel'in kopyası (import döngüsü/yük
// olmasın diye); eşitliğini TestSunucuHatasiGenelEsit denetler.
const sunucuHatasiGenel = "sunucu yapılandırması uygulanamadı — ayrıntı sunucu günlüğüne yazıldı; sorun sürerse yöneticinize başvurun"

// sabitMetin: yazım yerlerindeki sabit serbest metinlerin sade TR/EN hâli.
// Anahtar, kaynaktaki metnin BİREBİR aynısıdır.
var sabitMetin = map[string]iki{
	"toplu işlem":                            {"Toplu işlemle yapıldı", "Done as part of a bulk action"},
	"kısmen uygulandı":                       {"Kısmen uygulandı", "Partially applied"},
	"kullanıcı adı zaten var":                {"Kullanıcı adı zaten kullanılıyor", "Username is already taken"},
	"uzak yedekler BIRAKILDI (admin onayı)":  {"Yönetici onayıyla", "With admin approval"},
	"uzak kopya BIRAKILDI (kullanıcı onayı)": {"Kullanıcı onayıyla", "With user approval"},
	"hatalı kod":                             {"Hatalı iki aşamalı doğrulama kodu", "Wrong two-factor code"},
	"hatalı parola":                          {"Hatalı parola", "Wrong password"},
	"zorla (admin yeni kapıyı doğrulamadan)": {"Zorla kapatıldı (yeni port doğrulanmadan)", "Forced (new port not verified)"},
	sunucuHatasiGenel:                        {"Sunucu yapılandırması uygulanamadı; ayrıntı sunucu günlüğünde", "Server configuration could not be applied; details are in the server log"},
}

func metinAciklama(eylem, ham string) *Aciklama {
	s := notSade(eylem, ham)
	if s == "" {
		return nil
	}
	if a := ozelMetin(eylem, s); a != nil {
		return bosIseNil(a)
	}
	if c, ok := sabitMetin[s]; ok {
		return &Aciklama{TR: c.tr, EN: c.en}
	}
	if kv, ok := kvAyristir(s); ok {
		return bosIseNil(kvAciklama(eylem, kv))
	}
	// Serbest metin: olduğu gibi (ilk harf büyük); TR=EN.
	o := ilkBuyuk(s)
	return &Aciklama{TR: o, EN: o}
}

func bosIseNil(a *Aciklama) *Aciklama {
	if a == nil || (a.TR == "" && len(a.Ayrinti) == 0) {
		return nil
	}
	return a
}

// ozelMetin: eyleme özgü biçimler. Tanınmazsa nil (genel yola düşer).
func ozelMetin(eylem, s string) *Aciklama {
	if g, ok := strings.CutPrefix(s, geriAlindiOnek); ok {
		return geriAlindi(eylem, g)
	}
	switch eylem {
	case "hosting.plan":
		if m := reYeniPlan.FindStringSubmatch(s); m != nil {
			p := planAd(m[1])
			return &Aciklama{TR: "Yeni plan: " + p.tr, EN: "New plan: " + p.en}
		}
	case "bayi.askiya_al", "bayi.askidan_al":
		if m := reBayiEtki.FindStringSubmatch(s); m != nil {
			n, b := atoi(m[1]), atoi(m[2])
			tr := fmt.Sprintf("%d hosting hesabı etkilendi", n)
			en := fmt.Sprintf("%d hosting %s affected", n, cogul(n, "account", "accounts"))
			if b > 0 {
				tr += fmt.Sprintf(", %d%s uygulanamadı", b, sayiIyelik(b))
				en += fmt.Sprintf(", %d failed", b)
			}
			return &Aciklama{TR: tr, EN: en}
		}
	case "bayi.guncelle":
		return bayiDegisim(s)
	case "bayi.sil":
		if m := reBayiRed.FindStringSubmatch(s); m != nil {
			n := atoi(m[1])
			return &Aciklama{
				TR: fmt.Sprintf("Reddedildi: bayinin %d hosting hesabı var", n),
				EN: fmt.Sprintf("Rejected: reseller still has %d hosting %s", n, cogul(n, "account", "accounts")),
			}
		}
		if m := reBayiSilinen.FindStringSubmatch(s); m != nil {
			return &Aciklama{Ayrinti: []Ayrinti{{TR: "Bayi kimliği", EN: "Reseller ID", Deger: m[1]}}}
		}
	case "domain.ip_degistir":
		if m := reIPDegis.FindStringSubmatch(s); m != nil {
			return ipDegisim(m)
		}
	case "hosting.sahip_devir":
		if m := reSahipDevir.FindStringSubmatch(s); m != nil {
			return sahipDevir(m)
		}
	case "tasima_baslat":
		if m := reSiteSayisi.FindStringSubmatch(s); m != nil {
			n := atoi(m[1])
			return &Aciklama{TR: fmt.Sprintf("%d site taşınıyor", n), EN: fmt.Sprintf("%d %s being migrated", n, cogul(n, "site", "sites"))}
		}
	case "erisim.kural.ekle", "altalan.erisim.kural.ekle":
		if m := reErisimKural.FindStringSubmatch(s); m != nil {
			k := erisimKural(m[1], m[2])
			return &Aciklama{TR: k.tr + " eklendi: " + m[2], EN: k.en + " added: " + m[2]}
		}
	case "erisim.ayar", "altalan.erisim.ayar":
		if kv, ok := kvAyristir(s); ok {
			if a := erisimAyar(kv); a != nil {
				return a
			}
		}
	case "kapi.rol_port":
		if m := rePortDegis.FindStringSubmatch(s); m != nil {
			return &Aciklama{TR: "Port: " + m[1] + " → " + m[2], EN: "Port: " + m[1] + " → " + m[2]}
		}
	case "kapi.eski_uzat":
		if m := reGunSayisi.FindStringSubmatch(s); m != nil {
			n := atoi(m[1])
			return &Aciklama{TR: fmt.Sprintf("%d gün uzatıldı", n), EN: fmt.Sprintf("Extended by %d %s", n, cogul(n, "day", "days"))}
		}
	case "winajan.ekle":
		if m := reWinAjan.FindStringSubmatch(s); m != nil {
			return &Aciklama{
				TR:      "Adres: " + m[1] + " · Sürüm: " + m[2],
				EN:      "Address: " + m[1] + " · Version: " + m[2],
				Ayrinti: []Ayrinti{{TR: "Parmak izi", EN: "Fingerprint", Deger: m[3]}},
			}
		}
	case "api_anahtar.olustur":
		if m := reAnahtarOlustur.FindStringSubmatch(s); m != nil {
			ip := atoi(m[3])
			izT, izE := "izin yok", "no permissions"
			if m[2] != "" {
				izT = strings.ReplaceAll(m[2], ",", ", ")
				izE = izT
			}
			gT, gE := "süresiz (bitiş yok)", "no expiry"
			if m[4] != "süresiz" {
				gun := atoi(m[4])
				gT, gE = fmt.Sprintf("%d gün geçerli", gun), fmt.Sprintf("valid for %d %s", gun, cogul(gun, "day", "days"))
			}
			return &Aciklama{
				TR: fmt.Sprintf("Ad: %s · İzinler: %s · %d IP adresi · %s", m[1], izT, ip, gT),
				EN: fmt.Sprintf("Name: %s · Permissions: %s · %d IP %s · %s", m[1], izE, ip, cogul(ip, "address", "addresses"), gE),
			}
		}
	case "api_anahtar.yenile":
		if m := reAnahtarYenile.FindStringSubmatch(s); m != nil {
			a := &Aciklama{Ayrinti: []Ayrinti{{TR: "Eski anahtar", EN: "Old key", Deger: m[1]}}}
			if h := atoi(m[2]); h > 0 {
				a.TR = fmt.Sprintf("Eski anahtar %d saat daha geçerli", h)
				a.EN = fmt.Sprintf("Old key stays valid for %d more %s", h, cogul(h, "hour", "hours"))
			} else {
				a.TR, a.EN = "Eski anahtar hemen iptal edildi", "Old key revoked immediately"
			}
			return a
		}
	case "api_anahtar.iptal":
		if m := reAnahtarIptal.FindStringSubmatch(s); m != nil {
			k := iki{"Sahibi iptal etti", "Revoked by owner"}
			if m[2] == "yonetim" {
				k = iki{"Yönetici iptal etti", "Revoked by administrator"}
			} else if m[2] != "sahip" {
				k = iki{"Neden: " + m[2], "Reason: " + m[2]}
			}
			return &Aciklama{TR: "Ad: " + m[1] + " · " + k.tr, EN: "Name: " + m[1] + " · " + k.en}
		}
	case "api_anahtar.toplu_iptal":
		if m := reAnahtarToplu.FindStringSubmatch(s); m != nil {
			n := iptalNedeni(m[1])
			if m[2] == "" {
				return &Aciklama{TR: n.tr + "; anahtarlar iptal edilemedi", EN: n.en + "; keys could not be revoked"}
			}
			c := atoi(m[2])
			return &Aciklama{
				TR: fmt.Sprintf("%s; %d API anahtarı iptal edildi", n.tr, c),
				EN: fmt.Sprintf("%s; %d API %s revoked", n.en, c, cogul(c, "key", "keys")),
			}
		}
	case "api_anahtar.red":
		return anahtarRed(s)
	}
	return nil
}

// iptalNedeni: toplu anahtar iptalinin nedeni (reseller.bayiAnahtarlariniIptal).
func iptalNedeni(n string) iki {
	switch n {
	case "parola_sifirlama":
		return iki{"Bayi parolası sıfırlandı", "Reseller password was reset"}
	case "bayi_silindi":
		return iki{"Bayi silindi", "Reseller was deleted"}
	}
	o := okunurKod(n)
	return iki{o, o}
}

// anahtarRed: API anahtarıyla gelen isteğin reddedilme nedeni (middleware/anahtar.go).
func anahtarRed(s string) *Aciklama {
	sabit := map[string]iki{
		"iptal/süre":         {"Anahtar iptal edilmiş ya da süresi dolmuş", "Key is revoked or expired"},
		"rol tutarsız":       {"Anahtar sahibinin rolü değişmiş", "Key owner's role has changed"},
		"hesap askıda":       {"Hesap askıda", "Account is suspended"},
		"kapı uyumsuz":       {"Yanlış panel portu", "Wrong panel port"},
		"origin":             {"Tarayıcıdan kullanım engellendi", "Use from a browser was blocked"},
		"kök IP listesi boş": {"IP izin listesi boş", "IP allow list is empty"},
	}
	if c, ok := sabit[s]; ok {
		return &Aciklama{TR: c.tr, EN: c.en}
	}
	if ip, ok := strings.CutPrefix(s, "ip "); ok {
		return &Aciklama{TR: "İzin listesinde olmayan IP: " + ip, EN: "IP not on the allow list: " + ip}
	}
	if d, ok := strings.CutPrefix(s, "izin "); ok {
		return &Aciklama{TR: "Anahtarın bu işleme izni yok", EN: "Key has no permission for this action",
			Ayrinti: []Ayrinti{{TR: "İstenen izin", EN: "Required permission", Deger: d}}}
	}
	if d, ok := strings.CutPrefix(s, "uç kapalı "); ok {
		return &Aciklama{TR: "Bu işlem API anahtarına kapalı", EN: "This action is not available to API keys",
			Ayrinti: []Ayrinti{{TR: "Uç", EN: "Endpoint", Deger: d}}}
	}
	return nil
}

// geriAlindi: "render basarisiz, GERI ALINDI: <x>" — işlem uygulanamadı ve önceki
// hâle dönüldü. <x> eyleme göre kural, kural kimliği ya da hata metnidir.
func geriAlindi(eylem, x string) *Aciklama {
	switch {
	case strings.HasSuffix(eylem, "erisim.kural.ekle"):
		if m := reErisimKural.FindStringSubmatch(x); m != nil {
			k := erisimKural(m[1], m[2])
			return &Aciklama{
				TR: "Uygulanamadı, eklenmedi: " + ilkKucukTR(k.tr) + " " + m[2],
				EN: "Could not be applied, not added: " + ilkKucuk(k.en) + " " + m[2],
			}
		}
	case strings.HasSuffix(eylem, "erisim.kural.sil"):
		a := &Aciklama{TR: "Uygulanamadı, kural geri yüklendi", EN: "Could not be applied, rule restored"}
		if kv, ok := kvAyristir(x); ok {
			a.Ayrinti = kvAciklama(eylem, kv).Ayrinti
		}
		return a
	}
	a := &Aciklama{TR: "Uygulanamadı, önceki ayar geri yüklendi", EN: "Could not be applied, previous setting restored"}
	x = strings.TrimSpace(x)
	if c, ok := sabitMetin[x]; ok {
		// Bilinen genel hata (bayiye gösterilen): cümleye dilinde eklenir.
		a.TR += ": " + c.tr
		a.EN += ": " + c.en
	} else if x != "" {
		// Ham sunucu hatası (yalnız yönetici kayıtlarında): genişletmede.
		a.Ayrinti = []Ayrinti{{TR: "Hata", EN: "Error", Deger: x}}
	}
	return a
}

// erisimKural: "izin"/"red" + tek IP ya da aralık → "İzin verilen IP" vb.
func erisimKural(tip, cidr string) iki {
	aralik := false
	if i := strings.IndexByte(cidr, '/'); i >= 0 {
		on := cidr[i+1:]
		aralik = on != "32" && on != "128"
	}
	switch {
	case tip == "izin" && aralik:
		return iki{"İzin verilen IP aralığı", "Allowed IP range"}
	case tip == "izin":
		return iki{"İzin verilen IP", "Allowed IP"}
	case aralik:
		return iki{"Engellenen IP aralığı", "Blocked IP range"}
	default:
		return iki{"Engellenen IP", "Blocked IP"}
	}
}

// erisimAyar: "aktif=true varsayilan=izin" → "Erişim kısıtlaması açık · Kurala uymayanlar izinli".
func erisimAyar(kv []anahtarDeger) *Aciklama {
	var tr, en []string
	for _, p := range kv {
		switch p.k {
		case "aktif":
			if p.v == "true" {
				tr, en = append(tr, "Erişim kısıtlaması açık"), append(en, "Access restriction on")
			} else {
				tr, en = append(tr, "Erişim kısıtlaması kapalı"), append(en, "Access restriction off")
			}
		case "varsayilan":
			if p.v == "red" {
				tr, en = append(tr, "Kurala uymayanlar engellenir"), append(en, "Unmatched visitors are blocked")
			} else {
				tr, en = append(tr, "Kurala uymayanlar izinli"), append(en, "Unmatched visitors are allowed")
			}
		default:
			return nil
		}
	}
	return &Aciklama{TR: strings.Join(tr, " · "), EN: strings.Join(en, " · ")}
}

// bayiDegisim: "paket 1→2, max_disk_mb 100→200, ..." → "Değişen: paket (#1 → #2), disk kotası (100 MB → 200 MB)".
func bayiDegisim(s string) *Aciklama {
	var tr, en []string
	for _, p := range strings.Split(s, ", ") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if g, ok := strings.CutPrefix(p, paketleGelenOnk); ok {
			if kv, ok := kvAyristir(g); ok {
				var lt, le []string
				for _, x := range kv {
					l := anahtarEtiketi(x.k)
					d := degerBicimi(x.k, x.v)
					lt = append(lt, ilkKucukTR(l.tr)+" "+d.tr)
					le = append(le, ilkKucuk(l.en)+" "+d.en)
				}
				tr = append(tr, "paketle gelen sınırlar ("+strings.Join(lt, ", ")+")")
				en = append(en, "package limits ("+strings.Join(le, ", ")+")")
				continue
			}
		}
		if m := reDegisimOk.FindStringSubmatch(p); m != nil && reAnahtar.MatchString(m[1]) {
			l := anahtarEtiketi(m[1])
			a, b := degerBicimi(m[1], m[2]), degerBicimi(m[1], m[3])
			tr = append(tr, ilkKucukTR(l.tr)+" ("+a.tr+" → "+b.tr+")")
			en = append(en, ilkKucuk(l.en)+" ("+a.en+" → "+b.en+")")
			continue
		}
		tr, en = append(tr, p), append(en, p)
	}
	if len(tr) == 0 {
		return &Aciklama{}
	}
	return &Aciklama{TR: "Değişen: " + strings.Join(tr, ", "), EN: "Changed: " + strings.Join(en, ", ")}
}

// ipDegisim: "[toplu #N: ]eski → yeni (A:x SPF:y)[: hata]".
func ipDegisim(m []string) *Aciklama {
	a := &Aciklama{TR: m[2] + " → " + m[3], EN: m[2] + " → " + m[3]}
	akayit, spf := atoi(m[4]), atoi(m[5])
	if akayit+spf > 0 {
		a.TR += fmt.Sprintf(" · %d DNS kaydı güncellendi", akayit+spf)
		a.EN += fmt.Sprintf(" · %d DNS %s updated", akayit+spf, cogul(akayit+spf, "record", "records"))
	}
	if m[1] != "" {
		a.TR = "Toplu işlemle · " + a.TR
		a.EN = "Bulk action · " + a.EN
		a.Ayrinti = append(a.Ayrinti, Ayrinti{TR: "Toplu iş", EN: "Bulk job", Deger: "#" + m[1]})
	}
	if akayit+spf > 0 {
		a.Ayrinti = append(a.Ayrinti, Ayrinti{TR: "DNS kayıtları", EN: "DNS records", Deger: fmt.Sprintf("A: %d, SPF: %d", akayit, spf)})
	}
	if m[6] != "" {
		a.TR += " · Uygulanamadı"
		a.EN += " · Failed"
		a.Ayrinti = append(a.Ayrinti, Ayrinti{TR: "Hata", EN: "Error", Deger: m[6]})
	}
	return a
}

// sahipDevir: "bayi X→Y, müşteri X→Y, ftp parolası döndürüldü=true".
func sahipDevir(m []string) *Aciklama {
	taraf := func(v string) iki {
		switch v {
		case "ana hesap":
			return iki{"ana hesap", "main account"}
		case "değişmedi":
			return iki{"değişmedi", "unchanged"}
		}
		return iki{v, v}
	}
	var tr, en []string
	for _, p := range []struct {
		etk       iki
		eski, yen string
	}{{iki{"Bayi", "Reseller"}, m[1], m[2]}, {iki{"Müşteri", "Customer"}, m[3], m[4]}} {
		if p.yen == "değişmedi" {
			continue
		}
		e, y := taraf(p.eski), taraf(p.yen)
		tr = append(tr, p.etk.tr+": "+e.tr+" → "+y.tr)
		en = append(en, p.etk.en+": "+e.en+" → "+y.en)
	}
	if m[5] == "true" {
		tr, en = append(tr, "FTP parolası yenilendi"), append(en, "FTP password rotated")
	}
	return &Aciklama{TR: strings.Join(tr, " · "), EN: strings.Join(en, " · ")}
}

// ---------------------------------------------------------------------------
// anahtar=değer listeleri

type anahtarDeger struct{ k, v string }

// kvAyristir: "a=1 b=x y" → [(a,1),(b,"x y")]. İlk parça anahtar=değer OLMALI;
// "=" içermeyen sonraki parçalar önceki değere eklenir (boşluklu dosya yolu).
// Anahtar [a-z][a-z0-9_]* değilse ya da biçim tutmuyorsa ok=false.
func kvAyristir(s string) ([]anahtarDeger, bool) {
	var out []anahtarDeger
	for _, p := range strings.Fields(s) {
		k, v, ok := strings.Cut(p, "=")
		if ok && reAnahtar.MatchString(k) {
			out = append(out, anahtarDeger{k, v})
			continue
		}
		if len(out) == 0 {
			return nil, false
		}
		out[len(out)-1].v += " " + p
	}
	return out, len(out) > 0
}

// anahtarBilgi: bilinen anahtarın etiketi; teknik=true → yalnız genişletmede.
type anahtarBilgi struct {
	iki
	teknik bool
}

var anahtarSozlugu = map[string]anahtarBilgi{
	"kullanici":            {iki{"Kullanıcı", "User"}, false},
	"oturum_sonlandirildi": {iki{"Müşteri oturumları sonlandırıldı", "Customer sessions ended"}, false},
	"sonlandirilan":        {iki{"Sonlandırılan FTP oturumu", "FTP sessions ended"}, false},
	"plan":                 {iki{"Plan", "Plan"}, false},
	"paket":                {iki{"Paket", "Package"}, false},
	"domain":               {iki{"Alan adı sınırı", "Domain limit"}, false},
	"max_domain":           {iki{"Alan adı sınırı", "Domain limit"}, false},
	"disk":                 {iki{"Disk kotası", "Disk quota"}, false},
	"max_disk_mb":          {iki{"Disk kotası", "Disk quota"}, false},
	"trafik":               {iki{"Trafik kotası", "Traffic quota"}, false},
	"max_trafik_mb":        {iki{"Trafik kotası", "Traffic quota"}, false},
	"asim":                 {iki{"Aşım ilkesi", "Overage policy"}, false},
	"fazla_satis":          {iki{"Fazla satış", "Overselling"}, false},
	"durum":                {iki{"Durum", "Status"}, false},
	"mod":                  {iki{"Tür", "Type"}, false},
	"aktif":                {iki{"Etkin", "Enabled"}, false},
	"varsayilan":           {iki{"Varsayılan", "Default"}, false},
	"sebep":                {iki{"Sebep", "Reason"}, false},
	"hata":                 {iki{"Hata", "Error"}, false},
	"aralik_sayisi":        {iki{"Aralık sayısı", "Range count"}, false},
	"dosya":                {iki{"Dosya", "File"}, true},
	"yol":                  {iki{"Yol", "Path"}, true},
	"is":                   {iki{"İş", "Job"}, true},
	"is_id":                {iki{"İş", "Job"}, true},
	"sistem_kullanici":     {iki{"Sistem kullanıcısı", "System user"}, true},
	"domain_id":            {iki{"Hosting kimliği", "Hosting ID"}, true},
	"kural_id":             {iki{"Kural kimliği", "Rule ID"}, true},
	"id":                   {iki{"Kimlik", "ID"}, true},
	"sha":                  {iki{"SHA", "SHA"}, true},
	"sha256":               {iki{"SHA-256", "SHA-256"}, true},
	"kaybedilen":           {iki{"Kaybedilen", "Lost"}, true},
	"etki":                 {iki{"Etki", "Impact"}, true},
	"onarim":               {iki{"Onarım", "Remedy"}, true},
}

// anahtarEtiketi: bilinen anahtarın etiketi; bilinmeyen → okunur ("ftp_parola" → "Ftp parola").
func anahtarEtiketi(k string) iki {
	if b, ok := anahtarSozlugu[k]; ok {
		return b.iki
	}
	o := okunurKod(k)
	return iki{o, o}
}

// degerBicimi: anahtara göre değerin okunur hâli (MB, plan no, durum, evet/hayır).
func degerBicimi(k, v string) iki {
	switch k {
	case "plan", "paket":
		return planAd(v)
	case "disk", "max_disk_mb", "trafik", "max_trafik_mb":
		return boyut(v)
	case "domain", "max_domain":
		if v == "0" {
			return iki{"sınırsız", "unlimited"}
		}
	case "durum":
		switch v {
		case "active":
			return iki{"etkin", "active"}
		case "suspended":
			return iki{"askıda", "suspended"}
		}
	case "mod":
		if t, ok := geriYuklemeTuru[v]; ok {
			return t
		}
	case "asim":
		switch v {
		case "yok":
			return iki{"yok", "none"}
		case "disk_trafik":
			return iki{"disk ve trafik", "disk and traffic"}
		case "tumu":
			return iki{"tümü", "all"}
		}
	case "fazla_satis":
		switch v {
		case "0":
			return iki{"kapalı", "off"}
		case "1":
			return iki{"açık", "on"}
		}
	}
	switch v {
	case "true":
		return iki{"evet", "yes"}
	case "false":
		return iki{"hayır", "no"}
	}
	return iki{v, v}
}

var geriYuklemeTuru = map[string]iki{
	"tam":        {"tam yedek", "full backup"},
	"dosyalar":   {"yalnız dosyalar", "files only"},
	"veritabani": {"yalnız veritabanları", "databases only"},
	"dosya":      {"seçili dosyalar", "selected files"},
	"db":         {"seçili veritabanı", "selected database"},
}

// kvAciklama: anahtar=değer listesini "Etiket: değer · …" cümlesine çevirir;
// teknik anahtarlar Ayrinti'ye gider. Bilinen sayaç kalıpları cümleleşir.
func kvAciklama(eylem string, kv []anahtarDeger) *Aciklama {
	a := &Aciklama{}
	var tr, en []string
	ekle := func(t, e string) { tr, en = append(tr, t), append(en, e) }

	v := map[string]string{}
	for _, p := range kv {
		v[p.k] = p.v
	}
	kullanildi := map[string]bool{}
	// istenen/uygulanan/atlanan → "3 işlemden 3'ü uygulandı, 1 atlandı"
	if _, ok := v["istenen"]; ok {
		if _, ok2 := v["uygulanan"]; ok2 {
			ist, uyg, atl := atoi(v["istenen"]), atoi(v["uygulanan"]), atoi(v["atlanan"])
			t := fmt.Sprintf("%d işlemden %d%s uygulandı", ist, uyg, sayiIyelik(uyg))
			e := fmt.Sprintf("%d of %d applied", uyg, ist)
			if atl > 0 {
				t += fmt.Sprintf(", %d atlandı", atl)
				e += fmt.Sprintf(", %d skipped", atl)
			}
			ekle(t, e)
			kullanildi["istenen"], kullanildi["uygulanan"], kullanildi["atlanan"] = true, true, true
		}
	}
	if s, ok := v["sirada"]; ok {
		n := atoi(s)
		if n > 0 {
			ekle(fmt.Sprintf("%d alan adı tarama sırasına alındı", n),
				fmt.Sprintf("%d %s queued for scanning", n, cogul(n, "domain", "domains")))
		} else {
			ekle("Sıraya yeni alan adı alınmadı", "No new domain was queued")
		}
		kullanildi["sirada"] = true
	}
	if s, ok := v["aralik_sayisi"]; ok && strings.HasPrefix(eylem, "guvenilir_vekil") {
		n := atoi(s)
		ekle(fmt.Sprintf("Toplam %d güvenilir vekil aralığı", n),
			fmt.Sprintf("%d trusted proxy %s in total", n, cogul(n, "range", "ranges")))
		kullanildi["aralik_sayisi"] = true
	}
	for _, p := range kv {
		if kullanildi[p.k] {
			continue
		}
		l := anahtarEtiketi(p.k)
		d := degerBicimi(p.k, p.v)
		if b, ok := anahtarSozlugu[p.k]; ok && b.teknik {
			a.Ayrinti = append(a.Ayrinti, Ayrinti{TR: l.tr, EN: l.en, Deger: p.v})
			continue
		}
		if p.k == "mod" {
			// "Tür: tam yedek" yerine "Tam yedek"
			ekle(ilkBuyuk(d.tr), ilkBuyuk(d.en))
			continue
		}
		ekle(l.tr+": "+d.tr, l.en+": "+d.en)
	}
	a.TR, a.EN = strings.Join(tr, " · "), strings.Join(en, " · ")
	return a
}

// ---------------------------------------------------------------------------
// JSON nesnesi ({"a":1,"b":"x"}) açıklamalar — sistem kayıtları

func haritaAciklama(eylem string, m map[string]any) *Aciklama {
	anahtarlar := make([]string, 0, len(m))
	for k := range m {
		anahtarlar = append(anahtarlar, k)
	}
	sort.Strings(anahtarlar)
	kv := make([]anahtarDeger, 0, len(anahtarlar))
	for _, k := range anahtarlar {
		var s string
		switch x := m[k].(type) {
		case string:
			s = x
		case float64:
			s = strconv.FormatFloat(x, 'f', -1, 64)
		case nil:
			s = ""
		default:
			b, _ := json.Marshal(x)
			s = string(b)
		}
		kv = append(kv, anahtarDeger{k, s})
	}
	if eylem == "guvenlik.izolasyon_kaybi" {
		a := &Aciklama{
			TR: "Hosting paylaşılan PHP havuzuna düştü; komşu hesaplardan yalıtımı zayıfladı. Sorun giderilince planı yeniden uygulayın.",
			EN: "Hosting fell back to the shared PHP pool; its isolation from neighbouring accounts is weakened. Re-apply the plan once the cause is fixed.",
		}
		for _, p := range kv {
			l := anahtarEtiketi(p.k)
			a.Ayrinti = append(a.Ayrinti, Ayrinti{TR: l.tr, EN: l.en, Deger: p.v})
		}
		return a
	}
	if len(kv) == 0 {
		return nil
	}
	return bosIseNil(kvAciklama(eylem, kv))
}

// ---------------------------------------------------------------------------
// yardımcılar

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func cogul(n int, tek, cok string) string {
	if n == 1 {
		return tek
	}
	return cok
}

// ilkKucukTR: yalnız İLK harfi Türkçe kurallarıyla küçültür ("İzin verilen IP" →
// "izin verilen IP"; kısaltmalar bozulmaz). ilkKucuk: aynısı, İngilizce için.
func ilkKucukTR(s string) string { return ilkHarf(s, unicode.TurkishCase.ToLower) }
func ilkKucuk(s string) string   { return ilkHarf(s, unicode.ToLower) }

func ilkHarf(s string, f func(rune) rune) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = f(r[0])
	return string(r)
}

func ilkBuyuk(s string) string { return ilkHarf(s, unicode.TurkishCase.ToUpper) }

// planAd: plan/paket kimliği → "#3"; 0 → "yok".
func planAd(v string) iki {
	if v == "0" {
		return iki{"yok", "none"}
	}
	if _, err := strconv.ParseInt(v, 10, 64); err == nil {
		return iki{"#" + v, "#" + v}
	}
	return iki{v, v}
}

// boyut: "1024MB" ya da "1024" (MB) → "1 GB"; 0 → "sınırsız".
func boyut(v string) iki {
	n, err := strconv.ParseInt(strings.TrimSuffix(v, "MB"), 10, 64)
	if err != nil {
		return iki{v, v}
	}
	if n == 0 {
		return iki{"sınırsız", "unlimited"}
	}
	if n >= 1024 && n%1024 == 0 {
		s := strconv.FormatInt(n/1024, 10) + " GB"
		return iki{s, s}
	}
	s := strconv.FormatInt(n, 10) + " MB"
	return iki{s, s}
}

// sayiIyelik: sayının 3. tekil iyelik eki — "3'ü", "1'i", "2'si", "10'u", "0'ı".
// Ek, sayının OKUNUŞUNUN son sözcüğüne göre seçilir (ünlü uyumu).
func sayiIyelik(n int) string {
	if n < 0 {
		n = -n
	}
	birler := []string{"ı", "i", "si", "ü", "ü", "i", "sı", "si", "i", "u"} // sıfır bir iki üç dört beş altı yedi sekiz dokuz
	onlar := []string{"", "u", "si", "u", "ı", "si", "ı", "i", "i", "ı"}    // on yirmi otuz kırk elli altmış yetmiş seksen doksan
	switch {
	case n == 0:
		return "'ı"
	case n%10 != 0:
		return "'" + birler[n%10]
	case n%100 != 0:
		return "'" + onlar[(n/10)%10]
	case n%1000 != 0:
		return "'ü" // yüz
	case n%1000000 != 0:
		return "'i" // bin
	case n%1000000000 != 0:
		return "'u" // milyon
	default:
		return "'ı" // milyar
	}
}
