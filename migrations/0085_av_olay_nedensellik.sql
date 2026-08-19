-- FAZ 2 nedensellik: av_olay'a yol (dosya/exe) + pid → korelasyonda nedensel bağ
-- (aynı yol/pid/dizin) salt-zamansal FP-laundering'i keser.
ALTER TABLE av_olay
  ADD COLUMN yol VARCHAR(512) NOT NULL DEFAULT '' AFTER ozet,
  ADD COLUMN pid INT NOT NULL DEFAULT 0 AFTER yol;
