// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import { hataYakala } from '@/lib/hata'
import Breadcrumb from '@/components/Breadcrumb'
import ConfirmDialog from '@/components/ConfirmDialog'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }
type Yedek = { id: number; domain_id: number; tip: string; dosya: string; boyut_b: number; notlar: string; olusturma: string }
type Schedule = { freq: 'none' | 'daily' | 'weekly'; hour: number; retention: number; last_backup_at?: string }
type Destination = {
  yok?: boolean
  id?: number; tip?: 'ftp' | 'sftp'; host?: string; port?: number
  kullanici?: string; uzak_dizin?: string; aktif?: boolean
  son_yukleme?: string; son_durum?: string; son_hata?: string
}

export default function DomainBackupsPage() {
  const { onay, bilgi } = useDialog()
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [yedekler, setYedekler] = useState<Yedek[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [silinecek, setSilinecek] = useState<Yedek | null>(null)
  const [geriYukle, setGeriYukle] = useState<Yedek | null>(null)

  const [sched, setSched] = useState<Schedule>({ freq: 'none', hour: 3, retention: 7 })
  const [schedKayit, setSchedKayit] = useState(false)

  const [dest, setDest] = useState<Destination>({ yok: true })
  const [destForm, setDestForm] = useState({ tip: 'sftp' as 'ftp'|'sftp', host: '', port: 22, kullanici: '', parola: '', uzak_dizin: '/', aktif: true })
  const [destKayit, setDestKayit] = useState(false)
  const [destTest, setDestTest] = useState<{ ok: boolean; hata?: string } | null>(null)

  function yukle() {
    if (!id) return
    setYuk(true)
    Promise.all([
      api.get<Yedek[]>(`/domains/${id}/backups`),
      api.get<Schedule>(`/domains/${id}/backup-schedule`).catch(() => ({ data: { freq: 'none', hour: 3, retention: 7 } as Schedule })),
      api.get<Destination>(`/domains/${id}/backup-destination`).catch(() => ({ data: { yok: true } as Destination })),
    ]).then(([y, s, d]) => {
      setYedekler(y.data)
      setSched(s.data)
      setDest(d.data)
      if (!d.data.yok) {
        setDestForm({
          tip: (d.data.tip || 'sftp') as 'ftp'|'sftp',
          host: d.data.host || '',
          port: d.data.port || (d.data.tip === 'ftp' ? 21 : 22),
          kullanici: d.data.kullanici || '',
          parola: '',  // güvenlik: boş bırak, kullanıcı isterse yeniden girer
          uzak_dizin: d.data.uzak_dizin || '/',
          aktif: !!d.data.aktif,
        })
      }
    })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }

  async function destKaydet() {
    setDestKayit(true); setHata(null); setBasari(null); setDestTest(null)
    try {
      const r = await api.put<Destination>(`/domains/${id}/backup-destination`, destForm)
      setDest(r.data)
      setBasari('Uzak hedef kaydedildi')
      setTimeout(() => setBasari(null), 4000)
    } catch (e) {
      setHata(apiHata(e, 'Hedef kaydedilemedi'))
    } finally {
      setDestKayit(false)
    }
  }

  async function destBaglantiTesti() {
    setDestKayit(true); setDestTest(null)
    try {
      const r = await api.post<{ ok: boolean; hata?: string }>(`/domains/${id}/backup-destination/test`, destForm)
      setDestTest(r.data)
      setTimeout(() => setDestTest(null), 8000)
    } catch (e) {
      setDestTest({ ok: false, hata: apiHata(e) })
    } finally {
      setDestKayit(false)
    }
  }

  async function destSil() {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: 'Uzak yedek hedefi silinsin mi? Mevcut yedekler etkilenmez, sadece bundan sonraki otomatik gönderim durur.', tehlike: true }))) return
    setDestKayit(true)
    try {
      await api.delete(`/domains/${id}/backup-destination`)
      setDest({ yok: true })
      setDestForm({ tip: 'sftp', host: '', port: 22, kullanici: '', parola: '', uzak_dizin: '/', aktif: true })
      setBasari('Uzak hedef silindi')
      setTimeout(() => setBasari(null), 4000)
    } catch (e) {
      setHata(apiHata(e))
    } finally {
      setDestKayit(false)
    }
  }
  useEffect(() => {
    if (id) api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(hataYakala('Alan adı bilgisi alınamadı'))
    yukle()
  }, [id])

  async function scheduleKaydet(yeni: Schedule) {
    setSchedKayit(true); setHata(null); setBasari(null)
    try {
      const r = await api.put<{ schedule: Schedule }>(`/domains/${id}/backup-schedule`, yeni)
      setSched(r.data.schedule)
      setBasari(yeni.freq === 'none'
        ? 'Otomatik yedek kapatıldı'
        : `Otomatik yedek aktif: ${yeni.freq === 'daily' ? 'Günlük' : 'Haftalık'}, ${String(yeni.hour).padStart(2,'0')}:00, son ${yeni.retention} yedek tutulur`)
      setTimeout(() => setBasari(null), 5000)
    } catch (e) {
      setHata(apiHata(e, 'Plan kaydedilemedi'))
    } finally {
      setSchedKayit(false)
    }
  }

  async function olustur() {
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      await api.post(`/domains/${id}/backups`)
      setBasari('Yedek oluşturuldu')
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Yedek oluşturulamadı'))
    } finally {
      setIsleniyor(false)
    }
  }

  async function sil() {
    if (!silinecek) return
    try {
      await api.delete(`/domains/${id}/backups/${silinecek.id}`)
      setSilinecek(null); yukle()
    } catch (e) {
      (await bilgi({ baslik: 'Bilgi', mesaj: apiHata(e) }))
    }
  }

  function indir(y: Yedek) {
    const tok = localStorage.getItem('gosp.token') || ''
    fetch(`/api/v1/domains/${id}/backups/${y.id}/indir`, { headers: { Authorization: `Bearer ${tok}` } })
      .then(r => r.blob())
      .then(blob => {
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = y.dosya
        a.click()
      })
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'Yedekler' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Yedekler</h1>
      {domain && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
        {' · '}home + MySQL dump = tar.gz · {sched.freq === 'none'
          ? 'Otomatik yedek kapalı'
          : `${sched.freq === 'daily' ? 'Günlük' : 'Haftalık'} ${String(sched.hour).padStart(2,'0')}:00 · son ${sched.retention} oto-yedek korunur`}
      </p>}

      {/* Otomatik Yedek Planı */}
      <div className="mb-5 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Otomatik Yedekleme Planı</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
              Plan aktifken panel her saat tarama yapar; gelen saat slot'unda yedek üretilip retention dışına çıkanlar silinir.
            </p>
          </div>
          {sched.last_backup_at && (
            <div className="text-xs text-slate-500 dark:text-slate-500">Son oto-yedek: <span className="font-mono">{sched.last_backup_at.replace('T',' ').replace('Z','')}</span></div>
          )}
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {(['none','daily','weekly'] as const).map(f => {
            const aktif = sched.freq === f
            const meta: Record<string,{ad:string;ikon:string;aciklama:string;renk:string}> = {
              none: { ad:'Kapalı', ikon:'⏸', aciklama:'Otomatik yedek yok. Yalnız manuel "Şimdi Yedekle".', renk:'slate' },
              daily: { ad:'Günlük', ikon:'🌙', aciklama:'Her gün seçilen saatte yedek üretilir, son N tutulur.', renk:'emerald' },
              weekly: { ad:'Haftalık', ikon:'📅', aciklama:'Her 7 günde bir yedek, daha ekonomik disk kullanımı.', renk:'indigo' },
            }
            const m = meta[f]
            const renk: Record<string,string> = {
              slate:   aktif ? 'border-slate-500 bg-slate-100 dark:bg-slate-800 ring-2 ring-slate-400/20'      : 'border-slate-200 dark:border-slate-700 hover:border-slate-400 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800',
              emerald: aktif ? 'border-emerald-500 bg-emerald-50 dark:bg-emerald-900/20 ring-2 ring-emerald-500/20': 'border-slate-200 dark:border-slate-700 hover:border-emerald-300 hover:bg-emerald-50 dark:hover:bg-emerald-900/30 dark:bg-emerald-900/20',
              indigo:  aktif ? 'border-indigo-500 bg-indigo-50 dark:bg-indigo-900/20 ring-2 ring-indigo-500/20'   : 'border-slate-200 dark:border-slate-700 hover:border-indigo-300 hover:bg-indigo-50 dark:bg-indigo-900/20',
            }
            return (
              <button key={f} type="button" disabled={schedKayit || aktif}
                onClick={() => scheduleKaydet({ ...sched, freq: f })}
                className={`text-left p-3 border rounded-lg transition disabled:cursor-default ${renk[m.renk]}`}>
                <div className="flex items-center justify-between mb-1">
                  <span className="text-base leading-none">{m.ikon}</span>
                  {aktif && <span className="text-[10px] uppercase tracking-wider font-semibold text-emerald-700 dark:text-emerald-300">● Aktif</span>}
                </div>
                <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{m.ad}</div>
                <div className="text-[11px] text-slate-600 dark:text-slate-400 dark:text-slate-500 mt-1 leading-snug">{m.aciklama}</div>
              </button>
            )
          })}
        </div>

        {sched.freq !== 'none' && (
          <div className="mt-4 grid grid-cols-1 sm:grid-cols-2 gap-3">
            <label className="block">
              <span className="text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500">Çalışma saati (yerel)</span>
              <select
                value={sched.hour}
                onChange={e => scheduleKaydet({ ...sched, hour: Number(e.target.value) })}
                disabled={schedKayit}
                className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm bg-white dark:bg-slate-800">
                {Array.from({length:24},(_,i)=>i).map(h =>
                  <option key={h} value={h}>{String(h).padStart(2,'0')}:00</option>
                )}
              </select>
            </label>
            <label className="block">
              <span className="text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500">Tutulacak yedek sayısı (retention)</span>
              <input type="number" min={1} max={90} value={sched.retention}
                onChange={e => setSched(s => ({...s, retention: Math.max(1, Math.min(90, Number(e.target.value)||1))}))}
                onBlur={() => scheduleKaydet(sched)}
                disabled={schedKayit}
                className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono"/>
              <span className="text-[10px] text-slate-500 dark:text-slate-500 mt-0.5 block">Bu sayıyı aşan eski oto-yedekler silinir. Manuel yedekler korunur.</span>
            </label>
          </div>
        )}
      </div>

      {/* Uzak Yedek Hedefi (FTP/SFTP) */}
      <div className="mb-5 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5">
        <div className="flex items-center justify-between mb-3">
          <div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Uzak Yedek Hedefi (FTP / SFTP)</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
              Yedek üretildikten sonra arkaplanda uzak sunucuya yüklenir — disk arızasına karşı off-site koruma.
            </p>
          </div>
          {!dest.yok && dest.son_durum && (
            <span className={`text-[10px] uppercase tracking-wider font-semibold px-2 py-1 rounded ${
              dest.son_durum === 'basarili' ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' :
              dest.son_durum === 'hata' ? 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300' :
              'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 dark:text-slate-500'
            }`}>{dest.son_durum === 'basarili' ? '● Son: başarılı' : dest.son_durum === 'hata' ? '✗ Son: hata' : dest.son_durum}</span>
          )}
        </div>

        {!dest.yok && dest.son_yukleme && (
          <div className="mb-3 text-xs text-slate-500 dark:text-slate-500">
            Son yükleme: <span className="font-mono">{dest.son_yukleme}</span>
            {dest.son_durum === 'hata' && dest.son_hata && (
              <div className="mt-1 text-[11px] text-red-700 dark:text-red-300 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-2 font-mono whitespace-pre-wrap">{dest.son_hata}</div>
            )}
          </div>
        )}

        <div className="grid grid-cols-1 sm:grid-cols-6 gap-3 mb-3">
          <div className="sm:col-span-2">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Protokol</label>
            <div className="flex gap-2">
              {(['sftp','ftp'] as const).map(t => {
                const aktif = destForm.tip === t
                return (
                  <button key={t} type="button"
                    onClick={() => setDestForm(f => ({...f, tip: t, port: t === 'sftp' ? 22 : 21}))}
                    className={`flex-1 text-xs px-3 py-2 rounded border ${aktif ? 'border-brand-500 bg-brand-50 dark:bg-brand-900/20 text-brand-700 dark:text-brand-300 font-semibold' : 'border-slate-200 dark:border-slate-700 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800'}`}>
                    {t === 'sftp' ? '🔒 SFTP (port 22)' : '📡 FTP (port 21)'}
                  </button>
                )
              })}
            </div>
          </div>
          <div className="sm:col-span-3">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Host</label>
            <input type="text" value={destForm.host} placeholder="backup.firma.com"
              onChange={e => setDestForm(f => ({...f, host: e.target.value}))}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono"/>
          </div>
          <div className="sm:col-span-1">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Port</label>
            <input type="number" value={destForm.port}
              onChange={e => setDestForm(f => ({...f, port: Number(e.target.value)||0}))}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono"/>
          </div>
          <div className="sm:col-span-2">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Kullanıcı</label>
            <input type="text" value={destForm.kullanici} autoComplete="off"
              onChange={e => setDestForm(f => ({...f, kullanici: e.target.value}))}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono"/>
          </div>
          <div className="sm:col-span-2">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Parola {!dest.yok && <span className="text-[10px] text-slate-400 dark:text-slate-500">(boş bırakırsanız mevcut korunur)</span>}</label>
            <input type="password" value={destForm.parola} autoComplete="new-password"
              onChange={e => setDestForm(f => ({...f, parola: e.target.value}))}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono"/>
          </div>
          <div className="sm:col-span-2">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Uzak dizin</label>
            <input type="text" value={destForm.uzak_dizin}
              onChange={e => setDestForm(f => ({...f, uzak_dizin: e.target.value}))}
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono"/>
          </div>
        </div>

        <div className="flex items-center justify-between flex-wrap gap-3">
          <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
            <input type="checkbox" checked={destForm.aktif}
              onChange={e => setDestForm(f => ({...f, aktif: e.target.checked}))}
              className="cursor-pointer"/>
            Aktif (her yedek bu hedefe gönderilsin)
          </label>
          <div className="flex flex-wrap items-center gap-2">
            {destTest && (
              <span className={`text-xs px-2 py-1 rounded font-medium ${destTest.ok ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300'}`}>
                {destTest.ok ? '✓ Bağlantı OK' : '✗ ' + (destTest.hata?.slice(0, 80) || 'Hata')}
              </span>
            )}
            <button type="button" onClick={destBaglantiTesti} disabled={destKayit || !destForm.host || !destForm.kullanici}
              className="text-xs px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:opacity-50">
              {destKayit ? 'Test ediliyor…' : 'Bağlantı Testi'}
            </button>
            <button type="button" onClick={destKaydet} disabled={destKayit || !destForm.host || !destForm.kullanici}
              className="text-xs px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 rounded font-medium">
              Kaydet
            </button>
            {!dest.yok && (
              <button type="button" onClick={destSil} disabled={destKayit}
                className="text-xs px-3 py-1.5 border border-red-300 dark:border-red-700 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 rounded">
                Hedefi sil
              </button>
            )}
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 mb-4">
        <button onClick={olustur} disabled={isleniyor} className="px-3.5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">
          {isleniyor ? 'Yedekleniyor…' : '+ Şimdi Yedekle'}
        </button>
        <button onClick={yukle} className="px-3 py-2 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 text-sm rounded-md">↻ Yenile</button>
        <span className="ml-auto text-sm text-slate-500 dark:text-slate-500">{yedekler.length} yedek</span>
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {/* Mobilde kartlar zaten kendi çerçevelerini taşıyor — dış kapsayıcı yalnız lg'de çerçeve verir */}
      <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
        {/* Durum mesajları: dış kapsayıcının çerçevesi artık lg:-only olduğu için
            mobilde kendi kart çerçevelerini taşırlar (aksi halde çıplak metin kalırdı). */}
        {yuk ? <div className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 lg:rounded-none lg:border-0 lg:bg-transparent dark:lg:bg-transparent py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div> :
         yedekler.length === 0 ? <div className="rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 lg:rounded-none lg:border-0 lg:bg-transparent dark:lg:bg-transparent py-16 text-center text-sm text-slate-500 dark:text-slate-500">Henüz yedek yok</div> :
        <div className="lg:overflow-x-auto">
          <table className={T.tablo}>
          <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
            <tr>
              <th className={T.baslik}>Dosya</th>
              <th className={T.baslik}>Tip</th>
              <th className={T.baslik}>Boyut</th>
              <th className={T.baslik}>Oluşturulma</th>
              <th className={`${T.baslik} text-right`}>İşlemler</th>
            </tr>
          </thead>
          <tbody className={T.govde}>
            {yedekler.map(y => (
              <tr key={y.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800`}>
                {/* Birincil tanımlayıcı: dosya adı — mobilde kart başlığı */}
                <td className={`${T.hucreBaslik} font-mono break-all`}>{y.dosya}</td>
                <td className={T.hucre} data-etiket="Tip">
                  <span className={`text-xs px-1.5 py-0.5 rounded uppercase tracking-wider font-semibold ${
                    y.tip === 'planli' ? 'bg-sky-100 text-sky-700' : 'bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 dark:text-slate-500'
                  }`}>{y.tip === 'planli' ? 'Planlı' : y.tip}</span>
                </td>
                <td className={T.hucre} data-etiket="Boyut">
                  <span className="font-mono text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500">{formatBoyut(y.boyut_b)}</span>
                </td>
                <td className={T.hucre} data-etiket="Oluşturulma">
                  <span className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 whitespace-nowrap">{y.olusturma}</span>
                </td>
                <td className={`${T.hucreAksiyon} lg:text-right lg:space-x-1`}>
                  <button onClick={() => indir(y)} className="text-sm text-brand-600 dark:text-brand-400 hover:bg-brand-50 dark:hover:bg-brand-900/30 dark:bg-brand-900/20 px-2 py-1 rounded">İndir</button>
                  <button onClick={() => setGeriYukle(y)} className="text-sm text-amber-700 dark:text-amber-300 hover:bg-amber-50 dark:hover:bg-amber-900/30 dark:bg-amber-900/20 px-2 py-1 rounded">↺ Geri Yükle</button>
                  <button onClick={() => setSilinecek(y)} className="text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 px-2 py-1 rounded">Sil</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        </div>}
      </div>

      <ConfirmDialog
        acik={!!silinecek}
        baslik="Yedek dosyasını sil"
        mesaj={`"${silinecek?.dosya}" silinsin mi?`}
        tehlikeli onayMetni="Evet, sil"
        onOnay={sil}
        onIptal={() => setSilinecek(null)}
      />

      {geriYukle && id && (
        <RestoreModal
          yedek={geriYukle}
          domainId={id}
          onClose={() => setGeriYukle(null)}
          onDone={(m) => { setBasari(m); setGeriYukle(null); yukle() }}
          onErr={(m) => setHata(m)}
        />
      )}
    </div>
  )
}

// RestoreModal: granüler geri yükleme — tam / yalnız dosyalar / yalnız DB / dosya seç / DB seç.
// Varsayılanlar non-destructive: tam/dosyalar EZMEZ, dosya seçimi ayrı klasöre, DB seçimi yeni ada.
type Icerik = {
  dosyalar: { yol: string; boyut: number; dizin: boolean }[]
  veritabanlari: { ad: string; boyut: number }[]
  kesildi: boolean
}
const MODLAR: { deger: RMod; etiket: string; aciklama: string }[] = [
  { deger: 'tam', etiket: 'Tam', aciklama: 'Dosyalar + tüm veritabanları' },
  { deger: 'dosyalar', etiket: 'Yalnız Dosyalar', aciklama: 'Veritabanına dokunma' },
  { deger: 'veritabani', etiket: 'Yalnız Veritabanı', aciklama: 'Dosyalara dokunma' },
  { deger: 'dosya', etiket: 'Dosya Seç', aciklama: 'Belirli dosya/klasörleri geri al' },
  { deger: 'db', etiket: 'Veritabanı Seç', aciklama: 'Tek DB — üzerine yaz veya yeni ada' },
]
type RMod = 'tam' | 'dosyalar' | 'veritabani' | 'dosya' | 'db'

function RestoreModal({ yedek, domainId, onClose, onDone, onErr }: {
  yedek: Yedek; domainId: string
  onClose: () => void; onDone: (m: string) => void; onErr: (m: string) => void
}) {
  const [mod, setMod] = useState<RMod>('tam')
  const [temiz, setTemiz] = useState(false)
  const [busy, setBusy] = useState(false)
  const [icerik, setIcerik] = useState<Icerik | null>(null)
  const [icerikYuk, setIcerikYuk] = useState(false)
  const [ara, setAra] = useState('')
  const [secili, setSecili] = useState<Set<string>>(new Set())
  const [hedef, setHedef] = useState<'klasor' | 'yerinde'>('klasor')
  const [db, setDb] = useState('')
  const [dbHedef, setDbHedef] = useState<'ustune' | 'yeni'>('yeni')
  const [yeniDb, setYeniDb] = useState('')

  const granuler = mod === 'dosya' || mod === 'db'
  useEffect(() => {
    if (granuler && !icerik && !icerikYuk) {
      setIcerikYuk(true)
      api.get<Icerik>(`/domains/${domainId}/backups/${yedek.id}/icerik`)
        .then(r => setIcerik(r.data))
        .catch(e => onErr(apiHata(e, 'Yedek içeriği okunamadı')))
        .finally(() => setIcerikYuk(false))
    }
  }, [mod]) // eslint-disable-line react-hooks/exhaustive-deps

  const filtreli = (() => {
    if (!icerik) return []
    const q = ara.trim().toLowerCase()
    const list = q ? icerik.dosyalar.filter(d => d.yol.toLowerCase().includes(q)) : icerik.dosyalar
    return list.slice(0, 400)
  })()

  function toggle(y: string) {
    setSecili(prev => { const n = new Set(prev); n.has(y) ? n.delete(y) : n.add(y); return n })
  }

  async function gonder() {
    const body: Record<string, unknown> = { mod }
    if (mod === 'tam' || mod === 'dosyalar') body.temiz = temiz
    if (mod === 'dosya') {
      if (secili.size === 0) { onErr('En az bir dosya/klasör seçin'); return }
      body.yollar = [...secili]; body.hedef = hedef
    }
    if (mod === 'db') {
      if (!db) { onErr('Bir veritabanı seçin'); return }
      body.db = db
      if (dbHedef === 'yeni') {
        if (!yeniDb.trim()) { onErr('Yeni veritabanı adı girin'); return }
        body.hedef_db = yeniDb.trim()
      }
    }
    setBusy(true)
    try {
      const { data } = await api.post(`/domains/${domainId}/backups/${yedek.id}/geriyukle`, body)
      let msg = 'Geri yükleme tamam'
      if (data.uyari) msg = data.uyari
      else if (typeof data.db === 'string') msg = data.db
      onDone(msg)
    } catch (e) {
      onErr(apiHata(e, 'Geri yükleme başarısız'))
    } finally {
      setBusy(false)
    }
  }

  const secBtn = (aktif: boolean) =>
    `text-left rounded-xl border px-3 py-2 transition ${aktif
      ? 'border-slate-900 dark:border-slate-100 bg-slate-900 dark:bg-slate-100 text-white dark:text-slate-900'
      : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 hover:border-slate-400 dark:hover:border-slate-500'}`

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" onClick={onClose}>
      <div className="w-full max-w-2xl rounded-2xl bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 shadow-xl max-h-[90vh] overflow-hidden flex flex-col" onClick={e => e.stopPropagation()}>
        <div className="px-5 py-4 border-b border-slate-200 dark:border-slate-800">
          <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100">Yedekten geri yükle</h3>
          <p className="mt-0.5 text-xs font-mono text-slate-500 dark:text-slate-400 break-all">{yedek.dosya}</p>
        </div>

        <div className="px-5 py-4 overflow-y-auto space-y-4">
          <div className="grid grid-cols-2 sm:grid-cols-3 gap-2">
            {MODLAR.map(m => (
              <button key={m.deger} type="button" onClick={() => setMod(m.deger)} className={secBtn(mod === m.deger)}>
                <div className="text-sm font-medium">{m.etiket}</div>
                <div className={`text-[11px] mt-0.5 ${mod === m.deger ? 'text-white/70 dark:text-slate-900/70' : 'text-slate-400 dark:text-slate-500'}`}>{m.aciklama}</div>
              </button>
            ))}
          </div>

          {(mod === 'tam' || mod === 'dosyalar') && (
            <label className="flex items-start gap-2 rounded-xl border border-amber-200 dark:border-amber-900/50 bg-amber-50 dark:bg-amber-900/20 p-3 cursor-pointer">
              <input type="checkbox" checked={temiz} onChange={e => setTemiz(e.target.checked)} className="mt-0.5" />
              <span className="text-xs text-amber-800 dark:text-amber-200">
                <b>Temiz geri yükleme</b> — yedekte olmayan dosyaları SİL. Kapalıyken (önerilen) aktif uygulama korunur, yalnız yedektekiler üzerine yazılır.
              </span>
            </label>
          )}

          {mod === 'veritabani' && (
            <p className="text-xs text-slate-500 dark:text-slate-400 rounded-xl bg-slate-50 dark:bg-slate-800/60 p-3">
              Domaine ait tüm veritabanları yedekteki haline döndürülür. Dosyalara dokunulmaz.
            </p>
          )}

          {mod === 'dosya' && (
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <input value={ara} onChange={e => setAra(e.target.value)} placeholder="Dosya ara…"
                  className="flex-1 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-3 py-1.5 text-sm" />
                <span className="text-xs text-slate-400 shrink-0">{secili.size} seçili</span>
              </div>
              <div className="rounded-xl border border-slate-200 dark:border-slate-700 max-h-56 overflow-y-auto divide-y divide-slate-100 dark:divide-slate-800">
                {icerikYuk && <div className="p-3 text-sm text-slate-400">Yükleniyor…</div>}
                {!icerikYuk && filtreli.map(d => (
                  <label key={d.yol} className="flex items-center gap-2 px-3 py-1.5 text-sm cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800/50">
                    <input type="checkbox" checked={secili.has(d.yol)} onChange={() => toggle(d.yol)} />
                    <span className={`truncate ${d.dizin ? 'font-medium' : ''}`}>{d.dizin ? '📁 ' : ''}{d.yol}</span>
                  </label>
                ))}
                {!icerikYuk && filtreli.length === 0 && <div className="p-3 text-sm text-slate-400">Eşleşme yok</div>}
              </div>
              {icerik?.kesildi && <p className="text-[11px] text-slate-400">Liste kısaltıldı — daha spesifik aramak için filtreleyin.</p>}
              <div className="flex gap-2">
                <button type="button" onClick={() => setHedef('klasor')} className={`flex-1 ${secBtn(hedef === 'klasor')}`}>
                  <div className="text-sm font-medium">Ayrı klasöre</div>
                  <div className={`text-[11px] ${hedef === 'klasor' ? 'text-white/70 dark:text-slate-900/70' : 'text-slate-400'}`}>geri-yukleme-…/ (hiçbir şey ezilmez)</div>
                </button>
                <button type="button" onClick={() => setHedef('yerinde')} className={`flex-1 ${secBtn(hedef === 'yerinde')}`}>
                  <div className="text-sm font-medium">Yerinde</div>
                  <div className={`text-[11px] ${hedef === 'yerinde' ? 'text-white/70 dark:text-slate-900/70' : 'text-slate-400'}`}>orijinal konuma (üzerine yaz)</div>
                </button>
              </div>
            </div>
          )}

          {mod === 'db' && (
            <div className="space-y-2">
              {icerikYuk && <div className="text-sm text-slate-400">Yükleniyor…</div>}
              <div className="rounded-xl border border-slate-200 dark:border-slate-700 divide-y divide-slate-100 dark:divide-slate-800">
                {icerik?.veritabanlari.map(x => (
                  <label key={x.ad} className="flex items-center gap-2 px-3 py-2 text-sm cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800/50">
                    <input type="radio" name="db" checked={db === x.ad} onChange={() => { setDb(x.ad); setYeniDb(x.ad + '_geri') }} />
                    <span className="font-mono">{x.ad}</span>
                    <span className="ml-auto text-xs text-slate-400">{formatBoyut(x.boyut)}</span>
                  </label>
                ))}
                {!icerikYuk && !icerik?.veritabanlari.length && <div className="p-3 text-sm text-slate-400">Yedekte veritabanı yok</div>}
              </div>
              {db && (
                <div className="flex flex-col gap-2 rounded-xl bg-slate-50 dark:bg-slate-800/60 p-3">
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <input type="radio" name="dbh" checked={dbHedef === 'yeni'} onChange={() => setDbHedef('yeni')} />
                    Yeni veritabanına (güvenli, orijinal korunur)
                  </label>
                  {dbHedef === 'yeni' && (
                    <input value={yeniDb} onChange={e => setYeniDb(e.target.value)}
                      className="rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-3 py-1.5 text-sm font-mono ml-6" />
                  )}
                  <label className="flex items-center gap-2 text-sm cursor-pointer">
                    <input type="radio" name="dbh" checked={dbHedef === 'ustune'} onChange={() => setDbHedef('ustune')} />
                    <span><b className="text-amber-700 dark:text-amber-300">Üzerine yaz</b> — mevcut ‘{db}’ silinip yüklenir</span>
                  </label>
                </div>
              )}
            </div>
          )}
        </div>

        <div className="px-5 py-3 border-t border-slate-200 dark:border-slate-800 flex justify-end gap-2">
          <button type="button" onClick={onClose} disabled={busy}
            className="rounded-xl border border-slate-200 dark:border-slate-700 px-4 py-2 text-sm hover:bg-slate-50 dark:hover:bg-slate-800">İptal</button>
          <button type="button" onClick={gonder} disabled={busy}
            className="rounded-xl bg-slate-900 dark:bg-slate-100 text-white dark:text-slate-900 px-4 py-2 text-sm font-medium hover:opacity-90 disabled:opacity-50">
            {busy ? 'Geri yükleniyor…' : 'Geri Yükle'}
          </button>
        </div>
      </div>
    </div>
  )
}

function formatBoyut(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`
}