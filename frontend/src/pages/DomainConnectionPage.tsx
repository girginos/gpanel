// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

function panoYaz(text: string): boolean {
  // 1) Modern API (HTTPS / localhost only) — kullanıcı gesture içindeyse async ok
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).catch(() => {})
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
    window.prompt('Otomatik kopyalanamadı. Ctrl+C basıp Enter\'a tıklayın:', text)
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
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [kopya, setKopya] = useState<string | null>(null)
  const [parolaModal, setParolaModal] = useState<{ tip: 'ftp' | 'db'; cikti?: string } | null>(null)

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e)))
  }, [id])

  function kopyala(deg: string) {
    panoYaz(deg)
    setKopya(deg)
    setTimeout(() => setKopya(null), 1800)
  }

  return (
    <div className="px-6 py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'Bağlantı Bilgisi' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Bağlantı Bilgisi</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}<span className="text-xs text-slate-400 dark:text-slate-500">Değer üstüne tıkla → otomatik kopyalanır</span>
        </p>
      )}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {domain && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
          <Kart baslik="FTP / SFTP" renk="sky" ikon="M3 16V8a2 2 0 012-2h6l2 2h5a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2z">
            <Sat e="Sunucu" d={domain.ftp_host} onKopya={kopyala} kopya={kopya} />
            <Sat e="Port" d="21" onKopya={kopyala} kopya={kopya} />
            <Sat e="Kullanıcı adı" d={domain.ftp_user} onKopya={kopyala} kopya={kopya} mono />
            <Parola e="Parola" id={id!} tip="ftp" onAc={() => setParolaModal({ tip: 'ftp' })} />
            <Sat e="Ev dizini" d={`/home/${domain.sistem_kullanici}`} onKopya={kopyala} kopya={kopya} mono />
            <Link to={`/abonelikler/${id}/ftp`} className="block mt-2 text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">FTP yönetimine git →</Link>
          </Kart>

          <Kart baslik="MySQL / MariaDB" renk="violet" ikon="M4 7c0-1.657 3.582-3 8-3s8 1.343 8 3-3.582 3-8 3-8-1.343-8-3z">
            <Sat e="Sunucu" d={domain.db_host} onKopya={kopyala} kopya={kopya} />
            <Sat e="Port" d="3306" onKopya={kopyala} kopya={kopya} />
            <Sat e="Veritabanı" d={domain.db_adi} onKopya={kopyala} kopya={kopya} mono />
            <Sat e="Kullanıcı adı" d={domain.db_user} onKopya={kopyala} kopya={kopya} mono />
            <Parola e="Parola" id={id!} tip="db" onAc={() => setParolaModal({ tip: 'db' })} />
            <Link to={`/abonelikler/${id}/veritabanlari`} className="block mt-2 text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">Veritabanları yönetimine git →</Link>
          </Kart>

          <Kart baslik="Web" renk="amber" ikon="M21 12a9 9 0 11-18 0 9 9 0 0118 0z" cift>
            <Sat e="Web kökü" d={domain.web_root} onKopya={kopyala} kopya={kopya} mono />
            <Sat e="IPv4" d={domain.ipv4} onKopya={kopyala} kopya={kopya} mono />
            <Sat e="Sistem kullanıcısı" d={domain.sistem_kullanici} onKopya={kopyala} kopya={kopya} mono />
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
    <div className={`bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 ${cift ? 'lg:col-span-2' : ''}`}>
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
    <div className="flex items-center justify-between gap-3 py-1.5 border-b border-slate-100 dark:border-slate-800 last:border-0">
      <dt className="text-slate-500 dark:text-slate-500 text-xs uppercase tracking-wider">{e}</dt>
      <dd
        onClick={() => aktif && onKopya!(d)}
        className={`text-right flex items-center gap-2 group ${aktif ? 'cursor-pointer' : ''}`}
        title={aktif ? 'Tıkla → kopyala' : ''}
      >
        <span className={`${mono ? 'font-mono text-xs' : 'text-sm'} ${aktif ? 'text-slate-800 dark:text-slate-200 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 transition' : 'text-slate-800 dark:text-slate-200'}`}>
          {d}
        </span>
        {kopyalandi && (
          <span className="text-[10px] uppercase tracking-wider bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 px-1.5 py-0.5 rounded font-medium animate-pulse">
            ✓ Kopyalandı
          </span>
        )}
      </dd>
    </div>
  )
}

function Parola({ e, onAc }: { e: string; id: string; tip: string; onAc: () => void }) {
  return (
    <div className="flex items-center justify-between gap-3 py-1.5 border-b border-slate-100 dark:border-slate-800 last:border-0">
      <dt className="text-slate-500 dark:text-slate-500 text-xs uppercase tracking-wider">{e}</dt>
      <dd className="text-right">
        <button
          onClick={onAc}
          className="text-xs px-3 py-1 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 rounded font-medium transition inline-flex items-center gap-1"
        >
          🔑 Şifreyi Göster / Yenile
        </button>
      </dd>
    </div>
  )
}

