// Package sahtedb — YALNIZ testler için bellek içi database/sql sürücüsü.
//
// Gerçek SQL çalıştırmaz: sorgu metni kayıtlı bir parçayı içeriyorsa o parçanın hazır
// satırlarını döner, Exec'leri (argümanlarıyla) kaydeder. Amaç, handler'ın hangi sorguya
// ULAŞTIĞINI ve ne yazdığını sınamaktır (ör. yanlış kapıdaki girişin 2FA durumuna hiç
// inmediği). Üretim kodu bu paketi içe aktarmaz.
package sahtedb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// DB: sahte veritabanının durumu (hazır yanıtlar + kayıtlar).
type DB struct {
	mu         sync.Mutex
	yanitlar   []yanit
	sorgulanan []string
	yurutulen  []string
}

type yanit struct {
	parca    string
	satirlar [][]driver.Value
}

// Yeni: boş sahte veritabanı ve ona bağlı *sql.DB.
func Yeni() (*DB, *sql.DB) {
	s := &DB{}
	return s, sql.OpenDB(baglayici{s})
}

// Satirlar: sorgu metni parca'yı içerirse bu satırlar döner (ilk eklenen eşleşme kazanır;
// hiçbiri eşleşmezse sorgu satırsız döner → Scan sql.ErrNoRows).
func (s *DB) Satirlar(parca string, satirlar ...[]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	y := yanit{parca: parca}
	for _, satir := range satirlar {
		d := make([]driver.Value, len(satir))
		for i, v := range satir {
			d[i] = v
		}
		y.satirlar = append(y.satirlar, d)
	}
	s.yanitlar = append(s.yanitlar, y)
}

// Sorgulandi: metni parca'yı içeren bir sorgu yapıldı mı?
func (s *DB) Sorgulandi(parca string) bool { return s.icerir(&s.sorgulanan, parca) }

// Yurutuldu: metni ya da argümanları parca'yı içeren bir Exec yapıldı mı?
func (s *DB) Yurutuldu(parca string) bool { return s.icerir(&s.yurutulen, parca) }

func (s *DB) icerir(liste *[]string, parca string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range *liste {
		if strings.Contains(q, parca) {
			return true
		}
	}
	return false
}

type baglayici struct{ s *DB }

func (b baglayici) Connect(context.Context) (driver.Conn, error) { return baglanti(b), nil }
func (b baglayici) Driver() driver.Driver                        { return surucu(b) }

type surucu struct{ s *DB }

func (d surucu) Open(string) (driver.Conn, error) { return baglanti(d), nil }

type baglanti struct{ s *DB }

func (baglanti) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("sahtedb: Prepare desteklenmez")
}
func (baglanti) Close() error              { return nil }
func (baglanti) Begin() (driver.Tx, error) { return nil, errors.New("sahtedb: işlem desteklenmez") }

func (c baglanti) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.s.sorgulanan = append(c.s.sorgulanan, q)
	for _, y := range c.s.yanitlar {
		if strings.Contains(q, y.parca) {
			return &satirlar{veri: y.satirlar}, nil
		}
	}
	return &satirlar{}, nil
}

func (c baglanti) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	degerler := make([]any, len(args))
	for i, a := range args {
		degerler[i] = a.Value
	}
	c.s.yurutulen = append(c.s.yurutulen, q+" | "+fmt.Sprint(degerler...))
	return driver.RowsAffected(1), nil
}

type satirlar struct {
	veri [][]driver.Value
	i    int
}

func (r *satirlar) Columns() []string {
	n := 0
	if len(r.veri) > 0 {
		n = len(r.veri[0])
	}
	adlar := make([]string, n)
	for i := range adlar {
		adlar[i] = fmt.Sprintf("k%d", i)
	}
	return adlar
}

func (r *satirlar) Close() error { return nil }

func (r *satirlar) Next(dest []driver.Value) error {
	if r.i >= len(r.veri) {
		return io.EOF
	}
	copy(dest, r.veri[r.i])
	r.i++
	return nil
}
