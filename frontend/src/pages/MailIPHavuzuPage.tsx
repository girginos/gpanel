import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { api, apiHata } from '@/lib/api'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui'

type IPKaydi = {
  ip: string
  aktif: boolean
  etiket: string
  arayuzde?: boolean
  arayuz?: string
  warmup?: boolean
  warmup_gun?: number
  agirlik?: number
  dnsbl?: string[] | null
  son_kontrol?: string
  rotasyonda?: boolean
}
type SunucuAdres = { ip: string; arayuz: string; havuzda: boolean }
type Yanit = { havuz: IPKaydi[]; arayuz: string; sunucu_ipleri?: SunucuAdres[] }
type Dedicated = { domain: string; ip: string }
type DedYanit = { dedicated: Dedicated[]; aktif_ip: string[] }

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/

const MIPH_EN: Record<string, string> = {
  "firma.com": "company.com",
  "Bu adaptörde tanımlı": "Defined on this adapter",
  "Domaine Özel IP (Dedicated)": "Domain-Specific IP (Dedicated)",
  "Havuzdaki tüm IP'leri kara listelerde (DNSBL) tara": "Scan all IPs in the pool against blocklists (DNSBL)",
  "Henüz kara liste kontrolü yapılmadı": "No blocklist check yet",
  "Henüz uygulanmadı": "Not applied yet",
  "IP algılanamadı.": "IP could not be detected.",
  "IP havuzu yüklenemedi (mail eklentisi aktif mi?)": "Failed to load IP pool (is the mail add-on active?)",
  "Kara liste kontrolü yapılamadı": "Failed to run blocklist check",
  "Sunucuya tanımlı tüm IP'ler otomatik algılanır (adaptörler farklı olabilir). Havuza tek tıkla ekleyin.": "All IPs defined on the server are auto-detected (adapters may differ). Add to the pool with one click.",
  "Warm-up açık → kademeli ısınma; kapalı → tam ağırlık (10)": "Warm-up on → gradual warm-up; off → full weight (10)",
  "Warm-up kapalı — tam ağırlık": "Warm-up off — full weight",
  "kademeli ısınır": "warms up gradually",
  "otomatik rotasyondan çıkar": "removed from auto rotation",
  "Çıkış IP'si (havuzdan)": "Outgoing IP (from pool)",
  "Önce yukarıda en az bir aktif IP ekleyip kaydedin.": "First add and save at least one active IP above.",
  "✓ IP havuzu kaydedildi, arayüze tanımlandı ve Postfix rotasyonu uygulandı.": "✓ IP pool saved, defined on the interface and Postfix rotation applied.",
  "✓ IP temiz — kara listede değil": "✓ IP is clean — not on any blocklist",
  "Türkçe": "English",
  "Diğer tüm domainler ağırlıklı rotasyonu kullanmaya devam eder.": "All other domains keep using weighted rotation.",
  "Geçersiz IPv4 adresi: {0}": "Invalid IPv4 address: {0}",
  "Giden mail trafiği için birden çok IP tanımlayın. Aktif IP'ler otomatik olarak sunucu arayüzüne": "Define multiple IPs for outgoing mail traffic. Active IPs are automatically added to the server interface",
  "Gönderen domaini": "Sender domain",
  "IP eklendiğinden bu yana gün · rotasyon ağırlığı": "Days since the IP was added · rotation weight",
  "Saatlik çıkış": "Hourly outgoing",
  "Seçili domainlerin gideni rotasyon yerine": "Instead of rotation, outgoing mail of the selected domains",
  "ağırlık {0}/10": "weight {0}/10",
  "ağırlıklı-rastgele gönderilir. Yeni IP'ler": "is sent weighted-randomly. New IPs",
  "bir havuz IP'sinden çıkar.": "go out from a single pool IP.",
  "tanımlı": "defined",
  "{0} aktif IP — ağırlıklı rotasyon etkin": "{0} active IPs — weighted rotation enabled",
  "Ağ adaptörü": "Network adapter",
  "Arayüz": "Interface",
  "Son 24 saat: {0} giden mail": "Last 24 hours: {0} outgoing mail",
  "mail": "mail",
  "✓ Kara liste kontrolü tamamlandı — tüm IP'ler temiz (hiçbir kara listede değil).": "✓ Blocklist check completed — all IPs are clean (not on any blocklist).",
  "{0} IP kara listede: {1}": "{0} IP(s) on blocklist: {1}",
  "Kaydedilemedi": "Failed to save",
  "\"{0}\" için seçilen IP aktif havuzda değil: {1}": "The IP selected for \"{0}\" is not in the active pool: {1}",
  "✓ Domaine özel IP eşlemeleri kaydedildi ve Postfix'e uygulandı.": "✓ Domain-specific IP mappings saved and applied to Postfix.",
  "kapalı": "off",
  "ağırlık": "weight",
  "tam": "full",
  "gün": "day",
  "Kirli": "Listed",
  "Son kontrol: {0}": "Last check: {0}",
  "kara liste: kontrol edilmedi": "blocklist: not checked",
  "· rotasyonda": "· in rotation",
  "· rotasyon dışı": "· out of rotation",
  "· rotasyon dışı (pasif)": "· out of rotation (inactive)",
  "eklenir ve her e-posta havuzdaki IP'lerden": "is added, and each email is sent from the pool's IPs",
  "DNSBL'e düşen IP": "An IP that lands on a DNSBL",
  "Sunucudaki IP Adresleri": "IP Addresses on the Server",
  "Sunucu IP'lerini yeniden tara": "Re-scan server IPs",
  "+ Havuza ekle": "+ Add to pool",
  "✓ havuzda": "✓ in pool",
  "Giden IP Adresleri": "Outgoing IP Addresses",
  "Aktif IP yok — varsayılan sunucu IP'si kullanılır": "No active IP — the default server IP is used",
  "Kontrol ediliyor…": "Checking…",
  "Kara Liste Kontrol Et": "Run Blocklist Check",
  "+ IP ekle": "+ Add IP",
  "Henüz IP eklenmedi. \"+ IP ekle\" ile başlayın.": "No IP added yet. Start with \"+ Add IP\".",
  "IP adresi (IPv4)": "IP address (IPv4)",
  "Etiket": "Label",
  "etiket": "label",
  "yok": "none",
  "Uygulanıyor…": "Applying…",
  "Kaydet ve Uygula": "Save and Apply",
  "sabit": "fixed",
  "+ Ekle": "+ Add",
  "Domaine özel IP tanımlı değil. \"+ Ekle\" ile başlayın.": "No domain-specific IP defined. Start with \"+ Add\".",
  "(havuzda değil)": "(not in pool)",
  "Dedicated IP'leri Kaydet": "Save Dedicated IPs",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Kara liste kontrolü": "Blocklist check",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (MIPH_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function gecerliIP(s: string): boolean {
  const m = IPV4.exec(s.trim())
  return !!m && m.slice(1).every(o => +o >= 0 && +o <= 255)
}

