package git

import (
	"os"
	"testing"

	"girginospanel/internal/bayidilim"
)

// Testler makinenin GERÇEK bayi havuzu üyeliğini okumasın. Geliştirme sunucusunda testSK
// ("c_deneme") adında gerçek bir bayi hostingi açılınca kiracı dilimi beklenen
// girginos-c_deneme.slice yerine girginos-bayi185-c_deneme.slice oldu ve kiracı testleri
// ortama bağlı olarak düştü (2026-10-13). Üyelik dizini olmayan bir yola çekilir → üyelik yok.
func TestMain(m *testing.M) {
	bayidilim.UyelikDizini = "/nonexistent/gosp-git-test-uyelik"
	os.Exit(m.Run())
}
