// Package portyonetim — panelin backend + dış portlarını değiştirme.
//
// 🔴 EN KRİTİK ÖZELLİK. Yanlış port → panel kendi altından çekilir → operatör
// kilitlenir. Katmanlı savunma:
//   1. YASAKLI PORTLAR listesi (web/mail/DB/DNS/sistem) — SQL formatta reddedilir
//   2. `ss -tln` ile port dolu mu kontrolü (başka servis tutuyorsa "reddedildi")
//   3. Değişim async: env yedekle → conf yedekle → uygula → `curl /healthz` üç
//      kez → başarısızsa OTOMATİK GERI AL. Kilitlenme koruması bu curl-verify
//      adımıdır.
//
// Backend port (127.0.0.1:8080) ve dış port (nginx :8443) AYRI AYRI değişir.
// İç port değişimi = env + nginx proxy_pass. Dış port = nginx listen.

package portyonetim

// YasakliPortlar — kullanıcı tarafında girilse bile REDDEDİLECEK portlar.
// Kritik hizmetler port çakışması olmasın diye. Kullanıcının verdiği liste
// + öneriler.
var YasakliPortlar = map[int]string{
	// Web
	80:   "HTTP (nginx/apache)",
	443:  "HTTPS (nginx/apache)",
	7080: "LiteSpeed admin",
	8088: "HTTP alt",
	8899: "nginx alt (mevcut sistem)",
	// Mail
	25:   "SMTP",
	465:  "SMTPS",
	587:  "SMTP submission",
	110:  "POP3",
	995:  "POP3S",
	143:  "IMAP",
	993:  "IMAPS",
	24:   "LMTP",
	4190: "Sieve",
	// DB / cache
	3306:  "MySQL/MariaDB",
	5432:  "PostgreSQL",
	6379:  "Redis/Valkey",
	27017: "MongoDB",
	11211: "Memcached",
	// DNS / ağ
	53:  "DNS",
	22:  "SSH (panel kilitlenmesi çift-katman olmasın)",
	21:  "FTP control",
	20:  "FTP data",
	990: "FTPS",
	989: "FTPS data",
	953: "BIND rndc",
	// Diğer sık kullanılan
	3128:  "Squid",
	25565: "Minecraft (istemeden çakışma)",
	5060:  "SIP",
	5061:  "SIP TLS",
	5514:  "syslog",
	// Web (ek yaygin)
	8000: "HTTP dev (Django/Python)",
	8008: "HTTP alt",
	8888: "HTTP alt (Jupyter/vs)",
	// Izleme/panel (yaygin cakisma)
	9090: "Cockpit / Prometheus",
	9091: "Prometheus push",
	9100: "node_exporter",
	9200: "Elasticsearch REST",
	9300: "Elasticsearch node",
	// Dosya/paylasim
	2049: "NFS",
	111:  "rpcbind",
	139:  "NetBIOS SMB",
	445:  "SMB",
	// Container/k8s
	2379:  "etcd client",
	2380:  "etcd peer",
	6443:  "Kubernetes API",
	10250: "kubelet",
	// Mesaj kuyrugu
	5672:  "RabbitMQ AMQP",
	15672: "RabbitMQ management",
	// cPanel/Plesk (yaygin)
	2082: "cPanel",
	2083: "cPanel SSL",
	2086: "WHM",
	2087: "WHM SSL",
	2222: "DirectAdmin",
	8880: "Plesk",
	9418: "git://",
}

// PortYasakMi — port yasaklı listedeyse (true, açıklama). Aksi halde (false, "").
func PortYasakMi(port int) (bool, string) {
	if port < 1024 {
		return true, "sistem/ayrıcalıklı port (1-1023 arası) — panel unit'inde CAP_NET_BIND_SERVICE gerekir"
	}
	if port > 65535 {
		return true, "port aralık dışı (max 65535)"
	}
	if aciklama, ok := YasakliPortlar[port]; ok {
		return true, aciklama
	}
	return false, ""
}
