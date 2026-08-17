-- 0065: cp_port_gecmisi — panel port değişiklik geçmişi (audit)
CREATE TABLE IF NOT EXISTS cp_port_gecmisi (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    tip         VARCHAR(16)  NOT NULL COMMENT 'backend|dis',
    eski_port   INT          NOT NULL,
    yeni_port   INT          NOT NULL,
    basarili    TINYINT(1)   NOT NULL DEFAULT 0,
    rollback    TINYINT(1)   NOT NULL DEFAULT 0,
    son_hata    TEXT         NOT NULL DEFAULT '',
    aktor_uid   BIGINT       NULL,
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
