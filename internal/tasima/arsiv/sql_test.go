package arsiv

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func sqlT(t *testing.T, s string) (SQLRapor, error) {
	t.Helper()
	return SQLTara(context.Background(), strings.NewReader(s), SQLSinir{})
}

func kuralBeklenen(t *testing.T, s, kural string) {
	t.Helper()
	_, err := sqlT(t, s)
	var he *SQLHata
	if !errors.As(err, &he) || !errors.Is(err, ErrSQL) {
		t.Fatalf("SQLHata beklendi, gelen %v\nGİRDİ: %q", err, s)
	}
	if he.Kural != kural {
		t.Fatalf("kural %q beklendi, gelen %q\nGİRDİ: %q", kural, he.Kural, s)
	}
}

const iyiDump = "-- MySQL dump 10.13\n" +
	"/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;\n" +
	"/*!40101 SET NAMES utf8mb4 */;\n" +
	"/*!40103 SET TIME_ZONE='+00:00' */;\n" +
	"/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;\n" +
	"/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;\n" +
	"DROP TABLE IF EXISTS `user`;\n" +
	"CREATE TABLE `user` (\n  `id` int NOT NULL,\n  `system` varchar(20) DEFAULT NULL,\n  `source` text,\n  PRIMARY KEY (`id`)\n) ENGINE=InnoDB;\n" +
	"LOCK TABLES `user` WRITE;\n" +
	"INSERT INTO `user` VALUES (1,'GRANT ALL ON *.* TO x','-- not a comment; DROP USER y;'),(2,'it\\'s; GRANT','a\\\\');\n" +
	"INSERT INTO `user` VALUES (3,'/* x','system id\nsource /etc/passwd');\n" +
	"UNLOCK TABLES;\n" +
	"DELIMITER ;;\n" +
	"/*!50003 CREATE*/ /*!50017 DEFINER=`root`@`localhost`*/ /*!50003 TRIGGER `t` BEFORE INSERT ON `user` FOR EACH ROW BEGIN\n  SET NEW.id = NEW.id + 1;\nEND */;;\n" +
	"DELIMITER ;\n" +
	"# bir yorum\n" +
	"-- Dump completed on 2026-10-06\n"

func TestSQL_MesruDokumGecer(t *testing.T) {
	r, err := sqlT(t, iyiDump)
	if err != nil {
		t.Fatalf("meşru döküm reddedildi: %v", err)
	}
	if !r.Tamam || r.Tablo != 1 || r.Definer != 1 || r.Rutin != 1 || r.Ifade < 9 {
		t.Fatalf("beklenmeyen rapor: %+v", r)
	}
}

