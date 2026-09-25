// AlanPano grafikleri (recharts): canlı CPU/bellek, günlük istek, durum kodu halkası,
// aylık trafik. 🔴 Yalnız GERÇEK örnekler çizilir — tohum/sahte eğri yok; veri yoksa
// boş durum, hata varsa hata kutusu. Her grafiğin metin özeti aria-label'da.
import type { ReactNode } from 'react'
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, Pie, PieChart, ReferenceLine,
  ResponsiveContainer, Tooltip, XAxis, YAxis,
} from 'recharts'
import { Button } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import type { Istatistik, TrafikGecmis } from './tipler'
import type { Uc } from './useUc'
import type { useCanli } from './useCanli'
import { BosDurum, Durumlu, GrafikIpucu, HataDurum, Iskelet, PanoKart } from './parcalar'
import { AyrintiLink } from './parcalar'
import { GRAFIK_SARGI, ayTarihi, bayt, baytTikleri, cevir, cpuYuzde, hiz, mb, nginxGunu, saat, sablon, sayi, yerel, yuzde } from './bicim'

// Seri renkleri Tailux anlamsal token'larından (tonlar.ts ile aynı aile); token
// derlemede yoksa yanındaki değer kullanılır.
const RENK = {
  cpu: 'var(--color-primary-500, #3b82f6)',
  bellek: 'var(--color-violet-500, #8b5cf6)',
  istek: 'var(--color-primary-500, #3b82f6)',
  gelen: 'var(--color-success-light, #10b981)',
  giden: 'var(--color-primary-500, #3b82f6)',
}
const DURUM_GRUP: { anahtar: string; ad: string; renk: string }[] = [
  { anahtar: '2xx', ad: 'Başarılı', renk: 'var(--color-success-light, #10b981)' },
  { anahtar: '3xx', ad: 'Yönlendirme', renk: 'var(--color-info-light, #0ea5e9)' },
  { anahtar: '4xx', ad: 'İstemci hatası', renk: 'var(--color-warning-light, #ffa71a)' },
  { anahtar: '5xx', ad: 'Sunucu hatası', renk: 'var(--color-error-light, #ff6933)' },
]

const EKSEN = { axisLine: false, tickLine: false, tick: { fontSize: 11 } } as const

function Degrade({ id, renk }: { id: string; renk: string }) {
  return (
    <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stopColor={renk} stopOpacity={0.32} />
      <stop offset="100%" stopColor={renk} stopOpacity={0} />
    </linearGradient>
  )
}

function MiniIstat({ etiket, deger }: { etiket: string; deger: ReactNode }) {
  return (
    <div className="min-w-0 rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-600/60">
      <div className="truncate text-[11px] uppercase tracking-wide text-gray-500 dark:text-dark-300">{etiket}</div>
      <div className="truncate text-sm font-semibold tabular-nums text-gray-800 dark:text-dark-50">{deger}</div>
    </div>
  )
}

// ── Canlı CPU / bellek ─────────────────────────────────────────────────────
type Canli = ReturnType<typeof useCanli>

