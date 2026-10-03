package monitor

// Yokla — destek botunun site_yokla aracı için dışa açık sağlık yoklaması:
// /domains/{id}/health ile AYNI SSRF kapılı istemci (guvenliDialContext +
// yonlendirmeGuard), kimlik bilgisi taşımayan tek GET. Hedef URL'yi çağıran
// kurar; destek aracı yalnız oturumun KENDİ alan adını (DB'den) ve katalog
// deseninden geçmiş yolu verir.
func Yokla(hedefURL string) DomainHealth { return probe(hedefURL) }
