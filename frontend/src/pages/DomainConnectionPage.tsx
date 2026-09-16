import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'


const CONN_EN: Record<string, string> = {
  "İşlem başarısız": "Operation failed",
  "Değer üstüne tıkla → otomatik kopyalanır": "Click a value → it's copied automatically",
  "FTP yönetimine git →": "Go to FTP management →",
  "Panoya kopyalanamadı": "Failed to copy to clipboard",
  "Parola üretilemedi": "Failed to generate password",
  "Sistem kullanıcısı": "System user",
  "Tekrar üret": "Regenerate",
  "Tıkla → kopyala": "Click → copy",
  "Veritabanları yönetimine git →": "Go to database management →",
  "Web kökü": "Web root",
  "Yeni parola üret": "Generate new password",
  "veritabanı yok": "no database",
  "Parolayı şimdi kopyalayın — bu pencereyi kapattıktan sonra tekrar göremeyebilirsiniz.": "Copy the password now — you may not see it again after closing this window.",
  "Yeni parola üretildi": "New password generated",
  "Türkçe": "English",
  "Anasayfa": "Home",
  "Domainler": "Domains",
  "Bağlantı Bilgisi": "Connection Info",
  "Ev dizini": "Home directory",
  "Sunucu": "Server",
  "Kullanıcı adı": "Username",
  "Parola": "Password",
  "Veritabanı": "Database",
  "Otomatik kopyalanamadı. Ctrl+C basıp Enter'a tıklayın:": "Auto-copy failed. Press Ctrl+C and click Enter:",
  "Şifreyi Göster / Yenile": "Show / Reset Password",
  "Parolası": "Password",
  "Kullanıcı:": "User:",
  "Kapat": "Close",
  "Üretiliyor…": "Generating…",
  "Kopyala": "Copy",
  "Kopyalandı": "Copied",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CONN_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function panoYaz(text: string): boolean {
  // 1) Modern API (HTTPS / localhost only) — kullanıcı gesture içindeyse async ok
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).catch(hataYakala(cevir("Panoya kopyalanamadı")))
    return true
  }
  // 2) Fallback: textarea + execCommand
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.top = '0'
    ta.style.left = '0'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.focus()
    ta.select()
    ta.setSelectionRange(0, text.length)
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    if (ok) return true
  } catch {}
  // 3) Son çare: prompt — kullanıcı Ctrl+C ile manuel kopyalar
  try {
    window.prompt(cevir("Otomatik kopyalanamadı. Ctrl+C basıp Enter'a tıklayın:"), text)
    return true
  } catch {
    return false
  }
}



type Domain = {
  id: number; alan_adi: string; ipv4: string
  ftp_host: string; ftp_user: string
  db_host: string; db_user: string; db_adi: string
  sistem_kullanici: string; web_root: string
}

