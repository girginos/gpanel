import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'

type Row = {
  id?: number
  ad: string
  tip: string
  deger: string
  ttl: number
  oncelik: number
  sira: number
  aktif: boolean
}
type Meta = {
  soa_refresh: number
  soa_retry: number
  soa_expire: number
  soa_minimum: number
  soa_ttl: number
  dkim_selector: string
  dkim_aktif: boolean
}

const TIPLER = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA', 'PTR']

export default function DNSSablonuPage() {
  const [rows, setRows] = useState<Row[]>([])
  const [meta, setMeta] = useState<Meta | null>(null)
  const [yuk, setYuk] = useState(true)
  const [kaydediyor, setKaydediyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)

  function yukle() {
    setYuk(true)
    api.get<{ kayitlar: Row[]; meta: Meta }>('/dns-template')
      .then(r => { setRows(r.data.kayitlar || []); setMeta(r.data.meta) })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  function setRow(i: number, patch: Partial<Row>) {
    setRows(rs => rs.map((r, idx) => idx === i ? { ...r, ...patch } : r))
  }
  function satirEkle() {
    setRows(rs => [...rs, { ad: '@', tip: 'A', deger: '{IP}', ttl: 3600, oncelik: 0, sira: (rs.length + 1) * 10, aktif: true }])
  }
  function satirSil(i: number) {
    setRows(rs => rs.filter((_, idx) => idx !== i))
  }

  async function kaydet() {
    if (!meta) return
    setHata(null); setBasari(null); setKaydediyor(true)
    try {
      await api.put('/dns-template', { kayitlar: rows, meta })
      setBasari('Şablon kaydedildi. Yeni domainler ve "Varsayılan Şablonu Uygula" bu şablonu kullanır.')
      setTimeout(() => setBasari(null), 5000)
      yukle()
    } catch (e) { setHata(apiHata(e, 'Şablon kaydedilemedi')) }
    finally { setKaydediyor(false) }
  }

  const inp = 'w-full px-2.5 py-1.5 bg-white dark:bg-slate-900 border border-slate-300 dark:border-slate-600 rounded-lg text-sm text-slate-800 dark:text-slate-100 focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none'
  const btnDark = 'px-4 py-2 text-sm font-medium rounded-lg bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-50 inline-flex items-center gap-2'

  return (
    <div className="px-6 md:px-8 py-6">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Araçlar ve Ayarlar', href: '/araclar-ayarlar' }, { etiket: 'DNS Şablonu' }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Merkezi DNS Şablonu</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        Yeni bir domain eklendiğinde ve <span className="font-medium">"Varsayılan Şablonu Uygula"</span> butonuna basıldığında bu şablon uygulanır.
        Değişiklikleriniz anında geçerli olur.
      </p>

      {hata && <div className="mb-4 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-4 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      <div className="mb-4 px-3.5 py-2.5 bg-brand-50 dark:bg-brand-900/20 border border-brand-200 dark:border-brand-800 rounded-lg text-xs text-brand-800 dark:text-brand-200">
        <strong>Yer tutucular:</strong>{' '}
        <code className="font-mono">{'{DOMAIN}'}</code> alan adı ·{' '}
        <code className="font-mono">{'{IP}'}</code> sunucu IP ·{' '}
        <code className="font-mono">{'{SELECTOR}'}</code> DKIM seçici ·{' '}
        <code className="font-mono">{'{DKIM}'}</code> otomatik üretilen DKIM public key (TXT)
      </div>

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
      ) : (
        <>
          {/* Kayıt satırları */}
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden mb-5">
            <div className="lg:overflow-x-auto">
              <table className={T.tablo}>
                <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
                  <tr>
                    <th className={`${T.baslik} w-40`}>Ad (alt-ad)</th>
                    <th className={`${T.baslik} w-28`}>Tip</th>
                    <th className={T.baslik}>Değer</th>
                    <th className={`${T.baslik} w-24`}>TTL</th>
                    <th className={`${T.baslik} w-24`}>Öncelik</th>
                    <th className={`${T.baslik} text-center w-20`}>Aktif</th>
                    <th className={`${T.baslik} w-12`}></th>
                  </tr>
                </thead>
                <tbody className={T.govde}>
                  {rows.map((r, i) => (
                    <tr key={i} className={`${T.satir} lg:last:border-b-0 lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800/60`}>
                      {/* Birincil hücre: kaydın adı — mobilde kart başlığı olur, etiket istemez */}
                      <td className={T.hucreBaslik}><input value={r.ad} onChange={e => setRow(i, { ad: e.target.value })} className={inp + ' font-mono'} /></td>
                      <td className={T.hucre} data-etiket="Tip">
                        <span className="block flex-1 min-w-0">
                          <select value={r.tip} onChange={e => setRow(i, { tip: e.target.value })} className={inp + ' font-mono'}>
                            {TIPLER.map(t => <option key={t} value={t}>{t}</option>)}
                          </select>
                        </span>
                      </td>
                      <td className={T.hucre} data-etiket="Değer">
                        <span className="block flex-1 min-w-0"><input value={r.deger} onChange={e => setRow(i, { deger: e.target.value })} className={inp + ' font-mono'} /></span>
                      </td>
                      <td className={T.hucre} data-etiket="TTL">
                        <span className="block flex-1 min-w-0"><input type="number" min={60} value={r.ttl} onChange={e => setRow(i, { ttl: parseInt(e.target.value) || 3600 })} className={inp + ' font-mono'} /></span>
                      </td>
                      <td className={T.hucre} data-etiket="Öncelik">
                        {/* Tek flex öğesi: koşullu içerik ortak bir span'e sarıldı */}
                        <span className="block flex-1 min-w-0">
                          {(r.tip === 'MX' || r.tip === 'SRV')
                            ? <input type="number" min={0} value={r.oncelik} onChange={e => setRow(i, { oncelik: parseInt(e.target.value) || 0 })} className={inp + ' font-mono'} />
                            : <span className="text-slate-300 dark:text-slate-600 text-sm pl-2">—</span>}
                        </span>
                      </td>
                      <td className={`${T.hucre} lg:text-center`} data-etiket="Aktif">
                        <input type="checkbox" checked={r.aktif} onChange={e => setRow(i, { aktif: e.target.checked })} className="cursor-pointer w-4 h-4 accent-brand-600" />
                      </td>
                      <td className={`${T.hucreAksiyon} lg:text-center`}>
                        <button onClick={() => satirSil(i)} title="Satırı sil" className="text-red-500 hover:text-red-700 dark:hover:text-red-300 p-1 rounded hover:bg-red-50 dark:hover:bg-red-900/20">
                          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
                        </button>
                      </td>
                    </tr>
                  ))}
                  {/* Boş durum da tek/son satır olduğu için masaüstünde alt çizgi çizmemeli (eski divide-y davranışı) */}
                  {rows.length === 0 && (
                    <tr className={`${T.satir} lg:last:border-b-0`}><td colSpan={7} className={T.hucreDurum}>Şablonda kayıt yok — "Kayıt Ekle" ile başlayın.</td></tr>
                  )}
                </tbody>
              </table>
            </div>
            <div className="py-2.5 lg:px-3 lg:border-t lg:border-slate-100 dark:lg:border-slate-800">
              <button onClick={satirEkle} className="inline-flex items-center gap-1.5 text-sm px-3 py-1.5 bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 hover:bg-slate-50 dark:hover:bg-slate-800 rounded-lg text-slate-700 dark:text-slate-300">
                <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2.5}><path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" /></svg>
                Kayıt Ekle
              </button>
            </div>
          </div>

          {/* Meta: SOA + DKIM */}
          {meta && (
            <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-5">
              <h2 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-4">SOA & DKIM Parametreleri</h2>
              <div className="grid grid-cols-2 md:grid-cols-5 gap-3 mb-4">
                {(['soa_refresh', 'soa_retry', 'soa_expire', 'soa_minimum', 'soa_ttl'] as const).map(f => (
                  <label key={f} className="block">
                    <span className="block text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-1">{f.replace('soa_', '')} (sn)</span>
                    <input type="number" min={0} value={meta[f]} onChange={e => setMeta({ ...meta, [f]: parseInt(e.target.value) || 0 })} className={inp + ' font-mono'} />
                  </label>
                ))}
              </div>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-3 items-end">
                <label className="block">
                  <span className="block text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-1">DKIM Seçici</span>
                  <input value={meta.dkim_selector} onChange={e => setMeta({ ...meta, dkim_selector: e.target.value })} className={inp + ' font-mono'} placeholder="default" />
                </label>
                <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer pb-2">
                  <input type="checkbox" checked={meta.dkim_aktif} onChange={e => setMeta({ ...meta, dkim_aktif: e.target.checked })} className="w-4 h-4 accent-brand-600" />
                  DKIM anahtar üretimini etkinleştir
                </label>
              </div>
              <p className="text-[11px] text-slate-500 dark:text-slate-500 mt-3">
                DKIM açıkken şablonda <code className="font-mono">{'{DKIM}'}</code> içeren bir TXT kaydı olmalıdır (ör. ad: <code className="font-mono">{'{SELECTOR}._domainkey'}</code>).
                Her domain için 2048-bit RSA anahtar çifti otomatik üretilir; public key DNS'e yazılır, private key OpenDKIM kuruluysa mail sunucusuyla eşlenir.
              </p>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-3">
            <button onClick={kaydet} disabled={kaydediyor} className={btnDark}>
              {kaydediyor ? 'Kaydediliyor…' : 'Şablonu Kaydet'}
            </button>
            <button onClick={yukle} className="px-4 py-2 text-sm rounded-lg border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800">Geri Al</button>
          </div>
        </>
      )}
    </div>
  )
}
