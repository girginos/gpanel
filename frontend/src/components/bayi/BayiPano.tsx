// Bayi panosu (Seçenek 1, kullanıcı kararı 2026-10-03): KPI şeridi · kaynak havuzu
// (halka grafikler) · dikkat gerektirenler · SON OLUŞTURULAN 10 alan adı (oluşturulma
// yeni→eski; tam liste Domainler sayfasında, altta "Tümünü gör"). Veri: GET /reseller/ozet.
import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { adinaBaslat } from '@/lib/adina'
import { cevirT } from '@/lib/cevirT'
import { useBayiCevir } from '@/lib/bayiCeviri'
import { useToast } from '@/components/Toast'
import { Badge, Button, butonSinifi } from '@/components/ui'
import { UyariKutusu } from '@/components/UyariKutusu'
import { I, Ikon } from '@/components/Ikon'
import { KART, ODAK, cx, diskYuzde, mbYaz, tarihYaz, yuzde } from './ortak'
import { Cubuk, Donut } from './Grafik'
import PanoWidgetlari from '@/components/pano/PanoWidgetlari'
import { KpiEgilim } from '@/components/pano/Egilim'
import type { EgilimMetrik } from '@/components/pano/egilimHesap'

type SonDomain = { id: number; alan_adi: string; plan_ad: string; boyut_kb: number; disk_kota_mb: number; askida: boolean; olusturulma: string }
type Dikkat = { tur: 'ssl_alinamadi' | 'disk_yuksek' | 'askida'; onem: 'kritik' | 'uyari' | 'bilgi'; domain_id?: number; alan_adi?: string; yuzde?: number; adet?: number; ornekler?: string[] }
type Ozet = {
  paket_ad: string
  hosting_adet: number; hosting_limit: number; askida_adet: number
  disk_kullanim_kb: number; disk_taahhut_mb: number; disk_limit_mb: number
  trafik_kullanim_kb: number; trafik_taahhut_mb: number; trafik_limit_mb: number
  plan_adet: number
  son_domainler?: SonDomain[]; dikkat?: Dikkat[]
}

const ONEM = { kritik: 'text-error bg-error/10', uyari: 'text-warning bg-warning/10', bilgi: 'text-info bg-info/10' } as const

function Iskelet({ sinif }: { sinif: string }) {
  return <div className={cx('animate-pulse rounded-md bg-gray-200 dark:bg-dark-500', sinif)} aria-hidden="true" />
}

