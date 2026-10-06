package cron

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestEkle_TemelVeVarsayilanlar(t *testing.T) {
	temizle(t)
	g := Gorev{Dakika: "0", Saat: "3", Gun: "*", Ay: "*", Hafta: "*", Komut: "echo a"}
	s, err := Ekle("c_var1", []Gorev{g, gorev("5", "echo b")})
	if err != nil || s.Eklenen != 2 || s.Yinelenen != 0 || len(s.Reddedilen) != 0 {
		t.Fatalf("sonuç: %+v err=%v", s, err)
	}
	l, _ := read("c_var1")
	if len(l) != 2 || l[0].Tip != "komut" || l[0].Bildirim != "bilgi" || !l[0].Etkin || l[1].Idx != 1 {
		t.Fatalf("depo: %+v", l)
	}
	// zamanlayıcı dosyası da üretilmiş olmalı
	if _, err := os.Stat(cronDYolu("c_var1")); err != nil {
		t.Fatalf("cron.d yok: %v", err)
	}
}

func TestEkle_PasifGirdiEtkinYapilir(t *testing.T) {
	temizle(t)
	g := gorev("1", "echo x")
	g.Etkin = false
	if _, err := Ekle("c_var1", []Gorev{g}); err != nil {
		t.Fatal(err)
	}
	if l, _ := read("c_var1"); len(l) != 1 || !l[0].Etkin {
		t.Fatalf("etkin olmalı: %+v", l)
	}
}

func TestEkle_Idempotent(t *testing.T) {
	temizle(t)
	in := []Gorev{gorev("1", "echo a"), gorev("2", "echo b")}
	if s, err := Ekle("c_var1", in); err != nil || s.Eklenen != 2 {
		t.Fatalf("ilk: %+v %v", s, err)
	}
	s, err := Ekle("c_var1", in)
	if err != nil || s.Eklenen != 0 || s.Yinelenen != 2 || len(s.Reddedilen) != 0 {
		t.Fatalf("ikinci: %+v %v", s, err)
	}
	if l, _ := read("c_var1"); len(l) != 2 {
		t.Fatalf("depo büyüdü: %d", len(l))
	}
}

