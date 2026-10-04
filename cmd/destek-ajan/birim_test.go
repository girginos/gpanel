package main

// Birim dosyası + kurulum/güncelleme/paketleme bağlantıları için KAYNAK KORUMA testleri: ajanın panelden
// bağımsızlığı (destek varlık sebebi) ve kurulum sırası (eski gömülü ajan + yeni hizmet aynı anda
// merkeze bağlanmasın) metin düzeyinde de sabitlenir.

import (
	"os"
	"strings"
	"testing"
)

func oku(t *testing.T, yol string) string {
	t.Helper()
	b, err := os.ReadFile("../../" + yol)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// yorumsuz: '#' ile başlayan satırlar atılır.
func yorumsuz(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "#") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func TestBirimBagimsizVeSertlestirilmis(t *testing.T) {
	u := yorumsuz(oku(t, "assets/systemd/girginospanel-destek-ajan.service"))
	for _, yasak := range []string{"girginospanel.service", "BindsTo=", "PartOf=", "Requires=", "EnvironmentFile=", "Environment=", "ListenStream", "CAP_SYS_ADMIN", "CAP_NET_ADMIN", "CAP_SYS_PTRACE"} {
		if strings.Contains(u, yasak) {
			t.Errorf("birimde %q YASAK (panelden bağımsızlık / sır ortama girmez / asgari ayrıcalık)", yasak)
		}
	}
	for _, gerek := range []string{
		"LoadCredential=panel-env:/etc/girginospanel/env",
		"After=network-online.target mariadb.service",
		"Restart=always",
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		"ProtectHome=read-only",
		"PrivateTmp=yes",
		"CapabilityBoundingSet=",
		"MemoryMax=",
		"TasksMax=",
		"ExecStart=/usr/local/bin/girginospanel-destek-ajan",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(u, gerek) {
			t.Errorf("birimde %q yok", gerek)
		}
	}
	// ReadWritePaths yalnız bilinen dört yol (genişlerse bilinçli karar olsun).
	for _, l := range strings.Split(u, "\n") {
		if strings.HasPrefix(l, "ReadWritePaths=") {
			if got, want := strings.TrimPrefix(l, "ReadWritePaths="), "/run/girginospanel /etc/girginospanel /var/log/nginx /var/lib/nginx"; got != want {
				t.Errorf("ReadWritePaths değişti: %q (beklenen %q)", got, want)
			}
		}
	}
}

func TestKurulumSirasiAjanPanelden_Sonra(t *testing.T) {
	k := oku(t, "girginospanel-install.sh")
	panel := strings.Index(k, "systemctl restart girginospanel >/dev/null 2>&1")
	ajan := strings.Index(k, "systemctl restart girginospanel-destek-ajan")
	if panel < 0 || ajan < 0 || ajan < panel {
		t.Fatalf("installer: ajan paneli izlemeli (panel=%d ajan=%d)", panel, ajan)
	}
	if !strings.Contains(k, `"$A/systemd/girginospanel-destek-ajan.service"`) || !strings.Contains(k, `"$A/girginospanel-destek-ajan"`) {
		t.Error("installer ajan ikilisini/birimini kurmuyor")
	}
	// Birim kurulurken ETKİNLEŞTİRİLMEZ (enable/start yalnız panelden sonra).
	if i := strings.Index(k, "install -m 0644 \"$A/systemd/girginospanel-destek-ajan.service\""); i < 0 || i > panel {
		t.Error("birim dosyası panel başlatılmadan önce kurulmalı")
	}
}

func TestGuncellemeSirasiAjanSaglikliPaneldenSonra(t *testing.T) {
	g := oku(t, "assets/ops/girginospanel-update")
	saglik := strings.Index(g, "if saglik_bekle; then HEALTHY=1; fi")
	basla := strings.Index(g, "systemctl restart girginospanel-destek-ajan")
	hasarli := strings.Index(g, "yeni sürüm sağlıklı başlamadı")
	if saglik < 0 || basla < 0 || hasarli < 0 {
		t.Fatalf("güncelleme betiği beklenen noktaları içermiyor: %d %d %d", saglik, basla, hasarli)
	}
	if basla < saglik {
		t.Error("ajan, panel SAĞLIKLI olmadan başlatılıyor (eski gömülü ajanla çift bağlantı riski)")
	}
	if basla > hasarli {
		t.Error("ajan başlatma bloğu başarı dalında değil (geri alma dalında ajan başlatılmamalı)")
	}
	// Ajan hatası güncellemeyi başarısız saymaz: bloğun içinde die yok.
	blok := g[basla:hasarli]
	if i := strings.Index(blok, "# 11b)"); i > 0 {
		blok = blok[:i]
	}
	if strings.Contains(blok, "die ") {
		t.Error("ajan başlatma bloğu die içeriyor — panel ajana bağımlı olmamalı")
	}
}

func TestPaketlemeAjaniDerler(t *testing.T) {
	b := oku(t, "scripts/build-assets.sh")
	if !strings.Contains(b, "./cmd/destek-ajan") || !strings.Contains(b, "assets/girginospanel-destek-ajan") {
		t.Error("build-assets.sh destek ajanını derlemiyor (GOAMD64=v1 ortamıyla)")
	}
	p := oku(t, "scripts/girginospanel-paketle")
	if !strings.Contains(p, `girginospanel-destek-ajan" ] || dur`) {
		t.Error("paketle: ajan ikilisi yoksa paket reddedilmeli")
	}
}

func TestSirYenilemeAjaniYenidenBaslatir(t *testing.T) {
	s := oku(t, "cmd/server/sir.go")
	if !strings.Contains(s, "girginospanel-destek-ajan.service") {
		t.Error("sir yenile destek ajanını yeniden başlatmıyor (bayat DB parolası)")
	}
}
