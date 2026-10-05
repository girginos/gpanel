import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import type { Color } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { TaramaButonu } from '@/components/TaramaButonu'

/*
 * Domaine ÖZEL güvenlik sayfası — bir domainin bilinen açıklarını listeler.
 * /website-security/domain/:id · Website Security Monitor'den satıra tıklanınca gelinir.
 * Yönetici ve bayi aynı sayfayı kullanır; kapsamı sunucu zorlar. Bayiye başka kiracının
 * domaini olmayan domainle AYNI görünür (boş envanter + 0 bulgu, tarama 404) →
 * ikisi de "Domain bulunamadı".
 */

type Bulgu = {
  id: number
  domain_id: number
  ek_alan_id?: number
  alan_adi: string
  app_type: string
  package_name: string
  installed_version: string
  cve_id: string
  severity: string
  cvss: number
  title: string
  fixed_in: string
  source: string
  last_seen: string
}

type Sayfa = { toplam: number; sayfa: number; sayfa_boyut: number; items: Bulgu[] }

type Uygulama = {
  domain_id: number; ek_alan_id?: number; alan_adi: string; app_type: string; install_path: string
  app_version: string; paket_sayisi: number; bulgu_sayisi: number; son_tarama: string
  durum: string // taraniyor | sirada | acik | temiz | desteklenmiyor | beklemede
}
type EnvanterYanit = { toplam: number; items: Uygulama[]; desteklenmeyen: number }

// Tarama isteğinin yanıtı: kuyruğa alınınca 202 {sirada: n}.
type TaramaYanit = { sirada?: number }

const durumKodu = (e: unknown): number | undefined =>
  (e as { response?: { status?: number } })?.response?.status

// Şiddet → Tailux Badge. Kritik dolu (en güçlü vurgu), diğerleri soft; şiddet
// adı rozet metninde her zaman yazılı (renk tek başına anlam taşımaz).
const SEV_ROZET: Record<string, { color: Color; variant: 'filled' | 'soft' }> = {
  critical: { color: 'error',   variant: 'filled' },
  high:     { color: 'error',   variant: 'soft' },
  medium:   { color: 'warning', variant: 'soft' },
  low:      { color: 'neutral', variant: 'soft' },
}
const APP_META: Record<string, { ad: string; renk: Color }> = {
  'wordpress':    { ad: 'WordPress',    renk: 'info' },
  'nodejs':       { ad: 'Node.js',      renk: 'success' },
  'php-composer': { ad: 'PHP Composer', renk: 'secondary' },
}
const appAd = (t: string) => APP_META[t]?.ad ?? (t || '—')
const appRenk = (t: string): Color => APP_META[t]?.renk ?? 'neutral'


