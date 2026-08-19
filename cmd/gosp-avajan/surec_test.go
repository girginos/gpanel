package main

import (
	"encoding/binary"
	"testing"
)

// /proc/<pid>/stat: comm BOŞLUK + PARANTEZ içerebilir (klasik ayrıştırma tuzağı).
func TestStatAyristir(t *testing.T) {
	ppid, comm := statAyristir("1234 (bad (name) proc) S 987 1234 1234 0 -1 ...")
	if comm != "bad (name) proc" {
		t.Fatalf("comm=%q", comm)
	}
	if ppid != 987 {
		t.Fatalf("ppid=%d (987 bekleniyor)", ppid)
	}
}

func TestStatAyristirBasit(t *testing.T) {
	ppid, comm := statAyristir("42 (php-fpm) S 1 42 42 0 -1")
	if comm != "php-fpm" || ppid != 1 {
		t.Fatalf("comm=%q ppid=%d", comm, ppid)
	}
}

func TestSurecPuanlaWebKabuk(t *testing.T) {
	if p, a := surecPuanla("php-fpm", "bash", "/bin/bash", 1000); p != 30 {
		t.Fatalf("php-fpm→bash puan=%d (%s)", p, a)
	}
}

func TestSurecPuanlaWebIndirici(t *testing.T) {
	if p, _ := surecPuanla("nginx", "curl", "/usr/bin/curl", 1000); p != 20 {
		t.Fatalf("nginx→curl puan=%d", p)
	}
}

func TestSurecPuanlaTmpExec(t *testing.T) {
	if p, _ := surecPuanla("bash", "a.out", "/tmp/a.out", 1000); p != 30 {
		t.Fatalf("/tmp exec puan=%d", p)
	}
}

// Negatif kontroller: normal exec'ler puan ÜRETMEMELİ.
func TestSurecPuanlaNormal(t *testing.T) {
	if p, _ := surecPuanla("systemd", "bash", "/bin/bash", 0); p != 0 {
		t.Fatalf("root systemd→bash yanlış puan=%d", p)
	}
	if p, _ := surecPuanla("php-fpm", "convert", "/usr/bin/convert", 1000); p != 0 {
		t.Fatalf("php-fpm→convert (meşru) yanlış puan=%d", p)
	}
	if p, _ := surecPuanla("bash", "ls", "/usr/bin/ls", 1000); p != 0 {
		t.Fatalf("bash→ls yanlış puan=%d", p)
	}
}

func TestDunyaYazilir(t *testing.T) {
	if !dunyaYazilir("/tmp/x") || !dunyaYazilir("/dev/shm/y") || !dunyaYazilir("/var/tmp/z") {
		t.Fatal("tmp/shm/var-tmp yazılabilir olmalı")
	}
	if dunyaYazilir("/usr/bin/curl") || dunyaYazilir("/home/c_x/public_html/a.php") {
		t.Fatal("normal yol yanlışlıkla dünya-yazılabilir sayıldı")
	}
}

// Sentetik netlink tamponundan EXEC pid çıkarımı.
func TestExecPidleri(t *testing.T) {
	buf := make([]byte, 16+20+16+8) // nlmsghdr + cn_msg + proc_event(what,cpu,ts) + pid,tgid
	binary.LittleEndian.PutUint32(buf[0:], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[36:], procEventExec) // proc_event.what @ 16+20
	binary.LittleEndian.PutUint32(buf[52:], 4242)          // process_pid @ 36+16
	pids := execPidleri(buf)
	if len(pids) != 1 || pids[0] != 4242 {
		t.Fatalf("EXEC pid çıkarılamadı: %v", pids)
	}
}

// EXEC olmayan (ör. FORK) olay pid üretmemeli.
func TestExecPidleriExecDegil(t *testing.T) {
	buf := make([]byte, 16+20+16+8)
	binary.LittleEndian.PutUint32(buf[0:], uint32(len(buf)))
	binary.LittleEndian.PutUint32(buf[36:], 0x00000001) // FORK
	binary.LittleEndian.PutUint32(buf[52:], 4242)
	if pids := execPidleri(buf); len(pids) != 0 {
		t.Fatalf("EXEC olmayan olaydan pid çıktı: %v", pids)
	}
}
