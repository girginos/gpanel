-- 0070: hostuyg app yedekleri — kurulum dizini + env + unit dosyası tar.gz
CREATE TABLE IF NOT EXISTS cp_host_uyg_yedekler (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    uygulama_id  BIGINT       NOT NULL,
    dosya        VARCHAR(512) NOT NULL COMMENT '/var/backups/gpanel-apps/<kod-ornek>/<ts>.tar.gz',
    boyut_byte   BIGINT       NOT NULL DEFAULT 0,
    sha256       CHAR(64)     NOT NULL,
    aciklama     VARCHAR(255) NOT NULL DEFAULT '',
    olusturan    BIGINT       NULL,
    olusturma    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_uyg_zaman (uygulama_id, olusturma),
    FOREIGN KEY (uygulama_id) REFERENCES cp_host_uygulamalar(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