const SEC_EN: Record<string, string> = {
  "Türkçe": "English",
  "paket": "packages",
  "Bu Domaini Tara": "Scan This Domain",
  "Ek": "Add-on",
  "Ek alan adı (addon domain)": "Add-on domain",
  "Bu Ek Alanı Tara": "Scan This Add-on Domain",
  "Filtre:": "Filter:",
  "Hepsi": "All",
  "Kritik {0}": "Critical {0}",
  "Yüksek {0}": "High {0}",
  "Orta {0}": "Medium {0}",
  "Düşük {0}": "Low {0}",
  "Tekrar dene": "Retry",
  "Paket": "Package",
  "Kurulu / Fix": "Installed / Fix",
  "Başlatılamadı": "Failed to start",
  "Başlatılıyor…": "Starting…",
  "Bu domainde bilinen açık bulunmadı.": "No known vulnerabilities found on this domain.",
  "Bu filtreyle açık yok.": "No vulnerabilities with this filter.",
  "Tarama hatası": "Scan error",
  "İşlem başarısız": "Operation failed",
  "Sırada": "Queued",
  "Tarama sıraya alındı": "Scan queued",
  "Bu domain tarama sırasında.": "This domain is queued for scanning.",
  "Tarama başlatıldı": "Scan started",
  "Sonuçlar tarama bitince burada görünür.": "Results appear here when the scan finishes.",
  "Tarama sınırı": "Scan limit",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SEC_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainSecurityPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams<{ id: string }>()
  const domainID = Number(id)
  // ?ek=<ek_alan_id>: ek alan adının sayfası (yoksa ana alan adı). Geçersiz değer
  // "Domain bulunamadı" görünümüdür; ana sayfa ek alanların bulgularını KARIŞTIRMAZ.
  const [sp] = useSearchParams()
  const ekHam = sp.get('ek')
  const ekID = ekHam === null ? 0 : Number(ekHam)
  const gecerliID = Number.isSafeInteger(domainID) && domainID > 0 && Number.isSafeInteger(ekID) && ekID >= 0 && (ekHam === null || ekID > 0)
  const [sayfa, setSayfa] = useState<Sayfa>({ toplam: 0, sayfa: 1, sayfa_boyut: 50, items: [] })
  const [uyg, setUyg] = useState<Uygulama[]>([])
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [filtre, setFiltre] = useState('')
  const [aktifSayfa, setAktifSayfa] = useState(1)
  const [taranıyor, setTaranıyor] = useState(false)
  const [bulunamadi, setBulunamadi] = useState(false)
  const dialog = useDialog()
  const toast = useToast()
  const nav = useNavigate()
  const ilk = useRef(false)

  const yukle = async () => {
    if (!ilk.current) setYukleniyor(true)
    setHata(null)
    try {
      const params = new URLSearchParams({ domain_id: String(domainID), ek_alan_id: String(ekID), page: String(aktifSayfa), page_size: '100' })
      if (filtre) params.set('severity', filtre)
      const [p, env] = await Promise.all([
        api.get<Sayfa>('/websec/findings?' + params.toString()),
        api.get<EnvanterYanit>('/websec/apps?domain_id=' + domainID),
      ])
      const toplam = p.data.toplam ?? 0
      setSayfa({
        toplam, sayfa: p.data.sayfa ?? 1,
        sayfa_boyut: p.data.sayfa_boyut ?? 100, items: Array.isArray(p.data.items) ? p.data.items : [],
      })
      // Sunucu ?domain_id= ile süzer; istemci süzgeci süzgeçsiz (eski) yanıtta da doğru kalır.
      const bunun = (env.data.items ?? []).filter((u) => u.domain_id === domainID && (u.ek_alan_id ?? 0) === ekID)
      setUyg(bunun)
      // Görünen her domain envanterde (uygulaması olmasa da) en az bir satır alır. Satır da
      // bulgu da yoksa domain ya yok ya başkasının — ikisi aynı görünüm.
      setBulunamadi(bunun.length === 0 && toplam === 0)
      ilk.current = true
    } catch (e) {
      const m = apiHata(e, cevir("Yüklenemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally { setYukleniyor(false) }
  }
  useEffect(() => { if (gecerliID) void yukle() }, [domainID, ekID, gecerliID, filtre, aktifSayfa])

  // Taranıyor ya da sıradaysa canlı tazele
  useEffect(() => {
    if (!uyg.some((u) => u.durum === 'taraniyor' || u.durum === 'sirada')) return
    const t = setInterval(yukle, 5000)
    return () => clearInterval(t)
  }, [uyg])

  // Kuyruğa alınınca 202 {sirada: n}; 429 = bayi tarama sınırı (sunucu mesajı);
  // 404 = domain yok ya da başkasının (aynı yanıt) → "Domain bulunamadı" görünümü.
  const domainTara = async () => {
    setTaranıyor(true)
    try {
      const r = await api.post<TaramaYanit>(ekID > 0 ? '/websec/rescan?ek_alan_id=' + ekID : '/websec/rescan?domain_id=' + domainID, {})
      const n = r.data?.sirada
      // Bayi: {sirada: n} (yalnız kendi domainleri); kök: {started: true} — sirada yok.
      if (typeof n === 'number') { if (n > 0) toast.bilgi(cevir("Tarama sıraya alındı"), cevir("Bu domain tarama sırasında.")) }
      else toast.bilgi(cevir("Tarama başlatıldı"), cevir("Sonuçlar tarama bitince burada görünür."))
      await yukle()
    } catch (e) {
      const kod = durumKodu(e)
      if (kod === 404) setBulunamadi(true)
      else if (kod === 429) await dialog.bilgi({ baslik: cevir("Tarama sınırı"), mesaj: apiHata(e, cevir("Tarama hatası")), ton: 'uyari' })
      else await dialog.bilgi({ baslik: cevir("Başlatılamadı"), mesaj: apiHata(e, cevir("Tarama hatası")), ton: 'tehlike' })
    } finally { setTaranıyor(false) }
  }

  const alanAdi = uyg[0]?.alan_adi || sayfa.items[0]?.alan_adi || `#${domainID}`
  const taraniyorMu = uyg.some((u) => u.durum === 'taraniyor')
  const siradaMu = !taraniyorMu && uyg.some((u) => u.durum === 'sirada')
  // Son taramada açık: envanterin 'acik' satırlarının bulgu sayısı (sunucu verisi; filtreden bağımsız)
  const acikSayisi = uyg.reduce((t, u) => t + (u.durum === 'acik' ? u.bulgu_sayisi : 0), 0)
  const sev = useMemo(() => {
    const c = { critical: 0, high: 0, medium: 0, low: 0 }
    for (const b of sayfa.items) if (b.severity in c) (c as Record<string, number>)[b.severity]++
    return c
  }, [sayfa.items])

  if (!gecerliID || bulunamadi) {
    return (
      <div>
        <Breadcrumb items={[
          { etiket: 'Website Security Monitor', href: '/website-security' },
          { etiket: `#${id ?? ''}` },
        ]} />
        <UyariKutusu ton="uyari" className="mt-4">
          {cevir("Domain bulunamadı.")}
          <div className="mt-2">
            <Button variant="outlined" boyut="sm" ikon={I.geri} onClick={() => nav('/website-security')}>{cevir("Listeye dön")}</Button>
          </div>
        </UyariKutusu>
      </div>
    )
  }

  return (
    <div>
      <Breadcrumb items={[
        { etiket: 'Website Security Monitor', href: '/website-security' },
        { etiket: alanAdi },
      ]} />

      <div className="mt-4 mb-6 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="flex flex-wrap items-center gap-2 text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">
            <span className="break-all">{alanAdi}</span>
            {ekID > 0 && (
              <Badge color="neutral" variant="soft" className="text-[10px] font-semibold uppercase" title={cevir("Ek alan adı (addon domain)")}>
                {cevir("Ek")}
              </Badge>
            )}
          </h1>
          <div className="mt-2 flex flex-wrap items-center gap-2">
            {uyg.map((u) => (
              <Badge key={u.app_type} color={appRenk(u.app_type)} variant="soft" className="font-normal">
                {appAd(u.app_type)}{u.app_version ? ' ' + u.app_version : ''} · {u.paket_sayisi} {cevir("paket")}
              </Badge>
            ))}
            {taraniyorMu && (
              <Badge color="info" variant="soft" className="gap-1.5 text-[11px]">
                <span className="size-1.5 animate-pulse rounded-full bg-current" aria-hidden="true" /> {cevir("Taranıyor…")}
              </Badge>
            )}
            {siradaMu && (
              <Badge color="info" variant="soft" className="gap-1.5 text-[11px]">
                <Ikon d={I.saat} className="size-3 shrink-0" /> {cevir("Sırada")}
              </Badge>
            )}
          </div>
        </div>
        <TaramaButonu
          boyut="md"
          durum={taraniyorMu ? 'taraniyor' : siradaMu ? 'sirada' : taranıyor ? 'baslatiliyor' : 'hazir'}
          etiket={cevir(ekID > 0 ? "Bu Ek Alanı Tara" : "Bu Domaini Tara")}
          acikSayisi={acikSayisi}
          onClick={() => { void domainTara() }}
        />
      </div>

      {/* Şiddet filtresi */}
      <div className="mb-3 flex flex-wrap items-center gap-1 text-xs">
        <span className="text-slate-500 mr-1">{cevir("Filtre:")}</span>
        {([['', cevir("Hepsi")], ['critical', cevirT(cevir("Kritik {0}"), sev.critical)], ['high', cevirT(cevir("Yüksek {0}"), sev.high)], ['medium', cevirT(cevir("Orta {0}"), sev.medium)], ['low', cevirT(cevir("Düşük {0}"), sev.low)]] as [string, string][]).map(([v, ad]) => (
          <button key={v} onClick={() => { setFiltre(v); setAktifSayfa(1) }}
            className={`rounded-md border px-2 py-1 ${filtre === v ? 'border-dark-700 bg-dark-800 text-white dark:border-slate-100 dark:bg-slate-100 dark:text-slate-900' : 'border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-dark-600 dark:text-slate-400 dark:hover:bg-dark-700'}`}>
            {ad}
          </button>
        ))}
      </div>

      {yukleniyor && !ilk.current ? (
        <div className="rounded-lg border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-dark-600">{cevir("Yükleniyor…")}</div>
      ) : hata ? (
        <UyariKutusu ton="tehlike">
          {hata}
          <div className="mt-2">
            <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={yukle} yukleniyor={yukleniyor}>{cevir("Tekrar dene")}</Button>
          </div>
        </UyariKutusu>
      ) : sayfa.items.length === 0 ? (
        <UyariKutusu ton="basari">
          {taraniyorMu ? cevir("Taranıyor…") : (filtre ? cevir("Bu filtreyle açık yok.") : cevir("Bu domainde bilinen açık bulunmadı."))}
        </UyariKutusu>
      ) : (
        <div className="overflow-hidden rounded-lg border border-slate-200 dark:border-dark-600">
          <div className="overflow-x-auto">
            <table className="min-w-[860px] w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-dark-800 dark:text-slate-400">
                <tr>
                  <th className="w-32 px-3 py-3 font-semibold">{cevir("Şiddet")}</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Paket")}</th>
                  <th className="w-40 px-3 py-3 font-semibold">{cevir("Kurulu / Fix")}</th>
                  <th className="w-40 px-3 py-3 font-semibold">CVE</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Başlık")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-dark-600">
                {sayfa.items.map((b) => (
                  <tr key={b.id} className="bg-white dark:bg-dark-900">
                    <td className="px-3 py-2.5">
                      <Badge color={(SEV_ROZET[b.severity] || SEV_ROZET.low).color} variant={(SEV_ROZET[b.severity] || SEV_ROZET.low).variant} className="whitespace-nowrap text-[11px]">
                        {b.severity || '—'}{b.cvss > 0 ? ' · ' + b.cvss.toFixed(1) : ''}
                      </Badge>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="font-mono text-[13px] break-all">{b.package_name}</div>
                      <Badge color={appRenk(b.app_type)} variant="soft" className="mt-0.5 whitespace-nowrap px-1.5 py-0.5 text-[10px]">{appAd(b.app_type)}</Badge>
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[13px] whitespace-nowrap">
                      <span className="text-slate-900 dark:text-slate-100">{b.installed_version}</span>
                      {b.fixed_in && <span className="ml-1 inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400"><Ikon d={I.ileri} className="size-3 shrink-0" />{b.fixed_in}</span>}
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[12px] text-slate-700 dark:text-slate-300 whitespace-nowrap">{b.cve_id}</td>
                    <td className="px-3 py-2.5 text-slate-600 dark:text-slate-400">{b.title}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  )
}
