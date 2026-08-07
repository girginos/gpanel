import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import type { Domain } from '@/components/DomainList'

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

const DURUM_RENK: Record<string, string> = {
  ok: 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300',
  uyari: 'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300',
  hata: 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300',
  bilgi: 'bg-slate-100 dark:bg-slate-700/50 text-slate-600 dark:text-slate-300',
}
const GENEL: Record<string, { renk: string; etiket: string }> = {
  saglikli: { renk: 'text-emerald-600 dark:text-emerald-400', etiket: 'Sağlıklı' },
  uyarili: { renk: 'text-amber-600 dark:text-amber-400', etiket: 'Uyarılı' },
  sorunlu: { renk: 'text-red-600 dark:text-red-400', etiket: 'Sorunlu' },
}

export default function DomainMailAyarlarPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [baglanti, setBaglanti] = useState<Baglanti | null>(null)
  const [monitor, setMonitor] = useState<Monitor | null>(null)
  const [analizEdiliyor, setAnalizEdiliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e, 'Domain yüklenemedi')))
  }, [id])

  useEffect(() => {
    if (!domain) return
    api.get<Baglanti>(`/eklenti/mail/baglanti/${domain.alan_adi}`)
      .then(r => setBaglanti(r.data)).catch(hataYakala('Mail bağlantı bilgileri alınamadı'))
  }, [domain])

  async function analizEt() {
    if (!domain) return
    setAnalizEdiliyor(true); setHata(null)
    try {
      const { data } = await api.get<Monitor>(`/eklenti/mail/monitor?domain=${encodeURIComponent(domain.alan_adi)}`)
      setMonitor(data)
    } catch (e) { setHata(apiHata(e, 'Analiz başarısız — mail sunucusuna ulaşılamadı')) }
    finally { setAnalizEdiliyor(false) }
  }

  if (!domain) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div>
    </div>
  )

  const g = monitor ? GENEL[monitor.genel_durum] : null

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-7xl">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain.alan_adi, href: `/abonelikler/${id}` },
        { etiket: 'Mail Ayarları' },
      ]} />
      <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">Mail Ayarları</h1>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">{domain.alan_adi} posta hizmeti — sağlık analizi ve bağlantı bilgileri.</p>

      {hata && <div className="mb-4 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {/* Sağlık Monitörü */}
      <section className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5">
        <div className="flex items-center justify-between gap-3 mb-4 flex-wrap">
          <div>
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Mail Sunucu Sağlığı</h2>
            <p className="text-xs text-slate-500 dark:text-slate-400">Servisler, portlar, DNS, TLS, DKIM, itibar, kara liste, teslimat — hepsini kontrol eder.</p>
          </div>
          <button
            onClick={analizEt}
            disabled={analizEdiliyor}
            className="inline-flex items-center gap-2 bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-4 py-2 rounded-lg disabled:opacity-60 transition">
            {analizEdiliyor ? (
              <><svg className="w-4 h-4 animate-spin" fill="none" viewBox="0 0 24 24"><circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" /><path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" /></svg>Analiz ediliyor…</>
            ) : (
              <><svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>Analiz Et</>
            )}
          </button>
        </div>

        {!monitor && !analizEdiliyor && (
          <div className="py-10 text-center">
            <svg className="w-12 h-12 mx-auto text-slate-300 dark:text-slate-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.3}><path strokeLinecap="round" strokeLinejoin="round" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" /></svg>
            <p className="text-sm text-slate-500 dark:text-slate-400">Henüz analiz yapılmadı. <b>Analiz Et</b> ile başlayın.</p>
          </div>
        )}

        {monitor && (
          <>
            <div className="flex items-center gap-4 mb-4 flex-wrap">
              <div className={`text-lg font-bold ${g?.renk}`}>● {g?.etiket}</div>
              <div className="flex items-center gap-2 text-xs">
                <span className={`px-2 py-0.5 rounded ${DURUM_RENK.ok}`}>{monitor.ozet.ok} OK</span>
                <span className={`px-2 py-0.5 rounded ${DURUM_RENK.uyari}`}>{monitor.ozet.uyari} Uyarı</span>
                <span className={`px-2 py-0.5 rounded ${DURUM_RENK.hata}`}>{monitor.ozet.hata} Hata</span>
                {!!monitor.ozet.bilgi && <span className={`px-2 py-0.5 rounded ${DURUM_RENK.bilgi}`}>{monitor.ozet.bilgi} Bilgi</span>}
              </div>
              <span className="text-xs text-slate-400 ml-auto">IP {monitor.ip} · {monitor.olusturma}</span>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {monitor.kategoriler.map(kat => (
                <div key={kat.ad} className="border border-slate-200 dark:border-slate-700 rounded-xl p-3">
                  <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 mb-2">{kat.ad}</h3>
                  <ul className="space-y-1.5">
                    {kat.kontroller.map((k, i) => (
                      <li key={i} className="flex items-start gap-2 text-sm">
                        <span className={`shrink-0 mt-0.5 text-[10px] font-bold px-1.5 py-0.5 rounded uppercase ${DURUM_RENK[k.durum]}`}>{k.durum}</span>
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
        <section className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
          <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Bağlantı Bilgileri</h2>
          <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">Mail istemcinize (Outlook, Thunderbird, telefon) elle kurulum için. Kullanıcı adı: <b>{baglanti.kullanici_adi}</b>.</p>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-4">
            <SunucuKart baslik="Gelen (IMAP)" host={baglanti.imap.host} port={baglanti.imap.port} g={baglanti.imap.guvenlik} />
            <SunucuKart baslik="Gelen (POP3)" host={baglanti.pop3.host} port={baglanti.pop3.port} g={baglanti.pop3.guvenlik} />
            <SunucuKart baslik="Giden (SMTP)" host={baglanti.smtp.host} port={baglanti.smtp.port} g={baglanti.smtp.guvenlik} />
          </div>
          <div className="flex flex-wrap gap-2 text-sm">
            <a href={baglanti.webmail} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg border border-slate-200 dark:border-slate-700 hover:bg-slate-50 dark:hover:bg-slate-700/50 text-slate-700 dark:text-slate-200">
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M3 8l9 6 9-6m-9 6V4" /></svg>
              Webmail'i Aç
            </a>
            <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-sky-50 dark:bg-sky-900/20 text-sky-700 dark:text-sky-300 text-xs">
              ✓ Outlook & Thunderbird otomatik kurulum aktif — istemciye sadece e-posta + parola girin
            </span>
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
    api.get<{ kayitlar: DnsKaydi[] }>(`/eklenti/mail/dnskayitlari/${domain}`)
      .then(r => setKayitlar(r.data.kayitlar || [])).catch(() => setKayitlar([]))
  }, [domain])
  if (!kayitlar || kayitlar.length === 0) return null
  return (
    <section className="mt-5 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
      <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Önerilen DNS Kayıtları</h2>
      <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">Postanın spam'e düşmemesi için domain DNS'inize bu kayıtları ekleyin (SPF · DKIM · DMARC). Değere tıklayıp kopyalayın.</p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm min-w-[520px]">
          <thead>
            <tr className="text-left text-xs uppercase tracking-wider text-slate-400 dark:text-slate-500 border-b border-slate-100 dark:border-slate-700">
              <th className="py-2 pr-3 font-medium">Ad</th>
              <th className="py-2 pr-3 font-medium">Tip</th>
              <th className="py-2 pr-3 font-medium">Değer</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-50 dark:divide-slate-700/50">
            {kayitlar.map((r, i) => (
              <tr key={i} className="align-top">
                <td className="py-2.5 pr-3 font-mono text-slate-700 dark:text-slate-300 whitespace-nowrap">{r.ad}</td>
                <td className="py-2.5 pr-3"><span className="text-[11px] font-medium px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300">{r.tip}</span></td>
                <td className="py-2.5">
                  <button onClick={() => { navigator.clipboard?.writeText(r.deger); setKopyalanan(r.deger); setTimeout(() => setKopyalanan(null), 1500) }}
                    title="Kopyalamak için tıklayın"
                    className="group text-left font-mono text-xs text-slate-700 dark:text-slate-200 bg-slate-50 dark:bg-slate-900 hover:bg-brand-50 dark:hover:bg-brand-900/20 rounded px-2 py-1 break-all w-full transition-colors">
                    {r.deger}
                    <span className="ml-1.5 text-[10px] font-sans text-brand-500 opacity-0 group-hover:opacity-100">{kopyalanan === r.deger ? '✓ kopyalandı' : 'kopyala'}</span>
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
    <div className="border border-slate-200 dark:border-slate-700 rounded-xl p-3 text-sm">
      <div className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400 mb-1.5">{baslik}</div>
      <div className="font-mono text-slate-800 dark:text-slate-200">{host}</div>
      <div className="text-slate-500 dark:text-slate-400 text-xs mt-0.5">Port {port} · {g}</div>
    </div>
  )
}
