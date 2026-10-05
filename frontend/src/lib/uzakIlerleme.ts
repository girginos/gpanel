import { useEffect, useRef, useState } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { cevirT } from '@/lib/cevirT'
import { api } from '@/lib/api'

// Google Drive gönderiminin CANLI ilerlemesi: sunucu ucu GET /admin/backups/uzak-kopya/ilerleme
// (bellek izleyicisi; hız ve süre yalnız gerçek ölçümden). Yoklama: etkinken hızlı, boştayken yavaş,
// sekme gizliyken DURUR (görünür olunca hemen yenilenir), bileşen kalkınca temizlenir, istekler
// üst üste binmez (zincirleme setTimeout; önceki yanıt gelmeden yenisi gitmez).

const EN: Record<string, string> = {
  'Drive’a gönderiliyor': 'Sending to Drive',
  'Gönderim tamamlandı': 'Upload finished',
  'Şifreleniyor': 'Encrypting',
  'Yükleniyor': 'Uploading',
  'Doğrulanıyor': 'Verifying',
  'şifreleniyor': 'encrypting',
  'yükleniyor': 'uploading',
  'doğrulanıyor': 'verifying',
  '{0}/{1} yedek': '{0}/{1} backups',
  'yaklaşık {0} dk kalan': 'about {0} min left',
  'yaklaşık {0} sa {1} dk kalan': 'about {0} h {1} min left',
  '1 dakikadan az kalan': 'less than 1 min left',
  'süre hesaplanıyor…': 'estimating time…',
  '{0} yedek yeniden denenecek': '{0} backups will be retried',
  'Tamamlandı — {0} yedek gönderildi, {1} hata': 'Finished — {0} backups sent, {1} errors',
  'Hata alan işler için “Son işler” bölümüne bakın.': 'See “Recent jobs” for the failed ones.',
  'Son işler': 'Recent jobs',
  'Gönderim ilerlemesi': 'Upload progress',
  'sırada': 'queued',
  'Gönderiliyor': 'Sending',
}
export const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)

export type UzakAktifIs = {
  is_id: number; yedek_id: number; domain: string; dosya: string; evre: string
  evre_yuzde: number; is_yuzde: number; bayt_gonderilen: number; bayt_toplam: number; hiz_bps: number
}
export type UzakKuyruk = {
  toplam_is: number; tamam: number; hata: number; bekleyen: number; yeniden_denenecek: number
  toplam_bayt: number; tamamlanan_bayt: number; kalan_bayt: number; yuzde: number; hiz_bps: number; eta_sn: number | null
}
export type UzakIlerleme = { aktif: UzakAktifIs | null; kuyruk: UzakKuyruk; aktif_mi: boolean; tamamlandi: boolean }

// Bayt biçimi: 1024 tabanlı (mevcut fmtByte ile aynı), ondalık ayracı dile göre.
export function fmtBayt(b: number): string {
  const nf = new Intl.NumberFormat(i18n.language === 'en' ? 'en-US' : 'tr-TR', { minimumFractionDigits: 1, maximumFractionDigits: 1 })
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${nf.format(b / 1024)} KB`
  if (b < 1024 * 1024 * 1024) return `${nf.format(b / 1024 / 1024)} MB`
  return `${nf.format(b / 1024 / 1024 / 1024)} GB`
}

// "yaklaşık 12 dk kalan": kesinlik taslamaz; dakikaya yukarı yuvarlar.
export function kalanSure(sn: number): string {
  if (sn < 45) return cevir('1 dakikadan az kalan')
  const dk = Math.ceil(sn / 60)
  return dk >= 60
    ? cevirT(cevir('yaklaşık {0} sa {1} dk kalan'), Math.floor(dk / 60), dk % 60)
    : cevirT(cevir('yaklaşık {0} dk kalan'), dk)
}

export function evreEtiket(evre: string, buyuk = false): string {
  switch (evre) {
    case 'sifreleniyor': return cevir(buyuk ? 'Şifreleniyor' : 'şifreleniyor')
    case 'dogrulaniyor': return cevir(buyuk ? 'Doğrulanıyor' : 'doğrulanıyor')
    default: return cevir(buyuk ? 'Yükleniyor' : 'yükleniyor')
  }
}

export const sinirla = (y: number): number => (Number.isFinite(y) ? Math.min(100, Math.max(0, y)) : 0)

export function ilerlemeGorunur(v: UzakIlerleme | null): boolean {
  if (!v) return false
  return v.aktif_mi || v.aktif !== null || v.kuyruk.bekleyen > 0 || v.kuyruk.yeniden_denenecek > 0 || v.tamamlandi
}

// useUzakIlerleme — ilerleme ucunu yoklar. etkin=false → hiç istek yok. hizlandir artınca (ör. "Gönder"
// tıklandı) yoklama hemen yeniden başlar.
export function useUzakIlerleme(etkin: boolean, hizlandir = 0, hizliMs = 2000, yavasMs = 15000): UzakIlerleme | null {
  const [veri, setVeri] = useState<UzakIlerleme | null>(null)
  useEffect(() => {
    if (!etkin) { setVeri(null); return }
    let iptal = false
    let gidiyor = false
    let zamanlayici: ReturnType<typeof setTimeout> | undefined
    let son: UzakIlerleme | null = null
    const planla = (ms: number) => {
      if (zamanlayici) clearTimeout(zamanlayici)
      if (!iptal) zamanlayici = setTimeout(() => { void tur() }, ms)
    }
    const tur = async () => {
      if (iptal || gidiyor) return
      if (document.hidden) return // görünür olunca visibilitychange yeniden başlatır
      gidiyor = true
      try {
        const { data } = await api.get<UzakIlerleme>('/admin/backups/uzak-kopya/ilerleme')
        if (iptal) return
        son = data
        setVeri(data)
      } catch { /* bir sonraki turda yeniden denenir */ } finally { gidiyor = false }
      if (!iptal) planla(son && (son.aktif_mi || son.kuyruk.bekleyen > 0) ? hizliMs : yavasMs)
    }
    const gorunurOlunca = () => { if (!document.hidden) planla(0) }
    document.addEventListener('visibilitychange', gorunurOlunca)
    planla(0)
    return () => {
      iptal = true
      if (zamanlayici) clearTimeout(zamanlayici)
      document.removeEventListener('visibilitychange', gorunurOlunca)
    }
  }, [etkin, hizlandir, hizliMs, yavasMs])
  return etkin ? veri : null
}

// Aktif iş bitince / sona erince geri çağrı (özet ve tablo yenilensin).
export function useIlerlemeBitisi(v: UzakIlerleme | null, f: () => void) {
  const onceki = useRef(false)
  const fn = useRef(f)
  fn.current = f
  const simdi = !!v && (v.aktif_mi || v.aktif !== null)
  useEffect(() => {
    if (onceki.current && !simdi) fn.current()
    onceki.current = simdi
  }, [simdi])
}

