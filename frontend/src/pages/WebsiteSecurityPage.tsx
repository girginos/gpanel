import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { useRol, yoneticiMi } from '@/lib/rol'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import { TaramaButonu } from '@/components/TaramaButonu'
import { YOKLAMA_MS, degerlendir, satirAnahtari, zamanYaz, type Bekleyen } from '@/lib/websecTarama'

/*
 * Website Security Monitor — yönetici + bayi.
 *
 * Tarayıcı panel'deki TÜM domainleri 6 saatte bir tarar. Yönetici sunucu genelini,
 * bayi YALNIZ kendi hesaplarındaki domainleri görür: kapsamı sunucu zorlar (sayaç,
 * liste, durum dahil); sayfa bayide yönetici-yalnız alanları (taranan uygulama, son
 * hata) çizmez. Bu sayfa DOMAIN ODAKLI:
 * her domain bir satır (durum: taranıyor / sırada / N açık / temiz / uygulama yok / beklemede).
 * Açıkların kendisi domaine ÖZEL sayfada listelenir (/website-security/domain/:id)
 * — böylece domain listesi ile açık listesi artık alt alta yığılmaz.
 */

// Bayi yanıtında scanned_apps / duration_ms / last_error / app_sayaclari YOK
// (sunucu geneli bilgi) — bu yüzden opsiyonel.
type Status = {
  running: boolean
  last_run: string | null
  last_success: string | null
  total_findings: number
  critical: number; high: number; medium: number; low: number
  scanned_apps?: number
  duration_ms?: number
  last_error?: string | null
  next_estimate?: string
  ekosistemler?: string[] | null
  app_sayaclari?: Record<string, number> | null
  // Son yem sorgulu taramada güvenlik veri kaynaklarına erişilemedi (Temiz güvencesi eskimiş olabilir)
  feed_uyari?: boolean
}

// Görünen her domain'in (yönetici: tümü, bayi: kendi) güvenlik durumu.
type Uygulama = {
  domain_id: number
  ek_alan_id?: number // >0: satır bir EK alan adınındır (alan_adi = ek alanın kendi adı)
  alan_adi: string
  app_type: string
  install_path: string
  app_version: string
  paket_sayisi: number
  bulgu_sayisi: number
  son_tarama: string
  domain_son_tarama?: string // domainin kendi son tarama zamanı (uygulaması olmasa da dolu)
  son_sonuc?: { uygulama_sayisi: number; bulgu_sayisi: number }
  durum: string // taraniyor | sirada | acik | temiz | desteklenmiyor | beklemede
}

// Tarama isteğinin yanıtı: kuyruğa alınınca 202 {sirada: n}.
type TaramaYanit = { sirada?: number; zaten?: boolean }

const durumKodu = (e: unknown): number | undefined =>
  (e as { response?: { status?: number } })?.response?.status

type EnvanterYanit = {
  toplam: number
  items: Uygulama[]
  desteklenmeyen: number
}

const SEV_RENK: Record<string, string> = {
  critical: 'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300',
  high:     'bg-orange-100 text-orange-800 dark:bg-orange-950/40 dark:text-orange-300',
  medium:   'bg-amber-100 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300',
  low:      'bg-slate-100 text-slate-700 dark:bg-dark-700 dark:text-slate-300',
}

