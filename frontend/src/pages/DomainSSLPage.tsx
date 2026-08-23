import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; ipv4: string; ssl: boolean; ssl_bitis?: string }
type SSLDurum = {
  aktif: boolean
  kaynak: string
  bitis_iso?: string
  cert_yol?: string
  key_yol?: string
}
type SSLAdim = { ad: string; etiket: string; durum: string; mesaj?: string; sure?: string }
type SSLIlerleme = { durum: string; adimlar?: SSLAdim[]; hata?: string }

type KapsamSatir = {
  etiket: string
  host: string
  durum: 'kurulu' | 'eksik' | 'dns_yok'
  kaynak?: string
  bitis_iso?: string
  dns_var: boolean
  grup: 'web' | 'mail'
  aciklama?: string
}
type KapsamResp = { mail_eklenti: boolean; sunucu_ip: string; kapsamlar: KapsamSatir[] }


const SSL_EN: Record<string, string> = {
  "Anasayfa": "Home",
  "SSL kuruluyor…": "Installing SSL…",
  "SSL kurulumu — hata": "SSL installation — error",
  "Mevcut Durum": "Current Status",
  "Korumalı": "Protected",
  "Korumasız": "Unprotected",
  "Kaynak": "Source",
  "Sertifika yolu": "Certificate path",
  "Anahtar yolu": "Key path",
  "SSL'i Kaldır (HTTP'ye dön)": "Remove SSL (revert to HTTP)",
  "Bu domain için aktif SSL sertifikası yok. Aşağıdan birini kurabilirsiniz.": "There is no active SSL certificate for this domain. You can install one below.",
  "Self-Signed Sertifika": "Self-Signed Certificate",
  "Sunucu tarafından öz-imzalı bir sertifika oluşturur. Tarayıcı uyarısı verir ama bağlantı şifrelidir.": "Creates a self-signed certificate on the server. The browser shows a warning but the connection is encrypted.",
  "Test/dev ortamı için uygun.": "Suitable for test/dev environments.",
  "✗ Tarayıcıda \"güvenli değil\" uyarısı": "✗ \"Not secure\" warning in the browser",
  "Kuruluyor…": "Installing…",
  "Self-Signed Kur": "Install Self-Signed",
  "Resmi Let's Encrypt sertifikası, tüm tarayıcılarda otomatik güvenilir. 90 günde bir otomatik yenilenir.": "Official Let's Encrypt certificate, automatically trusted in all browsers. Auto-renews every 90 days.",
  "✓ Otomatik yenileme (cron)": "✓ Automatic renewal (cron)",
  "için ayrı sertifika alınıp IMAP/POP/SMTP + webmail'e kurulur.": "a separate certificate is obtained and installed for IMAP/POP/SMTP + webmail.",
  "Let's Encrypt Sertifikası Al": "Get Let's Encrypt Certificate",
  "Kurulu": "Installed",
  "DNS yok": "No DNS",
  "Eksik": "Missing",
  "Mail eklentisi etkin": "Mail add-on enabled",
  "Her alt-alan için ayrı sertifika durumu. Posta istemcileri (Outlook, telefon)": "Separate certificate status for each subdomain. Mail clients (Outlook, phone)",
  "adına bağlanır — o satır “Kurulu” değilse istemci sertifika uyarısı verir.": "connect to this name — if that row is not “Installed”, the client shows a certificate warning.",
  "Posta": "Mail",
  "Posta SSL kur": "Install mail SSL",
  "Bu sayfayı kapatabilirsiniz; kurulum arka planda sürer. Posta SSL'inde 7 alt-alan (mail, webmail, smtp, imap, pop, autoconfig, autodiscover) tek tek doğrulandığı için 1–2 dakika sürebilir.": "You can close this page; installation continues in the background. Mail SSL may take 1–2 minutes because 7 subdomains (mail, webmail, smtp, imap, pop, autoconfig, autodiscover) are verified one by one.",
  "Onay gerekiyor": "Confirmation required",
  "Let's Encrypt sertifikası alınması için alan adının bu sunucuya DNS A kaydı ile yönlenmiş olması gerekir. Devam edilsin mi?": "To obtain a Let's Encrypt certificate, the domain must point to this server with a DNS A record. Continue?",
  "SSL kaldırılsın mı? Site HTTP'ye dönecek.": "Remove SSL? The site will revert to HTTP.",
  "Alan adı bilgisi alınamadı": "Failed to get domain info",
  "Bitiş": "Expiry",
  "Bu alt alanların da sunucuya DNS A kaydı gerekir.": "These subdomains also need a DNS A record to this server.",
  "Posta alt-alanlarına sertifika kurulmamış — Outlook/istemciler şifre sorabilir.": "No certificate installed for mail subdomains — Outlook/clients may ask for a password.",
  "Posta sunucusunu da güvenceye al": "Also secure the mail server",
  "SSL kaldırma başarısız": "Failed to remove SSL",
  "SSL kaldırıldı. Site HTTP olarak çalışıyor.": "SSL removed. The site runs over HTTP.",
  "SSL kuruldu, bazı adımlar uyarıyla tamamlandı:": "SSL installed, some steps completed with warnings:",
  "SSL kurulumu başarısız:": "SSL installation failed:",
  "SSL kurulumu başlatılamadı": "Failed to start SSL installation",
  "SSL kurulumu tamamlandı": "SSL installation completed",
  "SSL kurulumu tamamlandı — site artık HTTPS üzerinden çalışıyor.": "SSL installation completed — the site now runs over HTTPS.",
  "SSL/TLS Sertifikaları": "SSL/TLS Certificates",
  "Self-signed (öz-imzalı)": "Self-signed",
  "Sertifika Kapsamı": "Certificate Scope",
  "Yükleniyor…": "Loading…",
  "başladı": "started",
  "güvenli değil": "not secure",
  "Öz-imzalı": "Self-signed",
  "Let's Encrypt (Ücretsiz)": "Let's Encrypt (Free)",
  "⚠ Alan adı bu sunucuya DNS ile yönelmiş olmalı": "⚠ The domain must point to this server via DNS",
  "✓ Anında kurulur": "✓ Installs instantly",
  "✓ DNS bağımlılığı yok": "✓ No DNS dependency",
  "✓ Tarayıcılarda yeşil kilit": "✓ Green padlock in browsers",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SSL_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainSSLPage() {
  useTranslation() // dil degisince re-render aboneligi
  const { onay } = useDialog()
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [durum, setDurum] = useState<SSLDurum | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [uyari, setUyari] = useState<string | null>(null)  // LE alınamadı → self-signed fail-safe vb.
  const [mailAktif, setMailAktif] = useState(false)  // mail eklentisi kurulu+etkin mi
  const [kapsam, setKapsam] = useState<KapsamResp | null>(null)  // domain/www/mail/webmail durumu
  const [mailSSL, setMailSSL] = useState(true)       // cevir("posta sunucusunu de güvenceye al")
  // Asenkron kurulum ilerlemesi (adım-adım). isDurum: ''|calisiyor|tamam|hata.
  const [adimlar, setAdimlar] = useState<SSLAdim[]>([])
  const [isDurum, setIsDurum] = useState<string>('')

  function yukle() {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(hataYakala(cevir("Alan adı bilgisi alınamadı")))
    api.get<SSLDurum>(`/domains/${id}/ssl`).then(r => setDurum(r.data)).catch(e => setHata(apiHata(e)))
    // Mail eklentisi aktifse SSL akışında mail seçeneğini göster.
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif)))
      .catch(() => setMailAktif(false))
    api.get<KapsamResp>(`/domains/${id}/ssl/kapsam`).then(r => setKapsam(r.data)).catch(() => setKapsam(null))
  }
  useEffect(yukle, [id])

  // İlerlemeyi çek. Biten kurulumda (tamam/hata) sonucu banner'a yazar ve durum
  // kartını yeniler. Sayfa yeniden açılırsa DEVAM EDEN iş kaldığı yerden görünür.
  function ilerlemeCek(sonra?: () => void) {
    if (!id) return
    api.get<SSLIlerleme>(`/domains/${id}/ssl/ilerleme`).then(r => {
      const d = r.data
      if (!d || d.durum === 'yok') { sonra?.(); return }
      setAdimlar(d.adimlar || [])
      setIsDurum(d.durum)
      if (d.durum !== 'calisiyor') {
        // Bitti: cert durumunu yenile + sonucu özetle.
        yukle()
        const uyarili = (d.adimlar || []).filter(a => a.durum === 'uyari')
        if (d.durum === 'hata') {
          setHata(cevir("SSL kurulumu başarısız:") + ' ' + (d.hata || (d.adimlar || []).find(a => a.durum === 'hata')?.mesaj || ''))
        } else if (uyarili.length) {
          setUyari(cevir("SSL kuruldu, bazı adımlar uyarıyla tamamlandı:") + ' ' + uyarili.map(a => a.mesaj).filter(Boolean).join(' | '))
        } else {
          setBasari(cevir("SSL kurulumu tamamlandı — site artık HTTPS üzerinden çalışıyor."))
        }
      }
      sonra?.()
    }).catch(() => sonra?.())
  }

  // Devam eden iş varken 1.5 sn'de bir çek.
  useEffect(() => {
    if (isDurum !== 'calisiyor') return
    const t = setInterval(() => ilerlemeCek(), 1500)
    return () => clearInterval(t)
  }, [isDurum, id])

  // İlk açılışta: YALNIZ devam eden bir kurulum varsa göster (sekme kapanıp
  // açıldıysa kaldığı yerden). Biten eski işi her açılışta gösterme.
  useEffect(() => {
    if (!id) return
    api.get<SSLIlerleme>(`/domains/${id}/ssl/ilerleme`).then(r => {
      if (r.data && r.data.durum === 'calisiyor') {
        setAdimlar(r.data.adimlar || [])
        setIsDurum('calisiyor')
      }
    }).catch(() => {})
  }, [id])

  async function issue(tip: 'self-signed' | 'letsencrypt') {
    if (tip === 'letsencrypt' && !(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevir("Let's Encrypt sertifikası alınması için alan adının bu sunucuya DNS A kaydı ile yönlenmiş olması gerekir. Devam edilsin mi?") }))) return
    setIsleniyor(true); setHata(null); setBasari(null); setUyari(null); setAdimlar([])
    try {
      const govde: { tip: string; mail_ssl?: boolean } = { tip }
      if (tip === 'letsencrypt' && mailAktif && mailSSL) govde.mail_ssl = true
      // 🔴 ASENKRON: istek HEMEN döner (cevir("başladı")); SSL çekimi arka planda sürer
      // (sayfa kapansa da). İlerleme aşağıda adım-adım gösterilir — poll başlar.
      await api.post(`/domains/${id}/ssl/issue`, govde)
      setIsDurum('calisiyor')   // poll useEffect'i tetikler
      ilerlemeCek()             // ilk adımı hemen göster
    } catch (e) {
      setHata(apiHata(e, cevir("SSL kurulumu başlatılamadı")))
    } finally {
      setIsleniyor(false)
    }
  }

  async function disable() {
    if (!(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevir("SSL kaldırılsın mı? Site HTTP'ye dönecek.") }))) return
    setIsleniyor(true); setHata(null); setBasari(null); setUyari(null)
    try {
      await api.delete(`/domains/${id}/ssl`)
      setBasari(cevir("SSL kaldırıldı. Site HTTP olarak çalışıyor."))
      yukle()
    } catch (e) {
      setHata(apiHata(e, cevir("SSL kaldırma başarısız")))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' },
        { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("SSL/TLS Sertifikaları") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("SSL/TLS Sertifikaları")}</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}
          IP: <span className="font-mono">{domain.ipv4}</span>
        </p>
      )}

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {uyari && <div className="mb-3 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-md text-sm text-amber-800 dark:text-amber-300">{uyari}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}
      {/* Adım-adım kurulum ilerlemesi (asenkron; sayfa kapansa da sürer) */}
      {(isDurum === 'calisiyor' || (isDurum && adimlar.length > 0)) && (
        <div className="mb-5 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
          <div className="flex items-center gap-2 mb-2">
            {isDurum === 'calisiyor' && <span className="inline-block w-4 h-4 border-2 border-brand-500 border-t-transparent rounded-full animate-spin" />}
            <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">
              {isDurum === 'calisiyor' ? cevir("SSL kuruluyor…") : isDurum === 'hata' ? cevir("SSL kurulumu — hata") : cevir("SSL kurulumu tamamlandı")}
            </h2>
          </div>
          {isDurum === 'calisiyor' && (
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
              {cevir("Bu sayfayı kapatabilirsiniz; kurulum arka planda sürer. Posta SSL'inde 7 alt-alan (mail, webmail, smtp, imap, pop, autoconfig, autodiscover) tek tek doğrulandığı için 1–2 dakika sürebilir.")}
            </p>
          )}
          <ol className="space-y-2.5">
            {adimlar.map((a, i) => (
              <li key={i} className="flex items-start gap-2.5 text-sm">
                <span className="mt-0.5 shrink-0 w-4 text-center">
                  {a.durum === 'tamam' ? <span className="text-emerald-500 font-bold">✓</span>
                    : a.durum === 'uyari' ? <span className="text-amber-500 font-bold">!</span>
                    : a.durum === 'hata' ? <span className="text-red-500 font-bold">✗</span>
                    : <span className="inline-block w-3.5 h-3.5 border-2 border-brand-500 border-t-transparent rounded-full animate-spin align-middle" />}
                </span>
                <div className="min-w-0">
                  <div className="text-slate-800 dark:text-slate-200">
                    {a.etiket}
                    {a.sure && a.durum !== 'calisiyor' && <span className="text-slate-400 dark:text-slate-500"> ({a.sure})</span>}
                  </div>
                  {a.mesaj && <div className={'text-xs mt-0.5 break-words ' + (a.durum === 'hata' ? 'text-red-600 dark:text-red-400' : a.durum === 'uyari' ? 'text-amber-600 dark:text-amber-400' : 'text-slate-500 dark:text-slate-500')}>{a.mesaj}</div>}
                </div>
              </li>
            ))}
          </ol>
        </div>
      )}

      {/* Durum kartı */}
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-6 mb-5">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Mevcut Durum")}</h2>
          {durum && (
            durum.aktif && durum.kaynak === 'letsencrypt' ? (
              // Yalnız GERÇEK CA (Let's Encrypt) = tarayıcıda güvenilir → yeşil Korumalı.
              <span className="text-xs px-2 py-1 bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 rounded uppercase font-semibold tracking-wider flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
                {cevir(cevir("Korumalı"))}
              </span>
            ) : durum.aktif ? (
              // Self-signed: şifreli AMA tarayıcı güvenmez → yeşil DEĞİL, amber cevir("Öz-imzalı").
              <span className="text-xs px-2 py-1 bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 rounded uppercase font-semibold tracking-wider flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-400"></span>
                {cevir("Öz-imzalı")}
              </span>
            ) : (
              <span className="text-xs px-2 py-1 bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 rounded uppercase font-semibold tracking-wider flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-400"></span>
                {cevir(cevir("Korumasız"))}
              </span>
            )
          )}
        </div>
        {!durum ? (
          <div className="text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
        ) : durum.aktif ? (
          <div className="space-y-2 text-sm">
            <Sat e={cevir("Kaynak")} d={durum.kaynak === 'letsencrypt' ? "Let's Encrypt" : cevir("Self-signed (öz-imzalı)")} />
            {durum.bitis_iso && <Sat e={cevir("Bitiş")} d={new Date(durum.bitis_iso).toLocaleDateString('tr-TR', { dateStyle: 'long' })} />}
            <Sat e={cevir("Sertifika yolu")} d={durum.cert_yol || '—'} mono />
            <Sat e={cevir("Anahtar yolu")} d={durum.key_yol || '—'} mono />
            <button
              onClick={disable}
              disabled={isleniyor || isDurum === 'calisiyor'}
              className="mt-4 px-4 py-2 border border-red-300 dark:border-red-700 text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 disabled:opacity-50 rounded-md text-sm font-medium transition"
            >
              {cevir("SSL'i Kaldır (HTTP'ye dön)")}
            </button>
          </div>
        ) : (
          <div className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500">
            {cevir(cevir("Bu domain için aktif SSL sertifikası yok. Aşağıdan birini kurabilirsiniz."))}
          </div>
        )}
      </div>

      {kapsam && kapsam.kapsamlar.length > 0 && (
        <KapsamKarti kapsam={kapsam} onKur={() => issue('letsencrypt')} />
      )}

      {/* Aksiyon kartları */}
      {durum && !durum.aktif && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-6">
            <div className="flex items-center gap-2 mb-2">
              <div className="w-9 h-9 rounded-lg bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 flex items-center justify-center">
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"/></svg>
              </div>
              <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Self-Signed Sertifika")}</h3>
            </div>
            <p className="text-sm text-slate-500 dark:text-slate-500 mb-4">
              {cevir(cevir("Sunucu tarafından öz-imzalı bir sertifika oluşturur. Tarayıcı uyarısı verir ama bağlantı şifrelidir."))}
              {cevir(cevir("Test/dev ortamı için uygun."))}
            </p>
            <ul className="text-xs text-slate-500 dark:text-slate-500 mb-4 space-y-1">
              <li>{cevir("✓ DNS bağımlılığı yok")}</li>
              <li>{cevir("✓ Anında kurulur")}</li>
              <li>{cevir("✗ Tarayıcıda \"güvenli değil\" uyarısı")}</li>
            </ul>
            <button
              onClick={() => issue('self-signed')}
              disabled={isleniyor || isDurum === 'calisiyor'}
              className="w-full px-4 py-2.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md transition"
            >
              {(isleniyor || isDurum === 'calisiyor') ? cevir("Kuruluyor…") : cevir("Self-Signed Kur")}
            </button>
          </div>

          <div className="bg-white dark:bg-slate-800 border border-emerald-200 dark:border-emerald-800 rounded-2xl p-6">
            <div className="flex items-center gap-2 mb-2">
              <div className="w-9 h-9 rounded-lg bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 flex items-center justify-center">
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}><path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"/></svg>
              </div>
              <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Let's Encrypt (Ücretsiz)")}</h3>
            </div>
            <p className="text-sm text-slate-500 dark:text-slate-500 mb-4">
              {cevir("Resmi Let's Encrypt sertifikası, tüm tarayıcılarda otomatik güvenilir. 90 günde bir otomatik yenilenir.")}
            </p>
            <ul className="text-xs text-slate-500 dark:text-slate-500 mb-4 space-y-1">
              <li>{cevir("✓ Tarayıcılarda yeşil kilit")}</li>
              <li>{cevir("✓ Otomatik yenileme (cron)")}</li>
              <li>{cevir("⚠ Alan adı bu sunucuya DNS ile yönelmiş olmalı")}</li>
            </ul>
            {mailAktif && (
              <label className="flex items-start gap-2 mb-4 p-2.5 rounded-lg bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 cursor-pointer">
                <input type="checkbox" checked={mailSSL} onChange={e => setMailSSL(e.target.checked)} className="mt-0.5 cursor-pointer" />
                <span className="text-xs text-slate-700 dark:text-slate-300">
                  <b>{cevir("Posta sunucusunu da güvenceye al")}</b> — <span className="font-mono">mail.{domain?.alan_adi}</span> + <span className="font-mono">webmail.{domain?.alan_adi}</span> {cevir("için ayrı sertifika alınıp IMAP/POP/SMTP + webmail'e kurulur.")}
                  <span className="block text-[11px] text-slate-400 dark:text-slate-500 mt-0.5">{cevir("Bu alt alanların da sunucuya DNS A kaydı gerekir.")}</span>
                </span>
              </label>
            )}
            <button
              onClick={() => issue('letsencrypt')}
              disabled={isleniyor || isDurum === 'calisiyor'}
              className="w-full px-4 py-2.5 bg-emerald-600 hover:bg-emerald-700 disabled:bg-emerald-300 text-white text-sm font-medium rounded-md transition"
            >
              {(isleniyor || isDurum === 'calisiyor') ? cevir("Kuruluyor…") : cevir("Let's Encrypt Sertifikası Al")}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function Sat({ e, d, mono }: { e: string; d: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-slate-500 dark:text-slate-500">{e}</span>
      <span className={`text-slate-800 dark:text-slate-200 text-right break-all ${mono ? 'font-mono text-xs' : ''}`}>{d}</span>
    </div>
  )
}

