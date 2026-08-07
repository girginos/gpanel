import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import type { Domain } from '@/components/DomainList'
import { useDialog } from '@/components/Dialog'
import { kaydet, oku, sil, GUN } from '@/lib/kalici'

type MailDomain = { id: number; ad: string; kutu_sayisi: number }
type Kutu = { id: number; email: string; domain_id: number; quota_bytes: number; kullanilan_bytes: number; aktif: boolean }
type Iletim = { aktif: boolean; hedef: string; kopya: boolean }
type SunucuBilgi = { host: string; port: number; guvenlik: string }
// 🔴 ALANLAR OPSİYONEL: eklenti sürümü panelden ESKİ olabilir (lisans sunucusundan
// inen paket geride kalmış olabilir). Eski sürüm ör. `smtps` döndürmez; zorunlu
// varsayıp `smtps.guvenlik` okumak TypeError atıp sayfayı BEYAZ bırakıyordu.
// Canlı gözlendi (144.76.189.1). Sürüm uyuşmazlığı sayfayı ASLA çökertmemeli.
type Baglanti = {
  domain: string
  imap?: SunucuBilgi; pop3?: SunucuBilgi; smtp?: SunucuBilgi; smtps?: SunucuBilgi
  webmail?: string; kullanici_adi?: string
}
type TasimaDurum = {
  durum: string; kaynak?: string; mesaj?: string; aktarilan?: number
  hata?: string; log?: string[]; imapsync?: boolean
}
type TasimaAday = {
  host: string; port: number; guvenlik: string; kaynak: string
  yanit: boolean; banner?: string; sure?: string
}

// Taslak — sekme kapansa bile 24 saat korunan form durumu.
// 🔴 Parola alanları BİLEREK yok: localStorage kalıcıdır ve XSS ile okunabilir.
type Taslak = {
  sekme?: 'genel' | 'oto' | 'iletim' | 'tasima' | 'aktar'
  elle?: boolean
  adaylar?: TasimaAday[]
  adaylarMesaj?: string
  tasima?: { host: string; port: number; guvenlik: string; kullanici: string }
}
const taslakAnahtar = (kutuId: number) => `kutu.${kutuId}`

// Taşıma sihirbazı adımları.
const ADIMLAR = [
  { n: 1, ad: 'Eski hesap' },
  { n: 2, ad: 'Sunucu' },
  { n: 3, ad: 'Doğrula & başlat' },
]

