-- GirginOSPanel — F1 başlangıç şeması
-- charset utf8mb4 / collation utf8mb4_unicode_ci (Türkçe karakter güvenli)

CREATE DATABASE IF NOT EXISTS panel
  CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE panel;

CREATE TABLE IF NOT EXISTS users (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  username        VARCHAR(64)  NOT NULL UNIQUE,
  email           VARCHAR(255) NOT NULL DEFAULT '',
  password_hash   VARCHAR(255) NOT NULL,
  role            ENUM('admin','reseller','user') NOT NULL DEFAULT 'user',
  reseller_id     BIGINT UNSIGNED NULL,
  full_name       VARCHAR(255) NOT NULL DEFAULT '',
  status          ENUM('active','suspended') NOT NULL DEFAULT 'active',
  last_login_at   TIMESTAMP NULL,
  last_login_ip   VARCHAR(45) NOT NULL DEFAULT '',
  created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY ix_users_reseller (reseller_id),
  KEY ix_users_role     (role),
  CONSTRAINT fk_users_reseller
    FOREIGN KEY (reseller_id) REFERENCES users(id) ON DELETE SET NULL
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS audit_log (
  id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ts              TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  actor_user_id   BIGINT UNSIGNED NULL,
  actor_username  VARCHAR(64) NOT NULL DEFAULT '',
  ip              VARCHAR(45) NOT NULL DEFAULT '',
  action          VARCHAR(64) NOT NULL,
  target          VARCHAR(255) NOT NULL DEFAULT '',
  detail          JSON NULL,
  ok              TINYINT(1) NOT NULL DEFAULT 1,
  KEY ix_audit_ts     (ts),
  KEY ix_audit_actor  (actor_user_id),
  KEY ix_audit_action (action)
) ENGINE=InnoDB;