// KapsamKarti — SSL kapsamlarını (domain/www/mail/webmail) satır satır gösterir.
function KapsamKarti({ kapsam, onKur }: { kapsam: KapsamResp; onKur?: () => void }) {
  const rozet = (d: KapsamSatir) => {
    if (d.durum === 'kurulu') {
      return <span className="inline-flex items-center gap-1.5 text-xs font-semibold px-2 py-1 rounded bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300"><span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />{cevir("Kurulu")}</span>
    }
    if (d.durum === 'dns_yok') {
      return <span className="inline-flex items-center gap-1.5 text-xs font-semibold px-2 py-1 rounded bg-slate-100 dark:bg-slate-700/50 text-slate-500 dark:text-slate-400"><span className="w-1.5 h-1.5 rounded-full bg-slate-400" />{cevir("DNS yok")}</span>
    }
    return <span className="inline-flex items-center gap-1.5 text-xs font-semibold px-2 py-1 rounded bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300"><span className="w-1.5 h-1.5 rounded-full bg-amber-400" />{cevir("Eksik")}</span>
  }
  const eksikMailVar = kapsam.kapsamlar.some(k => k.grup === 'mail' && k.durum === 'eksik')
  return (
    <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-6 mb-5">
      <div className="flex items-center justify-between mb-1">
        <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Sertifika Kapsamı")}</h2>
        {kapsam.mail_eklenti && (
          <span className="text-[11px] text-slate-400 dark:text-slate-500">{cevir("Mail eklentisi etkin")}</span>
        )}
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">
        {cevir("Her alt-alan için ayrı sertifika durumu. Posta istemcileri (Outlook, telefon)")} <span className="font-mono">mail.{'{'}alan{'}'}</span> {cevir("adına bağlanır — o satır “Kurulu” değilse istemci sertifika uyarısı verir.")}
      </p>
      <div className="divide-y divide-slate-100 dark:divide-slate-700/60">
        {kapsam.kapsamlar.map(d => (
          <div key={d.host} className="flex items-center justify-between gap-3 py-2.5">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-slate-800 dark:text-slate-200">{d.etiket}</span>
                <span className="text-[11px] px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700/50 text-slate-500 dark:text-slate-400">{d.grup === 'mail' ? cevir("Posta") : 'Web'}</span>
              </div>
              <div className="text-xs font-mono text-slate-400 dark:text-slate-500 truncate">{d.host}</div>
              {d.durum !== 'kurulu' && d.aciklama && (
                <div className="text-[11px] text-slate-400 dark:text-slate-500 mt-0.5">{d.aciklama}</div>
              )}
              {d.durum === 'kurulu' && d.bitis_iso && (
                <div className="text-[11px] text-slate-400 dark:text-slate-500 mt-0.5">{cevir("Bitiş")}: {new Date(d.bitis_iso).toLocaleDateString('tr-TR', { dateStyle: 'medium' })}</div>
              )}
            </div>
            {rozet(d)}
          </div>
        ))}
      </div>
      {eksikMailVar && onKur && (
        <div className="mt-4 rounded-lg border border-amber-200 dark:border-amber-800/60 bg-amber-50 dark:bg-amber-950/30 px-3 py-2.5 flex items-center justify-between gap-3">
          <span className="text-xs text-amber-800 dark:text-amber-200">{cevir("Posta alt-alanlarına sertifika kurulmamış — Outlook/istemciler şifre sorabilir.")}</span>
          <button onClick={onKur} className="shrink-0 text-xs font-medium px-3 py-1.5 rounded-lg bg-amber-600 hover:bg-amber-700 text-white transition">{cevir("Posta SSL kur")}</button>
        </div>
      )}
    </div>
  )
}
