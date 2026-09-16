import { useEffect, useRef, useState } from 'react'
import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { cevirT } from '@/lib/cevirT'

// Sağ altta SABİT ilerleme penceresi — uzun süren, adım-adım işler için.
//
// 🔴 NEDEN AYRI BİR KATMAN: adım listeleri önce sayfanın/kartın İÇİNDE
// çiziliyordu ve iş sürdükçe 0'dan ~29 satıra büyüyordu. Eklentiler ekranında
// kartlar CSS Grid çocuğu olduğu için satırdaki DİĞER kartlar da uzuyor,
// üstelik 2,5 saniyelik yoklamayla bu sürekli tekrarlanıyordu — sayfa boyunca
// düzen kayması. Panel kendi yüksekliği sınırlı bir katmanda yaşar; altındaki
// içerik artık zıplamaz.
//
// Aynı desen İKİ yerde kullanılır (eklenti kurulumu ve SSL kurulumu); ikisinin
// de sunucu tarafı aynı biçimi döndürür (adim listesi + durum + basladi/bitti
// + toplam). Kopyalamak yerine tek bileşen: davranış (odak, canlı bölge,
// yükseklik ölçümü) tek yerde düzeltilir.

export type IlerlemeAdim = {
  ad: string
  etiket: string
  durum: string // bekliyor|calisiyor|tamam|uyari|hata
  mesaj?: string
  sure?: string
}

export type IlerlemeIsi = {
  anahtar: string // benzersiz kimlik (eklenti adı, domain id…)
  baslik: string  // kullanıcıya görünen ad
  durum: string   // calisiyor|tamam|hata
  hata?: string
  adimlar: IlerlemeAdim[]
  toplam?: number // çalıştırılması PLANLANAN adım sayısı (yoksa belirsiz kip)
  basladi?: string
  bitti?: string
}

const EN: Record<string, string> = {
  "Kurulum ilerlemesi": "Installation progress",
  "{0}. adım": "Step {0}",
  "{0} / {1} adım": "{0} / {1} steps",
  "Küçült": "Minimise",
  "Büyüt": "Expand",
  "Kapat": "Close",
  "Kurulum tamamlandı": "Installation complete",
  "Kurulum sürüyor": "Installation running",
  "Kurulum başarısız": "Installation failed",
  "Ayrıntılı günlük": "Detailed log",
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (EN[tr] || ORTAK_EN[tr] || tr) : tr)

// İlerleme ölçüsü. `toplam` yoksa YÜZDE HESAPLANMAZ.
//
// 🔴 Adımlar append-only gelir: `adimlar.length` "şimdiye kadar başlamış adım"
// demektir, "toplam" değil. bitmis/adimlar.length ile hesaplasaydık çubuk iş
// boyunca hep ~%95-100 gösterirdi — yani yalan söylerdi. Toplam bilinmiyorsa
// belirsiz kipe düşülür ve yalnız sayaç gösterilir.
export function ilerleme(is: IlerlemeIsi) {
  const bitmis = is.adimlar.filter(a => a.durum !== 'calisiyor').length
  const toplam = is.toplam || 0
  // bitmis > toplam: koşucuya adım eklenmiş ama sayı güncellenmemiş demektir.
  // Yanlış yüzde göstermektense belirsize düş.
  const belirsiz = toplam <= 0 || bitmis > toplam
  const bitti = is.durum === 'tamam'
  const yuzde = belirsiz ? 0 : bitti ? 100 : Math.min(99, Math.round((bitmis / toplam) * 100))
  const suren = is.adimlar.find(a => a.durum === 'calisiyor')
  return { bitmis, toplam, belirsiz, yuzde, suren }
}

// 🔴 BİTMİŞ işte süre `bitti - basladi`dır, `now - basladi` DEĞİL.
// Yoklama iş bitince duruyor, ama bileşen başka sebeplerle yeniden
// çizildiğinde sayaç işlemeye devam ediyordu: üç gün açık kalan bir sekmede
// "Kurulum tamamlandı · 4320d 0s" görünürdü.
export function gecenSure(basladi?: string, bitti?: string): string {
  if (!basladi) return ''
  const t0 = Date.parse(basladi)
  if (!Number.isFinite(t0)) return ''
  const t1 = bitti ? Date.parse(bitti) : NaN
  const son = Number.isFinite(t1) ? t1 : Date.now()
  const sn = Math.max(0, Math.round((son - t0) / 1000))
  if (sn < 60) return `${sn}s`
  const dk = Math.floor(sn / 60)
  if (dk < 60) return `${dk}d ${sn % 60}s`
  return `${Math.floor(dk / 60)}sa ${dk % 60}d`
}

