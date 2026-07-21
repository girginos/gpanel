// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import ListToolbar from '@/components/ListToolbar'
import EmptyState from '@/components/EmptyState'
import Modal from '@/components/Modal'
import ConfirmDialog from '@/components/ConfirmDialog'

type Plan = {
  id: number
  ad: string
  aciklama: string
  disk_kota_mb: number
  trafik_kota_mb: number
  max_domain: number
  max_db: number
  max_email: number
  max_ftp: number
  php_surum: string
  fastcgi_cache: boolean
  client_max_body_mb: number
  nginx_ek_direktifler: string
  varsayilan: boolean
  olusturulma: string
}
type Surum = { surum: string; aciklama?: string }

export default function ServicePlansPage() {
  const [items, setItems] = useState<Plan[]>([])
  const [surumler, setSurumler] = useState<Surum[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [modal, setModal] = useState<Plan | null>(null)
  const [silinecek, setSilinecek] = useState<Plan | null>(null)

  function yukle() {
    setYuk(true); setHata(null)
    api.get<Plan[]>('/plans')
      .then(r => setItems(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [])
  useEffect(() => {
    api.get<Surum[]>('/php/versions').then(r => setSurumler(r.data || [])).catch(() => {})
  }, [])

  async function sil() {
    if (!silinecek) return
    try {
      await api.delete(`/plans/${silinecek.id}`)
      setSilinecek(null); yukle()
    } catch (e) {
      alert(apiHata(e, 'Silme başarısız'))
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Hizmet Planları' }]} />
      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-2">Hizmet Planları</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">
        Domainler için hizmet planları (paketler) tanımlayın. Her domain bir plana bağlanır;
        disk, trafik, PHP sürümü, veritabanı ve alt domain limiti gibi kaynaklar paket başına ayarlanır.
      </p>

      <ListToolbar
        birincil={{ etiket: 'Plan Ekle', onClick: () => setModal({} as Plan) }}
        butonlar={[]}
      />

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
      ) : items.length === 0 ? (
        <EmptyState
          baslik="Henüz hizmet planı yok"
          aciklama="İlk paketinizi tanımlayarak başlayın."
          buton={{ etiket: 'Plan Ekle', onClick: () => setModal({} as Plan) }}
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {items.map(p => (
            <div key={p.id} className={`bg-white dark:bg-slate-800 border rounded-2xl p-5 shadow-sm ${p.varsayilan ? 'border-brand-400 ring-2 ring-brand-100 dark:ring-brand-900/40' : 'border-slate-200 dark:border-slate-700'}`}>
              <div className="flex items-start justify-between mb-2">
                <div className="min-w-0">
                  <h3 className="text-lg font-semibold text-slate-900 dark:text-slate-100 flex items-center gap-2">
                    {p.ad}
                    {p.varsayilan && <span className="text-[10px] uppercase tracking-wider bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 px-1.5 py-0.5 rounded font-semibold">Varsayılan</span>}
                  </h3>
                  {p.aciklama && <p className="text-sm text-slate-500 dark:text-slate-500 mt-0.5">{p.aciklama}</p>}
                </div>
                {p.php_surum && <span className="shrink-0 text-[11px] font-mono font-semibold bg-slate-100 dark:bg-slate-700/60 text-slate-600 dark:text-slate-300 px-2 py-0.5 rounded">PHP {p.php_surum}</span>}
              </div>

              <dl className="grid grid-cols-1 sm:grid-cols-2 gap-y-1.5 text-sm mt-4">
                <Sat e="Disk" d={fmt(p.disk_kota_mb, 'MB')} />
                <Sat e="Trafik" d={fmt(p.trafik_kota_mb, 'MB/ay')} />
                <Sat e="Domain" d={fmt(p.max_domain, 'adet')} />
                <Sat e="Veritabanı" d={fmt(p.max_db, 'adet')} />
                <Sat e="FTP" d={fmt(p.max_ftp, 'hesap')} />
              </dl>

              <div className="mt-4 flex gap-2">
                <Link to={`/araclar/paketler/${p.id}`} className="flex-1 text-center text-sm px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-md">
                  Detay & Kaynak Limitleri
                </Link>
                <button onClick={() => setSilinecek(p)} className="text-sm px-3 py-1.5 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 rounded-md">Sil</button>
              </div>
            </div>
          ))}
        </div>
      )}

      {modal && (
        <PlanModal
          plan={modal}
          surumler={surumler}
          onKapat={() => setModal(null)}
          onKayit={() => { setModal(null); yukle() }}
        />
      )}

      <ConfirmDialog
        acik={!!silinecek}
        baslik="Planı sil"
        mesaj={`"${silinecek?.ad}" planı silinsin mi?`}
        tehlikeli
        onayMetni="Evet, sil"
        onOnay={sil}
        onIptal={() => setSilinecek(null)}
      />
    </div>
  )
}

function Sat({ e, d }: { e: string; d: string }) {
  return (
    <>
      <dt className="text-slate-500 dark:text-slate-500">{e}</dt>
      <dd className="text-slate-800 dark:text-slate-200 text-right font-mono">{d}</dd>
    </>
  )
}

function fmt(n: number, birim: string) {
  if (n <= 0) return 'sınırsız'
  if (birim.startsWith('MB') && n >= 1024) return `${(n / 1024).toFixed(1)} G${birim.slice(2)}`
  return `${n.toLocaleString('tr-TR')} ${birim}`
}

function PlanModal({ plan, surumler, onKapat, onKayit }: { plan: Plan; surumler: Surum[]; onKapat: () => void; onKayit: () => void }) {
  const yeni = !plan.id
  const [form, setForm] = useState<Plan>({
    id: plan.id || 0,
    ad: plan.ad || '',
    aciklama: plan.aciklama || '',
    disk_kota_mb: plan.disk_kota_mb || 1024,
    trafik_kota_mb: plan.trafik_kota_mb || 10240,
    max_domain: plan.max_domain || 1,
    max_db: plan.max_db || 1,
    max_email: plan.max_email || 0,
    max_ftp: plan.max_ftp || 2,
    php_surum: plan.php_surum || '8.3',
    fastcgi_cache: plan.fastcgi_cache || false,
    client_max_body_mb: plan.client_max_body_mb || 64,
    nginx_ek_direktifler: plan.nginx_ek_direktifler || '',
    varsayilan: plan.varsayilan || false,
    olusturulma: '',
  })
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)

  const phpOpts = Array.from(new Set([
    ...surumler.map(s => s.surum),
    form.php_surum,
    ...(surumler.length === 0 ? ['7.4', '8.1', '8.2', '8.3', '8.4'] : []),
  ].filter(Boolean)))

  async function gonder(e: React.FormEvent) {
    e.preventDefault()
    setIsleniyor(true); setHata(null)
    try {
      if (yeni) await api.post('/plans', form)
      else await api.put(`/plans/${form.id}`, form)
      onKayit()
    } catch (e) {
      setHata(apiHata(e, 'Kayıt başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={true} baslik={yeni ? 'Yeni Plan' : 'Planı Düzenle'} onKapat={onKapat} genislik="lg">
      <form onSubmit={gonder} className="space-y-4">
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <Alan etiket="Plan adı" value={form.ad} setVal={v => setForm({ ...form, ad: v })} required />
          <Alan etiket="Açıklama" value={form.aciklama} setVal={v => setForm({ ...form, aciklama: v })} />
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          <Sayi etiket="Disk (MB)" value={form.disk_kota_mb} setVal={v => setForm({ ...form, disk_kota_mb: v })} />
          <Sayi etiket="Trafik (MB)" value={form.trafik_kota_mb} setVal={v => setForm({ ...form, trafik_kota_mb: v })} />
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 mb-1">PHP Sürümü</label>
            <select value={form.php_surum} onChange={e => setForm({ ...form, php_surum: e.target.value })}
              className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-800 rounded text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none">
              {phpOpts.map(v => <option key={v} value={v}>PHP {v}</option>)}
            </select>
          </div>
          <Sayi etiket="Max domain" value={form.max_domain} setVal={v => setForm({ ...form, max_domain: v })} />
          <Sayi etiket="Max DB" value={form.max_db} setVal={v => setForm({ ...form, max_db: v })} />
          <Sayi etiket="Max FTP" value={form.max_ftp} setVal={v => setForm({ ...form, max_ftp: v })} />
        </div>
        <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
          <input type="checkbox" checked={form.varsayilan} onChange={e => setForm({ ...form, varsayilan: e.target.checked })} className="rounded" />
          Yeni domainlerde varsayılan plan
        </label>
        <p className="text-xs text-slate-500 dark:text-slate-500">0 = sınırsız. Disk/trafik MB cinsindendir. Bu plandaki yeni domainler seçili PHP sürümüyle kurulur.</p>

        {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-sm text-red-700 dark:text-red-300">{hata}</div>}

        <div className="flex justify-end gap-2 pt-2">
          <button type="button" onClick={onKapat} className="px-4 py-2 border border-slate-200 dark:border-slate-700 rounded-md text-sm">İptal</button>
          <button type="submit" disabled={isleniyor || !form.ad.trim()} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm rounded-md">{isleniyor ? 'Kaydediliyor…' : (yeni ? 'Ekle' : 'Güncelle')}</button>
        </div>
      </form>
    </Modal>
  )
}

function Alan({ etiket, value, setVal, required }: { etiket: string; value: string; setVal: (v: string) => void; required?: boolean }) {
  return (
    <div>
      <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{etiket}</label>
      <input type="text" value={value} onChange={e => setVal(e.target.value)} required={required}
        className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
    </div>
  )
}
function Sayi({ etiket, value, setVal }: { etiket: string; value: number; setVal: (v: number) => void }) {
  return (
    <div>
      <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">{etiket}</label>
      <input type="number" min={0} value={value} onChange={e => setVal(parseInt(e.target.value) || 0)}
        className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
    </div>
  )
}
