-- Plana saatlik giden mail üst sınırı (mail eklentisi limiti). 0 = sınırsız.
ALTER TABLE service_plans ADD COLUMN IF NOT EXISTS saatlik_mail_limiti INT NOT NULL DEFAULT 0;
