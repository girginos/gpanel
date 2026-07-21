import { useEffect, useMemo, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Domain = { id: number; alan_adi: string }
type Sonuc = { site_url: string; admin_url: string; admin_kullanici: string; admin_parola: string; surum: string }
type TumKurulum = {
  domain_id: number; alan_adi: string; dizin: string; surum: string
  son_surum: string; durum: 'guncel' | 'eski' | 'bilinmiyor'; kurulum_tarihi: string
  site_url: string; admin_url: string
}

export default function WordPressPage() {
  const [domainler, setDomainler] = useState<Domain[]>([])
  const [domainId, setDomainId] = useState<number | null>(null)
  const [tum, setTum] = useState<TumKurulum[]>([])
  const [tumYuk, setTumYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [kuruyor, setKuruyor] = useState(false)
  const [sonuc, setSonuc] = useState<Sonuc | null>(null)
  const [mesgul, setMesgul] = useState<string | null>(null)

  const [altDizin, setAltDizin] = useState('')
  const [baslik, setBaslik] = useState('')
  const [adminK, setAdminK] = useState('admin')
  const [adminE, setAdminE] = useState('')

  useEffect(() => {
    api.get<Domain[]>('/domains').then(r => {
      setDomainler(r.data || [])
      if (r.data?.length) setDomainId(r.data[0].id)
    }).catch(e => setHata(apiHata(e)))
    tumListele()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  function tumListele() {
    setTumYuk(true)
    api.get<TumKurulum[]>('/wordpress/tumu')
      .then(r => setTum(r.data || []))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setTumYuk(false))
  }

  async function kur(e: React.FormEvent) {
    e.preventDefault()
    if (!domainId) return
    setHata(null); setSonuc(null); setKuruyor(true)
    try {
      const { data } = await api.post<Sonuc>(`/domains/${domainId}/wordpress`, {
        alt_dizin: altDizin.trim(), site_basligi: baslik.trim(), admin_kullanici: adminK.trim(), admin_email: adminE.trim(),
      })
      setSonuc(data); setBaslik(''); setAltDizin('')
      tumListele()
    } catch (err) { setHata(apiHata(err, 'Kurulum başarısız')) }
    finally { setKuruyor(false) }
  }

  async function guncelle(t: TumKurulum) {
    const key = t.domain_id + t.dizin
    setMesgul(key); setHata(null)
    try { await api.post(`/domains/${t.domain_id}/wordpress/guncelle`, { dizin: t.dizin }); tumListele() }
    catch (err) { setHata(apiHata(err, 'Güncellenemedi')) }
    finally { setMesgul(null) }
  }

  async function sil(t: TumKurulum) {
    if (t.dizin.includes('kök')) { alert('Kök dizindeki WordPress panelden silinemez.'); return }
    if (!confirm(`${t.alan_adi}${t.dizin} altındaki WordPress silinsin mi?\nBu dizindeki tüm dosyalar ve veritabanı kaldırılır. Geri alınamaz.`)) return
    const key = t.domain_id + t.dizin
    setMesgul(key); setHata(null)
    try {
      await api.delete(`/domains/${t.domain_id}/wordpress`, { data: { dizin: t.dizin, db_sil: true } })
      tumListele()
    } catch (err) { setHata(apiHata(err, 'Silinemedi')) }
    finally { setMesgul(null) }
  }

  const sel = domainler.find(d => d.id === domainId)
  const eskiler = useMemo(() => tum.filter(t => t.durum === 'eski'), [tum])

  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'WordPress' }]} />
      <div className="flex items-center gap-3 mb-1">
        <span className="text-2xl">📝</span>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">WordPress</h1>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">Sunucudaki tüm WordPress kurulumlarını görüntüleyin, güncelleyin ve yeni kurulum yapın.</p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {/* Güvenlik uyarı bandı */}
      {!tumYuk && eskiler.length > 0 && (
        <div className="mb-4 px-4 py-3 rounded-2xl border border-amber-300 dark:border-amber-800 bg-amber-50 dark:bg-amber-900/20 flex items-start gap-3">
          <span className="text-lg leading-none">⚠️</span>
          <div className="text-sm text-amber-800 dark:text-amber-200">
            <strong>{eskiler.length} kurulumda güncelleme mevcut.</strong> Eski WordPress sürümleri bilinen güvenlik açıkları içerir — en kısa sürede güncelleyin.
            <div className="mt-1 text-xs text-amber-700 dark:text-amber-300 font-mono">
              {eskiler.map(e => `${e.alan_adi}${e.dizin === '/ (kök)' ? '' : e.dizin}`).join(' · ')}
            </div>
          </div>
        </div>
      )}

      {/* Kurulum sonucu — kimlik bilgileri (bir kez) */}
      {sonuc && (
        <div className="mb-4 rounded-2xl border border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/15 p-4">
          <div className="flex items-center gap-2 text-sm font-semibold text-emerald-700 dark:text-emerald-300 mb-2">
            ✅ WordPress {sonuc.surum} kuruldu
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-1.5 text-sm">
            <Bilgi et="Site" v={sonuc.site_url} link />
            <Bilgi et="Yönetim" v={sonuc.admin_url} link />
            <Bilgi et="Kullanıcı" v={sonuc.admin_kullanici} mono />
            <Bilgi et="Parola" v={sonuc.admin_parola} mono />
          </div>
          <p className="text-[11px] text-amber-700 dark:text-amber-400 mt-2">⚠ Parolayı şimdi kaydedin — tekrar gösterilmez.</p>
        </div>
      )}

      {/* Geniş tablo: tüm kurulumlar */}
      <div className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl overflow-hidden mb-6">
        <div className="flex items-center justify-between px-4 py-3 border-b border-slate-100 dark:border-slate-700/60">
          <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Kurulu WordPress Siteleri {!tumYuk && <span className="text-slate-400 font-normal">· {tum.length}</span>}</h3>
          <button onClick={tumListele} disabled={tumYuk} className="text-xs px-2.5 py-1 border border-slate-200 dark:border-slate-700 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50">↻ Yenile</button>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="bg-slate-50 dark:bg-slate-900/50 text-[11px] uppercase tracking-wider text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-slate-700/60">
              <tr>
                <th className="text-left font-medium px-4 py-2.5">Domain</th>
                <th className="text-left font-medium px-4 py-2.5">Dizin</th>
                <th className="text-left font-medium px-4 py-2.5">Sürüm</th>
                <th className="text-left font-medium px-4 py-2.5">Durum</th>
                <th className="text-left font-medium px-4 py-2.5 whitespace-nowrap">Kurulum</th>
                <th className="text-right font-medium px-4 py-2.5">İşlemler</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-700/60">
              {tumYuk ? (
                <tr><td colSpan={6} className="px-4 py-10 text-center text-sm text-slate-400">Kurulumlar taranıyor… (sürüm + güncelleme kontrolü)</td></tr>
              ) : tum.length === 0 ? (
                <tr><td colSpan={6} className="px-4 py-10 text-center">
                  <div className="text-2xl mb-1">📝</div>
                  <p className="text-sm text-slate-500 dark:text-slate-400">Sunucuda hiç WordPress kurulumu bulunamadı.</p>
                  <p className="text-xs text-slate-400 mt-1">Aşağıdaki formdan yeni bir kurulum yapabilirsiniz.</p>
                </td></tr>
              ) : (
                tum.map(t => {
                  const key = t.domain_id + t.dizin
                  const eski = t.durum === 'eski'
                  return (
                    <tr key={key} className={eski ? 'bg-amber-50/50 dark:bg-amber-900/10' : 'hover:bg-slate-50 dark:hover:bg-slate-800/40'}>
                      <td className="px-4 py-2.5">
                        <a href={t.site_url} target="_blank" rel="noreferrer" className="font-medium text-slate-800 dark:text-slate-100 hover:text-brand-600 dark:hover:text-brand-400">{t.alan_adi}</a>
                      </td>
                      <td className="px-4 py-2.5 font-mono text-xs text-slate-500 dark:text-slate-400 whitespace-nowrap">{t.dizin}</td>
                      <td className="px-4 py-2.5">
                        <span className="text-xs px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300 font-mono font-semibold">{t.surum ? `v${t.surum}` : '—'}</span>
                      </td>
                      <td className="px-4 py-2.5"><DurumRozet t={t} /></td>
                      <td className="px-4 py-2.5 text-xs text-slate-500 dark:text-slate-400 font-mono whitespace-nowrap">{t.kurulum_tarihi || '—'}</td>
                      <td className="px-4 py-2.5">
                        <div className="flex items-center justify-end gap-1.5">
                          <a href={t.admin_url} target="_blank" rel="noreferrer" className="text-xs px-2.5 py-1 border border-slate-200 dark:border-slate-700 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700">Yönetim</a>
                          <button disabled={!!mesgul} onClick={() => guncelle(t)}
                            className={`text-xs px-2.5 py-1 rounded-md disabled:opacity-50 ${eski ? 'bg-amber-500 hover:bg-amber-600 text-white' : 'border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700'}`}>
                            {mesgul === key ? '…' : eski ? `Güncelle → v${t.son_surum}` : 'Güncelle'}
                          </button>
                          {!t.dizin.includes('kök') && (
                            <button disabled={!!mesgul} onClick={() => sil(t)} className="text-xs px-2.5 py-1 border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 rounded-md hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50">Sil</button>
                          )}
                        </div>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* Yeni kurulum */}
      <form onSubmit={kur} className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl p-4 max-w-2xl">
        <h3 className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-3">Yeni Kurulum</h3>
        <div className="mb-3">
          <label className="block text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-1.5">Domain</label>
          <select value={domainId ?? ''} onChange={e => setDomainId(Number(e.target.value))}
            className="w-full sm:w-80 px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none">
            {domainler.map(d => <option key={d.id} value={d.id}>{d.alan_adi}</option>)}
          </select>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Alan et="Site Başlığı" v={baslik} set={setBaslik} zorunlu ph="Benim Blogum" />
          <Alan et="Alt Dizin (isteğe bağlı)" v={altDizin} set={setAltDizin} ph="boş = kök · örn: blog" mono />
          <Alan et="Admin Kullanıcı" v={adminK} set={setAdminK} zorunlu mono />
          <Alan et="Admin E-posta" v={adminE} set={setAdminE} zorunlu type="email" ph="admin@site.com" />
        </div>
        <button disabled={kuruyor || !domainId} className="mt-3 px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-lg disabled:opacity-50">
          {kuruyor ? 'Kuruluyor… (~30 sn)' : `WordPress Kur${sel ? ` · ${sel.alan_adi}` : ''}`}
        </button>
      </form>
    </div>
  )
}

function DurumRozet({ t }: { t: TumKurulum }) {
  if (t.durum === 'eski') {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/40 text-amber-800 dark:text-amber-200 font-medium">
        <span className="w-1.5 h-1.5 rounded-full bg-amber-500"></span>
        Güncelleme var{t.son_surum && ` → v${t.son_surum}`}
      </span>
    )
  }
  if (t.durum === 'guncel') {
    return (
      <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300 font-medium">
        <span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
        Güncel
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400 font-medium">
      Bilinmiyor
    </span>
  )
}

function Alan({ et, v, set, zorunlu, ph, mono, type }: { et: string; v: string; set: (s: string) => void; zorunlu?: boolean; ph?: string; mono?: boolean; type?: string }) {
  return (
    <label className="block">
      <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold">{et}</span>
      <input value={v} onChange={e => set(e.target.value)} required={zorunlu} placeholder={ph} type={type || 'text'}
        className={`mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none ${mono ? 'font-mono' : ''}`} />
    </label>
  )
}
function Bilgi({ et, v, mono, link }: { et: string; v: string; mono?: boolean; link?: boolean }) {
  return (
    <div className="flex items-baseline gap-1.5 min-w-0">
      <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold shrink-0">{et}</span>
      {link ? <a href={v} target="_blank" rel="noreferrer" className="text-xs text-brand-600 dark:text-brand-400 hover:underline truncate font-mono">{v}</a>
        : <span className={`text-xs text-slate-800 dark:text-slate-100 truncate ${mono ? 'font-mono' : ''}`}>{v}</span>}
    </div>
  )
}