function ParolaSifirlaModal({ tip, domainId, ftpUser, dbUser, onKapat, onKopya }:
  { tip: 'ftp' | 'db'; domainId: string; ftpUser: string; dbUser: string; onKapat: () => void; onKopya: (s: string) => void }) {
  const [yeni, setYeni] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [gosterMevcut, setGosterMevcut] = useState(false)
  const [mevcutParola, setMevcutParola] = useState<string | null>(null)

  // Mevcut parolayı çek (FTP için DB'de saklı, db_pass_plain için databases endpoint)
  useEffect(() => {
    if (!gosterMevcut) return
    if (tip === 'ftp') {
      api.get<{ ftp_pass_plain: string }>(`/domains/${domainId}/ftp/parola-goster`)
        .then(r => setMevcutParola(r.data.ftp_pass_plain || '(saklanmıyor)'))
        .catch(() => setMevcutParola('(yetki yok)'))
    } else {
      api.get<any[]>(`/domains/${domainId}/databases`)
        .then(r => {
          const main = (r.data || [])[0]
          setMevcutParola(main?.db_parola || main?.db_pass_plain || '(saklanmıyor)')
        })
        .catch(() => setMevcutParola('(yetki yok)'))
    }
  }, [gosterMevcut, tip, domainId])

  async function olustur() {
    setIsleniyor(true); setHata(null)
    try {
      if (tip === 'ftp') {
        const r = await api.put<{ parola: string }>(`/domains/${domainId}/ftp/password`, {})
        setYeni(r.data.parola)
      } else {
        // İlk DB id'sini al
        const dbs = await api.get<any[]>(`/domains/${domainId}/databases`)
        const main = (dbs.data || [])[0]
        if (!main) throw new Error('veritabanı yok')
        const r = await api.put<{ parola: string }>(`/databases/${main.id}/password`, {})
        setYeni(r.data.parola)
      }
    } catch (e) { setHata(apiHata(e, 'Parola üretilemedi')) }
    finally { setIsleniyor(false) }
  }

  const user = tip === 'ftp' ? ftpUser : dbUser
  const tipAd = tip === 'ftp' ? 'FTP' : 'Veritabanı'

  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={onKapat}>
      <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={ev => ev.stopPropagation()}>
        <div className="flex items-center gap-2 mb-3">
          <span className="text-2xl">🔑</span>
          <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">{tipAd} Parolası</h3>
        </div>
        <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-4 bg-slate-50 dark:bg-slate-900 px-3 py-2 rounded">
          <span className="text-slate-500 dark:text-slate-500">Kullanıcı:</span> <code className="font-mono text-slate-900 dark:text-slate-100">{user}</code>
        </div>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-xs text-red-700 dark:text-red-300">{hata}</div>}

        {/* Mevcut parolayı göster */}
        {!yeni && (
          <div className="mb-4">
            {!gosterMevcut ? (
              <button onClick={() => setGosterMevcut(true)}
                className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded-md text-slate-700 dark:text-slate-300">
                👁 Mevcut parolayı göster
              </button>
            ) : (
              <div className="px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded">
                <div className="text-[10px] uppercase tracking-wider text-amber-700 dark:text-amber-300 mb-1">Mevcut parola</div>
                <div className="flex items-center gap-2">
                  <code className="font-mono text-sm text-slate-900 dark:text-slate-100 flex-1 break-all">{mevcutParola || '...'}</code>
                  {mevcutParola && mevcutParola.length > 5 && (
                    <KopyaButton text={mevcutParola} renk="amber" />
                  )}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Yeni parola */}
        {yeni && (
          <div className="mb-4 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded">
            <div className="text-[10px] uppercase tracking-wider text-emerald-700 dark:text-emerald-300 mb-1">✓ Yeni parola üretildi</div>
            <div className="flex items-center gap-2">
              <code className="font-mono text-sm text-slate-900 dark:text-slate-100 flex-1 break-all">{yeni}</code>
              <KopyaButton text={yeni} renk="emerald" />
            </div>
            <p className="text-[11px] text-emerald-700 dark:text-emerald-300 mt-2">⚠ Parolayı şimdi kopyalayın — bu pencereyi kapattıktan sonra tekrar göremeyebilirsiniz.</p>
          </div>
        )}

        <div className="flex gap-2 justify-end mt-4">
          <button onClick={onKapat} className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">
            Kapat
          </button>
          <button onClick={olustur} disabled={isleniyor}
            className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 text-sm rounded font-medium">
            {isleniyor ? 'Üretiliyor…' : (yeni ? '↻ Tekrar üret' : '⚡ Yeni parola üret')}
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
      {k ? '✓ Kopyalandı' : 'Kopyala'}
    </button>
  )
}