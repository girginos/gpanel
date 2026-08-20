// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

type Surum = { surum: string; ini_dir: string; service: string }
type Ext = { adi: string; aktif: boolean; ini_dosya: string }

const ZORUNLU = new Set([
  'core', 'date', 'standard', 'pdo', 'mysqlnd', 'phar', 'spl', 'reflection',
  'session', 'pcre', 'tokenizer', 'json', 'hash', 'random', 'libxml',
])

export default function PHPModuleriPage({ gomulu }: { gomulu?: boolean } = {}) {
  const { onay, bilgi } = useDialog()
  const [surumler, setSurumler] = useState<Surum[]>([])
  const [aktifSurum, setAktifSurumState] = useState(() => {
    try { return localStorage.getItem('gosp.phpModul.surum') || '8.3' } catch { return '8.3' }
  })
  const setAktifSurum = (s: string) => {
    setAktifSurumState(s)
    try { localStorage.setItem('gosp.phpModul.surum', s) } catch {}
  }
  const [exts, setExts] = useState<Ext[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [filtre, setFiltre] = useState('')
  const [peclModal, setPeclModal] = useState(false)
  const [peclIlerleme, setPeclIlerleme] = useState<{ paket: string; adim: string; yuzde: number; yontem: string } | null>(null)

  function yukle() {
    setYuk(true); setHata(null)
    api.get(`/php-extensions?surum=${aktifSurum}`)
      .then(r => {
        setExts(r.data.icerik || [])
        const srm = r.data.surumler || []
        setSurumler(srm)
        // Kayitli surum artik kurulu degilse ilk kurulu surume dus
        if (srm.length > 0 && !srm.some((x: Surum) => x.surum === aktifSurum)) {
          setAktifSurum(srm[0].surum)
        }
      })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [aktifSurum])

  async function toggle(e: Ext) {
    if (ZORUNLU.has(e.adi.toLowerCase())) {
      (await bilgi({ baslik: 'Bilgi', mesaj: 'Bu modül PHP\'nin temel parçasıdır, kapatılamaz.' }))
      return
    }
    const yeniAktif = !e.aktif
    try {
      await api.put('/php-extensions/toggle', {
        surum: aktifSurum,
        ini_dosya: e.ini_dosya,
        aktif: yeniAktif,
      })
      setBasari(`✓ ${e.adi} ${yeniAktif ? 'aktif edildi' : 'devre dışı'} · PHP-FPM yeniden başlatıldı`)
      setTimeout(() => setBasari(null), 3000)
      yukle()
    } catch (err) {
      setHata(apiHata(err, 'Toggle başarısız'))
    }
  }

  async function ioncubeKur() {
    if (!(await onay({ baslik: 'Onay gerekiyor', mesaj: `IonCube Loader PHP ${aktifSurum} için kurulacak.\n\nioncube.com'dan tar.gz indirilir → .so kopyalanır → zend_extension olarak yüklenir.\nDevam?` }))) return
    setYuk(true); setHata(null)
    try {
      const r = await api.post('/php-extensions/ioncube-kur', { surum: aktifSurum })
      const d = r.data
      setBasari(`✓ IonCube kuruldu — ${d.yuklendi ? 'LOADED' : 'ini yazıldı ancak runtime\'da görünmedi'}`)
      setTimeout(() => setBasari(null), 5000)
      yukle()
    } catch (err) {
      setHata(apiHata(err, 'IonCube kurulum başarısız'))
      setYuk(false)
    }
  }

  async function ioncubeKaldir() {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: `IonCube Loader PHP ${aktifSurum}'ten kaldırılacak. Devam?`, tehlike: true }))) return
    setYuk(true); setHata(null)
    try {
      await api.post('/php-extensions/ioncube-kaldir', { surum: aktifSurum })
      setBasari('✓ IonCube kaldırıldı')
      setTimeout(() => setBasari(null), 3000)
      yukle()
    } catch (err) {
      setHata(apiHata(err, 'IonCube kaldırma başarısız'))
      setYuk(false)
    }
  }

  async function peclKur(paket: string) {
    if (!paket.match(/^[a-zA-Z0-9_-]+$/)) {
      (await bilgi({ baslik: 'Bilgi', mesaj: 'Geçersiz paket adı' })); return
    }
    if (!(await onay({ baslik: 'Onay gerekiyor', mesaj: `PECL paketi "${paket}" PHP ${aktifSurum} için kurulacak. Hazır paket yoksa PEAR + derleme araçları otomatik kurulup kaynaktan derlenir (birkaç dakika sürebilir). Devam?` }))) return
    setPeclModal(false); setHata(null); setBasari(null)
    setPeclIlerleme({ paket, adim: 'Başlatılıyor…', yuzde: 2, yontem: '' })
    try {
      const { data } = await api.post('/php-extensions/pecl-install', { surum: aktifSurum, paket })
      // Asenkron iş: is_id ile ilerlemeyi izle (backend derleme adımlarını raporlar).
      if (data.is_id) {
        for (;;) {
          await new Promise(r => setTimeout(r, 1500))
          const p = await api.get('/php-extensions/pecl-durum', { params: { id: data.is_id } })
          setPeclIlerleme({ paket, adim: p.data.adim || '…', yuzde: p.data.yuzde || 0, yontem: p.data.yontem || '' })
          if (p.data.durum === 'hata') throw new Error(p.data.hata || 'Kurulum başarısız')
          if (p.data.durum === 'tamam') break
        }
      }
      setBasari(`✓ ${paket} kuruldu`)
      yukle()
    } catch (err) {
      setHata(apiHata(err, 'PECL kurulum başarısız'))
    } finally {
      setPeclIlerleme(null)
    }
  }

  const filtreli = filtre ? exts.filter(e => e.adi.toLowerCase().includes(filtre.toLowerCase())) : exts
  const aktifSayi = exts.filter(e => e.aktif).length
  const pasifSayi = exts.length - aktifSayi

  return (
    <div className={gomulu ? '' : 'px-4 py-4 sm:px-6 sm:py-5'}>
      {!gomulu && (
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: 'Sistem Yönetimi' },
          { etiket: 'PHP Modülleri' },
        ]} />
      )}

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between mb-1">
        {!gomulu && <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">PHP Modülleri</h1>}
        <div className="flex gap-2 [&>button]:flex-1 sm:[&>button]:flex-none">
          <button onClick={() => {
              const ioncubeKurlu = exts.some(e => e.adi.toLowerCase().includes('ioncube'))
              if (ioncubeKurlu) ioncubeKaldir(); else ioncubeKur()
            }}
            className="px-3 py-2 sm:px-4 text-xs sm:text-sm whitespace-nowrap bg-amber-600 hover:bg-amber-700 text-white rounded-md">
            {exts.some(e => e.adi.toLowerCase().includes('ioncube')) ? '⊗ IonCube Kaldır' : <span className="inline-flex items-center gap-1.5"><Ikon d={I.kilit} />IonCube Yükle</span>}
          </button>
          <button onClick={() => setPeclModal(true)}
            className="px-3 py-2 sm:px-4 text-xs sm:text-sm whitespace-nowrap bg-slate-700 hover:bg-slate-800 text-white rounded-md">
            <span className="inline-flex items-center gap-1.5"><Ikon d={I.kutu} />PECL'den Kur</span>
          </button>
        </div>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        Sunucu genelinde PHP eklenti yönetimi. Toggle ile aç/kapat, FPM otomatik yeniden başlatılır. <strong>Sunucu bazında</strong> — tüm domain'leri etkiler.
      </p>

      {/* Sürüm sekmesi */}
      <div className="flex gap-2 mb-4 border-b border-slate-200 dark:border-slate-700 overflow-x-auto [&>*]:flex-shrink-0">
        {surumler.map(s => (
          <button key={s.surum} onClick={() => setAktifSurum(s.surum)}
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition ${
              aktifSurum === s.surum
                ? 'border-brand-500 text-brand-700 dark:text-brand-300'
                : 'border-transparent text-slate-500 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300'
            }`}>
            PHP {s.surum}
          </button>
        ))}
      </div>

      {/* Üst bar — sayaç + arama */}
      <div className="flex items-center justify-between mb-4 gap-3">
        <div className="flex items-center gap-3 text-sm">
          <span className="px-2.5 py-0.5 rounded-full bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 font-medium text-xs">
            {aktifSayi} aktif
          </span>
          <span className="px-2.5 py-0.5 rounded-full bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 dark:text-slate-500 font-medium text-xs">
            {pasifSayi} pasif
          </span>
          <span className="text-slate-400 dark:text-slate-500 text-xs">Toplam {exts.length}</span>
        </div>
        <input
          type="text"
          value={filtre}
          onChange={e => setFiltre(e.target.value)}
          placeholder="🔍 Modül ara..."
          className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm w-64 focus:border-brand-500 outline-none"
        />
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}
      {peclIlerleme && (
        <div className="mb-3 rounded-lg border border-brand-200 dark:border-brand-800 bg-brand-50 dark:bg-brand-950/40 px-4 py-3">
          <div className="flex items-center justify-between mb-1.5">
            <span className="text-sm font-medium text-brand-800 dark:text-brand-200">
              <span className="inline-block w-3.5 h-3.5 mr-2 align-[-2px] rounded-full border-2 border-brand-400 border-t-transparent animate-spin" />
              <span className="font-mono">{peclIlerleme.paket}</span> kuruluyor
              {peclIlerleme.yontem && <span className="ml-1.5 text-[10px] uppercase tracking-wide text-brand-500">({peclIlerleme.yontem === 'dnf' ? 'hazır paket' : 'derleme'})</span>}
            </span>
            <span className="text-xs tabular-nums text-brand-700 dark:text-brand-300">%{peclIlerleme.yuzde}</span>
          </div>
          <div className="text-xs text-brand-700 dark:text-brand-300 mb-2">{peclIlerleme.adim}</div>
          <div className="h-2 rounded-full bg-brand-100 dark:bg-brand-900 overflow-hidden">
            <div className="h-full rounded-full bg-brand-500 transition-all duration-500" style={{ width: `${Math.min(100, peclIlerleme.yuzde)}%` }} />
          </div>
        </div>
      )}

      {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div> : (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-2">
          {filtreli.map(e => {
            const zorunlu = ZORUNLU.has(e.adi.toLowerCase())
            return (
              <div key={e.ini_dosya}
                className={`flex items-center justify-between gap-2 px-3 py-2 rounded-md border ${
                  e.aktif
                    ? 'bg-emerald-50 dark:bg-emerald-900/20 border-emerald-200 dark:border-emerald-800'
                    : 'bg-slate-50 dark:bg-slate-900 border-slate-200 dark:border-slate-700'
                }`}>
                <div className="min-w-0 flex-1">
                  <div className="font-mono text-sm font-semibold text-slate-900 dark:text-slate-100 truncate">{e.adi}</div>
                  {zorunlu && <div className="text-[10px] text-slate-500 dark:text-slate-500">temel modül</div>}
                </div>
                <button
                  onClick={() => toggle(e)}
                  disabled={zorunlu}
                  className={`flex-shrink-0 relative inline-flex h-5 w-9 items-center rounded-full transition ${
                    e.aktif ? 'bg-emerald-500' : 'bg-slate-300'
                  } ${zorunlu ? 'opacity-40 cursor-not-allowed' : ''}`}
                  title={zorunlu ? 'Temel modül, kapatılamaz' : (e.aktif ? 'Devre dışı bırak' : 'Aktif et')}
                >
                  <span className={`inline-block h-3 w-3 transform rounded-full bg-white dark:bg-slate-800 shadow transition ${e.aktif ? 'translate-x-5' : 'translate-x-1'}`} />
                </button>
              </div>
            )
          })}
        </div>
      )}

      {peclModal && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => setPeclModal(false)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-2">PECL'den Modül Kur</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-3">Hazır paket varsa (bundled: <code className="font-mono">gmp, imap, bcmath</code> · PECL: <code className="font-mono">redis, mongodb, imagick</code>) doğrudan kurar; yoksa kaynaktan derler.</p>
            <p className="text-xs text-amber-700 dark:text-amber-300 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded p-2 mb-3">
              ⚠ PHP {aktifSurum} için derleme yapılır. Hedef: <code className="font-mono">/etc/php.d/</code> ya da Remi dizinine
            </p>
            <input id="peclPaketAdi" type="text" autoFocus placeholder="örn: mongodb"
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm mb-3"
              onKeyDown={e => {
                if (e.key === 'Enter') {
                  const v = (e.target as HTMLInputElement).value.trim()
                  if (v) peclKur(v)
                }
              }} />
            <div className="flex justify-end gap-2">
              <button onClick={() => setPeclModal(false)}
                className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
              <button onClick={() => {
                const v = (document.getElementById('peclPaketAdi') as HTMLInputElement)?.value?.trim()
                if (v) peclKur(v)
              }} className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm rounded">Kur</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}