func TestSQL_Reddedilenler(t *testing.T) {
	cases := []struct{ ad, sql, kural string }{
		{"grant", "GRANT ALL ON *.* TO 'x'@'%';\n", KuralYetki},
		{"grant-sürümlü-yorumda", "/*!40000 GRANT ALL ON *.* TO 'x' */;\n", KuralYetki},
		{"grant-rutin-gövdesinde", "DELIMITER ;;\nCREATE PROCEDURE p() BEGIN GRANT ALL ON *.* TO x; END;;\n", KuralYetki},
		{"revoke", "CREATE TABLE t (a int);\nREVOKE ALL ON *.* FROM x;\n", KuralYetki},
		{"create-user", "CREATE USER 'x'@'%' IDENTIFIED BY 'p';\n", KuralYonetim},
		{"create-user-ine", "CREATE USER IF NOT EXISTS x;\n", KuralYonetim},
		{"alter-user", "ALTER USER root@localhost IDENTIFIED BY 'p';\n", KuralYonetim},
		{"drop-user", "DROP USER x;\n", KuralYonetim},
		{"create-database", "CREATE DATABASE IF NOT EXISTS x;\n", KuralYonetim},
		{"drop-database", "DROP DATABASE `mysql`;\n", KuralYonetim},
		{"create-schema", "CREATE SCHEMA x;\n", KuralYonetim},
		{"set-password", "SET PASSWORD FOR root@localhost = 'x';\n", KuralSQLKipi},
		{"set-global", "SET GLOBAL general_log_file='/x';\n", KuralSQLKipi},
		{"set-persist", "SET PERSIST max_connections=1;\n", KuralSQLKipi},
		{"outfile", "CREATE TABLE t AS SELECT 1 INTO OUTFILE '/tmp/x';\n", KuralDosya},
		{"dumpfile", "INSERT INTO t VALUES (1) INTO DUMPFILE '/tmp/x';\n", KuralDosya},
		{"load-data", "LOAD DATA INFILE '/etc/passwd' INTO TABLE t;\n", KuralIzinsizIfade},
		{"load-data-local", "LOAD DATA LOCAL INFILE '/etc/passwd' INTO TABLE t;\n", KuralIzinsizIfade},
		{"load_file", "INSERT INTO t VALUES (LOAD_FILE('/etc/passwd'));\n", KuralDosya},
		{"udf-soname", "CREATE FUNCTION f RETURNS STRING SONAME 'x.so';\n", KuralDosya},
		{"select", "SELECT * FROM mysql.user;\n", KuralIzinsizIfade},
		{"call", "CALL p();\n", KuralIzinsizIfade},
		{"prepare", "SET @a = 1;\nPREPARE s FROM 'DROP TABLE x';\n", KuralOlmayanIfade},
		{"flush", "FLUSH PRIVILEGES;\n", KuralIzinsizIfade},
		{"shutdown", "SHUTDOWN;\n", KuralIzinsizIfade},
		{"sleep", "INSERT INTO t VALUES (SLEEP(9999));\n", KuralKaynak},
		{"benchmark", "INSERT INTO t VALUES (BENCHMARK(1e9, MD5(1)));\n", KuralKaynak},
		{"istemci-kabuk", "CREATE TABLE t (a int);\n\\! id\n", KuralIstemci},
		{"istemci-kaynak-ters-bölü", "\\. /etc/passwd\n", KuralIstemci},
		{"istemci-db-seç", "\\u mysql\n", KuralIstemci},
		{"istemci-system", "INSERT INTO t VALUES (1);\nsystem id\n", KuralIstemci},
		{"istemci-source", "source /etc/passwd\n", KuralIstemci},
		{"istemci-use", "USE mysql;\n", KuralIstemci},
		{"istemci-tee", "tee /root/x\n", KuralIstemci},
		{"istemci-ifade-ortasında", "CREATE TABLE t (\nsystem id\n);\n", KuralIstemci},
		{"istemci-büyük-harf", "SYSTEM id\n", KuralIstemci},
		{"istemci-yorumdan-sonra", "/**/ system id\n", KuralIstemci},
		{"sql-kipi-düz", "SET sql_mode='NO_BACKSLASH_ESCAPES';\n", KuralSQLKipi},
		{"sql-kipi-sürümlü", "/*!40101 SET SQL_MODE='ANSI_QUOTES,NO_BACKSLASH_ESCAPES' */;\n", KuralSQLKipi},
		{"sql-kipi-ansi", "SET sql_mode='ANSI';\n", KuralSQLKipi},
		{"sql-kipi-birleşik-dizgi", "SET sql_mode='NO_BACK' 'SLASH_ESCAPES';\n", KuralSQLKipi},
		{"sql-kipi-işlev", "SET sql_mode=CONCAT('NO_BACK','SLASH_ESCAPES');\n", KuralSQLKipi},
		{"sql-kipi-onaltılık", "SET sql_mode=0x4e4f5f4241434b534c4153485f45534341504553;\n", KuralSQLKipi},
		{"sql-kipi-onaltılık-x", "SET sql_mode=x'4e4f';\n", KuralSQLKipi},
		{"sql-kipi-alt-sorgu", "SET @@sql_mode=(SELECT 1);\n", KuralSQLKipi},
		{"sql-kipi-dev-dizgi", "SET @a='" + strings.Repeat("x", 5000) + "';\n", KuralSQLKipi},
		{"delimiter-ters-bölü", "DELIMITER \\\\\n", KuralIstemci},
		{"delimiter-boş", "DELIMITER\n", KuralIstemci},
		{"delimiter-ifade-ortasında", "CREATE TABLE t (\nDELIMITER ;;\n);\n", KuralIstemci},
		{"kesik-dizgi", "INSERT INTO t VALUES ('abc);\n", KuralBozuk},
		{"kesik-ident", "INSERT INTO `t VALUES (1);\n", KuralBozuk},
		{"kesik-sürümlü-yorum", "/*!40101 SET NAMES utf8 ;\n", KuralBozuk},
		{"kesik-blok-yorum", "/* kapanmıyor\nCREATE TABLE t (a int);\n", KuralBozuk},
		{"ters-bölü-kodda", "CREATE TABLE t (a int) \\g\n", KuralIstemci},
		{"ilk-simge-sayı", "1;\n", KuralIzinsizIfade},
		{"ilk-simge-parantez", "(SELECT 1);\n", KuralIzinsizIfade},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) { kuralBeklenen(t, c.sql, c.kural) })
	}
}

