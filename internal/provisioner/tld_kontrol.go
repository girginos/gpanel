package provisioner

// Yasaklı domain listesi için hafif callback. provisioner paketi bilerek başka
// bir iç pakete (domainyasak, database/sql) bağımlı olmadan çalışsın — bu
// paketin ana rolü sistem provizyonlaması, güvenlik politikası değil.
//
// main.go açılışta `SetYasakliKontrolu(domainyasak.Yasakli)` çağırır.
//
// Callback nil ise yasak listesi kontrolü çalışmaz (varsayılan = herşey
// serbest). Bu, ünite testleri ve development ortamlarında sürpriz davranışı
// önler.
//
// 🔴 NOT: dosya adı tarihsel nedenle "tld_kontrol.go" — özellik başlangıçta
// TLD (uzantı) engellemesi olarak tasarlanmıştı, sonra phishing koruması için
// tam domain-bazlı yeniden yapıldı. Kalan tek şey dosya adı; bir kez yeniden
// adlandırmak yerine burada belirtmek, git tarihini korur.

// yasakliKontrol, hostname'i alır ve (yasak-mı, eşleşen-domain) döner.
// nil ise hiçbir kontrol yok kabul edilir.
var yasakliKontrol func(hostname string) (bool, string)

// SetYasakliKontrolu — main.go tarafından bir kez çağrılır.
func SetYasakliKontrolu(fn func(string) (bool, string)) {
	yasakliKontrol = fn
}