// 🔴 Kayan parıltı (shimmer) stili — BİR KEZ enjekte edilir.
// Tailwind config'e custom keyframe eklemek tüm paneli etkileyen geniş bir
// değişiklik olurdu; bu efekt yalnız ilerleme çubuğuna ait, o yüzden kendi
// stilini taşır. SSR/DOM yoksa sessiz geçer.
function shimmerStiliniEkle() {
  if (typeof document === 'undefined' || document.getElementById('gosp-ilerleme-stil')) return
  const st = document.createElement('style')
  st.id = 'gosp-ilerleme-stil'
  st.textContent =
    '@keyframes gosp-shimmer{0%{transform:translateX(-100%)}100%{transform:translateX(280%)}}' +
    '.gosp-shimmer{position:absolute;top:0;bottom:0;width:35%;' +
    'background:linear-gradient(90deg,transparent,rgba(255,255,255,.55),transparent);' +
    'animation:gosp-shimmer 1.15s ease-in-out infinite}' +
    '@media(prefers-reduced-motion:reduce){.gosp-shimmer{animation:none;display:none}}'
  document.head.appendChild(st)
}

export function IlerlemeCubugu({ is }: { is: IlerlemeIsi }) {
  useEffect(() => { shimmerStiliniEkle() }, [])
  const calisiyor = is.durum === 'calisiyor'
  const { belirsiz, yuzde } = ilerleme(is)
  const hata = is.durum === 'hata'
  const renk = hata ? 'bg-rose-500' : is.durum === 'tamam' ? 'bg-emerald-500' : 'bg-brand-600'
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-dark-600"
      role={belirsiz ? undefined : 'progressbar'}
      aria-valuenow={belirsiz ? undefined : yuzde}
      aria-valuemin={belirsiz ? undefined : 0}
      aria-valuemax={belirsiz ? undefined : 100}>
      {/* Belirsiz kip: yüzde bilinmiyor. Dolu bir çubuk göstermek yalan olurdu;
          kayan dar bir şerit "çalışıyor ama ne kadar kaldığı bilinmiyor" der. */}
      {belirsiz && !hata && is.durum !== 'tamam'
        ? <div className={`h-full w-1/3 animate-pulse rounded-full ${renk}`} />
        : <div className={`relative h-full overflow-hidden rounded-full transition-[width] duration-500 ${renk}`}
            style={{ width: `${belirsiz ? 100 : yuzde}%` }}>
            {/* Yüzde biliniyor ama iş SÜRÜYOR: dolu çubuğun üstünde kayan
                parıltı "hâlâ çalışıyor" der (donmuş bir çubuktan ayırır). */}
            {calisiyor && <span className="gosp-shimmer" aria-hidden />}
          </div>}
    </div>
  )
}

