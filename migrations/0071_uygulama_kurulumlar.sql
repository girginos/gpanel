-- 0071: cp_uygulama_kurulumlar — tenant-level app installer kayıtları.
CREATE TABLE IF NOT EXISTS cp_uygulama_kurulumlar (
    id           BIGINT AUTO_INCREMENT PRIMARY KEY,
    domain_id    BIGINT       NOT NULL,
    kod          VARCHAR(64)  NOT NULL,
    ad           VARCHAR(128) NOT NULL,
    surum        VARCHAR(32)  NOT NULL DEFAULT '',
    alt_dizin    VARCHAR(255) NOT NULL DEFAULT '' COMMENT '/ = root, blog = /blog/',
    yonetim_url  VARCHAR(512) NOT NULL DEFAULT '',
    db_adi       VARCHAR(64)  NOT NULL DEFAULT '',
    created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_domain (domain_id),
    INDEX idx_kod (kod)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
