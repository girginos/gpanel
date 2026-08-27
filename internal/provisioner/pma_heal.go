package provisioner

// Cloud/GCP + sistemik phpMyAdmin düzeltmeleri (startup-heal — MEVCUT kurulumlar için).
//
// 🔴 GCP/cloud kök sorunu: pma-signon.php DB-host'u web-request-host'undan türetiyordu →
// Google Cloud'da dış IP NIC'te YOK (1:1 NAT) → phpMyAdmin dış-IP TCP → hairpin/denied →
// "Token eksik" döngüsü. KESİN FIX (cloud-agnostik): phpMyAdmin DAİMA localhost SOCKET ile
// bağlanır → DB-user'ların @localhost (socket) kaydına eşleşir.
//
// Bu heal (provisioner.Init'ten her boot) idempotent garanti eder:
//   - /opt/girginospanel/pma-signon/pma-signon.php  → host='localhost' (yoksa/eskiyse yaz)
//   - /etc/girginospanel/pma-internal.token         → yoksa üret (root:apache 0640)
//   - /etc/php-fpm.d/phpmyadmin.conf                → mysqli/pdo_mysql.default_socket eklenir
//   - /opt/phpmyadmin/config.inc.php                → host 127.0.0.1 → localhost
//
// Installer/update mevcut kurulumlara bu asset'leri KOPYALAMADIĞI için (yalnız binary/
// frontend/migration/ops) bu Go-heal tek güvence noktasıdır.

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	pmaSignonDir  = "/opt/girginospanel/pma-signon"
	pmaSignonPath = "/opt/girginospanel/pma-signon/pma-signon.php"
	pmaTokenPath  = "/etc/girginospanel/pma-internal.token"
	pmaPoolPath   = "/etc/php-fpm.d/phpmyadmin.conf"
	pmaConfigPath = "/opt/phpmyadmin/config.inc.php"
)

// pmaSignonPHP: signon endpoint'in KANONİK içeriği. 🔴 PMA_single_signon_host = 'localhost'
// (DAİMA socket — web-host'tan TÜRETME). Bu içerik assets/phpmyadmin/pma-signon.php ile
// BİREBİR aynı olmalı (installer taze kurulumda onu kopyalar; bu heal mevcutları düzeltir).
const pmaSignonPHP = `<?php
/**
 * phpMyAdmin signon endpoint
 * Panel kısa-ömürlü token üretir, kullanıcı /pma-signon.php?t=<token> URL'ine yönlendirilir,
 * bu script token'i panel API'sine sorar (X-Internal-Auth header ile), DB credentials döner,
 * session'a yazar ve phpMyAdmin'e yönlendirir.
 */
declare(strict_types=1);
session_name('pma_signon');
ini_set('session.cookie_path', '/');
session_start();

if (empty($_GET['t'])) {
    http_response_code(400);
    die('Token eksik. Panel uzerinden gecis yapin.');
}
$token = (string)$_GET['t'];
if (!preg_match('/^[a-f0-9]{16,128}$/', $token)) {
    http_response_code(400);
    die('Token formati gecersiz.');
}

$internalToken = trim((string)@file_get_contents('/etc/girginospanel/pma-internal.token'));
if ($internalToken === '') {
    http_response_code(500);
    die('PMA internal token sunucuda yok.');
}

$ch = curl_init('http://127.0.0.1:8080/api/v1/internal/pma-redeem');
curl_setopt_array($ch, [
    CURLOPT_RETURNTRANSFER => true,
    CURLOPT_POST           => true,
    CURLOPT_POSTFIELDS     => json_encode(['token' => $token]),
    CURLOPT_HTTPHEADER     => [
        'Content-Type: application/json',
        'X-Internal-Auth: ' . $internalToken,
    ],
    CURLOPT_CONNECTTIMEOUT => 3,
    CURLOPT_TIMEOUT        => 5,
]);
$resp = curl_exec($ch);
$code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
curl_close($ch);

if ($code !== 200 || !$resp) {
    http_response_code(401);
    die('Token bozulamadi (kod ' . (int)$code . '). Panel uzerinden yeniden deneyin.');
}
$data = json_decode($resp, true);
if (!is_array($data) || empty($data['kullanici'])) {
    http_response_code(500);
    die('Sunucudan beklenmedik yanit.');
}

$_SESSION['PMA_single_signon_user']     = $data['kullanici'];
$_SESSION['PMA_single_signon_password'] = $data['parola'];
$_SESSION['PMA_single_signon_host'] = 'localhost';
$_SESSION['PMA_single_signon_only_db']  = [$data['db']];
session_write_close();

// 🔴 Kullaniciyi SUNUCU KOKUNE degil, kendi VERITABANINA birak.
// Eskiden '/pma/' ile kok sayfaya dusuluyordu: phpMyAdmin orada SUNUCU
// kapsaminda acilir, "Disa aktar" sekmesi de sunucu kapsamli calisir ve dosyayi
// $cfg['Export']['file_template_server'] = '@SERVER@' ile adlandirir -> her
// export "localhost.sql" olur, hangi veritabani oldugu belli olmaz.
// Veritabani kapsaminda acilinca sablon '@DATABASE@' olur -> "<db_adi>.sql".
// (only_db zaten tek DB'ye kilitliyordu; sorun yetkide degil, ACILIS KAPSAMINDA.)
$hedef = '/pma/';
if (!empty($data['db'])) {
    // phpMyAdmin 5.x rota bicimi.
    $hedef = '/pma/index.php?route=/database/structure&db=' . rawurlencode($data['db']);
}
header('Location: ' . $hedef, true, 302);
exit;
`

