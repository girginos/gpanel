package git

// zamanasimi_test.go — denetim-b: git clone/fetch/pull takılırsa (yavaş/ölü uzak depo)
// süreç ve bekleyen HTTP/webhook istek goroutine'i SONSUZA DEK kalmamalı.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKiraciCalistirGercek_ZamanAsimindaSonlanir(t *testing.T) {
	s := sahteKomutOrtamiKur(t)
	// takılan git: sahte runuser kendini uzun bir sleep ile DEĞİŞTİRİR (exec → aynı pid).
	bin := filepath.Dir(filepath.Join(s.kayit, "..", "bin", "runuser"))
	betik := "#!/bin/sh\nexec sleep 60\n"
	if err := os.WriteFile(filepath.Join(bin, "runuser"), []byte(betik), 0o755); err != nil {
		t.Fatal(err)
	}
	eski := gitZamanAsimi
	gitZamanAsimi = 300 * time.Millisecond
	defer func() { gitZamanAsimi = eski }()

	basla := time.Now()
	_, _, err := kiraciCalistirGercek(testSK, s.cwd, nil, "git", "clone")
	if err == nil {
		t.Fatal("takılan komut hata ile bitmeli")
	}
	if gecen := time.Since(basla); gecen > 10*time.Second {
		t.Fatalf("zaman aşımı işlemedi: %v", gecen)
	}
}