function boyut(b: number) {
  if (b < 0) return '—'
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`
  const mb = b / 1024 / 1024
  if (mb < 1024) return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
  return `${(mb / 1024).toFixed(1)} GB`
}
function guicluParola(uzunluk = 16) {
  const alfabe = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!@#$%*-_'
  const arr = new Uint32Array(uzunluk); crypto.getRandomValues(arr)
  let s = ''; for (let i = 0; i < uzunluk; i++) s += alfabe[arr[i] % alfabe.length]; return s
}

const Ikon = {
  giris: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M13 7l5 5m0 0l-5 5m5-5H6" /></svg>,
  anahtar: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M15 7a2 2 0 012 2m4-2a6 6 0 01-7.743 5.743L11 14H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z" /></svg>,
  kalem: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" /></svg>,
  yenile: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" /></svg>,
  duraklat: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M10 9v6m4-6v6m7-3a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>,
  oynat: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z" /><path strokeLinecap="round" strokeLinejoin="round" d="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>,
  cevap: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M3 10h10a8 8 0 018 8v2M3 10l6 6m-6-6l6-6" /></svg>,
  ilet: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M14 5l7 7m0 0l-7 7m7-7H10a7 7 0 00-7 7v0" /></svg>,
  tasima: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M8 7h12m0 0l-4-4m4 4l-4 4M16 17H4m0 0l4 4m-4-4l4-4" /></svg>,
  disari: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" /></svg>,
  iceri: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" /></svg>,
  fis: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" /></svg>,
  kopya: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>,
  onay: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" /></svg>,
  ayar: (p: { className?: string }) => <svg className={p.className} fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}><path strokeLinecap="round" strokeLinejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" /><path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>,
}

// Tek-tık kopyalanabilir değer satırı (entegrasyon kartı).
function KopyaSatir({ etiket, deger }: { etiket: string; deger: string }) {
  const [ok, setOk] = useState(false)
  return (
    <div className="flex items-center justify-between gap-3 py-2">
      <div className="min-w-0">
        <div className="text-[11px] uppercase tracking-wide text-slate-400 dark:text-slate-500">{etiket}</div>
        <div className="font-mono text-sm text-slate-800 dark:text-slate-100 truncate">{deger}</div>
      </div>
      <button
        onClick={() => { navigator.clipboard?.writeText(deger); setOk(true); setTimeout(() => setOk(false), 1500) }}
        title="Kopyala"
        className={`shrink-0 inline-flex items-center gap-1 text-xs font-medium px-2 py-1.5 rounded-lg border transition-colors ${ok ? 'border-emerald-300 text-emerald-600 dark:text-emerald-400 dark:border-emerald-700' : 'border-slate-200 dark:border-slate-600 text-slate-500 dark:text-slate-300 hover:bg-slate-50 hover:border-slate-300 dark:hover:bg-slate-700/60'}`}>
        {ok ? <><Ikon.onay className="w-3.5 h-3.5" />Kopyalandı</> : <><Ikon.kopya className="w-3.5 h-3.5" />Kopyala</>}
      </button>
    </div>
  )
}

export default function MailKutuDetayPage() {
  const { onay, sor } = useDialog()
  const { id, kutuId } = useParams()
  const navigate = useNavigate()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [mailDomain, setMailDomain] = useState<MailDomain | null>(null)
  const [kutu, setKutu] = useState<Kutu | null | undefined>(undefined) // undefined=yükleniyor
  const [hata, setHata] = useState<string | null>(null)
  const [bildirim, setBildirim] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [sekme, setSekme] = useState<'genel' | 'oto' | 'iletim' | 'tasima' | 'aktar'>('genel')

  // Modallar
  const [uretilen, setUretilen] = useState<{ email: string; parola: string } | null>(null)
  const [kopyalandi, setKopyalandi] = useState(false)

  // Oto-yanıt
  const [oto, setOto] = useState({ aktif: false, konu: '', mesaj: '' })
  // İletim
  const [iletim, setIletim] = useState<Iletim>({ aktif: false, hedef: '', kopya: true })
  // Entegrasyon
  const [baglanti, setBaglanti] = useState<Baglanti | null>(null)
  // Taşıma
  const [tasima, setTasima] = useState({ host: '', port: 993, guvenlik: 'ssl', kullanici: '', parola: '', hedef_parola: '' })
  const [tasimaDurum, setTasimaDurum] = useState<TasimaDurum | null>(null)
  const tasimaTimer = useRef<number | null>(null)
  // Taşıma sihirbazı: otomatik keşif / elle giriş + ön doğrulama.
  const [elle, setElle] = useState(false)
  const [adaylar, setAdaylar] = useState<TasimaAday[] | null>(null)
  const [adaylarMesaj, setAdaylarMesaj] = useState('')
  const [kesfediliyor, setKesfediliyor] = useState(false)
  const [dogrulama, setDogrulama] = useState<{ ok: boolean; mesaj?: string; hata?: string } | null>(null)
  const [dogrulaniyor, setDogrulaniyor] = useState(false)
  const [taslakVar, setTaslakVar] = useState(false)
  // Sihirbaz adımı (1..3). Taslakta SAKLANMAZ: parolalar saklanmadığı için
  // 3. adımdan devam etmek "doğrula" aşamasında kafa karıştırıcı hata verirdi;
  // her açılışta 1. adımdan başlanır (sunucu/aday seçimi korunur).
  const [adim, setAdim] = useState(1)

  const kid = Number(kutuId)

  // Domain → mail domain → kutu.
  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e, 'Domain yüklenemedi')))
  }, [id])
  useEffect(() => {
    if (!domain) return
    api.get<{ domainler: MailDomain[] }>('/eklenti/mail/domainler')
      .then(r => {
        const md = (r.data.domainler || []).find(x => x.ad.toLowerCase() === domain.alan_adi.toLowerCase())
        setMailDomain(md || null)
        if (!md) { setKutu(null); setHata('Bu domain için mail servisi kurulu değil.') }
      })
      .catch(e => { setHata(apiHata(e, 'Mail domaini alınamadı')); setKutu(null) })
  }, [domain])
  function kutuYukle(mdId: number) {
    api.get<{ hesaplar: Kutu[] }>(`/eklenti/mail/hesaplar?domain=${mdId}`)
      .then(r => setKutu((r.data.hesaplar || []).find(k => k.id === kid) || null))
      .catch(e => { setHata(apiHata(e, 'Kutu alınamadı')); setKutu(null) })
  }
  useEffect(() => { if (mailDomain) kutuYukle(mailDomain.id) }, [mailDomain])

  // Kutu geldiğinde oto-yanıt + iletim + taşıma durumunu yükle.
  useEffect(() => {
    if (!kutu) return
    api.get<{ aktif: boolean; konu: string; mesaj: string }>(`/eklenti/mail/hesaplar/${kutu.id}/otomatik-yanit`)
      .then(r => setOto({ aktif: r.data.aktif, konu: r.data.konu || '', mesaj: r.data.mesaj || '' })).catch(() => {})
    api.get<Iletim>(`/eklenti/mail/hesaplar/${kutu.id}/iletim`)
      .then(r => setIletim({ aktif: r.data.aktif, hedef: r.data.hedef || '', kopya: r.data.kopya })).catch(() => {})
    // Entegrasyon bilgileri Genel sekmesinde satır içi gösterilir (çekmece yok).
    if (domain) {
      api.get<Baglanti>(`/eklenti/mail/baglanti/${domain.alan_adi}?email=${encodeURIComponent(kutu.email)}`)
        .then(r => setBaglanti(r.data))
        .catch(e => setHata(apiHata(e, 'Entegrasyon bilgileri alınamadı')))
    }
    tasimaDurumCek(kutu.id)

    // Kaydedilmiş taslağı geri yükle (sekme + taşıma formu). Parolalar SAKLANMAZ,
    // bu yüzden mevcut parola alanları korunur ve kullanıcıdan yeniden istenir.
    const t = oku<Taslak>(taslakAnahtar(kutu.id))
    if (t) {
      if (t.sekme) setSekme(t.sekme)
      if (t.elle !== undefined) setElle(t.elle)
      if (t.adaylar) { setAdaylar(t.adaylar); setAdaylarMesaj(t.adaylarMesaj || '') }
      if (t.tasima) setTasima(v => ({ ...v, ...t.tasima, parola: v.parola, hedef_parola: v.hedef_parola }))
      setTaslakVar(true)
    }

    return () => { if (tasimaTimer.current) window.clearInterval(tasimaTimer.current) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kutu?.id])

  function bildir(m: string) { setBildirim(m); setHata(null); setTimeout(() => setBildirim(null), 4000) }

  // ── Genel ──
  async function aktifDegistir() {
    if (!kutu) return
    const yeni = !kutu.aktif
    if (!yeni && !(await onay({ baslik: 'Onay gerekiyor', mesaj: `${kutu.email} askıya alınacak — giriş yapamaz (postalar korunur). Emin misiniz?` }))) return
    setIsleniyor(true); setHata(null)
    try {
      await api.put(`/eklenti/mail/hesaplar/${kutu.id}/aktif`, { aktif: yeni })
      setKutu({ ...kutu, aktif: yeni }); bildir(`✓ ${kutu.email} ${yeni ? 'aktifleştirildi' : 'askıya alındı'}.`)
    } catch (e) { setHata(apiHata(e, 'Durum değiştirilemedi')) } finally { setIsleniyor(false) }
  }
  async function kotaOnar() {
    if (!kutu) return
    setIsleniyor(true); setHata(null)
    try {
      const r = await api.post<{ kullanilan_bytes: number }>(`/eklenti/mail/hesaplar/${kutu.id}/kota-onar`)
      setKutu({ ...kutu, kullanilan_bytes: r.data.kullanilan_bytes }); bildir('✓ Kota onarıldı ve yeniden hesaplandı.')
    } catch (e) { setHata(apiHata(e, 'Kota onarılamadı')) } finally { setIsleniyor(false) }
  }
  async function parolaUret() {
    if (!kutu) return
    if (!(await onay({ baslik: 'Onay gerekiyor', mesaj: `${kutu.email} için yeni güçlü parola üretilsin mi? Mevcut parola geçersiz olur.` }))) return
    setIsleniyor(true); setHata(null)
    try {
      const r = await api.post<{ email: string; parola: string }>(`/eklenti/mail/hesaplar/${kutu.id}/parola-uret`)
      setKopyalandi(false); setUretilen({ email: r.data.email, parola: r.data.parola })
    } catch (e) { setHata(apiHata(e, 'Parola üretilemedi')) } finally { setIsleniyor(false) }
  }
  async function parolaElle() {
    if (!kutu) return
    const p = (await sor({ baslik: `${kutu.email} için yeni parola (min 6):` }))
    if (!p) return
    setIsleniyor(true); setHata(null)
    try { await api.put(`/eklenti/mail/hesaplar/${kutu.id}/parola`, { parola: p }); bildir('✓ Parola güncellendi.') }
    catch (e) { setHata(apiHata(e, 'Parola değiştirilemedi')) } finally { setIsleniyor(false) }
  }
  async function webmailGiris() {
    if (!kutu) return
    const w = window.open('about:blank', '_blank'); setIsleniyor(true); setHata(null)
    try {
      const r = await api.post<{ url: string }>(`/eklenti/mail/hesaplar/${kutu.id}/giris`)
      if (w) w.location.href = r.data.url; else window.location.href = r.data.url
    } catch (e) { if (w) w.close(); setHata(apiHata(e, 'Webmail girişi açılamadı')) } finally { setIsleniyor(false) }
  }
  async function kutuSil() {
    if (!kutu) return
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: `"${kutu.email}" posta kutusu ve TÜM e-postaları kalıcı silinecek. Emin misiniz?`, tehlike: true }))) return
    setIsleniyor(true); setHata(null)
    try { await api.delete(`/eklenti/mail/hesaplar/${kutu.id}`); navigate(`/abonelikler/${id}/mail/kutular`) }
    catch (e) { setHata(apiHata(e, 'Silinemedi')); setIsleniyor(false) }
  }

  // ── Oto-yanıt ──
  async function otoKaydet() {
    if (!kutu) return
    if (oto.aktif && !oto.mesaj.trim()) { setHata('Otomatik yanıt için mesaj boş olamaz'); return }
    setIsleniyor(true); setHata(null)
    try { await api.put(`/eklenti/mail/hesaplar/${kutu.id}/otomatik-yanit`, oto); bildir(`✓ Otomatik yanıt ${oto.aktif ? 'açıldı' : 'kapatıldı'}.`) }
    catch (e) { setHata(apiHata(e, 'Otomatik yanıt kaydedilemedi')) } finally { setIsleniyor(false) }
  }

  // ── İletim ──
  async function iletimKaydet() {
    if (!kutu) return
    if (iletim.aktif && !iletim.hedef.trim()) { setHata('İletim için en az bir hedef e-posta girin'); return }
    setIsleniyor(true); setHata(null)
    try {
      const r = await api.put<Iletim & { durum: string }>(`/eklenti/mail/hesaplar/${kutu.id}/iletim`, iletim)
      setIletim({ aktif: r.data.aktif, hedef: r.data.hedef || '', kopya: r.data.kopya })
      bildir(`✓ İletim ${r.data.aktif ? 'açıldı' : 'kapatıldı'}.`)
    } catch (e) { setHata(apiHata(e, 'İletim kaydedilemedi')) } finally { setIsleniyor(false) }
  }

  // ── Dışa / İçe aktar ──
  async function disariAktar() {
    if (!kutu) return
    setIsleniyor(true); setHata(null)
    try {
      const r = await api.get(`/eklenti/mail/hesaplar/${kutu.id}/disari-aktar`, { responseType: 'blob' })
      const url = URL.createObjectURL(r.data as Blob)
      const a = document.createElement('a')
      a.href = url; a.download = `${kutu.email.replace('@', '_at_')}-maildir.tar.gz`
      document.body.appendChild(a); a.click(); a.remove(); URL.revokeObjectURL(url)
      bildir('✓ Yedek indiriliyor.')
    } catch (e) { setHata(apiHata(e, 'Dışa aktarma başarısız')) } finally { setIsleniyor(false) }
  }
  const dosyaRef = useRef<HTMLInputElement | null>(null)
  async function iceriAktar(dosya: File) {
    if (!kutu) return
    setIsleniyor(true); setHata(null)
    try {
      const fd = new FormData(); fd.append('dosya', dosya)
      const r = await api.post<{ eklenen: number }>(`/eklenti/mail/hesaplar/${kutu.id}/iceri-aktar`, fd, { timeout: 300_000 })
      const n = r.data.eklenen
      bildir(n >= 0 ? `✓ İçe aktarıldı — ${n} mesaj eklendi.` : '✓ İçe aktarıldı.')
      kutuYukle(mailDomain!.id)
    } catch (e) { setHata(apiHata(e, 'İçe aktarma başarısız')) } finally { setIsleniyor(false); if (dosyaRef.current) dosyaRef.current.value = '' }
  }

  // ── Taşıma ──
  // Durumu çeker; 'calisiyor' ise polling'i KENDİ başlatır (sayfa açıkken taşıma
  // sürüyorsa donmaz), bittiğinde durdurur. Böylece mount'ta da doğru devam eder.
  function tasimaDurumCek(kutuId: number) {
    api.get<TasimaDurum>(`/eklenti/mail/hesaplar/${kutuId}/tasima`).then(r => {
      setTasimaDurum(r.data)
      if (r.data.durum === 'calisiyor') {
        if (!tasimaTimer.current) tasimaTimer.current = window.setInterval(() => tasimaDurumCek(kutuId), 2000)
      } else if (tasimaTimer.current) {
        window.clearInterval(tasimaTimer.current); tasimaTimer.current = null
      }
    }).catch(() => {})
  }
  // Kaydedilmiş taslağı sil ve formu sıfırla.
  function taslagiTemizle() {
    if (kutu) sil(taslakAnahtar(kutu.id))
    setTaslakVar(false)
    setTasima({ host: '', port: 993, guvenlik: 'ssl', kullanici: '', parola: '', hedef_parola: '' })
    setAdaylar(null); setAdaylarMesaj(''); setDogrulama(null); setElle(false)
    setAdim(1)
  }

  // Eski e-posta adresinden sunucu ayarlarını otomatik keşfet (autoconfig/SRV/MX).
  async function sunucuKesfet() {
    setKesfediliyor(true); setHata(null); setDogrulama(null)
    try {
      const r = await api.post<{ adaylar: TasimaAday[]; mesaj: string }>(
        '/eklenti/mail/tasima/kesfet', { email: tasima.kullanici }, { timeout: 60_000 })
      const liste = r.data.adaylar || []
      setAdaylar(liste); setAdaylarMesaj(r.data.mesaj || '')
      // Yanıt veren ilk adayı otomatik seç (kullanıcı değiştirebilir).
      const ilk = liste.find(a => a.yanit)
      if (ilk) setTasima(t => ({ ...t, host: ilk.host, port: ilk.port, guvenlik: ilk.guvenlik }))
      else setElle(true) // hiçbiri yanıt vermediyse elle girişe geç
    } catch (e) { setHata(apiHata(e, 'Sunucu araması başarısız')) }
    finally { setKesfediliyor(false) }
  }

  // Taşımadan ÖNCE gerçek IMAP girişi dene — hatayı 2 saat sonra değil şimdi göster.
  async function baglantiDogrula() {
    setDogrulaniyor(true); setHata(null)
    try {
      const r = await api.post<{ ok: boolean; mesaj?: string; hata?: string }>(
        '/eklenti/mail/tasima/dogrula',
        { host: tasima.host, port: tasima.port, guvenlik: tasima.guvenlik, kullanici: tasima.kullanici, parola: tasima.parola },
        { timeout: 60_000 })
      setDogrulama(r.data)
    } catch (e) { setHata(apiHata(e, 'Doğrulama başarısız')) }
    finally { setDogrulaniyor(false) }
  }

  async function tasimaBaslat() {
    if (!kutu) return
    if (!tasima.host || !tasima.kullanici || !tasima.parola || !tasima.hedef_parola) { setHata('Uzak sunucu, kullanıcı, uzak parola ve hedef kutu parolası zorunlu'); return }
    setIsleniyor(true); setHata(null)
    try {
      await api.post(`/eklenti/mail/hesaplar/${kutu.id}/tasima`, tasima)
      bildir('✓ Taşıma başladı — ilerleme aşağıda görünecek.')
      tasimaDurumCek(kutu.id) // 'calisiyor' görünce polling'i kendi başlatır
    } catch (e) { setHata(apiHata(e, 'Taşıma başlatılamadı')) } finally { setIsleniyor(false) }
  }

  // Taslağı otomatik kaydet (sekme + taşıma formu, parolasız). 24 saat yaşar.
  //
  // 🔴 HOOK'LAR ERKEN return'DEN ÖNCE OLMAK ZORUNDA: bu useEffect aşağıdaki
  // `if (kutu === undefined) return` satırının ALTINDAYDI. İlk render'da kutu
  // henüz yüklenmediği için erken çıkılıyor ve hook hiç çalışmıyordu; kutu
  // gelince render devam edip fazladan bir hook çağrılıyordu →
  // "Rendered more hooks than during the previous render" → React ağacı
  // çöküyor ve sayfa BEYAZ kalıyordu. (Canlı gözlendi.)
  useEffect(() => {
    if (!kutu) return
    kaydet<Taslak>(taslakAnahtar(kutu.id), {
      sekme, elle, adaylar: adaylar || undefined, adaylarMesaj,
      tasima: { host: tasima.host, port: tasima.port, guvenlik: tasima.guvenlik, kullanici: tasima.kullanici },
    }, GUN)
  }, [kutu, sekme, elle, adaylar, adaylarMesaj, tasima.host, tasima.port, tasima.guvenlik, tasima.kullanici])

  const bcDom = domain?.alan_adi || '…'
  if (kutu === undefined) return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-7xl mx-auto">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' }]} />
      <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div>
    </div>
  )

  const btnSec = 'inline-flex items-center gap-1.5 text-xs font-medium px-3 py-2 rounded-lg border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 hover:border-slate-300 dark:hover:bg-slate-700/60 disabled:opacity-50 transition-colors'
  const btnPri = 'inline-flex items-center gap-1.5 text-xs font-medium px-3.5 py-2 rounded-lg bg-brand-600 hover:bg-brand-700 text-white shadow-sm shadow-brand-600/20 disabled:opacity-50 transition-colors'
  const kart = 'bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl shadow-sm'
  const etiket = 'block text-xs font-medium text-slate-500 dark:text-slate-400 mb-1.5'
  const girdi = 'w-full rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400'

  // saglayiciUyarisi — büyük sağlayıcılar parolayla IMAP'i kısıtladı. Kullanıcı
  // parolayı DENEMEDEN önce uyarılmalı; aksi halde "parolam yanlış" sanıp
  // defalarca dener. (Sunucu da giriş reddinde aynı yönlendirmeyi döner.)
  const saglayiciUyarisi: { agir: boolean; baslik: string; metin: string; pst?: boolean } | null = (() => {
    const h = `${tasima.host || ''} ${tasima.kullanici || ''}`.toLowerCase()
    if (/office365|outlook|hotmail|live\.com|msn\./.test(h))
      return {
        agir: true, pst: true,
        baslik: 'Bu hesap parolayla taşınamaz',
        metin: 'Microsoft, Outlook.com / Hotmail / Microsoft 365 hesaplarında parolayla IMAP erişimini (Basic Auth) kapattı. ' +
          'Parolanız doğru olsa bile sunucu reddeder — uygulama şifresi de üretilemez. ' +
          'Bunun yerine Outlook’ta Dosya → Aç ve Dışa Aktar → Dışa aktar ile bir .pst dosyası oluşturup içe aktarın.',
      }
    if (/gmail|googlemail/.test(h))
      return {
        agir: false,
        baslik: 'Gmail normal parolayı kabul etmez',
        metin: 'Google hesabınızda 2 Adımlı Doğrulama’yı açın, ardından bir “Uygulama Şifresi” üretip parola alanına ONU yazın. Hesabın kendi parolası çalışmaz.',
      }
    if (/yahoo|aol\./.test(h))
      return {
        agir: false,
        baslik: 'Yahoo/AOL uygulama şifresi ister',
        metin: 'Hesap güvenlik ayarlarından bir “Uygulama Şifresi” üretip parola alanına onu yazın.',
      }
    if (/yandex/.test(h))
      return {
        agir: false,
        baslik: 'Yandex uygulama parolası ister',
        metin: 'Yandex ID → Güvenlik → Uygulama parolaları bölümünden bir parola üretip onu kullanın.',
      }
    return null
  })()

  // Uyarı şeridi — 2. ve 3. adımda aynı bileşen (kullanıcı parolayı DENEMEDEN görür).
  const UyariSeridi = () => saglayiciUyarisi ? (
    <div className={`mt-3 rounded-xl border px-4 py-3 text-xs leading-relaxed ${saglayiciUyarisi.agir
      ? 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800 text-red-700 dark:text-red-300'
      : 'bg-amber-50 dark:bg-amber-900/20 border-amber-200 dark:border-amber-800 text-amber-800 dark:text-amber-300'}`}>
      <div className="font-semibold mb-1">{saglayiciUyarisi.agir ? '⛔ ' : '⚠ '}{saglayiciUyarisi.baslik}</div>
      <p>{saglayiciUyarisi.metin}</p>
      {saglayiciUyarisi.pst && (
        <button onClick={() => setSekme('aktar')}
          className="mt-2.5 inline-flex items-center gap-1.5 text-xs font-medium px-3 py-1.5 rounded-lg bg-red-600 hover:bg-red-700 text-white transition-colors">
          <Ikon.iceri className="w-3.5 h-3.5" />.pst içe aktarmaya git
        </button>
      )}
    </div>
  ) : null

  // adimTamam — o adım "Devam" için yeterli mi (ileri gitmeyi kapıda tutar).
  function adimTamam(n: number) {
    if (n === 1) return tasima.kullanici.includes('@') && tasima.parola.length > 0
    if (n === 2) return tasima.host.trim().length > 0
    return true
  }

  const sekmeler: { k: typeof sekme; ad: string; ikon: (p: { className?: string }) => JSX.Element }[] = [
    { k: 'genel', ad: 'Genel', ikon: Ikon.ayar },
    { k: 'oto', ad: 'Oto-Yanıt', ikon: Ikon.cevap },
    { k: 'iletim', ad: 'İletim', ikon: Ikon.ilet },
    { k: 'tasima', ad: 'Taşıma', ikon: Ikon.tasima },
    { k: 'aktar', ad: 'İçe / Dışa Aktar', ikon: Ikon.disari },
  ]

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-7xl mx-auto">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: bcDom, href: `/abonelikler/${id}` },
        { etiket: 'Mail Kutuları', href: `/abonelikler/${id}/mail/kutular` },
        { etiket: kutu?.email || 'Kutu' },
      ]} />

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {kutu === null ? (
        <div className={`${kart} p-8 text-center`}>
          <p className="text-sm text-slate-500 dark:text-slate-300 mb-4">Posta kutusu bulunamadı.</p>
          <Link to={`/abonelikler/${id}/mail/kutular`} className={btnPri}>← Mail Kutularına dön</Link>
        </div>
      ) : (
        <>
          {/* Başlık kartı */}
          <div className={`${kart} p-5 mb-4`}>
            {/* 3 sütun: kimlik · ORTADA kota barı · aksiyonlar */}
            <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:gap-6">
              <div className="flex items-center gap-3 min-w-0 lg:w-[320px] lg:shrink-0">
                <div className="w-11 h-11 shrink-0 rounded-xl bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300">
                  <Ikon.cevap className="w-5 h-5" />
                </div>
                <div className="flex items-center gap-2 min-w-0">
                  <h1 className={`font-mono text-lg font-semibold truncate ${kutu.aktif ? 'text-slate-900 dark:text-slate-100' : 'text-slate-400 line-through'}`}>{kutu.email}</h1>
                  {kutu.aktif
                    ? <span className="shrink-0 text-[10px] font-semibold px-1.5 py-0.5 rounded bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-400 uppercase">Aktif</span>
                    : <span className="shrink-0 text-[10px] font-semibold px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-400 uppercase">Askıda</span>}
                </div>
              </div>
              {/* Kota barı — e-posta ile butonların ortasında */}
              <div className="flex-1 min-w-0 flex items-center gap-2 lg:justify-center">
                <KotaBar kullanilan={kutu.kullanilan_bytes} kota={kutu.quota_bytes} />
                <button onClick={kotaOnar} disabled={isleniyor} title="Kotayı onar / yeniden hesapla"
                  className="shrink-0 grid h-8 w-8 place-items-center rounded-lg border border-slate-200 dark:border-slate-600 text-slate-500 dark:text-slate-400 hover:bg-slate-50 hover:border-slate-300 dark:hover:bg-slate-700/60 disabled:opacity-50 transition-colors">
                  <Ikon.yenile className="w-4 h-4" />
                </button>
              </div>
              <div className="flex items-center gap-2 flex-wrap lg:shrink-0 lg:justify-end">
                <button onClick={webmailGiris} disabled={isleniyor || !kutu.aktif} className={btnPri}><Ikon.giris className="w-4 h-4" />Giriş</button>
                <button onClick={aktifDegistir} disabled={isleniyor} className={btnSec}>
                  {kutu.aktif ? <><Ikon.duraklat className="w-4 h-4" />Askıya al</> : <><Ikon.oynat className="w-4 h-4" />Aktifleştir</>}
                </button>
              </div>
            </div>
          </div>

          {/* Sekmeler */}
          <div className="flex items-center gap-1 mb-4 border-b border-slate-200 dark:border-slate-700 overflow-x-auto">
            {sekmeler.map(s => (
              <button key={s.k} onClick={() => setSekme(s.k)}
                className={`inline-flex items-center gap-1.5 px-3.5 py-2.5 text-sm font-medium whitespace-nowrap border-b-2 -mb-px transition-colors ${sekme === s.k ? 'border-brand-500 text-brand-700 dark:text-brand-300' : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200'}`}>
                <s.ikon className="w-4 h-4" />{s.ad}
              </button>
            ))}
          </div>

          {/* GENEL */}
          {sekme === 'genel' && (
            <div className={`${kart} p-5 space-y-5`}>
              {/* Mail entegrasyonu — istemci kurulum bilgileri (tek tık kopya) */}
              <div>
                <div className="flex items-center gap-2.5 mb-1">
                  <div className="w-9 h-9 shrink-0 rounded-lg bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300"><Ikon.fis className="w-5 h-5" /></div>
                  <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Mail Entegrasyonu</h3>
                </div>
                <p className="text-xs text-slate-400 mb-4">
                  Outlook, Thunderbird, telefon vb. istemcilere bu bilgileri girin. Kullanıcı adı ve parola tüm sunucularda aynıdır.
                </p>
                {!baglanti ? (
                  <div className="py-6 text-center text-sm text-slate-400">Entegrasyon bilgileri yükleniyor…</div>
                ) : (
                  <div className="space-y-4">
                    <div className="rounded-xl border border-slate-200 dark:border-slate-700 divide-y divide-slate-100 dark:divide-slate-700/70 px-4">
                      <KopyaSatir etiket="Kullanıcı adı" deger={baglanti.kullanici_adi || kutu.email} />
                    </div>
                    <div className="grid gap-4 sm:grid-cols-2">
                      {/* Eklenti sürümü eskiyse bazı alanlar HİÇ gelmez — eksik
                          olanı çiz(me)mek gerekir, okumaya kalkmak sayfayı çökertir. */}
                      {([
                        ['Gelen — IMAP', baglanti.imap],
                        ['Gelen — POP3', baglanti.pop3],
                        ['Giden — SMTP (STARTTLS)', baglanti.smtp],
                        ['Giden — SMTP (SSL)', baglanti.smtps],
                      ] as [string, SunucuBilgi | undefined][])
                        .filter((x): x is [string, SunucuBilgi] => !!x[1] && typeof x[1].host === 'string')
                        .map(([ad, s]) => (
                        <div key={ad}>
                          <div className="text-xs font-semibold text-slate-600 dark:text-slate-300 mb-1.5">
                            {ad} <span className="ml-1 text-[10px] font-normal px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400">{s.guvenlik}</span>
                          </div>
                          <div className="rounded-xl border border-slate-200 dark:border-slate-700 divide-y divide-slate-100 dark:divide-slate-700/70 px-4">
                            <KopyaSatir etiket="Sunucu" deger={s.host} />
                            <KopyaSatir etiket="Port" deger={String(s.port)} />
                          </div>
                        </div>
                      ))}
                    </div>
                    {baglanti.webmail && (
                      <div className="rounded-xl border border-slate-200 dark:border-slate-700 divide-y divide-slate-100 dark:divide-slate-700/70 px-4">
                        <KopyaSatir etiket="Webmail" deger={baglanti.webmail} />
                      </div>
                    )}
                  </div>
                )}
              </div>
              <div className="border-t border-slate-100 dark:border-slate-700 pt-5">
                <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-2">Parola</h3>
                <p className="text-xs text-slate-400 mb-3">Kutunun IMAP/SMTP/webmail parolasını sıfırlayın.</p>
                <div className="flex items-center gap-2 flex-wrap">
                  <button onClick={parolaUret} disabled={isleniyor} className={btnPri}><Ikon.anahtar className="w-3.5 h-3.5" />Güçlü parola üret</button>
                  <button onClick={parolaElle} disabled={isleniyor} className={btnSec}><Ikon.kalem className="w-3.5 h-3.5" />Elle değiştir</button>
                </div>
              </div>
              <div className="border-t border-red-100 dark:border-red-900/40 pt-5">
                <h3 className="text-sm font-semibold text-red-600 dark:text-red-400 mb-1">Tehlikeli Bölge</h3>
                <p className="text-xs text-slate-400 mb-3">Posta kutusu ve içindeki tüm e-postalar kalıcı olarak silinir. Geri alınamaz.</p>
                <button onClick={kutuSil} disabled={isleniyor} className="inline-flex items-center gap-1.5 text-xs font-medium px-3 py-2 rounded-lg border border-red-200 dark:border-red-800/70 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 transition-colors"><Ikon.kalem className="w-3.5 h-3.5" />Posta kutusunu sil</button>
              </div>
            </div>
          )}

          {/* OTO-YANIT */}
          {sekme === 'oto' && (
            <div className={`${kart} p-5`}>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Otomatik Yanıt (Tatil)</h3>
              <p className="text-xs text-slate-400 mb-4">Gelen her postaya otomatik yanıt gönderilir (aynı kişiye günde bir kez).</p>
              <label className="flex items-center gap-2.5 mb-4 cursor-pointer">
                <input type="checkbox" checked={oto.aktif} onChange={e => setOto(o => ({ ...o, aktif: e.target.checked }))} className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
                <span className="text-sm text-slate-700 dark:text-slate-200">Otomatik yanıtı etkinleştir</span>
              </label>
              <div className={`space-y-3 transition-opacity ${oto.aktif ? '' : 'opacity-50 pointer-events-none'}`}>
                <div><label className={etiket}>Konu</label><input value={oto.konu} onChange={e => setOto(o => ({ ...o, konu: e.target.value }))} maxLength={255} placeholder="Ofis dışındayım" className={girdi} /></div>
                <div><label className={etiket}>Mesaj</label><textarea value={oto.mesaj} onChange={e => setOto(o => ({ ...o, mesaj: e.target.value }))} rows={5} maxLength={8000} placeholder="Merhaba, şu an ofis dışındayım. En kısa sürede dönüş yapacağım." className={`${girdi} resize-y`} /></div>
              </div>
              <div className="mt-4"><button onClick={otoKaydet} disabled={isleniyor} className={btnPri}>Kaydet</button></div>
            </div>
          )}

          {/* İLETİM */}
          {sekme === 'iletim' && (
            <div className={`${kart} p-5`}>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Mail İletim (Yönlendirme)</h3>
              <p className="text-xs text-slate-400 mb-4">Gelen postayı başka adres(ler)e ilet. Birden çok hedefi virgülle ayırın.</p>
              <label className="flex items-center gap-2.5 mb-4 cursor-pointer">
                <input type="checkbox" checked={iletim.aktif} onChange={e => setIletim(i => ({ ...i, aktif: e.target.checked }))} className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
                <span className="text-sm text-slate-700 dark:text-slate-200">İletimi etkinleştir</span>
              </label>
              <div className={`space-y-3 transition-opacity ${iletim.aktif ? '' : 'opacity-50 pointer-events-none'}`}>
                <div><label className={etiket}>Hedef e-posta(lar)</label><textarea value={iletim.hedef} onChange={e => setIletim(i => ({ ...i, hedef: e.target.value }))} rows={2} placeholder="ornek@baska.com, ikinci@baska.com" className={`${girdi} resize-y font-mono`} /></div>
                <label className="flex items-center gap-2.5 cursor-pointer">
                  <input type="checkbox" checked={iletim.kopya} onChange={e => setIletim(i => ({ ...i, kopya: e.target.checked }))} className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
                  <span className="text-sm text-slate-700 dark:text-slate-200">Bu kutuda bir kopya da sakla</span>
                </label>
                <p className="text-[11px] text-slate-400">Kopya kapalıysa gelen posta yalnızca hedefe iletilir, bu kutuda tutulmaz.</p>
              </div>
              <div className="mt-4"><button onClick={iletimKaydet} disabled={isleniyor} className={btnPri}>Kaydet</button></div>
            </div>
          )}

          {/* TAŞIMA */}
          {sekme === 'tasima' && (
            <div className={`${kart} p-5`}>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Mail Taşıma (Uzaktan içe çekme)</h3>
              <p className="text-xs text-slate-400 mb-4">Başka bir sunucudaki IMAP kutusundan tüm mailleri bu kutuya kopyalar (imapsync). Kaynak kutu değişmez.</p>
              {tasimaDurum?.imapsync === false && (
                <div className="mb-4 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-xs text-amber-800 dark:text-amber-300">Bu sunucuda <span className="font-mono">imapsync</span> kurulu değil — taşıma kullanılamıyor. Sunucuya <span className="font-mono">imapsync</span> kurulmalı.</div>
              )}

              {/* ADIM ÇUBUĞU — tamamlanan adıma tıklanarak geri dönülebilir */}
              <ol className="flex items-center gap-2 mb-5 text-xs">
                {ADIMLAR.map((s, i) => {
                  const aktif = adim === s.n, gecildi = adim > s.n
                  return (
                    <li key={s.n} className="flex items-center gap-2">
                      <button type="button" onClick={() => gecildi && setAdim(s.n)} disabled={!gecildi}
                        className={`grid h-6 w-6 place-items-center rounded-full text-[11px] font-semibold transition-colors ${gecildi ? 'bg-emerald-500 text-white hover:bg-emerald-600 cursor-pointer' : aktif ? 'bg-brand-600 text-white' : 'bg-slate-100 dark:bg-slate-700 text-slate-400'}`}>
                        {gecildi ? '✓' : s.n}
                      </button>
                      <span className={aktif ? 'font-medium text-slate-700 dark:text-slate-200' : 'text-slate-400'}>{s.ad}</span>
                      {i < ADIMLAR.length - 1 && <span className="w-6 h-px bg-slate-200 dark:bg-slate-700" />}
                    </li>
                  )
                })}
              </ol>

              {taslakVar && adim === 1 && (
                <div className="mb-4 px-3 py-2 bg-slate-50 dark:bg-slate-900/40 border border-slate-200 dark:border-slate-700 rounded-lg text-xs text-slate-600 dark:text-slate-300 flex items-start justify-between gap-3">
                  <span>
                    Kaldığınız yerden devam ediyorsunuz — form 24 saat saklanır.
                    <span className="text-slate-400"> Güvenlik gereği parolalar saklanmaz, yeniden girin.</span>
                  </span>
                  <button onClick={taslagiTemizle} className="shrink-0 font-medium text-slate-500 dark:text-slate-400 hover:text-red-600 dark:hover:text-red-400 transition-colors">Temizle</button>
                </div>
              )}

              {/* ───── ADIM 1: eski hesap ───── */}
              {adim === 1 && (
                <div>
                  <p className="text-xs text-slate-500 dark:text-slate-400 mb-3">Taşımak istediğiniz <b>eski</b> posta hesabının bilgilerini girin.</p>
                  <div className="grid sm:grid-cols-2 gap-3">
                    <div>
                      <label className={etiket}>Eski e-posta adresi</label>
                      <input value={tasima.kullanici} autoFocus
                        onChange={e => { setTasima(t => ({ ...t, kullanici: e.target.value })); setAdaylar(null); setDogrulama(null) }}
                        placeholder="kullanici@eskisunucu.com" className={girdi} />
                    </div>
                    <div>
                      <label className={etiket}>Eski hesabın parolası</label>
                      <input type="password" value={tasima.parola}
                        onChange={e => { setTasima(t => ({ ...t, parola: e.target.value })); setDogrulama(null) }}
                        className={girdi} />
                    </div>
                  </div>
                </div>
              )}

              {/* ───── ADIM 2: sunucu ───── */}
              {adim === 2 && (
                <div>
                  <p className="text-xs text-slate-500 dark:text-slate-400 mb-3">
                    <span className="font-mono text-slate-700 dark:text-slate-200">{tasima.kullanici}</span> için eski sunucuyu seçin.
                  </p>
                  <div className="flex items-center gap-2 flex-wrap">
                    <button onClick={sunucuKesfet} disabled={kesfediliyor} className={btnPri}>
                      {kesfediliyor
                        ? <><span className="w-3.5 h-3.5 rounded-full border-2 border-white border-t-transparent animate-spin" />Aranıyor…</>
                        : <><Ikon.ayar className="w-4 h-4" />Sunucuyu otomatik bul</>}
                    </button>
                    <button onClick={() => { setElle(v => !v); setAdaylar(null) }} className={btnSec}>
                      {elle ? 'Otomatik moda dön' : 'Elle gireceğim'}
                    </button>
                  </div>

                  {!elle && adaylar && (
                    <div className="mt-3">
                      {adaylarMesaj && <p className="text-xs text-slate-500 dark:text-slate-400 mb-2">{adaylarMesaj}</p>}
                      <ul className="space-y-1.5">
                        {adaylar.map(a => {
                          const secili = tasima.host === a.host && tasima.port === a.port && tasima.guvenlik === a.guvenlik
                          return (
                            <li key={`${a.host}:${a.port}:${a.guvenlik}`}>
                              <button onClick={() => { setTasima(t => ({ ...t, host: a.host, port: a.port, guvenlik: a.guvenlik })); setDogrulama(null) }}
                                className={`w-full flex items-center gap-3 rounded-xl border px-3 py-2.5 text-left transition-colors ${secili ? 'border-brand-400 bg-brand-50/60 dark:bg-brand-900/20' : 'border-slate-200 dark:border-slate-700 hover:border-slate-300 dark:hover:bg-slate-700/30'}`}>
                                <span className={`shrink-0 w-2 h-2 rounded-full ${a.yanit ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`} />
                                <span className="min-w-0 flex-1">
                                  <span className="block font-mono text-sm text-slate-800 dark:text-slate-100 truncate">{a.host}:{a.port}</span>
                                  <span className="block text-[11px] text-slate-400">
                                    {a.guvenlik === 'ssl' ? 'SSL' : 'STARTTLS'} · kaynak: {a.kaynak}
                                    {a.yanit ? ` · yanıt verdi (${a.sure})` : ' · yanıt yok'}
                                  </span>
                                </span>
                                {a.yanit && <span className="shrink-0 text-[10px] font-semibold px-1.5 py-0.5 rounded bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-400 uppercase">Bulundu</span>}
                              </button>
                            </li>
                          )
                        })}
                      </ul>
                    </div>
                  )}

                  {elle && (
                    <div className="mt-3 grid sm:grid-cols-2 gap-3">
                      <div><label className={etiket}>Uzak IMAP sunucusu</label><input value={tasima.host} onChange={e => { setTasima(t => ({ ...t, host: e.target.value })); setDogrulama(null) }} placeholder="mail.eskisunucu.com" className={girdi} /></div>
                      <div className="grid grid-cols-2 gap-3">
                        <div><label className={etiket}>Port</label><input type="number" value={tasima.port} onChange={e => setTasima(t => ({ ...t, port: +e.target.value }))} className={girdi} /></div>
                        <div><label className={etiket}>Güvenlik</label><select value={tasima.guvenlik} onChange={e => { const g = e.target.value; setTasima(t => ({ ...t, guvenlik: g, port: g === 'ssl' ? 993 : g === 'tls' ? 143 : t.port })) }} className={girdi}><option value="ssl">SSL (993)</option><option value="tls">STARTTLS (143)</option></select></div>
                      </div>
                    </div>
                  )}

                  {/* Sağlayıcı kısıtı varsa BURADA uyar — kullanıcı parolayı
                      denemeden önce ne yapması gerektiğini görsün. */}
                  <UyariSeridi />
                </div>
              )}

              {/* ───── ADIM 3: doğrula & başlat ───── */}
              {adim === 3 && (
                <div>
                  {/* Özet */}
                  <dl className="rounded-xl border border-slate-200 dark:border-slate-700 divide-y divide-slate-100 dark:divide-slate-700/70 mb-4">
                    {[
                      ['Kaynak hesap', tasima.kullanici],
                      ['Kaynak sunucu', `${tasima.host}:${tasima.port} (${tasima.guvenlik === 'ssl' ? 'SSL' : 'STARTTLS'})`],
                      ['Hedef kutu', kutu.email],
                    ].map(([k, v]) => (
                      <div key={k} className="flex items-center justify-between gap-3 px-4 py-2.5">
                        <dt className="text-xs text-slate-400 shrink-0">{k}</dt>
                        <dd className="font-mono text-sm text-slate-800 dark:text-slate-100 truncate">{v}</dd>
                      </div>
                    ))}
                  </dl>

                  <div className="mb-4"><UyariSeridi /></div>

                  <div className="max-w-sm">
                    <label className={etiket}>Hedef kutu parolası <span className="text-slate-400">(bu kutu)</span></label>
                    <input type="password" value={tasima.hedef_parola} onChange={e => setTasima(t => ({ ...t, hedef_parola: e.target.value }))} placeholder="bu kutunun parolası" className={girdi} />
                    <p className="mt-1.5 text-[11px] text-slate-400">Bilmiyorsanız Genel sekmesinden yeni bir parola üretebilirsiniz.</p>
                  </div>

                  {dogrulama && (
                    <div className={`mt-3 px-3 py-2 rounded-lg text-xs border ${dogrulama.ok
                      ? 'bg-emerald-50 dark:bg-emerald-900/20 border-emerald-200 dark:border-emerald-800 text-emerald-700 dark:text-emerald-300'
                      : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800 text-red-700 dark:text-red-300'}`}>
                      {dogrulama.ok ? `✓ ${dogrulama.mesaj}` : `Bağlanılamadı: ${dogrulama.hata}`}
                    </div>
                  )}

                  <div className="mt-4 flex items-center gap-2 flex-wrap">
                    <button onClick={baglantiDogrula} disabled={dogrulaniyor} className={btnSec}>
                      {dogrulaniyor ? 'Doğrulanıyor…' : dogrulama?.ok ? 'Yeniden doğrula' : 'Bağlantıyı doğrula'}
                    </button>
                    <button onClick={tasimaBaslat} disabled={isleniyor || tasimaDurum?.durum === 'calisiyor' || !dogrulama?.ok || !tasima.hedef_parola} className={btnPri}
                      title={!dogrulama?.ok ? 'Önce bağlantıyı doğrulayın' : 'Taşımayı başlat'}>
                      <Ikon.tasima className="w-4 h-4" />{tasimaDurum?.durum === 'calisiyor' ? 'Taşınıyor…' : 'Taşımayı başlat'}
                    </button>
                  </div>
                </div>
              )}

              {/* GEZİNME — ileri/geri */}
              <div className="mt-5 pt-4 border-t border-slate-100 dark:border-slate-700 flex items-center justify-between gap-2">
                <button onClick={() => setAdim(a => Math.max(1, a - 1))} disabled={adim === 1}
                  className={`${btnSec} ${adim === 1 ? 'invisible' : ''}`}>← Geri</button>
                {adim < 3 ? (
                  <button onClick={() => setAdim(a => a + 1)} disabled={!adimTamam(adim)}
                    title={adimTamam(adim) ? '' : adim === 1 ? 'E-posta ve parolayı girin' : 'Bir sunucu seçin ya da elle girin'}
                    className={btnPri}>Devam →</button>
                ) : <span />}
              </div>

              {/* İlerleme paneli — hangi adımda olursanız olun görünür */}
              {tasimaDurum && tasimaDurum.durum !== 'yok' && (
                <div className="mt-4 rounded-xl border border-slate-200 dark:border-slate-700 bg-slate-50/60 dark:bg-slate-900/30 p-4">
                  <div className="flex items-center gap-2 text-sm">
                    {tasimaDurum.durum === 'calisiyor' && <span className="w-3.5 h-3.5 rounded-full border-2 border-brand-500 border-t-transparent animate-spin" />}
                    {tasimaDurum.durum === 'tamam' && <Ikon.onay className="w-4 h-4 text-emerald-500" />}
                    {tasimaDurum.durum === 'hata' && <span className="text-red-500">!</span>}
                    <span className="font-medium text-slate-700 dark:text-slate-200">{tasimaDurum.durum === 'calisiyor' ? 'Taşınıyor' : tasimaDurum.durum === 'tamam' ? 'Tamamlandı' : 'Hata'}</span>
                    {typeof tasimaDurum.aktarilan === 'number' && tasimaDurum.aktarilan > 0 && <span className="text-xs text-slate-400 tabular-nums">· {tasimaDurum.aktarilan} mesaj</span>}
                  </div>
                  {tasimaDurum.mesaj && <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{tasimaDurum.mesaj}</p>}
                  {tasimaDurum.hata && <p className="mt-1 text-xs text-red-600 dark:text-red-400">{tasimaDurum.hata}</p>}
                  {tasimaDurum.log && tasimaDurum.log.length > 0 && (
                    <pre className="mt-2 max-h-40 overflow-auto text-[11px] font-mono text-slate-500 dark:text-slate-400 bg-white dark:bg-slate-950/40 rounded-lg p-2 border border-slate-200 dark:border-slate-700">{tasimaDurum.log.join('\n')}</pre>
                  )}
                </div>
              )}
            </div>
          )}

          {/* İÇE / DIŞA AKTAR */}
          {sekme === 'aktar' && (
            <div className="grid sm:grid-cols-2 gap-4">
              <div className={`${kart} p-5`}>
                <div className="w-10 h-10 rounded-lg bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300 mb-3"><Ikon.disari className="w-5 h-5" /></div>
                <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">Dışa Aktar</h3>
                <p className="text-xs text-slate-400 mb-4">Kutunun tüm postalarını <span className="font-mono">.tar.gz</span> (Maildir) olarak indir. Yedekleme veya başka kutuya taşımak için.</p>
                <button onClick={disariAktar} disabled={isleniyor} className={btnPri}><Ikon.disari className="w-3.5 h-3.5" />Yedeği indir</button>
              </div>
              <div className={`${kart} p-5`}>
                <div className="w-10 h-10 rounded-lg bg-brand-50 dark:bg-brand-900/30 flex items-center justify-center text-brand-600 dark:text-brand-300 mb-3"><Ikon.iceri className="w-5 h-5" /></div>
                <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">İçe Aktar</h3>
                <p className="text-xs text-slate-400 mb-4"><span className="font-mono">.tar.gz</span> (Maildir yedeği) veya <span className="font-mono">.mbox</span> yükleyin. Mevcut postalar korunur, üzerine eklenir.</p>
                <input ref={dosyaRef} type="file" accept=".tar.gz,.tgz,.mbox,.eml" onChange={e => { const f = e.target.files?.[0]; if (f) iceriAktar(f) }} className="hidden" />
                <button onClick={() => dosyaRef.current?.click()} disabled={isleniyor} className={btnSec}><Ikon.iceri className="w-3.5 h-3.5" />Dosya seç ve yükle</button>
              </div>
            </div>
          )}
        </>
      )}

      {/* Üretilen parola modalı */}
      {uretilen && (
        <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/40 backdrop-blur-sm p-4" onClick={() => setUretilen(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl p-6 max-w-sm w-full shadow-2xl border border-slate-200 dark:border-slate-700" onClick={e => e.stopPropagation()}>
            <div className="flex items-center gap-2.5 mb-3">
              <div className="w-9 h-9 rounded-lg bg-emerald-50 dark:bg-emerald-900/30 flex items-center justify-center text-emerald-600 dark:text-emerald-400"><Ikon.anahtar className="w-5 h-5" /></div>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Yeni parola üretildi</h3>
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400 mb-4"><span className="font-mono text-slate-700 dark:text-slate-300">{uretilen.email}</span> — bu parola yalnızca şimdi gösteriliyor, güvenli bir yere kaydedin.</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 font-mono text-sm bg-slate-100 dark:bg-slate-900 rounded-lg px-3 py-2.5 break-all select-all text-slate-800 dark:text-slate-100">{uretilen.parola}</code>
              <button onClick={() => { navigator.clipboard?.writeText(uretilen.parola); setKopyalandi(true) }} className="shrink-0 text-xs font-medium px-3 py-2.5 rounded-lg bg-brand-600 hover:bg-brand-700 text-white transition-colors">{kopyalandi ? '✓ Kopyalandı' : 'Kopyala'}</button>
            </div>
            <button onClick={() => setUretilen(null)} className="mt-4 w-full text-sm font-medium px-4 py-2 rounded-lg border border-slate-200 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 transition-colors">Kapat</button>
          </div>
        </div>
      )}
    </div>
  )
}

