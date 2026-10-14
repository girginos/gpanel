package bayikaynak

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"girginospanel/internal/bayidilim"
)

// Hosting sorgusu (üye listesi) başarısız olursa üyelikler SİLİNMEZ: boş liste "kimse üye değil"
// demek olurdu ve tüm hostingler havuzdan düşerdi (bellekteki dilim adları kaybolurdu).
func TestUzlastir_UyeSorgusuHatasindaUyelikVeDosyalarKorunur(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	if _, err := Uzlastir(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if bayidilim.Uyelik("c_a") != 1 || bayidilim.Uyelik("c_b") != 1 {
		t.Fatal("ön koşul: üyelikler kurulu olmalı")
	}
	if _, err := db.Exec(`ALTER TABLE domains CHANGE sistem_kullanici sk_x VARCHAR(64) NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	if _, err := Uzlastir(context.Background(), db); err == nil {
		t.Fatal("hosting sorgusu bozukken hata beklenirdi")
	}
	if bayidilim.Uyelik("c_a") != 1 || bayidilim.Uyelik("c_b") != 1 {
		t.Error("sorgu hatasında üyelikler silindi")
	}
	if !o.Var(filepath.Join(o.Birim, "girginos-bayi1.slice")) || !o.Var(filepath.Join(o.Birim, "php-fpm-c_a.service.d", bayidilim.DropinAd)) {
		t.Error("sorgu hatasında dilim/drop-in silindi")
	}
	if n := len(o.Komut.Satirlar()); n != 0 {
		t.Errorf("sorgu hatasında komut çalıştı: %v", o.Komut.Satirlar())
	}
}

// Pasif (süreçsiz) havuz diliminde değer değişince dosya yazılır ve systemd'ye bildirilir, ama
// `set-property --runtime` ÇAĞRILMAZ (sonraki başlatma dosyayı okur; runtime override kalıntısı bırakılmaz).
func TestUzlastir_PasifDilimeCanliUygulanmaz(t *testing.T) {
	o := ortamKur(t)
	db := geciciDB(t)
	seedBayiler(t, db, o)
	if _, err := Uzlastir(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	// sahte komut yürütücüsü varsayılanı: is-active → inactive.
	if _, err := db.Exec(`UPDATE users SET kaynak_cpu_cekirdek=3 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	o.Komut.Sifirla()
	if _, err := Uzlastir(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(o.Oku(t, filepath.Join(o.Birim, "girginos-bayi1.slice")), "CPUQuota=300%") {
		t.Error("dilim dosyası güncellenmedi")
	}
	if o.Komut.Say("systemctl daemon-reload") != 1 {
		t.Errorf("daemon-reload: %v", o.Komut.Satirlar())
	}
	if n := o.Komut.Say("systemctl set-property"); n != 0 {
		t.Errorf("pasif dilime canlı uygulama yapıldı (%d çağrı): %v", n, o.Komut.Satirlar())
	}
}
