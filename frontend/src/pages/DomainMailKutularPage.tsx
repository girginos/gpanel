import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import type { Domain } from '@/components/DomainList'
import { useDialog } from '@/components/Dialog'

type MailDomain = { id: number; ad: string; kutu_sayisi: number }
type Kutu = { id: number; email: string; domain_id: number; quota_bytes: number; kullanilan_bytes: number; aktif: boolean }

// Client-side güçlü parola üretici (yeni kutu formu için — kripto-güvenli).

const MAILKUTU_EN: Record<string, string> = {
  "ornek": "example",
  "Bu domain için mail servisi henüz kurulmadı.": "Mail service is not set up for this domain yet.",
  "Güçlü parola üret": "Generate strong password",
  "Henüz posta kutusu yok. Yukarıdan ekleyin.": "No mailboxes yet. Add one above.",
  "Kutu detayları: oto-yanıt, iletim, taşıma, entegrasyon, içe/dışa aktar": "Mailbox details: auto-reply, forwarding, migration, integration, import/export",
  "Kutu eklenemedi (parola min 6 karakter olmalı)": "Failed to add mailbox (password must be at least 6 characters)",
  "Mail Kutuları": "Mailboxes",
  "Mail domaini oluşturulamadı": "Failed to create mail domain",
  "Mail domainleri alınamadı": "Failed to get mail domains",
  "Panoya kopyalanamadı — adresi elle seçip kopyalayabilirsiniz.": "Failed to copy to clipboard — you can select and copy the address manually.",
  "Parola üretilemedi": "Failed to generate password",
  "Posta Kutuları": "Mailboxes",
  "Sınırsız kota": "Unlimited quota",
  "Tıkla — e-posta adresini kopyala": "Click — copy the email address",
  "Webmail girişi açılamadı": "Failed to open webmail login",
  "Webmail kullanılamıyor.": "Webmail is unavailable.",
  "Webmail kullanılamıyor:": "Webmail is unavailable:",
  "Webmail'e tek tıkla giriş": "One-click webmail login",
  "Yeni güçlü parola üret (şifre sıfırla)": "Generate a new strong password (reset password)",
  "Yeni parola üretildi": "New password generated",
  "Türkçe": "English",
  "Askıda": "Suspended",
  "Detaylar": "Details",
  "Domainler": "Domains",
  "Ekle": "Add",
  "Giriş": "Login",
  "Kapat": "Close",
  "Kullanıcı adı": "Username",
  "Üret": "Generate",
  "Domain yüklenemedi": "Failed to load domain",
  "Kutular alınamadı": "Failed to get mailboxes",
  "IMAP/SMTP istemcileri (Outlook, telefon) çalışmaya devam eder. Yönetici:": "IMAP/SMTP clients (Outlook, phone) keep working. Admin:",
  "adımlarına bakın.": "see the steps.",
  "kurulum ekranındaki": "on the setup screen, see the",
  "Eklentiler → Mail": "Plugins → Mail",
  "parola veya üret →": "password or generate →",
  "{0} için yeni güçlü parola üretilsin mi? Mevcut parola geçersiz olur.": "Generate a new strong password for {0}? The current password will be invalidated.",
  "Şifre sıfırla": "Reset password",
  "— bu parola yalnızca şimdi gösteriliyor, güvenli bir yere kaydedin.": "— this password is shown only now, save it somewhere safe.",
  "✓ Mail domaini oluşturuldu. Artık kutu ekleyebilirsiniz.": "✓ Mail domain created. You can now add mailboxes.",
  "✓ {0} oluşturuldu.": "✓ {0} created.",
  "kullanılıyor": "used",
  "Onay gerekiyor": "Confirmation required",
  "Anasayfa": "Home",
  "posta kutularını yönetin.": "manage the mailboxes.",
  "— Posta akışı etkilenmez: gelen postalar kutuya düşer,": "— Mail flow is not affected: incoming mail is delivered to the mailbox,",
  "Kuruluyor…": "Setting up…",
  "Mail Servisini Kur": "Set up Mail Service",
  "Yeni Posta Kutusu": "New Mailbox",
  "Parola (min 6)": "Password (min 6)",
  "Kota (MB)": "Quota (MB)",
  "kutu": "mailboxes",
  "Kopyalandı": "Copied",
  "Askıdaki kutuya giriş yapılamaz": "Cannot log in to a suspended mailbox",
  "Kopyala": "Copy",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (MAILKUTU_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function guicluParola(uzunluk = 16) {
  const alfabe = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%*-_'
  const arr = new Uint32Array(uzunluk)
  crypto.getRandomValues(arr)
  let s = ''
  for (let i = 0; i < uzunluk; i++) s += alfabe[arr[i] % alfabe.length]
  return s
}

function boyut(b: number) {
  if (b < 0) return '—'
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`
  const mbv = b / 1024 / 1024
  if (mbv < 1024) return `${mbv < 10 ? mbv.toFixed(1) : Math.round(mbv)} MB`
  return `${(mbv / 1024).toFixed(1)} GB`
}

// ── İkonlar (tek stil, inline SVG) ──
const Ikon = {
  giris: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M13 7l5 5m0 0l-5 5m5-5H6" /></svg>,
  anahtar: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M15 7a2 2 0 012 2m4-2a6 6 0 01-7.743 5.743L11 14H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" /></svg>,
  kalem: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>,
  yenile: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>,
  cop: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" /></svg>,
  zarf: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M3 8l9 6 9-6m-9 6V4M3 8v10a2 2 0 002 2h14a2 2 0 002-2V8l-9 6-9-6z" /></svg>,
  duraklat: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>,
  oynat: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" /><path strokeLinecap="round" strokeLinejoin="round" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>,
  cevap: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6" /></svg>,
  ayar: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" /><path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>,
  kopya: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>,
  onay: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" /></svg>,
}

// panoyaYaz — HTTPS'te navigator.clipboard; erişilemezse execCommand ile yedek yol.
// (Panel HTTPS ama iç ağda http:// ile açan olabilir — sessiz başarısızlık YASAK.)
async function panoyaYaz(metin: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) { await navigator.clipboard.writeText(metin); return true }
  } catch { /* yedek yola düş */ }
  try {
    const ta = document.createElement('textarea')
    ta.value = metin
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch { return false }
}

// Kota kullanım barı — kullanılan / toplam (0 kota = sınırsız).
function KotaBar({ kullanilan, kota }: { kullanilan: number; kota: number }) {
  const kul = Math.max(0, kullanilan)
  if (!kota) {
    return <div className="text-xs text-slate-500 dark:text-slate-400">{boyut(kul)} {cevir("kullanılıyor")} · <span className="text-slate-400 dark:text-slate-500">{cevir("Sınırsız kota")}</span></div>
  }
  const pct = Math.min(100, Math.round((kul / kota) * 100))
  const renk = pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-amber-500' : 'bg-emerald-500'
  return (
    <div className="w-full max-w-md">
      <div className="flex items-baseline justify-between text-xs mb-1">
        <span className="text-slate-600 dark:text-slate-300 tabular-nums">{boyut(kul)} <span className="text-slate-400">/ {boyut(kota)}</span></span>
        <span className={`tabular-nums font-medium ${pct >= 90 ? 'text-red-600 dark:text-red-400' : pct >= 70 ? 'text-amber-600 dark:text-amber-400' : 'text-slate-400 dark:text-slate-500'}`}>%{pct}</span>
      </div>
      <div className="h-2 w-full rounded-full bg-slate-100 dark:bg-slate-700/70 overflow-hidden" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
        <div className={`h-full ${renk} rounded-full transition-all duration-500`} style={{ width: `${Math.max(pct, 2)}%` }} />
      </div>
    </div>
  )
}

export default function DomainMailKutularPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const { id } = useParams()
  const navigate = useNavigate()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [mailDomain, setMailDomain] = useState<MailDomain | null | undefined>(undefined) // undefined=yükleniyor, null=yok
  const [kutular, setKutular] = useState<Kutu[] | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  // Webmail tek-tık giriş kullanılamıyorsa SEBEBİ kalıcı olarak gösterilir.
  // 🔴 Sessiz başarısızlık YASAK: kullanıcı butona basıp hiçbir şey olmamasını
  // değil, cevir("neden olmadığını") görmeli.
  const [webmailKapali, setWebmailKapali] = useState<string | null>(null)
  const [bildirim, setBildirim] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [uretilen, setUretilen] = useState<{ email: string; parola: string } | null>(null)
  const [kopyalandi, setKopyalandi] = useState(false)
  // Listede tek tıkla kopyalanan e-posta (kısa süreli "✓" geri bildirimi).
  const [kopyalananEmail, setKopyalananEmail] = useState<string | null>(null)

  // Yeni kutu formu
  const [yeniKullanici, setYeniKullanici] = useState('')
  const [yeniParola, setYeniParola] = useState('')
  const [yeniQuota, setYeniQuota] = useState(1024)

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e, cevir("Domain yüklenemedi"))))
  }, [id])

  function mailDomainYukle(d: Domain) {
    api.get<{ domainler: MailDomain[] }>('/eklenti/mail/domainler')
      .then(r => {
        const md = (r.data.domainler || []).find(x => x.ad.toLowerCase() === d.alan_adi.toLowerCase())
        setMailDomain(md || null)
      })
      .catch(e => { setHata(apiHata(e, cevir("Mail domainleri alınamadı"))); setMailDomain(null) })
  }
  useEffect(() => { if (domain) mailDomainYukle(domain) }, [domain])

  function kutulariYukle(did: number) {
    setKutular(null)
    api.get<{ hesaplar: Kutu[] }>(`/eklenti/mail/hesaplar?domain=${did}`)
      .then(r => setKutular(r.data.hesaplar || []))
      .catch(e => { setHata(apiHata(e, cevir("Kutular alınamadı"))); setKutular([]) })
  }
  useEffect(() => { if (mailDomain) kutulariYukle(mailDomain.id) }, [mailDomain])

  async function mailDomainOlustur() {
    if (!domain) return
    setIsleniyor(true); setHata(null)
    try {
      await api.post('/eklenti/mail/domainler', { ad: domain.alan_adi })
      setBildirim(cevir("✓ Mail domaini oluşturuldu. Artık kutu ekleyebilirsiniz."))
      mailDomainYukle(domain)
    } catch (e) { setHata(apiHata(e, cevir("Mail domaini oluşturulamadı"))) }
    finally { setIsleniyor(false) }
  }

  async function kutuEkle(e: React.FormEvent) {
    e.preventDefault()
    if (!mailDomain || !domain) return
    const email = `${yeniKullanici.trim().toLowerCase()}@${domain.alan_adi}`
    setIsleniyor(true); setHata(null); setBildirim(null)
    try {
      await api.post('/eklenti/mail/hesaplar', { domain_id: mailDomain.id, email, parola: yeniParola, quota_mb: yeniQuota })
      setBildirim(cevirT(cevir("✓ {0} oluşturuldu."), email))
      setYeniKullanici(''); setYeniParola('')
      kutulariYukle(mailDomain.id)
    } catch (er) { setHata(apiHata(er, cevir("Kutu eklenemedi (parola min 6 karakter olmalı)"))) }
    finally { setIsleniyor(false) }
  }

  // E-posta adresini tek tıkla panoya kopyala.
  async function emailKopyala(email: string) {
    if (await panoyaYaz(email)) {
      setKopyalananEmail(email)
      window.setTimeout(() => setKopyalananEmail(v => (v === email ? null : v)), 1500)
    } else {
      setHata(cevir("Panoya kopyalanamadı — adresi elle seçip kopyalayabilirsiniz."))
    }
  }

  // Tek-tık güçlü parola üret (düz metin bir kez modalda gösterilir).
  async function parolaUret(k: Kutu) {
    if (!(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevirT(cevir("{0} için yeni güçlü parola üretilsin mi? Mevcut parola geçersiz olur."), k.email) }))) return
    setIsleniyor(true); setHata(null)
    try {
      const r = await api.post<{ email: string; parola: string }>(`/eklenti/mail/hesaplar/${k.id}/parola-uret`)
      setKopyalandi(false)
      setUretilen({ email: r.data.email, parola: r.data.parola })
    } catch (e) { setHata(apiHata(e, cevir("Parola üretilemedi"))) }
    finally { setIsleniyor(false) }
  }

  // Tek-tık webmail girişi — popup engelini aşmak için sekmeyi ÖNCE aç, sonra yönlendir.
  async function webmailGiris(k: Kutu) {
    const w = window.open('about:blank', '_blank')
    setIsleniyor(true); setHata(null)
    try {
      const r = await api.post<{ url: string }>(`/eklenti/mail/hesaplar/${k.id}/giris`)
      if (w) w.location.href = r.data.url
      else window.location.href = r.data.url
    } catch (e) {
      if (w) w.close()
      const mesaj = apiHata(e, cevir("Webmail girişi açılamadı"))
      // 503 = webmail sunucuda kurulu/yapılandırılmış değil (geçici bir hata değil).
      if ((e as { response?: { status?: number } })?.response?.status === 503) setWebmailKapali(mesaj)
      setHata(mesaj)
    }
    finally { setIsleniyor(false) }
  }

  if (!domain) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
    </div>
  )

  const btnSec = 'inline-flex items-center gap-2 text-sm font-medium px-4 py-2.5 rounded-xl border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 hover:border-slate-300 dark:hover:bg-slate-700/60 disabled:opacity-50 transition-colors'
  const btnPri = 'inline-flex items-center gap-2 text-sm font-medium px-4 py-2.5 rounded-xl bg-brand-600 hover:bg-brand-700 text-white shadow-sm shadow-brand-600/20 disabled:opacity-50 transition-colors'

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-7xl mx-auto">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain.alan_adi, href: `/abonelikler/${id}` },
        { etiket: cevir("Mail Kutuları") },
      ]} />
      <div className="flex items-start justify-between gap-4 mb-5">
        <div>
          <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">{cevir("Mail Kutuları")}</h1>
          <p className="text-sm text-slate-500 dark:text-slate-400">{domain.alan_adi} {cevir("posta kutularını yönetin.")}</p>
        </div>
      </div>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {webmailKapali && (
        <div className="mb-3 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-sm text-amber-800 dark:text-amber-300">
          <b>{cevir("Webmail kullanılamıyor.")}</b> {webmailKapali} {cevir("— Posta akışı etkilenmez: gelen postalar kutuya düşer,")}
          {cevir("IMAP/SMTP istemcileri (Outlook, telefon) çalışmaya devam eder. Yönetici:")} <span className="font-mono">{cevir("Eklentiler → Mail")}</span>
          {cevir("kurulum ekranındaki")} <span className="font-mono">webmail-*</span> {cevir("adımlarına bakın.")}
        </div>
      )}

      {mailDomain === undefined && <div className="py-8 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}

      {mailDomain === null && (
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-8 text-center">
          <div className="w-12 h-12 mx-auto mb-3 rounded-xl bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300"><Ikon.zarf className="w-6 h-6" /></div>
          <p className="text-sm text-slate-600 dark:text-slate-300 mb-4">{cevir("Bu domain için mail servisi henüz kurulmadı.")}</p>
          <button onClick={mailDomainOlustur} disabled={isleniyor}
            className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-4 py-2 rounded-lg disabled:opacity-60">
            {isleniyor ? cevir("Kuruluyor…") : cevir("Mail Servisini Kur")}
          </button>
        </div>
      )}

      {mailDomain && (
        <>
          {/* Yeni kutu */}
          <form onSubmit={kutuEkle} className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5 shadow-sm">
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">{cevir("Yeni Posta Kutusu")}</h2>
            <div className="flex flex-wrap items-end gap-3">
              <div className="flex-1 min-w-[220px]">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">{cevir("Kullanıcı adı")}</label>
                <div className="flex items-center">
                  <input value={yeniKullanici} onChange={e => setYeniKullanici(e.target.value)} required placeholder={cevir("ornek")}
                    className="w-full rounded-l-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
                  <span className="px-3 py-2 text-sm bg-slate-50 dark:bg-slate-700 border border-l-0 border-slate-300 dark:border-slate-600 rounded-r-lg text-slate-500 dark:text-slate-400 whitespace-nowrap">@{domain.alan_adi}</span>
                </div>
              </div>
              <div className="min-w-[210px] flex-1 sm:flex-none">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">{cevir("Parola (min 6)")}</label>
                <div className="flex items-center gap-1.5">
                  <input type="text" value={yeniParola} onChange={e => setYeniParola(e.target.value)} required minLength={6} placeholder={cevir("parola veya üret →")}
                    className="w-full min-w-0 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
                  <button type="button" onClick={() => setYeniParola(guicluParola())} title={cevir("Güçlü parola üret")}
                    className="shrink-0 inline-flex items-center gap-1 text-xs font-medium px-2.5 py-2 rounded-lg border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 hover:border-slate-300 dark:hover:bg-slate-700/60 transition-colors">
                    <Ikon.anahtar className="w-3.5 h-3.5" />{cevir("Üret")}
                  </button>
                </div>
              </div>
              <div className="w-28">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">{cevir("Kota (MB)")}</label>
                <input type="number" min={0} value={yeniQuota} onChange={e => setYeniQuota(+e.target.value)}
                  className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
              </div>
              <button type="submit" disabled={isleniyor}
                className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-5 py-2 rounded-lg disabled:opacity-60 transition-colors">{cevir("Ekle")}</button>
            </div>
          </form>

          {/* Kutu listesi */}
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden shadow-sm">
            <div className="px-5 py-3.5 border-b border-slate-100 dark:border-slate-700 flex items-center justify-between">
              <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Posta Kutuları")}</h2>
              <span className="text-xs text-slate-400 tabular-nums">{kutular?.length ?? 0} {cevir("kutu")}</span>
            </div>
            {kutular === null && <div className="py-10 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}
            {kutular?.length === 0 && (
              <div className="py-12 text-center">
                <div className="w-10 h-10 mx-auto mb-2 rounded-lg bg-slate-50 dark:bg-slate-700/50 flex items-center justify-center text-slate-300 dark:text-slate-500"><Ikon.zarf className="w-5 h-5" /></div>
                <p className="text-sm text-slate-400">{cevir("Henüz posta kutusu yok. Yukarıdan ekleyin.")}</p>
              </div>
            )}
            {kutular && kutular.length > 0 && (
              <ul className="divide-y divide-slate-100 dark:divide-slate-700/70">
                {kutular.map(k => (
                  <li key={k.id} className="px-4 sm:px-5 py-4 hover:bg-slate-50/70 dark:hover:bg-slate-700/20 transition-colors">
                    {/* 3 sütun: kimlik · ORTADA kota barı · butonlar. Dar ekranda
                        alt alta yığılır (mobilde sıkışmaz). */}
                    <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:gap-6">
                      {/* Kimlik */}
                      <div className="flex items-center gap-3 min-w-0 lg:w-[340px] lg:shrink-0">
                        <div className="w-10 h-10 shrink-0 rounded-xl bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300">
                          <Ikon.zarf className="w-5 h-5" />
                        </div>
                        <div className="flex items-center gap-2 min-w-0">
                          {/* Tek tık = e-postayı panoya kopyala (istemcilere yapıştırmak için). */}
                          <button
                            type="button"
                            onClick={() => emailKopyala(k.email)}
                            title={kopyalananEmail === k.email ? cevir("Kopyalandı") : cevir("Tıkla — e-posta adresini kopyala")}
                            className={`group inline-flex items-center gap-1.5 min-w-0 rounded-md -mx-1 px-1 py-0.5 text-left transition-colors hover:bg-brand-50/70 dark:hover:bg-brand-900/20 ${k.aktif ? 'text-slate-800 dark:text-slate-100' : 'text-slate-400 dark:text-slate-500'}`}>
                            <span className={`font-mono text-sm font-medium truncate ${k.aktif ? '' : 'line-through'}`}>{k.email}</span>
                            {kopyalananEmail === k.email
                              ? <Ikon.onay className="w-3.5 h-3.5 shrink-0 text-emerald-500" />
                              : <Ikon.kopya className="w-3.5 h-3.5 shrink-0 text-slate-300 dark:text-slate-600 opacity-0 group-hover:opacity-100 transition-opacity" />}
                          </button>
                          {!k.aktif && <span className="shrink-0 text-[10px] font-medium px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-400 uppercase">{cevir("Askıda")}</span>}
                        </div>
                      </div>
                      {/* Kota barı — satırın ortasında */}
                      <div className="flex-1 min-w-0 flex lg:justify-center">
                        <KotaBar kullanilan={k.kullanilan_bytes} kota={k.quota_bytes} />
                      </div>
                      {/* Aksiyonlar */}
                      <div className="flex items-center gap-2 flex-wrap lg:shrink-0 lg:justify-end">
                        <button onClick={() => webmailGiris(k)} disabled={isleniyor || !k.aktif || !!webmailKapali} title={webmailKapali ? cevir("Webmail kullanılamıyor:") + ' ' + webmailKapali : k.aktif ? cevir("Webmail'e tek tıkla giriş") : cevir("Askıdaki kutuya giriş yapılamaz")} className={btnPri}>
                          <Ikon.giris className="w-4 h-4" />{cevir("Giriş")}
                        </button>
                        <button onClick={() => parolaUret(k)} disabled={isleniyor} title={cevir("Yeni güçlü parola üret (şifre sıfırla)")} className={btnSec}>
                          <Ikon.anahtar className="w-4 h-4" />{cevir("Şifre sıfırla")}
                        </button>
                        <button onClick={() => navigate(`/abonelikler/${id}/mail/kutular/${k.id}`)} title={cevir("Kutu detayları: oto-yanıt, iletim, taşıma, entegrasyon, içe/dışa aktar")} className={btnSec}>
                          <Ikon.ayar className="w-4 h-4" />{cevir("Detaylar")}
                        </button>
                      </div>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </>
      )}

      {/* Üretilen parola modalı */}
      {uretilen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4" onClick={() => setUretilen(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl p-6 max-w-sm w-full shadow-2xl border border-slate-200 dark:border-slate-700" onClick={e => e.stopPropagation()}>
            <div className="flex items-center gap-2.5 mb-3">
              <div className="w-9 h-9 rounded-lg bg-emerald-50 dark:bg-emerald-900/30 flex items-center justify-center text-emerald-600 dark:text-emerald-400"><Ikon.anahtar className="w-5 h-5" /></div>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Yeni parola üretildi")}</h3>
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">
              <span className="font-mono text-slate-700 dark:text-slate-300">{uretilen.email}</span> {cevir("— bu parola yalnızca şimdi gösteriliyor, güvenli bir yere kaydedin.")}
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 font-mono text-sm bg-slate-100 dark:bg-slate-900 rounded-lg px-3 py-2.5 break-all select-all text-slate-800 dark:text-slate-100">{uretilen.parola}</code>
              <button
                onClick={() => { navigator.clipboard?.writeText(uretilen.parola); setKopyalandi(true) }}
                className="shrink-0 text-xs font-medium px-3 py-2.5 rounded-lg bg-brand-600 hover:bg-brand-700 text-white transition-colors">
                {kopyalandi ? '✓ ' + cevir("Kopyalandı") : cevir("Kopyala")}
              </button>
            </div>
            <button onClick={() => setUretilen(null)}
              className="mt-4 w-full text-sm font-medium px-4 py-2 rounded-lg border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors">{cevir("Kapat")}</button>
          </div>
        </div>
      )}
    </div>
  )
}
