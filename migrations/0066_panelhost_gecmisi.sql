-- 0066: cp_panelhost_gecmisi — panel Ayarla/SslKur iş kayıtları (audit + restart-safe)
CREATE TABLE IF NOT EXISTS cp_panelhost_gecmisi (
    id            BIGINT AUTO_INCREMENT PRIMARY KEY,
    is_id         VARCHAR(64)  NOT NULL UNIQUE,
    tip           VARCHAR(16)  NOT NULL COMMENT 'ayarla|sslkur',
    hostname      VARCHAR(255) NOT NULL,
    durum         VARCHAR(16)  NOT NULL DEFAULT 'kosuyor',
    basla         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    bitis         DATETIME     NULL,
    hata          TEXT         NOT NULL DEFAULT '',
    adimlar_json  LONGTEXT     NOT NULL,
    aktor_uid     BIGINT       NULL,
    INDEX idx_tip_basla (tip, basla),
    INDEX idx_durum (durum)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
