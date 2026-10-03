package efektiflimit

import (
	"os"
	"strings"
	"testing"
)

func bosluksuz(s string) string { return strings.Join(strings.Fields(s), " ") }

// Göç 0120'deki CREATE ile TabloDDL aynı olmalı (testlerin şeması canlıdan sapmasın).
func TestTabloDDL_GocIleAyni(t *testing.T) {
	b, err := os.ReadFile("../../migrations/0120_hosting_limit_ust.sql")
	if err != nil {
		t.Fatal(err)
	}
	m := string(b)
	i := strings.Index(m, "CREATE TABLE IF NOT EXISTS hosting_limit_ust")
	if i < 0 {
		t.Fatal("göçte hosting_limit_ust CREATE yok")
	}
	j := strings.Index(m[i:], ";")
	if got, want := bosluksuz(m[i:i+j]), bosluksuz(TabloDDL); got != want {
		t.Fatalf("TabloDDL göçten sapmış:\ngöç:  %s\nddl:  %s", got, want)
	}
	// Her üst yazılabilir alanın sütunu tabloda var.
	for _, a := range Alanlar {
		if !strings.Contains(TabloDDL, " "+a.Sutun+" ") {
			t.Errorf("hosting_limit_ust'ta %s sütunu yok", a.Sutun)
		}
	}
}
