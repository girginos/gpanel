// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import { T } from '@/lib/tablo'

type Gorev = {
  idx: number
  dakika: string
  saat: string
  gun: string
  ay: string
  hafta: string
  komut: string
  yorum?: string
}

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }

type ListResp = { sistem_kullanici: string; toplam: number; gorevler: Gorev[] }

const ON_AYARLAR: Array<{ etiket: string; secim: { dakika: string; saat: string; gun: string; ay: string; hafta: string } }> = [
  { etiket: 'Her dakika',        secim: { dakika: '*',  saat: '*', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Her saat',          secim: { dakika: '0',  saat: '*', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Her gün gece 03:00', secim: { dakika: '0',  saat: '3', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Pazartesi 09:00',   secim: { dakika: '0',  saat: '9', gun: '*', ay: '*', hafta: '1' } },
  { etiket: 'Her 5 dakika',      secim: { dakika: '*/5', saat: '*', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Her 15 dakika',     secim: { dakika: '*/15', saat: '*', gun: '*', ay: '*', hafta: '*' } },
  { etiket: 'Ayın 1\'i 00:00',   secim: { dakika: '0',  saat: '0', gun: '1', ay: '*', hafta: '*' } },
]

export default function DomainCronPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [gorevler, setGorevler] = useState<Gorev[]>([])
  const [yukleniyor, setYukleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [modal, setModal] = useState(false)

  function yukle() {
    if (!id) return
    setYukleniyor(true); setHata(null)
    api.get<ListResp>(`/domains/${id}/cron`)
      .then(r => setGorevler(r.data.gorevler))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYukleniyor(false))
  }

  useEffect(() => {
    if (id) api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(() => {})
    yukle()
  }, [id])

  async function sil(g: Gorev) {
    if (!confirm(`"${g.komut.slice(0, 60)}..." görevi silinsin mi?`)) return
    try {
      await api.delete(`/domains/${id}/cron/${g.idx}`)
      yukle()
    } catch (e) {
      alert(apiHata(e, 'Silme başarısız'))
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1300px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'Zamanlanmış Görevler' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Zamanlanmış Görevler</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}
          <span className="font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">/var/spool/cron/{domain.sistem_kullanici}</span>
        </p>
      )}

      <div className="flex flex-wrap items-center gap-2 mb-4">
        <button
          onClick={() => setModal(true)}
          className="inline-flex items-center gap-1.5 px-3.5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-md shadow-sm transition"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2.5}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
          </svg>
          Görev Ekle
        </button>
        <button onClick={yukle} className="px-3 py-2 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 text-sm rounded-md transition">↻ Yenile</button>
        <span className="ml-auto text-sm text-slate-500 dark:text-slate-500">{gorevler.length} görev</span>
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {/* Çerçeve yalnız masaüstünde: mobilde satırlar zaten kart olduğundan
          ikinci bir çerçeve iç içe görünüm yaratırdı. */}
      <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
        {yukleniyor ? (
          <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
        ) : gorevler.length === 0 ? (
          <div className="py-16 text-center">
            <div className="w-14 h-14 mx-auto rounded-full bg-slate-100 dark:bg-slate-800 flex items-center justify-center mb-3">
              <svg className="w-7 h-7 text-slate-400 dark:text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.5}>
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
            <p className="text-sm text-slate-500 dark:text-slate-500">Henüz görev yok. Yukarıdan ekleyin.</p>
          </div>
        ) : (
          // Yatay kaydırma yalnız masaüstünde; mobilde kart dizilimi gereksiz kılıyor.
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
            <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
              <tr>
                <th className={T.baslik}>Dak</th>
                <th className={T.baslik}>Saat</th>
                <th className={T.baslik}>Gün</th>
                <th className={T.baslik}>Ay</th>
                <th className={T.baslik}>Hafta</th>
                <th className={T.baslik}>Komut / Açıklama</th>
                <th className={`${T.baslik} text-right`}>İşlem</th>
              </tr>
            </thead>
            <tbody className={T.govde}>
              {gorevler.map((g) => (
                <tr key={g.idx} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800 transition`}>
                  <td className={T.hucre} data-etiket="Dak">
                    <span className="font-mono">{g.dakika}</span>
                  </td>
                  <td className={T.hucre} data-etiket="Saat">
                    <span className="font-mono">{g.saat}</span>
                  </td>
                  <td className={T.hucre} data-etiket="Gün">
                    <span className="font-mono">{g.gun}</span>
                  </td>
                  <td className={T.hucre} data-etiket="Ay">
                    <span className="font-mono">{g.ay}</span>
                  </td>
                  <td className={T.hucre} data-etiket="Hafta">
                    <span className="font-mono">{g.hafta}</span>
                  </td>
                  {/* Birincil tanımlayıcı: görevi ayırt eden şey komutun kendisidir. */}
                  <td className={T.hucreBaslik}>
                    <div className="font-mono text-slate-800 dark:text-slate-200 break-all lg:break-normal lg:truncate lg:max-w-md" title={g.komut}>{g.komut}</div>
                    {g.yorum && <div className="text-xs font-normal text-slate-500 dark:text-slate-500 mt-0.5">{g.yorum}</div>}
                  </td>
                  <td className={`${T.hucreAksiyon} lg:text-right`}>
                    <button onClick={() => sil(g)} className="text-sm text-red-600 dark:text-red-400 hover:text-red-700 dark:text-red-300 px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 transition">Sil</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          </div>
        )}
      </div>

      <CronEkleModal acik={modal} onKapat={() => setModal(false)} onEklendi={yukle} domainId={Number(id)} />
    </div>
  )
}

function CronEkleModal({ acik, onKapat, onEklendi, domainId }: {
  acik: boolean; onKapat: () => void; onEklendi: () => void; domainId: number
}) {
  const [dakika, setDakika] = useState('0')
  const [saat, setSaat] = useState('3')
  const [gun, setGun] = useState('*')
  const [ay, setAy] = useState('*')
  const [hafta, setHafta] = useState('*')
  const [komut, setKomut] = useState('')
  const [yorum, setYorum] = useState('')
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)

  function uygula(p: typeof ON_AYARLAR[number]['secim']) {
    setDakika(p.dakika); setSaat(p.saat); setGun(p.gun); setAy(p.ay); setHafta(p.hafta)
  }

  async function gonder(e: React.FormEvent) {
    e.preventDefault()
    setIsleniyor(true); setHata(null)
    try {
      await api.post(`/domains/${domainId}/cron`, { dakika, saat, gun, ay, hafta, komut: komut.trim(), yorum: yorum.trim() })
      onEklendi()
      setKomut(''); setYorum('')
      onKapat()
    } catch (e) {
      setHata(apiHata(e, 'Ekleme başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={acik} baslik="Yeni Zamanlanmış Görev" onKapat={onKapat} genislik="lg">
      <form onSubmit={gonder} className="space-y-4">
        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Hazır Şablonlar</label>
          <div className="flex flex-wrap gap-1.5">
            {ON_AYARLAR.map(p => (
              <button
                key={p.etiket}
                type="button"
                onClick={() => uygula(p.secim)}
                className="px-2 py-1 text-xs bg-slate-100 dark:bg-slate-800 hover:bg-brand-100 dark:bg-brand-900/30 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 rounded transition"
              >
                {p.etiket}
              </button>
            ))}
          </div>
        </div>

        <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-2">
          <Alan etiket="Dakika"   value={dakika} onChange={setDakika} />
          <Alan etiket="Saat"     value={saat}   onChange={setSaat} />
          <Alan etiket="Gün"      value={gun}    onChange={setGun} />
          <Alan etiket="Ay"       value={ay}     onChange={setAy} />
          <Alan etiket="Hafta"    value={hafta}  onChange={setHafta} />
        </div>
        <p className="text-xs text-slate-500 dark:text-slate-500">Cron biçimi — <code className="font-mono">*</code> her zaman, <code className="font-mono">*/5</code> her 5'te bir, <code className="font-mono">0,15,30</code> liste, <code className="font-mono">9-17</code> aralık.</p>

        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Komut</label>
          <input
            type="text"
            value={komut}
            onChange={e => setKomut(e.target.value)}
            placeholder="/usr/bin/php /home/c_user/public_html/cron.php"
            required
            className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none text-sm font-mono"
          />
        </div>

        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Açıklama (opsiyonel)</label>
          <input
            type="text"
            value={yorum}
            onChange={e => setYorum(e.target.value)}
            placeholder="örn. Her gece yedek scripti"
            className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none text-sm"
          />
        </div>

        {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

        <div className="flex justify-end gap-2 pt-2">
          <button type="button" onClick={onKapat} disabled={isleniyor} className="px-4 py-2 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 rounded-md text-sm">İptal</button>
          <button type="submit" disabled={isleniyor || !komut.trim()} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">
            {isleniyor ? 'Ekleniyor…' : 'Ekle'}
          </button>
        </div>
      </form>
    </Modal>
  )
}

function Alan({ etiket, value, onChange }: { etiket: string; value: string; onChange: (v: string) => void }) {
  return (
    <div>
      <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{etiket}</label>
      <input
        type="text"
        value={value}
        onChange={e => onChange(e.target.value)}
        className="w-full px-2 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
      />
    </div>
  )
}