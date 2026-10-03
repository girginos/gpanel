package kapi

import "testing"

func TestAnahtarUyumluMatrisi(t *testing.T) {
	cases := []struct {
		kapi      string
		kok, bayi bool
	}{
		{"", true, true},
		{Eski, true, true},
		{Admin, true, false},
		{Bayi, false, true},
		{Musteri, false, false},
		{Bilinmeyen, false, false},
	}
	for _, c := range cases {
		if AnahtarUyumlu(c.kapi, "admin") != c.kok || AnahtarUyumlu(c.kapi, "reseller") != c.bayi {
			t.Errorf("kapı %q: kök=%v bayi=%v bekleniyordu", c.kapi, c.kok, c.bayi)
		}
		if AnahtarUyumlu(c.kapi, "user") {
			t.Errorf("kapı %q: müşteri rolü anahtar sahibi olamaz", c.kapi)
		}
	}
}
