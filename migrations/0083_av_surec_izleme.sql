-- FAZ1 süreç davranış izleme (netlink) aç/kapa. Varsayılan 0 (opt-in).
ALTER TABLE av_ayarlar ADD COLUMN surec_izleme TINYINT(1) NOT NULL DEFAULT 0;
