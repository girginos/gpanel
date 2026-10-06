package hesaplar

// ftp_oturum_test.go — FTP açık oturum düşürme: gerçek pure-ftpd YOK; pure-ftpwho
// çıktısı ve SIGTERM dikişle sahte. Kanıtlanan: doğru kullanıcının PID'leri,
// BAŞKA kullanıcıya sinyal yok (ön ek / benzer ad dahil), bozuk ad reddi (komut bile
// çalışmaz), kısmi hata, liste hatası.

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"sort"
	"testing"
)

const ftpWhoCikti = "" +
	"101|c_a|12|ACTIVE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"102|c_ab|12|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"103|c_a-ek|12|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"104|c_b|12|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"105|c_a|40|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"106|c_a\\|x|40|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"1|c_a|40|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"abc|c_a|40|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"107|C_A|40|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"108|c_a-|40|IDLE|x|1.2.3.4|5.6.7.8|21|0|0|0|0\n" +
	"saçma satır\n\n"

type ftpSahte struct {
	whoCagri int
	oldu     []int
	killErr  map[int]error
	whoErr   bool
}

func ftpSahteKur(t *testing.T, f *ftpSahte) {
	t.Helper()
	eW, eK := ftpWhoKomut, ftpOturumSonlandir
	t.Cleanup(func() { ftpWhoKomut, ftpOturumSonlandir = eW, eK })
	ftpWhoKomut = func(ctx context.Context) *exec.Cmd {
		f.whoCagri++
		if f.whoErr {
			return exec.CommandContext(ctx, "false")
		}
		return exec.CommandContext(ctx, "printf", "%s", ftpWhoCikti)
	}
	ftpOturumSonlandir = func(pid int) error {
		f.oldu = append(f.oldu, pid)
		return f.killErr[pid]
	}
}

func ftpSirali(p []int) []int { sort.Ints(p); return p }

func TestFTPOturumlariDusur_DogruKullaniciyaYalnizOna(t *testing.T) {
	f := &ftpSahte{}
	ftpSahteKur(t, f)
	n, err := FTPOturumlariDusur("c_a")
	if err != nil || n != 2 || !reflect.DeepEqual(ftpSirali(f.oldu), []int{101, 105}) {
		t.Fatalf("c_a: n=%d err=%v oldu=%v (yalnız 101,105 beklenirdi; c_ab/c_a-ek/c_b'ye dokunulmamalı)", n, err, f.oldu)
	}
	f.oldu = nil
	n, err = FTPOturumlariDusur("c_a-ek")
	if err != nil || n != 1 || !reflect.DeepEqual(f.oldu, []int{103}) {
		t.Fatalf("ek hesap: n=%d err=%v oldu=%v", n, err, f.oldu)
	}
}

func TestFTPHostingOturumlariDusur_AnaVeEkAmaBaskasiDegil(t *testing.T) {
	f := &ftpSahte{}
	ftpSahteKur(t, f)
	n, err := FTPHostingOturumlariDusur("c_a")
	if err != nil || n != 3 || !reflect.DeepEqual(ftpSirali(f.oldu), []int{101, 103, 105}) {
		t.Fatalf("hosting: n=%d err=%v oldu=%v (c_ab=102, c_b=104 DÜŞMEMELİ)", n, err, f.oldu)
	}
}

func TestFTPOturumlariDusur_BozukAdReddedilirKomutCalismaz(t *testing.T) {
	f := &ftpSahte{}
	ftpSahteKur(t, f)
	for _, kotu := range []string{"", "c_a;id", "c_a|c_b", "../c_a", "C_A", "root", "c_", "c_a-", "c_a b", "c_a\n",
		"$(id)", "c_a\x00", "c_" + string(make([]byte, 70)), "-c_a", "c_a-EK"} {
		if _, err := FTPOturumlariDusur(kotu); err == nil {
			t.Errorf("FTPOturumlariDusur(%q) reddedilmeli", kotu)
		}
	}
	for _, kotu := range []string{"", "c_a-ek", "root", "c_a;id", "c_a/../x"} {
		if _, err := FTPHostingOturumlariDusur(kotu); err == nil {
			t.Errorf("FTPHostingOturumlariDusur(%q) reddedilmeli", kotu)
		}
	}
	if f.whoCagri != 0 || len(f.oldu) != 0 {
		t.Fatalf("geçersiz adla komut/sinyal çalıştı: who=%d oldu=%v", f.whoCagri, f.oldu)
	}
}

func TestFTPOturumlariDusur_KismiHataDigerleriniDener(t *testing.T) {
	f := &ftpSahte{killErr: map[int]error{101: errors.New("EPERM")}}
	ftpSahteKur(t, f)
	n, err := FTPOturumlariDusur("c_a")
	if err == nil || n != 1 || !reflect.DeepEqual(ftpSirali(f.oldu), []int{101, 105}) {
		t.Fatalf("kısmi hata: n=%d err=%v oldu=%v", n, err, f.oldu)
	}
}

func TestFTPOturumlariDusur_ListeHatasiDondurur(t *testing.T) {
	f := &ftpSahte{whoErr: true}
	ftpSahteKur(t, f)
	if n, err := FTPOturumlariDusur("c_a"); err == nil || n != 0 || len(f.oldu) != 0 {
		t.Fatalf("liste hatası: n=%d err=%v oldu=%v", n, err, f.oldu)
	}
}
