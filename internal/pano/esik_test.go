package pano

import (
	"reflect"
	"testing"
)

// Kova + histerezis: 70→86→101→97→94→86→82→79→88 dizisi tam olarak [85,100,85]
// bildirimi üretmeli (97'de 100'de kalır, 94'te 85'e iner ama bildirim yok,
// 82'de 85'te kalır, 79'da sıfırlanır, 88'de yeniden 85).
func TestKotaKovasiHisterezis(t *testing.T) {
	seviye := 0
	var bildirim []int
	for _, y := range []float64{70, 86, 101, 97, 94, 86, 82, 79, 88} {
		yeni := kotaKovasi(seviye, y)
		if yukariGecti(seviye, yeni) {
			bildirim = append(bildirim, yeni)
		}
		seviye = yeni
	}
	if want := []int{85, 100, 85}; !reflect.DeepEqual(bildirim, want) {
		t.Fatalf("bildirim dizisi %v, beklenen %v", bildirim, want)
	}
}

func TestKotaKovasiSinirlar(t *testing.T) {
	cases := []struct {
		onceki int
		yuzde  float64
		want   int
	}{
		{0, 84.9, 0}, {0, 85, 85}, {0, 100, 100}, {85, 80, 85}, {85, 79.9, 0},
		{100, 95, 100}, {100, 94.9, 85}, {100, 50, 0},
	}
	for _, c := range cases {
		if got := kotaKovasi(c.onceki, c.yuzde); got != c.want {
			t.Errorf("kotaKovasi(%d, %v) = %d, beklenen %d", c.onceki, c.yuzde, got, c.want)
		}
	}
}

func TestSSLSeviyesi(t *testing.T) {
	seviye := sslNormal
	var bildirim []int
	// 30 → 14 (yakın) → 10 → 3 (kritik) → -1 (doldu) → yenilendi 89 → 15 (iniş yok) → 14 (yakın)
	for _, g := range []int{30, 14, 10, 3, -1, 89, 60, 15, 14} {
		yeni := sslSeviyesi(seviye, g)
		if yukariGecti(seviye, yeni) {
			bildirim = append(bildirim, yeni)
		}
		seviye = yeni
	}
	if want := []int{sslYakin, sslKritik, sslDoldu, sslYakin}; !reflect.DeepEqual(bildirim, want) {
		t.Fatalf("ssl bildirim dizisi %v, beklenen %v", bildirim, want)
	}
	// Yenilenmeden 15. güne "geri sayım" iniş sayılmaz.
	if got := sslSeviyesi(sslYakin, 16); got != sslYakin {
		t.Errorf("16 gün: seviye korunmalı, %d", got)
	}
}
