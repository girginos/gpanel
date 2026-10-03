package apianahtar

import (
	"reflect"
	"testing"
)

func TestSemaVeKimlikAyristir(t *testing.T) {
	u, _ := Uret()
	if d, ok := SemaMi("gosp bayi7:" + u.Tam); !ok || d != "bayi7:"+u.Tam {
		t.Fatalf("şema: %q %v", d, ok)
	}
	if _, ok := SemaMi("GOSP  bayi7:" + u.Tam); !ok {
		t.Fatal("şema harf duyarsız olmalı")
	}
	for _, b := range []string{"Bearer " + u.Tam, u.Tam, "gospx a:b", "gosp"} {
		if _, ok := SemaMi(b); ok {
			t.Errorf("şema olmamalı: %q", b)
		}
	}
	k, tam, ok := KimlikAyristir("bayi7:" + u.Tam)
	if !ok || k != "bayi7" || tam != u.Tam {
		t.Fatalf("ayrıştırma: %q %q %v", k, tam, ok)
	}
	for _, d := range []string{u.Tam, ":" + u.Tam, "bayi7:", "a b:" + u.Tam,
		string(make([]byte, 65)) + ":" + u.Tam} {
		if _, _, ok := KimlikAyristir(d); ok {
			t.Errorf("reddedilmeli: %q", d)
		}
	}
}

func TestKullaniciEsit(t *testing.T) {
	if !KullaniciEsit("Bayi7", "bayi7") || !KullaniciEsit("root", "ROOT") {
		t.Fatal("harf duyarsız eşleşmeli")
	}
	if KullaniciEsit("bayi7", "bayi8") || KullaniciEsit("bayi7", "bayi7 ") || KullaniciEsit("", "") {
		t.Fatal("farklı/boş ad eşleşmemeli")
	}
}

// FTP ve DB parolası AYRI izin: biri ötekinin ucunu açmaz; "oku" seçilemez;
// eski birleşik "parola" yeni anahtarda seçilemez.
func TestParolaIzinleriAyri(t *testing.T) {
	ftpU := "PUT " + p + "/domains/{id}/ftp/password"
	ftpEk := "PUT " + p + "/domains/{id}/ftp-accounts/{kullanici}/password"
	dbU := "PUT " + p + "/domains/{id}/databases/users/{kullanici}/password"
	for _, rol := range []string{RolKok, RolBayi} {
		ftp := []string{"ftp.parola:yaz"}
		db := []string{"db.parola:yaz"}
		for _, c := range []struct {
			desen   string
			izinler []string
			beklen  int
		}{
			{ftpU, ftp, KararIzinli}, {ftpEk, ftp, KararIzinli}, {dbU, ftp, KararIzinYetmez},
			{dbU, db, KararIzinli}, {ftpU, db, KararIzinYetmez}, {ftpEk, db, KararIzinYetmez},
			{ftpU, []string{"hesap:yaz"}, KararIzinYetmez},
			{"PUT " + p + "/databases/{dbid}/password", []string{"db.parola:yaz"}, KararUcKapali},
		} {
			if got := Karar(c.desen, rol, c.izinler); got != c.beklen {
				t.Errorf("%s %s %v: %d (beklenen %d)", rol, c.desen, c.izinler, got, c.beklen)
			}
		}
	}
	for _, kotu := range [][]string{{"ftp.parola:oku"}, {"db.parola:oku"}, {"parola:yaz"}} {
		if _, err := IzinleriKanonik(kotu, RolBayi); err == nil {
			t.Errorf("reddedilmeli: %v", kotu)
		}
	}
	got, err := IzinleriKanonik([]string{"db.parola:yaz", "ftp.parola:yaz", "hesap:oku"}, RolBayi)
	if err != nil || !reflect.DeepEqual(got, []string{"db.parola:yaz", "ftp.parola:yaz", "hesap:oku"}) {
		t.Fatalf("kanonik: %v %v", got, err)
	}
}

func TestIzinleriGocEskiParola(t *testing.T) {
	if got := IzinleriGoc([]string{"hesap:oku", "parola:yaz"}); !reflect.DeepEqual(got,
		[]string{"db.parola:yaz", "ftp.parola:yaz", "hesap:oku"}) {
		t.Fatalf("eski parola:yaz iki izne açılmalı: %v", got)
	}
	if got := IzinleriGoc([]string{"parola:oku", "dns:oku"}); !reflect.DeepEqual(got, []string{"dns:oku"}) {
		t.Fatalf("eski parola:oku hiçbir uca yetmiyordu → karşılığı yok: %v", got)
	}
	in := []string{"hesap:oku"}
	if got := IzinleriGoc(in); !reflect.DeepEqual(got, in) {
		t.Fatalf("eski izin yoksa dokunulmamalı: %v", got)
	}
}