// mariadbSocket: MariaDB unix socket yolunu döndürür (SHOW/@@socket üzerinden), çözülemezse
// AlmaLinux/MariaDB standart yolu. phpMyAdmin socket-login için (GCP-agnostik) kullanılır.
func mariadbSocket() string {
	const fallback = "/var/lib/mysql/mysql.sock"
	if pkgDB != nil {
		var s string
		if err := pkgDB.QueryRow(`SELECT @@socket`).Scan(&s); err == nil && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return fallback
}

// ensurePMAStartup: phpMyAdmin cloud/socket düzeltmelerini idempotent uygular. Panel host'unda
// pma kurulu değilse (hiçbir pma dosyası yok) sessiz gecer.
func ensurePMAStartup() {
	pmaKurulu := dirVar(pmaSignonDir) || dosyaVar(pmaPoolPath) || dirVar("/opt/phpmyadmin")
	if !pmaKurulu {
		return // bu host'ta phpMyAdmin yok
	}
	sock := mariadbSocket()
	ensurePMASignon()
	ensurePMAToken()
	ensurePMAPoolSocket(sock)
	ensurePMAConfigHost()
}

func dosyaVar(p string) bool { _, e := os.Stat(p); return e == nil }
func dirVar(p string) bool   { fi, e := os.Stat(p); return e == nil && fi.IsDir() }

// ensurePMASignon: pma-signon.php'yi kanonik (host=localhost) içerikle yazar — dosya yoksa
// VEYA mevcut içerik host='localhost' fix'ini içermiyorsa (eski türetmeli sürüm) düzeltir.
func ensurePMASignon() {
	_ = os.MkdirAll(pmaSignonDir, 0755)
	cur, err := os.ReadFile(pmaSignonPath)
	if err == nil && strings.Contains(string(cur), "PMA_single_signon_host = 'localhost'") {
		return // zaten doğru
	}
	if err := os.WriteFile(pmaSignonPath, []byte(pmaSignonPHP), 0644); err != nil {
		log.Printf("pma heal: signon yazılamadı: %v", err)
		return
	}
	_, _ = exec.Command("restorecon", pmaSignonPath).CombinedOutput()
	log.Printf("pma heal: pma-signon.php kanonik (host=localhost) içerikle yazıldı")
}

// ensurePMAToken: internal auth token'ı yoksa üretir (root:apache 0640) — pma-signon.php ile
// panel API'si aynı dosyayı okur → rastgele değer eşleşir. Var olan token'a DOKUNMAZ.
func ensurePMAToken() {
	if b, err := os.ReadFile(pmaTokenPath); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return // zaten var
	}
	_ = os.MkdirAll(filepath.Dir(pmaTokenPath), 0755)
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		log.Printf("pma heal: token üretilemedi: %v", err)
		return
	}
	tok := hex.EncodeToString(raw) // 64 hex — openssl rand -hex 32 ile aynı
	if err := os.WriteFile(pmaTokenPath, []byte(tok+"\n"), 0640); err != nil {
		log.Printf("pma heal: token yazılamadı: %v", err)
		return
	}
	// root:apache 0640 → pma-signon FPM pool'u (apache) okur; başkası okuyamaz.
	gid := 0
	if g, err := user.LookupGroup("apache"); err == nil {
		if n, e := strconv.Atoi(g.Gid); e == nil {
			gid = n
		}
	}
	if gid != 0 {
		_ = os.Chown(pmaTokenPath, 0, gid)
		_ = os.Chmod(pmaTokenPath, 0640)
	} else {
		_ = os.Chmod(pmaTokenPath, 0644) // apache grubu yok → pool okuyabilsin
	}
	log.Printf("pma heal: /etc/girginospanel/pma-internal.token üretildi")
}

