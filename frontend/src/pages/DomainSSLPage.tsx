import i18n from '@/lib/i18n'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useRef, useState } from 'react'
import { useParams, Link, useSearchParams } from 'react-router-dom'
import { api, apiHata, UZUN_ISTEK } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import IlerlemePaneli, { type IlerlemeIsi } from '@/components/IlerlemePaneli'
import { Button, Badge } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { UyariKutusu } from '@/components/UyariKutusu'
import SslAlanAdlari from '@/components/SslAlanAdlari'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; ipv4: string; ssl: boolean; ssl_bitis?: string }
type SSLDurum = {
  aktif: boolean
  kaynak: string
  bitis_iso?: string
  cert_yol?: string
  key_yol?: string
}
type SSLAdim = { ad: string; etiket: string; durum: string; mesaj?: string; sure?: string }
type SSLIlerleme = { durum: string; adimlar?: SSLAdim[]; hata?: string; toplam?: number; basladi?: string; bitti?: string }

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
  "Alan adı": "Domain name",
  "Posta SSL kurulmadı": "Mail SSL not installed",
  "Sertifika bu adı kapsamıyor": "Certificate does not cover this name",
  "DNS bu sunucuya gelmiyor — sertifika alınamaz": "DNS does not point to this server — certificate cannot be obtained",
  "Seçili posta alt-alanlarına Let's Encrypt sertifikası kurulur. Kullanmadıklarını (ör. Outlook yoksa autodiscover) işaretten kaldırabilirsin.": "A Let's Encrypt certificate is installed for the selected mail subdomains. You can uncheck the ones you don't use (e.g. autodiscover if you don't use Outlook).",
  "Seçili alt-alanlara SSL kur": "Install SSL for selected",
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
  "Tarayıcıda \"güvenli değil\" uyarısı": "\"Not secure\" warning in the browser",
  "Kuruluyor…": "Installing…",
  "Self-Signed Kur": "Install Self-Signed",
  "Resmi Let's Encrypt sertifikası, tüm tarayıcılarda otomatik güvenilir. 90 günde bir otomatik yenilenir.": "Official Let's Encrypt certificate, automatically trusted in all browsers. Auto-renews every 90 days.",
  "Otomatik yenileme (cron)": "Automatic renewal (cron)",
  "için ayrı sertifika alınıp IMAP/POP/SMTP + webmail'e kurulur.": "a separate certificate is obtained and installed for IMAP/POP/SMTP + webmail.",
  "Let's Encrypt Sertifikası Al": "Get Let's Encrypt Certificate",
  "Kurulu": "Installed",
  "DNS yok": "No DNS",
  "Eksik": "Missing",
  "Mail eklentisi etkin": "Mail add-on enabled",
  "DNS'i yenile": "Refresh DNS",
  "DNS önbelleğini temizle ve kayıtları kamu DNS'ten yeniden oku": "Flush DNS cache and re-read records from public DNS",
  "DNS önbelleği temizlendi.": "DNS cache cleared.",
  "DNS yenileme başarısız": "DNS refresh failed",
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
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "SSL kurulumu tamamlandı — site artık HTTPS üzerinden çalışıyor.": "SSL installation completed — the site now runs over HTTPS.",
  "SSL/TLS Sertifikaları": "SSL/TLS Certificates",
  "Self-signed (öz-imzalı)": "Self-signed",
  "Sertifika Kapsamı": "Certificate Scope",
  "Yükleniyor…": "Loading…",
  "başladı": "started",
  "güvenli değil": "not secure",
  "Öz-imzalı": "Self-signed",
  "Let's Encrypt (Ücretsiz)": "Let's Encrypt (Free)",
  "Alan adı bu sunucuya DNS ile yönelmiş olmalı": "The domain must point to this server via DNS",
  "Anında kurulur": "Installs instantly",
  "DNS bağımlılığı yok": "No DNS dependency",
  "Tarayıcılarda yeşil kilit": "Green padlock in browsers",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SSL_EN[tr] || ORTAK_EN[tr] || tr) : tr)
