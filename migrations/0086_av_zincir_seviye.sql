-- FAZ 3: av_zincir.seviye (tek gerçek kaynak). UI guven>=80 ile yeniden hesaplamaz
-- → bildirim ile çelişmez (ZincirPuanla kritik'i nedensel/3-aşama ile verir).
ALTER TABLE av_zincir ADD COLUMN seviye VARCHAR(16) NOT NULL DEFAULT 'uyari' AFTER guven;
