package php

// subpool_aski_test.go — alt alan silinince hesap askısının saklandığı havuz
// kopyası (<ad>.conf.askida) da gider: kalırsa aynı id'li havuz askıdan alınınca
// geri yüklenirdi. Havuz dizinleri t.TempDir (KurulSurumler dikişi); yüklü
// havuz (.conf) KONMAZ → reload komutu çalışmaz, php-fpm'e dokunulmaz.

import (
	"os"
	"path/filepath"
	"testing"

	"girginospanel/internal/provisioner"
)

func TestRemoveNamedPool_AskidaKopyasiDaSilinir(t *testing.T) {
	eski := KurulSurumler
	t.Cleanup(func() { KurulSurumler = eski })
	d1, d2 := t.TempDir(), t.TempDir()
	KurulSurumler = []Surum{{Surum: "8.3", PoolDir: d1, Service: "php-fpm"}, {Surum: "8.2", PoolDir: d2, Service: "php82-php-fpm"}}
	ad := SubPoolName("c_rnp", 7)
	sakli := filepath.Join(d2, ad+".conf"+provisioner.AskiHavuzEki)
	komsu := filepath.Join(d1, SubPoolName("c_rnp", 8)+".conf"+provisioner.AskiHavuzEki)
	for _, y := range []string{sakli, komsu} {
		if err := os.WriteFile(y, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	RemoveNamedPool(ad)
	if _, err := os.Lstat(sakli); err == nil {
		t.Fatal("silinen alt alanın askıdaki havuz kopyası kalmamalı")
	}
	if _, err := os.Lstat(komsu); err != nil {
		t.Fatal("başka alt alanın havuzuna dokunulmamalı")
	}
}
