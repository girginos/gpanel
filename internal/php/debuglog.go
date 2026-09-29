package php

// debuglog.go — GET/DELETE /domains/{id}/php/debug-log icin PHP debug log'un
// (/home/<sk>/.gpanel/php_debug.log) SYMLINK/TOCTOU-guvenli okunmasi ve kesilmesi.
//
// TEHDIT: panel root calisir; /home/<sk> kiraci-sahipli (c_X:c_X 0710) GUVENILMEZ
// ust dizindir. Eski kod sabit yolu os.Open / os.OpenFile(O_TRUNC) ile aciyordu —
// ikisi de symlink izler: kiraci ~/.gpanel ya da ~/.gpanel/php_debug.log yerine
// symlink (ya da kendi dizinini / hardlink / FIFO) koyarak root'a KEYFI dosyanin son
// 200 satirini okutabiliyor (komsu kiraci / sistem sirlari), keyfi dosyayi sifira
// KESTIREBILIYOR ve istek goroutine'ini asabiliyordu. Yazma tarafi
// (provisioner.installDebugShim) ayni tehdidi dir-fd + O_NOFOLLOW ile zaten kapatir;
// burasi AYNI modeli okuma/kesme tarafina uygular:
//
//  1. ev: open(O_DIRECTORY|O_NOFOLLOW) → pinlenmis dir-fd; sahibi = kiraci uid.
//  2. .gpanel: openat(evFd, O_DIRECTORY|O_NOFOLLOW) + Fstat → root sahipli ve
//     grup/digerleri YAZAMAZ olmali (yazici degismezi root:root 0755). Boylece
//     icindeki girdileri yalniz root degistirebilir.
//  3. php_debug.log: openat(gpFd, O_NOFOLLOW|O_NONBLOCK) + Fstat → DUZENLI dosya,
//     sahibi ev sahibi (kiraci) uid, tek bag (Nlink==1: hardlink'le baska dosya degil).
//  4. Kesme YOLA gore degil, dogrulanmis fd uzerinden ftruncate ile (O_TRUNC ile
//     acmak dogrulamadan ONCE keserdi).
//
// Her adim bir oncekinin fd'sine goredir → kontrol ile kullanim arasinda yol yeniden
// cozulmez (TOCTOU yok). Herhangi bir kosul tutmazsa dosyaya DOKUNULMAZ.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	debugLogDizinAdi = ".gpanel"
	debugLogDosyaAdi = "php_debug.log"
)

var errDebugLogGuvensiz = errors.New("debug log güvensiz (symlink/sahiplik/tür doğrulaması başarısız)")

// debugLogAc: ev altindaki .gpanel/php_debug.log'u symlink IZLEMEDEN acar ve
// dogrular. erisim: unix.O_RDONLY ya da unix.O_WRONLY (O_TRUNC/O_CREAT VERILMEZ —
// kesme dogrulamadan SONRA fd uzerinden yapilir, dosya asla yaratilmaz).
func debugLogAc(ev string, erisim int) (*os.File, error) {
	evFd, err := unix.Open(ev, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(evFd)
	var evSt unix.Stat_t
	if err := unix.Fstat(evFd, &evSt); err != nil {
		return nil, err
	}

	gpFd, err := unix.Openat(evFd, debugLogDizinAdi, unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(gpFd)
	var gpSt unix.Stat_t
	if err := unix.Fstat(gpFd, &gpSt); err != nil {
		return nil, err
	}
	if gpSt.Uid != 0 {
		return nil, errDebugLogGuvensiz // kiraci-sahipli .gpanel: ic yapisi kiraci kontrolunde
	}
	if gpSt.Mode&0o022 != 0 {
		return nil, errDebugLogGuvensiz // grup/digerleri (ya da ACL maskesi) yazabilir
	}

	fd, err := unix.Openat(gpFd, debugLogDosyaAdi,
		erisim|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err // ELOOP: symlink; ENOENT: debug hic tetiklenmemis
	}
	var fSt unix.Stat_t
	if err := unix.Fstat(fd, &fSt); err != nil {
		unix.Close(fd)
		return nil, err
	}
	guvensiz := fSt.Mode&unix.S_IFMT != unix.S_IFREG || // FIFO/aygit/dizin: yalniz duzenli dosya
		fSt.Uid != evSt.Uid || // sahip kiraci degil (root/komsu dosyasi)
		fSt.Nlink != 1 // hardlink: baska yerdeki bir dosyanin ikinci adi
	if guvensiz {
		unix.Close(fd)
		return nil, errDebugLogGuvensiz
	}
	return os.NewFile(uintptr(fd), ev+"/"+debugLogDizinAdi+"/"+debugLogDosyaAdi), nil
}

// debugLogSatirlar: ev'deki debug log'un son 200 satiri. Dosya yok / guvensiz → bos.
// DoS-guvenli: tum dosya bellege okunmaz; yalniz dosya SONUNDAN son ~64KB okunur.
func debugLogSatirlar(ev string) []string {
	f, err := debugLogAc(ev, unix.O_RDONLY)
	if err != nil {
		return []string{}
	}
	defer f.Close()
	const tailBytes = 64 * 1024
	var data []byte
	if st, serr := f.Stat(); serr == nil && st.Size() > tailBytes {
		buf := make([]byte, tailBytes)
		if _, e := f.ReadAt(buf, st.Size()-tailBytes); e == nil || e == io.EOF {
			if i := bytes.IndexByte(buf, '\n'); i >= 0 {
				buf = buf[i+1:] // ilk kismi satiri at
			}
			data = buf
		}
	} else {
		data, _ = io.ReadAll(io.LimitReader(f, tailBytes))
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	const maxLines = 200
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

// debugLogKes: ev'deki debug log'u dogrulanmis fd uzerinden sifira keser.
// Dosya yok / guvensiz → hata doner, HICBIR dosyaya dokunulmaz.
func debugLogKes(ev string) error {
	f, err := debugLogAc(ev, unix.O_WRONLY)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Truncate(0)
}