// Kota kullanım barı (liste sayfasıyla aynı).
function KotaBar({ kullanilan, kota }: { kullanilan: number; kota: number }) {
  const kul = Math.max(0, kullanilan)
  if (!kota) return <div className="text-xs text-slate-500 dark:text-slate-400">{boyut(kul)} kullanılıyor · <span className="text-slate-400">Sınırsız kota</span></div>
  const pct = Math.min(100, Math.round((kul / kota) * 100))
  const renk = pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-amber-500' : 'bg-emerald-500'
  return (
    <div className="w-full max-w-md">
      <div className="flex items-baseline justify-between text-xs mb-1">
        <span className="text-slate-600 dark:text-slate-300 tabular-nums">{boyut(kul)} <span className="text-slate-400">/ {boyut(kota)}</span></span>
        <span className={`tabular-nums font-medium ${pct >= 90 ? 'text-red-600 dark:text-red-400' : pct >= 70 ? 'text-amber-600 dark:text-amber-400' : 'text-slate-400'}`}>%{pct}</span>
      </div>
      <div className="h-2 w-full rounded-full bg-slate-100 dark:bg-slate-700/70 overflow-hidden">
        <div className={`h-full ${renk} rounded-full transition-all duration-500`} style={{ width: `${Math.max(pct, 2)}%` }} />
      </div>
    </div>
  )
}
