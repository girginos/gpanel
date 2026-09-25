package istatistik

// gunluk_test.go — "son 7 gün" grafiği GERÇEK TARİHE göre seçilir. Eski kod
// "dd/Mon/yyyy" anahtarlarını METİN olarak sıralıyordu: ay/yıl sınırında en yeni
// günler atılıp eski ayın günleri "son 7 gün" diye gösteriliyordu.

import (
	"strings"
	"testing"
)

func gunMetni(l []Gun) string {
	var s []string
	for _, g := range l {
		s = append(s, g.Tarih)
	}
	return strings.Join(s, " ")
}

func gunHaritasi(gunler ...string) map[string]int {
	m := map[string]int{}
	for i, g := range gunler {
		m[g] = i + 1
	}
	return m
}

func TestSonGunler_AySiniri(t *testing.T) {
	m := gunHaritasi("25/Sep/2026", "26/Sep/2026", "27/Sep/2026", "28/Sep/2026", "29/Sep/2026",
		"30/Sep/2026", "01/Oct/2026", "02/Oct/2026", "03/Oct/2026")
	got := gunMetni(sonGunler(m, 7))
	want := "27/Sep/2026 28/Sep/2026 29/Sep/2026 30/Sep/2026 01/Oct/2026 02/Oct/2026 03/Oct/2026"
	if got != want {
		t.Fatalf("ay sınırı:\n gelen %s\n beklenen %s", got, want)
	}
}

func TestSonGunler_YilSiniri(t *testing.T) {
	m := gunHaritasi("26/Dec/2025", "27/Dec/2025", "28/Dec/2025", "29/Dec/2025", "30/Dec/2025",
		"31/Dec/2025", "01/Jan/2026", "02/Jan/2026")
	got := gunMetni(sonGunler(m, 7))
	want := "27/Dec/2025 28/Dec/2025 29/Dec/2025 30/Dec/2025 31/Dec/2025 01/Jan/2026 02/Jan/2026"
	if got != want {
		t.Fatalf("yıl sınırı:\n gelen %s\n beklenen %s", got, want)
	}
}

// Ay adı sırası alfabetik değildir (Apr < Aug < Dec ...): aynı yıl içinde farklı aylar.
func TestSonGunler_AyAdiSirasi(t *testing.T) {
	m := gunHaritasi("10/Aug/2026", "10/Apr/2026", "10/Dec/2026", "10/Feb/2026")
	got := gunMetni(sonGunler(m, 7))
	want := "10/Feb/2026 10/Apr/2026 10/Aug/2026 10/Dec/2026"
	if got != want {
		t.Fatalf("ay adı sırası:\n gelen %s\n beklenen %s", got, want)
	}
}

// Sayılar doğru güne bağlı kalır; bozuk anahtar en eski sayılıp ilk kesilir.
func TestSonGunler_SayiVeBozukAnahtar(t *testing.T) {
	m := map[string]int{"bozuk": 99}
	for i, g := range []string{"28/Feb/2026", "01/Mar/2026", "02/Mar/2026", "03/Mar/2026", "04/Mar/2026", "05/Mar/2026", "06/Mar/2026"} {
		m[g] = 10 + i
	}
	l := sonGunler(m, 7)
	if len(l) != 7 || l[0].Tarih != "28/Feb/2026" || l[0].Istek != 10 || l[6].Tarih != "06/Mar/2026" || l[6].Istek != 16 {
		t.Fatalf("beklenmeyen sonuç: %+v", l)
	}
	if strings.Contains(gunMetni(l), "bozuk") {
		t.Fatalf("bozuk anahtar kesilmeliydi: %+v", l)
	}
	if got := sonGunler(map[string]int{}, 7); len(got) != 0 {
		t.Fatalf("boş harita → boş: %+v", got)
	}
}
