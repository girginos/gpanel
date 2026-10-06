// Site Taşıma — kaynak listesi ve uç tabanı.
//
// Kaynaklar (Plesk, GirginOSPanel, cPanel, DirectAdmin …) ön yüzde sabit DEĞİL: sunucudaki
// kayıt defterinden gelir (GET /tasima/kaynaklar). Kök yeni kaynak açınca bayi aynı listeyi
// yeniden derleme olmadan görür; kökün kapattığı kaynak bayiye hiç gelmez.
//
// Yanıt sözleşmesi:
//   { acik: boolean,        bu oturum taşıma kullanabilir mi. Kök: her zaman true.
//                           Bayi: tasima_bayi_acik anahtarı (kapalıyken kaynaklar boş).
//     bayi_acik: boolean,   yalnız kök yanıtında anlamlı: "Bayiler kullanabilir" anahtarı.
//     kaynaklar: [{ kod, etiket, olgunluk: 'dogrulandi'|'deneysel', etkin: boolean,
//                   yetenekler: { dosya, veritabani, dns, ssl, posta, sahipler } }] }
//   Bayiye yalnız etkin kaynaklar gelir; kök hepsini görür (kaynak anahtarları için).
//
// 🔴 Yanıt güvenilmez girdi gibi ayıklanır ve KAPALI tarafta kalır: bilinmeyen/eksik alan
// "yok" sayılır (acik, etkin ve her yetenek yalnız tam `true` ise açık); geçersiz kodlu
// kaynak atılır (kod URL'ye ve seçici değerine girer); bilinmeyen olgunluk "deneysel"dir.
// Yetki kararı sunucudadır; bu ayıklama yalnız arayüzün yanlış bir şey çizmesini önler.
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, apiHata } from '@/lib/api'

/** Taşıma uçlarının tabanı (eski `/system/tasima` yalnız kök için takma ad). */
export const TASIMA_TABAN = '/tasima'

export type TasimaOlgunluk = 'dogrulandi' | 'deneysel'

export type TasimaYetenek = {
  dosya: boolean
  veritabani: boolean
  dns: boolean
  ssl: boolean
  posta: boolean
  sahipler: boolean
  /** Cron görevleri taşınır mı (arşiv kaynakları için true). */
  cron: boolean
}

export type TasimaKaynak = {
  kod: string
  etiket: string
  olgunluk: TasimaOlgunluk
  etkin: boolean
  /** Arşiv kaynağı mı (dosya yolu/yükleme ile; SSH sunucusu yok). */
  arsiv: boolean
  /** Yalnız kök yanıtında anlamlı: bayiler bu arşiv kaynağını kullanabilir mi. */
  bayi_etkin: boolean
  yetenekler: TasimaYetenek
}

/** Arşiv boyut tavanları (bayt). 0 = bilinmiyor → arayüz yüklemeyi/yolu kapalı tutar. */
export type ArsivLimitleri = { yol_tavan: number; yukleme_tavan: number; parca: number }

export type TasimaKaynaklari = {
  acik: boolean
  bayi_acik: boolean
  kaynaklar: TasimaKaynak[]
  arsiv_limitleri: ArsivLimitleri
}

// Kod sunucuda VARCHAR(32) ve PUT /tasima/kaynaklar/{kod} yolunda kullanılır.
const KOD_DESENI = /^[a-z0-9][a-z0-9_-]{0,31}$/

const YETENEK_ADLARI = ['dosya', 'veritabani', 'dns', 'ssl', 'posta', 'sahipler', 'cron'] as const

