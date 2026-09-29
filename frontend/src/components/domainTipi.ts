// Domain kaydı (GET /domains, /domains/:id) — alan adı sayfalarının ortak tipi.
// (Kullanılmayan eski alan adı listesi bileşeni kaldırılınca buraya taşındı.)
export type Domain = {
  id: number
  alan_adi: string
  php_surum: string
  ssl: boolean
  ssl_bitis?: string
  durum: 'aktif' | 'pasif' | string
  sistem_kullanici: string
  boyut_kb: number
  trafik_kb: number
  olusturulma: string
  ipv4: string
  ftp_host: string
  ftp_user: string
  db_host: string
  db_user: string
  db_adi: string
  web_root: string
  notlar?: string
  ssh_erisim?: boolean
  askida?: boolean
  // Hizmet planının adı (plan atanmamışsa yok) — internal/domains Domain.PlanAd.
  plan_ad?: string
}
