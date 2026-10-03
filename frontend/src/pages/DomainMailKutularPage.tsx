import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState, useRef } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { Button, Badge } from '@/components/ui'
import Breadcrumb from '@/components/Breadcrumb'
import type { Domain } from '@/components/domainTipi'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import Modal from '@/components/Modal'
import { UyariKutusu } from '@/components/UyariKutusu'
import { I, Ikon } from '@/components/Ikon'

type MailDomain = { id: number; ad: string; kutu_sayisi: number }
type Kutu = { id: number; email: string; domain_id: number; quota_bytes: number; kullanilan_bytes: number; aktif: boolean }

// Client-side güçlü parola üretici (yeni kutu formu için — kripto-güvenli).

const MAILKUTU_EN: Record<string, string> = {
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
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
  "Mail domaini oluşturuldu. Artık kutu ekleyebilirsiniz.": "Mail domain created. You can now add mailboxes.",
  "{0} oluşturuldu.": "{0} created.",
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
      <div className="h-2 w-full rounded-full bg-slate-100 dark:bg-dark-600/70 overflow-hidden" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
        <div className={`h-full ${renk} rounded-full transition-all duration-500`} style={{ width: `${Math.max(pct, 2)}%` }} />
      </div>
    </div>
  )
}

export default function DomainMailKutularPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  const navigate = useNavigate()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [mailDomain, setMailDomain] = useState<MailDomain | null | undefined>(undefined) // undefined=yükleniyor, null=yok
  const [kutular, setKutular] = useState<Kutu[] | null>(null)
  const [, setHata] = useState<string | null>(null)
  // Webmail tek-tık giriş kullanılamıyorsa SEBEBİ kalıcı olarak gösterilir.
  // 🔴 Sessiz başarısızlık YASAK: kullanıcı butona basıp hiçbir şey olmamasını
  // değil, cevir("neden olmadığını") görmeli.
  const [webmailKapali, setWebmailKapali] = useState<string | null>(null)
  const [, setBildirim] = useState<string | null>(null)
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
    let iptal = false
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (iptal) return; const m = apiHata(e, cevir("Domain yüklenemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
    return () => { iptal = true }
  }, [id])

  const mailDomainYukleNesli = useRef(0)
  function mailDomainYukle(d: Domain) {
    const _n = ++mailDomainYukleNesli.current
    api.get<{ domainler: MailDomain[] }>('/eklenti/mail/domainler')
      .then(r => {
        if (_n !== mailDomainYukleNesli.current) return
        const md = (r.data.domainler || []).find(x => x.ad.toLowerCase() === d.alan_adi.toLowerCase())
        setMailDomain(md || null)
      })
      .catch(e => { if (_n !== mailDomainYukleNesli.current) return; const m = apiHata(e, cevir("Mail domainleri alınamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m); setMailDomain(null) })
  }
  useEffect(() => { if (domain) mailDomainYukle(domain); return () => { mailDomainYukleNesli.current++ } }, [domain])

  const kutulariYukleNesli = useRef(0)
  function kutulariYukle(did: number) {
    setKutular(null)
    const _n = ++kutulariYukleNesli.current
    api.get<{ hesaplar: Kutu[] }>(`/eklenti/mail/hesaplar?domain=${did}`)
      .then(r => { if (_n !== kutulariYukleNesli.current) return; setKutular(r.data.hesaplar || []) })
      .catch(e => { if (_n !== kutulariYukleNesli.current) return; const m = apiHata(e, cevir("Kutular alınamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m); setKutular([]) })
  }
  useEffect(() => { if (mailDomain) kutulariYukle(mailDomain.id); return () => { kutulariYukleNesli.current++ } }, [mailDomain])

  async function mailDomainOlustur() {
    if (!domain) return
    setIsleniyor(true); setHata(null)
    try {
      await api.post('/eklenti/mail/domainler', { ad: domain.alan_adi })
      setBildirim(cevir("Mail domaini oluşturuldu. Artık kutu ekleyebilirsiniz."))
      toast.basari(cevir("Kaydedildi"), cevir("Mail domaini oluşturuldu. Artık kutu ekleyebilirsiniz."))
      mailDomainYukle(domain)
    } catch (e) { const m = apiHata(e, cevir("Mail domaini oluşturulamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
    finally { setIsleniyor(false) }
  }

  async function kutuEkle(e: React.FormEvent) {
    e.preventDefault()
    if (!mailDomain || !domain) return
    const email = `${yeniKullanici.trim().toLowerCase()}@${domain.alan_adi}`
    setIsleniyor(true); setHata(null); setBildirim(null)
    try {
      await api.post('/eklenti/mail/hesaplar', { domain_id: mailDomain.id, email, parola: yeniParola, quota_mb: yeniQuota })
      const m = cevirT(cevir("{0} oluşturuldu."), email)
      setBildirim(m)
      toast.basari(m)
      setYeniKullanici(''); setYeniParola('')
      kutulariYukle(mailDomain.id)
    } catch (er) { const m = apiHata(er, cevir("Kutu eklenemedi (parola min 6 karakter olmalı)")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
    finally { setIsleniyor(false) }
  }

  // E-posta adresini tek tıkla panoya kopyala.
  async function emailKopyala(email: string) {
    if (await panoyaYaz(email)) {
      setKopyalananEmail(email)
      window.setTimeout(() => setKopyalananEmail(v => (v === email ? null : v)), 1500)
    } else {
      setHata(cevir("Panoya kopyalanamadı — adresi elle seçip kopyalayabilirsiniz."))
      toast.hata(cevir("İşlem başarısız"), cevir("Panoya kopyalanamadı — adresi elle seçip kopyalayabilirsiniz."))
    }
  }

  // Tek-tık güçlü parola üret (düz metin bir kez modalda gösterilir).
  async function parolaUret(k: Kutu) {
    if (!(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevirT(cevir("{0} için yeni güçlü parola üretilsin mi? Mevcut parola geçersiz olur."), k.email), ton: 'uyari', onayIkonu: I.anahtar }))) return
    setIsleniyor(true); setHata(null)
    try {
      const r = await api.post<{ email: string; parola: string }>(`/eklenti/mail/hesaplar/${k.id}/parola-uret`)
      setKopyalandi(false)
      setUretilen({ email: r.data.email, parola: r.data.parola })
    } catch (e) { const m = apiHata(e, cevir("Parola üretilemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
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
      toast.hata(cevir("İşlem başarısız"), mesaj)
    }
    finally { setIsleniyor(false) }
  }

  if (!domain) return (
    <div>
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
    </div>
  )

  return (
    <div>
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

      {webmailKapali && (
        <UyariKutusu ton="uyari" className="mb-3" baslik={cevir("Webmail kullanılamıyor.")}>
          {webmailKapali} {cevir("— Posta akışı etkilenmez: gelen postalar kutuya düşer,")}
          {cevir("IMAP/SMTP istemcileri (Outlook, telefon) çalışmaya devam eder. Yönetici:")} <span className="font-mono">{cevir("Eklentiler → Mail")}</span>
          {cevir("kurulum ekranındaki")} <span className="font-mono">webmail-*</span> {cevir("adımlarına bakın.")}
        </UyariKutusu>
      )}

      {mailDomain === undefined && <div className="py-8 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}

      {mailDomain === null && (
        <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-8 text-center">
          <div className="w-12 h-12 mx-auto mb-3 rounded-lg bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300"><Ikon d={I.mail} className="w-6 h-6" /></div>
          <p className="text-sm text-slate-600 dark:text-slate-300 mb-4">{cevir("Bu domain için mail servisi henüz kurulmadı.")}</p>
          <Button onClick={mailDomainOlustur} yukleniyor={isleniyor} color="primary" ikon={I.oynat}>
            {isleniyor ? cevir("Kuruluyor…") : cevir("Mail Servisini Kur")}
          </Button>
        </div>
      )}

      {mailDomain && (
        <>
          {/* Yeni kutu */}
          <form onSubmit={kutuEkle} className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-5 shadow-xs">
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">{cevir("Yeni Posta Kutusu")}</h2>
            <div className="flex flex-wrap items-end gap-3">
              <div className="flex-1 min-w-[220px]">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">{cevir("Kullanıcı adı")}</label>
                <div className="flex items-center">
                  <input value={yeniKullanici} onChange={e => setYeniKullanici(e.target.value)} required placeholder={cevir("ornek")}
                    className="w-full rounded-l-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
                  <span className="px-3 py-2 text-sm bg-slate-50 dark:bg-dark-600 border border-l-0 border-slate-300 dark:border-slate-600 rounded-r-lg text-slate-500 dark:text-slate-400 whitespace-nowrap">@{domain.alan_adi}</span>
                </div>
              </div>
              <div className="min-w-[210px] flex-1 sm:flex-none">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">{cevir("Parola (min 6)")}</label>
                <div className="flex items-center gap-1.5">
                  <input type="text" value={yeniParola} onChange={e => setYeniParola(e.target.value)} required minLength={6} placeholder={cevir("parola veya üret →")}
                    className="w-full min-w-0 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
                  <Button onClick={() => setYeniParola(guicluParola())} title={cevir("Güçlü parola üret")}
                    variant="outlined" ikon={I.anahtar} className="shrink-0">
                    {cevir("Üret")}
                  </Button>
                </div>
              </div>
              <div className="w-28">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">{cevir("Kota (MB)")}</label>
                <input type="number" min={0} value={yeniQuota} onChange={e => setYeniQuota(+e.target.value)}
                  className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
              </div>
              <Button type="submit" disabled={isleniyor} color="primary" ikon={I.arti}>{cevir("Ekle")}</Button>
            </div>
          </form>

          {/* Kutu listesi */}
          <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg overflow-hidden shadow-xs">
            <div className="px-5 py-3.5 border-b border-slate-100 dark:border-dark-600 flex items-center justify-between">
              <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Posta Kutuları")}</h2>
              <span className="text-xs text-slate-400 tabular-nums">{kutular?.length ?? 0} {cevir("kutu")}</span>
            </div>
            {kutular === null && <div className="py-10 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>}
            {kutular?.length === 0 && (
              <div className="py-12 text-center">
                <div className="w-10 h-10 mx-auto mb-2 rounded-lg bg-slate-50 dark:bg-dark-600/50 flex items-center justify-center text-slate-300 dark:text-slate-500"><Ikon d={I.mail} className="w-5 h-5" /></div>
                <p className="text-sm text-slate-400">{cevir("Henüz posta kutusu yok. Yukarıdan ekleyin.")}</p>
              </div>
            )}
            {kutular && kutular.length > 0 && (
              <ul className="divide-y divide-slate-100 dark:divide-dark-600/70">
                {kutular.map(k => (
                  <li key={k.id} className="px-4 sm:px-5 py-4 hover:bg-slate-50/70 dark:hover:bg-dark-600/20 transition-colors">
                    {/* 3 sütun: kimlik · ORTADA kota barı · butonlar. Dar ekranda
                        alt alta yığılır (mobilde sıkışmaz). */}
                    <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:gap-6">
                      {/* Kimlik */}
                      <div className="flex items-center gap-3 min-w-0 lg:w-[340px] lg:shrink-0">
                        <div className="w-10 h-10 shrink-0 rounded-lg bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300">
                          <Ikon d={I.mail} className="w-5 h-5" />
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
                              ? <Ikon d={I.onay} className="w-3.5 h-3.5 shrink-0 text-emerald-500" />
                              : <Ikon d={I.kopya} className="w-3.5 h-3.5 shrink-0 text-slate-300 dark:text-slate-600 opacity-0 group-hover:opacity-100 transition-opacity" />}
                          </button>
                          {!k.aktif && <Badge color="warning" variant="soft" className="shrink-0 text-[10px] uppercase">{cevir("Askıda")}</Badge>}
                        </div>
                      </div>
                      {/* Kota barı — satırın ortasında */}
                      <div className="flex-1 min-w-0 flex lg:justify-center">
                        <KotaBar kullanilan={k.kullanilan_bytes} kota={k.quota_bytes} />
                      </div>
                      {/* Aksiyonlar */}
                      <div className="flex items-center gap-2 flex-wrap lg:shrink-0 lg:justify-end">
                        <Button onClick={() => webmailGiris(k)} disabled={isleniyor || !k.aktif || !!webmailKapali} title={webmailKapali ? cevir("Webmail kullanılamıyor:") + ' ' + webmailKapali : k.aktif ? cevir("Webmail'e tek tıkla giriş") : cevir("Askıdaki kutuya giriş yapılamaz")}
                          color="primary" boyut="sm" ikonSonda={I.harici}>
                          {cevir("Giriş")}
                        </Button>
                        <Button onClick={() => parolaUret(k)} disabled={isleniyor} title={cevir("Yeni güçlü parola üret (şifre sıfırla)")}
                          variant="outlined" boyut="sm" ikon={I.anahtar}>
                          {cevir("Şifre sıfırla")}
                        </Button>
                        <Button onClick={() => navigate(`/abonelikler/${id}/mail/kutular/${k.id}`)} title={cevir("Kutu detayları: oto-yanıt, iletim, taşıma, entegrasyon, içe/dışa aktar")}
                          variant="outlined" boyut="sm" ikon={I.ayar}>
                          {cevir("Detaylar")}
                        </Button>
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
      <Modal acik={!!uretilen} baslik={cevir("Yeni parola üretildi")} ton="basari" genislik="sm" onKapat={() => setUretilen(null)}
        alt={<Button onClick={() => setUretilen(null)} variant="outlined" ikon={I.kapat}>{cevir("Kapat")}</Button>}>
        {uretilen && (
          <>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">
              <span className="font-mono text-slate-700 dark:text-slate-300">{uretilen.email}</span> {cevir("— bu parola yalnızca şimdi gösteriliyor, güvenli bir yere kaydedin.")}
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 font-mono text-sm bg-slate-100 dark:bg-dark-800 rounded-lg px-3 py-2.5 break-all select-all text-slate-800 dark:text-slate-100">{uretilen.parola}</code>
              <Button
                onClick={() => { navigator.clipboard?.writeText(uretilen.parola); setKopyalandi(true) }}
                color="primary" ikon={kopyalandi ? I.onay : I.kopya} className="shrink-0">
                {kopyalandi ? cevir("Kopyalandı") : cevir("Kopyala")}
              </Button>
            </div>
          </>
        )}
      </Modal>
    </div>
  )
}