function nesneMi(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function yetenekAyikla(ham: unknown): TasimaYetenek {
  const y = nesneMi(ham) ? ham : {}
  const sonuc = {} as TasimaYetenek
  for (const ad of YETENEK_ADLARI) sonuc[ad] = y[ad] === true
  return sonuc
}

function kaynakAyikla(ham: unknown): TasimaKaynak | null {
  if (!nesneMi(ham)) return null
  const kod = ham.kod
  if (typeof kod !== 'string' || !KOD_DESENI.test(kod)) return null
  const etiket = typeof ham.etiket === 'string' && ham.etiket.trim() !== '' ? ham.etiket.trim() : kod
  return {
    kod,
    etiket,
    olgunluk: ham.olgunluk === 'dogrulandi' ? 'dogrulandi' : 'deneysel',
    etkin: ham.etkin === true,
    arsiv: ham.arsiv === true,
    bayi_etkin: ham.bayi_etkin === true,
    yetenekler: yetenekAyikla(ham.yetenekler),
  }
}

function limitSayi(v: unknown): number {
  return typeof v === 'number' && Number.isFinite(v) && v > 0 ? v : 0
}

export function arsivLimitleriAyikla(ham: unknown): ArsivLimitleri {
  const l = nesneMi(ham) ? ham : {}
  return { yol_tavan: limitSayi(l.yol_tavan), yukleme_tavan: limitSayi(l.yukleme_tavan), parca: limitSayi(l.parca) }
}

/** Ham yanıtı sözleşmeye indirger (bkz. dosya başı). */
export function tasimaKaynaklariAyikla(ham: unknown): TasimaKaynaklari {
  const g = nesneMi(ham) ? ham : {}
  const acik = g.acik === true
  const kaynaklar: TasimaKaynak[] = []
  if (acik && Array.isArray(g.kaynaklar)) {
    const gorulen = new Set<string>()
    for (const k of g.kaynaklar) {
      const kay = kaynakAyikla(k)
      if (!kay || gorulen.has(kay.kod)) continue
      gorulen.add(kay.kod)
      kaynaklar.push(kay)
    }
  }
  return { acik, bayi_acik: g.bayi_acik === true, kaynaklar, arsiv_limitleri: arsivLimitleriAyikla(g.arsiv_limitleri) }
}

async function tasimaKaynaklariGetir(): Promise<TasimaKaynaklari> {
  const { data } = await api.get<unknown>(`${TASIMA_TABAN}/kaynaklar`)
  return tasimaKaynaklariAyikla(data)
}

export type TasimaKaynakDurumu = {
  /** `null` = henüz bilinmiyor (ilk yükleme sürüyor ya da hiç başarılı olmadı) → menü kapısı GİZLER. */
  veri: TasimaKaynaklari | null
  /** Son isteğin hatası (başarılıda ''). Hata olunca bilinen son `veri` korunur. */
  hata: string
  yenile: () => void
}

/**
 * Taşıma kaynaklarını izler (açılışta, 60 sn'de bir ve pencere odağında).
 * `etkin=false` (ör. müşteri oturumu — uç ona kapalı) → istek atılmaz, `veri` null.
 * Üst üste binen isteklerde yalnız en son başlatılanın yanıtı uygulanır.
 */
export function useTasimaKaynaklari(etkin = true): TasimaKaynakDurumu {
  const [veri, setVeri] = useState<TasimaKaynaklari | null>(null)
  const [hata, setHata] = useState('')
  const sira = useRef(0)

  const yenile = useCallback(() => {
    if (!etkin) return
    const benim = ++sira.current
    tasimaKaynaklariGetir()
      .then((v) => {
        if (benim !== sira.current) return
        setVeri(v)
        setHata('')
      })
      .catch((e: unknown) => {
        if (benim !== sira.current) return
        setHata(apiHata(e))
      })
  }, [etkin])

  useEffect(() => {
    if (!etkin) return
    yenile()
    const t = setInterval(yenile, 60_000)
    window.addEventListener('focus', yenile)
    return () => {
      clearInterval(t)
      window.removeEventListener('focus', yenile)
    }
  }, [etkin, yenile])

  return { veri: etkin ? veri : null, hata: etkin ? hata : '', yenile }
}
