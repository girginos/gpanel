package provisioner

// validate_yol_test.go — bayi kapsamı: kiracı ek nginx direktiflerinde DOSYA YOLU
// alan direktifler reddedilir. `nginx -t` bu yolları yapılandırma yüklenirken
// açtığı için izin verilseydi doğrulama ucu (nginx-settings, alt alan web-sunucu,
// plan) başka bir kiracının dosya yolunun varlığını sınayan bir kâhin olurdu.
// Saf fonksiyon testi — nginx çalıştırılmaz, dosyaya dokunulmaz.

import "testing"

func TestTehlikeliNginxDirektifi_DosyaYoluAlanlarReddedilir(t *testing.T) {
	const yol = " /home/c_baska/ssl/baska.pem;"
	for _, d := range []string{
		// sonek kuralı (nginxYolSoneki)
		"ssl_dhparam" + yol,
		"ssl_crl" + yol,
		"ssl_client_certificate" + yol,
		"ssl_password_file" + yol,
		"ssl_stapling_file" + yol,
		"proxy_ssl_certificate" + yol,
		"proxy_ssl_certificate_key" + yol,
		"proxy_ssl_trusted_certificate" + yol,
		"proxy_ssl_crl" + yol,
		"proxy_ssl_password_file" + yol,
		"grpc_ssl_certificate_key" + yol,
		"uwsgi_ssl_crl" + yol,
		"client_body_temp_path /home/c_baska/tmp;",
		"proxy_temp_path /home/c_baska/tmp;",
		"fastcgi_temp_path /home/c_baska/tmp;",
		"proxy_store /home/c_baska/depo;",
		"fastcgi_store /home/c_baska/depo;",
		// açık liste (sonek kuralına uymayanlar)
		"ssl_session_ticket_key" + yol,
		"ssl_conf_command DHParameters /home/c_baska/dh.pem;",
		"location /x { xslt_stylesheet /home/c_baska/a.xsl; }",
		"location /x { xml_entities /home/c_baska/a.dtd; }",
		// biçim varyantları: büyük harf, tırnaklı ad, blok içi, yorum/string sonrası
		"SSL_DHPARAM" + yol,
		`"ssl_dhparam"` + yol,
		"location /a { ssl_dhparam" + yol + " }",
		"add_header X-A \"#;\"; ssl_crl" + yol,
		"# not\nssl_dhparam" + yol,
		// önceden de yasak olanlar (gerileme kilidi)
		"include /home/c_baska/x.conf;",
		"auth_basic_user_file /home/c_baska/.htpasswd;",
		"ssl_trusted_certificate" + yol,
	} {
		if TehlikeliNginxDirektifi(d) == "" {
			t.Errorf("dosya yolu alan direktif reddedilmeli: %q", d)
		}
	}
}

// Yaygın, dosya yolu ALMAYAN kiracı direktifleri kabul edilmeye devam eder.
func TestTehlikeliNginxDirektifi_YolsuzDirektiflerKabul(t *testing.T) {
	for _, d := range []string{
		"add_header X-Frame-Options SAMEORIGIN;",
		"expires 30d;",
		"gzip on; gzip_types text/css application/javascript;",
		"client_max_body_size 64m;",
		`fastcgi_cache_key "$scheme$host$request_uri";`,
		"proxy_cache_key $host$uri;",
		"open_file_cache max=1000 inactive=20s;",
		"sendfile on;",
		"ssl_stapling on; ssl_protocols TLSv1.2 TLSv1.3;",
		"location /a { try_files $uri =404; }",
		"fastcgi_store_access user:rw;",
	} {
		if ad := TehlikeliNginxDirektifi(d); ad != "" {
			t.Errorf("yolsuz direktif reddedilmemeli: %q → %q", d, ad)
		}
	}
}
