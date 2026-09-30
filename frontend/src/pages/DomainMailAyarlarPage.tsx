import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import { Button, Badge, butonSinifi } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'
import type { Domain } from '@/components/domainTipi'

type Kontrol = { ad: string; durum: 'ok' | 'uyari' | 'hata' | 'bilgi'; mesaj: string }
type Kategori = { ad: string; kontroller: Kontrol[] }
type Monitor = {
  domain: string; ip: string; olusturma: string
  ozet: { ok: number; uyari: number; hata: number; bilgi?: number }
  genel_durum: 'saglikli' | 'uyarili' | 'sorunlu'
  kategoriler: Kategori[]
}
type Baglanti = {
  domain: string
  imap: { host: string; port: number; guvenlik: string }
  pop3: { host: string; port: number; guvenlik: string }
  smtp: { host: string; port: number; guvenlik: string }
  webmail: string; autoconfig: string; autodiscover: string; kullanici_adi: string
}

// Kontrol durumu → Tailux Badge rengi.
const DURUM_RENK: Record<string, 'success' | 'warning' | 'error' | 'neutral'> = {
  ok: 'success',
  uyari: 'warning',
  hata: 'error',
  bilgi: 'neutral',
}
const GENEL: Record<string, { renk: string; etiket: string }> = {
  saglikli: { renk: 'text-emerald-600 dark:text-emerald-400', etiket: 'Sağlıklı' },
  uyarili: { renk: 'text-amber-600 dark:text-amber-400', etiket: 'Uyarılı' },
  sorunlu: { renk: 'text-red-600 dark:text-red-400', etiket: 'Sorunlu' },
}


