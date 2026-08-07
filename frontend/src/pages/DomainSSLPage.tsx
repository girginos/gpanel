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

export default function DomainSSLPage() {
  const { onay } = useDialog()
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [durum, setDurum] = useState<SSLDurum | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [uyari, setUyari] = useState<string | null>(null)  // LE alınamadı → self-signed fail-safe vb.
  const [mailAktif, setMailAktif] = useState(false)  // mail eklentisi kurulu+etkin mi
  const [mailSSL, setMailSSL] = useState(true)       // "posta sunucusunu de güvenceye al"
  // Asenkron kurulum ilerlemesi (adım-adım). isDurum: ''|calisiyor|tamam|hata.
  const [adimlar, setAdimlar] = useState<SSLAdim[]>([])
  const [isDurum, setIsDurum] = useState<string>('')

  function yukle() {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(hataYakala('Alan adı bilgisi alınamadı'))
    api.get<SSLDurum>(`/domains/${id}/ssl`).then(r => setDurum(r.data)).catch(e => setHata(apiHata(e)))
    // Mail eklentisi aktifse SSL akışında mail seçeneğini göster.
    api.get<{ ad: string; aktif: boolean }[]>('/eklentiler')
      .then(r => setMailAktif(r.data.some(e => e.ad === 'mail' && e.aktif)))
      .catch(() => setMailAktif(false))
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
          setHata('SSL kurulumu başarısız: ' + (d.hata || (d.adimlar || []).find(a => a.durum === 'hata')?.mesaj || ''))
        } else if (uyarili.length) {
          setUyari('SSL kuruldu, bazı adımlar uyarıyla tamamlandı: ' + uyarili.map(a => a.mesaj).filter(Boolean).join(' | '))
        } else {
          setBasari('SSL kurulumu tamamlandı — site artık HTTPS üzerinden çalışıyor.')
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
    if (tip === 'letsencrypt' && !(await onay({ baslik: 'Onay gerekiyor', mesaj: 'Let\'s Encrypt sertifikası alınması için alan adının bu sunucuya DNS A kaydı ile yönlenmiş olması gerekir. Devam edilsin mi?' }))) return
    setIsleniyor(true); setHata(null); setBasari(null); setUyari(null); setAdimlar([])
    try {
      const govde: { tip: string; mail_ssl?: boolean } = { tip }
      if (tip === 'letsencrypt' && mailAktif && mailSSL) govde.mail_ssl = true
      // 🔴 ASENKRON: istek HEMEN döner ("başladı"); SSL çekimi arka planda sürer
      // (sayfa kapansa da). İlerleme aşağıda adım-adım gösterilir — poll başlar.
      await api.post(`/domains/${id}/ssl/issue`, govde)
      setIsDurum('calisiyor')   // poll useEffect'i tetikler
      ilerlemeCek()             // ilk adımı hemen göster
    } catch (e) {
      setHata(apiHata(e, 'SSL kurulumu başlatılamadı'))
    } finally {
      setIsleniyor(false)
    }
  }

  async function disable() {
    if (!(await onay({ baslik: 'Onay gerekiyor', mesaj: 'SSL kaldırılsın mı? Site HTTP\'ye dönecek.' }))) return
    setIsleniyor(true); setHata(null); setBasari(null); setUyari(null)
    try {
      await api.delete(`/domains/${id}/ssl`)
      setBasari('SSL kaldırıldı. Site HTTP olarak çalışıyor.')
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'SSL kaldırma başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'SSL/TLS Sertifikaları' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">SSL/TLS Sertifikaları</h1>
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
              {isDurum === 'calisiyor' ? 'SSL kuruluyor…' : isDurum === 'hata' ? 'SSL kurulumu — hata' : 'SSL kurulumu tamamlandı'}
            </h2>
          </div>
          {isDurum === 'calisiyor' && (
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">
              Bu sayfayı kapatabilirsiniz; kurulum arka planda sürer. Posta SSL'inde 7 alt-alan (mail, webmail, smtp, imap, pop, autoconfig, autodiscover) tek tek doğrulandığı için 1–2 dakika sürebilir.
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
          <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">Mevcut Durum</h2>
          {durum && (
            durum.aktif && durum.kaynak === 'letsencrypt' ? (
              // Yalnız GERÇEK CA (Let's Encrypt) = tarayıcıda güvenilir → yeşil Korumalı.
              <span className="text-xs px-2 py-1 bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 rounded uppercase font-semibold tracking-wider flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
                Korumalı
              </span>
            ) : durum.aktif ? (
              // Self-signed: şifreli AMA tarayıcı güvenmez → yeşil DEĞİL, amber "Öz-imzalı".
              <span className="text-xs px-2 py-1 bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 rounded uppercase font-semibold tracking-wider flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-400"></span>
                Öz-imzalı
              </span>
            ) : (
              <span className="text-xs px-2 py-1 bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 rounded uppercase font-semibold tracking-wider flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-400"></span>
                Korumasız
              </span>
            )
          )}
        </div>
        {!durum ? (
          <div className="text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
        ) : durum.aktif ? (
          <div className="space-y-2 text-sm">
            <Sat e="Kaynak" d={durum.kaynak === 'letsencrypt' ? "Let's Encrypt" : 'Self-signed (öz-imzalı)'} />
            {durum.bitis_iso && <Sat e="Bitiş" d={new Date(durum.bitis_iso).toLocaleDateString('tr-TR', { dateStyle: 'long' })} />}
            <Sat e="Sertifika yolu" d={durum.cert_yol || '—'} mono />
            <Sat e="Anahtar yolu" d={durum.key_yol || '—'} mono />
            <button
              onClick={disable}
              disabled={isleniyor || isDurum === 'calisiyor'}
              className="mt-4 px-4 py-2 border border-red-300 dark:border-red-700 text-red-700 dark:text-red-300 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 disabled:opacity-50 rounded-md text-sm font-medium transition"
            >
              SSL'i Kaldır (HTTP'ye dön)
            </button>
          </div>
        ) : (
          <div className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500">
            Bu domain için aktif SSL sertifikası yok. Aşağıdan birini kurabilirsiniz.
          </div>
        )}
      </div>

      {/* Aksiyon kartları */}
      {durum && !durum.aktif && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-6">
            <div className="flex items-center gap-2 mb-2">
              <div className="w-9 h-9 rounded-lg bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 flex items-center justify-center">
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}><path d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"/></svg>
              </div>
              <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">Self-Signed Sertifika</h3>
            </div>
            <p className="text-sm text-slate-500 dark:text-slate-500 mb-4">
              Sunucu tarafından öz-imzalı bir sertifika oluşturur. Tarayıcı uyarısı verir ama bağlantı şifrelidir.
              Test/dev ortamı için uygun.
            </p>
            <ul className="text-xs text-slate-500 dark:text-slate-500 mb-4 space-y-1">
              <li>✓ DNS bağımlılığı yok</li>
              <li>✓ Anında kurulur</li>
              <li>✗ Tarayıcıda "güvenli değil" uyarısı</li>
            </ul>
            <button
              onClick={() => issue('self-signed')}
              disabled={isleniyor || isDurum === 'calisiyor'}
              className="w-full px-4 py-2.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md transition"
            >
              {(isleniyor || isDurum === 'calisiyor') ? 'Kuruluyor…' : 'Self-Signed Kur'}
            </button>
          </div>

          <div className="bg-white dark:bg-slate-800 border border-emerald-200 dark:border-emerald-800 rounded-2xl p-6">
            <div className="flex items-center gap-2 mb-2">
              <div className="w-9 h-9 rounded-lg bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 flex items-center justify-center">
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}><path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z"/></svg>
              </div>
              <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">Let's Encrypt (Ücretsiz)</h3>
            </div>
            <p className="text-sm text-slate-500 dark:text-slate-500 mb-4">
              Resmi Let's Encrypt sertifikası, tüm tarayıcılarda otomatik güvenilir. 90 günde bir otomatik yenilenir.
            </p>
            <ul className="text-xs text-slate-500 dark:text-slate-500 mb-4 space-y-1">
              <li>✓ Tarayıcılarda yeşil kilit</li>
              <li>✓ Otomatik yenileme (cron)</li>
              <li>⚠ Alan adı bu sunucuya DNS ile yönelmiş olmalı</li>
            </ul>
            {mailAktif && (
              <label className="flex items-start gap-2 mb-4 p-2.5 rounded-lg bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 cursor-pointer">
                <input type="checkbox" checked={mailSSL} onChange={e => setMailSSL(e.target.checked)} className="mt-0.5 cursor-pointer" />
                <span className="text-xs text-slate-700 dark:text-slate-300">
                  <b>Posta sunucusunu da güvenceye al</b> — <span className="font-mono">mail.{domain?.alan_adi}</span> + <span className="font-mono">webmail.{domain?.alan_adi}</span> için ayrı sertifika alınıp IMAP/POP/SMTP + webmail'e kurulur.
                  <span className="block text-[11px] text-slate-400 dark:text-slate-500 mt-0.5">Bu alt alanların da sunucuya DNS A kaydı gerekir.</span>
                </span>
              </label>
            )}
            <button
              onClick={() => issue('letsencrypt')}
              disabled={isleniyor || isDurum === 'calisiyor'}
              className="w-full px-4 py-2.5 bg-emerald-600 hover:bg-emerald-700 disabled:bg-emerald-300 text-white text-sm font-medium rounded-md transition"
            >
              {(isleniyor || isDurum === 'calisiyor') ? 'Kuruluyor…' : 'Let\'s Encrypt Sertifikası Al'}
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