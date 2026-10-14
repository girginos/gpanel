package laravel

// kuyruk_birimi_test.go — kuyruk işleyicisi birim şablonu: StartLimit* anahtarlarının [Unit]
// bölümünde olması ve provisioner'daki artık süpürmesiyle uyumu.
//
// 🔴 StartLimitIntervalSec [Service]'te YOK SAYILIR (systemd "Unknown key"; StartLimitBurst eski
// takma adıyla uygulanır) → etkin aralık 10 sn kalır, patlama aşılmaz, çöken işleyici sonsuza dek
// 5 sn'de bir yeniden başlar (181: 147.961 yeniden başlatma). Şablon düzeltmesini ve kurulu eski
// birimleri açılışta yamalayan provisioner.LaravelKuyrukBirimiYamala'nın ona UYUMUNU kilitler.

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/provisioner"
)

// kb_bolumler: unit gövdesi → bölüm adı → boş olmayan (kırpılmış) satırlar.
func kb_bolumler(govde string) map[string][]string {
	m := map[string][]string{}
	bolum := ""
	for _, s := range strings.Split(govde, "\n") {
		t := strings.TrimSpace(s)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			bolum = t
			continue
		}
		if t != "" {
			m[bolum] = append(m[bolum], t)
		}
	}
	return m
}

func kb_icerir(satirlar []string, s string) bool {
	for _, x := range satirlar {
		if x == s {
			return true
		}
	}
	return false
}

func kb_birim() string {
	return queueUnit(7, "c_a", "/home/c_a/app", "/usr/bin/php", "database", 60, 1000)
}

func TestKuyrukBirimi_StartLimitUnitBolumunde(t *testing.T) {
	b := kb_bolumler(kb_birim())
	for _, w := range []string{
		fmt.Sprintf("StartLimitIntervalSec=%d", queueStartLimitAralikSn),
		fmt.Sprintf("StartLimitBurst=%d", queueStartLimitPatlama),
	} {
		if !kb_icerir(b["[Unit]"], w) {
			t.Errorf("[Unit] bölümünde %q olmalı: %v", w, b["[Unit]"])
		}
		if kb_icerir(b["[Service]"], w) {
			t.Errorf("[Service] bölümünde %q OLMAMALI (systemd yok sayar → sonsuz yeniden başlatma)", w)
		}
	}
	for bolum, satirlar := range b {
		if bolum == "[Unit]" {
			continue
		}
		for _, s := range satirlar {
			if strings.HasPrefix(s, "StartLimit") {
				t.Errorf("%s bölümünde StartLimit anahtarı olmamalı: %q", bolum, s)
			}
		}
	}
	// provisioner'daki süpürmeler birimin sahibini [Service] User= satırından okur
	if !kb_icerir(b["[Service]"], "User=c_a") {
		t.Errorf("[Service] bölümünde User=<hesap> olmalı: %v", b["[Service]"])
	}
}

// Tavan, çöken bir işleyiciyi gerçekten yakalar: RestartSec=5 ile `patlama` yeniden başlatma
// pencereye sığmalı (aksi halde sınır hiç tetiklenmez — [Service]'teki ölü ayarla aynı sonuç).
func TestKuyrukBirimi_StartLimitCokenIsleyiciyiYakalar(t *testing.T) {
	const restartSn = 5
	if !strings.Contains(kb_birim(), fmt.Sprintf("RestartSec=%d\n", restartSn)) {
		t.Fatal("test varsayımı: RestartSec değişti; StartLimit hesabını gözden geçir")
	}
	if queueStartLimitPatlama*restartSn >= queueStartLimitAralikSn {
		t.Fatalf("%d başlatma × %d sn = %d sn ≥ pencere %d sn: sınır asla tetiklenmez",
			queueStartLimitPatlama, restartSn, queueStartLimitPatlama*restartSn, queueStartLimitAralikSn)
	}
}

// Kurulu (düzeltme öncesi) biçimi yamalayan provisioner fonksiyonu, yeni şablonu BAYT BAYT üretir:
// yamalı birim ile yeni yazılan birim aynıdır, açılış süpürmesi sonrası ayrışma olmaz.
func TestKuyrukBirimi_EskiBicimYamasiYeniSablonaEsit(t *testing.T) {
	yeni := kb_birim()
	sl := fmt.Sprintf("StartLimitIntervalSec=%d\nStartLimitBurst=%d\n", queueStartLimitAralikSn, queueStartLimitPatlama)
	eski := strings.Replace(yeni, sl+"\n[Service]", "\n[Service]", 1)
	eski = strings.Replace(eski, "RestartSec=5\n", "RestartSec=5\n"+sl, 1)
	if eski == yeni || !kb_icerir(kb_bolumler(eski)["[Service]"], "StartLimitBurst=10") {
		t.Fatal("test düzeneği: eski biçim kurulamadı")
	}
	got, degisti := provisioner.LaravelKuyrukBirimiYamala(eski)
	if !degisti || got != yeni {
		t.Fatalf("yama eski biçimi yeni şablona çevirmeli (değişti=%v):\n%s\n--- beklenen ---\n%s", degisti, got, yeni)
	}
	if g2, d2 := provisioner.LaravelKuyrukBirimiYamala(yeni); d2 || g2 != yeni {
		t.Fatalf("yeni şablona dokunulmamalı (değişti=%v)", d2)
	}
}

// provisioner laravel'i import edemez (laravel → provisioner yönü var): adlandırma ve dizinler orada
// sabit olarak yinelenir. Bu test iki tarafın ayrışmasını yakalar — aksi halde süpürme sessizce hiçbir
// şeyle eşleşmez ya da yanlış dosyaya bakar.
func TestLaravelArtik_ProvisionerAdlandirmasiTutarli(t *testing.T) {
	if got, want := provisioner.LaravelKuyrukBirimOnEki+"7.service", queueUnitName(7)+".service"; got != want {
		t.Errorf("kuyruk birim adı: provisioner %q, laravel %q", got, want)
	}
	if got, want := provisioner.LaravelCronDosyaOnEki+"7", filepath.Base(cronDPath(7)); got != want {
		t.Errorf("cron dosya adı: provisioner %q, laravel %q", got, want)
	}
	if provisioner.LaravelBirimDizini != queueUnitDizini || filepath.Dir(queueUnitPath(7)) != provisioner.LaravelBirimDizini {
		t.Errorf("birim dizini: provisioner %q, laravel %q", provisioner.LaravelBirimDizini, queueUnitDizini)
	}
	if provisioner.LaravelCronDizini != cronDDizin {
		t.Errorf("cron dizini: provisioner %q, laravel %q", provisioner.LaravelCronDizini, cronDDizin)
	}
	if provisioner.LaravelCronAskiDizini != laravelCronAskiDizini {
		t.Errorf("askı cron dizini: provisioner %q, laravel %q", provisioner.LaravelCronAskiDizini, laravelCronAskiDizini)
	}
}
