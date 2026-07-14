// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'

type Domain = {
  id: number; alan_adi: string; sistem_kullanici: string
  boyut_kb: number; trafik_kb: number; durum: string
  php_surum?: string; is_demo?: boolean
  olusturulma?: string; plan_id?: number; plan_ad?: string
}
type Plan = { id: number; ad: string; disk_kota_mb?: number }
type PHPVer = { surum: string; aciklama?: string }
type OlusturmaSonuc = {
  alan_adi: string; sistem_kullanici: string; ftp_user: string; ftp_host: string
  db_host: string; db_user: string; db_adi: string
  olusturulan_parolalar: { ftp: string; db: string }
}

function fmtKB(kb: number) {
  if (kb < 1024) return kb + ' KB'
  if (kb < 1024 * 1024) return (kb / 1024).toFixed(1) + ' MB'
  return (kb / 1024 / 1024).toFixed(2) + ' GB'
}

export default function DomainsPage() {
  const [items, setItems] = useState<Domain[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [q, setQ] = useState('')
  const [secili, setSecili] = useState<Set<number>>(new Set())
  const [isleniyor, setIsleniyor] = useState(false)
  const [silOnay, setSilOnay] = useState(false)

  const [planlar, setPlanlar] = useState<Plan[]>([])
  const [phpSurumler, setPhpSurumler] = useState<PHPVer[]>([])
  const [olusturAcik, setOlusturAcik] = useState(false)
  const [olusturuluyor, setOlusturuluyor] = useState(false)
  const [olusturmaSonuc, setOlusturmaSonuc] = useState<OlusturmaSonuc | null>(null)
  const [fAlanAdi, setFAlanAdi] = useState('')
  const [fPHPSurum, setFPHPSurum] = useState('8.3')
  const [fPlanID, setFPlanID] = useState<number | ''>('')

  function yukle() {
    setYuk(true)
    Promise.all([
      api.get<Domain[]>('/domains'),
      api.get<Plan[]>('/plans').catch(() => ({ data: [] })),
      api.get<PHPVer[]>('/php/versions').catch(() => ({ data: [] })),
    ]).then(([dr, pr, phpr]) => {
      setItems(dr.data)
      setPlanlar(pr.data as Plan[]); setPhpSurumler(phpr.data as PHPVer[])
    }).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  function olusturAc() {
    setHata(null); setBasari(null); setOlusturmaSonuc(null)
    // varsayılan plan = "Başlangıç" (yoksa ilk plan, o da yoksa boş)
    const varsayilan = planlar.find(p => p.ad === 'Başlangıç') || planlar[0]
    setFAlanAdi(''); setFPHPSurum('8.3'); setFPlanID(varsayilan ? varsayilan.id : '')
    setOlusturAcik(true)
  }

  async function olusturGonder(e: React.FormEvent) {
    e.preventDefault()
    setHata(null)
    const alanAdi = fAlanAdi.trim().toLowerCase()
    if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(alanAdi)) {
      setHata('Geçersiz alan adı. Örn: ornek.com veya panel.ornek.com')
      return
    }
    setOlusturuluyor(true)
    try {
      const body: any = { alan_adi: alanAdi, php_surum: fPHPSurum }
      if (fPlanID !== '') body.plan_id = fPlanID
      const r = await api.post<OlusturmaSonuc>('/domains', body)
      setOlusturAcik(false)
      setOlusturmaSonuc(r.data)
      setBasari(`✓ "${alanAdi}" oluşturuldu — Linux user, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL DB ve DNS zone hazır.`)
      setTimeout(() => setBasari(null), 8000)
      yukle()
    } catch (e: any) {
      setHata(apiHata(e, 'Domain oluşturulamadı'))
    } finally {
      setOlusturuluyor(false)
    }
  }

  async function panoYaz(metin: string) {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(metin); return true
      }
    } catch {}
    try {
      const ta = document.createElement('textarea')
      ta.value = metin; ta.style.position = 'fixed'; ta.style.opacity = '0'
      document.body.appendChild(ta); ta.select(); document.execCommand('copy')
      document.body.removeChild(ta); return true
    } catch {}
    try { window.prompt('Kopyalamak için Ctrl+C, sonra Enter:', metin); return true } catch {}
    return false
  }

  const filtreli = useMemo(() => {
    const s = q.trim().toLowerCase()
    if (!s) return items
    return items.filter(d => d.alan_adi.toLowerCase().includes(s) || d.sistem_kullanici.toLowerCase().includes(s))
  }, [items, q])

  function togga(id: number) {
    setSecili(prev => {
      const yeni = new Set(prev)
      if (yeni.has(id)) yeni.delete(id); else yeni.add(id)
      return yeni
    })
  }
  function tumunuSec(secVar: boolean) {
    if (secVar) setSecili(new Set(filtreli.map(d => d.id)))
    else setSecili(new Set())
  }

  async function topluSil() {
    setSilOnay(false); setIsleniyor(true); setHata(null)
    const ids = Array.from(secili); let basarili = 0
    for (const id of ids) {
      try { await api.delete(`/domains/${id}`); basarili++ } catch {}
    }
    setSecili(new Set()); setBasari(`✓ ${basarili}/${ids.length} domain silindi`)
    setTimeout(() => setBasari(null), 4000)
    setIsleniyor(false); yukle()
  }

  async function durumDegistir(yeniDurum: 'aktif' | 'pasif') {
    setIsleniyor(true); setHata(null)
    const ids = Array.from(secili)
    try {
      await api.post('/domains/toplu/durum', { ids, durum: yeniDurum })
      setBasari(`✓ ${ids.length} domain "${yeniDurum}" durumuna geçirildi`)
      setTimeout(() => setBasari(null), 4000)
      setSecili(new Set()); yukle()
    } catch (e) { setHata(apiHata(e, 'Durum değiştirme başarısız')) }
    finally { setIsleniyor(false) }
  }

  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler' }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-2">Domainler</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        Tüm kayıtlı domainlerinizi listeleyin, toplu seçim ile durum değiştirin veya silin.
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {/* Toolbar */}
      <div className="flex items-center gap-2 mb-3 flex-wrap">
        <div className="flex-1 max-w-md">
          <input type="text" value={q} onChange={e => setQ(e.target.value)}
            placeholder="🔍 Domain ara..."
            className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 outline-none" />
        </div>
        <span className="text-xs text-slate-500 dark:text-slate-500">{filtreli.length} / {items.length}</span>
        <button onClick={olusturAc}
          className="ml-auto inline-flex items-center gap-1.5 text-sm px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 rounded-md font-medium shadow-sm">
          <span className="text-base leading-none">+</span> Yeni Domain
        </button>
      </div>

      {/* Toplu işlem barı */}
      {secili.size > 0 && (
        <div className="mb-3 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-300 dark:border-amber-700 rounded-md flex items-center gap-2 flex-wrap">
          <span className="text-sm font-semibold text-amber-800 dark:text-amber-200">{secili.size} seçili</span>
          <button onClick={() => durumDegistir('aktif')} disabled={isleniyor}
            className="text-xs px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded">
            ▶ Aktif Et
          </button>
          <button onClick={() => durumDegistir('pasif')} disabled={isleniyor}
            className="text-xs px-3 py-1.5 bg-slate-600 hover:bg-slate-700 text-white rounded">
            ⏸ Pasif Et
          </button>
          <button onClick={() => setSilOnay(true)} disabled={isleniyor}
            className="text-xs px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white rounded font-medium">
            🗑 Sil ({secili.size})
          </button>
          <button onClick={() => setSecili(new Set())} disabled={isleniyor}
            className="text-xs px-3 py-1.5 border border-amber-300 dark:border-amber-700 text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:bg-amber-900/30 rounded">
            Seçimi temizle
          </button>
        </div>
      )}

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
      ) : items.length === 0 ? (
        <EmptyState baslik="Henüz domain yok"
          aciklama="İlk domain'inizi ekleyerek başlayın. Linux kullanıcı, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL veritabanı ve DNS zone otomatik oluşturulur."
          buton={{ etiket: 'Domain Oluştur', onClick: olusturAc }} />
      ) : (
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden">
          <table className="w-full">
            <thead className="bg-slate-50 dark:bg-slate-900 text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500 border-b border-slate-200 dark:border-slate-700">
              <tr>
                <th className="px-3 py-2.5 w-10 text-center">
                  <input type="checkbox"
                    checked={filtreli.length > 0 && secili.size === filtreli.length}
                    ref={ref => { if (ref) ref.indeterminate = secili.size > 0 && secili.size < filtreli.length }}
                    onChange={e => tumunuSec(e.target.checked)}
                    className="cursor-pointer" />
                </th>
                <th className="text-left px-4 py-2.5">Domain Adı</th>
                <th className="text-left px-4 py-2.5">Sistem Kullanıcısı</th>
                <th className="text-left px-4 py-2.5">Plan</th>
                <th className="text-left px-4 py-2.5">PHP</th>
                <th className="text-left px-4 py-2.5">Disk</th>
                <th className="text-left px-4 py-2.5">Durum</th>
                <th className="text-left px-4 py-2.5">Oluşturulma</th>
                <th className="text-right px-4 py-2.5">İşlemler</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {filtreli.map(d => {
                return (
                  <tr key={d.id} className={`hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 transition ${secili.has(d.id) ? 'bg-brand-50 dark:bg-brand-900/20' : ''}`}>
                    <td className="px-3 py-2.5 text-center">
                      <input type="checkbox" checked={secili.has(d.id)}
                        onChange={() => togga(d.id)} className="cursor-pointer" />
                    </td>
                    <td className="px-4 py-2.5">
                      <Link to={`/abonelikler/${d.id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">
                        {d.alan_adi}
                      </Link>
                      {d.is_demo && <span className="ml-2 text-[10px] uppercase tracking-wider bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 px-1.5 py-0.5 rounded">DEMO</span>}
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500">{d.sistem_kullanici}</td>
                    <td className="px-4 py-2.5 text-sm">
                      {d.plan_ad ? <span className="text-slate-700 dark:text-slate-300">{d.plan_ad}</span> : <span className="text-slate-400 dark:text-slate-500 italic">—</span>}
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500">{d.php_surum || '-'}</td>
                    <td className="px-4 py-2.5 font-mono text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500">{fmtKB(d.boyut_kb)}</td>
                    <td className="px-4 py-2.5">
                      <span className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded font-semibold ${
                        d.durum === 'aktif' ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-500'
                      }`}>{d.durum}</span>
                    </td>
                    <td className="px-4 py-2.5 font-mono text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 whitespace-nowrap">{d.olusturulma || '-'}</td>
                    <td className="px-4 py-2.5 text-right">
                      <Link to={`/abonelikler/${d.id}`} className="text-xs text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300">Yönet →</Link>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Domain Oluştur Modal */}
      {olusturAcik && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => !olusturuluyor && setOlusturAcik(false)}>
          <form onSubmit={olusturGonder} className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-lg p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-1">Yeni Domain Oluştur</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">
              Linux kullanıcı, nginx vhost, PHP-FPM havuzu, FTP hesabı, MySQL veritabanı ve DNS zone otomatik kurulur.
            </p>

            {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

            <div className="space-y-3">
              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Alan adı <span className="text-red-500">*</span></label>
                <input
                  type="text"
                  value={fAlanAdi}
                  onChange={e => setFAlanAdi(e.target.value)}
                  placeholder="ornek.com"
                  autoFocus
                  required
                  disabled={olusturuluyor}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/15 outline-none"
                />
                <div className="text-[11px] text-slate-400 dark:text-slate-500 mt-1">Küçük harf, rakam, tire ve nokta. Örn: <span className="font-mono">site.com</span> veya <span className="font-mono">panel.site.com</span></div>
              </div>

              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">PHP Sürümü</label>
                <select
                  value={fPHPSurum}
                  onChange={e => setFPHPSurum(e.target.value)}
                  disabled={olusturuluyor}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 outline-none bg-white dark:bg-slate-800"
                >
                  {phpSurumler.length === 0
                    ? <option value="8.3">PHP 8.3 (varsayılan)</option>
                    : phpSurumler.map(p => (
                        <option key={p.surum} value={p.surum}>PHP {p.surum}{p.aciklama ? ` — ${p.aciklama}` : ''}</option>
                      ))
                  }
                </select>
              </div>

              <div>
                <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Hizmet Planı</label>
                <select
                  value={fPlanID}
                  onChange={e => setFPlanID(e.target.value === '' ? '' : Number(e.target.value))}
                  disabled={olusturuluyor}
                  className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 outline-none bg-white dark:bg-slate-800"
                >
                  <option value="">— (yok) —</option>
                  {planlar.map(p => (
                    <option key={p.id} value={p.id}>{p.ad}</option>
                  ))}
                </select>
              </div>
            </div>

            <div className="flex justify-end gap-2 mt-5">
              <button type="button" onClick={() => setOlusturAcik(false)} disabled={olusturuluyor}
                className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
              <button type="submit" disabled={olusturuluyor || !fAlanAdi.trim()}
                className="px-4 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 text-sm rounded font-medium inline-flex items-center gap-2">
                {olusturuluyor && (
                  <svg className="animate-spin w-3.5 h-3.5" viewBox="0 0 24 24" fill="none">
                    <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.3"/>
                    <path d="M22 12a10 10 0 0 1-10 10" stroke="currentColor" strokeWidth="3"/>
                  </svg>
                )}
                {olusturuluyor ? 'Oluşturuluyor…' : 'Oluştur'}
              </button>
            </div>
          </form>
        </div>
      )}

      {/* Oluşturma Sonucu Modal (FTP + DB parolaları) */}
      {olusturmaSonuc && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => setOlusturmaSonuc(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-lg p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-emerald-700 dark:text-emerald-300 mb-1">✓ Domain Oluşturuldu</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-4">
              <span className="font-mono text-slate-700 dark:text-slate-300">{olusturmaSonuc.alan_adi}</span> sağlamlandı. Aşağıdaki parolalar <strong>sadece bir kez</strong> gösterilir — güvenli bir yere kaydedin.
            </p>

            <div className="space-y-3">
              <div className="border border-slate-200 dark:border-slate-700 rounded-md p-3 bg-slate-50 dark:bg-slate-900">
                <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 font-semibold mb-2">FTP</div>
                <KopyaSatir e="Host" v={olusturmaSonuc.ftp_host || '—'} kopyala={panoYaz} />
                <KopyaSatir e="Kullanıcı" v={olusturmaSonuc.ftp_user} kopyala={panoYaz} />
                <KopyaSatir e="Parola" v={olusturmaSonuc.olusturulan_parolalar.ftp} kopyala={panoYaz} parola />
              </div>

              <div className="border border-slate-200 dark:border-slate-700 rounded-md p-3 bg-slate-50 dark:bg-slate-900">
                <div className="text-[10px] uppercase tracking-wider text-slate-500 dark:text-slate-500 font-semibold mb-2">MySQL Veritabanı</div>
                <KopyaSatir e="Host" v={olusturmaSonuc.db_host || 'localhost'} kopyala={panoYaz} />
                <KopyaSatir e="Veritabanı" v={olusturmaSonuc.db_adi} kopyala={panoYaz} />
                <KopyaSatir e="Kullanıcı" v={olusturmaSonuc.db_user} kopyala={panoYaz} />
                <KopyaSatir e="Parola" v={olusturmaSonuc.olusturulan_parolalar.db} kopyala={panoYaz} parola />
              </div>

              <div className="text-[11px] text-slate-500 dark:text-slate-500 italic">
                Sistem kullanıcısı: <span className="font-mono">{olusturmaSonuc.sistem_kullanici}</span>
              </div>
            </div>

            <div className="flex justify-end mt-5">
              <button onClick={() => setOlusturmaSonuc(null)}
                className="px-4 py-1.5 bg-slate-700 hover:bg-slate-800 text-white text-sm rounded">Tamam</button>
            </div>
          </div>
        </div>
      )}

      {/* Toplu Sil Onay */}
      {silOnay && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => setSilOnay(false)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-red-700 dark:text-red-300 mb-2">⚠ Toplu Domain Silme</h3>
            <p className="text-sm text-slate-700 dark:text-slate-300 mb-3">
              <span className="font-semibold">{secili.size}</span> domain ve tüm bağımlı kaynakları (Linux kullanıcı, ev dizini, DB, FTP, vhost, DNS zone) <strong>geri dönüşsüz</strong> silinecek.
            </p>
            <ul className="text-xs font-mono text-slate-500 dark:text-slate-500 bg-slate-50 dark:bg-slate-900 rounded p-2 max-h-40 overflow-auto mb-4">
              {Array.from(secili).slice(0, 8).map(id => {
                const d = items.find(x => x.id === id)
                return <li key={id} className="truncate">{d?.alan_adi || '?'}</li>
              })}
              {secili.size > 8 && <li className="text-slate-400 dark:text-slate-500 italic">+ {secili.size - 8} daha…</li>}
            </ul>
            <div className="flex justify-end gap-2">
              <button onClick={() => setSilOnay(false)}
                className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
              <button onClick={topluSil} disabled={isleniyor}
                className="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded font-medium">
                Evet, Sil
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function KopyaSatir({ e, v, kopyala, parola }: { e: string; v: string; kopyala: (m: string) => Promise<boolean>; parola?: boolean }) {
  const [kopyalandi, setKopyalandi] = useState(false)
  const [acik, setAcik] = useState(!parola)
  async function tikla() {
    const ok = await kopyala(v)
    if (ok) { setKopyalandi(true); setTimeout(() => setKopyalandi(false), 1500) }
  }
  return (
    <div className="flex items-center gap-2 text-xs py-1">
      <span className="w-24 text-slate-500 dark:text-slate-500 shrink-0">{e}</span>
      <code
        onClick={tikla}
        className={`flex-1 font-mono px-2 py-1 rounded border cursor-pointer select-all transition ${
          kopyalandi ? 'border-emerald-300 bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300' : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 hover:border-brand-400 text-slate-800 dark:text-slate-200'
        }`}
        title="Tıklayarak kopyala"
      >
        {parola && !acik ? '••••••••••' : v}
      </code>
      {parola && (
        <button type="button" onClick={() => setAcik(s => !s)}
          className="text-[10px] px-1.5 py-0.5 rounded border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800">
          {acik ? 'Gizle' : 'Göster'}
        </button>
      )}
      {kopyalandi && <span className="text-[10px] text-emerald-600 dark:text-emerald-400 font-semibold">Kopyalandı</span>}
    </div>
  )
}