export default function DomainConnectionPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const toast = useToast()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [, setHata] = useState<string | null>(null)
  const [kopya, setKopya] = useState<string | null>(null)
  const [parolaModal, setParolaModal] = useState<{ tip: 'ftp' | 'db'; cikti?: string } | null>(null)

  useEffect(() => {
    let iptal = false
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (iptal) return; const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
    return () => { iptal = true }
  }, [id])

  function kopyala(deg: string) {
    panoYaz(deg)
    setKopya(deg)
    setTimeout(() => setKopya(null), 1800)
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: cevir('Anasayfa'), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Bağlantı Bilgisi") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Bağlantı Bilgisi")}</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}<span className="text-xs text-slate-400 dark:text-slate-500">{cevir("Değer üstüne tıkla → otomatik kopyalanır")}</span>
        </p>
      )}

      {domain && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          <Kart baslik="FTP / SFTP" renk="sky" ikon="M3 16V8a2 2 0 012-2h6l2 2h5a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2z">
            <Sat e={cevir(cevir("Sunucu"))} d={domain.ftp_host} onKopya={kopyala} kopya={kopya} />
            <Sat e="Port" d="21" onKopya={kopyala} kopya={kopya} />
            <Sat e={cevir("Kullanıcı adı")} d={domain.ftp_user} onKopya={kopyala} kopya={kopya} mono />
            <Parola e={cevir("Parola")} id={id!} tip="ftp" onAc={() => setParolaModal({ tip: 'ftp' })} />
            <Sat e={cevir("Ev dizini")} d={`/home/${domain.sistem_kullanici}`} onKopya={kopyala} kopya={kopya} mono />
            <Link to={`/abonelikler/${id}/ftp`} className="block mt-2 text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{cevir("FTP yönetimine git →")}</Link>
          </Kart>

          <Kart baslik="MySQL / MariaDB" renk="violet" ikon="M4 7c0-1.657 3.582-3 8-3s8 1.343 8 3-3.582 3-8 3-8-1.343-8-3z">
            <Sat e={cevir(cevir("Sunucu"))} d={domain.db_host} onKopya={kopyala} kopya={kopya} />
            <Sat e="Port" d="3306" onKopya={kopyala} kopya={kopya} />
            <Sat e={cevir("Veritabanı")} d={domain.db_adi} onKopya={kopyala} kopya={kopya} mono />
            <Sat e={cevir("Kullanıcı adı")} d={domain.db_user} onKopya={kopyala} kopya={kopya} mono />
            <Parola e={cevir("Parola")} id={id!} tip="db" onAc={() => setParolaModal({ tip: 'db' })} />
            <Link to={`/abonelikler/${id}/veritabanlari`} className="block mt-2 text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{cevir("Veritabanları yönetimine git →")}</Link>
          </Kart>

          <Kart baslik="Web" renk="amber" ikon="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" cift>
            <Sat e={cevir("Web kökü")} d={domain.web_root} onKopya={kopyala} kopya={kopya} mono />
            <Sat e="IPv4" d={domain.ipv4} onKopya={kopyala} kopya={kopya} mono />
            <Sat e={cevir("Sistem kullanıcısı")} d={domain.sistem_kullanici} onKopya={kopyala} kopya={kopya} mono />
            <Sat e="HTTP URL" d={`http://${domain.alan_adi}/`} onKopya={kopyala} kopya={kopya} />
            <Sat e="HTTPS URL" d={`https://${domain.alan_adi}/`} onKopya={kopyala} kopya={kopya} />
          </Kart>
        </div>
      )}

      {parolaModal && (
        <ParolaSifirlaModal
          tip={parolaModal.tip}
          domainId={id!}
          ftpUser={domain?.ftp_user || ''}
          dbUser={domain?.db_user || ''}
          onKapat={() => setParolaModal(null)}
          onKopya={kopyala}
        />
      )}
    </div>
  )
}

function Kart({ baslik, renk, ikon, children, cift }: { baslik: string; renk: string; ikon: string; children: React.ReactNode; cift?: boolean }) {
  const bg: Record<string, string> = {
    sky: 'bg-sky-100 text-sky-700',
    violet: 'bg-violet-100 dark:bg-violet-900/30 text-violet-700 dark:text-violet-300',
    amber: 'bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300',
  }
  return (
    <div className={`bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 ${cift ? 'lg:col-span-2' : ''}`}>
      <div className="flex items-center gap-2 mb-3">
        <div className={`w-9 h-9 rounded-lg flex items-center justify-center ${bg[renk]}`}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}>
            <path strokeLinecap="round" strokeLinejoin="round" d={ikon} />
          </svg>
        </div>
        <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">{baslik}</h3>
      </div>
      <dl className="space-y-2 text-sm">{children}</dl>
    </div>
  )
}

function Sat({ e, d, mono, onKopya, kopya }: { e: string; d: string; mono?: boolean; onKopya?: (s: string) => void; kopya?: string | null }) {
  const aktif = !!onKopya
  const kopyalandi = kopya === d
  return (
    <div className="flex items-center justify-between gap-3 py-1.5 border-b border-slate-100 dark:border-dark-600 last:border-0">
      <dt className="text-slate-500 dark:text-slate-500 text-xs uppercase tracking-wider">{e}</dt>
      <dd
        onClick={() => aktif && onKopya!(d)}
        className={`text-right flex items-center gap-2 group ${aktif ? 'cursor-pointer' : ''}`}
        title={aktif ? cevir('Tıkla → kopyala') : ''}
      >
        <span className={`${mono ? 'font-mono text-xs' : 'text-sm'} ${aktif ? 'text-slate-800 dark:text-slate-200 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 transition' : 'text-slate-800 dark:text-slate-200'}`}>
          {d}
        </span>
        {kopyalandi && (
          <span className="text-[10px] uppercase tracking-wider bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 px-1.5 py-0.5 rounded font-medium animate-pulse">
            {cevir(cevir("Kopyalandı"))}
          </span>
        )}
      </dd>
    </div>
  )
}

function Parola({ e, onAc }: { e: string; id: string; tip: string; onAc: () => void }) {
  return (
    <div className="flex items-center justify-between gap-3 py-1.5 border-b border-slate-100 dark:border-dark-600 last:border-0">
      <dt className="text-slate-500 dark:text-slate-500 text-xs uppercase tracking-wider">{e}</dt>
      <dd className="text-right">
        <button
          onClick={onAc}
          className="text-xs px-3 py-1 bg-dark-800 hover:bg-dark-700 dark:bg-dark-600 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded font-medium transition inline-flex items-center gap-1"
        >
          <Ikon d={I.anahtar} /> {cevir(cevir("Şifreyi Göster / Yenile"))}
        </button>
      </dd>
    </div>
  )
}

