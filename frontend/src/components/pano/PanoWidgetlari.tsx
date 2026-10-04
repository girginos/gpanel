// Pano widget ızgarası (D3b) — bayi panosu (öncelik) ve kök panosu. Seçenek 1
// kurumsal dili (KART, ince kenarlık, sade tipografi). Her kart kendi ucunu
// (GET /pano/w/{id}) çağırır ve yükleniyor / boş / hata hâllerini ayrı çizer.
// Kapsam sunucuda jetondan: bayi yalnız kendi hesaplarını görür.
//
// DENGE KURALLARI (pano-denge): ızgara satır başına EŞİT yükseklik (stretch);
// her kart flex-col — gövde esner, altbilgi (son ölçüm/tarama + "Tümünü gör")
// kartın en altına yapışır; listeler SABİT en çok N satır gösterir (taşan kısım
// "Tümünü gör" ile açılır); uzun alan adları kırpılır (title ile tam ad); KPI
// kutuları her kartta aynı yükseklik/tipografi; boş durum kalan alanda ortalanır.
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { goreliZaman } from '@/lib/bildirim'
import { Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { KART, ODAK, cx, mbYaz } from '@/components/bayi/ortak'
import { Cubuk } from '@/components/bayi/Grafik'
import { useWidget, type WidgetDurum } from './useWidget'
import { EgilimIcerik } from './Egilim'
import type { EgilimVeri } from './egilimHesap'
import { pt } from './ceviri'

type KotaVeri = { esik: number; toplam: number; disk_olcum: string; hesaplar: { id: number; alan_adi: string; metrik: 'disk' | 'trafik'; yuzde: number; kullanim_mb: number; limit_mb: number }[] }
type SSLVeri = { doldu: number; yakin: number; sslsiz: number; son_tarama: string; hesaplar: { id: number; alan_adi: string; kalan_gun: number }[] }
type AskiVeri = { toplam: number; hesaplar: { id: number; alan_adi: string; kaynak: string; zaman: string }[] }
type WebsecVeri = { sayim: Record<'critical' | 'high' | 'medium' | 'low', number>; son_tarama: string; son: { domain_id: number; alan_adi: string; paket: string; surum: string; cve: string; onem: string; duzeltme: string; ilk: string }[] }

// Kart başına gösterilen en çok satır (sabit; fazlası "Tümünü gör" ile).
const N_KOTA = 3
const N_SSL = 4
const N_ASKI = 4
const N_WEBSEC = 4

const LINK = cx('block min-w-0 truncate rounded font-medium text-gray-900 hover:underline dark:text-dark-50', ODAK)
const SATIR_AYRAC = 'divide-y divide-gray-100 dark:divide-dark-600'

function Iskelet({ sinif }: { sinif: string }) {
  return <div className={cx('animate-pulse rounded-md bg-gray-200 dark:bg-dark-500', sinif)} aria-hidden="true" />
}

// Bos — kalan alanda ortalanan boş durum (kart eşit yükseklikte kalır).
function Bos({ children }: { children: ReactNode }) {
  return <div className="flex min-h-24 flex-1 items-center justify-center px-2 py-4 text-center text-sm text-gray-500 dark:text-dark-300">{children}</div>
}

function TumunuGor({ yol, baslik }: { yol: string; baslik: string }) {
  return (
    <Link to={yol} title={baslik} className={cx('inline-flex shrink-0 items-center gap-0.5 rounded text-xs font-medium text-primary-600 hover:underline dark:text-primary-400', ODAK)}>
      {pt('Tümünü gör')}<Ikon d={I.ileri} className="size-3.5" />
    </Link>
  )
}

// WidgetKabugu — başlık + ikon + yenile; yükleniyor/hata hâlleri burada.
// alt: sol altbilgi metni (ölçüm/tarama zamanı); yol: sağ altbilgi "Tümünü gör".
function WidgetKabugu<T>({ baslik, ikon, d, alt, yol, children }: { baslik: string; ikon: string; d: WidgetDurum<T>; alt?: ReactNode; yol?: string; children: (v: T) => ReactNode }) {
  const hazir = d.veri !== null && !d.hata
  return (
    <section className={cx(KART, 'flex h-full min-h-[17rem] min-w-0 flex-col p-5')} aria-label={baslik} aria-busy={d.yuk}>
      <div className="flex h-7 items-center justify-between gap-2">
        <h3 className="flex min-w-0 items-center gap-2 text-sm font-semibold text-gray-900 dark:text-dark-50">
          <Ikon d={ikon} className="size-4 shrink-0 text-gray-400 dark:text-dark-300" />
          <span className="truncate" title={baslik}>{baslik}</span>
        </h3>
        <Button isIcon variant="flat" boyut="sm" onClick={d.yenile} yukleniyor={d.yuk && d.veri !== null}
          className="size-7 shrink-0 rounded-full" aria-label={`${pt('Yenile')}: ${baslik}`} title={pt('Yenile')} ikon={I.yenile} />
      </div>
      <div className="@container mt-3 flex min-h-0 flex-1 flex-col">
        {d.hata ? (
          <div className="flex flex-1 flex-col items-center justify-center py-4 text-center">
            <p className="text-sm text-error">{pt('Veri alınamadı')}</p>
            <Button boyut="sm" variant="outlined" ikon={I.yenile} onClick={d.yenile} className="mt-2">{pt('Yeniden dene')}</Button>
          </div>
        ) : d.veri === null ? (
          <div className="space-y-2.5">{[0, 1, 2].map((n) => <Iskelet key={n} sinif="h-7 w-full" />)}</div>
        ) : children(d.veri)}
      </div>
      {hazir && (alt || yol) && (
        <div className="mt-3 flex min-h-8 items-center justify-between gap-3 border-t border-gray-100 pt-2 text-[11px] text-gray-400 dark:border-dark-600 dark:text-dark-300">
          <span className="min-w-0 truncate">{alt}</span>
          {yol && <TumunuGor yol={yol} baslik={baslik} />}
        </div>
      )}
    </section>
  )
}

// Sayac — KPI kutusu: her kartta aynı yükseklik (h-16), etiket en çok 2 satır.
function Sayac({ ad, deger, ton }: { ad: string; deger: number; ton?: 'error' | 'warning' | 'info' }) {
  const renk = deger > 0 && ton ? { error: 'text-error', warning: 'text-warning', info: 'text-info' }[ton] : 'text-gray-900 dark:text-dark-50'
  return (
    <div className="flex h-16 min-w-0 flex-col justify-between rounded-md bg-gray-50 px-2.5 py-2 dark:bg-dark-800">
      <div className="line-clamp-2 text-[11px] leading-tight text-gray-500 dark:text-dark-300" title={ad}>{ad}</div>
      <div className={cx('text-lg font-semibold leading-none tabular-nums', renk)}>{deger}</div>
    </div>
  )
}

function OlcumMetni(olcum?: string) {
  return olcum ? `${pt('Son disk ölçümü')}: ${goreliZaman(olcum)}` : pt('Disk kotası ölçümü kapalı; değerler son elle ölçümden.')
}

function EgilimKarti() {
  const d = useWidget<EgilimVeri>('egilim')
  return <WidgetKabugu baslik={`${pt('Disk ve trafik eğilimi')} · ${pt('Son 30 gün')}`} ikon={I.grafik} d={d} alt={d.veri && OlcumMetni(d.veri.disk_olcum)}>{(v) => <EgilimIcerik veri={v} />}</WidgetKabugu>
}

function KotaKarti() {
  const d = useWidget<KotaVeri>('kota')
  return (
    <WidgetKabugu baslik={pt('Yakında dolacak hesaplar')} ikon={I.db} d={d} yol="/domainler"
      alt={d.veri && OlcumMetni(d.veri.disk_olcum)}>
      {(v) => v.hesaplar.length === 0 ? <Bos>{pt("Kotasının %85'ini aşan hesap yok")}</Bos> : (
        <>
          <p className="mb-2 truncate text-xs text-gray-500 dark:text-dark-300">{pt('Kotasının %{0} kadarını aşan', v.esik)}</p>
          <ul className="space-y-2.5">
            {v.hesaplar.slice(0, N_KOTA).map((h) => (
              <li key={`${h.id}-${h.metrik}`}>
                <div className="flex items-baseline justify-between gap-2 text-xs">
                  <Link to={`/abonelikler/${h.id}`} title={h.alan_adi} className={cx(LINK, 'text-sm')}>{h.alan_adi}</Link>
                  <span className="shrink-0 tabular-nums text-gray-500 dark:text-dark-300">{pt(h.metrik === 'disk' ? 'Disk' : 'Trafik')} · %{Math.round(h.yuzde)}</span>
                </div>
                <div className="mt-1"><Cubuk deger={Math.min(100, Math.round(h.yuzde))} etiket={`${h.alan_adi}: %${Math.round(h.yuzde)}`} /></div>
                <div className="mt-0.5 truncate text-[11px] tabular-nums text-gray-400 dark:text-dark-300">{mbYaz(h.kullanim_mb)} / {mbYaz(h.limit_mb)}</div>
              </li>
            ))}
          </ul>
          {Math.max(v.toplam, v.hesaplar.length) > N_KOTA && <p className="mt-2 text-xs text-gray-500 dark:text-dark-300">{pt('ve {0} hesap daha', Math.max(v.toplam, v.hesaplar.length) - N_KOTA)}</p>}
        </>
      )}
    </WidgetKabugu>
  )
}

function kalanMetni(g: number) {
  if (g < 0) return pt('{0} gün önce doldu', -g)
  if (g === 0) return pt('bugün doluyor')
  return pt('{0} gün kaldı', g)
}

function SSLKarti() {
  const d = useWidget<SSLVeri>('ssl')
  return (
    <WidgetKabugu baslik={pt('SSL sertifikaları')} ikon={I.ssl} d={d} yol="/domainler"
      alt={d.veri?.son_tarama ? `${pt('Son tarama')}: ${goreliZaman(d.veri.son_tarama)}` : undefined}>
      {(v) => (
        <>
          <div className="grid grid-cols-3 gap-2">
            <Sayac ad={pt('Süresi doldu')} deger={v.doldu} ton="error" />
            <Sayac ad={pt('14 gün içinde')} deger={v.yakin} ton="warning" />
            <Sayac ad={pt("SSL'siz aktif")} deger={v.sslsiz} ton="info" />
          </div>
          {v.hesaplar.length === 0 ? <Bos>{pt('30 gün içinde dolacak sertifika yok')}</Bos> : (
            <ul className={cx('mt-3', SATIR_AYRAC)}>
              {v.hesaplar.slice(0, N_SSL).map((h) => (
                <li key={h.id} className="flex h-9 items-center justify-between gap-2 text-sm">
                  <Link to={`/abonelikler/${h.id}/ssl`} title={h.alan_adi} className={LINK}>{h.alan_adi}</Link>
                  <span className={cx('shrink-0 text-xs tabular-nums', h.kalan_gun < 0 ? 'text-error' : h.kalan_gun <= 14 ? 'text-warning' : 'text-gray-500 dark:text-dark-300')}>{kalanMetni(h.kalan_gun)}</span>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </WidgetKabugu>
  )
}

const KAYNAK: Record<string, string> = { manuel: 'Yönetici', bayi_el: 'Bayi', bayi: 'Bayi hesabı askıda' }

function AskidaKarti() {
  const d = useWidget<AskiVeri>('askida')
  return (
    <WidgetKabugu baslik={pt('Son askıya alınanlar')} ikon={I.durakla} d={d} yol="/domainler"
      alt={d.veri && d.veri.toplam > 0 ? pt('Toplam {0}', d.veri.toplam) : undefined}>
      {(v) => v.hesaplar.length === 0 ? <Bos>{pt('Askıda hesap yok')}</Bos> : (
        <ul className={SATIR_AYRAC}>
          {v.hesaplar.slice(0, N_ASKI).map((h) => (
            <li key={h.id} className="flex h-12 items-center justify-between gap-2">
              <div className="min-w-0">
                <Link to={`/abonelikler/${h.id}`} title={h.alan_adi} className={cx(LINK, 'text-sm')}>{h.alan_adi}</Link>
                {h.kaynak && <div className="truncate text-[11px] text-gray-500 dark:text-dark-300">{pt(KAYNAK[h.kaynak] || h.kaynak)}</div>}
              </div>
              {h.zaman && <span className="shrink-0 text-xs text-gray-400 dark:text-dark-300">{goreliZaman(h.zaman)}</span>}
            </li>
          ))}
        </ul>
      )}
    </WidgetKabugu>
  )
}

const ONEM: [keyof WebsecVeri['sayim'], string, 'error' | 'warning' | 'info' | undefined][] = [
  ['critical', 'Kritik', 'error'], ['high', 'Yüksek', 'warning'], ['medium', 'Orta', 'info'], ['low', 'Düşük', undefined],
]

function WebsecKarti() {
  const d = useWidget<WebsecVeri>('websec')
  return (
    <WidgetKabugu baslik={pt('Website Security bulguları')} ikon={I.kalkanTara} d={d} yol="/website-security"
      alt={d.veri && (d.veri.son_tarama ? `${pt('Son tarama')}: ${goreliZaman(d.veri.son_tarama)}` : pt('Henüz tarama yapılmadı'))}>
      {(v) => (
        <>
          <div className="grid grid-cols-4 gap-2">
            {ONEM.map(([k, ad, ton]) => <Sayac key={k} ad={pt(ad)} deger={v.sayim[k] ?? 0} ton={ton} />)}
          </div>
          {v.son.length === 0 ? <Bos>{pt('Kritik/yüksek önemli bulgu yok')}</Bos> : (
            <ul className={cx('mt-3', SATIR_AYRAC)}>
              {v.son.slice(0, N_WEBSEC).map((b, i) => {
                const ozet = `${b.paket}${b.surum ? ` ${b.surum}` : ''} · ${b.cve}${b.duzeltme ? ` · ${pt('Düzeltilmiş sürüm {0}', b.duzeltme)}` : ''}`
                return (
                  <li key={`${b.domain_id}-${b.cve}-${i}`} className="flex h-12 flex-col justify-center">
                    <div className="flex items-center justify-between gap-2">
                      <Link to={`/website-security/domain/${b.domain_id}`} title={b.alan_adi} className={cx(LINK, 'text-sm')}>{b.alan_adi}</Link>
                      <span className={cx('shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium', b.onem === 'critical' ? 'bg-error/10 text-error' : 'bg-warning/10 text-warning')}>{pt(b.onem === 'critical' ? 'Kritik' : 'Yüksek')}</span>
                    </div>
                    <div className="mt-0.5 truncate text-xs text-gray-500 dark:text-dark-300" title={ozet}>{ozet}</div>
                  </li>
                )
              })}
            </ul>
          )}
        </>
      )}
    </WidgetKabugu>
  )
}

// PanoWidgetlari — ızgara: mobil 1 sütun, md 2, xl 3. Varsayılan (5 widget) düzen
// dikdörtgen dolar: eğilim 2 sütun geniş; md'de (2 / 1+1 / 1+1) ve xl'de
// (2+1 / 1+1+1) boş hücre kalmaz. Satırdaki kartlar eşit yükseklik (stretch).
// `gizli`: kapalı widget kimlikleri — geniş hücre bırakılır, ızgara 1'lik
// hücrelerle yeniden akar ve son satırdaki artan kart(lar) satırı doldurur.
export type PanoWidgetId = 'egilim' | 'kota' | 'ssl' | 'askida' | 'websec'
export default function PanoWidgetlari({ gizli = [] }: { gizli?: PanoWidgetId[] }) {
  useTranslation()
  const genis = gizli.length === 0
  const kartlar: [PanoWidgetId, ReactNode, string][] = [
    ['egilim', <EgilimKarti key="egilim" />, 'md:col-span-2 xl:col-span-2'],
    ['ssl', <SSLKarti key="ssl" />, ''],
    ['kota', <KotaKarti key="kota" />, ''],
    ['askida', <AskidaKarti key="askida" />, ''],
    ['websec', <WebsecKarti key="websec" />, ''],
  ]
  return (
    <section aria-label={pt('Pano istatistikleri')} data-pano-izgara
      className={cx('grid grid-cols-1 gap-5 md:grid-cols-2 xl:grid-cols-3',
        !genis && 'md:max-xl:[&>*:last-child:nth-child(odd)]:col-span-2 xl:[&>*:last-child:nth-child(3n+1)]:col-span-3 xl:[&>*:last-child:nth-child(3n+2)]:col-span-2')}>
      {kartlar.filter(([id]) => !gizli.includes(id)).map(([id, k, sinif]) => (
        <div key={id} data-pano-kart={id} className={cx('flex min-w-0 *:w-full', genis && sinif)}>{k}</div>
      ))}
    </section>
  )
}
