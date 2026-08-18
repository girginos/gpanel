package avmotor

// TABAN KURAL SETİ — imza servisi devreye girmeden ÖNCE de koruma olsun diye
// ikilinin içinde gömülü gelir.
//
// 🔴 NEDEN GÖMÜLÜ: imza paketi indirilemezse (ağ kesik, servis kapalı, yeni
// kurulum) motor kuralsız kalır ve "antivirüs çalışıyor" derken hiçbir şey
// yapmaz — bu oturumda avladığımız "başarısızlık güven olarak görünür"
// sınıfının ta kendisi. Taban set, dağıtım kanalı ölü olsa bile en yaygın
// saldırıları yakalar.
//
// PUANLAMA MANTIĞI
// ────────────────
//
//	100 → tek başına KRİTİK. Yalnızca meşru kodda gerçekçi karşılığı olmayan
//	      desenler bu puanı alır.
//	 60 → güçlü kanıt; ikinci bir sinyalle kritik olur.
//	 40 → ortadaki kanıt; iki tanesi şüpheliyi geçirir.
//	 20 → zayıf sinyal; tek başına asla yeterli değil.
//
// 🔴 Meşru eklentiler `base64_decode`, `eval`, `system` KULLANIR. Bu yüzden
// tek fonksiyon adı asla 100 almaz — ZİNCİR aranır (girdi → çözme → yürütme).
// BacktickDeseni — backtick YURUTME operatoru + istek verisi. Ham string
// (backtick ile sarilamaz cunku desen backtick iceriyor) — cift tirnakla
// ama Go raw yerine acik yaziliyor.
var BacktickDeseni = "(?:=|\\(|\\breturn\\b|\\becho\\b|\\bprint\\b|\\.)\\s*`[^`]*[$]_(GET|POST|REQUEST)\\s*\\[[^`]*`"