function ParolaSifirlaModal({ tip, domainId, ftpUser, dbUser, onKapat, onKopya }:
  { tip: 'ftp' | 'db'; domainId: string; ftpUser: string; dbUser: string; onKapat: () => void; onKopya: (s: string) => void }) {
  const toast = useToast()
  const [parola, setParola] = useState('')
  const [isleniyor, setIsleniyor] = useState(false)
  const [, setHata] = useState<string | null>(null)

  async function kaydet() {
    if (parola.trim().length < 8) { toast.hata(cevir("İşlem başarısız"), cevir("Parola en az 8 karakter olmalı")); return }
    setIsleniyor(true); setHata(null)
    try {
      if (tip === 'ftp') {
        await api.put(`/domains/${domainId}/ftp/password`, { parola })
      } else {
        // İlk DB id'sini al
        const dbs = await api.get<any[]>(`/domains/${domainId}/databases`)
        const main = (dbs.data || [])[0]
        if (!main) throw new Error(cevir("veritabanı yok"))
        await api.put(`/databases/${main.id}/password`, { parola })
      }
      toast.basari(cevir("✓ Parola güncellendi"))
      onKapat()
    } catch (e) { const m = apiHata(e, cevir("Parola güncellenemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
    finally { setIsleniyor(false) }
  }

  const user = tip === 'ftp' ? ftpUser : dbUser
  const tipAd = tip === 'ftp' ? 'FTP' : cevir("Veritabanı")

  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={onKapat}>
      <div className="bg-white dark:bg-dark-700 rounded-lg w-full max-w-md p-5 shadow-xl" onClick={ev => ev.stopPropagation()}>
        <div className="flex items-center gap-2 mb-3">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-6 w-6"><path d="M15.75 7.5a3.75 3.75 0 10-3.4 3.73L6 17.6V21h3.4l.9-.9v-1.8h1.8l1.2-1.2v-1.8h1.4l1.05-1.05A3.75 3.75 0 0015.75 7.5z"/></svg>
          <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">{tipAd} {cevir("Parolası")}</h3>
        </div>
        <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-4 bg-slate-50 dark:bg-dark-800 px-3 py-2 rounded">
          <span className="text-slate-500 dark:text-slate-500">{cevir("Kullanıcı:")}</span> <code className="font-mono text-slate-900 dark:text-slate-100">{user}</code>
        </div>

        {/* Yeni parola — kullanıcı yazar, gösterilmez (write-only) */}
        <div className="mb-4">
          <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">{cevir("Yeni parola")}</label>
          <input type="password" value={parola} onChange={e => setParola(e.target.value)} autoComplete="new-password"
            placeholder={cevir("Yeni parola girin (≥8)")}
            className="w-full px-2.5 py-1.5 rounded-md border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 text-sm" />
        </div>

        <div className="flex gap-2 justify-end mt-4">
          <button onClick={onKapat} className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-dark-800 dark:hover:bg-dark-700 text-sm rounded">
            {cevir("Kapat")}
          </button>
          <button onClick={kaydet} disabled={isleniyor || parola.trim().length < 8}
            className="px-3 py-1.5 bg-dark-800 hover:bg-dark-700 dark:bg-dark-600 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm rounded font-medium">
            {isleniyor ? cevir('Kaydediliyor…') : <span className="inline-flex items-center gap-1.5"><Ikon d={I.yenile} /> {cevir("Parolayı güncelle")}</span>}
          </button>
        </div>
      </div>
    </div>
  )
}

function KopyaButton({ text, renk }: { text: string; renk: 'amber' | 'emerald' }) {
  const [k, setK] = useState(false)
  const bg: Record<string, string> = {
    amber: 'bg-amber-100 dark:bg-amber-900/30 hover:bg-amber-200 text-amber-800 dark:text-amber-200',
    emerald: 'bg-emerald-100 dark:bg-emerald-900/30 hover:bg-emerald-200 text-emerald-800 dark:text-emerald-200',
  }
  return (
    <button
      onClick={() => {
        const ok = panoYaz(text)
        if (ok) {
          setK(true)
          setTimeout(() => setK(false), 1500)
        }
      }}
      className={`text-[10px] px-2 py-1 rounded font-medium transition ${bg[renk]} ${k ? 'ring-2 ring-emerald-400' : ''}`}
    >
      {k ? <span className="inline-flex items-center gap-1"><Ikon d={I.onay} /> {cevir("Kopyalandı")}</span> : cevir('Kopyala')}
    </button>
  )
}