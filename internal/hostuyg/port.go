package hostuyg

// Port havuzu — 7000-7999 aralığında dinamik port rezervasyonu.
//
// Rezervasyon iki katmanlıdır:
//   1. DB'de cp_host_uyg_portlari (UNIQUE(port, protokol)) — kalıcı kayıt
//   2. `ss -tln` / `ss -uln` — canlı check (başka bir servis o portu tuttuysa)
//
// Tarif.Portlar.Zorunlu != 0 ise sabit port istenir (WG=51820 gibi); yasak
// listede veya meşgul ise hata döner. Aksi halde havuzdan boş port bulunur.

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	HavuzBaslangic = 7000
	HavuzBitis     = 7999
)

// YasakliPortlar — havuz içinde bile bu portlara dokunma. (Diğer servisler
// bu aralığa sızarsa kırılmasın diye reserve tutulan portlar).
var YasakliPortlar = map[int]bool{
	7080: true, // LiteSpeed admin
	7443: true, // alternate HTTPS
}

// PortAyir — bir uygulama için portları rezerv et.
//   - Zorunlu != 0 ise o portu al (meşgul/yasak ise hata)
//   - Aksi halde havuzdan ilk boş TCP/UDP portu
//
// Dönen: [ad → port] map (Tarif.Portlar.Ad → gerçek port).
func PortAyir(ctx context.Context, db *sql.DB, uygulamaID int64, portlar []PortTarifi) (map[string]int, error) {
	sonuc := make(map[string]int, len(portlar))
	kullanilan := map[int]bool{} // bu çağrı içinde aynı porta çift-atama olmasın

	for _, pt := range portlar {
		var port int
		if pt.Zorunlu != 0 {
			// Sabit port
			if YasakliPortlar[pt.Zorunlu] {
				return nil, fmt.Errorf("port %d yasak listede (recipe zorunlu)", pt.Zorunlu)
			}
			if portMesgulSistemde(pt.Zorunlu) {
				return nil, fmt.Errorf("port %d sistemde başka bir servis tarafından kullanılıyor", pt.Zorunlu)
			}
			if portMesgulDBde(ctx, db, pt.Zorunlu, pt.Protokol) {
				return nil, fmt.Errorf("port %d başka bir gpanel uygulaması tarafından rezerve edilmiş", pt.Zorunlu)
			}
			port = pt.Zorunlu
		} else {
			// Havuzdan boş bul
			p, err := havuzdanBul(ctx, db, pt.Protokol, kullanilan)
			if err != nil {
				return nil, err
			}
			port = p
		}
		sonuc[pt.Ad] = port
		kullanilan[port] = true

		// DB'ye INSERT
		_, err := db.ExecContext(ctx,
			`INSERT INTO cp_host_uyg_portlari (uygulama_id, port, protokol, aciklama)
			 VALUES (?, ?, ?, ?)`,
			uygulamaID, port, pt.Protokol, pt.Ad)
		if err != nil {
			return nil, fmt.Errorf("port %d DB rezerv hatası: %w", port, err)
		}
	}
	return sonuc, nil
}

// PortSerbestBirak — uygulama silinirken portları temizle (CASCADE zaten DB'de,
// ama defensive olarak da çağrılabilir).
func PortSerbestBirak(ctx context.Context, db *sql.DB, uygulamaID int64) error {
	_, err := db.ExecContext(ctx,
		`DELETE FROM cp_host_uyg_portlari WHERE uygulama_id=?`, uygulamaID)
	return err
}

func havuzdanBul(ctx context.Context, db *sql.DB, protokol string, kullanilanCagri map[int]bool) (int, error) {
	// DB'de zaten kullanılan portları çek
	rows, err := db.QueryContext(ctx,
		`SELECT port FROM cp_host_uyg_portlari WHERE protokol=? OR protokol='both'`, protokol)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	dbKullanilan := map[int]bool{}
	for rows.Next() {
		var p int
		if rows.Scan(&p) == nil {
			dbKullanilan[p] = true
		}
	}

	// Sistem port set (ss -tln/-uln)
	sysKullanilan := sistemPortlari(protokol)

	for p := HavuzBaslangic; p <= HavuzBitis; p++ {
		if YasakliPortlar[p] || dbKullanilan[p] || sysKullanilan[p] || kullanilanCagri[p] {
			continue
		}
		return p, nil
	}
	return 0, errors.New("port havuzunda (7000-7999) boş port kalmadı")
}

func portMesgulSistemde(port int) bool {
	return sistemPortlari("tcp")[port] || sistemPortlari("udp")[port]
}

func portMesgulDBde(ctx context.Context, db *sql.DB, port int, protokol string) bool {
	var n int
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM cp_host_uyg_portlari
		 WHERE port=? AND (protokol=? OR protokol='both' OR ?='both')`,
		port, protokol, protokol).Scan(&n)
	return n > 0
}

// sistemPortlari — `ss -tlnH` (tcp) veya `ss -ulnH` (udp) çıktısını parse.
func sistemPortlari(protokol string) map[int]bool {
	set := map[int]bool{}
	flag := "-tlnH"
	if protokol == "udp" {
		flag = "-ulnH"
	}
	out, err := exec.Command("ss", flag).Output()
	if err != nil {
		return set
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// tcp: Recv-Q Send-Q Local Peer ...  → f[3] = local
		// udp: State  Recv-Q Send-Q Local Peer ... → udp `ss -uln` farklı yapı olabilir
		var local string
		if len(f) >= 4 {
			local = f[3]
		}
		if local == "" {
			continue
		}
		if i := strings.LastIndex(local, ":"); i > 0 {
			if p, e := strconv.Atoi(local[i+1:]); e == nil {
				set[p] = true
			}
		}
	}
	return set
}
