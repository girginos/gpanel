package marka

// alanadi_nginx.go — bayi özel panel alan adları için nginx çıktısının SAF
// üreticileri (G/Ç yok; birim testli). Tasarım: tek panel bloğu ilkesi korunur
// (kapi/nginx.go), adlar ek include ile aynı bloğa girer; sertifika SNI'ye göre
// değişkenli seçilir (nginx ≥1.15.9).
//
//	conf.d/00-gosp-marka-sni.conf (http bağlamı; ÖNCE bu yazılır, yoksa
//	  $gp_panel_crt referansı nginx -t'yi düşürür):
//	    map $ssl_server_name $gp_panel_crt { default <panel.crt>; <ad> <bayi crt>; }
//	    map $ssl_server_name $gp_panel_key { ... }
//	/etc/nginx/gosp/marka-adlari.inc (conf.d DIŞINDA): server_name <ad> ...;
//	_panel.conf: ssl_certificate $gp_panel_crt; ssl_certificate_key $gp_panel_key;
//	  + ilk server_name satırının ALTINA include satırı (idempotent).
//
// 🔴 BU DALDA BAĞLANMADI: dosyaları yazan, nginx -t + geri alma + reload yapan
// ve acme.sh ile sertifika alan G/Ç katmanı (provisioner/panelmarka.go) test
// VPS'te nginx -t + LE staging ile kanıtlanmadan devreye alınmamalı.

import (
	"errors"
	"sort"
	"strings"
)

const (
	PanelCrt        = "/etc/ssl/girginospanel/panel.crt"
	PanelKey        = "/etc/ssl/girginospanel/panel.key"
	BayiSertDizin   = "/etc/ssl/girginospanel/bayi"
	MarkaAdlariInc  = "/etc/nginx/gosp/marka-adlari.inc"
	MarkaSNIConf    = "/etc/nginx/conf.d/00-gosp-marka-sni.conf"
	panelIncludeSat = "include " + MarkaAdlariInc + ";"
)

var errKotuAd = errors.New("geçersiz alan adı")

func adlariDogrula(adlar []string) ([]string, error) {
	out := make([]string, 0, len(adlar))
	gor := map[string]bool{}
	for _, a := range adlar {
		// Tırnak, boşluk, ';', '{', '$' vb. HostnameGecerli'den geçemez.
		if !HostnameGecerli(a) {
			return nil, errKotuAd
		}
		if !gor[a] {
			gor[a] = true
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out, nil
}

// SNIHaritasi — conf.d/00-gosp-marka-sni.conf içeriği (adlar sıralı → kararlı).
func SNIHaritasi(adlar []string) (string, error) {
	adlar, err := adlariDogrula(adlar)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# GirginOSPanel — bayi özel panel adları (OTOMATİK ÜRETİLDİ, elle düzenlemeyin)\n")
	for _, tur := range []struct{ deg, vars, uz string }{
		{"$gp_panel_crt", PanelCrt, ".crt"},
		{"$gp_panel_key", PanelKey, ".key"},
	} {
		b.WriteString("map $ssl_server_name " + tur.deg + " {\n")
		b.WriteString("    default " + tur.vars + ";\n")
		for _, a := range adlar {
			b.WriteString("    " + a + " " + BayiSertDizin + "/" + a + tur.uz + ";\n")
		}
		b.WriteString("}\n")
	}
	return b.String(), nil
}

// AdlarInclude — marka-adlari.inc içeriği. Ad yoksa yalnız yorum (geçerli boş include).
func AdlarInclude(adlar []string) (string, error) {
	adlar, err := adlariDogrula(adlar)
	if err != nil {
		return "", err
	}
	if len(adlar) == 0 {
		return "# bayi özel panel adı yok\n", nil
	}
	return "server_name " + strings.Join(adlar, " ") + ";\n", nil
}

// PanelConfUyarla — _panel.conf'u idempotent uyarlar: statik sertifika satırları
// değişkene çevrilir, ilk server_name satırının altına include eklenir.
// server_name satırına ve öteki satırlara DOKUNMAZ. (yeni, değişti mi, hata)
func PanelConfUyarla(conf string) (string, bool, error) {
	satirlar := strings.Split(conf, "\n")
	degisti := false
	includeVar := false
	ilkSN := -1
	for i, s := range satirlar {
		t := strings.TrimSpace(s)
		girinti := s[:len(s)-len(strings.TrimLeft(s, " \t"))]
		switch {
		case t == "ssl_certificate "+PanelCrt+";":
			satirlar[i] = girinti + "ssl_certificate $gp_panel_crt;"
			degisti = true
		case t == "ssl_certificate_key "+PanelKey+";":
			satirlar[i] = girinti + "ssl_certificate_key $gp_panel_key;"
			degisti = true
		case t == panelIncludeSat:
			includeVar = true
		case ilkSN < 0 && strings.HasPrefix(t, "server_name "):
			ilkSN = i
		}
	}
	if !includeVar {
		if ilkSN < 0 {
			return conf, false, errors.New("_panel.conf: server_name satırı yok")
		}
		s := satirlar[ilkSN]
		girinti := s[:len(s)-len(strings.TrimLeft(s, " \t"))]
		yeni := append([]string{}, satirlar[:ilkSN+1]...)
		yeni = append(yeni, girinti+panelIncludeSat)
		yeni = append(yeni, satirlar[ilkSN+1:]...)
		satirlar = yeni
		degisti = true
	}
	return strings.Join(satirlar, "\n"), degisti, nil
}
