package dbgovernor

// kredi2_test.go — canlı kredi sınırları (süre, ömür, boşluk) ve boşluk eşiğinin tavanı.

import (
	"testing"
	"time"
)

// uzunKos: tek uzun deyimi tur kadar koşturur (sayaç hareketsiz; deyimin Sure'u her turda 15 sn artar).
func (o *ortam) uzunKos(u string, qid int64, tur int) {
	for i := 0; i < tur; i++ {
		o.uzunDeyim(u, qid, time.Duration(5+15*i)*time.Second)
		o.adim(15, nil)
	}
}

// Kredi deyimin SÜRESİ kadardır: 65 sn görülmüş deyimin ardından 140 CPU-sn'lik sıçramanın yalnız
// 65'i kredilenir, kalan 75 CPU-sn (1,25 çekirdek) sayılır ve kısar.
func TestKrediYalnizDeyimSuresiKadar(t *testing.T) {
	o := yeniOrtam(t)
	o.m.NCPU = 8
	o.taban()
	o.uzunKos("a_db", 501, 5)
	o.sorgular()
	o.adim(15, hizlar("a_db", 140.0/15))
	if o.yon.alterSayisi() == 0 {
		t.Fatal("kredi deyim süresini aşan CPU'yu örtmemeli")
	}
}

// Kredi ömrü: deyim kaybolduktan en çok krediOmur tur sonra gelen sayaç sıçraması kredilenir;
// daha geç gelen kredilenmez.
func TestKrediOmruSiniri(t *testing.T) {
	for g := 0; g <= 4; g++ {
		o := yeniOrtam(t)
		o.m.NCPU = 8
		o.taban()
		o.uzunKos("a_db", 501, 9) // 125 sn
		o.sorgular()
		o.calis(g, nil)
		o.adim(15, hizlar("a_db", 130.0/15))
		o.calis(3, nil)
		kisildi := o.yon.alterSayisi() != 0
		if beklenen := g > krediOmur; kisildi != beklenen {
			t.Fatalf("sıçrama %d tur geç: kısıldı=%v beklenen=%v", g, kisildi, beklenen)
		}
	}
}

// Boşluk (uzun kesinti) krediyi sıfırlar: deyim kesintiden önce ya da kesinti SIRASINDA bitse bile
// kredisi, kesinti sonrası ayrı bir CPU patlamasını örtmemeli.
func TestKrediBoslukSifirlar(t *testing.T) {
	for _, ad := range []string{"once", "icinde"} {
		t.Run(ad, func(t *testing.T) {
			o := yeniOrtam(t)
			o.m.NCPU = 8
			o.taban()
			o.uzunKos("a_db", 501, 9)
			o.sorgular()
			if ad == "once" {
				o.adim(15, nil) // deyim kayboldu, kredi birikti
			}
			o.adim(300, nil) // 5 dk kesinti: boşluk turu
			o.adim(15, hizlar("a_db", 130.0/15))
			o.calis(3, nil)
			if o.yon.alterSayisi() == 0 {
				t.Fatal("kesinti sonrası ayrı CPU patlaması eski krediyle örtülmemeli")
			}
		})
	}
}

// Uyuyan/kilit bekleyen deyim CPU yakmaz: bitişi kredi üretmez (gerçek CPU'yu örtmesin).
func TestKrediYalnizCPUYakanDeyim(t *testing.T) {
	o := yeniOrtam(t)
	o.m.NCPU = 8
	o.taban()
	for i := 0; i < 9; i++ {
		o.olcer.mu.Lock()
		o.olcer.calisan = []Sorgu{{Kullanici: "a_db", QueryID: 601, Sure: time.Duration(5+15*i) * time.Second, Durum: "User sleep", Metin: "SELECT SLEEP(900)"}}
		o.olcer.mu.Unlock()
		o.adim(15, nil)
	}
	o.sorgular()
	o.adim(15, hizlar("a_db", 130.0/15))
	o.calis(3, nil)
	if o.yon.alterSayisi() == 0 {
		t.Fatal("uyuyan deyimin süresi CPU kredisi sayılmamalı")
	}
}

// Tur süresi tavanı: 10 dk takılan bir turdan sonra eşik 2,5x90 sn'dir; sonraki 1000 sn'lik aralık
// boşluktur (tavansız eşik 1500 sn olur ve biriken sayaç sahte yük sayılırdı).
func TestUzunTakilmaSonrasiBoslukSayilir(t *testing.T) {
	o := yeniOrtam(t)
	o.ayar.Poll = 10 * time.Second
	var bekle time.Duration
	o.olcer.onKos = func() { o.simdi = o.simdi.Add(bekle) }
	o.taban()
	bekle = 10 * time.Minute
	o.adim(10, hizlar("a_db", 0.1))
	bekle = 0
	o.adim(400, hizlar("a_db", 20.0))
	o.calis(2, nil)
	if o.yon.alterSayisi() != 0 || len(o.m.AktifKullanicilar()) != 0 {
		t.Fatalf("uzun aralık boşluk sayılmalı, kısıt oluşmamalı: %v", o.yon.alterler)
	}
}