const MAILAYAR_EN: Record<string, string> = {
  "İşlem başarısız": "Operation failed",
  "Analiz başarısız — mail sunucusuna ulaşılamadı": "Analysis failed — could not reach the mail server",
  "Henüz analiz yapılmadı.": "No analysis yet.",
  "Kopyalamak için tıklayın": "Click to copy",
  "Mail Ayarları": "Mail Settings",
  "Mail Sunucu Sağlığı": "Mail Server Health",
  "Mail bağlantı bilgileri alınamadı": "Failed to get mail connection info",
  "Mail istemcinize (Outlook, Thunderbird, telefon) elle kurulum için. Kullanıcı adı:": "For manual setup in your mail client (Outlook, Thunderbird, phone). Username:",
  "Postanın spam'e düşmemesi için domain DNS'inize bu kayıtları ekleyin (SPF · DKIM · DMARC). Değere tıklayıp kopyalayın.": "Add these records to your domain DNS so mail doesn't go to spam (SPF · DKIM · DMARC). Click a value to copy it.",
  "Sağlıklı": "Healthy",
  "Uyarılı": "Warning",
  "Önerilen DNS Kayıtları": "Recommended DNS Records",
  "kopyalandı": "copied",
  "Türkçe": "English",
  "Ad": "Name",
  "Bağlantı Bilgileri": "Connection Details",
  "Değer": "Value",
  "Domain yüklenemedi": "Failed to load domain",
  "Domainler": "Domains",
  "Webmail'i Aç": "Open Webmail",
  "ile başlayın.": "to start.",
  "Sorunlu": "Problem",
  "Anasayfa": "Home",
  "Port": "Port",
  "kopyala": "copy",
  "posta hizmeti — sağlık analizi ve bağlantı bilgileri.": "mail service — health analysis and connection details.",
  "Servisler, portlar, DNS, TLS, DKIM, itibar, kara liste, teslimat — hepsini kontrol eder.": "Services, ports, DNS, TLS, DKIM, reputation, blocklist, delivery — checks all of them.",
  "Analiz ediliyor…": "Analyzing…",
  "Analiz Et": "Analyze",
  "Uyarı": "Warning",
  "Hata": "Error",
  "Bilgi": "Info",
  "Gelen (IMAP)": "Incoming (IMAP)",
  "Gelen (POP3)": "Incoming (POP3)",
  "Giden (SMTP)": "Outgoing (SMTP)",
  "Outlook & Thunderbird otomatik kurulum aktif — istemciye sadece e-posta + parola girin": "Outlook & Thunderbird auto-setup active — just enter email + password in the client",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (MAILAYAR_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainMailAyarlarPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const toast = useToast()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [baglanti, setBaglanti] = useState<Baglanti | null>(null)
  const [monitor, setMonitor] = useState<Monitor | null>(null)
  const [analizEdiliyor, setAnalizEdiliyor] = useState(false)
  const [, setHata] = useState<string | null>(null)

  useEffect(() => {
    let iptal = false
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (iptal) return; const m = apiHata(e, cevir("Domain yüklenemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
    return () => { iptal = true }
  }, [id])

  useEffect(() => {
    let iptal = false
    if (!domain) return
    api.get<Baglanti>(`/eklenti/mail/baglanti/${domain.alan_adi}`)
      .then(r => { if (iptal) return; setBaglanti(r.data) }).catch(e => { if (!iptal) hataYakala(cevir("Mail bağlantı bilgileri alınamadı"))(e) })
    return () => { iptal = true }
  }, [domain])

  async function analizEt() {
    if (!domain) return
    setAnalizEdiliyor(true); setHata(null)
    try {
      const { data } = await api.get<Monitor>(`/eklenti/mail/monitor?domain=${encodeURIComponent(domain.alan_adi)}`)
      setMonitor(data)
    } catch (e) { const m = apiHata(e, cevir("Analiz başarısız — mail sunucusuna ulaşılamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
    finally { setAnalizEdiliyor(false) }
  }

  if (!domain) return (
    <div>
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
    </div>
  )

  const g = monitor ? GENEL[monitor.genel_durum] : null

  return (
    <div>
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain.alan_adi, href: `/abonelikler/${id}` },
        { etiket: cevir("Mail Ayarları") },
      ]} />
      <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">{cevir("Mail Ayarları")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">{domain.alan_adi} {cevir("posta hizmeti — sağlık analizi ve bağlantı bilgileri.")}</p>

      {/* Sağlık Monitörü */}
      <section className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-5">
        <div className="flex items-center justify-between gap-3 mb-4 flex-wrap">
          <div>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Mail Sunucu Sağlığı")}</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400">{cevir("Servisler, portlar, DNS, TLS, DKIM, itibar, kara liste, teslimat — hepsini kontrol eder.")}</p>
          </div>
          <Button
            onClick={analizEt}
            yukleniyor={analizEdiliyor}
            color="primary"
            ikon={I.oynat}>
            {analizEdiliyor ? cevir("Analiz ediliyor…") : cevir("Analiz Et")}
          </Button>
        </div>

        {!monitor && !analizEdiliyor && (
          <div className="py-10 text-center">
            <svg className="w-12 h-12 mx-auto text-slate-300 dark:text-slate-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.3}><path strokeLinecap="round" strokeLinejoin="round" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" /></svg>
            <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Henüz analiz yapılmadı.")} <b>{cevir("Analiz Et")}</b> {cevir("ile başlayın.")}</p>
          </div>
        )}

        {monitor && (
          <>
            <div className="flex items-center gap-4 mb-4 flex-wrap">
              <div className={`inline-flex items-center gap-2 text-lg font-bold ${g?.renk}`}><span className="inline-block size-2 rounded-full bg-current" aria-hidden />{g ? cevir(g.etiket) : ''}</div>
              <div className="flex items-center gap-2 text-xs">
                <Badge variant="soft" color={DURUM_RENK.ok}>{monitor.ozet.ok} OK</Badge>
                <Badge variant="soft" color={DURUM_RENK.uyari}>{monitor.ozet.uyari} {cevir("Uyarı")}</Badge>
                <Badge variant="soft" color={DURUM_RENK.hata}>{monitor.ozet.hata} {cevir("Hata")}</Badge>
                {!!monitor.ozet.bilgi && <Badge variant="soft" color={DURUM_RENK.bilgi}>{monitor.ozet.bilgi} {cevir("Bilgi")}</Badge>}
              </div>
              <span className="text-xs text-slate-400 ml-auto">IP {monitor.ip} · {monitor.olusturma}</span>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {monitor.kategoriler.map(kat => (
                <div key={kat.ad} className="border border-slate-200 dark:border-dark-600 rounded-lg p-3">
                  <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 mb-2">{kat.ad}</h3>
                  <ul className="space-y-1.5">
                    {kat.kontroller.map((k, i) => (
                      <li key={i} className="flex items-start gap-2 text-sm">
                        <Badge variant="soft" color={DURUM_RENK[k.durum] ?? 'neutral'} className="shrink-0 mt-0.5 text-[10px] font-bold px-1.5 py-0.5 uppercase">{k.durum}</Badge>
                        <span className="min-w-0">
                          <span className="text-slate-800 dark:text-slate-200 font-medium">{k.ad}</span>
                          <span className="text-slate-500 dark:text-slate-400"> — {k.mesaj}</span>
                        </span>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </>
        )}
      </section>

      {/* Bağlantı Bilgileri */}
      {baglanti && (
        <section className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5">
          <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Bağlantı Bilgileri")}</h2>
          <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">{cevir("Mail istemcinize (Outlook, Thunderbird, telefon) elle kurulum için. Kullanıcı adı:")} <b>{baglanti.kullanici_adi}</b>.</p>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-4">
            <SunucuKart baslik={cevir("Gelen (IMAP)")} host={baglanti.imap.host} port={baglanti.imap.port} g={baglanti.imap.guvenlik} />
            <SunucuKart baslik={cevir("Gelen (POP3)")} host={baglanti.pop3.host} port={baglanti.pop3.port} g={baglanti.pop3.guvenlik} />
            <SunucuKart baslik={cevir("Giden (SMTP)")} host={baglanti.smtp.host} port={baglanti.smtp.port} g={baglanti.smtp.guvenlik} />
          </div>
          <div className="flex flex-wrap gap-2 text-sm">
            <a href={baglanti.webmail} target="_blank" rel="noreferrer" className={butonSinifi({ variant: 'outlined' })}>
              {cevir("Webmail'i Aç")}
              <Ikon d={I.harici} className="size-4 shrink-0 ms-2 -me-1" />
            </a>
            <Badge variant="soft" color="info" className="gap-1.5 px-3 py-1.5 rounded-lg leading-snug text-left">
              <Ikon d={I.onay} className="size-3.5 shrink-0" />
              {cevir("Outlook & Thunderbird otomatik kurulum aktif — istemciye sadece e-posta + parola girin")}
            </Badge>
          </div>
        </section>
      )}

      {/* DNS Kayıtları */}
      <DnsKayitlariKart domain={domain.alan_adi} />
    </div>
  )
}

type DnsKaydi = { ad: string; tip: string; deger: string; aciklama: string }

function DnsKayitlariKart({ domain }: { domain: string }) {
  const [kayitlar, setKayitlar] = useState<DnsKaydi[] | null>(null)
  const [kopyalanan, setKopyalanan] = useState<string | null>(null)
  useEffect(() => {
    let iptal = false
    api.get<{ kayitlar: DnsKaydi[] }>(`/eklenti/mail/dnskayitlari/${domain}`)
      .then(r => { if (iptal) return; setKayitlar(r.data.kayitlar || []) }).catch(() => { if (!iptal) setKayitlar([]) })
    return () => { iptal = true }
  }, [domain])
  if (!kayitlar || kayitlar.length === 0) return null
  return (
    <section className="mt-5 bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5">
      <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Önerilen DNS Kayıtları")}</h2>
      <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">{cevir("Postanın spam'e düşmemesi için domain DNS'inize bu kayıtları ekleyin (SPF · DKIM · DMARC). Değere tıklayıp kopyalayın.")}</p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm min-w-[520px]">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-slate-400 dark:text-slate-500 border-b border-slate-100 dark:border-dark-600">
              <th className="py-2 pr-3 font-medium">{cevir("Ad")}</th>
              <th className="py-2 pr-3 font-medium">Tip</th>
              <th className="py-2 pr-3 font-medium">{cevir("Değer")}</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-50 dark:divide-dark-600/50">
            {kayitlar.map((r, i) => (
              <tr key={i} className="align-top">
                <td className="py-2.5 pr-3 font-mono text-slate-700 dark:text-slate-300 whitespace-nowrap">{r.ad}</td>
                <td className="py-2.5 pr-3"><Badge variant="soft" className="text-[11px] px-1.5 py-0.5">{r.tip}</Badge></td>
                <td className="py-2.5">
                  <button onClick={() => { navigator.clipboard?.writeText(r.deger); setKopyalanan(r.deger); setTimeout(() => setKopyalanan(null), 1500) }}
                    title={cevir("Kopyalamak için tıklayın")}
                    className="group text-left font-mono text-xs text-slate-700 dark:text-slate-200 bg-slate-50 dark:bg-dark-800 hover:bg-brand-50 dark:hover:bg-brand-900/20 rounded px-2 py-1 break-all w-full transition-colors">
                    {r.deger}
                    <span className="ml-1.5 inline-flex items-center gap-0.5 align-middle text-[10px] font-sans text-brand-500 opacity-0 group-hover:opacity-100">{kopyalanan === r.deger ? <><Ikon d={I.onay} className="size-3 shrink-0" />{cevir("kopyalandı")}</> : cevir("kopyala")}</span>
                  </button>
                  <div className="text-[11px] text-slate-400 dark:text-slate-500 mt-1">{r.aciklama}</div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function SunucuKart({ baslik, host, port, g }: { baslik: string; host: string; port: number; g: string }) {
  return (
    <div className="border border-slate-200 dark:border-dark-600 rounded-lg p-3 text-sm">
      <div className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 mb-1.5">{baslik}</div>
      <div className="font-mono text-slate-800 dark:text-slate-200">{host}</div>
      <div className="text-slate-500 dark:text-slate-400 text-xs mt-0.5">{cevir("Port")} {port} · {g}</div>
    </div>
  )
}