// ceviriAciklama — backend'in DINAMIK kapsam aciklamalarini (host/IP gomulu) EN'e
// cevirir. Statik olanlar cevir sozlugunden gecer.
function ceviriAciklama(a?: string): string {
  if (!a) return ''
  if (i18n.language === 'en') {
    const m = a.match(/^(.+) A kaydı bu sunucuya \((.+)\) gelmeli$/)
    if (m) return `${m[1]} A record must point to this server (${m[2]})`
  }
  return cevir(a)
}

export default function DomainSSLPage() {
  useTranslation() // dil degisince re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  // ?hedef=ana|ek:<id>|alt:<id> — alan adı süzgeci/derin bağlantı (boş = hesabın tüm alan adları).
  const [sp, setSp] = useSearchParams()
  const hamHedef = sp.get('hedef') || ''
  const hedef = /^(ana|ek:\d+|alt:\d+)$/.test(hamHedef) ? hamHedef : ''
  const hedefDegistir = (h: string) => setSp(h ? { hedef: h } : {}, { replace: true })
  const [tabloYenile, setTabloYenile] = useState(0)  // alan adı tablosunu yeniden okutur
  const anaGorunur = hedef === '' || hedef === 'ana'
  const [domain, setDomain] = useState<Domain | null>(null)
  const [durum, setDurum] = useState<SSLDurum | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  // Hata/basari artik sag ust toast'ta gosterilir; state'ler mantik icin duruyor.
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [uyari, setUyari] = useState<string | null>(null)  // LE alınamadı → self-signed fail-safe vb.
  const [mailAktif, setMailAktif] = useState(false)  // mail eklentisi kurulu+etkin mi
  const [kapsam, setKapsam] = useState<KapsamResp | null>(null)  // domain/www/mail/webmail durumu
  const [mailSSL, setMailSSL] = useState(true)       // cevir("posta sunucusunu de güvenceye al")
  // Asenkron kurulum ilerlemesi (adım-adım). isDurum: ''|calisiyor|tamam|hata.
  const [adimlar, setAdimlar] = useState<SSLAdim[]>([])
  // Sağ alt ilerleme penceresi (eklenti kurulumundaki panelin aynısı).
  // Adım listesi sayfanın içinde çiziliyordu ve her adımda uzayıp altındaki
  // kartları itiyordu; artık kendi katmanında.
  const [isToplam, setIsToplam] = useState(0)
  const [isBasladi, setIsBasladi] = useState('')
  const [isBitti, setIsBitti] = useState('')
  const [panelKucuk, setPanelKucuk] = useState(false)
  const [panelGizli, setPanelGizli] = useState(false)
  const [panelYuksek, setPanelYuksek] = useState(0)
  const [isDurum, setIsDurum] = useState<string>('')

  const yukleNesli = useRef(0)
  function yukle() {
    if (!id) return
    const _n = ++yukleNesli.current
    api.get<Domain>(`/domains/${id}`).then(r => { if (_n !== yukleNesli.current) return; setDomain(r.data) }).catch(e => { if (_n === yukleNesli.current) hataYakala(cevir("Alan adı bilgisi alınamadı"))(e) })
    api.get<SSLDurum>(`/domains/${id}/ssl`).then(r => { if (_n !== yukleNesli.current) return; setDurum(r.data) }).catch(e => {
      if (_n !== yukleNesli.current) return
      const m = apiHata(e)
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    })
    // Mail eklentisi aktifse SSL akışında mail seçeneğini göster.
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => { if (_n !== yukleNesli.current) return; setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif)) })
      .catch(() => { if (_n === yukleNesli.current) setMailAktif(false) })
    api.get<KapsamResp>(`/domains/${id}/ssl/kapsam`).then(r => { if (_n !== yukleNesli.current) return; setKapsam(r.data) }).catch(() => { if (_n === yukleNesli.current) setKapsam(null) })
  }
  useEffect(() => { yukle(); return () => { yukleNesli.current++ } }, [id])

  // İlerlemeyi çek. Biten kurulumda (tamam/hata) sonucu banner'a yazar ve durum
  // kartını yeniler. Sayfa yeniden açılırsa DEVAM EDEN iş kaldığı yerden görünür.
  function ilerlemeCek(sonra?: () => void) {
    if (!id) return
    api.get<SSLIlerleme>(`/domains/${id}/ssl/ilerleme`).then(r => {
      const d = r.data
      if (!d || d.durum === 'yok') { sonra?.(); return }
      setAdimlar(d.adimlar || [])
      setIsDurum(d.durum)
      setIsToplam(d.toplam || 0)
      setIsBasladi(d.basladi || '')
      setIsBitti(d.bitti || '')
      if (d.durum !== 'calisiyor') {
        // Bitti: cert durumunu yenile + sonucu özetle.
        yukle()
        setTabloYenile(n => n + 1)
        const uyarili = (d.adimlar || []).filter(a => a.durum === 'uyari')
        if (d.durum === 'hata') {
          const m = cevir("SSL kurulumu başarısız:") + ' ' + (d.hata || (d.adimlar || []).find(a => a.durum === 'hata')?.mesaj || '')
          setHata(m)
          toast.hata(cevir("İşlem başarısız"), m)
        } else if (uyarili.length) {
          setUyari(cevir("SSL kuruldu, bazı adımlar uyarıyla tamamlandı:") + ' ' + uyarili.map(a => a.mesaj).filter(Boolean).join(' | '))
        } else {
          setBasari(cevir("SSL kurulumu tamamlandı — site artık HTTPS üzerinden çalışıyor."))
          toast.basari(cevir("SSL kurulumu tamamlandı"), cevir("SSL kurulumu tamamlandı — site artık HTTPS üzerinden çalışıyor."))
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
    let iptal = false
    if (!id) return
    api.get<SSLIlerleme>(`/domains/${id}/ssl/ilerleme`).then(r => {
      if (iptal) return
      if (r.data && r.data.durum === 'calisiyor') {
        setAdimlar(r.data.adimlar || [])
        setIsDurum('calisiyor')
        setIsToplam(r.data.toplam || 0)
        setIsBasladi(r.data.basladi || '')
        setPanelGizli(false)
      }
    }).catch(() => {})
    return () => { iptal = true }
  }, [id])

  async function issue(tip: 'self-signed' | 'letsencrypt', mailAltlar?: string[]) {
    if (tip === 'letsencrypt' && !(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevir("Let's Encrypt sertifikası alınması için alan adının bu sunucuya DNS A kaydı ile yönlenmiş olması gerekir. Devam edilsin mi?") }))) return
    setIsleniyor(true); setHata(null); setBasari(null); setUyari(null); setAdimlar([])
    try {
      const govde: { tip: string; mail_ssl?: boolean; mail_altlar?: string[] } = { tip }
      if (tip === 'letsencrypt' && mailAktif && mailSSL) govde.mail_ssl = true
      // Kapsam kartindan secilen mail alt-alanlari: sadece onlar icin cert.
      if (tip === 'letsencrypt' && mailAltlar && mailAltlar.length) { govde.mail_ssl = true; govde.mail_altlar = mailAltlar }
      // 🔴 ASENKRON: istek HEMEN döner (cevir("başladı")); SSL çekimi arka planda sürer
      // (sayfa kapansa da). İlerleme aşağıda adım-adım gösterilir — poll başlar.
      await api.post(`/domains/${id}/ssl/issue`, govde)
      setIsDurum('calisiyor')   // poll useEffect'i tetikler
      ilerlemeCek()             // ilk adımı hemen göster
    } catch (e) {
      const m = apiHata(e, cevir("SSL kurulumu başlatılamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  // DNS'i yenile: sunucunun yerel çözümleyici önbelleğini temizler. Domain bu
  // sunucuya yeni taşındıysa eski IP önbellekte kalıp mail/webmail satırlarını
  // "DNS yok" gösterebiliyordu; temizleyip kapsamı kamu DNS'ten yeniden okuruz.
  async function dnsYenile() {
    setHata(null); setBasari(null); setUyari(null)
    try {
      const r = await api.post<{ ok: boolean; mesaj?: string }>(`/domains/${id}/ssl/dns-yenile`, {}, UZUN_ISTEK)
      const m = r.data?.mesaj || cevir("DNS önbelleği temizlendi.")
      setBasari(m)
      toast.basari(cevir("Kaydedildi"), m)
      const k = await api.get<KapsamResp>(`/domains/${id}/ssl/kapsam`)
      setKapsam(k.data)
    } catch (e) {
      const m = apiHata(e, cevir("DNS yenileme başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
  }

  async function disable() {
    if (!(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevir("SSL kaldırılsın mı? Site HTTP'ye dönecek."), ton: 'tehlike', onayIkonu: I.cop }))) return
    setIsleniyor(true); setHata(null); setBasari(null); setUyari(null)
    try {
      await api.delete(`/domains/${id}/ssl`)
      setBasari(cevir("SSL kaldırıldı. Site HTTP olarak çalışıyor."))
      toast.basari(cevir("Kaydedildi"), cevir("SSL kaldırıldı. Site HTTP olarak çalışıyor."))
      yukle()
      setTabloYenile(n => n + 1)
    } catch (e) {
      const m = apiHata(e, cevir("SSL kaldırma başarısız"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  // Panelde gösterilecek iş: süren VEYA (biten ve kullanıcı kapatmadıysa).
  const panelIsler: IlerlemeIsi[] = (isDurum && adimlar.length > 0 && (isDurum === 'calisiyor' || !panelGizli))
    ? [{
        anahtar: String(id),
        baslik: domain?.alan_adi || cevir("SSL/TLS Sertifikaları"),
        durum: isDurum,
        adimlar,
        toplam: isToplam,
        basladi: isBasladi,
        bitti: isBitti,
      }]
    : []

  return (
    <div
      style={panelYuksek > 0 ? { paddingBottom: panelYuksek + 32 } : undefined}>
      <IlerlemePaneli
        isler={panelIsler}
        kucuk={panelKucuk}
        setKucuk={setPanelKucuk}
        onGizle={() => setPanelGizli(true)}
        onYukseklik={setPanelYuksek}
        baslik={cevir("SSL kurulumu")}
      />
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

      {uyari && <UyariKutusu ton="uyari" className="mb-3">{uyari}</UyariKutusu>}
      {/* Adım listesi SAĞ ALT PANELE taşındı — sayfanın içinde büyüyüp
          altındaki kartları itmesin. Panel return'ün sonunda. */}

      {/* Hesabın TÜM alan adları (ana + ek + alt): durum tablosu + işlemler */}
      {id && (
        <SslAlanAdlari
          id={id}
          hedef={hedef}
          onHedef={hedefDegistir}
          anaKur={(tip) => { void issue(tip) }}
          anaKaldir={() => { void disable() }}
          anaMesgul={isleniyor || isDurum === 'calisiyor'}
          yenileSay={tabloYenile}
        />
      )}

      {anaGorunur && (<>
      {/* Durum kartı */}
      <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-6 mb-5">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Mevcut Durum")}</h2>
          {durum && (
            durum.aktif && durum.kaynak === 'letsencrypt' ? (
              // Yalnız GERÇEK CA (Let's Encrypt) = tarayıcıda güvenilir → yeşil Korumalı.
              <Badge color="success" variant="soft" className="gap-1.5 uppercase font-semibold tracking-wider">
                <span className="size-1.5 rounded-full bg-current" aria-hidden="true"></span>
                {cevir(cevir("Korumalı"))}
              </Badge>
            ) : durum.aktif ? (
              // Self-signed: şifreli AMA tarayıcı güvenmez → yeşil DEĞİL, amber cevir("Öz-imzalı").
              <Badge color="warning" variant="soft" className="gap-1.5 uppercase font-semibold tracking-wider">
                <span className="size-1.5 rounded-full bg-current" aria-hidden="true"></span>
                {cevir("Öz-imzalı")}
              </Badge>
            ) : (
              <Badge color="warning" variant="soft" className="gap-1.5 uppercase font-semibold tracking-wider">
                <span className="size-1.5 rounded-full bg-current" aria-hidden="true"></span>
                {cevir(cevir("Korumasız"))}
              </Badge>
            )
          )}
        </div>
        {!durum ? (
          <div className="text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
        ) : durum.aktif ? (
          <div className="space-y-2 text-sm">
            <Sat e={cevir("Kaynak")} d={durum.kaynak === 'letsencrypt' ? "Let's Encrypt" : cevir("Self-signed (öz-imzalı)")} />
            {durum.bitis_iso && <Sat e={cevir("Bitiş")} d={new Date(durum.bitis_iso).toLocaleDateString('tr-TR', { dateStyle: 'long' })} />}
            <Button
              color="error"
              variant="soft"
              onClick={disable}
              disabled={isleniyor || isDurum === 'calisiyor'}
              yukleniyor={isleniyor}
              ikon={I.cop}
              className="mt-4"
            >
              {cevir("SSL'i Kaldır (HTTP'ye dön)")}
            </Button>
          </div>
        ) : (
          <div className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500">
            {cevir(cevir("Bu domain için aktif SSL sertifikası yok. Aşağıdan birini kurabilirsiniz."))}
          </div>
        )}
      </div>

      {kapsam && kapsam.kapsamlar.length > 0 && (
        <KapsamKarti kapsam={kapsam} onKur={(altlar) => issue('letsencrypt', altlar)} onDnsYenile={dnsYenile} />
      )}

      {/* Aksiyon kartları */}
      {durum && !durum.aktif && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-6">
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
              <li className="flex items-start gap-1"><Ikon d={I.onay} className="h-3.5 w-3.5 mt-px shrink-0 text-success dark:text-success-lighter" />{cevir("DNS bağımlılığı yok")}</li>
              <li className="flex items-start gap-1"><Ikon d={I.onay} className="h-3.5 w-3.5 mt-px shrink-0 text-success dark:text-success-lighter" />{cevir("Anında kurulur")}</li>
              <li className="flex items-start gap-1"><Ikon d={I.kapat} className="h-3.5 w-3.5 mt-px shrink-0 text-error dark:text-error-lighter" />{cevir("Tarayıcıda \"güvenli değil\" uyarısı")}</li>
            </ul>
            <Button
              color="primary"
              onClick={() => issue('self-signed')}
              yukleniyor={isleniyor || isDurum === 'calisiyor'}
              ikon={I.kalkan}
              className="w-full"
            >
              {(isleniyor || isDurum === 'calisiyor') ? cevir("Kuruluyor…") : cevir("Self-Signed Kur")}
            </Button>
          </div>

          <div className="bg-white dark:bg-dark-700 border border-emerald-200 dark:border-emerald-800 rounded-lg p-6">
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
              <li className="flex items-start gap-1"><Ikon d={I.onay} className="h-3.5 w-3.5 mt-px shrink-0 text-success dark:text-success-lighter" />{cevir("Tarayıcılarda yeşil kilit")}</li>
              <li className="flex items-start gap-1"><Ikon d={I.onay} className="h-3.5 w-3.5 mt-px shrink-0 text-success dark:text-success-lighter" />{cevir("Otomatik yenileme (cron)")}</li>
              <li className="flex items-start gap-1"><Ikon d={I.uyari} className="h-3.5 w-3.5 mt-px shrink-0 text-warning dark:text-warning-lighter" />{cevir("Alan adı bu sunucuya DNS ile yönelmiş olmalı")}</li>
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
            <Button
              color="primary"
              onClick={() => issue('letsencrypt')}
              yukleniyor={isleniyor || isDurum === 'calisiyor'}
              ikon={I.kalkan}
              className="w-full"
            >
              {(isleniyor || isDurum === 'calisiyor') ? cevir("Kuruluyor…") : cevir("Let's Encrypt Sertifikası Al")}
            </Button>
          </div>
        </div>
      )}
      </>)}
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
function KapsamKarti({ kapsam, onKur, onDnsYenile }: { kapsam: KapsamResp; onKur?: (mailAltlar: string[]) => void; onDnsYenile?: () => void }) {
  const onek = (host: string) => host.split('.')[0]
  const mailSatirlar = kapsam.kapsamlar.filter(k => k.grup === 'mail')
  // Varsayilan: tum mail alt-alanlari TIKLI; kullanici gereksizi kaldirir.
  const [secili, setSecili] = useState<Set<string>>(() => new Set(mailSatirlar.map(k => onek(k.host))))
  const secTogglee = (p: string) => setSecili(s => { const n = new Set(s); if (n.has(p)) n.delete(p); else n.add(p); return n })
  const rozet = (d: KapsamSatir) => {
    if (d.durum === 'kurulu') {
      return <Badge color="success" variant="soft" className="shrink-0 gap-1.5 font-semibold"><span className="size-1.5 rounded-full bg-current" aria-hidden="true" />{cevir("Kurulu")}</Badge>
    }
    if (d.durum === 'dns_yok') {
      return <Badge variant="soft" className="shrink-0 gap-1.5 font-semibold"><span className="size-1.5 rounded-full bg-current" aria-hidden="true" />{cevir("DNS yok")}</Badge>
    }
    return <Badge color="warning" variant="soft" className="shrink-0 gap-1.5 font-semibold"><span className="size-1.5 rounded-full bg-current" aria-hidden="true" />{cevir("Eksik")}</Badge>
  }
  return (
    <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-6 mb-5">
      <div className="flex items-center justify-between mb-1">
        <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">{cevir("Sertifika Kapsamı")}</h2>
        {onDnsYenile && (
          <Button
            type="button"
            variant="outlined"
            onClick={onDnsYenile}
            title={cevir("DNS önbelleğini temizle ve kayıtları kamu DNS'ten yeniden oku")}
            boyut="sm"
            ikon={I.yenile}
          >
            {cevir("DNS'i yenile")}
          </Button>
        )}
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">
        {cevir("Her alt-alan için ayrı sertifika durumu. Posta istemcileri (Outlook, telefon)")} <span className="font-mono">mail.{'{'}alan{'}'}</span> {cevir("adına bağlanır — o satır “Kurulu” değilse istemci sertifika uyarısı verir.")}
      </p>
      <div className="divide-y divide-slate-100 dark:divide-dark-600/60">
        {kapsam.kapsamlar.map(d => (
          <div key={d.host} className="flex items-center justify-between gap-3 py-2.5">
            <div className="flex items-start gap-2.5 min-w-0">
              {d.grup === 'mail' && onKur && (
                <input type="checkbox" checked={secili.has(onek(d.host))} onChange={() => secTogglee(onek(d.host))} aria-label={cevir(d.etiket)} className="mt-1 accent-brand-600 h-4 w-4 shrink-0" />
              )}
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium text-slate-800 dark:text-slate-200">{cevir(d.etiket)}</span>
                  <Badge variant="soft" className="px-1.5 py-0.5 text-[11px] font-normal">{d.grup === 'mail' ? cevir("Posta") : 'Web'}</Badge>
                </div>
                <div className="text-xs font-mono text-slate-400 dark:text-slate-500 truncate">{d.host}</div>
                {d.durum !== 'kurulu' && d.aciklama && (
                  <div className="text-[11px] text-slate-400 dark:text-slate-500 mt-0.5">{ceviriAciklama(d.aciklama)}</div>
                )}
                {d.durum === 'kurulu' && d.bitis_iso && (
                  <div className="text-[11px] text-slate-400 dark:text-slate-500 mt-0.5">{cevir("Bitiş")}: {new Date(d.bitis_iso).toLocaleDateString(i18n.language === 'en' ? 'en-US' : 'tr-TR', { dateStyle: 'medium' })}</div>
                )}
              </div>
            </div>
            {rozet(d)}
          </div>
        ))}
      </div>
      {mailSatirlar.length > 0 && onKur && (
        <UyariKutusu ton="uyari" className="mt-4" kucuk>
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
            <span>{cevir("Seçili posta alt-alanlarına Let's Encrypt sertifikası kurulur. Kullanmadıklarını (ör. Outlook yoksa autodiscover) işaretten kaldırabilirsin.")}</span>
            <Button color="primary" boyut="sm" ikon={I.kalkan} disabled={secili.size === 0} onClick={() => onKur([...secili])} className="shrink-0">{cevir("Seçili alt-alanlara SSL kur")} ({secili.size})</Button>
          </div>
        </UyariKutusu>
      )}
    </div>
  )
}