function CanliSeri({ baslik, deger, alt, veri, anahtar, renk, tikler, sinir, bicim, ozet, id }: {
  baslik: string; deger: string; alt: string; veri: { t: number; v: number | null }[]; anahtar: string
  renk: string; tikler: number[]; sinir?: number; bicim: (v: number) => string; ozet: string; id: string
}) {
  return (
    <div className="min-w-0">
      <div className="mb-2 flex items-baseline justify-between gap-2">
        <span className="text-xs font-medium text-gray-500 dark:text-dark-300">{baslik}</span>
        <span className="truncate text-xs text-gray-500 dark:text-dark-300">{alt}</span>
      </div>
      <div className="mb-1 text-2xl font-semibold tabular-nums text-gray-800 dark:text-dark-50">{deger}</div>
      <div className={`h-32 ${GRAFIK_SARGI}`} role="img" aria-label={ozet}>
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={veri} margin={{ top: 6, right: 4, bottom: 0, left: 0 }}>
            <defs><Degrade id={id} renk={renk} /></defs>
            <CartesianGrid strokeDasharray="3 3" vertical={false} />
            <XAxis dataKey="t" {...EKSEN} tickFormatter={(t: number) => saat(new Date(t * 1000), true)} minTickGap={40} />
            <YAxis {...EKSEN} width={52} domain={[0, tikler[tikler.length - 1]]} ticks={tikler} tickFormatter={(v: number) => bicim(v)} />
            {sinir ? <ReferenceLine y={sinir} strokeDasharray="4 4" ifOverflow="extendDomain" /> : null}
            <Tooltip cursor={{ strokeDasharray: '3 3' }}
              content={(p) => <GrafikIpucu p={p} bicim={bicim} baslik={(l) => saat(new Date(Number(l) * 1000), true)} />} />
            <Area type="monotone" dataKey="v" name={anahtar} stroke={renk} strokeWidth={2} fill={`url(#${id})`}
              dot={false} activeDot={{ r: 3 }} isAnimationActive={false} connectNulls={false} />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}

function enDusukYuksek(d: number[]): [number, number] {
  return d.length ? [Math.min(...d), Math.max(...d)] : [0, 0]
}

export function CanliKart({ c, className = '' }: { c: Canli; className?: string }) {
  const rozet = c.durum === 'hazir' || (c.durum === 'hata' && c.noktalar.length) ? (
    c.duraklatildi || c.durum === 'hata' ? (
      <span className="inline-flex shrink-0 items-center gap-1.5 text-xs text-gray-500 dark:text-dark-300">
        <span className="inline-block size-2 rounded-full bg-gray-300 dark:bg-dark-400" aria-hidden="true" />{cevir('Duraklatıldı')}
      </span>
    ) : (
      <span className="this:success inline-flex shrink-0 items-center gap-1.5 text-xs font-medium text-this-darker dark:text-this-lighter">
        <span className="relative inline-flex size-2" aria-hidden="true">
          <span className="absolute inline-flex size-full animate-ping rounded-full bg-this opacity-60 motion-reduce:animate-none" />
          <span className="relative inline-flex size-2 rounded-full bg-this" />
        </span>{cevir('Canlı')}
      </span>
    )
  ) : null

  let govde: ReactNode
  if (c.durum === 'yukleniyor') {
    govde = <Iskelet yukseklik="h-44" satir={1} />
  } else if (c.durum === 'destekyok') {
    govde = <BosDurum ikon={I.monitor} baslik={cevir('Bu sunucuda kiracı ölçümü yok')}
      aciklama={cevir('Kiracının kaynak grubu (cgroup) bulunamadı; canlı CPU/bellek gösterilemiyor.')} />
  } else if (c.durum === 'hata' && !c.noktalar.length) {
    govde = <HataDurum baslik={cevir('Canlı veri alınamadı')} mesaj={c.hata || ''} onYeniden={c.yeniden} />
  } else {
    const s = c.son
    const cpuVeri = c.noktalar.map((n) => ({ t: n.t, v: n.cpu }))
    const belVeri = c.noktalar.map((n) => ({ t: n.t, v: n.bellekB }))
    const cpuDizi = cpuVeri.map((x) => x.v).filter((v): v is number => v != null)
    const belDizi = belVeri.map((x) => x.v).filter((v): v is number => v != null)
    const cpuLimit = s?.cpu_limit_yuzde || 0
    const belLimit = s?.bellek_limit_b || 0
    const [cMin, cMax] = enDusukYuksek(cpuDizi)
    const [bMin, bMax] = enDusukYuksek(belDizi)
    const cpuSimdi = s?.cpu_yuzde
    const belSimdi = s ? s.bellek_b : 0
    // CPU ekseni: 0 / yarı / üst (üst = 100, sınır ya da tepenin 50'ye yuvarlanmışı).
    const cpuUst = Math.ceil(Math.max(100, cpuLimit, cMax * 1.15) / 50) * 50
    // Bellek ekseni: sınır varsa 0 / yarı / sınır; yoksa okunur bayt tikleri.
    const belTik = belLimit ? [0, belLimit / 2, belLimit] : baytTikleri(Math.max(bMax * 1.2, 1048576))
    govde = (
      <>
        <div className="grid grid-cols-1 gap-5 @xl:grid-cols-2">
          <CanliSeri id="ap-cpu" baslik="CPU" anahtar="CPU" renk={RENK.cpu}
            deger={cpuSimdi == null ? cevir('Ölçülüyor…') : cpuYuzde(cpuSimdi)}
            alt={cpuLimit ? sablon('Sınır %{0}', sayi(cpuLimit)) : cevir('Sınırsız')}
            veri={cpuVeri} tikler={[0, cpuUst / 2, cpuUst]} sinir={cpuLimit || undefined}
            bicim={cpuYuzde}
            ozet={sablon('CPU kullanımı, son {0} örnek: en düşük %{1}, en yüksek %{2}, şu an %{3}',
              cpuDizi.length, sayi(cMin, 1), sayi(cMax, 1), cpuSimdi == null ? '—' : sayi(cpuSimdi, 1))} />
          <CanliSeri id="ap-bellek" baslik={cevir('Bellek')} anahtar={cevir('Bellek')} renk={RENK.bellek}
            deger={s ? bayt(s.bellek_b) : '—'}
            alt={belLimit ? `${sablon('Sınır {0}', bayt(belLimit))} · ${yuzde((belSimdi / belLimit) * 100)}` : cevir('Sınırsız')}
            veri={belVeri} tikler={belTik} sinir={belLimit || undefined}
            bicim={bayt}
            ozet={sablon('Bellek kullanımı, son {0} örnek: en düşük {1}, en yüksek {2}, şu an {3}',
              belDizi.length, bayt(bMin), bayt(bMax), bayt(belSimdi))} />
        </div>
        <div className="mt-4 grid grid-cols-3 gap-2">
          {/* İlk yoklamada io null (delta yok) → "ölçülüyor…"; 0 gösterilmez. */}
          <MiniIstat etiket={cevir('Disk okuma')} deger={s?.io_okuma_bps == null ? cevir('Ölçülüyor…') : hiz(s.io_okuma_bps)} />
          <MiniIstat etiket={cevir('Disk yazma')} deger={s?.io_yazma_bps == null ? cevir('Ölçülüyor…') : hiz(s.io_yazma_bps)} />
          <MiniIstat etiket={cevir('Süreç')} deger={s ? sayi(s.surec) : '—'} />
        </div>
        {c.durum === 'hata' && (
          <UyariKutusu ton="uyari" kucuk className="mt-3" baslik={cevir('Canlı veri kesildi')}>
            <div className="flex flex-wrap items-center gap-2">
              <span className="min-w-0 flex-1">{c.hata}</span>
              <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={c.yeniden}>{cevir('Yeniden dene')}</Button>
            </div>
          </UyariKutusu>
        )}
      </>
    )
  }
  return (
    <PanoKart className={className} ikon={I.grafik} baslik={cevir('Canlı kaynak kullanımı')}
      alt={c.duraklatildi ? cevir('Sekme arka planda — yoklama durdu') : cevir("10 sn'de bir · son 5 dk")} sag={rozet}>
      {govde}
    </PanoKart>
  )
}

// ── Günlük istekler ───────────────────────────────────────────────────────
const gunEtiketi = (d: Date) => d.toLocaleDateString(yerel(), { day: 'numeric', month: 'short' })

export function GunlukIstekKart({ ist, domainId, className = '' }: { ist: Uc<Istatistik>; domainId: number; className?: string }) {
  return (
    <PanoKart className={className} ikon={I.anten} baslik={cevir('Günlük istekler')} alt={cevir('Erişim günlüğü · son 7 gün')}
      sag={<AyrintiLink to={`/abonelikler/${domainId}/istatistik`}>{cevir('Ayrıntı')}</AyrintiLink>}>
      <Durumlu uc={ist} iskelet={<Iskelet yukseklik="h-44" satir={1} />}>
        {(o) => {
          if (!o.log_var || o.toplam_istek === 0) {
            return <BosDurum ikon={I.anten} baslik={cevir('Henüz erişim günlüğü verisi yok')} aciklama={cevir('Site trafik almaya başladığında burada görünür.')} />
          }
          // Arka uç günleri "dd/Mon/yyyy" METNİ olarak sıralıyor (ay sınırında yanlış
          // sıra) — burada gerçek tarihe göre sıralanır; ayrıştırılamayan gün atılmaz.
          const gunler = (o.gunluk ?? [])
            .map((g, i) => ({ ...g, d: nginxGunu(g.tarih), i }))
            .sort((a, b) => (a.d && b.d ? a.d.getTime() - b.d.getTime() : a.i - b.i))
            .map((g) => ({ etiket: g.d ? gunEtiketi(g.d) : g.tarih, istek: g.istek }))
          const tepe = gunler.reduce((m, g) => (g.istek > m.istek ? g : m), gunler[0] ?? { etiket: '—', istek: 0 })
          return (
            <>
              <div className="mb-4 grid grid-cols-2 gap-2 @md:grid-cols-4">
                <MiniIstat etiket={cevir('Toplam istek')} deger={sayi(o.toplam_istek)} />
                <MiniIstat etiket={cevir('Bant')} deger={mb(o.toplam_bant_mb)} />
                <MiniIstat etiket={cevir('Tekil IP')} deger={sayi(o.tekil_ip)} />
                <MiniIstat etiket={cevir('Bot oranı')} deger={yuzde(o.bot_orani)} />
              </div>
              <div className={`h-44 ${GRAFIK_SARGI}`} role="img"
                aria-label={sablon('Günlük istek sayısı, {0} gün: toplam {1}, en yüksek {2} ({3})',
                  gunler.length, sayi(gunler.reduce((a, g) => a + g.istek, 0)), sayi(tepe.istek), tepe.etiket)}>
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={gunler} margin={{ top: 6, right: 4, bottom: 0, left: 0 }}>
                    <defs><Degrade id="ap-istek" renk={RENK.istek} /></defs>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} />
                    <XAxis dataKey="etiket" {...EKSEN} minTickGap={20} />
                    <YAxis {...EKSEN} width={44} tickFormatter={(v: number) => sayi(v)} allowDecimals={false} />
                    <Tooltip cursor={{ strokeDasharray: '3 3' }} content={(p) => <GrafikIpucu p={p} bicim={(v) => `${sayi(v)} ${cevir('istek')}`} />} />
                    <Area type="monotone" dataKey="istek" name={cevir('Toplam istek')} stroke={RENK.istek} strokeWidth={2}
                      fill="url(#ap-istek)" dot={{ r: 2.5, strokeWidth: 0, fill: RENK.istek }} activeDot={{ r: 4 }} isAnimationActive={false} />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </>
          )
        }}
      </Durumlu>
    </PanoKart>
  )
}

