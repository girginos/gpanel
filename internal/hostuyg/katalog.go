package hostuyg

// Katalog — Faz 3.4: 8 Docker-siz native binary recipe.
//
// Kaldırılan: Vaultwarden (Docker-only), Uptime Kuma (Node source-only).
// Bunların native binary release'i YOK — Docker'a bağımlı, katalog kuralımıza
// (bkz feedback_docker_yok) uymuyor.

var Katalog = []Tarif{
	// -------------------------------------------------------------------
	// Gitea — Git hosting, Go static binary
	// -------------------------------------------------------------------
	{
		Kod:      "gitea",
		Ad:       "Gitea",
		Aciklama: "Kendi git deponuz — GitHub benzeri, hafif, Go binary tek dosya.",
		Kategori: "gelistirme",
		Ikon:     "🌿",
		LogoURL:  "https://cdn.simpleicons.org/gitea/609926",
		Surum:    "1.22.6",

		IndirmeURL: "https://dl.gitea.com/gitea/1.22.6/gitea-1.22.6-linux-amd64",
		SHA256:     "fd77f1a0273c85a0950207c1cfa6753a9fa57604e4ab1382484b191cc919ce15",
		IcerikTuru: "binary",
		BinaryYol:  "gitea",

		CalistirKomutu: []string{"{kurulum}/gitea", "web", "-c", "{kurulum}/app.ini"},
		CalismaDizini:  "{kurulum}",
		CevreDegisken: map[string]string{
			"GITEA_WORK_DIR": "{kurulum}",
			"USER":           "{sistem_kullanici}",
		},
		MemoryMax: "512M",
		CPUQuota:  "50%",
		TasksMax:  200,

		Portlar: []PortTarifi{
			{Ad: "web", Protokol: "tcp"},
			{Ad: "ssh", Protokol: "tcp", DisAcik: true},
		},
		ConfigDosyalar: []ConfigDosyaTarifi{
			{
				Yol: "app.ini",
				Icerik: `APP_NAME = Gitea
RUN_USER = {sistem_kullanici}
RUN_MODE = prod

[server]
DOMAIN = {panelhost}
HTTP_ADDR = 127.0.0.1
HTTP_PORT = {port_web}
SSH_LISTEN_HOST = 0.0.0.0
SSH_PORT = {port_ssh}
DISABLE_SSH = false

[database]
DB_TYPE = sqlite3
PATH    = {kurulum}/data/gitea.db

[security]
INTERNAL_TOKEN = {secret64}
SECRET_KEY     = {secret32}

[service]
DISABLE_REGISTRATION = true
`,
				Izin:  0640,
				Sahip: "app",
			},
		},
		NginxProxy: &NginxProxyTarifi{
			SubPathOn:   "/gitea",
			UpgradeWS:   true,
			MaxBodySize: "500m",
		},
		SaglikHTTP:  "http://127.0.0.1:{port_web}/api/healthz",
		SaglikBekle: 5,
	},

	// -------------------------------------------------------------------
	// Caddy — Web sunucu / reverse proxy, Go static
	// -------------------------------------------------------------------
	{
		Kod:      "caddy",
		Ad:       "Caddy",
		Aciklama: "Modern web sunucu — otomatik HTTPS, HTTP/3, admin API, reverse proxy.",
		Kategori: "web",
		Ikon:     "🧱",
		LogoURL:  "https://cdn.simpleicons.org/caddy/1F88C0",
		Surum:    "2.8.4",

		IndirmeURL: "https://github.com/caddyserver/caddy/releases/download/v2.8.4/caddy_2.8.4_linux_amd64.tar.gz",
		SHA256:     "a7e8306c54138cf88e371c5ec0caf7baf142ecc1d60a30897dfb67d65d3748c8",
		IcerikTuru: "tarball_gz",

		CalistirKomutu: []string{"{kurulum}/caddy", "run", "--config", "{kurulum}/Caddyfile", "--adapter", "caddyfile"},
		CalismaDizini:  "{kurulum}",
		CevreDegisken: map[string]string{
			"XDG_CONFIG_HOME": "{kurulum}/config",
			"XDG_DATA_HOME":   "{kurulum}/data",
		},
		MemoryMax: "128M",
		CPUQuota:  "25%",
		TasksMax:  50,

		Portlar: []PortTarifi{
			{Ad: "admin", Protokol: "tcp"},
		},
		ConfigDosyalar: []ConfigDosyaTarifi{
			{
				Yol: "Caddyfile",
				Icerik: `{
    admin 127.0.0.1:{port_admin}
    auto_https off
}
`,
				Izin:  0640,
				Sahip: "app",
			},
		},
		SaglikHTTP:  "http://127.0.0.1:{port_admin}/config/",
		SaglikBekle: 4,
	},

	// -------------------------------------------------------------------
	// Syncthing — P2P file sync, Go static
	// -------------------------------------------------------------------
	{
		Kod:      "syncthing",
		Ad:       "Syncthing",
		Aciklama: "Sunucular arası şifreli P2P dosya senkronizasyonu — hafif, self-hosted, web arayüz.",
		Kategori: "depolama",
		Ikon:     "🔄",
		LogoURL:  "https://cdn.simpleicons.org/syncthing/0891D1",
		Surum:    "1.28.0",

		IndirmeURL: "https://github.com/syncthing/syncthing/releases/download/v1.28.0/syncthing-linux-amd64-v1.28.0.tar.gz",
		SHA256:     "9bc818c8e85c5dd3add93d53bf3a6e340aac67d26ebcdb7460c14a4870f51fb6",
		IcerikTuru: "tarball_gz",

		CalistirKomutu: []string{"{kurulum}/syncthing-linux-amd64-v1.28.0/syncthing",
			"serve", "--no-browser", "--home={kurulum}/config",
			"--gui-address=127.0.0.1:{port_web}"},
		CalismaDizini: "{kurulum}",
		MemoryMax:     "256M",
		CPUQuota:      "40%",
		TasksMax:      100,

		Portlar: []PortTarifi{
			{Ad: "web", Protokol: "tcp"},
			{Ad: "sync", Protokol: "tcp", DisAcik: true},
		},
		NginxProxy: &NginxProxyTarifi{
			SubPathOn:   "/syncthing",
			UpgradeWS:   true,
			MaxBodySize: "128m",
		},
		SaglikHTTP:  "http://127.0.0.1:{port_web}/rest/noauth/health",
		SaglikBekle: 5,
	},

	// -------------------------------------------------------------------
	// Grafana — Monitoring dashboard, Go binary + web assets
	// -------------------------------------------------------------------
	{
		Kod:      "grafana",
		Ad:       "Grafana",
		Aciklama: "Metrikler, loglar, izleme için görsel dashboard — Prometheus/InfluxDB/PostgreSQL uyumlu.",
		Kategori: "izleme",
		Ikon:     "📊",
		LogoURL:  "https://cdn.simpleicons.org/grafana/F46800",
		Surum:    "11.3.1",

		IndirmeURL: "https://dl.grafana.com/oss/release/grafana-11.3.1.linux-amd64.tar.gz",
		SHA256:     "cd426520532ee22582c4bcdc898ee33dd15f540b3ed0f9add0e1a09f4db2823b",
		IcerikTuru: "tarball_gz",

		CalistirKomutu: []string{"{kurulum}/grafana-v11.3.1/bin/grafana", "server"},
		CalismaDizini:  "{kurulum}/grafana-v11.3.1",
		CevreDegisken: map[string]string{
			"GF_PATHS_HOME":                 "{kurulum}/grafana-v11.3.1",
			"GF_PATHS_DATA":                 "{kurulum}/data",
			"GF_PATHS_LOGS":                 "{kurulum}/logs",
			"GF_PATHS_PLUGINS":              "{kurulum}/plugins",
			"GF_PATHS_PROVISIONING":         "{kurulum}/grafana-v11.3.1/conf/provisioning",
			"GF_SERVER_HTTP_ADDR":           "127.0.0.1",
			"GF_SERVER_HTTP_PORT":           "{port_web}",
			"GF_SERVER_ROOT_URL":            "https://{panelhost}:8443/grafana/",
			"GF_SERVER_SERVE_FROM_SUB_PATH": "true",
			"GF_SECURITY_ADMIN_PASSWORD":    "{secret16}",
		},
		MemoryMax: "512M",
		CPUQuota:  "60%",
		TasksMax:  200,

		Portlar: []PortTarifi{
			{Ad: "web", Protokol: "tcp"},
		},
		NginxProxy: &NginxProxyTarifi{
			SubPathOn:   "/grafana",
			UpgradeWS:   true,
			MaxBodySize: "64m",
		},
		SaglikHTTP:  "http://127.0.0.1:{port_web}/api/health",
		SaglikBekle: 6,
	},

	// -------------------------------------------------------------------
	// Prometheus — Time-series database, Go static
	// -------------------------------------------------------------------
	{
		Kod:      "prometheus",
		Ad:       "Prometheus",
		Aciklama: "Time-series metrik toplama + sorgulama — Grafana ile ideal eşleşme.",
		Kategori: "izleme",
		Ikon:     "🔥",
		LogoURL:  "https://cdn.simpleicons.org/prometheus/E6522C",
		Surum:    "3.0.1",

		IndirmeURL: "https://github.com/prometheus/prometheus/releases/download/v3.0.1/prometheus-3.0.1.linux-amd64.tar.gz",
		SHA256:     "43f6f228ef59e0c2f6994e489c5c76c6671553eaa99ded0aea1cd31366222916",
		IcerikTuru: "tarball_gz",

		CalistirKomutu: []string{"{kurulum}/prometheus-3.0.1.linux-amd64/prometheus",
			"--config.file={kurulum}/prometheus.yml",
			"--storage.tsdb.path={kurulum}/data",
			"--web.listen-address=127.0.0.1:{port_web}",
			"--web.external-url=/prometheus/",
			"--web.route-prefix=/",
		},
		CalismaDizini: "{kurulum}",
		MemoryMax:     "512M",
		CPUQuota:      "50%",
		TasksMax:      100,

		Portlar: []PortTarifi{
			{Ad: "web", Protokol: "tcp"},
		},
		ConfigDosyalar: []ConfigDosyaTarifi{
			{
				Yol: "prometheus.yml",
				Icerik: `global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
      - targets: ['127.0.0.1:{port_web}']
`,
				Izin:  0640,
				Sahip: "app",
			},
		},
		NginxProxy: &NginxProxyTarifi{
			SubPathOn:   "/prometheus",
			UpgradeWS:   false,
			MaxBodySize: "10m",
		},
		SaglikHTTP:  "http://127.0.0.1:{port_web}/-/healthy",
		SaglikBekle: 4,
		// Prometheus WAL sürekli yeniden yazılır — backup gerektirmez
		BackupExclude:   []string{"data/wal/*", "data/chunks_head/*"},
		BackupTutSayisi: 3, // TSDB büyük, 3 tut
	},

	// -------------------------------------------------------------------
	// MinIO — S3-compatible object storage, Go static binary
	// -------------------------------------------------------------------
	{
		Kod:      "minio",
		Ad:       "MinIO",
		Aciklama: "S3-compatible object storage — self-hosted, kendi bucket'ların, backup/artifact deposu.",
		Kategori: "depolama",
		Ikon:     "🪣",
		LogoURL:  "https://cdn.simpleicons.org/minio/C72E49",
		Surum:    "2025-09-07",

		IndirmeURL: "https://dl.min.io/server/minio/release/linux-amd64/minio",
		SHA256:     "7c5bd8512c6e966455b1d198209358b2d191c77a83ab377c4073281065fb855f",
		IcerikTuru: "binary",
		BinaryYol:  "minio",

		CalistirKomutu: []string{"{kurulum}/minio", "server", "{kurulum}/data",
			"--address", "127.0.0.1:{port_api}",
			"--console-address", "127.0.0.1:{port_console}"},
		CalismaDizini: "{kurulum}",
		CevreDegisken: map[string]string{
			"MINIO_ROOT_USER":     "admin", // Grafana/SFTPGo/Statping ile tutarlı
			"MINIO_ROOT_PASSWORD": "{secret16}",
			"MINIO_BROWSER":       "on",
		},
		MemoryMax: "512M",
		CPUQuota:  "50%",
		TasksMax:  150,

		Portlar: []PortTarifi{
			{Ad: "api", Protokol: "tcp"},     // S3 API — programatik erişim
			{Ad: "console", Protokol: "tcp"}, // Web console
		},
		NginxProxy: &NginxProxyTarifi{
			SubPathOn:   "/minio",
			UpgradeWS:   true,
			MaxBodySize: "10240m", // 10GB S3 upload
			// MV#7 fix: proxy_request_buffering off — istemci direkt akış
			// (nginx client_body_temp_path'e 10GB yazmasın, disk dolmasın).
			// Read/send timeout büyük upload için 1 saat.
			ExtraDirektif: "proxy_request_buffering off; proxy_buffering off; proxy_read_timeout 3600s; proxy_send_timeout 3600s;",
		},
		SaglikHTTP:  "http://127.0.0.1:{port_api}/minio/health/live",
		SaglikBekle: 4,
		// MinIO tmp/multipart upload'ları — restart-sırasında sıfırlanır
		BackupExclude: []string{"data/.minio.sys/tmp/*", "data/.minio.sys/multipart/*"},
	},

	// -------------------------------------------------------------------
	// SFTPGo — SFTP/FTP/WebDAV server, Go static binary
	// -------------------------------------------------------------------
	{
		Kod:      "sftpgo",
		Ad:       "SFTPGo",
		Aciklama: "SFTP/FTP/WebDAV/S3/GCS server + web admin — multi-protokol dosya paylaşımı. Admin şifresi ilk açılışta yaratılır; sonradan web UI'dan değiştirilir (recipe secret'i etkisiz).",
		Kategori: "depolama",
		Ikon:     "📁",
		LogoURL:  "https://cdn.simpleicons.org/filezilla/BF0000",
		Surum:    "2.7.5",

		IndirmeURL: "https://github.com/drakkan/sftpgo/releases/download/v2.7.5/sftpgo_v2.7.5_linux_x86_64.tar.xz",
		SHA256:     "6bfecb99d17e0dc53c3b019100e3577d0e591876b3c593847ee4ab3b25952ffa",
		IcerikTuru: "tarball_xz",

		CalistirKomutu: []string{"{kurulum}/sftpgo", "serve",
			"--config-file", "{kurulum}/sftpgo.json"},
		CalismaDizini: "{kurulum}",
		CevreDegisken: map[string]string{
			"SFTPGO_DEFAULT_ADMIN_USERNAME": "admin",
			"SFTPGO_DEFAULT_ADMIN_PASSWORD": "{secret16}",
		},
		MemoryMax: "256M",
		CPUQuota:  "40%",
		TasksMax:  150,

		Portlar: []PortTarifi{
			{Ad: "web", Protokol: "tcp"},                 // Web admin
			{Ad: "sftp", Protokol: "tcp", DisAcik: true}, // SFTP client'ları dışardan
		},
		ConfigDosyalar: []ConfigDosyaTarifi{
			{
				Yol: "sftpgo.json",
				Icerik: `{
  "common": {
    "idle_timeout": 15
  },
  "sftpd": {
    "bindings": [
      { "port": {port_sftp}, "address": "0.0.0.0" }
    ]
  },
  "httpd": {
    "bindings": [
      { "port": {port_web}, "address": "127.0.0.1", "enable_web_admin": true, "enable_web_client": true }
    ]
  },
  "data_provider": {
    "driver": "sqlite",
    "name": "sftpgo.db"
  }
}
`,
				Izin:  0640,
				Sahip: "app",
			},
		},
		NginxProxy: &NginxProxyTarifi{
			SubPathOn:   "/sftpgo",
			UpgradeWS:   true,
			MaxBodySize: "1024m",
		},
		SaglikHTTP:  "http://127.0.0.1:{port_web}/healthz",
		SaglikBekle: 4,
	},

	// -------------------------------------------------------------------
	// Headscale — WireGuard control plane (Tailscale açık kaynak yönetim)
	// Go tek binary. Client'lar Tailscale açık istemci ile bağlanır.
	// -------------------------------------------------------------------
	{
		Kod:      "headscale",
		Ad:       "Headscale VPN",
		Aciklama: "Docker-siz WireGuard VPN — Tailscale açık-kaynak kontrol düzlemi. Kullanıcı client'ları Tailscale ile bağlanır.",
		Kategori: "guvenlik",
		Ikon:     "🔒",
		LogoURL:  "https://cdn.simpleicons.org/tailscale/242424",
		Surum:    "0.29.3",

		IndirmeURL: "https://github.com/juanfont/headscale/releases/download/v0.29.3/headscale_0.29.3_linux_amd64",
		SHA256:     "8dc183758024ed7095cf610fedea0790233613c71353bc8be2715d82ba29b92c",
		IcerikTuru: "binary",
		BinaryYol:  "headscale",

		CalistirKomutu: []string{"{kurulum}/headscale", "serve", "-c", "{kurulum}/config.yaml"},
		CalismaDizini:  "{kurulum}",
		MemoryMax:      "256M",
		CPUQuota:       "30%",
		TasksMax:       100,

		Portlar: []PortTarifi{
			{Ad: "web", Protokol: "tcp"},               // HTTP API + WebSocket
			{Ad: "metrics", Protokol: "tcp"},           // Prometheus /metrics
			{Ad: "wg", Protokol: "udp", DisAcik: true}, // WireGuard peer discovery (STUN)
		},
		ConfigDosyalar: []ConfigDosyaTarifi{
			{
				Yol: "config.yaml",
				Icerik: `server_url: http://{panelhost}:{port_web}
listen_addr: 127.0.0.1:{port_web}
metrics_listen_addr: 127.0.0.1:{port_metrics}
grpc_listen_addr: 127.0.0.1:{port_metrics}
grpc_allow_insecure: false

private_key_path: {kurulum}/private.key
noise:
  private_key_path: {kurulum}/noise_private.key

prefixes:
  v4: 100.64.0.0/10
  v6: fd7a:115c:a1e0::/48

derp:
  server:
    enabled: false
  urls:
    - https://controlplane.tailscale.com/derpmap/default
  auto_update_enabled: true
  update_frequency: 24h

disable_check_updates: true
ephemeral_node_inactivity_timeout: 30m

database:
  type: sqlite
  sqlite:
    path: {kurulum}/db.sqlite

log:
  level: info
  format: text

dns:
  base_domain: headscale.local
  magic_dns: true
  nameservers:
    global:
      - 1.1.1.1
      - 9.9.9.9

policy:
  mode: file
  path: ""

unix_socket: {kurulum}/headscale.sock
unix_socket_permission: "0770"
`,
				Izin:  0640,
				Sahip: "app",
			},
		},
		NginxProxy: &NginxProxyTarifi{
			SubPathOn:   "/headscale",
			UpgradeWS:   true,
			MaxBodySize: "64m",
		},
		SaglikHTTP:  "http://127.0.0.1:{port_web}/health",
		SaglikBekle: 5,
	},

	// -------------------------------------------------------------------
	// TeamSpeak 3 — voice server, native C++ binary tar.bz2
	// UDP 9987 voice + TCP 10011 query + TCP 30033 file transfer
	// EULA otomatik kabul edilir (recipe koşulu). Panel admin sorumluluğunda.
	// -------------------------------------------------------------------
	{
		Kod:      "teamspeak3",
		Ad:       "TeamSpeak 3",
		Aciklama: "Sesli iletişim sunucusu — 32 slot ücretsiz. Client'lar UDP 9987 voice portundan bağlanır.",
		Kategori: "iletisim",
		Ikon:     "🎧",
		LogoURL:  "https://cdn.simpleicons.org/teamspeak/2580C3",
		Surum:    "3.13.7",

		IndirmeURL: "https://files.teamspeak-services.com/releases/server/3.13.7/teamspeak3-server_linux_amd64-3.13.7.tar.bz2",
		SHA256:     "775a5731a9809801e4c8f9066cd9bc562a1b368553139c1249f2a0740d50041e",
		IcerikTuru: "tarball_bz2",

		CalistirKomutu: []string{"{kurulum}/teamspeak3-server_linux_amd64/ts3server",
			"license_accepted=1",
			// Query erişim: sadece localhost'tan (panel backend)
			"query_ip_denylist={kurulum}/query_ip_denylist.txt",
			"query_ip_allowlist={kurulum}/query_ip_allowlist.txt",
			// Portlar — 10080 (webquery HTTP) çakışırsa dinamik havuzdan
			"query_port={port_query}",
			"query_ssh_port={port_query_ssh}",
			"query_http_port={port_query_http}",
			"filetransfer_port={port_file}",
			"voice_ip=0.0.0.0",
			"default_voice_port={port_voice}",
			// Config dizinlerini kurulum altına al (log/data ayrı)
			"logpath={kurulum}/logs",
			"dbsqlpath={kurulum}/teamspeak3-server_linux_amd64/sql/",
			"dbplugin=ts3db_sqlite3",
			"dbsqlcreatepath=create_sqlite/",
			"dbconnection={kurulum}/ts3server.sqlitedb",
			// Ilk kurulumda admin şifresi + query şifresi sabit — panel query için kullanır
			"serveradmin_password={secret16}",
			"serverquery_password={secret16}",
		},
		CalismaDizini: "{kurulum}/teamspeak3-server_linux_amd64",
		CevreDegisken: map[string]string{
			"TS3SERVER_LICENSE": "accept",
			"LD_LIBRARY_PATH":   "{kurulum}/teamspeak3-server_linux_amd64",
		},
		MemoryMax: "256M",
		CPUQuota:  "40%",
		TasksMax:  100,

		Portlar: []PortTarifi{
			{Ad: "voice", Protokol: "udp", Zorunlu: 9987, DisAcik: true},
			{Ad: "query", Protokol: "tcp", Zorunlu: 10011, DisAcik: false}, // localhost-only (panel)
			{Ad: "query_ssh", Protokol: "tcp", DisAcik: false},             // dinamik havuz
			{Ad: "query_http", Protokol: "tcp", DisAcik: false},            // dinamik havuz (eski 10080)
			{Ad: "file", Protokol: "tcp", Zorunlu: 30033, DisAcik: true},
		},
		ConfigDosyalar: []ConfigDosyaTarifi{
			{Yol: "query_ip_allowlist.txt", Icerik: "127.0.0.1\n::1\n"},
			{Yol: "query_ip_denylist.txt", Icerik: ""},
			{Yol: "logs/.keep", Icerik: ""}, // logs dizini oluşsun
		},
		// Query port TCP 10011 açılınca hazır — TS3 banner "TS3" gönderir
		SaglikTCP:   10011,
		SaglikBekle: 6,
	},

	// -------------------------------------------------------------------
	// Statping-ng — uptime monitoring dashboard, Go static
	// -------------------------------------------------------------------
	{
		Kod:      "statping",
		Ad:       "Statping-ng",
		Aciklama: "Servis uptime monitoring + status page — HTTP/TCP/UDP/ICMP check, Discord/Slack alarm.",
		Kategori: "izleme",
		Ikon:     "📈",
		LogoURL:  "https://cdn.simpleicons.org/uptimekuma/5CDD8B",
		Surum:    "0.93.0",

		IndirmeURL: "https://github.com/statping-ng/statping-ng/releases/download/v0.93.0/statping-linux-amd64.tar.gz",
		SHA256:     "263ca172c19a61e7272618c5b4acdaab855580a5289963205c5160024628b82d",
		IcerikTuru: "tarball_gz",
		BinaryYol:  "statping",

		CalistirKomutu: []string{"{kurulum}/statping", "--port={port_web}"},
		CalismaDizini:  "{kurulum}",
		CevreDegisken: map[string]string{
			"STATPING_DIR":   "{kurulum}/data",
			"IS_DOCKER":      "false",
			"ADMIN_USER":     "admin",
			"ADMIN_PASSWORD": "{secret16}",
		},
		MemoryMax: "256M",
		CPUQuota:  "40%",
		TasksMax:  100,

		Portlar: []PortTarifi{
			{Ad: "web", Protokol: "tcp"},
		},
		NginxProxy: &NginxProxyTarifi{
			SubPathOn:   "/statping",
			UpgradeWS:   true,
			MaxBodySize: "16m",
		},
		SaglikHTTP:  "http://127.0.0.1:{port_web}/health",
		SaglikBekle: 6,
	},
}
