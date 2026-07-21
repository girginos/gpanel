// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Modal from './Modal'

const PHP_FALLBACK = ['7.4', '8.1', '8.2', '8.3', '8.4']

type Plan = { id: number; ad: string; php_surum: string; varsayilan: boolean }
type Surum = { surum: string; aciklama?: string }

export default function AddDomainModal({
  acik, onKapat, onEklendi,
}: {
  acik: boolean
  onKapat: () => void
  onEklendi: () => void
}) {
  const [alanAdi, setAlanAdi] = useState('')
  const [phpSurum, setPhpSurum] = useState('8.3')
  const [planId, setPlanId] = useState<number | ''>('')
  const [planlar, setPlanlar] = useState<Plan[]>([])
  const [surumler, setSurumler] = useState<Surum[]>([])
  const [yukleniyor, setYukleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)

  // Modal açıldığında planları + kurulu PHP sürümlerini çek
  useEffect(() => {
    if (!acik) return
    api.get<Plan[]>('/plans').then(r => {
      const list = r.data || []
      setPlanlar(list)
      // Varsayılan plan varsa ön-seç + PHP'sini uygula
      const vars = list.find(p => p.varsayilan)
      if (vars) {
        setPlanId(vars.id)
        if (vars.php_surum) setPhpSurum(vars.php_surum)
      }
    }).catch(() => {})
    api.get<Surum[]>('/php/versions').then(r => setSurumler(r.data || [])).catch(() => {})
  }, [acik])

  function planDegis(v: string) {
    const idNum = v === '' ? '' : Number(v)
    setPlanId(idNum)
    if (idNum !== '') {
      const p = planlar.find(x => x.id === idNum)
      if (p?.php_surum) setPhpSurum(p.php_surum)
    }
  }

  const phpOpts = Array.from(new Set([
    ...(surumler.length ? surumler.map(s => s.surum) : PHP_FALLBACK),
    phpSurum,
  ].filter(Boolean)))

  const seciliPlan = planId === '' ? null : planlar.find(p => p.id === planId)
  const phpPlandan = !!seciliPlan && seciliPlan.php_surum === phpSurum

  async function gonder(e: React.FormEvent) {
    e.preventDefault()
    setHata(null); setBasari(null); setYukleniyor(true)
    try {
      const govde: Record<string, unknown> = {
        alan_adi: alanAdi.trim().toLowerCase(),
        php_surum: phpSurum,
      }
      if (planId !== '') govde.plan_id = planId
      const { data } = await api.post('/domains', govde)
      setBasari(`${data.alan_adi} başarıyla oluşturuldu (sistem kullanıcısı: ${data.sistem_kullanici})`)
      setTimeout(() => {
        setAlanAdi('')
        setBasari(null)
        onEklendi()
        onKapat()
      }, 1500)
    } catch (e) {
      setHata(apiHata(e, 'Domain eklenemedi'))
    } finally {
      setYukleniyor(false)
    }
  }

  return (
    <Modal acik={acik} baslik="Yeni Domain Ekle" onKapat={onKapat} genislik="md">
      <form onSubmit={gonder} className="space-y-4">
        <div>
          <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Alan Adı</label>
          <input
            type="text"
            value={alanAdi}
            onChange={(e) => setAlanAdi(e.target.value)}
            placeholder="example.com"
            autoFocus
            required
            className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none transition text-sm"
          />
          <p className="text-xs text-slate-500 dark:text-slate-500 mt-1">Örnek: <code className="font-mono">site.com</code>, <code className="font-mono">musteri-1.org</code></p>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">Plan (Paket)</label>
            <select
              value={planId}
              onChange={(e) => planDegis(e.target.value)}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none transition text-sm bg-white dark:bg-slate-800"
            >
              <option value="">Plan seçilmedi</option>
              {planlar.map(p => (
                <option key={p.id} value={p.id}>{p.ad}{p.varsayilan ? ' (varsayılan)' : ''}</option>
              ))}
            </select>
            <p className="text-xs text-slate-500 dark:text-slate-500 mt-1">Kaynak limitleri ve varsayılan PHP bu plandan gelir.</p>
          </div>
          <div>
            <label className="block text-sm font-medium text-slate-700 dark:text-slate-300 mb-1.5">PHP Sürümü</label>
            <select
              value={phpSurum}
              onChange={(e) => setPhpSurum(e.target.value)}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none transition text-sm bg-white dark:bg-slate-800"
            >
              {phpOpts.map(v => <option key={v} value={v}>PHP {v}</option>)}
            </select>
            <p className="text-xs text-slate-500 dark:text-slate-500 mt-1">
              {phpPlandan ? <span className="text-brand-600 dark:text-brand-400">✓ Plandan geldi ({seciliPlan?.ad})</span> : 'Plandan bağımsız değiştirebilirsiniz.'}
            </p>
          </div>
        </div>

        <div className="bg-sky-50 dark:bg-sky-900/20 border border-sky-200 rounded-md p-3 text-xs text-sky-800">
          <strong>Otomatik yapılacaklar:</strong> Linux kullanıcı (<code className="font-mono">c_&lt;slug&gt;</code>) + ev dizini (<code className="font-mono">/home/c_&lt;slug&gt;/public_html</code>) + nginx vhost + hoşgeldin sayfası
        </div>

        {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
        {basari && <div className="px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onKapat}
            disabled={yukleniyor}
            className="px-4 py-2 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 rounded-md text-sm transition"
          >
            İptal
          </button>
          <button
            type="submit"
            disabled={yukleniyor || !alanAdi.trim()}
            className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 rounded-md text-sm font-medium transition"
          >
            {yukleniyor ? 'Sağlanıyor…' : 'Domain Ekle'}
          </button>
        </div>
      </form>
    </Modal>
  )
}
