package musteri

import "net/http"

// HostKiraciKanca — istek doğrulanmış (aktif) bir bayi özel panel adresine mi geldi;
// (bayi users.id, true). main.go bağlar (marka paketi); nil → kural kapalı.
var HostKiraciKanca func(r *http.Request) (int64, bool)
