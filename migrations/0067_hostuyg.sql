-- 0067: host-level uygulamalar (native app framework — admin only)
--
-- cp_host_uygulamalar: kurulu uygulamaların ana kaydı.
-- cp_host_uyg_portlari: her uygulamanın rezerv ettiği port(lar).
--   - Port havuzu 7000-7999.
--   - Bir uygulama birden fazla port alabilir (TeamSpeak: 9987 UDP + 30033 TCP).
--   - Uygulama silinince port'lar da silinir (ON DELETE CASCADE).

CREATE TABLE IF NOT EXISTS cp_host_uygulamalar (
    id            BIGINT AUTO_INCREMENT PRIMARY KEY,
    kod           VARCHAR(64)  NOT NULL UNIQUE COMMENT 'recipe kodu — vaultwarden, gitea, teamspeak',
    ornek_ad      VARCHAR(128) NOT NULL COMMENT 'kullanıcı-görünür isim — birden fazla kurulum için farklı',
    surum         VARCHAR(32)  NOT NULL DEFAULT '',
    kurulum_yolu  VARCHAR(255) NOT NULL COMMENT '/opt/gpanel-apps/<kod-ornek>',
    sistem_kullanici VARCHAR(64) NOT NULL COMMENT 'gpanel-app-<kod-ornek>',
    systemd_unit  VARCHAR(128) NOT NULL COMMENT 'gpanel-app-<kod-ornek>.service',
    durum         VARCHAR(16)  NOT NULL DEFAULT 'kurulmakta' COMMENT 'kurulmakta|aktif|durduruldu|hata|kaldirilmakta',
    son_hata      TEXT         NOT NULL DEFAULT '',
    meta_json     TEXT         NOT NULL DEFAULT '{}' COMMENT 'sürüm-özel yapılandırma, secret hash vb.',
    kuran_uid     BIGINT       NULL,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_kod (kod),
    INDEX idx_durum (durum)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cp_host_uyg_portlari (
    id            BIGINT AUTO_INCREMENT PRIMARY KEY,
    uygulama_id   BIGINT       NOT NULL,
    port          INT          NOT NULL,
    protokol      VARCHAR(8)   NOT NULL DEFAULT 'tcp' COMMENT 'tcp|udp|both',
    aciklama      VARCHAR(128) NOT NULL DEFAULT '',
    firewall_acik TINYINT(1)   NOT NULL DEFAULT 0,
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY unq_port_proto (port, protokol),
    INDEX idx_uyg (uygulama_id),
    FOREIGN KEY (uygulama_id) REFERENCES cp_host_uygulamalar(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cp_host_uyg_islemler (
    id            BIGINT AUTO_INCREMENT PRIMARY KEY,
    is_id         VARCHAR(64)  NOT NULL UNIQUE,
    uygulama_id   BIGINT       NULL,
    tip           VARCHAR(16)  NOT NULL COMMENT 'kur|kaldir|baslat|durdur',
    kod           VARCHAR(64)  NOT NULL,
    durum         VARCHAR(16)  NOT NULL DEFAULT 'kosuyor',
    basla         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    bitis         DATETIME     NULL,
    hata          TEXT         NOT NULL DEFAULT '',
    adimlar_json  LONGTEXT     NOT NULL,
    aktor_uid     BIGINT       NULL,
    INDEX idx_uyg (uygulama_id),
    INDEX idx_tip_basla (tip, basla)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