// ── Durum kodu dağılımı ───────────────────────────────────────────────────
export function DurumKoduKart({ ist, className = '' }: { ist: Uc<Istatistik>; className?: string }) {
  return (
    <PanoKart className={className} ikon={I.pano} baslik={cevir('Durum kodları')} alt={cevir('HTTP yanıt dağılımı')}>
      <Durumlu uc={ist} iskelet={<Iskelet yukseklik="h-44" satir={2} />}>
        {(o) => {
          const dilimler = DURUM_GRUP.map((g) => ({ ...g, ad: cevir(g.ad), deger: o.durum_grup?.[g.anahtar] ?? 0 }))
          const toplam = dilimler.reduce((a, x) => a + x.deger, 0)
          if (!o.log_var || toplam === 0) {
            return <BosDurum ikon={I.pano} baslik={cevir('Henüz erişim günlüğü verisi yok')} aciklama={cevir('Site trafik almaya başladığında burada görünür.')} />
          }
          const pay = (v: number) => (v / toplam) * 100
          const payMetin = (v: number) => { const p = pay(v); return p > 0 && p < 1 ? '<' + yuzde(1) : yuzde(p) }
          return (
            <div className="flex flex-1 flex-col items-center gap-4 @sm:flex-row @4xl:flex-col">
              <div className="relative size-40 shrink-0" role="img"
                aria-label={sablon('Durum kodu dağılımı: {0}', dilimler.map((x) => `${x.anahtar} ${payMetin(x.deger)}`).join(', '))}>
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie data={dilimler.filter((x) => x.deger > 0)} dataKey="deger" nameKey="ad" innerRadius="68%" outerRadius="100%"
                      paddingAngle={2} stroke="none" isAnimationActive={false}>
                      {dilimler.filter((x) => x.deger > 0).map((x) => <Cell key={x.anahtar} fill={x.renk} />)}
                    </Pie>
                    <Tooltip content={(p) => <GrafikIpucu p={p} bicim={(v) => `${sayi(v)} · ${payMetin(v)}`} baslik={() => cevir('Durum kodları')} />} />
                  </PieChart>
                </ResponsiveContainer>
                <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                  <span className="text-lg font-semibold tabular-nums text-gray-800 dark:text-dark-50">{sayi(toplam)}</span>
                  <span className="text-[11px] text-gray-500 dark:text-dark-300">{cevir('istek')}</span>
                </div>
              </div>
              <ul className="w-full min-w-0 space-y-2 text-xs">
                {dilimler.map((x) => (
                  <li key={x.anahtar} className="flex items-center gap-2">
                    <span className="inline-block size-2.5 shrink-0 rounded-sm" style={{ background: x.renk }} aria-hidden="true" />
                    <span className="font-mono text-gray-700 dark:text-dark-100">{x.anahtar}</span>
                    <span className="min-w-0 flex-1 truncate text-gray-500 dark:text-dark-300">{x.ad}</span>
                    <span className="tabular-nums text-gray-700 dark:text-dark-100">{sayi(x.deger)}</span>
                    <span className="w-12 text-end tabular-nums text-gray-500 dark:text-dark-300">{payMetin(x.deger)}</span>
                  </li>
                ))}
              </ul>
            </div>
          )
        }}
      </Durumlu>
    </PanoKart>
  )
}

