// Müşteri ana sayfasının okuduğu uçların yanıt tipleri (hepsi müşteriye açık uçlar).
// Şemalar Go kodundan:
//   GET /domains/{id}/subdomain   internal/subdomain/subdomain.go (Sub)
//   GET /domains/{id}/backups     internal/backups/backups.go (Yedek; yeniden eskiye)
//   GET /php/versions             internal/php/php.go (Surum; eol phpsurum meta'sından)
//   GET /bildirimler              internal/bildirim (rol kapsamlı: müşteri yalnız kendi domaini)
//   GET /eklentiler               internal/eklenti (müşteriye yalnız musteri_erisim=1 olanlar)
// /domains/{id} → components/domainTipi.ts, /domains/{id}/kaynak → components/alanpano/tipler.ts

export type AltAlan = { id: number; alt_ad: string; tam_ad: string; php_surum: string; docroot: string; created_at: string }

// mevcut: 'yok' = kaydın dosyası hiçbir yerde bulunamadı (geçerli yedek DEĞİL).
export type Yedek = { id: number; tip: string; boyut_b: number; olusturma: string; mevcut?: string }

export type PhpSurum = { surum: string; eol?: boolean }

export type Bildirim = { id: number; seviye: string; kategori: string; baslik: string; mesaj: string; domain_id: number | null; okundu: boolean; tarih: string }
export type BildirimYaniti = { bildirimler: Bildirim[]; okunmamis: number }

export type EklentiKaydi = { ad: string; aktif: boolean }

export type AlanTuru = 'ana' | 'alt'

// Alan adları panelinin ve kapsam seçicinin ortak satırı.
export type AlanSatiri = {
  anahtar: string // 'ana' | 'alt-<id>'
  ad: string
  tur: AlanTuru
  altId: number | null
  php: string
  phpEol: boolean
}

export type UyariTonu = 'bilgi' | 'uyari' | 'tehlike'
export type UyariEylemi = { etiket: string; ikon: string; yol: string }
export type Uyari = { id: string; ton: UyariTonu; baslik: string; aciklama: string; eylem?: UyariEylemi }
