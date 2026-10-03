// Pano widget ızgarası (D3b) — bayi panosu (öncelik) ve kök panosu. Seçenek 1
// kurumsal dili (KART, ince kenarlık, sade tipografi). Her kart kendi ucunu
// (GET /pano/w/{id}) çağırır ve yükleniyor / boş / hata hâllerini ayrı çizer.
// Kapsam sunucuda jetondan: bayi yalnız kendi hesaplarını görür.
import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '@/store/auth'
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
type IsSatir = { id: number; tur: string; durum: string; toplam: number; basarili: number; basarisiz: number; zaman: string }
type IslerVeri = { tasima: IsSatir[]; toplu?: IsSatir[]; yedek: { planli: number; geciken: number; hata_7gun?: number; isler?: IsSatir[] } }
type WebsecVeri = { sayim: Record<'critical' | 'high' | 'medium' | 'low', number>; son_tarama: string; son: { domain_id: number; alan_adi: string; paket: string; surum: string; cve: string; onem: string; duzeltme: string; ilk: string }[] }

const LINK = cx('break-all rounded font-medium text-gray-900 hover:underline dark:text-dark-50', ODAK)

function Iskelet({ sinif }: { sinif: string }) {
  return <div className={cx('animate-pulse rounded-md bg-gray-200 dark:bg-dark-500', sinif)} aria-hidden="true" />
}

function Bos({ children }: { children: ReactNode }) {
  return <p className="py-6 text-center text-sm text-gray-500 dark:text-dark-300">{children}</p>
}

// WidgetKabugu — başlık + ikon + yenile; yükleniyor/hata hâlleri burada.
function WidgetKabugu<T>({ baslik, ikon, d, alt, children }: { baslik: string; ikon: string; d: WidgetDurum<T>; alt?: ReactNode; children: (v: T) => ReactNode }) {
  return (
    <section className={cx(KART, 'flex flex-col p-5')} aria-label={baslik} aria-busy={d.yuk}>
      <div className="flex items-center justify-between gap-2">
        <h3 className="flex min-w-0 items-center gap-2 text-sm font-semibold text-gray-900 dark:text-dark-50">
          <Ikon d={ikon} className="size-4 shrink-0 text-gray-400 dark:text-dark-300" />
          <span className="truncate">{baslik}</span>
        </h3>
        <Button isIcon variant="flat" boyut="sm" onClick={d.yenile} yukleniyor={d.yuk && d.veri !== null}
          className="size-7 rounded-full" aria-label={`${pt('Yenile')}: ${baslik}`} title={pt('Yenile')} ikon={I.yenile} />
      </div>
      <div className="mt-3 flex-1">
        {d.hata ? (
          <div className="py-4 text-center">
            <p className="text-sm text-error">{pt('Veri alınamadı')}</p>
            <Button boyut="sm" variant="outlined" ikon={I.yenile} onClick={d.yenile} className="mt-2">{pt('Yeniden dene')}</Button>
          </div>
        ) : d.veri === null ? (
          <div className="space-y-2.5">{[0, 1, 2].map((n) => <Iskelet key={n} sinif="h-7 w-full" />)}</div>
        ) : children(d.veri)}
      </div>
      {alt && d.veri !== null && !d.hata && <div className="mt-3 border-t border-gray-100 pt-2 text-[11px] text-gray-400 dark:border-dark-600 dark:text-dark-300">{alt}</div>}
    </section>
  )
}

function Sayac({ ad, deger, ton }: { ad: string; deger: number; ton?: 'error' | 'warning' | 'info' }) {
  const renk = deger > 0 && ton ? { error: 'text-error', warning: 'text-warning', info: 'text-info' }[ton] : 'text-gray-900 dark:text-dark-50'
  return (
    <div className="min-w-0 rounded-md bg-gray-50 px-2.5 py-2 dark:bg-dark-800">
      <div className="truncate text-[11px] text-gray-500 dark:text-dark-300">{ad}</div>
      <div className={cx('text-lg font-semibold leading-tight tabular-nums', renk)}>{deger}</div>
    </div>
  )
}

const DURUM_TON: Record<string, string> = {
  tamam: 'bg-success/10 text-success', hata: 'bg-error/10 text-error', kismi: 'bg-warning/10 text-warning',
  kesildi: 'bg-warning/10 text-warning', iptal: 'bg-gray-200/60 text-gray-600 dark:bg-dark-500/50 dark:text-dark-200',
}
function DurumRozeti({ durum }: { durum: string }) {
  return <span className={cx('shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium', DURUM_TON[durum] || 'bg-info/10 text-info')}>{pt(durum)}</span>
}