func TabanSet() KuralSeti {
	php := []string{"php", "phar", "phtml", "php5", "php7", "php8", "inc"}
	return KuralSeti{
		Surum:  0, // 0 = gömülü taban; imza paketi geldiğinde üzerine yazılır
		Uretim: "gömülü",
		Kurallar: []Kural{
			// ── Yürütme zincirleri: girdi doğrudan yürütmeye gidiyor ──
			{ID: "GOSP-PHP-EVAL-B64", Ad: "eval(base64_decode(...))",
				Desen: `(?i)eval\s*\(\s*(base64_decode|gzinflate|gzuncompress|str_rot13|strrev)\s*\(`,
				Puan:  100, Uzanti: php},
			{ID: "GOSP-PHP-EVAL-SUPERGLOBAL", Ad: "eval doğrudan istek verisiyle",
				Desen: `(?i)eval\s*\(\s*\$_(GET|POST|REQUEST|COOKIE|SERVER)\s*\[`,
				Puan:  100, Uzanti: php},
			{ID: "GOSP-PHP-ASSERT-SUPERGLOBAL", Ad: "assert() istek verisiyle",
				Desen: `(?i)assert\s*\(\s*\$_(GET|POST|REQUEST|COOKIE)\s*\[`,
				Puan:  100, Uzanti: php},
			{ID: "GOSP-PHP-PREG-E", Ad: "preg_replace /e değiştiricisi (kod yürütür)",
				Desen: `(?i)preg_replace\s*\(\s*['"][^'"]*['"]\s*e[imsxuADSUXJ]*['"]`,
				Puan:  100, Uzanti: php},
			{ID: "GOSP-PHP-CREATE-FUNCTION", Ad: "create_function ile dinamik kod",
				Desen: `(?i)create_function\s*\(\s*['"]`,
				Puan:  60, Uzanti: php},

			// ── Kabuk erişimi + istek verisi ──
			{ID: "GOSP-PHP-SHELL-SUPERGLOBAL", Ad: "kabuk çağrısı istek verisiyle",
				Desen: `(?i)(system|shell_exec|passthru|popen|proc_open|exec)\s*\(\s*\$_(GET|POST|REQUEST|COOKIE)\s*\[`,
				Puan:  100, Uzanti: php},
			// 🔴 Backtick operatoru yanlis pozitife cok acik: gercek WordPress
			// cekirdeginde 92 dosyada eslesti (WP PHPDoc yorumlarinda backtick
			// kullaniyor: `$wpdb->prepare()`). Yalniz OPERATOR baglaminda
			// (atama/return/echo/nokta sonrasi) ve ayni backtick cifti icinde
			// superglobal; puan 60 -> tek basina KRITIK degil, ikinci kanit gerekir.
			{ID: "GOSP-PHP-BACKTICK-YURUTME", Ad: "backtick yurutme istek verisiyle",
				Desen: BacktickDeseni,
				Puan:  60, Uzanti: php},

			// ── Değişken fonksiyon çağrısı (klasik webshell gizleme) ──
			{ID: "GOSP-PHP-DEGISKEN-FONKSIYON", Ad: "$degisken(...) dinamik çağrı",
				Desen: `\$[a-zA-Z_][a-zA-Z0-9_]*\s*\(\s*\$_(GET|POST|REQUEST|COOKIE)\s*\[`,
				Puan:  100, Uzanti: php},

			// ── Obfuscation göstergeleri ──
			{ID: "GOSP-PHP-CHR-ZINCIR", Ad: "chr() zinciriyle string kurma",
				Desen: `(?:chr\s*\(\s*\d+\s*\)\s*\.\s*){6,}`,
				Puan:  60, Uzanti: php},
			{ID: "GOSP-PHP-HEX-DEGISKEN", Ad: "\\x kaçışlı fonksiyon adı",
				Desen: `["'](?:\\x[0-9a-fA-F]{2}){5,}["']`,
				Puan:  60, Uzanti: php},
			{ID: "GOSP-PHP-GZINFLATE-B64", Ad: "gzinflate(base64_decode(...)) yükü",
				Desen: `(?i)gzinflate\s*\(\s*base64_decode\s*\(`,
				Puan:  60, Uzanti: php},

			// ── Dosya yükleme + yürütme (yükleyici webshell) ──
			{ID: "GOSP-PHP-MOVE-UPLOADED-PHP", Ad: "yüklenen dosyayı .php olarak kaydetme",
				Desen: `(?i)move_uploaded_file\s*\([^)]*\.ph(p|tml|ar)`,
				Puan:  100, Uzanti: php},

			// ── Uzaktan kod çekme ──
			{ID: "GOSP-PHP-UZAK-INCLUDE", Ad: "http üzerinden include/require",
				Desen: `(?i)(include|require)(_once)?\s*\(?\s*['"]https?://`,
				Puan:  100, Uzanti: php},
			{ID: "GOSP-PHP-CURL-EVAL", Ad: "uzaktan indirilen içeriği yürütme",
				Desen: `(?i)eval\s*\(\s*(file_get_contents|curl_exec)\s*\(`,
				Puan:  100, Uzanti: php},

			// ── Kalıcılık / gizlenme ──
			{ID: "GOSP-PHP-HTACCESS-HANDLER", Ad: ".htaccess ile PHP handler enjeksiyonu",
				Desen: `(?i)AddType\s+application/x-httpd-php\s+\.(jpg|png|gif|txt|ico)`,
				Puan:  100},
			{ID: "GOSP-PHP-BOT-GIZLEME", Ad: "arama motoruna farklı içerik (cloaking)",
				Desen: `(?i)\$_SERVER\s*\[\s*['"]HTTP_USER_AGENT['"]\s*\][^;]{0,80}(googlebot|bingbot|yandex)`,
				Puan:  40, Uzanti: php},

			// ── Bilinen webshell parmak izleri ──
			{ID: "GOSP-SHELL-C99-R57", Ad: "c99/r57 webshell işareti",
				Desen: `(?i)(c99shell|r57shell|WSO\s*\d|FilesMan|b374k|IndoXploit|priv8)`,
				Puan:  100, Uzanti: php},
			{ID: "GOSP-SHELL-PAROLA-KAPISI", Ad: "webshell parola kapısı deseni",
				Desen: `(?i)\$(pass|password|pwd)\s*=\s*['"][0-9a-f]{32}['"]\s*;.{0,200}(md5|hash)\s*\(\s*\$_`,
				Puan:  100, Uzanti: php},

			// ── Zayıf sinyaller (tek başına asla yetmez) ──
			{ID: "GOSP-PHP-ERROR-SUSTUR", Ad: "hata bastırma + yürütme",
				Desen: `@\s*(eval|system|shell_exec|assert)\s*\(`,
				Puan:  40, Uzanti: php},
			{ID: "GOSP-PHP-INI-KAPAT", Ad: "güvenlik ayarını çalışma anında kapatma",
				Desen: `(?i)ini_set\s*\(\s*['"](disable_functions|open_basedir|safe_mode)['"]`,
				Puan:  60, Uzanti: php},
			{ID: "GOSP-PHP-COK-UZUN-B64", Ad: "çok uzun base64 bloğu",
				Desen: `["'][A-Za-z0-9+/]{500,}={0,2}["']`,
				Puan:  20, Uzanti: php},

			// ── JavaScript enjeksiyonu (tema/eklenti dosyalarına) ──
			{ID: "GOSP-JS-DOC-WRITE-UNESCAPE", Ad: "document.write(unescape(...))",
				Desen: `(?i)document\.write\s*\(\s*unescape\s*\(`,
				Puan:  60, Uzanti: []string{"js"}},
			{ID: "GOSP-JS-EVAL-FROMCHARCODE", Ad: "eval(String.fromCharCode(...))",
				Desen: `(?i)eval\s*\(\s*String\.fromCharCode\s*\(`,
				Puan:  100, Uzanti: []string{"js"}},
		},
	}
}