// ── Aylık trafik ─────────────────────────────────────────────────────────
const ayKisa = (s: string) => { const d = ayTarihi(s); return d ? d.toLocaleDateString(yerel(), { month: 'short' }) : s }
const ayUzun = (s: string) => { const d = ayTarihi(s); return d ? d.toLocaleDateString(yerel(), { month: 'long', year: 'numeric' }) : s }

export function AylikTrafikKart({ trafik, kotaMB, className = '' }: { trafik: Uc<TrafikGecmis>; kotaMB: number; className?: string }) {
  const kalemler = trafik.veri?.kalemler ?? []
  const buAy = kalemler[kalemler.length - 1]
  // Gelen bayt ölçülmüyorsa (gelen_b hep 0) "gelen" serisi 0 çizgisi olarak yanıltırdı →
  // tek seri: aktarılan (giden) veri.
  const tekSeri = !!trafik.veri?.gelen_olculmuyor
  return (
    <PanoKart className={className} ikon={I.kure} baslik={cevir('Aylık trafik')}
      alt={tekSeri ? cevir('Son 12 ay · aktarılan (giden) veri') : cevir('Son 12 ay · gelen + giden')}
      sag={buAy ? <span className="shrink-0 text-end text-xs text-gray-500 dark:text-dark-300"><b className="block text-sm font-semibold tabular-nums text-gray-800 dark:text-dark-50">{bayt(buAy.toplam_b)}</b>{ayUzun(buAy.ay)}</span> : undefined}>
      <Durumlu uc={trafik} iskelet={<Iskelet yukseklik="h-48" />}>
        {() => {
          if (!kalemler.length || kalemler.every((k) => !k.toplam_b)) {
            return <BosDurum ikon={I.kure} baslik={cevir('Henüz trafik kaydı yok')} aciklama={cevir('Aylık kovalar site trafik aldıkça dolar.')} />
          }
          const tepe = kalemler.reduce((m, k) => (k.toplam_b > m.toplam_b ? k : m), kalemler[0])
          const kotaB = kotaMB > 0 ? kotaMB * 1048576 : 0
          // Kota çizgisi yalnız ölçeği ezmiyorsa (en yüksek ayın 2 katından küçükse) çizilir.
          const kotaCiz = kotaB > 0 && kotaB <= tepe.toplam_b * 2
          const veri = kalemler.map((k) => (tekSeri ? { ay: k.ay, toplam: k.toplam_b } : { ay: k.ay, gelen: k.gelen_b, giden: k.giden_b }))
          const tikler = baytTikleri(Math.max(tepe.toplam_b, kotaCiz ? kotaB : 0))
          return (
            <>
              <div className={`h-52 ${GRAFIK_SARGI}`} role="img"
                aria-label={sablon('Aylık trafik, {0} ay: toplam {1}, en yüksek {2} ({3})', kalemler.length,
                  bayt(kalemler.reduce((a, k) => a + k.toplam_b, 0)), bayt(tepe.toplam_b), ayUzun(tepe.ay))}>
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={veri} margin={{ top: 6, right: 4, bottom: 0, left: 0 }} barCategoryGap="22%">
                    <CartesianGrid strokeDasharray="3 3" vertical={false} />
                    <XAxis dataKey="ay" {...EKSEN} tickFormatter={ayKisa} interval="preserveStartEnd" minTickGap={8} />
                    <YAxis {...EKSEN} width={58} domain={[0, tikler[tikler.length - 1]]} ticks={tikler} tickFormatter={(v: number) => bayt(v)} />
                    {kotaCiz && <ReferenceLine y={kotaB} strokeDasharray="4 4" label={{ value: cevir('Kota'), position: 'insideTopRight', fontSize: 11 }} />}
                    <Tooltip content={(p) => <GrafikIpucu p={p} bicim={bayt} baslik={(l) => ayUzun(String(l))} />} />
                    {tekSeri ? (
                      <Bar dataKey="toplam" name={cevir('Aktarılan veri')} fill={RENK.giden} radius={[3, 3, 0, 0]} isAnimationActive={false} />
                    ) : (
                      <>
                        <Bar dataKey="gelen" name={cevir('Gelen')} stackId="t" fill={RENK.gelen} isAnimationActive={false} />
                        <Bar dataKey="giden" name={cevir('Giden')} stackId="t" fill={RENK.giden} radius={[3, 3, 0, 0]} isAnimationActive={false} />
                      </>
                    )}
                  </BarChart>
                </ResponsiveContainer>
              </div>
              <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-dark-300">
                {tekSeri ? (
                  <>
                    <span className="inline-flex items-center gap-1.5"><span className="inline-block size-2.5 rounded-sm" style={{ background: RENK.giden }} aria-hidden="true" />{cevir('Aktarılan veri')}</span>
                    <span className="inline-flex items-center gap-1.5"><Ikon d={I.bilgi} className="size-3.5 shrink-0" />{cevir('Gelen trafik ölçülmüyor')}</span>
                  </>
                ) : (
                  <>
                    <span className="inline-flex items-center gap-1.5"><span className="inline-block size-2.5 rounded-sm" style={{ background: RENK.gelen }} aria-hidden="true" />{cevir('Gelen')}</span>
                    <span className="inline-flex items-center gap-1.5"><span className="inline-block size-2.5 rounded-sm" style={{ background: RENK.giden }} aria-hidden="true" />{cevir('Giden')}</span>
                  </>
                )}
                {kotaMB > 0 && <span className="ms-auto">{cevir('Kota')}: {mb(kotaMB)}</span>}
              </div>
            </>
          )
        }}
      </Durumlu>
    </PanoKart>
  )
}