function IsListesi({ baslik, satirlar, bos, yol }: { baslik: string; satirlar: IsSatir[]; bos: string; yol?: string }) {
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between text-xs font-medium text-gray-600 dark:text-dark-200">
        <span>{baslik}</span>
        {yol && satirlar.length > 0 && <Link to={yol} className={cx('rounded text-primary-600 hover:underline dark:text-primary-400', ODAK)} aria-label={baslik}><Ikon d={I.ileri} className="size-3.5" /></Link>}
      </div>
      {satirlar.length === 0 ? <p className="text-xs text-gray-400 dark:text-dark-300">{bos}</p> : (
        <ul className="divide-y divide-gray-100 dark:divide-dark-600">
          {satirlar.map((s) => (
            <li key={s.id} className="flex items-center gap-2 py-1.5 text-xs">
              <span className="tabular-nums text-gray-400 dark:text-dark-300">#{s.id}</span>
              <span className="min-w-0 flex-1 truncate text-gray-700 dark:text-dark-100">{s.tur}</span>
              <span className="tabular-nums text-gray-500 dark:text-dark-300">{pt('{0}/{1} hesap', s.basarili, s.toplam)}</span>
              <DurumRozeti durum={s.durum} />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function EgilimKarti() {
  const d = useWidget<EgilimVeri>('egilim')
  return <WidgetKabugu baslik={`${pt('Disk ve trafik eğilimi')} · ${pt('Son 30 gün')}`} ikon={I.grafik} d={d}>{(v) => <EgilimIcerik veri={v} />}</WidgetKabugu>
}

function KotaKarti() {
  const d = useWidget<KotaVeri>('kota')
  return (
    <WidgetKabugu baslik={pt('Yakında dolacak hesaplar')} ikon={I.db} d={d}
      alt={d.veri && (d.veri.disk_olcum ? `${pt('Son disk ölçümü')}: ${goreliZaman(d.veri.disk_olcum)}` : pt('Disk kotası ölçümü kapalı; değerler son elle ölçümden.'))}>
      {(v) => v.hesaplar.length === 0 ? <Bos>{pt("Kotasının %85'ini aşan hesap yok")}</Bos> : (
        <>
          <p className="mb-2 text-xs text-gray-500 dark:text-dark-300">{pt('Kotasının %{0} kadarını aşan', v.esik)}</p>
          <ul className="space-y-2.5">
            {v.hesaplar.map((h) => (
              <li key={`${h.id}-${h.metrik}`}>
                <div className="flex items-baseline justify-between gap-2 text-xs">
                  <Link to={`/abonelikler/${h.id}`} className={cx(LINK, 'text-sm')}>{h.alan_adi}</Link>
                  <span className="shrink-0 tabular-nums text-gray-500 dark:text-dark-300">{pt(h.metrik === 'disk' ? 'Disk' : 'Trafik')} · %{Math.round(h.yuzde)}</span>
                </div>
                <div className="mt-1"><Cubuk deger={Math.min(100, Math.round(h.yuzde))} etiket={`${h.alan_adi}: %${Math.round(h.yuzde)}`} /></div>
                <div className="mt-0.5 text-[11px] tabular-nums text-gray-400 dark:text-dark-300">{mbYaz(h.kullanim_mb)} / {mbYaz(h.limit_mb)}</div>
              </li>
            ))}
          </ul>
          {v.toplam > v.hesaplar.length && <p className="mt-2 text-xs text-gray-500 dark:text-dark-300">{pt('ve {0} hesap daha', v.toplam - v.hesaplar.length)}</p>}
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
    <WidgetKabugu baslik={pt('SSL sertifikaları')} ikon={I.ssl} d={d}
      alt={d.veri?.son_tarama ? `${pt('Son tarama')}: ${goreliZaman(d.veri.son_tarama)}` : undefined}>
      {(v) => (
        <>
          <div className="grid grid-cols-3 gap-2">
            <Sayac ad={pt('Süresi doldu')} deger={v.doldu} ton="error" />
            <Sayac ad={pt('14 gün içinde')} deger={v.yakin} ton="warning" />
            <Sayac ad={pt("SSL'siz aktif")} deger={v.sslsiz} ton="info" />
          </div>
          {v.hesaplar.length === 0 ? <Bos>{pt('30 gün içinde dolacak sertifika yok')}</Bos> : (
            <ul className="mt-3 divide-y divide-gray-100 dark:divide-dark-600">
              {v.hesaplar.map((h) => (
                <li key={h.id} className="flex items-center justify-between gap-2 py-2 text-sm">
                  <Link to={`/abonelikler/${h.id}/ssl`} className={LINK}>{h.alan_adi}</Link>
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
    <WidgetKabugu baslik={pt('Son askıya alınanlar')} ikon={I.durakla} d={d} alt={d.veri && d.veri.toplam > 0 ? pt('Toplam {0}', d.veri.toplam) : undefined}>
      {(v) => v.hesaplar.length === 0 ? <Bos>{pt('Askıda hesap yok')}</Bos> : (
        <ul className="divide-y divide-gray-100 dark:divide-dark-600">
          {v.hesaplar.map((h) => (
            <li key={h.id} className="flex items-center justify-between gap-2 py-2">
              <div className="min-w-0">
                <Link to={`/abonelikler/${h.id}`} className={cx(LINK, 'text-sm')}>{h.alan_adi}</Link>
                {h.kaynak && <div className="text-[11px] text-gray-500 dark:text-dark-300">{pt(KAYNAK[h.kaynak] || h.kaynak)}</div>}
              </div>
              {h.zaman && <span className="shrink-0 text-xs text-gray-400 dark:text-dark-300">{goreliZaman(h.zaman)}</span>}
            </li>
          ))}
        </ul>
      )}
    </WidgetKabugu>
  )
}

function IslerKarti({ yonetici }: { yonetici: boolean }) {
  const d = useWidget<IslerVeri>('isler')
  return (
    <WidgetKabugu baslik={pt('Taşıma ve yedek işleri')} ikon={I.tasi} d={d}>
      {(v) => (
        <div className="space-y-4">
          <IsListesi baslik={pt('Son taşımalar')} satirlar={v.tasima} bos={pt('Taşıma işi yok')} yol="/araclar/tasima" />
          <div>
            <div className="mb-1.5 text-xs font-medium text-gray-600 dark:text-dark-200">{pt('Yedek')}</div>
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
              <Sayac ad={pt('Planlı yedekli hesap')} deger={v.yedek.planli} />
              <Sayac ad={pt('Geciken yedek')} deger={v.yedek.geciken} ton="warning" />
              {v.yedek.hata_7gun !== undefined && <Sayac ad={pt('Son 7 günde yedek hatası')} deger={v.yedek.hata_7gun} ton="error" />}
            </div>
          </div>
          {yonetici && v.yedek.isler && <IsListesi baslik={pt('Son yedek işleri')} satirlar={v.yedek.isler} bos={pt('Yedek işi yok')} yol="/backup-yonetimi" />}
          {yonetici && v.toplu && <IsListesi baslik={pt('Son toplu işlemler')} satirlar={v.toplu} bos={pt('Toplu işlem yok')} />}
        </div>
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
    <WidgetKabugu baslik={pt('Website Security bulguları')} ikon={I.kalkanTara} d={d}
      alt={d.veri && (d.veri.son_tarama ? `${pt('Son tarama')}: ${goreliZaman(d.veri.son_tarama)}` : pt('Henüz tarama yapılmadı'))}>
      {(v) => (
        <>
          <div className="grid grid-cols-4 gap-2">
            {ONEM.map(([k, ad, ton]) => <Sayac key={k} ad={pt(ad)} deger={v.sayim[k] ?? 0} ton={ton} />)}
          </div>
          {v.son.length === 0 ? <Bos>{pt('Kritik/yüksek önemli bulgu yok')}</Bos> : (
            <ul className="mt-3 divide-y divide-gray-100 dark:divide-dark-600">
              {v.son.map((b, i) => (
                <li key={`${b.domain_id}-${b.cve}-${i}`} className="py-2">
                  <div className="flex items-center justify-between gap-2">
                    <Link to={`/website-security/domain/${b.domain_id}`} className={cx(LINK, 'text-sm')}>{b.alan_adi}</Link>
                    <span className={cx('shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium', b.onem === 'critical' ? 'bg-error/10 text-error' : 'bg-warning/10 text-warning')}>{pt(b.onem === 'critical' ? 'Kritik' : 'Yüksek')}</span>
                  </div>
                  <div className="mt-0.5 truncate text-xs text-gray-500 dark:text-dark-300">
                    {b.paket}{b.surum && ` ${b.surum}`} · {b.cve}{b.duzeltme && ` · ${pt('Düzeltilmiş sürüm {0}', b.duzeltme)}`}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </WidgetKabugu>
  )
}

// PanoWidgetlari — ızgara (mobilde tek sütun, md 2, xl 3). Yönetici ek olarak
// sunucu geneli yedek işlerini ve toplu işlemleri görür (bayide kaynak yok).
export default function PanoWidgetlari() {
  useTranslation()
  const yonetici = useAuth((s) => s.kullanici?.rol === 'admin')
  return (
    <section aria-label={pt('Pano istatistikleri')} className="grid grid-cols-1 items-start gap-5 md:grid-cols-2 xl:grid-cols-3">
      <EgilimKarti />
      <KotaKarti />
      <SSLKarti />
      <AskidaKarti />
      <IslerKarti yonetici={yonetici} />
      <WebsecKarti />
    </section>
  )
}
