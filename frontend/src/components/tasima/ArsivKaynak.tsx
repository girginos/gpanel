import { useCallback, useEffect, useRef, useState } from 'react'
import axios from 'axios'
import { api, apiHata } from '@/lib/api'
import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { Button } from '@/components/ui'
import { I as IK } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { useToast } from '@/components/Toast'
import { TASIMA_TABAN, type ArsivLimitleri } from '@/lib/tasima'
import {
  ayniDosya, baytBicim, beklemeMs, denemeHakkiVar, hataKarari, hizHesapla, kalanSn, listeKes,
  parcaAraligi, uzantiBilinen, yuklemeAyikla, yuklemeDogrula, yuklemeListeAyikla, yuzdeHesapla,
  type ArsivKesifBilgi, type ArsivYukleme,
} from '@/lib/tasimaYukle'

/*
 * Site Taşıma — arşiv kaynağı (cPanel/DirectAdmin yedeği) girdi bileşenleri.
 *  - ArsivGirdiKart: sunucudaki dosya yolu (YALNIZ kök) ya da parçalı + devam edilebilir yükleme.
 *  - ArsivOzetKart: keşif özeti, uyarılar, taşınmayacaklar, ön kontrol.
 * 🔴 Arşiv içeriğinden gelen metinler (uyarılar, adlar) düşman girdidir: yalnız React metin
 * düğümü olarak yazılır (dangerouslySetInnerHTML YOK).
 */

