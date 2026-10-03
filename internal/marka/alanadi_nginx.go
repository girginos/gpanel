package marka

// alanadi_nginx.go — bayi özel panel alan adları için nginx çıktısının SAF
// üreticileri (G/Ç yok; birim testli). G/Ç katmanı: provisioner/panelmarka.go
// (acme.sh + atomik yazım + nginx -t + geri alma + TLS ölçümü).
//
// Tasarım: tek panel bloğu ilkesi korunur (kapi/nginx.go), adlar ek include ile
// aynı bloğa girer; sertifika SNI'ye göre değişkenli seçilir (nginx ≥1.15.9).
//
//	conf.d/00-gosp-marka-sni.conf (http bağlamı):
//	    map $ssl_server_name $gp_panel_crt { default <varsayılan kopya>; <ad> <bayi crt>; }
//	    map $ssl_server_name $gp_panel_key { ... }
//	/etc/nginx/gosp/marka-adlari.inc (conf.d DIŞINDA): server_name <ad> ...;
//	_panel.conf: ssl_certificate $gp_panel_crt; ssl_certificate_key $gp_panel_key;
//	  + ilk server_name satırının ALTINA include satırı (idempotent).
//	conf.d/_panel_marka_acme.conf: ad başına :80 mini vhost (HTTP-01 + HTTPS 301).
//
// 🔴 DEĞİŞKENLİ SERTİFİKA = ANAHTARI WORKER OKUR: statik yolda anahtarı root
// master yükler; değişkenli yolda her el sıkışmada nginx WORKER'ı (user nginx)
// okur. panel.key 0600 root'tur ve öyle kalmalıdır (başka akışlar ona güvenir).
// Bu yüzden varsayılan (panel) sertifikanın KOPYASI BayiSertDizin'de 0640
// root:nginx tutulur ve map'in default'u ona bakar. Değişkenli kip YALNIZ en az
// bir etkin bayi adı varken açılır; son ad gidince PanelConfGeriAl statiğe döner
// (özelliği kullanmayan sunucuda sıfır değişiklik).

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

const (
	PanelCrt       = "/etc/ssl/girginospanel/panel.crt"
	PanelKey       = "/etc/ssl/girginospanel/panel.key"
	BayiSertDizin  = "/etc/ssl/girginospanel/bayi"
	MarkaAdlariInc = "/etc/nginx/gosp/marka-adlari.inc"
	MarkaSNIConf   = "/etc/nginx/conf.d/00-gosp-marka-sni.conf"
	// MarkaAcmeConf — bayi adlarının :80 mini vhost'ları (panelhost'un
	// _panel_acme.conf'unun eşi; kapı uzlaştırması yönlendirme portunu ikisinde de eşitler).
	MarkaAcmeConf = "/etc/nginx/conf.d/_panel_marka_acme.conf"
	// PanelAcmeWebroot — panelhost.WebrootDir ile AYNI (panel tarafı HTTP-01 kökü).
	PanelAcmeWebroot = "/var/www/gpanel-acme-challenge"
	// VarsayilanCrt/Key — panel sertifikasının worker'ın okuyabildiği kopyası.
	// '_' bir hostname'de geçemez → hiçbir bayi adıyla çakışmaz.
	VarsayilanCrt = BayiSertDizin + "/_varsayilan.crt"
	VarsayilanKey = BayiSertDizin + "/_varsayilan.key"

	panelIncludeSat = "include " + MarkaAdlariInc + ";"
	degiskenCrtSat  = "ssl_certificate $gp_panel_crt;"
	degiskenKeySat  = "ssl_certificate_key $gp_panel_key;"
)

var errKotuAd = errors.New("geçersiz alan adı")

// BayiCrt / BayiKey — adın sertifika/anahtar yolu. Ad HostnameGecerli değilse ""
// (yol enjeksiyonu: '/', '..', boşluk vb. geçemez).
func BayiCrt(ad string) string {
	if !HostnameGecerli(ad) {
		return ""
	}
	return BayiSertDizin + "/" + ad + ".crt"
}

func BayiKey(ad string) string {
	if !HostnameGecerli(ad) {
		return ""
	}
	return BayiSertDizin + "/" + ad + ".key"
}

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
		{"$gp_panel_crt", VarsayilanCrt, ".crt"},
		{"$gp_panel_key", VarsayilanKey, ".key"},
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

// AcmeVhost80 — bayi adlarının :80 mini vhost'ları: AD BAŞINA bir server bloğu
// ($server_name bloğun İLK adıdır; ortak blok b.com isteğini a.com'a yönlendirirdi).
// Yalnız HTTP-01 yolu webroot'a; kalan trafik genel panel portuna 301
// (kapi.AcmeYonlendirmeUzlastir aynı deseni tanır). Ad yoksa "".
func AcmeVhost80(adlar []string, port int) (string, error) {
	adlar, err := adlariDogrula(adlar)
	if err != nil {
		return "", err
	}
	if port <= 0 || port > 65535 {
		return "", errors.New("geçersiz panel portu")
	}
	if len(adlar) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("# GirginOSPanel — bayi özel panel adları :80 (OTOMATİK ÜRETİLDİ, elle düzenlemeyin)\n")
	for _, a := range adlar {
		b.WriteString("server {\n    listen 80;\n    listen [::]:80;\n    server_name " + a + ";\n\n")
		b.WriteString("    location /.well-known/acme-challenge/ {\n        root " + PanelAcmeWebroot + ";\n        try_files $uri =404;\n        access_log off;\n    }\n\n")
		b.WriteString("    location / {\n        return 301 https://$server_name:" + strconv.Itoa(port) + "$request_uri;\n    }\n}\n")
	}
	return b.String(), nil
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
			satirlar[i] = girinti + degiskenCrtSat
			degisti = true
		case t == "ssl_certificate_key "+PanelKey+";":
			satirlar[i] = girinti + degiskenKeySat
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

// PanelConfGeriAl — PanelConfUyarla'nın tersi: değişkenli sertifika satırları
// statiğe döner, include satırı silinir. Öteki satırlara DOKUNMAZ. (yeni, değişti mi)
func PanelConfGeriAl(conf string) (string, bool) {
	satirlar := strings.Split(conf, "\n")
	out := make([]string, 0, len(satirlar))
	degisti := false
	for _, s := range satirlar {
		t := strings.TrimSpace(s)
		girinti := s[:len(s)-len(strings.TrimLeft(s, " \t"))]
		switch t {
		case degiskenCrtSat:
			out = append(out, girinti+"ssl_certificate "+PanelCrt+";")
			degisti = true
		case degiskenKeySat:
			out = append(out, girinti+"ssl_certificate_key "+PanelKey+";")
			degisti = true
		case panelIncludeSat:
			degisti = true
		default:
			out = append(out, s)
		}
	}
	return strings.Join(out, "\n"), degisti
}

// PanelConfDegiskenli — _panel.conf değişkenli sertifika kipinde mi?
func PanelConfDegiskenli(conf string) bool {
	for _, s := range strings.Split(conf, "\n") {
		if t := strings.TrimSpace(s); t == degiskenCrtSat || t == degiskenKeySat || t == panelIncludeSat {
			return true
		}
	}
	return false
}
