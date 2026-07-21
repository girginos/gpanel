// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; ipv4: string; ssl: boolean; ssl_bitis?: string }
type SSLDurum = {
  aktif: boolean
  kaynak: string
  bitis_iso?: string
  cert_yol?: string
  key_yol?: string
}

export default function DomainSSLPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [durum, setDurum] = useState<SSLDurum | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)

  function yukle() {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(() => {})
    api.get<SSLDurum>(`/domains/${id}/ssl`).then(r => setDurum(r.data)).catch(e => setHata(apiHata(e)))
  }
  useEffect(yukle, [id])

  async function issue(tip: 'self-signed' | 'letsencrypt') {
    if (tip === 'letsencrypt' && !confirm('Let\'s Encrypt sertifikası alınması için alan adının bu sunucuya DNS A kaydı ile yönlenmiş olması gerekir. Devam edilsin mi?')) return
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/domains/${id}/ssl/issue`, { tip })
      setBasari(`Sertifika kuruldu (${tip}). Bitiş: ${data.bitis}. Site artık HTTPS üzerinden çalışıyor.`)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'SSL kurulumu başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  async function disable() {
    if (!confirm('SSL kaldırılsın mı? Site HTTP\'ye dönecek.')) return
    setIsleniyor(true); setHata(null); setBasari(null)
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
    <div className="px-6 py-5 max-w-[1100px]">
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
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {/* Durum kartı */}
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-6 mb-5">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100">Mevcut Durum</h2>
          {durum && (
            durum.aktif ? (
              <span className="text-xs px-2 py-1 bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 rounded uppercase font-semibold tracking-wider flex items-center gap-1.5">
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
                Korumalı
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
              disabled={isleniyor}
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
              disabled={isleniyor}
              className="w-full px-4 py-2.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md transition"
            >
              {isleniyor ? 'Kuruluyor…' : 'Self-Signed Kur'}
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
            <button
              onClick={() => issue('letsencrypt')}
              disabled={isleniyor}
              className="w-full px-4 py-2.5 bg-emerald-600 hover:bg-emerald-700 disabled:bg-emerald-300 text-white text-sm font-medium rounded-md transition"
            >
              {isleniyor ? 'Kuruluyor…' : 'Let\'s Encrypt Sertifikası Al'}
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