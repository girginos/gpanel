// Website Security Monitor — "Tara" sonrası geri bildirim için SAF yardımcılar
// (React'siz; sayfa bunları kullanır, kaynak kapsam testi bu dosyayı da denetler).
import i18n from '@/lib/i18n'

// Sunucu zamanları MariaDB UTC "YYYY-AA-GG SS:DD:ss" dizgesidir (saat dilimi yok).
// TEK biçimleyici: üst kart ve tablo satırı aynı çıktıyı verir — kullanıcının yerel
// saat dilimi, kısa tarih + kısa saat. Boş/geçersiz → '—' / ham metin.
export function zamanYaz(s: string | null | undefined): string {
  if (!s) return '—'
  const d = new Date(s.includes(' ') ? s.replace(' ', 'T') + 'Z' : s)
  if (Number.isNaN(d.getTime())) return s
  return d.toLocaleString(i18n.language === 'en' ? 'en-US' : 'tr-TR', { dateStyle: 'short', timeStyle: 'short' })
}

export type DomainSatir = {
  domain_id: number
  durum: string
  domain_son_tarama?: string
  son_sonuc?: { uygulama_sayisi: number; bulgu_sayisi: number }
}

// Bekleyen: istek sonrası sonucu beklenen domain. onceki = istek anındaki domain
// son tarama damgası; aktifGoruldu = satır bir kez sırada/taranıyor göründü.
export type Bekleyen = { onceki: string; aktifGoruldu: boolean; t0: number }

export const aktifDurum = (d: string) => d === 'taraniyor' || d === 'sirada'

// Tarama çok kısa sürebilir (0 uygulamada ~6 ms): "taranıyor" hiç görülmeyebilir.
// Bu yüzden bitiş = domain son tarama damgasının DEĞİŞMESİ ya da (aktif görüldükten
// sonra) aktif olmaktan çıkması. ZAMAN_ASIMI_MS sonunda beklemeyi bırakır.
export const ZAMAN_ASIMI_MS = 3 * 60 * 1000
export const YOKLAMA_MS = 2000

export type Degerlendirme = {
  biten: number[]
  zamanAsimi: number[]
  kalan: Map<number, Bekleyen>
  uygulama: number
  bulgu: number
}

export function degerlendir(bekleyen: Map<number, Bekleyen>, satirlar: DomainSatir[], simdi: number): Degerlendirme {
  const kalan = new Map<number, Bekleyen>()
  const biten: number[] = []
  const zamanAsimi: number[] = []
  let uygulama = 0
  let bulgu = 0
  const sonuc = new Map<number, DomainSatir>()
  const aktif = new Map<number, boolean>()
  for (const s of satirlar) {
    if (!sonuc.has(s.domain_id)) sonuc.set(s.domain_id, s)
    if (aktifDurum(s.durum)) aktif.set(s.domain_id, true)
  }
  for (const [id, b] of bekleyen) {
    const s = sonuc.get(id)
    if (!s) { biten.push(id); continue } // domain silinmiş/görünmez: beklemeyi bırak
    const aktifMi = aktif.get(id) === true
    const goruldu = b.aktifGoruldu || aktifMi
    const degisti = (s.domain_son_tarama ?? '') !== b.onceki
    if (!aktifMi && (degisti || goruldu)) {
      biten.push(id)
      uygulama += s.son_sonuc?.uygulama_sayisi ?? 0
      bulgu += s.son_sonuc?.bulgu_sayisi ?? 0
    } else if (simdi - b.t0 > ZAMAN_ASIMI_MS) {
      zamanAsimi.push(id)
    } else {
      kalan.set(id, { ...b, aktifGoruldu: goruldu })
    }
  }
  return { biten, zamanAsimi, kalan, uygulama, bulgu }
}
