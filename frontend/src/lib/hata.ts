// Sessiz hata yutmanin yerine gecen ortak hata yuzeyi.
//
// 🔴 NEDEN: kod tabaninda 36 adet `.catch(() => {})` vardi. Istek dustugunde
// kullanici HICBIR SEY gormuyordu; ekranda bos bir acilir menu ya da varsayilan
// bir deger kaliyordu. Yani BASARISIZLIK, "yukleme bitti, sonuc bu" gibi
// GUVEN olarak render ediliyordu (or. PHP surumleri alinamayinca menu bos
// kaliyor ve "hic PHP surumu yok" gibi okunuyor).
//
// Burada iki sey garanti edilir:
//   1. Hata HER ZAMAN console'a tam nesnesiyle yazilir (teshis).
//   2. Kullaniciya gorunur, kapatilabilir bir uyari cikar (durust durum).
//
// Kullanim:  .catch(hataYakala('PHP surumleri alinamadi'))

import { apiHata } from './api'

export type HataKaydi = { id: number; baglam: string; mesaj: string }

type Dinleyici = (kayitlar: HataKaydi[]) => void

let kayitlar: HataKaydi[] = []
let sonrakiID = 1
const dinleyiciler = new Set<Dinleyici>()

// Ayni baglam icin en son ne zaman uyari gosterildi (ms). Bazi cagrilar 4-5
// saniyelik yoklama dongulerinde; kalici bir arizada ekrani uyari yagmuruna
// tutmadan, ama sorunu da gizlemeden haber vermek gerekir.
const sonGosterim = new Map<string, number>()
const TEKRAR_ARALIGI_MS = 30_000
const OMUR_MS = 9_000
const AZAMI_KAYIT = 4

function yayinla() {
  for (const f of dinleyiciler) f(kayitlar)
}

export function hataAbone(f: Dinleyici): () => void {
  dinleyiciler.add(f)
  f(kayitlar)
  return () => {
    dinleyiciler.delete(f)
  }
}

export function hataKapat(id: number) {
  kayitlar = kayitlar.filter((k) => k.id !== id)
  yayinla()
}

export function hataBildir(baglam: string, err: unknown) {
  // Teshis izi her kosulda birakilir — uyari gosterilmese bile.
  console.error(`[${baglam}]`, err)

  // 401 zaten oturum akisinin isi (api.ts interceptor'i cikis yaptirir);
  // ustune bir de "yetkisiz" uyarisi basmak yaniltici olur.
  const durum = (err as { response?: { status?: number } })?.response?.status
  if (durum === 401) return

  const simdi = Date.now()
  const son = sonGosterim.get(baglam) ?? 0
  if (simdi - son < TEKRAR_ARALIGI_MS) return
  sonGosterim.set(baglam, simdi)

  const kayit: HataKaydi = { id: sonrakiID++, baglam, mesaj: apiHata(err, 'bilinmeyen hata') }
  kayitlar = [...kayitlar, kayit].slice(-AZAMI_KAYIT)
  yayinla()
  setTimeout(() => hataKapat(kayit.id), OMUR_MS)
}

/** `.catch(hataYakala('...'))` — sessiz `.catch(() => {})` yerine. */
export function hataYakala(baglam: string) {
  return (err: unknown) => hataBildir(baglam, err)
}
