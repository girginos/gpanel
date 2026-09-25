// Alan adı panosu (AlanPano) — uç yanıt tipleri. Şemalar Go kodundan:
//   /kaynak        internal/kaynak/kaynak.go (Ozet) + sözleşme: db_mb, eposta_olculmuyor
//   /istatistik    internal/istatistik/istatistik.go (Ozet)
//   /performans    internal/performans/performans.go (Ozet)
//   /health        internal/monitor/monitor.go (DomainHealth)
//   /ssl           internal/domains/handlers_ssl.go (sslDurumResp)
//   /backups       internal/backups/backups.go (Yedek)
//   /bildirimler   internal/bildirim/bildirim.go (Bildirim)
//   /canli, /trafik-gecmis — sözleşme "YENİ ARKA UÇ" (musteri-pano-sozlesme.txt)
// Go'da boş dilimler `null` döner: dizi alanları burada `| null` taşır, okuyan `?? []` yapar.

export type Limit = { kullanim: number; limit: number } // limit 0 = sınırsız

export type KaynakOzet = {
  alan_adi: string; sk: string; plan_adi: string; php_surum: string
  ipv4: string; ssl_aktif: boolean; ssl_bitis?: string
  disk_mb: Limit; trafik_mb: Limit
  db_sayisi: Limit; ftp_sayisi: Limit; eposta_sayi: Limit; domain_sayi: Limit
  inode_kullanim: number; inode_limit: number
  dns_kayit: number; cron_is: number
  yedek_sayisi: number; yedek_mb: number
  // Alan adının tüm veritabanlarının toplam boyutu (MB, ondalıklı; ör. 1.45).
  // Ölçülemezse anahtar HİÇ YOK → "ölçülemedi"; veritabanı yoksa 0 → "0 MB".
  db_mb?: number
  // E-posta sayacının gerçek bir kaynağı yoksa true — çubuk gizlenir (hep 0 yanıltır).
  // Bugün arka uç HEP true döndürür (b-be fc06afb).
  eposta_olculmuyor?: boolean
}

// Bir ucun ön yüzdeki durumu: veri yenilenirken eskisi korunur (yukleniyor + veri).
export type UcDurumu<T> = { veri: T | null; yukleniyor: boolean; hata: string | null }

export type Istatistik = {
  alan_adi: string; log_var: boolean
  toplam_istek: number; toplam_bant_mb: number; tekil_ip: number; bot_orani: number
  durum_grup: Record<string, number> | null
  // tarih nginx biçiminde: "25/Sep/2026"
  gunluk: { tarih: string; istek: number }[] | null
}

export type Performans = {
  alan_adi: string; php_surum: string; skor: number
  ogeler: { ad: string; aktif: boolean; deger: string; ayar: string; aciklama: string }[] | null
  oneriler: { metin: string; onem: string; ayar: string }[] | null
}

export type Saglik = {
  url: string; durum_kodu: number; yanit_suresi_ms: number; erisilebilir: boolean
  hata?: string; sema: string; boyut_byte: number; server?: string
  ssl?: { gecerli: boolean; bitis_tarihi: string; kalan_gun: number; cikaran?: string; ozne?: string }
}

export type SSLDurum = { aktif: boolean; kaynak: string; bitis_iso?: string }

// mevcut: yerel (dosya diskte) | uzak_iddia (uzağa taşındı etiketi, doğrulanmadı) | yok
export type Yedek = {
  mevcut: string; id: number; domain_id: number; tip: string; dosya: string
  boyut_b: number; notlar: string; olusturma: string
}

export type Bildirim = {
  id: number; seviye: string; kategori: string; baslik: string; mesaj: string
  domain_id: number | null; okundu: boolean; tarih: string
}
export type BildirimYanit = { bildirimler: Bildirim[] | null; okunmamis: number }

// GET /domains/{id}/canli — kiracı slice'ının anlık ölçümü. İlk yoklamada cpu_yuzde ve
// io_*_bps null (delta için ikinci örnek gerekir) → ön yüz "ölçülüyor…" der, 0 DEMEZ.
export type CanliOrnek = {
  destek: boolean
  cpu_yuzde: number | null // tek çekirdek = 100 (çok çekirdekte >100 olabilir)
  cpu_limit_yuzde: number // 0 = sınırsız
  bellek_b: number
  bellek_limit_b: number // 0 = sınırsız
  io_okuma_bps: number | null
  io_yazma_bps: number | null
  surec: number
  zaman: number // unix saniye
}

export type TrafikAy = { ay: string; gelen_b: number; giden_b: number; toplam_b: number }
// gelen_olculmuyor: gelen bayt hiçbir yerde ölçülmüyor → gelen_b hep 0, toplam_b = giden_b;
// grafik tek seri ("Aktarılan veri") çizer. false/yoksa gelen + giden iki seri.
export type TrafikGecmis = { kalemler: TrafikAy[] | null; gelen_olculmuyor?: boolean }