var pmaSocketLineRe = regexp.MustCompile(`(?m)^\s*php_value\[(?:mysqli|pdo_mysql)\.default_socket\]`)

// ensurePMAPoolSocket: phpmyadmin FPM pool'una mysqli/pdo_mysql default_socket'i ekler
// (GCP'de TCP dış-IP yerine socket → @localhost user'a bağlanır). Idempotent; eklenirse
// base php-fpm reload edilir (pool bu master'da dinler).
func ensurePMAPoolSocket(sock string) {
	cur, err := os.ReadFile(pmaPoolPath)
	if err != nil {
		return // pool yok
	}
	if pmaSocketLineRe.Match(cur) {
		return // zaten var
	}
	add := "php_value[mysqli.default_socket] = " + sock + "\n" +
		"php_value[pdo_mysql.default_socket] = " + sock + "\n"
	yeni := cur
	if len(yeni) > 0 && yeni[len(yeni)-1] != '\n' {
		yeni = append(yeni, '\n')
	}
	yeni = append(yeni, []byte(add)...)
	if err := os.WriteFile(pmaPoolPath, yeni, 0644); err != nil {
		log.Printf("pma heal: pool socket yazılamadı: %v", err)
		return
	}
	_, _ = exec.Command("systemctl", "reload-or-restart", "php-fpm").CombinedOutput()
	log.Printf("pma heal: phpmyadmin pool socket eklendi (%s) + php-fpm reload", sock)
}

var pmaConfigHostRe = regexp.MustCompile(`(\$cfg\['Servers'\]\[\$i\]\['host'\]\s*=\s*)'127\.0\.0\.1'`)

// ensurePMAConfigHost: config.inc.php'de server host'unu localhost'a çevirir (socket-login).
func ensurePMAConfigHost() {
	cur, err := os.ReadFile(pmaConfigPath)
	if err != nil {
		return
	}
	if !pmaConfigHostRe.Match(cur) {
		return // zaten localhost (veya farklı) — dokunma
	}
	yeni := pmaConfigHostRe.ReplaceAll(cur, []byte("${1}'localhost'"))
	if err := os.WriteFile(pmaConfigPath, yeni, 0644); err != nil {
		log.Printf("pma heal: config host yazılamadı: %v", err)
		return
	}
	_, _ = exec.Command("restorecon", pmaConfigPath).CombinedOutput()
	log.Printf("pma heal: config.inc.php host → localhost (socket-login)")
}
