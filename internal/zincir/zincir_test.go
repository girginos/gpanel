package zincir

import (
	"testing"
	"time"
)

func ol(asama, seviye string) Olay { return Olay{Asama: asama, Seviye: seviye} }
func olY(asama, yol string, pid int, t time.Time) Olay {
	return Olay{Asama: asama, Yol: yol, Pid: pid, Zaman: t}
}

func TestTekAsamaYetersiz(t *testing.T) {
	if ZincirPuanla([]Olay{ol("dosya_yazma", "kritik"), ol("dosya_yazma", "uyari")}).Yeterli {
		t.Fatal("tek asama zincir sayildi")
	}
}

func TestBilinmeyenAsama(t *testing.T) {
	if ZincirPuanla([]Olay{ol("dosya_yazma", "uyari"), ol("zzz", "uyari")}).Yeterli {
		t.Fatal("bilinmeyen asama gecerli sayildi")
	}
}

// Salt ZAMANSAL 2 asama (nedensel yok) → 55 UYARI (FP-laundering onlenir).
func TestZamansal2Asama(t *testing.T) {
	s := ZincirPuanla([]Olay{ol("giris", "uyari"), ol("c2", "uyari")})
	if !s.Yeterli || s.Guven != 55 {
		t.Fatalf("guven=%d (55 bekleniyor)", s.Guven)
	}
	if s.Seviye != "uyari" || s.Nedensel {
		t.Fatalf("nedensel-yok uyari olmali: %+v", s)
	}
}

// NEDENSEL 2 asama (ayni yol = dusen dosya calistirildi) + sirali → 55+25+5=85 KRITIK.
func TestNedensel2Asama(t *testing.T) {
	t0 := time.Now()
	s := ZincirPuanla([]Olay{
		olY("dosya_yazma", "/home/c_x/public_html/shell.php", 0, t0),
		olY("calistirma", "/home/c_x/public_html/shell.php", 0, t0.Add(time.Second)),
	})
	if s.Guven != 85 || s.Seviye != "kritik" || !s.Nedensel {
		t.Fatalf("nedensel kritik 85 bekleniyor: %+v", s)
	}
}

func TestNedenselPid(t *testing.T) {
	t0 := time.Now()
	s := ZincirPuanla([]Olay{
		{Asama: "calistirma", Pid: 4242, Zaman: t0},
		{Asama: "c2", Pid: 4242, Zaman: t0.Add(time.Second)},
	})
	if !s.Nedensel {
		t.Fatal("ayni pid nedensel olmali")
	}
}

// TERS zaman sirasi → sirali bonusu YOK (nedensel de yok).
func TestTersSira(t *testing.T) {
	t0 := time.Now()
	s := ZincirPuanla([]Olay{
		olY("calistirma", "/tmp/a", 0, t0),
		olY("giris", "/var/b", 0, t0.Add(time.Minute)),
	})
	if s.Guven != 55 {
		t.Fatalf("ters sira guven=%d (55, +5 sirali OLMAMALI)", s.Guven)
	}
}

// 3 SIRALI asama, nedensel yok → 75 KRITIK (distinct>=3 && sirali).
func Test3SiraliAsama(t *testing.T) {
	t0 := time.Now()
	s := ZincirPuanla([]Olay{
		olY("giris", "/tmp/a", 0, t0),
		olY("dosya_yazma", "/var/b", 0, t0.Add(time.Second)),
		olY("calistirma", "/etc/c", 0, t0.Add(2*time.Second)),
	})
	if s.Guven != 75 || s.Seviye != "kritik" {
		t.Fatalf("3 sirali guven=%d seviye=%s (75 kritik bekleniyor)", s.Guven, s.Seviye)
	}
}

func TestYolBagli(t *testing.T) {
	if !yolBagli("/home/c/x/a.php", "/home/c/x/a.php") {
		t.Fatal("ayni yol bagli olmali")
	}
	if yolBagli("/home/c/x/a", "/home/c/x/b") {
		t.Fatal("farkli dosya (ayni dizin) bagli sayildi — FP riski")
	}
	if yolBagli("/home/c/x/a", "/tmp/b") {
		t.Fatal("farkli dizin bagli sayildi")
	}
	if yolBagli("/a", "/b") {
		t.Fatal("kok dizin bagli sayildi")
	}
	if yolBagli("", "/x") {
		t.Fatal("bos yol bagli sayildi")
	}
}

func TestZincirImza(t *testing.T) {
	a := ZincirImza(5, []string{"dosya_yazma", "calistirma"})
	if a != ZincirImza(5, []string{"dosya_yazma", "calistirma"}) {
		t.Fatal("kararsiz imza")
	}
	if a == ZincirImza(6, []string{"dosya_yazma", "calistirma"}) || a == ZincirImza(5, []string{"dosya_yazma", "c2"}) {
		t.Fatal("imza duyarsiz")
	}
	if len(a) != 32 {
		t.Fatalf("uzunluk %d", len(a))
	}
}

func TestAsamaOzet(t *testing.T) {
	if AsamaOzet([]string{"dosya_yazma", "calistirma", "c2"}) != "File Write → Execution → C2" {
		t.Fatal("ozet yanlis")
	}
}
