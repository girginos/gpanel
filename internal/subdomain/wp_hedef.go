package subdomain

// wp_hedef.go — WordPress Toolkit'in (internal/wordpress) ek/alt alan adı hedeflerini
// çözerken kullandığı DIŞA AÇIK yol/SSL yardımcıları. Yol ve sertifika düzeni TEK yerde
// (bu paket) tanımlı kalır; wordpress paketi klasör adı yazmaz. evKok parametre: orada
// testler geçici bir ev dizinine yönlendirir (üretimde "/home").

// AltDocrootYolu — <evKok>/<sk>/subdomains/<tam_ad> (alt alan adı docroot'u).
func AltDocrootYolu(evKok, sk, tamAd string) string {
	return evKok + "/" + sk + "/subdomains/" + tamAd
}

// SSLAktifYol — <evKok>/<sk>/ssl/<ad>.crt VE .key mevcutsa true (dosya doğruluk kaynağı;
// ek ve alt alan adı sertifikaları aynı dizinde, <ad>.crt/.key adıyla durur).
func SSLAktifYol(evKok, sk, ad string) bool {
	d := evKok + "/" + sk + "/ssl/"
	return dosyaVar(d+ad+".crt") && dosyaVar(d+ad+".key")
}
