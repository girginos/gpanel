package subdomain

import "testing"

func TestSubNginxDogrula_SayisalAlanSinirlari(t *testing.T) {
	if err := subNginxDogrula(subNginxDefaults()); err != nil {
		t.Fatalf("varsayılan reddedildi: %v", err)
	}
	kotu := []func(*subNginx){
		func(n *subNginx) { n.FastcgiCacheDakika = -1 },
		func(n *subNginx) { n.FastcgiCacheDakika = 1 << 40 },
		func(n *subNginx) { n.BrowserCacheGun = -5 },
		func(n *subNginx) { n.HSTSMaxAge = -1 },
		func(n *subNginx) { n.ClientMaxBodyMB = 1 << 30 },
	}
	for i, f := range kotu {
		n := subNginxDefaults()
		f(&n)
		if subNginxDogrula(n) == nil {
			t.Errorf("durum %d kabul edildi", i)
		}
	}
}
