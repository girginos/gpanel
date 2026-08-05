import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import type { Domain } from '@/components/DomainList'

type MailDomain = { id: number; ad: string; kutu_sayisi: number }
type Kutu = { id: number; email: string; domain_id: number; quota_bytes: number; kullanilan_bytes: number; aktif: boolean }

// Client-side güçlü parola üretici (yeni kutu formu için — kripto-güvenli).
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
}

// Kota kullanım barı — kullanılan / toplam (0 kota = sınırsız).
function KotaBar({ kullanilan, kota }: { kullanilan: number; kota: number }) {
  const kul = Math.max(0, kullanilan)
  if (!kota) {
    return <div className="text-xs text-slate-500 dark:text-slate-400">{boyut(kul)} kullanılıyor · <span className="text-slate-400 dark:text-slate-500">Sınırsız kota</span></div>
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
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [mailDomain, setMailDomain] = useState<MailDomain | null | undefined>(undefined) // undefined=yükleniyor, null=yok
  const [kutular, setKutular] = useState<Kutu[] | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  // Webmail tek-tık giriş kullanılamıyorsa SEBEBİ kalıcı olarak gösterilir.
  // 🔴 Sessiz başarısızlık YASAK: kullanıcı butona basıp hiçbir şey olmamasını
  // değil, "neden olmadığını" görmeli.
  const [webmailKapali, setWebmailKapali] = useState<string | null>(null)
  const [bildirim, setBildirim] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [uretilen, setUretilen] = useState<{ email: string; parola: string } | null>(null)
  const [kopyalandi, setKopyalandi] = useState(false)
  const [otoKutu, setOtoKutu] = useState<Kutu | null>(null)
  const [otoForm, setOtoForm] = useState({ aktif: false, konu: '', mesaj: '' })
  const [otoYukleniyor, setOtoYukleniyor] = useState(false)

  // Yeni kutu formu
  const [yeniKullanici, setYeniKullanici] = useState('')
  const [yeniParola, setYeniParola] = useState('')
  const [yeniQuota, setYeniQuota] = useState(1024)

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e, 'Domain yüklenemedi')))
  }, [id])

  function mailDomainYukle(d: Domain) {
    api.get<{ domainler: MailDomain[] }>('/eklenti/mail/domainler')
      .then(r => {
        const md = (r.data.domainler || []).find(x => x.ad.toLowerCase() === d.alan_adi.toLowerCase())
        setMailDomain(md || null)
      })
      .catch(e => { setHata(apiHata(e, 'Mail domainleri alınamadı')); setMailDomain(null) })
  }
  useEffect(() => { if (domain) mailDomainYukle(domain) }, [domain])

  function kutulariYukle(did: number) {
    setKutular(null)
    api.get<{ hesaplar: Kutu[] }>(`/eklenti/mail/hesaplar?domain=${did}`)
      .then(r => setKutular(r.data.hesaplar || []))
      .catch(e => { setHata(apiHata(e, 'Kutular alınamadı')); setKutular([]) })
  }
  useEffect(() => { if (mailDomain) kutulariYukle(mailDomain.id) }, [mailDomain])

  async function mailDomainOlustur() {
    if (!domain) return
    setIsleniyor(true); setHata(null)
    try {
      await api.post('/eklenti/mail/domainler', { ad: domain.alan_adi })
      setBildirim('✓ Mail domaini oluşturuldu. Artık kutu ekleyebilirsiniz.')
      mailDomainYukle(domain)
    } catch (e) { setHata(apiHata(e, 'Mail domaini oluşturulamadı')) }
    finally { setIsleniyor(false) }
  }

  async function kutuEkle(e: React.FormEvent) {
    e.preventDefault()
    if (!mailDomain || !domain) return
    const email = `${yeniKullanici.trim().toLowerCase()}@${domain.alan_adi}`
    setIsleniyor(true); setHata(null); setBildirim(null)
    try {
      await api.post('/eklenti/mail/hesaplar', { domain_id: mailDomain.id, email, parola: yeniParola, quota_mb: yeniQuota })
      setBildirim(`✓ ${email} oluşturuldu.`)
      setYeniKullanici(''); setYeniParola('')
      kutulariYukle(mailDomain.id)
    } catch (er) { setHata(apiHata(er, 'Kutu eklenemedi (parola min 6 karakter olmalı)')) }
    finally { setIsleniyor(false) }
  }

  async function kutuSil(k: Kutu) {
    if (!window.confirm(`"${k.email}" posta kutusu ve TÜM e-postaları kalıcı silinecek. Emin misiniz?`)) return
    setIsleniyor(true); setHata(null)
    try {
      await api.delete(`/eklenti/mail/hesaplar/${k.id}`)
      setBildirim(`✓ ${k.email} silindi.`)
      if (mailDomain) kutulariYukle(mailDomain.id)
    } catch (e) { setHata(apiHata(e, 'Silinemedi')) }
    finally { setIsleniyor(false) }
  }

  async function parolaDegistir(k: Kutu) {
    const p = window.prompt(`${k.email} için yeni parola (min 6):`)
    if (!p) return
    setIsleniyor(true); setHata(null)
    try {
      await api.put(`/eklenti/mail/hesaplar/${k.id}/parola`, { parola: p })
      setBildirim(`✓ ${k.email} parolası güncellendi.`)
    } catch (e) { setHata(apiHata(e, 'Parola değiştirilemedi')) }
    finally { setIsleniyor(false) }
  }

  // Tek-tık güçlü parola üret (düz metin bir kez modalda gösterilir).
  async function parolaUret(k: Kutu) {
    if (!window.confirm(`${k.email} için yeni güçlü parola üretilsin mi? Mevcut parola geçersiz olur.`)) return
    setIsleniyor(true); setHata(null)
    try {
      const r = await api.post<{ email: string; parola: string }>(`/eklenti/mail/hesaplar/${k.id}/parola-uret`)
      setKopyalandi(false)
      setUretilen({ email: r.data.email, parola: r.data.parola })
    } catch (e) { setHata(apiHata(e, 'Parola üretilemedi')) }
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
      const mesaj = apiHata(e, 'Webmail girişi açılamadı')
      // 503 = webmail sunucuda kurulu/yapılandırılmış değil (geçici bir hata değil).
      if ((e as { response?: { status?: number } })?.response?.status === 503) setWebmailKapali(mesaj)
      setHata(mesaj)
    }
    finally { setIsleniyor(false) }
  }

  // Otomatik yanıt (tatil) modalını aç — mevcut ayarı yükle.
  async function otoAc(k: Kutu) {
    setOtoKutu(k); setOtoForm({ aktif: false, konu: '', mesaj: '' }); setOtoYukleniyor(true); setHata(null)
    try {
      const r = await api.get<{ aktif: boolean; konu: string; mesaj: string }>(`/eklenti/mail/hesaplar/${k.id}/otomatik-yanit`)
      setOtoForm({ aktif: r.data.aktif, konu: r.data.konu || '', mesaj: r.data.mesaj || '' })
    } catch (e) { setHata(apiHata(e, 'Otomatik yanıt yüklenemedi')) }
    finally { setOtoYukleniyor(false) }
  }
  async function otoKaydet() {
    if (!otoKutu) return
    if (otoForm.aktif && !otoForm.mesaj.trim()) { setHata('Otomatik yanıt için mesaj boş olamaz'); return }
    setOtoYukleniyor(true); setHata(null)
    try {
      await api.put(`/eklenti/mail/hesaplar/${otoKutu.id}/otomatik-yanit`, otoForm)
      setBildirim(`✓ ${otoKutu.email} otomatik yanıtı ${otoForm.aktif ? 'açıldı' : 'kapatıldı'}.`)
      setOtoKutu(null)
    } catch (e) { setHata(apiHata(e, 'Otomatik yanıt kaydedilemedi')) }
    finally { setOtoYukleniyor(false) }
  }

  // Posta kutusunu askıya al / aktifleştir (giriş engellenir / açılır).
  async function aktifDegistir(k: Kutu) {
    const yeni = !k.aktif
    if (yeni === false && !window.confirm(`${k.email} askıya alınacak — kutu giriş yapamaz (postalar korunur). Emin misiniz?`)) return
    setIsleniyor(true); setHata(null)
    try {
      await api.put(`/eklenti/mail/hesaplar/${k.id}/aktif`, { aktif: yeni })
      setBildirim(`✓ ${k.email} ${yeni ? 'aktifleştirildi' : 'askıya alındı'}.`)
      if (mailDomain) kutulariYukle(mailDomain.id)
    } catch (e) { setHata(apiHata(e, 'Durum değiştirilemedi')) }
    finally { setIsleniyor(false) }
  }

  // Tek-tık kota onarımı — doveadm force-resync + kullanımı yeniden hesapla.
  async function kotaOnar(k: Kutu) {
    setIsleniyor(true); setHata(null)
    try {
      await api.post(`/eklenti/mail/hesaplar/${k.id}/kota-onar`)
      setBildirim(`✓ ${k.email} kotası onarıldı ve yeniden hesaplandı.`)
      if (mailDomain) kutulariYukle(mailDomain.id)
    } catch (e) { setHata(apiHata(e, 'Kota onarılamadı')) }
    finally { setIsleniyor(false) }
  }

  if (!domain) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div>
    </div>
  )

  const btnSec = 'inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1.5 rounded-lg border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 hover:border-slate-300 dark:hover:bg-slate-700/60 disabled:opacity-50 transition-colors'
  const btnPri = 'inline-flex items-center gap-1.5 text-xs font-medium px-3 py-1.5 rounded-lg bg-brand-600 hover:bg-brand-700 text-white shadow-sm shadow-brand-600/20 disabled:opacity-50 transition-colors'
  const btnDan = 'inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1.5 rounded-lg border border-red-200 dark:border-red-800/70 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 transition-colors'

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-5xl mx-auto">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain.alan_adi, href: `/abonelikler/${id}` },
        { etiket: 'Mail Kutuları' },
      ]} />
      <div className="flex items-start justify-between gap-4 mb-5">
        <div>
          <h1 className="text-2xl font-semibold text-brand-700 dark:text-brand-300 mb-1">Mail Kutuları</h1>
          <p className="text-sm text-slate-500 dark:text-slate-400">{domain.alan_adi} posta kutularını yönetin.</p>
        </div>
      </div>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {webmailKapali && (
        <div className="mb-3 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-sm text-amber-800 dark:text-amber-300">
          <b>Webmail kullanılamıyor.</b> {webmailKapali} — Posta akışı etkilenmez: gelen postalar kutuya düşer,
          IMAP/SMTP istemcileri (Outlook, telefon) çalışmaya devam eder. Yönetici: <span className="font-mono">Eklentiler → Mail</span>
          kurulum ekranındaki <span className="font-mono">webmail-*</span> adımlarına bakın.
        </div>
      )}

      {mailDomain === undefined && <div className="py-8 text-center text-sm text-slate-400">Yükleniyor…</div>}

      {mailDomain === null && (
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-8 text-center">
          <div className="w-12 h-12 mx-auto mb-3 rounded-xl bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300"><Ikon.zarf className="w-6 h-6" /></div>
          <p className="text-sm text-slate-600 dark:text-slate-300 mb-4">Bu domain için mail servisi henüz kurulmadı.</p>
          <button onClick={mailDomainOlustur} disabled={isleniyor}
            className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-4 py-2 rounded-lg disabled:opacity-60">
            {isleniyor ? 'Kuruluyor…' : 'Mail Servisini Kur'}
          </button>
        </div>
      )}

      {mailDomain && (
        <>
          {/* Yeni kutu */}
          <form onSubmit={kutuEkle} className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5 shadow-sm">
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">Yeni Posta Kutusu</h2>
            <div className="flex flex-wrap items-end gap-3">
              <div className="flex-1 min-w-[220px]">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">Kullanıcı adı</label>
                <div className="flex items-center">
                  <input value={yeniKullanici} onChange={e => setYeniKullanici(e.target.value)} required placeholder="ornek"
                    className="w-full rounded-l-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
                  <span className="px-3 py-2 text-sm bg-slate-50 dark:bg-slate-700 border border-l-0 border-slate-300 dark:border-slate-600 rounded-r-lg text-slate-500 dark:text-slate-400 whitespace-nowrap">@{domain.alan_adi}</span>
                </div>
              </div>
              <div className="min-w-[210px] flex-1 sm:flex-none">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">Parola (min 6)</label>
                <div className="flex items-center gap-1.5">
                  <input type="text" value={yeniParola} onChange={e => setYeniParola(e.target.value)} required minLength={6} placeholder="parola veya üret →"
                    className="w-full min-w-0 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
                  <button type="button" onClick={() => setYeniParola(guicluParola())} title="Güçlü parola üret"
                    className="shrink-0 inline-flex items-center gap-1 text-xs font-medium px-2.5 py-2 rounded-lg border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 hover:border-slate-300 dark:hover:bg-slate-700/60 transition-colors">
                    <Ikon.anahtar className="w-3.5 h-3.5" />Üret
                  </button>
                </div>
              </div>
              <div className="w-28">
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">Kota (MB)</label>
                <input type="number" min={0} value={yeniQuota} onChange={e => setYeniQuota(+e.target.value)}
                  className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
              </div>
              <button type="submit" disabled={isleniyor}
                className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-5 py-2 rounded-lg disabled:opacity-60 transition-colors">Ekle</button>
            </div>
          </form>

          {/* Kutu listesi */}
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden shadow-sm">
            <div className="px-5 py-3.5 border-b border-slate-100 dark:border-slate-700 flex items-center justify-between">
              <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Posta Kutuları</h2>
              <span className="text-xs text-slate-400 tabular-nums">{kutular?.length ?? 0} kutu</span>
            </div>
            {kutular === null && <div className="py-10 text-center text-sm text-slate-400">Yükleniyor…</div>}
            {kutular?.length === 0 && (
              <div className="py-12 text-center">
                <div className="w-10 h-10 mx-auto mb-2 rounded-lg bg-slate-50 dark:bg-slate-700/50 flex items-center justify-center text-slate-300 dark:text-slate-500"><Ikon.zarf className="w-5 h-5" /></div>
                <p className="text-sm text-slate-400">Henüz posta kutusu yok. Yukarıdan ekleyin.</p>
              </div>
            )}
            {kutular && kutular.length > 0 && (
              <ul className="divide-y divide-slate-100 dark:divide-slate-700/70">
                {kutular.map(k => (
                  <li key={k.id} className="px-4 sm:px-5 py-4 hover:bg-slate-50/70 dark:hover:bg-slate-700/20 transition-colors">
                    <div className="flex items-start gap-3.5 flex-wrap">
                      <div className="w-9 h-9 shrink-0 rounded-lg bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300">
                        <Ikon.zarf className="w-4.5 h-4.5" />
                      </div>
                      <div className="min-w-[180px] flex-1">
                        <div className="flex items-center gap-2">
                          <span className={`font-mono text-sm font-medium truncate ${k.aktif ? 'text-slate-800 dark:text-slate-100' : 'text-slate-400 dark:text-slate-500 line-through'}`}>{k.email}</span>
                          {!k.aktif && <span className="shrink-0 text-[10px] font-medium px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-400 uppercase">Askıda</span>}
                        </div>
                        <div className="mt-2"><KotaBar kullanilan={k.kullanilan_bytes} kota={k.quota_bytes} /></div>
                      </div>
                      <div className="flex items-center gap-1.5 flex-wrap justify-end">
                        <button onClick={() => webmailGiris(k)} disabled={isleniyor || !k.aktif || !!webmailKapali} title={webmailKapali ? 'Webmail kullanılamıyor: ' + webmailKapali : k.aktif ? "Webmail'e tek tıkla giriş" : 'Askıdaki kutuya giriş yapılamaz'} className={btnPri}>
                          <Ikon.giris className="w-3.5 h-3.5" />Giriş
                        </button>
                        <button onClick={() => aktifDegistir(k)} disabled={isleniyor} title={k.aktif ? 'Askıya al (giriş engelle)' : 'Aktifleştir'} className={btnSec}>
                          {k.aktif ? <><Ikon.duraklat className="w-3.5 h-3.5" />Askıya al</> : <><Ikon.oynat className="w-3.5 h-3.5" />Aktifleştir</>}
                        </button>
                        <button onClick={() => parolaUret(k)} disabled={isleniyor} title="Güçlü parola üret" className={btnSec}>
                          <Ikon.anahtar className="w-3.5 h-3.5" />Parola üret
                        </button>
                        <button onClick={() => parolaDegistir(k)} disabled={isleniyor} title="Parolayı elle değiştir" className={btnSec}>
                          <Ikon.kalem className="w-3.5 h-3.5" />Parola
                        </button>
                        <button onClick={() => otoAc(k)} disabled={isleniyor} title="Otomatik yanıt (tatil)" className={btnSec}>
                          <Ikon.cevap className="w-3.5 h-3.5" />Oto-yanıt
                        </button>
                        <button onClick={() => kotaOnar(k)} disabled={isleniyor} title="Kotayı onar / yeniden hesapla" className={btnSec}>
                          <Ikon.yenile className="w-3.5 h-3.5" />Kotayı onar
                        </button>
                        <button onClick={() => kutuSil(k)} disabled={isleniyor} title="Posta kutusunu sil" className={btnDan}>
                          <Ikon.cop className="w-3.5 h-3.5" />Sil
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

      {/* Otomatik yanıt (tatil) modalı */}
      {otoKutu && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4" onClick={() => setOtoKutu(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl p-6 max-w-md w-full shadow-2xl border border-slate-200 dark:border-slate-700" onClick={e => e.stopPropagation()}>
            <div className="flex items-center gap-2.5 mb-1">
              <div className="w-9 h-9 rounded-lg bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300"><Ikon.cevap className="w-5 h-5" /></div>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Otomatik Yanıt (Tatil)</h3>
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4 font-mono">{otoKutu.email}</p>
            <label className="flex items-center gap-2.5 mb-4 cursor-pointer">
              <input type="checkbox" checked={otoForm.aktif} onChange={e => setOtoForm(f => ({ ...f, aktif: e.target.checked }))}
                className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
              <span className="text-sm text-slate-700 dark:text-slate-200">Otomatik yanıtı etkinleştir</span>
            </label>
            <div className={`space-y-3 transition-opacity ${otoForm.aktif ? '' : 'opacity-50 pointer-events-none'}`}>
              <div>
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">Konu</label>
                <input value={otoForm.konu} onChange={e => setOtoForm(f => ({ ...f, konu: e.target.value }))} maxLength={255} placeholder="Ofis dışındayım"
                  className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5">Mesaj</label>
                <textarea value={otoForm.mesaj} onChange={e => setOtoForm(f => ({ ...f, mesaj: e.target.value }))} rows={5} maxLength={8000}
                  placeholder="Merhaba, şu an ofis dışındayım. En kısa sürede dönüş yapacağım."
                  className="w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm resize-y focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400" />
              </div>
              <p className="text-[11px] text-slate-400 dark:text-slate-500">Aynı göndericiye günde en fazla bir kez yanıt gönderilir.</p>
            </div>
            <div className="flex gap-2 mt-5">
              <button onClick={() => setOtoKutu(null)} className="flex-1 text-sm font-medium px-4 py-2 rounded-lg border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700">İptal</button>
              <button onClick={otoKaydet} disabled={otoYukleniyor} className="flex-1 text-sm font-medium px-4 py-2 rounded-lg bg-brand-600 hover:bg-brand-700 text-white disabled:opacity-60">{otoYukleniyor ? 'Kaydediliyor…' : 'Kaydet'}</button>
            </div>
          </div>
        </div>
      )}

      {/* Üretilen parola modalı */}
      {uretilen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm p-4" onClick={() => setUretilen(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl p-6 max-w-sm w-full shadow-2xl border border-slate-200 dark:border-slate-700" onClick={e => e.stopPropagation()}>
            <div className="flex items-center gap-2.5 mb-3">
              <div className="w-9 h-9 rounded-lg bg-emerald-50 dark:bg-emerald-900/30 flex items-center justify-center text-emerald-600 dark:text-emerald-400"><Ikon.anahtar className="w-5 h-5" /></div>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Yeni parola üretildi</h3>
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4">
              <span className="font-mono text-slate-700 dark:text-slate-300">{uretilen.email}</span> — bu parola yalnızca şimdi gösteriliyor, güvenli bir yere kaydedin.
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 font-mono text-sm bg-slate-100 dark:bg-slate-900 rounded-lg px-3 py-2.5 break-all select-all text-slate-800 dark:text-slate-100">{uretilen.parola}</code>
              <button
                onClick={() => { navigator.clipboard?.writeText(uretilen.parola); setKopyalandi(true) }}
                className="shrink-0 text-xs font-medium px-3 py-2.5 rounded-lg bg-brand-600 hover:bg-brand-700 text-white transition-colors">
                {kopyalandi ? '✓ Kopyalandı' : 'Kopyala'}
              </button>
            </div>
            <button onClick={() => setUretilen(null)}
              className="mt-4 w-full text-sm font-medium px-4 py-2 rounded-lg border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors">Kapat</button>
          </div>
        </div>
      )}
    </div>
  )
}
