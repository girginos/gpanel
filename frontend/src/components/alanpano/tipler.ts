// Alan adı kaynak uç tipleri (DomainKaynakKart + abonelik sayfası). Şema Go kodundan:
//   /kaynak        internal/kaynak/kaynak.go (Ozet) + sözleşme: db_mb, eposta_olculmuyor

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