const ARSIV_EN: Record<string, string> = {
  "Arşiv kaynağı": "Archive source",
  "Taşınacak yedek arşivini sunucudaki yoldan ya da yükleyerek seçin.": "Pick the backup archive to migrate from a path on the server or by uploading it.",
  "Sunucudaki dosya yolu": "File path on the server",
  "Dosya yükle": "Upload file",
  "Arşivin mutlak yolu": "Absolute path of the archive",
  "Yol sunucuda root sahipli, izin verilen bir dizinde ve symlink olmayan düzenli bir dosya olmalıdır. En fazla {0}.": "The path must be a regular, non-symlink file owned by root in an allowed directory. At most {0}.",
  "Arşivi keşfet": "Discover archive",
  "Arşiv taranıyor…": "Scanning archive…",
  "Arşiv inceleniyor… ({0} geçti). Büyük arşivlerde dakikalar sürebilir; sayfayı yenileyebilirsiniz.": "Inspecting archive… ({0} elapsed). Large archives can take minutes; you may refresh the page.",
  "Taşıma için hedefte yeterli boş alan görünmüyor.": "The target does not appear to have enough free space for the migration.",
  "Yükleme boyutu en fazla {0} olabilir. Büyük dosyalar parçalı yüklenir; kesilirse kaldığı yerden devam eder.": "Uploads can be at most {0}. Large files are uploaded in chunks and resume where they stopped.",
  "Yükleme limiti bilinmiyor; yükleme şu an kullanılamıyor.": "The upload limit is unknown; uploading is unavailable right now.",
  "Dosya seç": "Choose file",
  "Dosya boş.": "The file is empty.",
  "Dosya yükleme sınırını aşıyor ({0}).": "The file exceeds the upload limit ({0}).",
  "Yükleme sınırı bilinmiyor.": "The upload limit is unknown.",
  "Uzantı bilinen bir arşiv türü değil (.tar.gz, .tgz, .tar.zst, .tar.xz, .tar.bz2, .tar). Kesin doğrulama sunucuda yapılır.": "The extension is not a known archive type (.tar.gz, .tgz, .tar.zst, .tar.xz, .tar.bz2, .tar). The server does the definitive check.",
  "Yükleniyor": "Uploading",
  "Duraklatıldı": "Paused",
  "Yükleme başarısız": "Upload failed",
  "Yükleme tamamlandı": "Upload complete",
  "Duraklat": "Pause",
  "Sürdür": "Resume",
  "İptal": "Cancel",
  "Yüklemeyi iptal et": "Cancel upload",
  "Bağlantı koptu, yeniden deneniyor ({0}/{1})…": "Connection lost, retrying ({0}/{1})…",
  "kalan": "left",
  "Yüklemeler": "Uploads",
  "Yarım yükleme": "Incomplete upload",
  "Hazır": "Ready",
  "Keşfet": "Discover",
  "Sil": "Delete",
  "Devam et": "Continue",
  "Devam etmek için aynı dosyayı yeniden seçin (ad ve boyut aynı olmalı).": "To continue, select the same file again (name and size must match).",
  "Seçilen dosya yarım yüklemeyle eşleşmiyor (ad ve boyut aynı olmalı).": "The selected file does not match the incomplete upload (name and size must match).",
  "Yüklemeler alınamadı": "Could not load uploads",
  "Yükleme silinemedi": "Could not delete the upload",
  "Yükleme tamamlanamadı": "Could not finish the upload",
  "Sunucu beklenmeyen bir konum bildirdi.": "The server reported an unexpected position.",
  "Yol mutlak olmalı (/ ile başlar).": "The path must be absolute (start with /).",
  "Arşiv özeti": "Archive summary",
  "Arşiv boyutu": "Archive size",
  "Açılmış boyut": "Extracted size",
  "Arşiv üyesi": "Archive members",
  "Dosya": "Files",
  "Veritabanı": "Databases",
  "Posta kutusu": "Mailboxes",
  "Cron görevi": "Cron jobs",
  "DNS kaydı": "DNS records",
  "Ön kontrol": "Pre-check",
  "Taşıma için gereken boş alan ≈ {0} (arşivin yaklaşık 2,5 katı).": "Free space needed for the migration ≈ {0} (about 2.5x the archive).",
  "Uyarılar": "Warnings",
  "Taşınmayacaklar": "Will not be migrated",
  "{0} tane daha": "{0} more",
  "Cron görevleri (zamanlanmış görevler)": "Cron jobs (scheduled tasks)",
  "Arşivdeki zamanlanmış görevler siteye eklenir": "Scheduled tasks in the archive are added to the site",
  "Kutu parolaları taşınmaz, yeni parola belirlenir.": "Mailbox passwords are not migrated; new passwords are set.",
  "Arşivde SSL sertifikası bulunmaz; taşıma sonrası Let's Encrypt ile alınır.": "Archives contain no SSL certificate; one is issued via Let's Encrypt after migration.",
  "arşiv": "archive",
  "Bayiler de kullanabilsin": "Resellers can use it too",
  "Açıkken bayiler bu arşiv kaynağını dosya yükleyerek kullanabilir (varsayılan kapalı).": "When on, resellers can use this archive source by uploading files (off by default).",
  "Kaynak kapalıyken kullanılamaz.": "Unavailable while the source is off.",
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (ARSIV_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export type ArsivGirdi = { yol?: string; yukleme_id?: number }

const KART = 'rounded-lg border border-slate-200/70 bg-white p-5 sm:p-6 dark:border-dark-600/60 dark:bg-dark-700/40'
const inputCls =
  'w-full rounded-lg border border-slate-200 bg-white px-3 py-2.5 text-sm text-slate-900 ' +
  'placeholder:text-slate-400 transition focus:border-brand-400 focus:outline-none focus:ring-2 ' +
  'focus:ring-brand-500/15 disabled:cursor-not-allowed disabled:opacity-60 ' +
  'dark:border-dark-600 dark:bg-dark-800/60 dark:text-slate-100'

type Aktif = {
  id: number; ad: string; boyut: number; alinan: number
  durum: 'calisiyor' | 'durakladi' | 'hata' | 'bitti'
  hiz: number; deneme: number; mesaj: string
}

export function ArsivGirdiKart({ yonetici, limitler, kesifYuk, kesifGecen, onKesfet }: {
  yonetici: boolean
  limitler: ArsivLimitleri
  kesifYuk: boolean
  /** Süren keşfin geçen süresi (sn); null = keşif yok. */
  kesifGecen?: number | null
  onKesfet: (g: ArsivGirdi) => void
}) {
  const toast = useToast()
  const [sekme, setSekme] = useState<'yol' | 'yukle'>(yonetici ? 'yol' : 'yukle')
  const [yol, setYol] = useState('')
  const [yuklemeler, setYuklemeler] = useState<ArsivYukleme[]>([])
  const [aktif, setAktif] = useState<Aktif | null>(null)
  const [uyari, setUyari] = useState('')
  const dosyaRef = useRef<File | null>(null)
  const girdiRef = useRef<HTMLInputElement>(null)
  const devamRef = useRef<ArsivYukleme | null>(null) // "Devam et" ile seçtirilen yarım yükleme
  const abortRef = useRef<AbortController | null>(null)
  const durumRef = useRef<'' | 'duraklat' | 'iptal'>('')
  const calisanRef = useRef(false)

  useEffect(() => { if (!yonetici) setSekme('yukle') }, [yonetici])

  const listeYukle = useCallback(async () => {
    try {
      const { data } = await api.get<unknown>(`${TASIMA_TABAN}/arsiv/yuklemeler`)
      setYuklemeler(yuklemeListeAyikla(data))
    } catch (e) { toast.hata(cevir("Yüklemeler alınamadı"), apiHata(e)) }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  useEffect(() => { void listeYukle() }, [listeYukle])
  useEffect(() => () => { durumRef.current = 'iptal'; abortRef.current?.abort() }, [])

  const tavan = limitler.yukleme_tavan

  function aktifGuncelle(k: Partial<Aktif>) { setAktif(a => (a ? { ...a, ...k } : a)) }

  // Parçaları sırayla gönderir; ağ hatasında sunucudaki `alinan`ı okuyup kaldığı yerden sürer.
  async function yukle(dosya: File, mevcut?: ArsivYukleme) {
    if (calisanRef.current) return
    const red = yuklemeDogrula(dosya.size, tavan)
    if (red) {
      setUyari(red === 'bos' ? cevir("Dosya boş.") : red === 'tavan' ? cevirT(cevir("Dosya yükleme sınırını aşıyor ({0})."), baytBicim(tavan)) : cevir("Yükleme sınırı bilinmiyor."))
      return
    }
    setUyari(uzantiBilinen(dosya.name) ? '' : cevir("Uzantı bilinen bir arşiv türü değil (.tar.gz, .tgz, .tar.zst, .tar.xz, .tar.bz2, .tar). Kesin doğrulama sunucuda yapılır."))
    calisanRef.current = true
    durumRef.current = ''
    dosyaRef.current = dosya
    let id = mevcut?.id ?? 0
    let alinan = 0
    let parca = limitler.parca
    try {
      if (!id) {
        const { data } = await api.post<{ id: number; boyut: number; alinan: number; parca: number }>(`${TASIMA_TABAN}/arsiv/yukle`, { ad: dosya.name, boyut: dosya.size })
        id = Number(data.id) || 0
        alinan = Number(data.alinan) || 0
        if (Number(data.parca) > 0) parca = Number(data.parca)
        if (!id) throw new Error('id')
      } else {
        const { data } = await api.get<unknown>(`${TASIMA_TABAN}/arsiv/yukle/${id}`)
        alinan = yuklemeAyikla(data)?.alinan ?? 0
      }
      if (!(parca > 0)) { setAktif({ id, ad: dosya.name, boyut: dosya.size, alinan, durum: 'hata', hiz: 0, deneme: 0, mesaj: cevir("Yükleme sınırı bilinmiyor.") }); return }
      setAktif({ id, ad: dosya.name, boyut: dosya.size, alinan, durum: 'calisiyor', hiz: 0, deneme: 0, mesaj: '' })
      const t0 = Date.now()
      const a0 = alinan
      let deneme = 0
      while (alinan < dosya.size) {
        if (durumRef.current) break
        const ar = parcaAraligi(alinan, dosya.size, parca)
        if (!ar) break
        const ac = new AbortController()
        abortRef.current = ac
        try {
          const { data } = await api.put<{ alinan: number }>(`${TASIMA_TABAN}/arsiv/yukle/${id}`, dosya.slice(ar.bas, ar.son), {
            params: { konum: ar.bas },
            headers: { 'Content-Type': 'application/octet-stream' },
            signal: ac.signal,
            timeout: 3 * 60_000,
            transformRequest: [(b: unknown) => b],
          })
          const yeni = Number(data?.alinan)
          if (!Number.isFinite(yeni) || yeni <= alinan || yeni > dosya.size) throw Object.assign(new Error('konum'), { yerel: true })
          alinan = yeni
          deneme = 0
          aktifGuncelle({ alinan, deneme: 0, hiz: hizHesapla(alinan - a0, Date.now() - t0) })
        } catch (e) {
          if (durumRef.current || axios.isCancel(e)) break
          const x = e as { response?: { status?: number; data?: { alinan?: unknown } }; yerel?: boolean }
          const karar = x.yerel ? ({ eylem: 'vazgec' } as const) : hataKarari(x.response?.status, x.response?.data?.alinan)
          if (karar.eylem === 'vazgec') {
            aktifGuncelle({ durum: 'hata', mesaj: x.yerel ? cevir("Sunucu beklenmeyen bir konum bildirdi.") : apiHata(e, cevir("Yükleme başarısız")) })
            return
          }
          deneme++
          if (!denemeHakkiVar(deneme)) { aktifGuncelle({ durum: 'hata', mesaj: apiHata(e, cevir("Yükleme başarısız")) }); return }
          if (karar.eylem === 'esitle') {
            if (karar.alinan <= dosya.size) alinan = karar.alinan
            aktifGuncelle({ alinan })
            continue
          }
          aktifGuncelle({ deneme, mesaj: cevirT(cevir("Bağlantı koptu, yeniden deneniyor ({0}/{1})…"), deneme, 5) })
          await new Promise(r => setTimeout(r, beklemeMs(deneme)))
          if (durumRef.current) break
          try {
            const { data } = await api.get<unknown>(`${TASIMA_TABAN}/arsiv/yukle/${id}`)
            const y = yuklemeAyikla(data)
            if (y && y.alinan <= dosya.size) alinan = y.alinan
          } catch { /* sonraki turda yeniden denenir */ }
        }
      }
      const son = durumRef.current as '' | 'duraklat' | 'iptal'
      if (son === 'iptal') return
      if (son === 'duraklat') { aktifGuncelle({ durum: 'durakladi', mesaj: '' }); void listeYukle(); return }
      if (alinan >= dosya.size) {
        try {
          await api.post(`${TASIMA_TABAN}/arsiv/yukle/${id}/bitir`, {})
          aktifGuncelle({ durum: 'bitti', alinan: dosya.size, mesaj: '' })
          toast.basari(cevir("Yükleme tamamlandı"))
        } catch (e) {
          aktifGuncelle({ durum: 'hata', mesaj: apiHata(e, cevir("Yükleme tamamlanamadı")) })
        }
        void listeYukle()
      }
    } catch (e) {
      const m = apiHata(e, cevir("Yükleme başarısız"))
      setAktif(a => (a ? { ...a, durum: 'hata', mesaj: m } : { id, ad: dosya.name, boyut: dosya.size, alinan: 0, durum: 'hata', hiz: 0, deneme: 0, mesaj: m }))
    } finally {
      calisanRef.current = false
      abortRef.current = null
    }
  }

  function duraklat() { durumRef.current = 'duraklat'; abortRef.current?.abort() }
  function surdur() {
    const d = dosyaRef.current
    if (d && aktif) void yukle(d, { id: aktif.id, ad: aktif.ad, boyut: aktif.boyut, alinan: aktif.alinan, durum: 'yukleniyor', olusturma: '' })
  }
  async function iptal() {
    const id = aktif?.id
    durumRef.current = 'iptal'
    abortRef.current?.abort()
    setAktif(null)
    dosyaRef.current = null
    if (id) { try { await api.delete(`${TASIMA_TABAN}/arsiv/yukle/${id}`) } catch { /* listeden silinir */ } }
    void listeYukle()
  }
  async function sil(id: number) {
    try { await api.delete(`${TASIMA_TABAN}/arsiv/yukle/${id}`); void listeYukle() } catch (e) { toast.hata(cevir("Yükleme silinemedi"), apiHata(e)) }
  }

  function dosyaSecildi(f: File | undefined) {
    const devam = devamRef.current
    devamRef.current = null
    if (girdiRef.current) girdiRef.current.value = ''
    if (!f) return
    if (devam) {
      if (!ayniDosya(devam, f)) { setUyari(cevir("Seçilen dosya yarım yüklemeyle eşleşmiyor (ad ve boyut aynı olmalı).")); return }
      void yukle(f, devam)
      return
    }
    void yukle(f)
  }
  function dosyaPenceresi(devam?: ArsivYukleme) { devamRef.current = devam ?? null; girdiRef.current?.click() }

  const mesgul = !!aktif && (aktif.durum === 'calisiyor')
  const yolGecerli = yol.trim().startsWith('/') && yol.trim().length > 1
  const sekmeCls = (s: string) => `rounded-lg px-3 py-1.5 text-sm font-medium transition ${sekme === s ? 'bg-dark-800 text-white dark:bg-white dark:text-slate-900' : 'text-slate-600 hover:bg-slate-50 dark:text-slate-300 dark:hover:bg-dark-700'}`
  const yuzde = aktif ? yuzdeHesapla(aktif.alinan, aktif.boyut) : 0
  const kalan = aktif ? kalanSn(aktif.boyut - aktif.alinan, aktif.hiz) : 0
  const gorunenListe = yuklemeler.filter(y => !(aktif && aktif.id === y.id))

  return (
    <section className={KART}>
      <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Arşiv kaynağı")}</h2>
      <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{cevir("Taşınacak yedek arşivini sunucudaki yoldan ya da yükleyerek seçin.")}</p>

      {kesifYuk && kesifGecen != null && (
        <UyariKutusu ton="bilgi" kucuk className="mt-4">
          {cevirT(cevir("Arşiv inceleniyor… ({0} geçti). Büyük arşivlerde dakikalar sürebilir; sayfayı yenileyebilirsiniz."), sureKisa(Math.max(1, kesifGecen)))}
        </UyariKutusu>
      )}

      {yonetici && (
        <div className="mt-4 flex gap-1.5" role="tablist">
          <button type="button" role="tab" aria-selected={sekme === 'yol'} className={sekmeCls('yol')} onClick={() => setSekme('yol')}>{cevir("Sunucudaki dosya yolu")}</button>
          <button type="button" role="tab" aria-selected={sekme === 'yukle'} className={sekmeCls('yukle')} onClick={() => setSekme('yukle')}>{cevir("Dosya yükle")}</button>
        </div>
      )}

      {yonetici && sekme === 'yol' && (
        <div className="mt-4 max-w-3xl">
          <label className="block">
            <span className="mb-1.5 block text-xs font-medium text-slate-500 dark:text-slate-400">{cevir("Arşivin mutlak yolu")}</span>
            <input value={yol} onChange={e => setYol(e.target.value)} placeholder="/home/yedek/cpmove-kullanici.tar.gz" className={`${inputCls} font-mono`} spellCheck={false} autoComplete="off" />
          </label>
          <p className="mt-1.5 text-[11px] leading-relaxed text-slate-500 dark:text-slate-400">
            {cevirT(cevir("Yol sunucuda root sahipli, izin verilen bir dizinde ve symlink olmayan düzenli bir dosya olmalıdır. En fazla {0}."), limitler.yol_tavan > 0 ? baytBicim(limitler.yol_tavan) : '—')}
          </p>
          {yol.trim() !== '' && !yolGecerli && <p className="mt-1 text-[11px] text-amber-600 dark:text-amber-400">{cevir("Yol mutlak olmalı (/ ile başlar).")}</p>}
          <div className="mt-4">
            <Button type="button" color="primary" ikon={IK.dosya} ikonSonda={IK.ileri} yukleniyor={kesifYuk} disabled={!yolGecerli} onClick={() => onKesfet({ yol: yol.trim() })}>
              {kesifYuk ? cevir("Arşiv taranıyor…") : cevir("Arşivi keşfet")}
            </Button>
          </div>
        </div>
      )}

      {(!yonetici || sekme === 'yukle') && (
        <div className="mt-4 max-w-3xl">
          <p className="text-[11px] leading-relaxed text-slate-500 dark:text-slate-400">
            {tavan > 0
              ? cevirT(cevir("Yükleme boyutu en fazla {0} olabilir. Büyük dosyalar parçalı yüklenir; kesilirse kaldığı yerden devam eder."), baytBicim(tavan))
              : cevir("Yükleme limiti bilinmiyor; yükleme şu an kullanılamıyor.")}
          </p>
          <input ref={girdiRef} type="file" className="hidden" onChange={e => dosyaSecildi(e.target.files?.[0])} />
          <div className="mt-3">
            <Button type="button" color="primary" ikon={IK.yukle} disabled={tavan <= 0 || mesgul} onClick={() => dosyaPenceresi()}>{cevir("Dosya seç")}</Button>
          </div>
          {uyari && <UyariKutusu ton="uyari" kucuk className="mt-3"><span className="break-words">{uyari}</span></UyariKutusu>}

          {aktif && (
            <div className="mt-4 rounded-lg border border-slate-200/70 p-3.5 dark:border-dark-600/60">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="min-w-0">
                  <div className="truncate text-sm font-medium text-slate-900 dark:text-slate-100">{aktif.ad}</div>
                  <div className="text-[11px] text-slate-500 dark:text-slate-400">
                    {baytBicim(aktif.alinan)} / {baytBicim(aktif.boyut)} · {aktif.durum === 'calisiyor' ? cevir("Yükleniyor") : aktif.durum === 'durakladi' ? cevir("Duraklatıldı") : aktif.durum === 'bitti' ? cevir("Yükleme tamamlandı") : cevir("Yükleme başarısız")}
                    {aktif.durum === 'calisiyor' && aktif.hiz > 0 && <> · {baytBicim(aktif.hiz)}/s{kalan > 0 && <> · {sureKisa(kalan)} {cevir("kalan")}</>}</>}
                  </div>
                </div>
                <div className="flex items-center gap-1.5">
                  {aktif.durum === 'calisiyor' && <Button type="button" variant="outlined" boyut="sm" ikon={IK.durakla} onClick={duraklat}>{cevir("Duraklat")}</Button>}
                  {(aktif.durum === 'durakladi' || aktif.durum === 'hata') && aktif.alinan < aktif.boyut && <Button type="button" variant="outlined" boyut="sm" ikon={IK.oynat} onClick={surdur}>{cevir("Sürdür")}</Button>}
                  {aktif.durum === 'bitti' && <Button type="button" color="primary" boyut="sm" ikon={IK.sunucu} ikonSonda={IK.ileri} yukleniyor={kesifYuk} onClick={() => onKesfet({ yukleme_id: aktif.id })}>{cevir("Keşfet")}</Button>}
                  {aktif.durum !== 'bitti' && <Button type="button" color="error" variant="soft" boyut="sm" ikon={IK.kapat} onClick={() => void iptal()} title={cevir("Yüklemeyi iptal et")}>{cevir("İptal")}</Button>}
                </div>
              </div>
              <div className="mt-2.5 h-2 overflow-hidden rounded-full bg-slate-100 dark:bg-dark-700" role="progressbar" aria-valuenow={yuzde} aria-valuemin={0} aria-valuemax={100}>
                <div className={`h-full rounded-full transition-all duration-300 ${aktif.durum === 'hata' ? 'bg-red-500' : 'bg-brand-500'}`} style={{ width: `${yuzde}%` }} />
              </div>
              <div className="mt-1 flex justify-between text-[11px] tabular-nums text-slate-500 dark:text-slate-400"><span>{aktif.mesaj && <span className={aktif.durum === 'hata' ? 'text-red-600 dark:text-red-400' : ''}>{aktif.mesaj}</span>}</span><span>{yuzde}%</span></div>
            </div>
          )}

          {gorunenListe.length > 0 && (
            <div className="mt-4">
              <div className="mb-1.5 text-xs font-medium text-slate-500 dark:text-slate-400">{cevir("Yüklemeler")}</div>
              <div className="divide-y divide-slate-100 rounded-lg border border-slate-200/70 dark:divide-dark-600 dark:border-dark-600/60">
                {gorunenListe.map(y => (
                  <div key={y.id} className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 text-sm">
                    <div className="min-w-0">
                      <div className="truncate font-medium text-slate-900 dark:text-slate-100">{y.ad}</div>
                      <div className="text-[11px] text-slate-500 dark:text-slate-400">
                        {y.durum === 'hazir' ? `${baytBicim(y.boyut)} · ${cevir("Hazır")}` : `${baytBicim(y.alinan)} / ${baytBicim(y.boyut)} · ${cevir("Yarım yükleme")} (${yuzdeHesapla(y.alinan, y.boyut)}%)`}
                      </div>
                      {y.durum !== 'hazir' && <div className="text-[11px] text-slate-400">{cevir("Devam etmek için aynı dosyayı yeniden seçin (ad ve boyut aynı olmalı).")}</div>}
                    </div>
                    <div className="flex items-center gap-1.5">
                      {y.durum === 'hazir'
                        ? <Button type="button" color="primary" boyut="sm" ikon={IK.sunucu} ikonSonda={IK.ileri} yukleniyor={kesifYuk} disabled={mesgul} onClick={() => onKesfet({ yukleme_id: y.id })}>{cevir("Keşfet")}</Button>
                        : <Button type="button" color="primary" boyut="sm" ikon={IK.yukle} disabled={mesgul || tavan <= 0} onClick={() => dosyaPenceresi(y)}>{cevir("Devam et")}</Button>}
                      <Button type="button" color="error" variant="flat" isIcon ikon={IK.cop} className="size-8" disabled={mesgul} onClick={() => void sil(y.id)} title={cevir("Sil")} aria-label={cevir("Sil")} />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

function sureKisa(sn: number): string {
  if (!isFinite(sn) || sn <= 0) return '—'
  const s = Math.round(sn)
  if (s >= 3600) return `${Math.floor(s / 3600)} h ${Math.floor((s % 3600) / 60)} m`
  if (s >= 60) return `${Math.floor(s / 60)} m ${s % 60} s`
  return `${s} s`
}

function KatlanirListe({ baslik, ogeler }: { baslik: string; ogeler: string[] }) {
  const { gorunen, kalan } = listeKes(ogeler, 10)
  return (
    <details className="rounded-lg border border-slate-200/70 px-3 py-2 dark:border-dark-600/60">
      <summary className="cursor-pointer text-sm font-medium text-slate-800 dark:text-slate-100">{baslik} ({ogeler.length})</summary>
      <ul className="mt-2 list-disc space-y-1 ps-5 text-xs text-slate-600 dark:text-slate-300">
        {gorunen.map((m, i) => <li key={i} className="break-words">{m.length > 300 ? `${m.slice(0, 300)}…` : m}</li>)}
      </ul>
      {kalan > 0 && <p className="mt-1.5 text-[11px] text-slate-400">+ {cevirT(cevir("{0} tane daha"), kalan)}</p>}
    </details>
  )
}

export function ArsivOzetKart({ bilgi }: { bilgi: ArsivKesifBilgi }) {
  const o = bilgi.ozet
  const sayilar: [string, string][] = [
    [cevir("Arşiv boyutu"), baytBicim(o.arsiv_bayt)],
    [cevir("Açılmış boyut"), baytBicim(o.toplam_bayt)],
    [cevir("Arşiv üyesi"), String(o.uye_sayisi)],
    [cevir("Dosya"), String(o.dosya_sayisi)],
    [cevir("Veritabanı"), String(o.db_sayisi)],
    [cevir("Posta kutusu"), String(o.posta_sayisi)],
    [cevir("Cron görevi"), String(o.cron_sayisi)],
    [cevir("DNS kaydı"), String(o.dns_sayisi)],
  ]
  return (
    <section className={KART}>
      <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Arşiv özeti")}</h2>
      <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {sayilar.map(([et, deg]) => (
          <div key={et} className="rounded-lg bg-slate-50 px-3.5 py-3 dark:bg-dark-800/40">
            <div className="text-[11px] font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500">{et}</div>
            <div className="mt-0.5 text-lg font-semibold tabular-nums text-slate-900 dark:text-slate-100">{deg}</div>
          </div>
        ))}
      </div>
      <UyariKutusu ton="bilgi" kucuk className="mt-4" baslik={cevir("Ön kontrol")}>
        {cevirT(cevir("Taşıma için gereken boş alan ≈ {0} (arşivin yaklaşık 2,5 katı)."), baytBicim(bilgi.onkontrol.gerekli_bos_bayt))}
      </UyariKutusu>
      {bilgi.onkontrol.yeterli === false && (
        <UyariKutusu ton="uyari" kucuk className="mt-3">{cevir("Taşıma için hedefte yeterli boş alan görünmüyor.")}</UyariKutusu>
      )}
      {(bilgi.uyarilar.length > 0 || bilgi.tasinmayacak.length > 0) && (
        <div className="mt-4 space-y-2">
          {bilgi.uyarilar.length > 0 && <KatlanirListe baslik={cevir("Uyarılar")} ogeler={bilgi.uyarilar} />}
          {bilgi.tasinmayacak.length > 0 && <KatlanirListe baslik={cevir("Taşınmayacaklar")} ogeler={bilgi.tasinmayacak} />}
        </div>
      )}
    </section>
  )
}