// Dizgi içindeki '\” dizgiyi bitirmez: ardındaki "; GRANT" veridir (yanlış pozitif yok);
// ama dizgi DIŞINA taşan bir GRANT ayrıştırıcı farkı olmadan yakalanır.
func TestSQL_DizgiKacisiVeriKalir(t *testing.T) {
	if _, err := sqlT(t, "INSERT INTO t VALUES ('a\\'; GRANT ALL ON *.* TO x; --');\n"); err != nil {
		t.Fatalf("dizgi içi GRANT veri sayılmalı: %v", err)
	}
	kuralBeklenen(t, "INSERT INTO t VALUES ('a'); GRANT ALL ON *.* TO x;\n", KuralYetki)
	kuralBeklenen(t, "INSERT INTO t VALUES ('a\\\\'); GRANT ALL ON *.* TO x;\n", KuralYetki)
	kuralBeklenen(t, "INSERT INTO t VALUES (\"a\"); GRANT ALL ON *.* TO x;\n", KuralYetki)
}

// ';;' ayırıcısıyla gizlenen ifade: ayırıcı mysql istemcisiyle aynı değişir.
func TestSQL_AyiriciDegisimiIfadeyiGizlemez(t *testing.T) {
	kuralBeklenen(t, "DELIMITER //\nINSERT INTO t VALUES (1)//\nGRANT ALL ON *.* TO x//\n", KuralYetki)
	kuralBeklenen(t, "DELIMITER ;;\nCREATE TABLE t (a int);;\nDELIMITER ;\nCREATE USER x;\n", KuralYonetim)
}

func TestSQL_BoyutSiniri(t *testing.T) {
	_, err := SQLTara(context.Background(), strings.NewReader(iyiDump), SQLSinir{ToplamBayt: 100})
	var he *SQLHata
	if !errors.As(err, &he) || he.Kural != KuralBoyut {
		t.Fatalf("boyut sınırı beklendi: %v", err)
	}
}

func TestSQL_IptalEdilebilir(t *testing.T) {
	ctx, iptal := context.WithCancel(context.Background())
	iptal()
	büyük := "INSERT INTO t VALUES ('" + strings.Repeat("a", 3<<20) + "');\n"
	if _, err := SQLTara(ctx, strings.NewReader(büyük), SQLSinir{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("iptal beklendi: %v", err)
	}
}

func TestSQL_BosVeSonlandiricisizSon(t *testing.T) {
	if r, err := sqlT(t, ""); err != nil || r.Ifade != 0 {
		t.Fatalf("boş döküm: %+v %v", r, err)
	}
	if r, err := sqlT(t, "CREATE TABLE t (a int)"); err != nil || r.Ifade != 1 || r.Tablo != 1 {
		t.Fatalf("sonlandırıcısız son ifade: %+v %v", r, err)
	}
	kuralBeklenen(t, "CREATE TABLE t (a int);\nGRANT ALL ON *.* TO x", KuralYetki)
}