const APP_META: Record<string, { ad: string; renk: string }> = {
  'wordpress':    { ad: 'WordPress',    renk: 'bg-blue-100 text-blue-800 dark:bg-blue-950/40 dark:text-blue-300' },
  'nodejs':       { ad: 'Node.js',      renk: 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' },
  'php-composer': { ad: 'PHP Composer', renk: 'bg-violet-100 text-violet-800 dark:bg-violet-950/40 dark:text-violet-300' },
}
const appAd = (t: string) => APP_META[t]?.ad ?? (t || '—')
const appRenk = (t: string) => APP_META[t]?.renk ?? 'bg-slate-100 text-slate-700 dark:bg-dark-700 dark:text-slate-300'

// Durum rozetleri — domain başına.

const WSEC_EN: Record<string, string> = {
  "Türkçe": "English",
  "açık": "open",
  "Temiz": "Clean",
  "Uygulama yok": "No app",
  "Beklemede": "Pending",
  "Barındırılan tüm domainleri 6 saatte bir tarar, bilinen CVE zafiyetleriyle eşleştirir.": "Scans all hosted domains every 6 hours and matches them against known CVE vulnerabilities.",
  "Bir domainin açıkları için satırına tıklayın.": "Click a domain's row to see its vulnerabilities.",
  "filtre temizle": "clear filter",
  "Başlatılıyor…": "Starting…",
  "Kritik": "Critical",
  "Orta": "Medium",
  "Son tarama": "Last scan",
  "Taranan uygulama": "Scanned apps",
  "Toplam bulgu": "Total findings",
  "Son hata:": "Last error:",
  "Domain ara…": "Search domain…",
  "Ek": "Add-on",
  "Ek alan adı (addon domain)": "Add-on domain",
  "uygulama yok": "no app",
  "Tekrar dene": "Retry",
  "Bu arama/filtreyle domain yok.": "No domains with this search/filter.",
  "Paket": "Package",
  "Tara": "Scan",
  "Veri kaynakları: WordPress → wpvulnerability.net · Node.js / PHP Composer → osv.dev · Yeni bulgular her 6 saatte bir güncellenir": "Data sources: WordPress → wpvulnerability.net · Node.js / PHP Composer → osv.dev · New findings are updated every 6 hours",
  "Domain bulunamadı.": "Domain not found.",
  "Rescan hatası": "Rescan error",
  "Son başarılı": "Last successful",
  "Tarama hatası": "Scan error",
  "Tümünü Tara": "Scan All",
  "Tür:": "Type:",
  "İşlem başarısız": "Operation failed",
  "Hesaplarınızdaki domainleri 6 saatte bir tarar, bilinen CVE zafiyetleriyle eşleştirir.": "Scans the domains in your accounts every 6 hours and matches them against known CVE vulnerabilities.",
  "Domainlerimi Tara": "Scan My Domains",
  "Sırada": "Queued",
  "Tarama sıraya alındı": "Scan queued",
  "{0} domaininiz tarama sırasında.": "{0} of your domains are queued for scanning.",
  "Taranacak domaininiz yok.": "You have no domains to scan.",
  "Tarama başlatıldı": "Scan started",
  "Sonuçlar tarama bitince burada görünür.": "Results appear here when the scan finishes.",
  "Tarama sınırı": "Scan limit",
  "Sunucu geneli tarama sürüyor": "A server-wide scan is in progress",
  "Tarama tamamlandı": "Scan completed",
  "{0} uygulama tarandı, {1} bulgu.": "{0} applications scanned, {1} findings.",
  "Taranacak uygulama bulunamadı (WordPress, Node.js veya PHP Composer yok).": "No application to scan was found (no WordPress, Node.js or PHP Composer).",
  "Bu domainde taranabilir bir uygulama bulunamadı (WordPress, Node.js, PHP Composer). Yeni bir uygulama kurduysanız yeniden tarayın.": "No scannable application was found on this domain (WordPress, Node.js, PHP Composer). Scan again if you installed one.",
  "Uygulama bulunamadı": "No app found",
  "Zaten sırada ya da taranıyor": "Already queued or scanning",
  "Bu domain için tarama zaten sırada ya da sürüyor.": "A scan for this domain is already queued or in progress.",
  "Tarama beklenenden uzun sürüyor": "Scan is taking longer than expected",
  "Sonuç gelince bu sayfada görünür; sayfayı yenileyebilirsiniz.": "The result will appear on this page; you can refresh it.",
  "Güvenlik veri kaynaklarına şu an erişilemiyor, sonuçlar güncel olmayabilir.": "Security data sources are currently unreachable; results may be out of date.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (WSEC_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function DurumRozet({ u, bekliyor }: { u: Uygulama; bekliyor?: boolean }) {
  switch (bekliyor && u.durum !== 'sirada' ? 'taraniyor' : u.durum) {
    case 'taraniyor':
      return (
        <Badge color="info" variant="soft" className="inline-flex items-center gap-1.5 px-2 py-0.5 text-[11px] font-medium">
          <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-blue-500" /> {cevir("Taranıyor…")}
        </Badge>
      )
    case 'sirada':
      return (
        <Badge color="info" variant="soft" className="inline-flex items-center gap-1.5 px-2 py-0.5 text-[11px] font-medium">
          <Ikon d={I.saat} className="size-3 shrink-0" /> {cevir("Sırada")}
        </Badge>
      )
    case 'acik':
      return (
        <Badge color="error" variant="soft" className="px-2 py-0.5 text-[11px] font-semibold">
          {u.bulgu_sayisi} {cevir("açık")}
        </Badge>
      )
    case 'temiz':
      return (
        <Badge color="success" variant="soft" className="px-2 py-0.5 text-[11px] font-medium">
          {cevir("Temiz")}
        </Badge>
      )
    case 'desteklenmiyor':
      return (
        <span title={cevir("Bu domainde taranabilir bir uygulama bulunamadı (WordPress, Node.js, PHP Composer). Yeni bir uygulama kurduysanız yeniden tarayın.")}>
          <Badge variant="soft" className="px-2 py-0.5 text-[11px]">
            {cevir("Uygulama yok")}
          </Badge>
        </span>
      )
    default: // beklemede
      return (
        <Badge color="warning" variant="soft" className="px-2 py-0.5 text-[11px]">
          {cevir("Beklemede")}
        </Badge>
      )
  }
}

export default function WebsiteSecurityPage() {
  useTranslation() // dil re-render aboneligi
  const [status, setStatus] = useState<Status | null>(null)
  const [envanter, setEnvanter] = useState<EnvanterYanit | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [taranıyor, setTaranıyor] = useState(false)
  const [appFiltre, setAppFiltre] = useState<string>('') // domain tablosunu ekosisteme göre süz
  const [arama, setArama] = useState<string>('')
  const [tarananDomain, setTarananDomain] = useState<number | null>(null)
  // Sonucu beklenen domainler (istek sonrası): satır "Taranıyor…" gösterir, bitince toast.
  const [bekleyenIds, setBekleyenIds] = useState<Set<number>>(new Set())
  const bekleyen = useRef<Map<number, Bekleyen>>(new Map())
  const envanterRef = useRef<EnvanterYanit | null>(null)
  const yukluyor = useRef(false) // istekler üst üste binmesin
  const canli = useRef(true)     // bileşen mount'lu mu

  const dialog = useDialog()
  const toast = useToast()
  const nav = useNavigate()
  const ilkYuklendi = useRef(false)
  // Bayi (ve bilinmeyen rol) yönetici-yalnız öğeleri görmez; veri kapsamını sunucu zorlar.
  const yonetici = yoneticiMi(useRol())

  const bekleyenYaz = () => setBekleyenIds(new Set(bekleyen.current.keys()))

  // Bekleyen domainlerin sonucunu değerlendir: biten varsa tek toast ("Tarama tamamlandı"
  // + sonuç özeti); zaman aşımı → sessizce bırakma, bilgi ver.
  const sonucuDegerlendir = (env: EnvanterYanit) => {
    if (bekleyen.current.size === 0) return
    const d = degerlendir(bekleyen.current, env.items, Date.now())
    if (d.biten.length === 0 && d.zamanAsimi.length === 0) {
      bekleyen.current = d.kalan
      return
    }
    bekleyen.current = d.kalan
    bekleyenYaz()
    if (d.biten.length > 0) {
      if (d.uygulama === 0) toast.bilgi(cevir("Tarama tamamlandı"), cevir("Taranacak uygulama bulunamadı (WordPress, Node.js veya PHP Composer yok)."))
      else toast.basari(cevir("Tarama tamamlandı"), cevirT(cevir("{0} uygulama tarandı, {1} bulgu."), d.uygulama, d.bulgu))
    }
    if (d.zamanAsimi.length > 0) {
      toast.uyari(cevir("Tarama beklenenden uzun sürüyor"), cevir("Sonuç gelince bu sayfada görünür; sayfayı yenileyebilirsiniz."))
    }
  }

  // taze: sürmekte olan yüklemenin BİTMESİNİ bekleyip yeniden yükler (üst üste binmez).
  const yukle = async (taze = false) => {
    if (yukluyor.current) {
      if (!taze) return
      while (yukluyor.current) await new Promise((r) => setTimeout(r, 50))
    }
    yukluyor.current = true
    if (!ilkYuklendi.current) setYukleniyor(true)
    setHata(null)
    try {
      const [s, env] = await Promise.all([
        api.get<Status>('/websec/status'),
        api.get<EnvanterYanit>('/websec/apps'),
      ])
      if (!canli.current) return
      setStatus(s.data)
      setEnvanter(env.data)
      envanterRef.current = env.data
      ilkYuklendi.current = true
      sonucuDegerlendir(env.data)
    } catch (e) {
      const m = apiHata(e, cevir("Yüklenemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally { yukluyor.current = false; setYukleniyor(false) }
  }
  useEffect(() => {
    canli.current = true
    void yukle()
    return () => { canli.current = false }
  }, [])

  // Yoklama: tarama aktifken (sunucu running / satır taranıyor-sırada / sonucu beklenen
  // domain var) YOKLAMA_MS aralıkla tazele; bitince dur. Sekme gizliyken yoklama yok
  // (görününce hemen tazelenir); önceki istek bitmeden yenisi başlamaz (zincirleme
  // setTimeout); bileşen kalkınca zamanlayıcı ve dinleyici temizlenir.
  const aktifTarama = !!status?.running
    || (envanter?.items.some((u) => u.durum === 'taraniyor' || u.durum === 'sirada') ?? false)
    || bekleyenIds.size > 0
  useEffect(() => {
    if (!aktifTarama) return
    let zamanlayici: ReturnType<typeof setTimeout> | undefined
    let durdu = false
    const planla = () => {
      if (durdu) return
      zamanlayici = setTimeout(async () => {
        if (!document.hidden) await yukle()
        planla()
      }, YOKLAMA_MS)
    }
    const gorunur = () => { if (!document.hidden) void yukle() }
    document.addEventListener('visibilitychange', gorunur)
    planla()
    return () => {
      durdu = true
      if (zamanlayici) clearTimeout(zamanlayici)
      document.removeEventListener('visibilitychange', gorunur)
    }
  }, [aktifTarama])

  // Tarama isteği (yönetici ve bayi aynı uç). Kuyruğa alınınca 202 {sirada: n} → bildirim;
  // 429 = bayi tarama sınırı (sunucu mesajı "N dk sonra tekrar deneyin"); 404 = domain
  // yok ya da başkasının (ikisi aynı yanıt) → liste bayat, tazelenir.
  // anahtar: tek satır Tara'sında o satırın hedef anahtarı (satirAnahtari: ana alan adı
  // domain_id, ek alan -ek_alan_id); toplu istekte boş.
  const taramaIste = async (yol: string, varsayilanHata: string, anahtar?: number) => {
    // İstek öncesi damgalar: bitiş "domain son tarama damgası değişti" ile anlaşılır.
    const onceki = new Map<number, string>()
    for (const u of envanterRef.current?.items ?? []) onceki.set(satirAnahtari(u), u.domain_son_tarama ?? '')
    try {
      const r = await api.post<TaramaYanit>(yol, {})
      if (r.data?.zaten) {
        toast.bilgi(cevir("Zaten sırada ya da taranıyor"), cevir("Bu domain için tarama zaten sırada ya da sürüyor."))
      }
      const n = r.data?.sirada
      // Bayi yanıtı {sirada: n} — n YALNIZ bu isteğin kendi domainlerinden sırada bekleyenler
      // (ortak kuyruğun uzunluğu değil). Kök yanıtında sirada yok ({started: true}).
      if (r.data?.zaten) {
        // bildirim yukarıda verildi
      } else if (typeof n === 'number') {
        if (n > 0) toast.bilgi(cevir("Tarama sıraya alındı"), cevirT(cevir("{0} domaininiz tarama sırasında."), n))
        else toast.bilgi(cevir("Taranacak domaininiz yok."))
      } else {
        toast.bilgi(cevir("Tarama başlatıldı"), cevir("Sonuçlar tarama bitince burada görünür."))
      }
      await yukle(true)
      // Sonucu beklenecek domainler: tek domain → o; toplu → şimdi aktif olanlar ya da
      // istek öncesine göre damgası değişenler (tarama çok kısa sürmüş olabilir).
      const simdiki = envanterRef.current?.items ?? []
      const t0 = Date.now()
      const ekle = (id: number) => bekleyen.current.set(id, { onceki: onceki.get(id) ?? '', aktifGoruldu: false, t0 })
      if (anahtar) ekle(anahtar)
      else for (const u of simdiki) {
        const a = satirAnahtari(u)
        if (u.durum === 'taraniyor' || u.durum === 'sirada' || (u.domain_son_tarama ?? '') !== (onceki.get(a) ?? '')) ekle(a)
      }
      if (envanterRef.current) sonucuDegerlendir(envanterRef.current)
      bekleyenYaz()
    } catch (e) {
      const kod = durumKodu(e)
      if (kod === 429) {
        await dialog.bilgi({ baslik: cevir("Tarama sınırı"), mesaj: apiHata(e, varsayilanHata), ton: 'uyari' })
        return
      }
      await dialog.bilgi({ baslik: cevir("Başlatılamadı"), mesaj: apiHata(e, varsayilanHata), ton: 'tehlike' })
      if (kod === 404) await yukle(true)
    }
  }

  const tumunuTara = async () => {
    setTaranıyor(true)
    try { await taramaIste('/websec/rescan', cevir("Rescan hatası")) } finally { setTaranıyor(false) }
  }

  // Satır başına ayrı Tara: ana alan adı satırı YALNIZ ana docroot'u, ek alan satırı
  // YALNIZ o ek alanı tarar (sunucu kuyruk anahtarı farklıdır).
  const domainTara = async (u: Uygulama) => {
    const id = satirAnahtari(u)
    const yol = u.ek_alan_id ? '/websec/rescan?ek_alan_id=' + u.ek_alan_id : '/websec/rescan?domain_id=' + u.domain_id
    setTarananDomain(id)
    try { await taramaIste(yol, cevir("Tarama hatası"), id) } finally { setTarananDomain(null) }
  }

  const listelenen = useMemo(() => {
    let arr = envanter?.items ?? []
    if (appFiltre) arr = arr.filter((u) => u.app_type === appFiltre)
    const q = arama.trim().toLowerCase()
    if (q) arr = arr.filter((u) => (u.alan_adi || '').toLowerCase().includes(q))
    return arr
  }, [envanter, appFiltre, arama])

  const detayaGit = (u: Uygulama) => {
    // App'i olan domain'in detay (açık) sayfası anlamlı; app yoksa gitme.
    if (u.app_type) nav('/website-security/domain/' + u.domain_id + (u.ek_alan_id ? '?ek=' + u.ek_alan_id : ''))
  }

  return (
    <div>
      <Breadcrumb items={[{ etiket: 'Website Security Monitor' }]} />

      <div className="mt-4 mb-6 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">
            Website Security Monitor
          </h1>
          <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
            {yonetici
              ? cevir("Barındırılan tüm domainleri 6 saatte bir tarar, bilinen CVE zafiyetleriyle eşleştirir.")
              : cevir("Hesaplarınızdaki domainleri 6 saatte bir tarar, bilinen CVE zafiyetleriyle eşleştirir.")}{' '}
            {cevir("Bir domainin açıkları için satırına tıklayın.")}
          </p>
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <span className="text-xs text-slate-500">{cevir("Tür:")}</span>
            {(status?.ekosistemler ?? ['wordpress', 'nodejs', 'php-composer']).map((e) => {
              const aktif = appFiltre === e
              return (
                <button key={e} onClick={() => setAppFiltre(aktif ? '' : e)}
                  className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-[11px] font-medium transition-colors ${appRenk(e)} ${aktif ? 'ring-2 ring-dark-700 dark:ring-slate-100' : 'hover:opacity-80'}`}>
                  {appAd(e)}
                </button>
              )
            })}
            {appFiltre && (
              <button onClick={() => setAppFiltre('')} className="text-[11px] text-slate-500 underline hover:text-slate-700 dark:hover:text-slate-300">
                {cevir("filtre temizle")}
              </button>
            )}
          </div>
        </div>
        <Button
          color="primary"
          ikon={I.kalkanTara}
          className="max-sm:min-h-10"
          onClick={tumunuTara}
          yukleniyor={taranıyor || !!status?.running}
        >
          {status?.running ? cevir("Taranıyor…") : (taranıyor ? cevir("Başlatılıyor…") : cevir(yonetici ? "Tümünü Tara" : "Domainlerimi Tara"))}
        </Button>
      </div>

      {/* Sayaç kartları — şiddet toplamları (yönetici: sunucu geneli, bayi: kendi domainleri; kapsam sunucuda) */}
      {status && (
        <div className="mb-6 grid gap-3 sm:grid-cols-2 md:grid-cols-4">
          <SayacKart etiket={cevir("Kritik")} sayi={status.critical} renk={SEV_RENK.critical} />
          <SayacKart etiket={cevir("Yüksek")} sayi={status.high} renk={SEV_RENK.high} />
          <SayacKart etiket={cevir("Orta")} sayi={status.medium} renk={SEV_RENK.medium} />
          <SayacKart etiket={cevir("Düşük")} sayi={status.low} renk={SEV_RENK.low} />
        </div>
      )}

      {/* Durum */}
      {status && (
        <div className="mb-6 rounded-lg border border-slate-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
          <div className={`grid grid-cols-2 gap-3 text-sm ${yonetici ? 'sm:grid-cols-4' : 'sm:grid-cols-3'}`}>
            <div><div className="text-xs text-slate-500">{cevir("Son tarama")}</div><div className="mt-0.5 font-medium">{zamanYaz(status.last_run)}</div></div>
            <div><div className="text-xs text-slate-500">{cevir("Son başarılı")}</div><div className="mt-0.5 font-medium">{zamanYaz(status.last_success)}</div></div>
            {/* Taranan uygulama + son hata sunucu geneli bilgidir: yalnız yöneticide */}
            {yonetici && (
              <div><div className="text-xs text-slate-500">{cevir("Taranan uygulama")}</div><div className="mt-0.5 font-medium">{status.scanned_apps ?? '—'}</div></div>
            )}
            <div><div className="text-xs text-slate-500">{cevir("Toplam bulgu")}</div><div className="mt-0.5 font-medium">{status.total_findings}</div></div>
          </div>
          {status.feed_uyari && (
            <UyariKutusu ton="uyari" className="mt-3" kucuk>
              {cevir("Güvenlik veri kaynaklarına şu an erişilemiyor, sonuçlar güncel olmayabilir.")}
            </UyariKutusu>
          )}
          {yonetici && status.last_error && !status.feed_uyari && (
            <UyariKutusu ton="tehlike" className="mt-3" kucuk>
              {cevir("Son hata:")} {status.last_error}
            </UyariKutusu>
          )}
        </div>
      )}

      {/* Domain arama */}
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <div className="relative flex-1 min-w-[220px] max-w-md">
          <input
            value={arama}
            onChange={(e) => setArama(e.target.value)}
            placeholder={cevir("Domain ara…")}
            className="w-full rounded-lg border border-slate-300 bg-white pl-9 pr-8 py-2 text-sm outline-none focus:border-slate-500 dark:border-dark-600 dark:bg-dark-900 dark:text-slate-100"
          />
          <svg viewBox="0 0 24 24" className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-slate-400" fill="none" stroke="currentColor" strokeWidth="2">
            <circle cx="11" cy="11" r="8" /><path d="m21 21-4.35-4.35" />
          </svg>
        </div>
        {envanter && (
          <div className="ml-auto text-xs text-slate-500">
            {envanter.toplam} domain{envanter.desteklenmeyen > 0 ? ` · ${envanter.desteklenmeyen} ` + cevir("uygulama yok") : ''}
          </div>
        )}
      </div>

      {/* Domain tablosu — TEK ana tablo (açık listesi ayrı sayfada) */}
      {yukleniyor && !ilkYuklendi.current ? (
        <div className="rounded-lg border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-dark-600">{cevir("Yükleniyor…")}</div>
      ) : hata ? (
        <UyariKutusu ton="tehlike">
          {hata}
          <div className="mt-2">
            <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={() => void yukle(true)} yukleniyor={yukleniyor}>{cevir("Tekrar dene")}</Button>
          </div>
        </UyariKutusu>
      ) : listelenen.length === 0 ? (
        <div className="rounded-lg border border-slate-200 bg-slate-50 py-10 text-center text-sm text-slate-500 dark:border-dark-600 dark:bg-dark-900 dark:text-slate-400">
          {arama || appFiltre ? cevir("Bu arama/filtreyle domain yok.") : cevir("Domain bulunamadı.")}
        </div>
      ) : (
        <div className="overflow-hidden rounded-lg border border-slate-200 dark:border-dark-600">
          <div className="overflow-x-auto">
            <table className="min-w-[820px] w-full text-left text-sm">
              <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-dark-800 dark:text-slate-400">
                <tr>
                  <th className="px-3 py-3 font-semibold">{cevir("Alan adı")}</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Tür")}</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Sürüm")}</th>
                  <th className="px-3 py-3 text-right font-semibold">{cevir("Paket")}</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Durum")}</th>
                  <th className="px-3 py-3 font-semibold">{cevir("Son tarama")}</th>
                  <th className="px-3 py-3 text-right font-semibold">{cevir("İşlem")}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-dark-600">
                {listelenen.map((u) => {
                  const tiklanabilir = !!u.app_type
                  const anahtar = satirAnahtari(u)
                  return (
                    <tr key={`${anahtar}-${u.app_type}-${u.install_path}`}
                      onClick={() => detayaGit(u)}
                      className={`bg-white dark:bg-dark-900 ${tiklanabilir ? 'cursor-pointer hover:bg-slate-50 dark:hover:bg-dark-800/60' : ''}`}>
                      <td className="px-3 py-2.5 font-medium text-slate-900 dark:text-slate-100">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="break-all">{u.alan_adi || `#${u.domain_id}`}</span>
                          {u.ek_alan_id ? (
                            <span title={cevir("Ek alan adı (addon domain)")}
                              className="rounded bg-slate-100 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-slate-600 dark:bg-dark-700 dark:text-slate-300">
                              {cevir("Ek")}
                            </span>
                          ) : null}
                        </div>
                      </td>
                      <td className="px-3 py-2.5">
                        {u.app_type
                          ? <span className={`rounded px-1.5 py-0.5 text-xs ${appRenk(u.app_type)}`}>{appAd(u.app_type)}</span>
                          : <span className="text-slate-400">—</span>}
                      </td>
                      <td className="px-3 py-2.5 text-slate-600 dark:text-slate-400">{u.app_version || '—'}</td>
                      <td className="px-3 py-2.5 text-right tabular-nums">{u.paket_sayisi || '—'}</td>
                      <td className="px-3 py-2.5">
                        <DurumRozet u={u} bekliyor={bekleyenIds.has(anahtar)} />
                        {u.durum === 'desteklenmiyor' && !bekleyenIds.has(anahtar) && (
                          <div className="mt-1 max-w-[16rem] text-[11px] leading-snug text-slate-500">
                            {cevir("Bu domainde taranabilir bir uygulama bulunamadı (WordPress, Node.js, PHP Composer). Yeni bir uygulama kurduysanız yeniden tarayın.")}
                          </div>
                        )}
                      </td>
                      <td className="px-3 py-2.5 text-xs text-slate-500">{zamanYaz(u.son_tarama)}</td>
                      <td className="px-3 py-2.5 text-right">
                        <TaramaButonu
                          durum={u.durum === 'taraniyor' || (bekleyenIds.has(anahtar) && u.durum !== 'sirada') ? 'taraniyor' : u.durum === 'sirada' ? 'sirada' : tarananDomain === anahtar ? 'baslatiliyor' : 'hazir'}
                          etiket={cevir("Tara")}
                          baglam={u.alan_adi || `#${u.domain_id}`}
                          acikSayisi={u.durum === 'acik' ? u.bulgu_sayisi : 0}
                          onClick={(e) => { e.stopPropagation(); void domainTara(u) }}
                          // Yönetici ucu tek kilitli (tarama sürerken 409) → tarama boyunca kapalı;
                          // bayi isteği kuyruğa girer, yalnız zaten sırada/taranan satır kapalı.
                          kilitli={yonetici && !!status?.running}
                          kilitNedeni={cevir("Sunucu geneli tarama sürüyor")}
                        />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <p className="mt-4 text-xs text-slate-500">
        {cevir("Veri kaynakları: WordPress → wpvulnerability.net · Node.js / PHP Composer → osv.dev · Yeni bulgular her 6 saatte bir güncellenir")}
      </p>
    </div>
  )
}

function SayacKart({ etiket, sayi, renk }: { etiket: string; sayi: number; renk: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 text-left dark:border-dark-600 dark:bg-dark-800">
      <div className="text-xs font-medium uppercase tracking-wider text-slate-500">{etiket}</div>
      <div className="mt-1 flex items-baseline gap-2">
        <div className={`rounded-md px-2 py-0.5 text-2xl font-semibold ${renk}`}>{sayi}</div>
      </div>
    </div>
  )
}
