// Bayi Domainler (Seçenek 1, kullanıcı kararı 2026-10-03):
//   · Masaüstünde başlıklı kolonlar: Alan adı (altında yalnız durum) · Plan · Oluşturulma ·
//     Disk · Trafik · Panele gir. Kolon başlıkları sıralar (Sırala menüsüyle aynı durum).
//     Mobilde (md altı) kart düzeni: etiket–değer satırları. SSL sütunu YOK (acme.sh
//     otomatik yeniler) — yalnız "SSL alınamadı" uyarısı.
//   · Satıra tık / "Panele gir" = o alan adının müşteri panelinde tek tık oturum (lib/adina).
//   · Toplu işlemler: paket değiştir (havuz/kullanım kontrolü), IP değiştir (yalnız
//     bayiye açık sunucu IP'leri), sahiplik (bayide KAPALI — kökte bayiler arasıdır),
//     askıya al / aç, sil (onay metniyle). Her öğe sunucuda ayrı ayrı kapsam
//     doğrulamasından geçer (SahipYonetim); sonuç öğe başına gösterilir.
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { api, apiHata } from '@/lib/api'
import { adinaBaslat } from '@/lib/adina'
import { cevirT } from '@/lib/cevirT'
import { useBayiCevir } from '@/lib/bayiCeviri'
import { useToast } from '@/components/Toast'
import { Badge, Button } from '@/components/ui'
import Modal from '@/components/Modal'
import { UyariKutusu } from '@/components/UyariKutusu'
import EmptyState from '@/components/EmptyState'
import IPDegistirDiyalog from '@/components/IPDegistirDiyalog'
import { I, Ikon } from '@/components/Ikon'
import { ODAK, cx, mbYaz, sslHatali, tarihYaz, yuzde, type BayiDomain } from './ortak'
import { Cubuk } from './Grafik'
import { KullanimHucre } from './Hucreler'

type Islem = 'paket' | 'ip' | 'askida' | 'sil'
type Sonuc = { id: number; ok: boolean; neden?: string }
type Calisan = { tur: Islem; ids: number[]; adim: number; sonuc: Sonuc[] | null }
type Kolon = 'alan' | 'plan' | 'olusturulma' | 'disk' | 'trafik' | 'durum'
type Yon = 1 | -1
type Sira = { k: Kolon; yon: Yon }
type DFiltre = 'tum' | 'aktif' | 'askida'
type Plan = { id: number; ad: string; disk_kota_mb?: number }
type Havuz = { disk_limit_mb: number; disk_taahhut_mb: number }

// Sırala menüsü: kolon başlığı tıklamasının üretebileceği HER birleşim burada olmalı.
const SIRALAR: { k: Kolon; yon: Yon; ad: string }[] = [
  { k: 'alan', yon: 1, ad: 'Alan adı (A-Z)' }, { k: 'alan', yon: -1, ad: 'Alan adı (Z-A)' },
  { k: 'disk', yon: -1, ad: 'Disk kullanımı (yüksek)' }, { k: 'disk', yon: 1, ad: 'Disk kullanımı (düşük)' },
  { k: 'trafik', yon: -1, ad: 'Trafik (yüksek)' }, { k: 'trafik', yon: 1, ad: 'Trafik (düşük)' },
  { k: 'plan', yon: 1, ad: 'Plan (A-Z)' }, { k: 'plan', yon: -1, ad: 'Plan (Z-A)' },
  { k: 'durum', yon: 1, ad: 'Durum' },
  { k: 'olusturulma', yon: -1, ad: 'Oluşturulma (yeni önce)' }, { k: 'olusturulma', yon: 1, ad: 'Oluşturulma (eski önce)' },
]
// Başlığa ilk tıklamada yön: metinde A→Z, sayı/tarihte büyükten küçüğe.
const ILK_YON: Record<Kolon, Yon> = { alan: 1, plan: 1, olusturulma: -1, disk: -1, trafik: -1, durum: 1 }
const INPUT = cx('h-9 rounded-lg border border-gray-300 bg-white text-sm text-gray-900 placeholder:text-gray-400 dark:border-dark-450 dark:bg-dark-800 dark:text-dark-50', ODAK)