export default function IlerlemePaneli({ isler, kucuk, setKucuk, onGizle, onYukseklik, baslik }: {
  isler: IlerlemeIsi[]
  kucuk: boolean
  setKucuk: (v: boolean) => void
  onGizle?: (anahtar: string) => void
  onYukseklik?: (px: number) => void
  baslik?: string
}) {
  const [acikGunluk, setAcikGunluk] = useState<string | null>(null)
  // 🔴 SANİYE DÜZENLİ AKSIN. Sayaç yalnız üst bileşen yeniden çizince
  // güncelleniyordu; yoklama 1,5 sn'de bir geldiği için "1 sn, 2 sn, 1 sn…"
  // gibi düzensiz atlıyordu. Süren iş varken saniyede bir yeniden çizeriz;
  // gecenSure zaten basladi'dan hesapladığı için akış pürüzsüz olur. Biten
  // işte tick durur (gereksiz render yok).
  const [, tik] = useState(0)
  const surumVar = isler.some(x => x.durum === 'calisiyor')
  useEffect(() => {
    if (!surumVar) return
    const t = window.setInterval(() => tik(n => n + 1), 1000)
    return () => window.clearInterval(t)
  }, [surumVar])
  // 🔴 SAYFA DOLGUSU ÖLÇÜLÜR, TAHMİN EDİLMEZ. Sabit bir alt dolgu verilmişti;
  // panelin gövdesi ise max-h-[60vh] ve günlük açılınca daha da büyüyor. İki iş
  // listelendiğinde ya da günlük açıldığında altındaki düğmeler yine örtülüyordu
  // — yani düzeltilen şikâyet yer değiştirmiş oluyordu.
  const kutuRef = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    if (!onYukseklik) return
    const el = kutuRef.current
    if (!el) { onYukseklik(0); return }
    if (typeof ResizeObserver === 'undefined') { onYukseklik(el.offsetHeight); return }
    const ro = new ResizeObserver(() => onYukseklik(el.offsetHeight))
    ro.observe(el)
    onYukseklik(el.offsetHeight)
    return () => ro.disconnect()
  })
  useEffect(() => () => onYukseklik?.(0), [onYukseklik])
  if (isler.length === 0) return null
  const suren = isler.filter(x => x.durum === 'calisiyor').length
  const panelBaslik = baslik || cevir("Kurulum ilerlemesi")

  return (
    // z-[90]: Modal z-[100] ve Toast z-[140] ALTINDA kalmalı — bir kip pencere
    // açıkken onun üstünü örtmesin.
    <div ref={kutuRef} className="fixed bottom-4 right-4 z-[90] w-[22rem] max-w-[calc(100vw-2rem)]
                    rounded-xl border border-slate-200 bg-white shadow-lg
                    dark:border-dark-600 dark:bg-dark-800"
      /* 🔴 CANLI BÖLGE KÖKE KONMAZ. aria-live pencerenin tamamındaydı; içinde
         her yoklamada değişen saniye sayacı ve açıkken uzun bir günlük vardı —
         ekran okuyucu iş boyunca sürekli kesilip her turda tüm pencereyi
         yeniden okuyordu. Canlı olan tek şey DURUM METNİdir. */
      role="region" aria-label={panelBaslik}>
      <div className="flex items-center gap-2 border-b border-slate-200 px-3 py-2 dark:border-dark-600">
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-slate-900 dark:text-slate-100">
          {panelBaslik}
          {suren > 0 && isler.length > 1 && <span className="ml-1 text-slate-400">({suren})</span>}
        </span>
        <button type="button" onClick={() => setKucuk(!kucuk)}
          aria-label={kucuk ? cevir("Büyüt") : cevir("Küçült")}
          className="rounded p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-600
                     focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-500
                     dark:hover:bg-dark-600 dark:hover:text-slate-200">
          <svg className={`h-4 w-4 transition-transform ${kucuk ? 'rotate-180' : ''}`} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2} aria-hidden>
            <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
          </svg>
        </button>
      </div>

      {!kucuk && (
        <div className="max-h-[60vh] space-y-3 overflow-y-auto p-3">
          {isler.map((is) => {
            const { bitmis, toplam, belirsiz, suren: adim } = ilerleme(is)
            const bitti = is.durum !== 'calisiyor'
            return (
              <div key={is.anahtar} className="space-y-1.5">
                <div className="flex items-center gap-2">
                  <span className="min-w-0 flex-1 truncate text-sm text-slate-800 dark:text-slate-100">{is.baslik}</span>
                  <span className="shrink-0 text-xs tabular-nums text-slate-400">
                    {belirsiz
                      ? cevirT(cevir("{0}. adım"), String(bitmis + (adim ? 1 : 0)))
                      : cevirT(cevir("{0} / {1} adım"), String(bitmis), String(toplam))}
                  </span>
                  {/* Bitmiş iş kapatılabilir; SÜREN iş kapatılamaz — kullanıcı
                      ilerlemeyi kaybetmesin. */}
                  {bitti && onGizle && (
                    <button type="button" onClick={() => onGizle(is.anahtar)}
                      aria-label={cevir("Kapat")}
                      className="shrink-0 rounded p-0.5 text-slate-400 hover:text-slate-600
                                 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-500
                                 dark:hover:text-slate-200">
                      <svg className="h-3.5 w-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2} aria-hidden>
                        <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
                      </svg>
                    </button>
                  )}
                </div>
                <IlerlemeCubugu is={is} />
                <div className="flex items-baseline gap-2">
                  <span className="min-w-0 flex-1 truncate text-xs text-slate-500 dark:text-slate-400"
                    aria-live="polite">
                    {is.durum === 'hata' ? cevir("Kurulum başarısız")
                      : is.durum === 'tamam' ? cevir("Kurulum tamamlandı")
                        : (adim?.etiket || cevir("Kurulum sürüyor"))}
                  </span>
                  {/* Sayaç canlı bölgenin DIŞINDA: duyurulacak bilgi değil. */}
                  <span className="shrink-0 text-xs tabular-nums text-slate-400" aria-hidden>{gecenSure(is.basladi, is.bitti)}</span>
                </div>

                <button type="button"
                  onClick={() => setAcikGunluk(acikGunluk === is.anahtar ? null : is.anahtar)}
                  className="text-xs text-brand-600 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-500 dark:text-brand-400">
                  {cevir("Ayrıntılı günlük")}
                </button>
                {acikGunluk === is.anahtar && (
                  <ul className="max-h-52 space-y-1 overflow-y-auto rounded bg-slate-50 p-2 dark:bg-dark-900">
                    {is.adimlar.map((a, i) => (
                      <li key={i} className="text-xs">
                        <span className={`mr-1.5 font-mono ${
                          a.durum === 'tamam' ? 'text-emerald-600 dark:text-emerald-400'
                            : a.durum === 'calisiyor' ? 'text-sky-600 dark:text-sky-400'
                              : a.durum === 'uyari' ? 'text-amber-600 dark:text-amber-400'
                                : 'text-rose-600 dark:text-rose-400'}`}>
                          {a.durum === 'tamam' ? '✓' : a.durum === 'calisiyor' ? '…' : a.durum === 'uyari' ? '!' : '×'}
                        </span>
                        <span className="text-slate-700 dark:text-slate-200">{a.etiket}</span>
                        {a.sure && <span className="text-slate-400"> ({a.sure})</span>}
                        {a.mesaj && (a.durum === 'hata' || a.durum === 'uyari') && (
                          <div className="ml-5 mt-0.5 whitespace-pre-wrap break-words text-slate-500 dark:text-slate-400">{a.mesaj}</div>
                        )}
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