export default function BayiPano() {
  const t = useBayiCevir()
  const toast = useToast()
  const navigate = useNavigate()
  const [o, setO] = useState<Ozet | null>(null)
  const [hata, setHata] = useState('')
  const [yuk, setYuk] = useState(true)
  const [giriliyor, setGiriliyor] = useState<number | null>(null)

  useEffect(() => {
    let iptal = false
    api.get<Ozet>('/reseller/ozet')
      .then((r) => { if (!iptal) setO(r.data) })
      .catch((e) => { if (!iptal) setHata(apiHata(e)) })
      .finally(() => { if (!iptal) setYuk(false) })
    return () => { iptal = true }
  }, [])

  async function gir(id: number) {
    if (giriliyor) return
    setGiriliyor(id)
    try { await adinaBaslat(id) } catch (e) { toast.hata(t('Müşteri paneline girilemedi'), apiHata(e)) } finally { setGiriliyor(null) }
  }

  const diskMB = o ? o.disk_kullanim_kb / 1024 : 0
  const trafikMB = o ? o.trafik_kullanim_kb / 1024 : 0
  // egilim: KPI altındaki sparkline + değişim (D3b, GET /pano/w/egilim; geçmiş yoksa "Veri toplanıyor").
  const kpiler: { ad: string; deger: string; alt: string; egilim: EgilimMetrik }[] = o ? [
    { ad: 'Hosting hesabı', deger: String(o.hosting_adet), alt: o.hosting_limit > 0 ? `/ ${o.hosting_limit}` : '', egilim: 'hesap' },
    { ad: 'Disk kullanımı', deger: mbYaz(diskMB), alt: o.disk_limit_mb > 0 ? `/ ${mbYaz(o.disk_limit_mb)}` : '', egilim: 'disk_kb' },
    { ad: 'Aylık trafik', deger: mbYaz(trafikMB), alt: o.trafik_limit_mb > 0 ? `/ ${mbYaz(o.trafik_limit_mb)}` : '', egilim: 'trafik' },
    { ad: 'Askıda', deger: String(o.askida_adet), alt: '', egilim: 'askida' },
  ] : []
  const havuzlar = o ? [
    { ad: 'Disk', kull: diskMB, limit: o.disk_limit_mb, yaz: mbYaz, taahhut: o.disk_taahhut_mb },
    { ad: 'Hesap sayısı', kull: o.hosting_adet, limit: o.hosting_limit, yaz: (n: number) => String(n), taahhut: undefined },
    { ad: 'Aylık trafik', kull: trafikMB, limit: o.trafik_limit_mb, yaz: mbYaz, taahhut: o.trafik_taahhut_mb },
  ] : []
  const dikkat = o?.dikkat ?? []
  const son = o?.son_domainler ?? []

  const dikkatMetni = (d: Dikkat): { baslik: string; aciklama: string } => {
    switch (d.tur) {
      case 'ssl_alinamadi': return { baslik: cevirT(t('{0} için SSL alınamadı'), d.alan_adi), aciklama: t('Sertifikanın süresi doldu; otomatik yenileme başarısız oldu.') }
      case 'disk_yuksek': return { baslik: cevirT(t('{0} disk kotasının %{1} kadarını kullanıyor'), d.alan_adi, d.yuzde), aciklama: t('Planı yükseltin ya da dosyaları temizleyin.') }
      default: return { baslik: cevirT(t('{0} hesap askıda'), d.adet), aciklama: (d.ornekler ?? []).join(', ') + ((d.adet ?? 0) > (d.ornekler?.length ?? 0) ? '…' : '') }
    }
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-dark-50">{t('Pano')}</h1>
          <p className="text-sm text-gray-500 dark:text-dark-300">{t('Hosting hesaplarınızın ve kaynak havuzunuzun özeti')}</p>
        </div>
        <Button color="primary" ikon={I.arti} onClick={() => navigate('/domainler?yeni=1')}>{t('Yeni hosting hesabı')}</Button>
      </div>

      {hata && <UyariKutusu ton="tehlike" baslik={t('Pano yüklenemedi')}>{hata}</UyariKutusu>}

      {/* KPI şeridi */}
      <section aria-label={t('Özet göstergeler')} className={cx(KART, 'grid grid-cols-1 gap-px overflow-hidden !bg-gray-200 sm:grid-cols-2 lg:grid-cols-4 dark:!bg-dark-600')}>
        {yuk ? [0, 1, 2, 3].map((n) => (
          <div key={n} className="space-y-2 bg-white p-4 dark:bg-dark-700"><Iskelet sinif="h-3 w-24" /><Iskelet sinif="h-7 w-20" /></div>
        )) : kpiler.map((x) => (
          <div key={x.ad} className="bg-white p-4 dark:bg-dark-700">
            <div className="text-xs font-medium text-gray-500 dark:text-dark-300">{t(x.ad)}</div>
            <div className="mt-1 text-[1.7rem] font-semibold leading-none tabular-nums text-gray-900 dark:text-dark-50">
              {x.deger}{x.alt && <span className="ms-1.5 text-sm font-medium text-gray-500 dark:text-dark-300">{x.alt}</span>}
            </div>
            <KpiEgilim metrik={x.egilim} etiket={t(x.ad)} />
          </div>
        ))}
      </section>

      <div className="grid grid-cols-1 items-start gap-5 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        {/* Kaynak havuzu */}
        <section className={cx(KART, 'p-5')} aria-label={t('Kaynak havuzu')}>
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-sm font-semibold text-gray-900 dark:text-dark-50">{t('Kaynak havuzu')}</h2>
            {o?.paket_ad && <span className="truncate text-xs text-gray-500 dark:text-dark-300">{t('Bayi paketi')}: {o.paket_ad}</span>}
          </div>
          <div className="mt-4 grid gap-5" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 9.5rem), 1fr))' }}>
            {yuk ? [0, 1, 2].map((n) => <div key={n} className="flex justify-center"><Iskelet sinif="size-28 !rounded-full" /></div>) : havuzlar.map((r) => {
              const y = r.limit > 0 ? yuzde(r.kull, r.limit) : null
              return (
                <div key={r.ad} className="flex flex-col items-center text-center">
                  <Donut deger={y} merkez={y === null ? '∞' : `%${y}`} etiket={`${t(r.ad)}: ${y === null ? t('Sınırsız') : `%${y} ${t('kullanım')}`}`} />
                  <div className="mt-2 text-sm font-medium text-gray-900 dark:text-dark-50">{t(r.ad)}</div>
                  <div className="text-xs tabular-nums text-gray-500 dark:text-dark-300">{r.yaz(r.kull)} / {r.limit > 0 ? r.yaz(r.limit) : t('Sınırsız')}</div>
                  {r.taahhut !== undefined && r.limit > 0 && <div className="text-[11px] tabular-nums text-gray-400 dark:text-dark-300">{t('Taahhüt')} {r.yaz(r.taahhut)}</div>}
                </div>
              )
            })}
          </div>
        </section>

        {/* Dikkat gerektirenler */}
        <section className={cx(KART, 'p-5')} aria-label={t('Dikkat gerektirenler')}>
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-semibold text-gray-900 dark:text-dark-50">{t('Dikkat gerektirenler')}</h2>
            <span className="rounded-full bg-error/10 px-2 py-0.5 text-[11px] font-semibold tabular-nums text-error">{dikkat.filter((d) => d.onem !== 'bilgi').length}</span>
          </div>
          {yuk ? (
            <div className="mt-3 space-y-3">{[0, 1, 2].map((n) => <Iskelet key={n} sinif="h-8 w-full" />)}</div>
          ) : dikkat.length === 0 ? (
            <p className="py-6 text-center text-sm text-gray-500 dark:text-dark-300">{t('Her şey yolunda')}</p>
          ) : (
            <ul className="mt-3 divide-y divide-gray-200 dark:divide-dark-600">
              {dikkat.map((d, i) => {
                const m = dikkatMetni(d)
                return (
                  <li key={`${d.tur}-${d.domain_id ?? i}`} className="flex gap-3 py-3 first:pt-0 last:pb-0">
                    <span className={cx('mt-0.5 grid size-7 shrink-0 place-items-center rounded-md', ONEM[d.onem])}>
                      <Ikon d={d.onem === 'kritik' ? I.tehlike : d.onem === 'uyari' ? I.uyari : I.bilgi} className="size-4" />
                    </span>
                    <div className="min-w-0">
                      {d.domain_id ? (
                        <Link to={`/abonelikler/${d.domain_id}`} className={cx('break-all rounded text-sm font-medium text-gray-900 hover:underline dark:text-dark-50', ODAK)}>{m.baslik}</Link>
                      ) : <div className="text-sm font-medium text-gray-900 dark:text-dark-50">{m.baslik}</div>}
                      <div className="text-xs text-gray-500 dark:text-dark-300">{m.aciklama}</div>
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
        </section>
      </div>

      {/* İstatistik widget'ları (D3b): eğilim, yakında dolacaklar, SSL, askı, işler, Website Security */}
      <PanoWidgetlari />

      {/* Son oluşturulan 10 alan adı */}
      <section className={cx(KART, 'overflow-hidden')} aria-label={t('Son oluşturulan alan adları')}>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 p-4 dark:border-dark-600">
          <h2 className="text-sm font-semibold text-gray-900 dark:text-dark-50">{t('Son oluşturulan alan adları')}</h2>
          <span className="text-xs text-gray-500 dark:text-dark-300">{t('Satıra tıklayın: müşteri paneline girilir')}</span>
        </div>
        {yuk ? (
          <div className="space-y-3 p-4">{[0, 1, 2, 3, 4].map((n) => <Iskelet key={n} sinif="h-9 w-full" />)}</div>
        ) : son.length === 0 ? (
          <div className="px-4 py-10 text-center">
            <p className="text-sm font-semibold text-gray-900 dark:text-dark-50">{t('Henüz hosting hesabınız yok')}</p>
            <p className="mt-1 text-sm text-gray-500 dark:text-dark-300">{t('İlk hesabı oluşturun; müşterinize otomatik giriş bilgisi gönderilir.')}</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full border-collapse text-sm">
              <caption className="sr-only">{t('Son oluşturulan alan adları')}</caption>
              <thead className="bg-gray-50 dark:bg-dark-800">
                <tr className="text-start text-[11px] font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-300">
                  <th scope="col" className="px-3 py-2 text-start">{t('Alan adı')}</th>
                  <th scope="col" className="hidden px-3 py-2 text-start lg:table-cell">{t('Plan')}</th>
                  <th scope="col" className="px-3 py-2 text-start">{t('Disk')}</th>
                  <th scope="col" className="hidden px-3 py-2 text-start md:table-cell">{t('Oluşturulma')}</th>
                  <th scope="col" className="px-3 py-2 text-start">{t('Durum')}</th>
                  <th scope="col" className="w-10 px-2"><span className="sr-only">{t('Eylemler')}</span></th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200 dark:divide-dark-600">
                {son.map((h) => {
                  const y = diskYuzde(h)
                  return (
                    <tr key={h.id} onClick={() => gir(h.id)} className="cursor-pointer hover:bg-gray-50/70 dark:hover:bg-dark-600/40">
                      <td className="max-w-0 px-3 py-2.5 sm:max-w-none">
                        <button type="button" onClick={(e) => { e.stopPropagation(); gir(h.id) }} aria-label={`${h.alan_adi}: ${t('Panele gir')}`}
                          className={cx('block max-w-full truncate rounded text-start font-medium text-gray-900 dark:text-dark-50', ODAK)}>{h.alan_adi}</button>
                      </td>
                      <td className="hidden px-3 py-2.5 text-gray-700 lg:table-cell dark:text-dark-200">{h.plan_ad || '—'}</td>
                      <td className="px-3 py-2.5">
                        <div className="w-20 sm:w-32">
                          <div className="mb-1 text-xs tabular-nums text-gray-600 dark:text-dark-200">{mbYaz(h.boyut_kb / 1024)}{y !== null && <span className="text-gray-400"> · %{y}</span>}</div>
                          {y !== null && <Cubuk deger={y} etiket={`${h.alan_adi} ${t('Disk')}`} />}
                        </div>
                      </td>
                      <td className="hidden px-3 py-2.5 tabular-nums text-gray-700 md:table-cell dark:text-dark-200">{tarihYaz(h.olusturulma)}</td>
                      <td className="px-3 py-2.5"><Badge color={h.askida ? 'warning' : 'success'} variant="soft">{t(h.askida ? 'Askıda' : 'Aktif')}</Badge></td>
                      <td className="px-2 py-2.5 text-end">
                        <Button isIcon variant="flat" ikon={I.giris} className="size-8" yukleniyor={giriliyor === h.id} disabled={giriliyor !== null}
                          aria-label={`${h.alan_adi}: ${t('Panele gir')}`} title={t('Panele gir')} onClick={(e) => { e.stopPropagation(); gir(h.id) }} />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {!yuk && son.length > 0 && (
          <div className="border-t border-gray-200 p-2 text-center dark:border-dark-600">
            <Link to="/domainler" className={butonSinifi({ variant: 'flat', boyut: 'sm' })}>
              {t('Tümünü gör')}{o && o.hosting_adet > son.length ? ` (${o.hosting_adet})` : ''}
              <Ikon d={I.chevronSag} className="ms-1 size-4" />
            </Link>
          </div>
        )}
      </section>
    </div>
  )
}