func TestEkle_GirdiIciTekrar(t *testing.T) {
	temizle(t)
	s, err := Ekle("c_var1", []Gorev{gorev("1", "echo a"), gorev("1", "echo a"), gorev("2", "echo a")})
	if err != nil || s.Eklenen != 2 || s.Yinelenen != 1 {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestEkle_GecersizReddedilirIdxDogru(t *testing.T) {
	temizle(t)
	kotuZaman := gorev("*/5 * * * *", "echo z") // alan içinde boşluk
	bos := gorev("1", "")
	uzun := gorev("2", strings.Repeat("a", maxKomut+1))
	satir := gorev("3", "echo a\nrm -rf /")
	eksik := Gorev{Dakika: "1", Komut: "x"}
	iyi := gorev("4", "echo ok")
	s, err := Ekle("c_var1", []Gorev{kotuZaman, bos, iyi, uzun, satir, eksik})
	if err != nil || s.Eklenen != 1 || len(s.Reddedilen) != 5 {
		t.Fatalf("%+v %v", s, err)
	}
	var idx []int
	for _, r := range s.Reddedilen {
		if r.Neden == "" {
			t.Errorf("neden boş: %+v", r)
		}
		idx = append(idx, r.Idx)
	}
	if fmt.Sprint(idx) != "[0 1 3 4 5]" {
		t.Fatalf("idx: %v", idx)
	}
}

func TestEkle_AskidaYazmazVeHataDoner(t *testing.T) {
	temizle(t)
	if err := os.MkdirAll(askiKok, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(askiYolu("c_var1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Ekle("c_var1", []Gorev{gorev("1", "echo a")})
	if !errors.Is(err, ErrAskida) || s.Eklenen != 0 {
		t.Fatalf("askıda hata bekleniyordu: %+v %v", s, err)
	}
	if varMi(depoYolu("c_var1")) || varMi(cronDYolu("c_var1")) {
		t.Fatal("askıdayken depo/cron.d yazıldı")
	}
}

func TestEkle_GecersizSK(t *testing.T) {
	temizle(t)
	for _, sk := range []string{"", "root", "c_", "../c_x", "c_A", "c_x/y"} {
		if _, err := Ekle(sk, []Gorev{gorev("1", "echo a")}); !errors.Is(err, errBad) {
			t.Errorf("%q: errBad bekleniyordu, %v", sk, err)
		}
	}
}

func TestEkle_GorevSiniri100(t *testing.T) {
	temizle(t)
	var ilk []Gorev
	for i := 0; i < maxGorev-2; i++ {
		ilk = append(ilk, gorev("1", fmt.Sprintf("echo %d", i)))
	}
	if s, err := Ekle("c_var1", ilk); err != nil || s.Eklenen != maxGorev-2 {
		t.Fatalf("%+v %v", s, err)
	}
	var yeni []Gorev
	for i := 0; i < 5; i++ {
		yeni = append(yeni, gorev("2", fmt.Sprintf("echo y%d", i)))
	}
	// depoda olan bir görev sınırda bile yinelenen sayılmalı (red değil)
	yeni = append(yeni, ilk[0])
	s, err := Ekle("c_var1", yeni)
	if err != nil || s.Eklenen != 2 || s.Yinelenen != 1 || len(s.Reddedilen) != 3 {
		t.Fatalf("%+v %v", s, err)
	}
	for _, r := range s.Reddedilen {
		if r.Neden != "görev sınırı (100)" || r.Idx < 2 || r.Idx > 4 {
			t.Errorf("red: %+v", r)
		}
	}
	if l, _ := read("c_var1"); len(l) != maxGorev {
		t.Fatalf("depo %d", len(l))
	}
}

func TestEkle_EklenenYoksaDepoyaDokunmaz(t *testing.T) {
	temizle(t)
	s, err := Ekle("c_var1", []Gorev{gorev("x y", "echo")})
	if err != nil || s.Eklenen != 0 || len(s.Reddedilen) != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	if varMi(depoYolu("c_var1")) {
		t.Fatal("hiç eklenmedi ama depo yazıldı")
	}
}

func TestZamanGecerli_Disa(t *testing.T) {
	if !ZamanGecerli("*/5") || ZamanGecerli("* *") || ZamanGecerli("?") {
		t.Fatal("ZamanGecerli")
	}
}

// Askıdaki hesapta eklenecek görev olmasa da (geçersiz / yinelenen / boş girdi)
// hata dönmeli: kapı yalnız yazma anında değil, baştan çalışır.
func TestEkle_AskidaYazmasizGirdideDeHata(t *testing.T) {
	temizle(t)
	if err := os.MkdirAll(askiKok, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(askiYolu("c_var1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, in := range [][]Gorev{nil, {gorev("x y", "echo")}} {
		if _, err := Ekle("c_var1", in); !errors.Is(err, ErrAskida) {
			t.Errorf("%v: ErrAskida bekleniyordu, %v", in, err)
		}
	}
}

// Zamanlayıcı satırındaki görev kimliği, depodan geri okunan görevinkiyle
// aynı olmalı (Tip varsayılanı "komut" Ekle'de de uygulanır); aksi halde
// cron-calistir görevi bulamaz.
func TestEkle_CronDKimligiGeriOkunanlaAyni(t *testing.T) {
	temizle(t)
	g := Gorev{Dakika: "7", Saat: "*", Gun: "*", Ay: "*", Hafta: "*", Komut: "echo kimlik"}
	if _, err := Ekle("c_var1", []Gorev{g}); err != nil {
		t.Fatal(err)
	}
	l, _ := read("c_var1")
	if len(l) != 1 {
		t.Fatal(l)
	}
	if icerik := dosya(t, cronDYolu("c_var1")); !strings.Contains(icerik, gorevKimlik("c_var1", l[0])) {
		t.Fatalf("cron.d kimliği geri okunanla uyuşmuyor:\n%s", icerik)
	}
}