// SaatlikBar — son 24 saatlik giden mail mini bar grafiği (sparkline).
function SaatlikBar({ veri, max }: { veri: number[]; max: number }) {
  const dizi = veri.length ? veri : Array(24).fill(0)
  const toplam = dizi.reduce((a, b) => a + b, 0)
  return (
    <div className="w-full min-w-[90px]" title={cevirT(cevir("Son 24 saat: {0} giden mail"), toplam)}>
      <div className="flex items-end gap-px h-7">
        {dizi.map((c, i) => (
          <span key={i} className="flex-1 rounded-t-sm bg-brand-500/60 dark:bg-brand-400/60 min-h-[2px]"
            style={{ height: max > 0 ? `${Math.max(8, (c / max) * 100)}%` : '8%' }} title={`${c} ${cevir("mail")}`} />
        ))}
      </div>
      <div className="mt-0.5 text-[10px] text-slate-400 text-center tabular-nums">24s: {toplam}</div>
    </div>
  )
}

export default function MailIPHavuzuPage() {
  useTranslation() // dil re-render aboneligi
  const toast = useToast()
  const [havuz, setHavuz] = useState<IPKaydi[]>([])
  const [arayuz, setArayuz] = useState('')
  const [sunucuIP, setSunucuIP] = useState<SunucuAdres[]>([])
  const [dnsblKontrol, setDnsblKontrol] = useState(false)
  const [grafik, setGrafik] = useState<Record<string, number[]>>({})
  const [grafikMax, setGrafikMax] = useState(0)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [kaydediliyor, setKaydediliyor] = useState(false)
  // Hata/başarı artık sağ üst toast ile gösteriliyor; state yalnız akış için tutuluyor.
  const [, setHata] = useState<string | null>(null)
  const [, setBildirim] = useState<string | null>(null)

  // Dedicated (domaine özel IP) durumu.
  const [dedicated, setDedicated] = useState<Dedicated[]>([])
  const [aktifIP, setAktifIP] = useState<string[]>([])
  const [dedKaydediliyor, setDedKaydediliyor] = useState(false)
  const [, setDedHata] = useState<string | null>(null)
  const [, setDedBildirim] = useState<string | null>(null)

  function yukle() {
    api.get<Yanit>('/eklenti/mail/genel/ip-havuzu')
      .then(r => { setHavuz(r.data.havuz ?? []); setArayuz(r.data.arayuz ?? ''); setSunucuIP(r.data.sunucu_ipleri ?? []) })
      .catch(e => {
        const m = apiHata(e, cevir("IP havuzu yüklenemedi (mail eklentisi aktif mi?)"))
        setHata(m)
        toast.hata(cevir("İşlem başarısız"), m)
      })
      .finally(() => setYukleniyor(false))
  }
  function dedYukle() {
    api.get<DedYanit>('/eklenti/mail/genel/dedicated-ip')
      .then(r => { setDedicated(r.data.dedicated ?? []); setAktifIP(r.data.aktif_ip ?? []) })
      .catch(() => { /* havuz hatası zaten gösteriliyor */ })
  }
  function grafikYukle() {
    api.get<{ ipler: { ip: string; saatlik: number[] }[]; max: number }>('/eklenti/mail/genel/ip-havuzu/grafik')
      .then(r => {
        const m: Record<string, number[]> = {}
        ;(r.data.ipler ?? []).forEach(x => { m[x.ip] = x.saatlik })
        setGrafik(m); setGrafikMax(r.data.max ?? 0)
      })
      .catch(() => { /* grafik opsiyonel; hata gösterme */ })
  }
  useEffect(() => { yukle(); dedYukle(); grafikYukle() }, [])

  function satirGuncelle(i: number, yama: Partial<IPKaydi>) {
    setHavuz(h => h.map((k, j) => j === i ? { ...k, ...yama } : k))
  }
  function satirEkle(ip = '', etiket = '') {
    setHavuz(h => (ip && h.some(k => k.ip.trim() === ip)) ? h : [...h, { ip, aktif: true, etiket, warmup: true }])
  }

  // Manuel kara liste (DNSBL) kontrolü — sunucudaki her havuz IP'sini tarar, sonucu günceller.
  async function dnsblKontrolEt() {
    setDnsblKontrol(true); setHata(null); setBildirim(null)
    try {
      const r = await api.post<Yanit>('/eklenti/mail/genel/ip-havuzu/dnsbl-kontrol', {})
      setHavuz(r.data.havuz ?? [])
      const kirli = (r.data.havuz ?? []).filter(k => (k.dnsbl?.length ?? 0) > 0)
      const m = kirli.length === 0
        ? cevir("✓ Kara liste kontrolü tamamlandı — tüm IP'ler temiz (hiçbir kara listede değil).")
        : cevirT(cevir("{0} IP kara listede: {1}"), kirli.length, kirli.map(k => k.ip).join(', '))
      setBildirim(m)
      toast.basari(cevir("Kara liste kontrolü"), m)
    } catch (e) {
      const m = apiHata(e, cevir("Kara liste kontrolü yapılamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setDnsblKontrol(false) }
  }
  function satirSil(i: number) { setHavuz(h => h.filter((_, j) => j !== i)) }

  async function kaydet() {
    setHata(null); setBildirim(null)
    const temiz = havuz.map(k => ({ ...k, ip: k.ip.trim() })).filter(k => k.ip !== '')
    const gecersiz = temiz.find(k => !gecerliIP(k.ip))
    if (gecersiz) {
      const m = cevirT(cevir("Geçersiz IPv4 adresi: {0}"), gecersiz.ip)
      setHata(m)
      toast.hata(m)
      return
    }
    setKaydediliyor(true)
    try {
      const r = await api.put<Yanit>('/eklenti/mail/genel/ip-havuzu', {
        havuz: temiz.map(k => ({ ip: k.ip, aktif: k.aktif, etiket: k.etiket, warmup: k.warmup ?? true })),
      })
      setHavuz(r.data.havuz ?? [])
      setBildirim(cevir("✓ IP havuzu kaydedildi, arayüze tanımlandı ve Postfix rotasyonu uygulandı."))
      toast.basari(cevir("Kaydedildi"), cevir("✓ IP havuzu kaydedildi, arayüze tanımlandı ve Postfix rotasyonu uygulandı."))
      dedYukle() // aktif IP listesi değişmiş olabilir
    } catch (e) {
      const m = apiHata(e, cevir("Kaydedilemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setKaydediliyor(false) }
  }

  function dedEkle() {
    setDedicated(d => [...d, { domain: '', ip: aktifIP[0] ?? '' }])
  }
  function dedGuncelle(i: number, yama: Partial<Dedicated>) {
    setDedicated(d => d.map((x, j) => j === i ? { ...x, ...yama } : x))
  }
  function dedSil(i: number) { setDedicated(d => d.filter((_, j) => j !== i)) }

  async function dedKaydet() {
    setDedHata(null); setDedBildirim(null)
    const temiz = dedicated
      .map(d => ({ domain: d.domain.trim().toLowerCase(), ip: d.ip.trim() }))
      .filter(d => d.domain !== '')
    const gecersizIP = temiz.find(d => !aktifIP.includes(d.ip))
    if (gecersizIP) {
      const m = cevirT(cevir("\"{0}\" için seçilen IP aktif havuzda değil: {1}"), gecersizIP.domain, gecersizIP.ip)
      setDedHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
      return
    }
    setDedKaydediliyor(true)
    try {
      const r = await api.put<{ dedicated: Dedicated[] }>('/eklenti/mail/genel/dedicated-ip', { dedicated: temiz })
      setDedicated(r.data.dedicated ?? [])
      setDedBildirim(cevir("✓ Domaine özel IP eşlemeleri kaydedildi ve Postfix'e uygulandı."))
      toast.basari(cevir("Kaydedildi"), cevir("✓ Domaine özel IP eşlemeleri kaydedildi ve Postfix'e uygulandı."))
    } catch (e) {
      const m = apiHata(e, cevir("Kaydedilemedi"))
      setDedHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setDedKaydediliyor(false) }
  }

  const kart = 'bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 shadow-xs'
  const inp = 'rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400'
  const aktifSayisi = havuz.filter(k => k.aktif && k.ip.trim() !== '').length

  // Bir IP satırının warm-up + DNSBL + rotasyon durum rozetleri.
  function durumSatiri(k: IPKaydi) {
    const listeli = (k.dnsbl?.length ?? 0) > 0
    const agirlik = k.agirlik ?? (k.warmup === false ? 10 : 1)
    return (
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-500 dark:text-slate-400 pl-1">
        {/* warm-up */}
        {k.warmup === false
          ? <span title={cevir("Warm-up kapalı — tam ağırlık")}>warm-up: {cevir("kapalı")} · {cevir("ağırlık")} {agirlik}/10 ({cevir("tam")})</span>
          : <span title={cevir("IP eklendiğinden bu yana gün · rotasyon ağırlığı")}>
              warm-up: {cevir("gün")} {k.warmup_gun ?? 0} · {cevir("ağırlık")} {agirlik}/10
            </span>}
        {/* warm-up mini bar */}
        <span className="inline-flex h-1.5 w-16 rounded-full bg-slate-200 dark:bg-dark-600 overflow-hidden align-middle" title={cevirT(cevir("ağırlık {0}/10"), agirlik)}>
          <span className="h-full bg-brand-500" style={{ width: `${Math.round((agirlik / 10) * 100)}%` }} />
        </span>
        {/* kara liste (DNSBL) durumu — temizse "IP temiz", kirliyse yalnız kirli servisler */}
        {listeli
          ? <span className="text-red-600 dark:text-red-400 font-medium" title={cevirT(cevir("Son kontrol: {0}"), k.son_kontrol || '—')}>
              {cevir("Kirli")}: {k.dnsbl!.join(', ')}
            </span>
          : k.son_kontrol
            ? <span className="text-emerald-600 dark:text-emerald-400" title={cevirT(cevir("Son kontrol: {0}"), k.son_kontrol)}>{cevir("✓ IP temiz — kara listede değil")}</span>
            : <span className="text-slate-400" title={cevir("Henüz kara liste kontrolü yapılmadı")}>{cevir("kara liste: kontrol edilmedi")}</span>}
        {/* rotasyon durumu (ayrı gösterge) */}
        {k.rotasyonda
          ? <span className="text-emerald-600 dark:text-emerald-400">{cevir("· rotasyonda")}</span>
          : listeli
            ? <span className="text-red-500">{cevir("· rotasyon dışı")}</span>
            : <span className="text-slate-400">{cevir("· rotasyon dışı (pasif)")}</span>}
      </div>
    )
  }

  return (
    <div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">
        {cevir("Giden mail trafiği için birden çok IP tanımlayın. Aktif IP'ler otomatik olarak sunucu arayüzüne")}
        {arayuz && <span className="font-mono"> ({arayuz})</span>} {cevir("eklenir ve her e-posta havuzdaki IP'lerden")}
        {cevir("ağırlıklı-rastgele gönderilir. Yeni IP'ler")} <span className="font-medium">{cevir("kademeli ısınır")}</span> (warm-up),
        {cevir("DNSBL'e düşen IP")} <span className="font-medium">{cevir("otomatik rotasyondan çıkar")}</span>.
      </p>

      {yukleniyor ? <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div> : (
        <div className="space-y-5">
          {/* Sunucudaki mevcut IP'ler — dinamik, tüm adaptörler otomatik algılanır */}
          <section className={kart}>
            <div className="flex items-center justify-between mb-3">
              <div>
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Sunucudaki IP Adresleri")}</h2>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">{cevir("Sunucuya tanımlı tüm IP'ler otomatik algılanır (adaptörler farklı olabilir). Havuza tek tıkla ekleyin.")}</p>
              </div>
              <Button onClick={() => { setYukleniyor(true); yukle() }} variant="outlined" className="text-xs px-2.5 py-1.5" title={cevir("Sunucu IP'lerini yeniden tara")}><span className="inline-flex items-center gap-1.5"><Ikon d={I.yenile} /> {cevir("Yenile")}</span></Button>
            </div>
            {sunucuIP.length === 0 ? (
              <div className="py-4 text-center text-sm text-slate-400">{cevir("IP algılanamadı.")}</div>
            ) : (
              <div className="flex flex-wrap gap-2">
                {sunucuIP.map(s => (
                  <div key={s.ip} className="inline-flex items-center gap-2 rounded-lg border border-slate-200 dark:border-dark-600 bg-slate-50 dark:bg-dark-800/40 pl-3 pr-1.5 py-1.5">
                    <span className="font-mono text-sm text-slate-700 dark:text-slate-200">{s.ip}</span>
                    <span className="text-[11px] px-1.5 py-0.5 rounded bg-slate-200 dark:bg-dark-600 text-slate-600 dark:text-slate-300 font-mono" title={cevir("Ağ adaptörü")}>{s.arayuz}</span>
                    {s.havuzda
                      ? <span className="text-[11px] text-emerald-600 dark:text-emerald-400 px-1.5">{cevir("✓ havuzda")}</span>
                      : <Button onClick={() => satirEkle(s.ip)} color="primary" variant="outlined" className="text-[11px] px-2 py-0.5">{cevir("+ Havuza ekle")}</Button>}
                  </div>
                ))}
              </div>
            )}
          </section>

          <section className={kart}>
            <div className="flex items-center justify-between mb-4">
              <div>
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Giden IP Adresleri")}</h2>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                  {aktifSayisi > 0 ? cevirT(cevir("{0} aktif IP — ağırlıklı rotasyon etkin"), aktifSayisi) : cevir("Aktif IP yok — varsayılan sunucu IP'si kullanılır")}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <Button onClick={dnsblKontrolEt} disabled={dnsblKontrol || havuz.length === 0}
                  variant="outlined" className="text-sm px-3 py-1.5"
                  title={cevir("Havuzdaki tüm IP'leri kara listelerde (DNSBL) tara")}>
                  {dnsblKontrol ? cevir("Kontrol ediliyor…") : <span className="inline-flex items-center gap-1.5"><Ikon d={I.kalkan} /> {cevir("Kara Liste Kontrol Et")}</span>}
                </Button>
                <Button onClick={() => satirEkle()} color="primary" variant="outlined" className="text-sm px-3 py-1.5">{cevir("+ IP ekle")}</Button>
              </div>
            </div>

            {havuz.length === 0 ? (
              <div className="py-8 text-center text-sm text-slate-400">{cevir("Henüz IP eklenmedi. \"+ IP ekle\" ile başlayın.")}</div>
            ) : (
              <div className="space-y-3">
                <div className="hidden sm:grid grid-cols-[1.1fr_110px_1.4fr_auto_auto_auto_auto] gap-3 px-1 text-xs font-medium text-slate-400">
                  <span>{cevir("IP adresi (IPv4)")}</span><span>{cevir("Etiket")}</span><span className="text-center">{cevir("Saatlik çıkış")}</span><span>{cevir("Arayüz")}</span><span>Warm-up</span><span>{cevir("Aktif")}</span><span></span>
                </div>
                {havuz.map((k, i) => (
                  <div key={i} className="border-b border-slate-100 dark:border-dark-600/60 pb-3 last:border-0 last:pb-0">
                    <div className="grid grid-cols-1 sm:grid-cols-[1.1fr_110px_1.4fr_auto_auto_auto_auto] gap-3 items-center">
                      <input value={k.ip} onChange={e => satirGuncelle(i, { ip: e.target.value })} placeholder="1.2.3.4"
                        className={inp + ' font-mono' + (k.ip.trim() !== '' && !gecerliIP(k.ip) ? ' border-red-400 focus:ring-red-500/40' : '')} />
                      <input value={k.etiket} onChange={e => satirGuncelle(i, { etiket: e.target.value })} placeholder={cevir("etiket")} className={inp} />
                      <SaatlikBar veri={grafik[k.ip.trim()] ?? []} max={grafikMax} />
                      <span className="text-xs text-center whitespace-nowrap">
                        {k.arayuzde
                          ? <span className="text-emerald-600 dark:text-emerald-400 font-mono" title={cevir("Bu adaptörde tanımlı")}>✓ {k.arayuz || cevir("tanımlı")}</span>
                          : <span className="text-slate-400" title={cevir("Henüz uygulanmadı")}>✗ {cevir("yok")}</span>}
                      </span>
                      <label className="inline-flex items-center gap-1.5 cursor-pointer justify-self-start sm:justify-self-center" title={cevir("Warm-up açık → kademeli ısınma; kapalı → tam ağırlık (10)")}>
                        <input type="checkbox" checked={k.warmup ?? true} onChange={e => satirGuncelle(i, { warmup: e.target.checked })}
                          className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
                        <span className="text-xs text-slate-500 sm:hidden">Warm-up</span>
                      </label>
                      <label className="inline-flex items-center gap-1.5 cursor-pointer justify-self-start sm:justify-self-center">
                        <input type="checkbox" checked={k.aktif} onChange={e => satirGuncelle(i, { aktif: e.target.checked })}
                          className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
                        <span className="text-xs text-slate-500 sm:hidden">{cevir("Aktif")}</span>
                      </label>
                      <Button onClick={() => satirSil(i)} color="error" variant="flat" className="text-sm justify-self-start sm:justify-self-center px-2" title={cevir("Sil")}><Ikon d={I.kapat} /></Button>
                    </div>
                    {k.ip.trim() !== '' && durumSatiri(k)}
                  </div>
                ))}
              </div>
            )}
          </section>

          <div className="flex justify-end">
            <Button onClick={kaydet} disabled={kaydediliyor} color="primary"
              className="text-sm px-6 py-2.5">
              {kaydediliyor ? cevir("Uygulanıyor…") : cevir("Kaydet ve Uygula")}
            </Button>
          </div>

          {/* ── Domaine Özel IP (dedicated) ── */}
          <section className={kart}>
            <div className="flex items-center justify-between mb-4">
              <div>
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Domaine Özel IP (Dedicated)")}</h2>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                  {cevir("Seçili domainlerin gideni rotasyon yerine")} <span className="font-medium">{cevir("sabit")}</span> {cevir("bir havuz IP'sinden çıkar.")}
                  {cevir("Diğer tüm domainler ağırlıklı rotasyonu kullanmaya devam eder.")}
                </p>
              </div>
              <Button onClick={dedEkle} disabled={aktifIP.length === 0} color="primary" variant="outlined"
                className="text-sm px-3 py-1.5">{cevir("+ Ekle")}</Button>
            </div>

            {aktifIP.length === 0 ? (
              <div className="py-6 text-center text-sm text-slate-400">{cevir("Önce yukarıda en az bir aktif IP ekleyip kaydedin.")}</div>
            ) : dedicated.length === 0 ? (
              <div className="py-6 text-center text-sm text-slate-400">{cevir("Domaine özel IP tanımlı değil. \"+ Ekle\" ile başlayın.")}</div>
            ) : (
              <div className="space-y-2.5">
                <div className="hidden sm:grid grid-cols-[1fr_1fr_auto] gap-3 px-1 text-xs font-medium text-slate-400">
                  <span>{cevir("Gönderen domaini")}</span><span>{cevir("Çıkış IP'si (havuzdan)")}</span><span></span>
                </div>
                {dedicated.map((d, i) => (
                  <div key={i} className="grid grid-cols-1 sm:grid-cols-[1fr_1fr_auto] gap-3 items-center">
                    <input value={d.domain} onChange={e => dedGuncelle(i, { domain: e.target.value })} placeholder={cevir("firma.com")} className={inp} />
                    <select value={d.ip} onChange={e => dedGuncelle(i, { ip: e.target.value })} className={inp + ' font-mono'}>
                      {!aktifIP.includes(d.ip) && d.ip && <option value={d.ip}>{d.ip} {cevir("(havuzda değil)")}</option>}
                      {aktifIP.map(ip => <option key={ip} value={ip}>{ip}</option>)}
                    </select>
                    <Button onClick={() => dedSil(i)} color="error" variant="flat" className="text-sm justify-self-start sm:justify-self-center px-2" title={cevir("Sil")}><Ikon d={I.kapat} /></Button>
                  </div>
                ))}
              </div>
            )}

            {aktifIP.length > 0 && (
              <div className="flex justify-end mt-4">
                <Button onClick={dedKaydet} disabled={dedKaydediliyor} color="primary"
                  className="text-sm px-5 py-2">
                  {dedKaydediliyor ? cevir("Uygulanıyor…") : cevir("Dedicated IP'leri Kaydet")}
                </Button>
              </div>
            )}
          </section>
        </div>
      )}
    </div>
  )
}