export default function BayiDomainler({ items, yuk, yenile, onYeni }: {
  items: BayiDomain[]; yuk: boolean; yenile: () => void; onYeni: () => void
}) {
  const t = useBayiCevir()
  const toast = useToast()
  const [q, setQ] = useState('')
  const [filtre, setFiltre] = useState<DFiltre>('tum')
  const [sira, setSira] = useState<Sira>({ k: 'alan', yon: 1 })
  const [sec, setSec] = useState<Set<number>>(new Set())
  const [islem, setIslem] = useState<Islem | null>(null)
  const [calisan, setCalisan] = useState<Calisan | null>(null)
  const [giriliyor, setGiriliyor] = useState<number | null>(null)
  const tumRef = useRef<HTMLInputElement>(null)
  const sirId = useId()
  const sahipNotId = useId()

  const gorunen = useMemo(() => {
    const s = q.trim().toLocaleLowerCase('tr')
    const f = items.filter((h) => (filtre === 'tum' || (filtre === 'askida') === !!h.askida) && (!s || h.alan_adi.toLocaleLowerCase('tr').includes(s)))
    const ad = (a: BayiDomain, b: BayiDomain) => a.alan_adi.localeCompare(b.alan_adi, 'tr')
    const kiyas = (a: BayiDomain, b: BayiDomain): number => {
      switch (sira.k) {
        case 'plan': return (a.plan_ad || '').localeCompare(b.plan_ad || '', 'tr')
        case 'olusturulma': return (a.olusturulma || '').localeCompare(b.olusturulma || '') || a.id - b.id
        case 'disk': return a.boyut_kb - b.boyut_kb
        case 'trafik': return (a.trafik_kb || 0) - (b.trafik_kb || 0)
        case 'durum': return Number(!!a.askida) - Number(!!b.askida)
        default: return ad(a, b)
      }
    }
    // Eşitlikte alan adı A→Z (yönden bağımsız) — liste kararlı kalsın.
    return [...f].sort((a, b) => sira.yon * kiyas(a, b) || ad(a, b))
  }, [items, q, filtre, sira])
  // Liste yenilenince artık olmayan seçimler düşer.
  useEffect(() => { setSec((o) => new Set([...o].filter((id) => items.some((h) => h.id === id)))) }, [items])
  const secili = items.filter((h) => sec.has(h.id))
  const gorSecili = gorunen.filter((h) => sec.has(h.id)).length
  const tumunuSec = gorunen.length > 0 && gorSecili === gorunen.length
  useEffect(() => { if (tumRef.current) tumRef.current.indeterminate = gorSecili > 0 && !tumunuSec }, [gorSecili, tumunuSec])
  const sayim = { tum: items.length, aktif: items.filter((h) => !h.askida).length, askida: items.filter((h) => h.askida).length }
  const hepsiAskida = secili.length > 0 && secili.every((h) => h.askida)
  const hicAskida = secili.every((h) => !h.askida)
  const askiAd = hepsiAskida ? 'Askıdan çıkar' : hicAskida ? 'Askıya al' : 'Askıya al / Aç'

  const degistir = (id: number) => setSec((o) => { const n = new Set(o); if (n.has(id)) n.delete(id); else n.add(id); return n })
  const tumToggle = () => setSec((o) => { const n = new Set(o); if (tumunuSec) gorunen.forEach((h) => n.delete(h.id)); else gorunen.forEach((h) => n.add(h.id)); return n })
  const basliktanSirala = (k: Kolon) => setSira((o) => (o.k === k ? { k, yon: (o.yon === 1 ? -1 : 1) as Yon } : { k, yon: ILK_YON[k] }))

  async function gir(h: BayiDomain) {
    if (giriliyor) return
    setGiriliyor(h.id)
    try { await adinaBaslat(h.id) } catch (e) {
      toast.hata(t('Müşteri paneline girilemedi'), apiHata(e))
    } finally {
      setGiriliyor(null) // yeni sekmede açıldı: bu sayfa yerinde kalır
    }
  }

  // Toplu yürütme: öğe başına ayrı istek (her biri sunucuda kapsam doğrulamasından geçer),
  // sıralı — silme/askı ağır sunucu işleridir; sonuç öğe başına toplanır.
  async function yurut(tur: Islem, liste: BayiDomain[], tek: (h: BayiDomain) => Promise<unknown>) {
    setIslem(null)
    const ids = liste.map((h) => h.id)
    setCalisan({ tur, ids, adim: 0, sonuc: null })
    const sonuc: Sonuc[] = []
    for (let i = 0; i < liste.length; i++) {
      try { await tek(liste[i]); sonuc.push({ id: liste[i].id, ok: true }) } catch (e) {
        sonuc.push({ id: liste[i].id, ok: false, neden: apiHata(e) })
      }
      setCalisan({ tur, ids, adim: i + 1, sonuc: null })
    }
    setCalisan({ tur, ids, adim: ids.length, sonuc })
  }
  function bitir() {
    if (!calisan?.sonuc) return
    setSec(new Set(calisan.sonuc.filter((x) => !x.ok).map((x) => x.id)))
    setCalisan(null)
    yenile()
  }
  const bul = (id: number) => items.find((h) => h.id === id)

  if (yuk && items.length === 0) {
    return <div className="py-12 text-center text-sm text-gray-500 dark:text-dark-300">{t('Yükleniyor…')}</div>
  }

  // Satır parçaları: masaüstü tablo ile mobil kart AYNI parçaları kullanır.
  const secKutusu = (h: BayiDomain) => (
    <label className="grid shrink-0 cursor-pointer place-items-center p-1" onClick={(e) => e.stopPropagation()}>
      <input type="checkbox" checked={sec.has(h.id)} onChange={() => degistir(h.id)} aria-label={`${h.alan_adi} ${t('seç')}`} className="size-4 rounded accent-primary-600" />
    </label>
  )
  const alanHucre = (h: BayiDomain) => (
    <div className="min-w-0">
      <button type="button" onClick={(e) => { e.stopPropagation(); gir(h) }} aria-label={`${h.alan_adi}: ${t('Panele gir')}`}
        className={cx('block max-w-full truncate rounded text-start text-base font-semibold text-gray-900 dark:text-dark-50', ODAK, h.askida && 'opacity-80')}>
        {h.alan_adi}
      </button>
      <div className="mt-1 flex flex-wrap items-center gap-1.5">
        <Badge color={h.askida ? 'warning' : 'success'} variant="soft">{t(h.askida ? 'Askıda' : 'Aktif')}</Badge>
        {sslHatali(h) && <Badge color="error" variant="soft" title={t('Sertifikanın süresi doldu; otomatik yenileme başarısız oldu.')}>{t('SSL alınamadı')}</Badge>}
      </div>
    </div>
  )
  const diskHucre = (h: BayiDomain) => <KullanimHucre kullanilanMB={h.boyut_kb / 1024} kotaMB={h.disk_kota_mb} etiket={`${h.alan_adi} ${t('Disk')}`} />
  const trafikHucre = (h: BayiDomain) => <KullanimHucre kullanilanMB={(h.trafik_kb || 0) / 1024} kotaMB={h.trafik_kota_mb} etiket={`${h.alan_adi} ${t('Trafik')}`} />
  const girDugmesi = (h: BayiDomain, metinli: boolean) => (
    <Button boyut="sm" variant="soft" color="primary" ikon={I.giris} className="shrink-0" yukleniyor={giriliyor === h.id} disabled={giriliyor !== null}
      aria-label={`${h.alan_adi}: ${t('Panele gir')}`} onClick={(e) => { e.stopPropagation(); gir(h) }}>
      {metinli && <span className="sr-only lg:not-sr-only">{t('Panele gir')}</span>}
    </Button>
  )
  const baslik = (k: Kolon, children: ReactNode, className?: string) => {
    const aktif = sira.k === k
    return (
      <th key={k} scope="col" aria-sort={aktif ? (sira.yon === 1 ? 'ascending' : 'descending') : 'none'} className={cx('px-3 py-2 text-start', className)}>
        <button type="button" onClick={() => basliktanSirala(k)} className={cx('inline-flex items-center gap-1 rounded uppercase tracking-wider', ODAK, aktif ? 'text-gray-900 dark:text-dark-50' : 'hover:text-gray-700 dark:hover:text-dark-100')}>
          {children}
          <Ikon d={aktif && sira.yon === -1 ? I.chevronAsagi : I.chevronYukari} className={cx('size-3.5', !aktif && 'opacity-30')} />
        </button>
      </th>
    )
  }

  return (
    <div className={cx('space-y-4', secili.length > 0 && 'pb-28 md:pb-0')}>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold text-gray-900 dark:text-dark-50">{t('Domainler')}</h1>
          <p className="text-sm text-gray-500 dark:text-dark-300">{t('Müşteri paneline girmek için bir alan adına tıklayın')}</p>
        </div>
        <Button color="primary" ikon={I.arti} onClick={onYeni}>{t('Yeni hosting hesabı')}</Button>
      </div>

      {/* Arama + durum filtresi + sıralama */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-700">
        <label className="relative block w-full sm:w-72">
          <span className="sr-only">{t('Alan adı ara…')}</span>
          <Ikon d={I.ara} className="pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2 text-gray-400" />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t('Alan adı ara…')} className={cx(INPUT, 'w-full ps-8 pe-3')} />
        </label>
        <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t('Durum filtresi')}>
          {(['tum', 'aktif', 'askida'] as const).map((f) => {
            const a = filtre === f
            return (
              <button key={f} type="button" aria-pressed={a} onClick={() => setFiltre(f)}
                className={cx(ODAK, 'rounded-full border px-3 py-1 text-xs font-medium',
                  a ? 'border-primary-600 bg-primary-600 text-white' : 'border-gray-300 bg-white text-gray-700 hover:bg-gray-50 dark:border-dark-450 dark:bg-dark-700 dark:text-dark-100 dark:hover:bg-dark-600')}>
                {t(f === 'tum' ? 'Tümü' : f === 'aktif' ? 'Aktif' : 'Askıda')} <span className="tabular-nums opacity-70">{sayim[f]}</span>
              </button>
            )
          })}
        </div>
        <div className="flex items-center gap-2 sm:ms-auto">
          <label htmlFor={sirId} className="text-xs text-gray-500 dark:text-dark-300">{t('Sırala')}</label>
          <select id={sirId} value={`${sira.k}:${sira.yon}`} className={cx(INPUT, 'px-2')}
            onChange={(e) => { const [k, y] = e.target.value.split(':'); setSira({ k: k as Kolon, yon: (Number(y) === -1 ? -1 : 1) as Yon }) }}>
            {SIRALAR.map((s) => <option key={`${s.k}:${s.yon}`} value={`${s.k}:${s.yon}`}>{t(s.ad)}</option>)}
          </select>
        </div>
      </div>

      {/* Toplu işlem çubuğu: masaüstünde üstte yapışık, mobilde altta sabit */}
      {secili.length > 0 && (
        <div role="region" aria-label={t('Toplu işlemler')}
          className="fixed inset-x-3 bottom-[calc(4.5rem+env(safe-area-inset-bottom))] z-40 rounded-xl border border-gray-700 bg-gray-900 px-3 py-2.5 text-white shadow-xl md:sticky md:inset-x-auto md:bottom-auto md:top-[72px] md:z-20 dark:border-dark-450 dark:bg-dark-600">
          <div className="flex flex-wrap items-center gap-x-1 gap-y-1.5">
            <span className="me-2 ps-1 text-sm font-semibold tabular-nums" aria-live="polite">{secili.length} {t('seçildi')}</span>
            <TopluDugme ikon={I.kutu} onClick={() => setIslem('paket')}>{t('Paket değiştir')}</TopluDugme>
            <TopluDugme ikon={I.kure} onClick={() => setIslem('ip')}>{t('IP değiştir')}</TopluDugme>
            <TopluDugme ikon={I.degistir} disabled aria-describedby={sahipNotId} title={t('Yalnız kök yönetici kullanabilir')}>{t('Sahiplik değiştir')}</TopluDugme>
            <span id={sahipNotId} className="sr-only">{t('Bu işlem kökte bayiler arasıdır; bayi kendi kapsamı dışına aktaramaz')}</span>
            <TopluDugme ikon={hepsiAskida ? I.oynat : I.durakla} onClick={() => setIslem('askida')}>{t(askiAd)}</TopluDugme>
            <TopluDugme ikon={I.cop} tehlike onClick={() => setIslem('sil')}>{t('Sil')}</TopluDugme>
            <TopluDugme ikon={I.kapat} className="ms-auto" onClick={() => setSec(new Set())}>{t('Seçimi temizle')}</TopluDugme>
          </div>
        </div>
      )}

      {items.length === 0 ? (
        <EmptyState baslik={t('Henüz hosting hesabınız yok')} aciklama={t('İlk hesabı oluşturun; müşterinize otomatik giriş bilgisi gönderilir.')}
          buton={{ etiket: t('Yeni hosting hesabı'), onClick: onYeni }} />
      ) : (
        <section aria-label={t('Domainler')} className="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-700">
          {/* Mobil başlık şeridi: tümünü seç + sayaç (masaüstünde tablo başlığında) */}
          <div className="flex items-center gap-3 border-b border-gray-200 bg-gray-50 px-4 py-2 text-xs text-gray-500 md:hidden dark:border-dark-600 dark:bg-dark-800 dark:text-dark-300">
            <label className="flex items-center gap-3">
              <input ref={tumRef} type="checkbox" checked={tumunuSec} onChange={tumToggle} aria-label={t('Görünen tüm alan adlarını seç')} className="size-4 rounded accent-primary-600" />
              <span>{gorSecili > 0 ? `${gorSecili} ${t('seçildi')}` : t('Tümünü seç')}</span>
            </label>
            <span className="ms-auto tabular-nums" aria-live="polite">{gorunen.length} / {items.length}</span>
          </div>
          {gorunen.length === 0 ? (
            <p className="px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-300">{t('Sonuç yok')}</p>
          ) : (<>
            {/* Masaüstü: başlıklı kolonlar */}
            <div className="hidden md:block">
              <table className="w-full table-fixed border-collapse text-sm">
                <caption className="sr-only">{t('Domainler')}</caption>
                <thead className="bg-gray-50 dark:bg-dark-800">
                  <tr className="text-[11px] font-semibold text-gray-500 dark:text-dark-300">
                    <th scope="col" className="w-11 px-3 py-2">
                      <input type="checkbox" checked={tumunuSec} onChange={tumToggle} aria-label={t('Görünen tüm alan adlarını seç')} className="size-4 rounded accent-primary-600"
                        ref={(el) => { if (el) el.indeterminate = gorSecili > 0 && !tumunuSec }} />
                    </th>
                    {baslik('alan', t('Alan adı'))}
                    {baslik('plan', t('Plan'), 'w-[14%]')}
                    {baslik('olusturulma', t('Oluşturulma'), 'w-[14%]')}
                    {baslik('disk', t('Disk'), 'w-[16%]')}
                    {baslik('trafik', t('Trafik'), 'w-[16%]')}
                    <th scope="col" className="w-14 px-3 py-2 lg:w-36"><span className="sr-only">{t('Eylemler')}</span>
                      <span className="hidden text-end font-normal tabular-nums lg:block" aria-hidden>{gorunen.length} / {items.length}</span></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200 dark:divide-dark-600">
                  {gorunen.map((h) => (
                    <tr key={h.id} onClick={() => gir(h)}
                      className={cx('cursor-pointer align-middle', sec.has(h.id) ? 'bg-primary-50 dark:bg-primary-500/10' : 'hover:bg-gray-50 dark:hover:bg-dark-600/40')}>
                      <td className="px-3 py-3">{secKutusu(h)}</td>
                      <td className="px-3 py-3">{alanHucre(h)}</td>
                      <td className="truncate px-3 py-3 text-gray-700 dark:text-dark-200">{h.plan_ad || '—'}</td>
                      <td className="px-3 py-3 tabular-nums text-gray-700 dark:text-dark-200">{tarihYaz(h.olusturulma)}</td>
                      <td className="px-3 py-3">{diskHucre(h)}</td>
                      <td className="px-3 py-3">{trafikHucre(h)}</td>
                      <td className="px-3 py-3 text-end">{girDugmesi(h, true)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {/* Mobil: kart (etiket: değer) */}
            <ul className="divide-y divide-gray-200 md:hidden dark:divide-dark-600">
              {gorunen.map((h) => (
                <li key={h.id} onClick={() => gir(h)}
                  className={cx('cursor-pointer px-3 py-3', sec.has(h.id) ? 'bg-primary-50 dark:bg-primary-500/10' : 'hover:bg-gray-50 dark:hover:bg-dark-600/40')}>
                  <div className="flex items-start gap-2">
                    {secKutusu(h)}
                    <div className="min-w-0 flex-1">{alanHucre(h)}</div>
                    {girDugmesi(h, false)}
                  </div>
                  <dl className="mt-2.5 grid grid-cols-2 gap-x-4 gap-y-2 ps-9 text-xs">
                    <div><dt className="text-gray-500 dark:text-dark-300">{t('Plan')}</dt><dd className="truncate text-gray-800 dark:text-dark-100">{h.plan_ad || '—'}</dd></div>
                    <div><dt className="text-gray-500 dark:text-dark-300">{t('Oluşturulma')}</dt><dd className="tabular-nums text-gray-800 dark:text-dark-100">{tarihYaz(h.olusturulma)}</dd></div>
                    <div><dt className="mb-0.5 text-gray-500 dark:text-dark-300">{t('Disk')}</dt><dd>{diskHucre(h)}</dd></div>
                    <div><dt className="mb-0.5 text-gray-500 dark:text-dark-300">{t('Trafik')}</dt><dd>{trafikHucre(h)}</dd></div>
                  </dl>
                </li>
              ))}
            </ul>
          </>)}
        </section>
      )}

      {islem === 'paket' && <PaketDiyalog secili={secili} kapat={() => setIslem(null)}
        onayla={(p) => yurut('paket', secili, (h) => api.put(`/domains/${h.id}/plan`, { plan_id: p }))} />}
      {islem === 'ip' && <IPDegistirDiyalog liste={secili} kapat={() => setIslem(null)}
        onayla={(ip) => yurut('ip', secili.filter((h) => h.ipv4 !== ip), (h) => api.put(`/domains/${h.id}/ip`, { ipv4: ip }))} />}
      {islem === 'askida' && (
        <Modal acik baslik={t(askiAd)} ton={hepsiAskida ? 'bilgi' : 'uyari'} onKapat={() => setIslem(null)}
          alt={<>
            <Button variant="outlined" onClick={() => setIslem(null)}>{t('Vazgeç')}</Button>
            <Button color={hepsiAskida ? 'primary' : 'warning'} ikon={hepsiAskida ? I.oynat : I.durakla}
              onClick={() => yurut('askida', secili, (h) => api.post(`/domains/${h.id}/${h.askida ? 'askidan-al' : 'askiya-al'}`))}>{t(askiAd)}</Button>
          </>}>
          <div className="space-y-4 text-sm">
            <p>{t(hepsiAskida ? 'Seçili hesaplar yeniden etkinleştirilir; siteler ve e-postalar tekrar çalışır.'
              : hicAskida ? 'Seçili hesapların siteleri ve e-postaları durdurulur. Veriler silinmez.'
                : 'Aktif hesaplar askıya alınır, askıdakiler yeniden etkinleştirilir.')}</p>
            <AlanOzeti liste={secili} />
          </div>
        </Modal>
      )}
      {islem === 'sil' && <SilDiyalog secili={secili} kapat={() => setIslem(null)}
        onayla={() => yurut('sil', secili, (h) => api.delete(`/domains/${h.id}`, { params: { onay: h.alan_adi } }))} />}
      {calisan && <IlerlemeDiyalog c={calisan} bul={bul} bitir={bitir} />}
    </div>
  )
}

function TopluDugme({ ikon, tehlike, className, children, ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { ikon: string; tehlike?: boolean }) {
  return (
    <button type="button" {...rest}
      className={cx('inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium outline-none focus-visible:ring-2 focus-visible:ring-primary-400 disabled:cursor-not-allowed disabled:opacity-45',
        tehlike ? 'text-red-300 hover:bg-red-500/20' : 'text-white/90 hover:bg-white/15 disabled:hover:bg-transparent', className)}>
      <Ikon d={ikon} className="size-4" />{children}
    </button>
  )
}

function AlanOzeti({ liste }: { liste: BayiDomain[] }) {
  const t = useBayiCevir()
  return (
    <div>
      <div className="mb-1.5 text-xs font-medium text-gray-500 dark:text-dark-300">{t('Etkilenecek alan adları')} ({liste.length})</div>
      <ul className="flex max-h-28 flex-wrap gap-1.5 overflow-y-auto" aria-label={t('Etkilenecek alan adları')}>
        {liste.map((h) => <li key={h.id} className="rounded-md bg-gray-100 px-2 py-0.5 text-xs text-gray-800 dark:bg-dark-500 dark:text-dark-100">{h.alan_adi}</li>)}
      </ul>
    </div>
  )
}

function PaketDiyalog({ secili, kapat, onayla }: { secili: BayiDomain[]; kapat: () => void; onayla: (planID: number) => void }) {
  const t = useBayiCevir()
  const [planlar, setPlanlar] = useState<Plan[] | null>(null)
  const [havuz, setHavuz] = useState<Havuz | null>(null)
  const [plan, setPlan] = useState<number>(0)
  const sid = useId()
  useEffect(() => {
    let iptal = false
    api.get<Plan[]>('/plans').then((r) => { if (iptal) return; const l = r.data || []; setPlanlar(l); if (l[0]) setPlan(l[0].id) }).catch(() => { if (!iptal) setPlanlar([]) })
    api.get<Havuz>('/reseller/ozet').then((r) => { if (!iptal) setHavuz(r.data) }).catch(() => { /* havuz göstergesi isteğe bağlı */ })
    return () => { iptal = true }
  }, [])
  const p = planlar?.find((x) => x.id === plan)
  const kota = p?.disk_kota_mb || 0
  const asan = kota > 0 ? secili.filter((h) => h.boyut_kb > kota * 1024) : []
  // Havuz tahmini (taahhüt = plan kotaları toplamı): seçililerin mevcut kotası düşülür, yenisi eklenir.
  const sonra = havuz ? havuz.disk_taahhut_mb - secili.reduce((a, h) => a + (h.disk_kota_mb || 0), 0) + secili.length * kota : 0
  const lim = havuz?.disk_limit_mb || 0
  return (
    <Modal acik baslik={t('Paket değiştir')} onKapat={kapat} genislik="lg"
      alt={<>
        <Button variant="outlined" onClick={kapat}>{t('Vazgeç')}</Button>
        <Button color="primary" disabled={!p} onClick={() => p && onayla(p.id)}>{t('Planı değiştir')}</Button>
      </>}>
      <div className="space-y-4 text-sm">
        <div>
          <label htmlFor={sid} className="mb-1 block text-xs font-medium text-gray-500 dark:text-dark-300">{t('Yeni plan')}</label>
          <select id={sid} value={plan} onChange={(e) => setPlan(Number(e.target.value))} disabled={!planlar?.length}
            className={cx(INPUT, 'h-10 w-full px-3')}>
            {planlar === null && <option>{t('Yükleniyor…')}</option>}
            {planlar?.length === 0 && <option>{t('Plan bulunamadı')}</option>}
            {planlar?.map((x) => <option key={x.id} value={x.id}>{x.ad} · {x.disk_kota_mb ? mbYaz(x.disk_kota_mb) : t('Sınırsız')}</option>)}
          </select>
        </div>
        <AlanOzeti liste={secili} />
        {asan.length > 0 && (
          <UyariKutusu ton="uyari" baslik={cevirT(t('{0} hesap yeni planın disk sınırını aşıyor'), asan.length)}>
            {asan.map((h) => `${h.alan_adi} (${mbYaz(h.boyut_kb / 1024)})`).join(', ')} · {t('bu hesaplar için işlem başarısız olur')}
          </UyariKutusu>
        )}
        {havuz && lim > 0 && (
          <div>
            <div className="mb-1 flex justify-between text-xs tabular-nums text-gray-500 dark:text-dark-300">
              <span>{t('Havuzda ayrılan disk')}</span><span>{mbYaz(havuz.disk_taahhut_mb)} → {mbYaz(Math.max(0, sonra))} / {mbYaz(lim)}</span>
            </div>
            <Cubuk deger={yuzde(sonra, lim)} etiket={t('Havuzda ayrılan disk')} />
            {sonra >= lim && <p className="mt-1.5 text-xs font-medium text-error">{t('Havuz sınırı aşılıyor; bazı hesaplar için işlem reddedilir.')}</p>}
          </div>
        )}
      </div>
    </Modal>
  )
}

function SilDiyalog({ secili, kapat, onayla }: { secili: BayiDomain[]; kapat: () => void; onayla: () => void }) {
  const t = useBayiCevir()
  const [yaz, setYaz] = useState('')
  const iid = useId()
  // Onay metni: tek alan adında alan adının kendisi; çoklu seçimde silinecek adet.
  // (Sunucu her öğe için ayrıca ?onay=<alan adı> ister.)
  const beklenen = secili.length === 1 ? secili[0].alan_adi : String(secili.length)
  const tamam = yaz.trim().toLocaleLowerCase('tr') === beklenen.toLocaleLowerCase('tr')
  return (
    <Modal acik baslik={cevirT(t('{0} alan adı silinecek'), secili.length)} ton="tehlike" onKapat={kapat}
      alt={<>
        <Button variant="outlined" onClick={kapat}>{t('Vazgeç')}</Button>
        <Button color="error" disabled={!tamam} ikon={I.cop} onClick={onayla}>{t('Kalıcı olarak sil')}</Button>
      </>}>
      <div className="space-y-4 text-sm">
        <UyariKutusu ton="tehlike">{t('Bu işlem geri alınamaz. Hesapların dosyaları, veritabanları ve e-postaları kalıcı olarak silinir.')}</UyariKutusu>
        <ul className="max-h-36 divide-y divide-gray-200 overflow-y-auto rounded-lg border border-gray-200 dark:divide-dark-600 dark:border-dark-600" aria-label={t('Silinecek alan adları')}>
          {secili.map((h) => (
            <li key={h.id} className="flex items-center justify-between gap-2 px-3 py-1.5">
              <span className="truncate font-medium text-gray-900 dark:text-dark-50">{h.alan_adi}</span>
              <span className="shrink-0 text-xs text-gray-500 dark:text-dark-300">{mbYaz(h.boyut_kb / 1024)}</span>
            </li>
          ))}
        </ul>
        <div>
          <label htmlFor={iid} className="mb-1 block text-xs font-medium text-gray-500 dark:text-dark-300">
            {secili.length === 1 ? cevirT(t('Silmek için alan adını yazın: {0}'), beklenen) : cevirT(t('Silmek için silinecek alan adı sayısını yazın: {0}'), beklenen)}
          </label>
          <input id={iid} value={yaz} onChange={(e) => setYaz(e.target.value)} autoComplete="off" spellCheck={false} placeholder={beklenen}
            className={cx(INPUT, 'h-10 w-full px-3 font-mono', tamam && '!border-error')} />
        </div>
      </div>
    </Modal>
  )
}

function IlerlemeDiyalog({ c, bul, bitir }: { c: Calisan; bul: (id: number) => BayiDomain | undefined; bitir: () => void }) {
  const t = useBayiCevir()
  const bitti = !!c.sonuc
  const ok = c.sonuc?.filter((x) => x.ok).length ?? 0
  const hata = c.sonuc?.filter((x) => !x.ok) ?? []
  const AD: Record<Islem, string> = { paket: 'Paket değiştir', ip: 'IP değiştir', askida: 'Askıya al / Aç', sil: 'Sil' }
  return (
    <Modal acik baslik={bitti ? `${t(AD[c.tur])}: ${t('sonuç')}` : `${t(AD[c.tur])}…`} ton={bitti ? (hata.length ? 'uyari' : 'basari') : undefined}
      onKapat={() => { if (bitti) bitir() }} kapatilamaz={!bitti}
      alt={<Button color="primary" disabled={!bitti} onClick={bitir}>{t('Tamam')}</Button>}>
      <div className="space-y-4 text-sm" aria-live="polite">
        {!bitti ? (
          <>
            <div className="flex justify-between text-xs tabular-nums text-gray-500 dark:text-dark-300">
              <span className="truncate">{t('İşleniyor')}: {bul(c.ids[Math.min(c.adim, c.ids.length - 1)])?.alan_adi}</span><span>{c.adim} / {c.ids.length}</span>
            </div>
            <Cubuk deger={yuzde(c.adim, c.ids.length)} etiket={t('İlerleme')} />
          </>
        ) : (
          <>
            <div className="flex flex-wrap gap-3">
              <div className="flex items-center gap-2 rounded-lg bg-success/10 px-3 py-2 text-success"><Ikon d={I.onayDaire} className="size-5" /><span className="text-sm font-semibold tabular-nums">{ok} {t('başarılı')}</span></div>
              <div className={cx('flex items-center gap-2 rounded-lg px-3 py-2', hata.length ? 'bg-error/10 text-error' : 'bg-gray-100 text-gray-500 dark:bg-dark-500 dark:text-dark-300')}>
                <Ikon d={I.tehlike} className="size-5" /><span className="text-sm font-semibold tabular-nums">{hata.length} {t('hata')}</span>
              </div>
            </div>
            {hata.length > 0 && (
              <ul className="divide-y divide-gray-200 rounded-lg border border-error/30 dark:divide-dark-600" aria-label={t('Hatalar')}>
                {hata.map((x) => (
                  <li key={x.id} className="px-3 py-2">
                    <div className="font-medium text-gray-900 dark:text-dark-50">{bul(x.id)?.alan_adi ?? `#${x.id}`}</div>
                    <div className="text-xs text-error">{x.neden || t('Bilinmeyen hata')}</div>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
      </div>
    </Modal>
  )
}
