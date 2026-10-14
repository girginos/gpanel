package dbgovernor

// kaldir_toplam_test.go — çok DB kullanıcılı hostingte "kısıt kaldırıldı" denetimi, hostingin TÜM kullanıcılarında
// sonlandırılan okuma sorgularının toplamını yazmalı (gerçek VPS ölçümü, 2026-10-13: ilk kullanıcı 11 sorgu öldürülmüşken
// denetim, kaldırmayı tetikleyen SON kullanıcının sayacını — 0 — yazıyordu).

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestKaldirDenetimiHostingToplaminiYazar(t *testing.T) {
	o := yeniOrtam(t)
	o.taban()
	o.calis(4, hizlar("b_db", 0.9, "b_wp", 0.8)) // b hostingi: iki kullanıcı, toplam 1,7 çekirdek
	o.olcer.calisan = []Sorgu{{QueryID: 201, Kullanici: "b_db", Sure: 30 * time.Second, Durum: "Sending data", Metin: "SELECT * FROM big"}}
	o.calis(5, hizlar("b_db", 0.9, "b_wp", 0.8)) // 45 sn'yi aşınca 2. seviye: yalnız b_db'nin sorgusu öldürülür
	kb, ok1 := o.m.AktifKisit("b_db")
	kw, ok2 := o.m.AktifKisit("b_wp")
	if !ok1 || !ok2 || kb.Oldurulen == 0 || kw.Oldurulen != 0 {
		t.Fatalf("ön koşul: b_db sorgu öldürmüş, b_wp öldürmemiş olmalı: %+v %+v", kb, kw)
	}
	toplam := kb.Oldurulen
	o.olcer.calisan = nil
	for i := 0; i < 80 && len(o.m.AktifKullanicilar()) > 0; i++ {
		o.adim(15, nil)
	}
	if len(o.m.AktifKullanicilar()) != 0 {
		t.Fatal("yük bitince iki kullanıcının da kısıtı kalkmalıydı")
	}
	n, detay := 0, ""
	for _, e := range o.m.Olaylar() {
		if e.Tur == "denetim" && e.Eylem == "db.cpu_kisit_kaldir" {
			n++
			detay = e.Detay
		}
	}
	if n != 1 {
		t.Fatalf("hosting başına TEK kaldırma denetimi bekleniyordu: %d", n)
	}
	if want := fmt.Sprintf("sonlandırılan okuma sorgusu %d;", toplam); !strings.Contains(detay, want) {
		t.Fatalf("kaldırma denetimi hosting toplamını (%d) yazmalı: %q", toplam, detay)
	}
}
