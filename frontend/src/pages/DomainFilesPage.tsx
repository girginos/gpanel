// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState, useRef } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import DizinAgac from '@/components/DizinAgac'
import KodEditor from '@/components/KodEditor'

type Entry = {
  adi: string
  yol: string
  tip: 'klasor' | 'dosya' | 'sembolik'
  boyut_b: number
  mod: string
  degisme: string
}

type ListResp = { yol: string; icerik: Entry[]; toplam: number }
type Domain = { id: number; alan_adi: string; sistem_kullanici: string }

const ROOT = '/'

export default function DomainFilesPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [yol, setYol] = useState<string>('/public_html')
  const [icerik, setIcerik] = useState<Entry[]>([])
  const [yukleniyor, setYukleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [yukleneniDosya, setYukleneniDosya] = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [editor, setEditor] = useState<{yol: string; icerik: string} | null>(null)
  const [chmodFor, setChmodFor] = useState<Entry | null>(null)
  const [agacYenileme, setAgacYenileme] = useState(0)
  const [siruklemeSayaci, setSiruklemeSayaci] = useState(0)
  const [seciliSet, setSeciliSet] = useState<Set<string>>(new Set())
  const [topluSilOnay, setTopluSilOnay] = useState(false)
  const [extractAktif, setExtractAktif] = useState(false)
  const [yeniMenuAcik, setYeniMenuAcik] = useState(false)
  const [arsivMenuAcik, setArsivMenuAcik] = useState(false)
  const [daMenuAcik, setDaMenuAcik] = useState(false)
  const [aramaQ, setAramaQ] = useState('')
  const [aramaSonuc, setAramaSonuc] = useState<Entry[] | null>(null)
  const [kopyalaModal, setKopyalaModal] = useState<{ tip: 'kopyala' | 'tasi'; yollar: string[] } | null>(null)
  const [arsivModal, setArsivModal] = useState(false)
  const [yeniDosyaModal, setYeniDosyaModal] = useState(false)
  const [boyutSonuc, setBoyutSonuc] = useState<{ yol: string; boyut: number } | null>(null)
  const [topluYukleme, setTopluYukleme] = useState<{
    tamam: number
    toplam: number
    aktif: string
    aktifIndex: number
    yuklenenByte: number    // mevcut dosya icin
    toplamByte: number      // mevcut dosya icin
    hizBps: number          // bytes/sn
    etaSn: number           // saniye
    yuzde: number
  } | null>(null)
  const [renameFor, setRenameFor] = useState<Entry | null>(null)

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(() => {})
  }, [id])

  function tara() {
    if (!id) return
    setYukleniyor(true); setHata(null)
    api.get<ListResp>(`/domains/${id}/files`, { params: { yol } })
      .then(r => setIcerik(r.data.icerik))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYukleniyor(false))
  }
  useEffect(tara, [id, yol])
  useEffect(() => { setSeciliSet(new Set()) }, [yol])

  function git(yeni: string) {
    setYol(yeni)
  }

  function geri() {
    if (yol === '/' || yol === '') return
    const parca = yol.split('/').filter(Boolean)
    parca.pop()
    setYol('/' + parca.join('/'))
  }

  async function sil(e: Entry) {
    if (!confirm(`"${e.adi}" silinsin mi? Bu işlem geri alınamaz.`)) return
    try {
      await api.delete(`/domains/${id}/files`, { params: { yol: e.yol } })
      setAgacYenileme(x => x + 1)
      tara()
    } catch (err) {
      alert(apiHata(err, 'Silme başarısız'))
    }
  }

  async function klasorOlustur() {
    const ad = prompt('Yeni klasör adı:')
    if (!ad) return
    const hedef = (yol === '/' ? '' : yol) + '/' + ad
    try {
      await api.post(`/domains/${id}/files/mkdir`, { yol: hedef })
      setAgacYenileme(x => x + 1)
      tara()
    } catch (err) {
      alert(apiHata(err, 'Klasör oluşturma başarısız'))
    }
  }

  async function editorAc(e: Entry) {
    if (e.tip !== 'dosya') return
    try {
      const { data } = await api.get<{yol: string; icerik: string}>(`/domains/${id}/files/oku`, { params: { yol: e.yol } })
      setEditor({ yol: e.yol, icerik: data.icerik })
    } catch (err) {
      alert(apiHata(err, 'Açılamadı'))
    }
  }

  async function editorKaydet() {
    if (!editor) return
    try {
      await api.post(`/domains/${id}/files/yaz`, { yol: editor.yol, icerik: editor.icerik })
      setEditor(null); tara()
    } catch (err) {
      alert(apiHata(err, 'Kaydedilemedi'))
    }
  }

  async function yenidenAdlandir(e: Entry, yeniAd: string) {
    if (!yeniAd || yeniAd === e.adi) return
    const parca = e.yol.split('/')
    parca[parca.length - 1] = yeniAd
    const yeni = parca.join('/')
    try {
      await api.post(`/domains/${id}/files/rename`, { eski: e.yol, yeni })
      setRenameFor(null); setAgacYenileme(x => x + 1); tara()
    } catch (err) {
      alert(apiHata(err, 'Yeniden adlandırılamadı'))
    }
  }

  async function izinDegistir(e: Entry, mod: string) {
    try {
      await api.post(`/domains/${id}/files/chmod`, { yol: e.yol, mod })
      setChmodFor(null); tara()
    } catch (err) {
      alert(apiHata(err, 'İzin değiştirilemedi'))
    }
  }

  async function dosyaSec(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    if (!f) return
    setYukleneniDosya(f.name)
    const fd = new FormData()
    fd.append('dosya', f)
    try {
      await api.post(`/domains/${id}/files/upload`, fd, {
        params: { yol },
        headers: { 'Content-Type': 'multipart/form-data' },
      })
      tara()
    } catch (err) {
      alert(apiHata(err, 'Yükleme başarısız'))
    } finally {
      setYukleneniDosya(null)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  // Tek bir File nesnesini yükle (drag&drop + input için ortak helper)
  async function dosyaYukleTekli(f: File, onProgress?: (loaded: number, total: number) => void): Promise<boolean> {
    const fd = new FormData()
    fd.append('dosya', f)
    try {
      await api.post(`/domains/${id}/files/upload`, fd, {
        params: { yol },
        headers: { 'Content-Type': 'multipart/form-data' },
        onUploadProgress: (e: any) => {
          if (onProgress && typeof e.loaded === 'number') {
            onProgress(e.loaded, e.total || f.size)
          }
        },
      })
      return true
    } catch (err) {
      console.error('yukleme hata', f.name, err)
      return false
    }
  }

  async function dosyalariYukle(files: File[]) {
    if (!files.length) return
    setTopluYukleme({
      tamam: 0, toplam: files.length, aktif: files[0].name, aktifIndex: 0,
      yuklenenByte: 0, toplamByte: files[0].size,
      hizBps: 0, etaSn: 0, yuzde: 0,
    })
    let basarili = 0
    for (let i = 0; i < files.length; i++) {
      const f = files[i]
      // Per-dosya hız ölçümü için baslangic zamanı + son ölçüm
      const t0 = performance.now()
      let sonOlcum = t0
      let sonByte = 0

      const ok = await dosyaYukleTekli(f, (loaded, total) => {
        const simdi = performance.now()
        const dt = (simdi - sonOlcum) / 1000
        const db = loaded - sonByte
        let hiz = 0
        if (dt > 0.05) {
          hiz = db / dt
          sonOlcum = simdi
          sonByte = loaded
        }
        // Toplam hız (smooth)
        const toplamDt = (simdi - t0) / 1000
        const ortHiz = toplamDt > 0.1 ? loaded / toplamDt : 0
        const kalanByte = Math.max(0, total - loaded)
        const eta = ortHiz > 0 ? kalanByte / ortHiz : 0
        const yuzde = total > 0 ? (loaded / total) * 100 : 0
        setTopluYukleme(prev => prev ? {
          ...prev,
          tamam: i, aktif: f.name, aktifIndex: i,
          yuklenenByte: loaded, toplamByte: total,
          hizBps: hiz > 0 ? hiz : ortHiz,
          etaSn: eta,
          yuzde,
        } : null)
      })
      if (ok) basarili++
    }
    setTopluYukleme(null)
    setAgacYenileme(x => x + 1)
    tara()
    if (basarili < files.length) {
      alert(`${basarili}/${files.length} dosya yüklendi, bazıları başarısız oldu.`)
    }
  }


  function secimTogga(yol: string) {
    setSeciliSet(prev => {
      const yeni = new Set(prev)
      if (yeni.has(yol)) yeni.delete(yol); else yeni.add(yol)
      return yeni
    })
  }
  function tumunuSec(secVar: boolean) {
    if (secVar) setSeciliSet(new Set(icerik.map(e => e.yol)))
    else setSeciliSet(new Set())
  }

  async function topluSil() {
    setTopluSilOnay(false)
    const yollar = Array.from(seciliSet)
    let basarili = 0
    for (const y of yollar) {
      try {
        await api.delete(`/domains/${id}/files`, { params: { yol: y } })
        basarili++
      } catch (err) {
        console.error('sil hata', y, err)
      }
    }
    setSeciliSet(new Set())
    setAgacYenileme(x => x + 1)
    tara()
    if (basarili < yollar.length) alert(`${basarili}/${yollar.length} silindi.`)
  }

  async function extractEt(e: Entry) {
    setExtractAktif(true)
    try {
      await api.post(`/domains/${id}/files/extract`, { yol: e.yol })
      setAgacYenileme(x => x + 1)
      tara()
    } catch (err) {
      alert(apiHata(err, 'Açılamadı (zip/tar destek vardır)'))
    } finally {
      setExtractAktif(false)
    }
  }

  async function arama() {
    if (!aramaQ.trim()) { setAramaSonuc(null); return }
    try {
      const { data } = await api.get(`/domains/${id}/files/ara`, { params: { q: aramaQ, yol } })
      setAramaSonuc(data.icerik)
    } catch (err) {
      alert(apiHata(err, 'Arama başarısız'))
    }
  }

  async function kopyaTasi(hedef: string) {
    if (!kopyalaModal) return
    const url = kopyalaModal.tip === 'kopyala' ? 'copy' : 'move'
    try {
      const { data } = await api.post(`/domains/${id}/files/${url}`, {
        kaynaklar: kopyalaModal.yollar, hedef,
      })
      setKopyalaModal(null); setSeciliSet(new Set())
      setAgacYenileme(x => x + 1); tara()
      if (data.hatalar?.length) alert('Bazı hatalar: ' + data.hatalar.join('\n'))
    } catch (err) {
      alert(apiHata(err, kopyalaModal.tip === 'kopyala' ? 'Kopyalama hata' : 'Taşıma hata'))
    }
  }

  async function arsivle(ciktiAd: string, format: 'zip' | 'tar.gz') {
    const yollar = Array.from(seciliSet)
    if (yollar.length === 0) return
    const cikti = (yol === '/' ? '' : yol) + '/' + ciktiAd + (format === 'zip' ? '.zip' : '.tar.gz')
    try {
      await api.post(`/domains/${id}/files/archive`, { kaynaklar: yollar, cikti_yol: cikti, format })
      setArsivModal(false); setSeciliSet(new Set())
      setAgacYenileme(x => x + 1); tara()
    } catch (err) {
      alert(apiHata(err, 'Arşivleme hata'))
    }
  }

  async function yeniDosyaOlustur(ad: string) {
    const hedef = (yol === '/' ? '' : yol) + '/' + ad
    try {
      const { data } = await api.post(`/domains/${id}/files/yeni-dosya`, { yol: hedef })
      setYeniDosyaModal(false); tara()
      // Direkt editöre aç
      const okuResp = await api.get(`/domains/${id}/files/oku`, { params: { yol: hedef } })
      setEditor({ yol: hedef, icerik: okuResp.data.icerik })
    } catch (err) {
      alert(apiHata(err, 'Oluşturma hata'))
    }
  }

  async function boyutHesapla(yolu: string) {
    try {
      const { data } = await api.get(`/domains/${id}/files/boyut`, { params: { yol: yolu } })
      setBoyutSonuc({ yol: yolu, boyut: data.boyut_b })
    } catch (err) {
      alert(apiHata(err, 'Boyut hesabi hata'))
    }
  }

  function siruklemeGiris(e: React.DragEvent) {
    if (!Array.from(e.dataTransfer.types).includes('Files')) return
    e.preventDefault()
    setSiruklemeSayaci(x => x + 1)
  }
  function siruklemeCikis(e: React.DragEvent) {
    if (!Array.from(e.dataTransfer.types).includes('Files')) return
    e.preventDefault()
    setSiruklemeSayaci(x => Math.max(0, x - 1))
  }
  function siruklemeUstunde(e: React.DragEvent) {
    if (!Array.from(e.dataTransfer.types).includes('Files')) return
    e.preventDefault()
    e.dataTransfer.dropEffect = 'copy'
  }
  function birak(e: React.DragEvent) {
    e.preventDefault()
    setSiruklemeSayaci(0)
    const dt = e.dataTransfer
    if (!dt || dt.files.length === 0) return
    dosyalariYukle(Array.from(dt.files))
  }

  function indir(e: Entry) {
    const tok = localStorage.getItem('gosp.token') || ''
    const url = `/api/v1/domains/${id}/files/indir?yol=${encodeURIComponent(e.yol)}`
    // Header'lı GET tarayıcıdan; en basit: ayrı fetch + blob
    fetch(url, { headers: { Authorization: `Bearer ${tok}` } })
      .then(r => r.blob())
      .then(blob => {
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = e.adi
        a.click()
        setTimeout(() => URL.revokeObjectURL(a.href), 1000)
      })
      .catch(err => alert('İndirme başarısız: ' + err.message))
  }

  const parcalar = yol.split('/').filter(Boolean)

  return (
    <div className="px-6 py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '…', href: `/abonelikler/${id}` },
        { etiket: 'Dosyalar' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Dosya Yöneticisi</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}
          <span className="font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">/home/{domain.sistem_kullanici}</span>
        </p>
      )}

      <div className="grid grid-cols-12 gap-4">
        <aside className="col-span-12 lg:col-span-3">
          <DizinAgac domainId={id!} secili={yol} onSec={setYol} yenileme={agacYenileme} />
        </aside>
        <section
          className={`col-span-12 lg:col-span-9 relative ${siruklemeSayaci > 0 ? "ring-2 ring-brand-500 ring-offset-2 ring-offset-slate-50 rounded-lg" : ""}`}
          onDragEnter={siruklemeGiris}
          onDragLeave={siruklemeCikis}
          onDragOver={siruklemeUstunde}
          onDrop={birak}
        >
      {siruklemeSayaci > 0 && (
        <div className="absolute inset-0 z-30 border-2 border-dashed border-brand-500 bg-brand-50 dark:bg-brand-900/20 backdrop-blur-sm rounded-lg flex items-center justify-center pointer-events-none">
          <div className="text-center">
            <svg className="w-14 h-14 mx-auto text-brand-600 dark:text-brand-400 mb-2" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5M16.5 12L12 16.5m0 0L7.5 12m4.5 4.5V3" />
            </svg>
            <div className="text-lg font-semibold text-brand-700 dark:text-brand-300">Dosyaları buraya bırak</div>
            <div className="text-sm text-brand-600 dark:text-brand-400/80 mt-1">Hedef dizin: <code className="font-mono bg-white dark:bg-slate-800/60 px-1.5 py-0.5 rounded">{yol}</code></div>
          </div>
        </div>
      )}
      {seciliSet.size > 0 && (
        <div className="mb-3 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-300 dark:border-amber-700 rounded-md flex items-center gap-3 flex-wrap">
          <span className="text-sm font-semibold text-amber-800 dark:text-amber-200">{seciliSet.size} öğe seçili</span>
          <button onClick={() => setTopluSilOnay(true)} className="text-xs px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white rounded font-medium">Sil ({seciliSet.size})</button>
          <button onClick={() => setSeciliSet(new Set())} className="text-xs px-3 py-1.5 border border-amber-300 dark:border-amber-700 text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:bg-amber-900/30 rounded">Secimi temizle</button>
        </div>
      )}
      {topluYukleme && (
        <div className="mb-3 px-3 py-2.5 bg-sky-50 dark:bg-sky-900/20 border border-sky-200 rounded-md text-sm text-sky-800">
          <div className="flex items-center gap-3 mb-1.5">
            <svg className="w-4 h-4 flex-shrink-0 animate-spin" fill="none" viewBox="0 0 24 24">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
            </svg>
            <div className="flex-1 min-w-0">
              <div className="font-medium text-sm">
                Yükleniyor… <span className="font-mono">{topluYukleme.aktifIndex + 1} / {topluYukleme.toplam}</span>
              </div>
              <div className="text-xs text-sky-700/90 truncate">{topluYukleme.aktif}</div>
            </div>
            <div className="flex-shrink-0 text-right">
              <div className="text-sm font-mono font-semibold">{topluYukleme.yuzde.toFixed(1)}%</div>
              <div className="text-[10px] text-sky-700/80">{boyutBicim(topluYukleme.yuklenenByte)} / {boyutBicim(topluYukleme.toplamByte)}</div>
            </div>
          </div>
          {/* Progress bar */}
          <div className="h-1.5 bg-sky-100 rounded overflow-hidden">
            <div
              className="h-full bg-gradient-to-r from-sky-500 to-sky-600 transition-all duration-100"
              style={{ width: `${Math.min(100, topluYukleme.yuzde)}%` }}
            />
          </div>
          {/* Hiz + ETA */}
          <div className="flex items-center justify-between mt-1 text-[11px] font-mono text-sky-700/80">
            <span>{topluYukleme.hizBps > 0 ? hizBicim(topluYukleme.hizBps) : '—'}</span>
            <span>{topluYukleme.etaSn > 0 ? `Kalan: ${etaBicim(topluYukleme.etaSn)}` : ''}</span>
          </div>
        </div>
      )}
      {/* Toolbar — Plesk modeli */}
      <div className="flex items-center gap-1.5 mb-3 flex-wrap relative">
        {/* + dropdown (Yeni Dosya / Klasör / Upload) */}
        <div className="relative">
          <button onClick={() => setYeniMenuAcik(v => !v)}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 text-sm font-medium rounded shadow-sm">
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
            </svg>
            <svg className="w-3 h-3" fill="currentColor" viewBox="0 0 20 20">
              <path d="M5.516 7.548c.436-.446 1.043-.481 1.576 0L10 10.405l2.908-2.857c.533-.481 1.141-.446 1.576 0 .436.445.408 1.197 0 1.615-.406.418-3.695 3.629-3.695 3.629a1.105 1.105 0 01-1.576 0S5.924 9.581 5.516 9.163c-.409-.418-.436-1.17 0-1.615z" />
            </svg>
          </button>
          {yeniMenuAcik && (
            <div className="absolute z-40 mt-1 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md shadow-lg min-w-[180px] py-1">
              <button onClick={() => { setYeniMenuAcik(false); fileInputRef.current?.click() }} className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800">📤 Dosya Yükle</button>
              <button onClick={() => { setYeniMenuAcik(false); klasorOlustur() }} className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800">📁 Yeni Klasör</button>
              <button onClick={() => { setYeniMenuAcik(false); setYeniDosyaModal(true) }} className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800">📄 Yeni Dosya</button>
            </div>
          )}
        </div>

        {/* Copy */}
        <button onClick={() => setKopyalaModal({ tip: 'kopyala', yollar: Array.from(seciliSet) })}
          disabled={seciliSet.size === 0}
          className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:opacity-40 disabled:cursor-not-allowed text-sm rounded">
          Kopyala
        </button>

        {/* Move */}
        <button onClick={() => setKopyalaModal({ tip: 'tasi', yollar: Array.from(seciliSet) })}
          disabled={seciliSet.size === 0}
          className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:opacity-40 disabled:cursor-not-allowed text-sm rounded">
          Taşı
        </button>

        {/* Arşiv dropdown */}
        <div className="relative">
          <button onClick={() => setArsivMenuAcik(v => !v)}
            className="inline-flex items-center gap-1 px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">
            Arşiv
            <svg className="w-3 h-3" fill="currentColor" viewBox="0 0 20 20"><path d="M5.516 7.548c.436-.446 1.043-.481 1.576 0L10 10.405l2.908-2.857c.533-.481 1.141-.446 1.576 0 .436.445.408 1.197 0 1.615-.406.418-3.695 3.629-3.695 3.629a1.105 1.105 0 01-1.576 0S5.924 9.581 5.516 9.163c-.409-.418-.436-1.17 0-1.615z"/></svg>
          </button>
          {arsivMenuAcik && (
            <div className="absolute z-40 mt-1 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md shadow-lg min-w-[180px] py-1">
              <button
                onClick={() => {
                  setArsivMenuAcik(false)
                  if (seciliSet.size !== 1) { alert('Tek bir arşiv dosyası seçin'); return }
                  const yol = Array.from(seciliSet)[0]
                  const dosya = icerik.find(e => e.yol === yol)
                  if (dosya) extractEt(dosya)
                }}
                disabled={seciliSet.size !== 1}
                className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:text-slate-300">
                📂 Arşivi Aç (Extract)
              </button>
              <button
                onClick={() => { setArsivMenuAcik(false); setArsivModal(true) }}
                disabled={seciliSet.size === 0}
                className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:text-slate-300">
                📦 Arşive Ekle (ZIP/TAR.GZ)
              </button>
            </div>
          )}
        </div>

        {/* Daha dropdown */}
        <div className="relative">
          <button onClick={() => setDaMenuAcik(v => !v)}
            className="inline-flex items-center gap-1 px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">
            Daha
            <svg className="w-3 h-3" fill="currentColor" viewBox="0 0 20 20"><path d="M5.516 7.548c.436-.446 1.043-.481 1.576 0L10 10.405l2.908-2.857c.533-.481 1.141-.446 1.576 0 .436.445.408 1.197 0 1.615-.406.418-3.695 3.629-3.695 3.629a1.105 1.105 0 01-1.576 0S5.924 9.581 5.516 9.163c-.409-.418-.436-1.17 0-1.615z"/></svg>
          </button>
          {daMenuAcik && (
            <div className="absolute z-40 mt-1 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md shadow-lg min-w-[200px] py-1">
              <button
                onClick={() => {
                  setDaMenuAcik(false)
                  if (seciliSet.size !== 1) { alert('Tek bir öğe seçin'); return }
                  boyutHesapla(Array.from(seciliSet)[0])
                }}
                disabled={seciliSet.size !== 1}
                className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:text-slate-300">
                📏 Boyut Hesapla
              </button>
              <button
                onClick={() => {
                  setDaMenuAcik(false)
                  if (seciliSet.size !== 1) { alert('Tek bir öğe seçin'); return }
                  const yol = Array.from(seciliSet)[0]
                  const dosya = icerik.find(e => e.yol === yol)
                  if (dosya) setChmodFor(dosya)
                }}
                disabled={seciliSet.size !== 1}
                className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:text-slate-300">
                🔒 İzinleri Değiştir
              </button>
              <button
                onClick={() => {
                  setDaMenuAcik(false)
                  if (seciliSet.size !== 1) { alert('Tek bir öğe seçin'); return }
                  const yol = Array.from(seciliSet)[0]
                  const dosya = icerik.find(e => e.yol === yol)
                  if (dosya) setRenameFor(dosya)
                }}
                disabled={seciliSet.size !== 1}
                className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:text-slate-300">
                ✏ Yeniden Adlandır
              </button>
              <div className="border-t border-slate-100 dark:border-slate-800 my-1"></div>
              <button onClick={() => { setDaMenuAcik(false); tara() }}
                className="block w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800">
                ↻ Yenile
              </button>
            </div>
          )}
        </div>

        {/* Sil */}
        <button onClick={() => setTopluSilOnay(true)}
          disabled={seciliSet.size === 0}
          className="px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:bg-red-300 disabled:cursor-not-allowed text-white text-sm rounded font-medium">
          Sil{seciliSet.size > 0 ? ` (${seciliSet.size})` : ''}
        </button>

        <div className="flex-1" />

        {/* Arama */}
        <div className="relative">
          <input
            type="text"
            value={aramaQ}
            onChange={e => setAramaQ(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && arama()}
            placeholder="🔍 Dosya ara…"
            className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm w-56 focus:border-brand-500 outline-none"
          />
          {aramaSonuc && (
            <button onClick={() => { setAramaQ(''); setAramaSonuc(null) }}
              className="absolute right-1 top-1/2 -translate-y-1/2 px-1.5 text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300">×</button>
          )}
        </div>

        {/* Gizli upload input */}
        <input ref={fileInputRef} type="file" multiple onChange={e => { const list = Array.from(e.target.files || []); if (list.length === 1) dosyaSec(e); else if (list.length > 1) dosyalariYukle(list); e.target.value = ""; }} className="hidden" />

        <div className="ml-auto text-sm text-slate-500 dark:text-slate-500">{icerik.length} öğe</div>
      </div>

      {/* Path breadcrumb */}
      <div className="flex items-center gap-1 mb-4 text-sm flex-wrap bg-slate-50 dark:bg-slate-900 px-3 py-2 rounded-lg border border-slate-200 dark:border-slate-700">
        <button onClick={() => git('/')} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-mono">~</button>
        {parcalar.map((p, i) => {
          const yolBuraya = '/' + parcalar.slice(0, i + 1).join('/')
          return (
            <span key={i} className="flex items-center gap-1">
              <span className="text-slate-300">/</span>
              <button onClick={() => git(yolBuraya)} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-mono">{p}</button>
            </span>
          )
        })}
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {/* Dosya tablosu */}
      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden">
        {yukleniyor ? (
          <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
        ) : (
          <table className="w-full">
            <thead className="bg-slate-50 dark:bg-slate-900 text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500 border-b border-slate-200 dark:border-slate-700">
              <tr>
                <th className="px-3 py-2.5 w-10 text-center"><input type="checkbox" checked={icerik.length > 0 && seciliSet.size === icerik.length} ref={ref => { if (ref) ref.indeterminate = seciliSet.size > 0 && seciliSet.size < icerik.length }} onChange={e => tumunuSec(e.target.checked)} className="cursor-pointer" /></th>
                <th className="text-left px-4 py-2.5">Ad</th>
                <th className="text-left px-4 py-2.5">Boyut</th>
                <th className="text-left px-4 py-2.5">İzin</th>
                <th className="text-left px-4 py-2.5">Değişiklik</th>
                <th className="text-right px-4 py-2.5">İşlemler</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {yol !== '/' && (
                <tr className="hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 cursor-pointer" onClick={geri}>
                  <td className="px-4 py-2.5 text-sm" colSpan={6}>
                    <span className="text-slate-500 dark:text-slate-500">↑ üst klasör</span>
                  </td>
                </tr>
              )}
              {icerik.length === 0 && !yukleniyor && (
                <tr>
                  <td colSpan={6} className="px-4 py-12 text-center text-sm text-slate-400 dark:text-slate-500">Bu klasör boş</td>
                </tr>
              )}
              {(aramaSonuc ?? icerik).map((e) => (
                <tr key={e.yol} className={`hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 transition ${seciliSet.has(e.yol) ? 'bg-brand-50 dark:bg-brand-900/20' : ''}`}>
                  <td className="px-3 py-2.5 text-center">
                    <input type="checkbox" checked={seciliSet.has(e.yol)}
                      onChange={() => secimTogga(e.yol)}
                      onClick={ev => ev.stopPropagation()}
                      className="cursor-pointer" />
                  </td>
                  <td className="px-4 py-2.5">
                    {e.tip === 'klasor' ? (
                      <button
                        onClick={() => git(e.yol)}
                        className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium flex items-center gap-2"
                      >
                        <svg className="w-4 h-4 text-amber-500" fill="currentColor" viewBox="0 0 24 24">
                          <path d="M10 4H4c-1.11 0-2 .89-2 2v12c0 1.11.89 2 2 2h16c1.11 0 2-.89 2-2V8c0-1.11-.89-2-2-2h-8l-2-2z" />
                        </svg>
                        {e.adi}
                      </button>
                    ) : (
                      <span className="flex items-center gap-2 text-slate-800 dark:text-slate-200">
                        <svg className="w-4 h-4 text-slate-400 dark:text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.7}>
                          <path strokeLinecap="round" strokeLinejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                        </svg>
                        <span>{e.adi}</span>
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-sm font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">
                    {e.tip === 'klasor' ? '—' : formatBoyut(e.boyut_b)}
                  </td>
                  <td className="px-4 py-2.5 text-sm font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">{e.mod}</td>
                  <td className="px-4 py-2.5 text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500">{formatTarih(e.degisme)}</td>
                  <td className="px-4 py-2.5 text-right space-x-2">
                    {e.tip !== 'klasor' && (
                      <button
                        onClick={() => indir(e)}
                        className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 px-2 py-1 rounded hover:bg-brand-50 dark:hover:bg-brand-900/30 dark:bg-brand-900/20 transition"
                      >
                        İndir
                      </button>
                    )}
                    {e.tip === "dosya" && /\.(zip|tar|tar\.gz|tgz|tar\.bz2|tbz2|tar\.xz|txz|gz)$/i.test(e.adi) && (
                      <button
                        onClick={() => extractEt(e)}
                        disabled={extractAktif}
                        className="text-sm text-violet-600 dark:text-violet-400 hover:text-violet-700 dark:text-violet-300 disabled:text-slate-300 px-2 py-1 rounded hover:bg-violet-50 dark:bg-violet-900/20 transition"
                        title="Arşivi aç (extract)"
                      >📦 Aç</button>
                    )}
                    {e.tip === "dosya" && (
                      <button
                        onClick={() => editorAc(e)}
                        className="text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 px-2 py-1 rounded hover:bg-brand-50 dark:hover:bg-brand-900/30 dark:bg-brand-900/20 transition"
                        title="Duzenle"
                      >Duzenle</button>
                    )}
                    <button
                      onClick={() => setRenameFor(e)}
                      className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:text-slate-900 dark:hover:text-slate-100 dark:text-slate-100 px-2 py-1 rounded hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800 transition"
                      title="Yeniden adlandir"
                    >Adlandir</button>
                    <button
                      onClick={() => setChmodFor(e)}
                      className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:text-slate-900 dark:hover:text-slate-100 dark:text-slate-100 px-2 py-1 rounded hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800 transition"
                      title="Izinler"
                    >Izinler</button>
                    <button
                      onClick={() => sil(e)}
                      className="text-sm text-red-600 dark:text-red-400 hover:text-red-700 dark:text-red-300 px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 transition"
                    >
                      Sil
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
      {editor && (
        <KodEditor yol={editor.yol} icerik={editor.icerik}
          onChange={s => setEditor({ ...editor, icerik: s })}
          onKaydet={editorKaydet}
          onKapat={() => setEditor(null)} />
      )}
      {renameFor && (
        <RenameModal entry={renameFor}
          onTamam={ad => yenidenAdlandir(renameFor, ad)}
          onIptal={() => setRenameFor(null)} />
      )}
      {chmodFor && (
        <ChmodModal entry={chmodFor}
          onTamam={mod => izinDegistir(chmodFor, mod)}
          onIptal={() => setChmodFor(null)} />
      )}
      {kopyalaModal && (
        <KopyaTasiModal
          tip={kopyalaModal.tip}
          yollar={kopyalaModal.yollar}
          domainId={id!}
          onTamam={kopyaTasi}
          onIptal={() => setKopyalaModal(null)} />
      )}
      {arsivModal && (
        <ArsivModal
          adetSayi={seciliSet.size}
          onTamam={arsivle}
          onIptal={() => setArsivModal(false)} />
      )}
      {yeniDosyaModal && (
        <YeniDosyaModal
          onTamam={yeniDosyaOlustur}
          onIptal={() => setYeniDosyaModal(false)} />
      )}
      {boyutSonuc && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => setBoyutSonuc(null)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-2">📏 Boyut Bilgisi</h3>
            <p className="text-xs text-slate-500 dark:text-slate-500 mb-3 font-mono">{boyutSonuc.yol}</p>
            <div className="text-2xl font-bold text-brand-700 dark:text-brand-300 mb-2">
              {(() => {
                const b = boyutSonuc.boyut
                if (b < 1024) return b + ' B'
                if (b < 1024*1024) return (b/1024).toFixed(1) + ' KB'
                if (b < 1024*1024*1024) return (b/1024/1024).toFixed(1) + ' MB'
                return (b/1024/1024/1024).toFixed(2) + ' GB'
              })()}
            </div>
            <div className="text-xs text-slate-500 dark:text-slate-500 font-mono">{boyutSonuc.boyut.toLocaleString('tr-TR')} bayt</div>
            <div className="mt-4 flex justify-end">
              <button onClick={() => setBoyutSonuc(null)} className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 text-sm rounded">Tamam</button>
            </div>
          </div>
        </div>
      )}
      {topluSilOnay && (
        <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={() => setTopluSilOnay(false)}>
          <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
            <h3 className="text-base font-semibold text-red-700 dark:text-red-300 mb-2">⚠ Toplu Silme</h3>
            <p className="text-sm text-slate-700 dark:text-slate-300 mb-3">
              <span className="font-semibold">{seciliSet.size}</span> öğe geri dönüşsüz silinecek. Klasörler içerdiği dosyalarla birlikte silinir.
            </p>
            <ul className="text-xs font-mono text-slate-500 dark:text-slate-500 bg-slate-50 dark:bg-slate-900 rounded p-2 max-h-40 overflow-auto mb-4">
              {Array.from(seciliSet).slice(0, 8).map(y => <li key={y} className="truncate">{y}</li>)}
              {seciliSet.size > 8 && <li className="text-slate-400 dark:text-slate-500 italic">+ {seciliSet.size - 8} daha…</li>}
            </ul>
            <div className="flex justify-end gap-2">
              <button onClick={() => setTopluSilOnay(false)} className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
              <button onClick={topluSil} className="px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white text-sm rounded font-medium">Evet, Sil</button>
            </div>
          </div>
        </div>
      )}

        </section>
      </div>
    </div>
  )
}

function formatBoyut(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function formatTarih(iso: string): string {
  try {
    return new Date(iso).toLocaleString('tr-TR', { dateStyle: 'short', timeStyle: 'short' })
  } catch {
    return iso
  }
}

// ===== helper components =====
function RenameModal({ entry, onTamam, onIptal }: { entry: Entry; onTamam: (yeniAd: string) => void; onIptal: () => void }) {
  const [ad, setAd] = useState(entry.adi)
  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={onIptal}>
      <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Yeniden Adlandır</h3>
        <p className="text-xs text-slate-500 dark:text-slate-500 mb-3"><code className="font-mono">{entry.yol}</code></p>
        <input value={ad} onChange={e => setAd(e.target.value)} autoFocus
          className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm" />
        <div className="flex justify-end gap-2 mt-4">
          <button onClick={onIptal} className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
          <button onClick={() => onTamam(ad)} disabled={!ad || ad === entry.adi}
            className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 text-sm rounded">Adlandır</button>
        </div>
      </div>
    </div>
  )
}

function ChmodModal({ entry, onTamam, onIptal }: { entry: Entry; onTamam: (mod: string) => void; onIptal: () => void }) {
  const [mod, setMod] = useState(entry.mod || '0644')
  // 9-bit checkboxes
  const n = parseInt(mod.replace(/^0/, ''), 8) || 0
  function bit(b: number) { return (n & b) !== 0 }
  function tog(b: number) {
    const yeni = (n & b) ? n & ~b : n | b
    setMod('0' + yeni.toString(8).padStart(3, '0'))
  }
  const cls = (on: boolean) => `text-xs px-2 py-1 rounded border ${on ? 'bg-emerald-50 dark:bg-emerald-900/20 border-emerald-300 text-emerald-700 dark:text-emerald-300' : 'bg-slate-50 dark:bg-slate-900 border-slate-200 dark:border-slate-700 text-slate-500 dark:text-slate-500'}`
  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={onIptal}>
      <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">İzinler</h3>
        <p className="text-xs text-slate-500 dark:text-slate-500 mb-3"><code className="font-mono">{entry.yol}</code></p>
        <div className="grid grid-cols-3 gap-2 mb-3 text-center">
          <div className="text-xs text-slate-500 dark:text-slate-500 font-semibold">Sahip</div>
          <div className="text-xs text-slate-500 dark:text-slate-500 font-semibold">Grup</div>
          <div className="text-xs text-slate-500 dark:text-slate-500 font-semibold">Diğer</div>
          {[0o400, 0o040, 0o004].map((b, i) => <button key={'r'+i} onClick={() => tog(b)} className={cls(bit(b))}>Oku</button>)}
          {[0o200, 0o020, 0o002].map((b, i) => <button key={'w'+i} onClick={() => tog(b)} className={cls(bit(b))}>Yaz</button>)}
          {[0o100, 0o010, 0o001].map((b, i) => <button key={'x'+i} onClick={() => tog(b)} className={cls(bit(b))}>Çalıştır</button>)}
        </div>
        <div className="text-xs text-slate-500 dark:text-slate-500 mb-3">Octal: <input value={mod} onChange={e => setMod(e.target.value)} className="font-mono ml-1 px-2 py-0.5 border border-slate-300 dark:border-slate-600 rounded w-20" /></div>
        <div className="flex justify-end gap-2">
          <button onClick={onIptal} className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
          <button onClick={() => onTamam(mod)} className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 text-sm rounded">Uygula</button>
        </div>
      </div>
    </div>
  )
}

function boyutBicim(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`
}

function hizBicim(bps: number): string {
  return boyutBicim(bps) + '/sn'
}

function etaBicim(sn: number): string {
  if (sn < 1) return '<1 sn'
  if (sn < 60) return `${Math.round(sn)} sn`
  if (sn < 3600) return `${Math.floor(sn / 60)} dk ${Math.round(sn % 60)} sn`
  return `${Math.floor(sn / 3600)} sa ${Math.floor((sn % 3600) / 60)} dk`
}

function KopyaTasiModal({ tip, yollar, domainId, onTamam, onIptal }:
  { tip: 'kopyala' | 'tasi'; yollar: string[]; domainId: string | number; onTamam: (hedef: string) => void; onIptal: () => void }) {
  const [hedef, setHedef] = useState('/public_html')
  const baslik = tip === 'kopyala' ? 'Kopyala' : 'Taşı'
  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={onIptal}>
      <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-lg p-5 shadow-xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3">{baslik} ({yollar.length} öğe)</h3>
        <ul className="text-xs font-mono text-slate-500 dark:text-slate-500 bg-slate-50 dark:bg-slate-900 rounded p-2 max-h-32 overflow-auto mb-4">
          {yollar.slice(0, 5).map(y => <li key={y} className="truncate">{y}</li>)}
          {yollar.length > 5 && <li className="text-slate-400 dark:text-slate-500 italic">+ {yollar.length - 5} daha…</li>}
        </ul>
        <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Hedef dizin (home altında)</label>
        <input value={hedef} onChange={e => setHedef(e.target.value)} placeholder="/public_html/yedek"
          className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm" />
        <p className="text-xs text-slate-500 dark:text-slate-500 mt-1">Hedefin var olması gerekir. {tip === 'kopyala' ? 'Klasörler içerikleriyle kopyalanır.' : 'Aynı diskte taşıma anlık.'}</p>
        <div className="flex justify-end gap-2 mt-4">
          <button onClick={onIptal} className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
          <button onClick={() => onTamam(hedef)} className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 text-sm rounded">{baslik}</button>
        </div>
      </div>
    </div>
  )
}

function ArsivModal({ adetSayi, onTamam, onIptal }: { adetSayi: number; onTamam: (ad: string, format: 'zip' | 'tar.gz') => void; onIptal: () => void }) {
  const [ad, setAd] = useState('yedek-' + new Date().toISOString().slice(0, 10))
  const [format, setFormat] = useState<'zip' | 'tar.gz'>('zip')
  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={onIptal}>
      <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3">📦 Arşive Ekle ({adetSayi} öğe)</h3>
        <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Dosya adı</label>
        <input value={ad} onChange={e => setAd(e.target.value)}
          className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm mb-3" />
        <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Format</label>
        <div className="flex gap-2">
          <button onClick={() => setFormat('zip')}
            className={`px-3 py-1.5 text-sm rounded border ${format === 'zip' ? 'bg-brand-50 dark:bg-brand-900/20 border-brand-500 text-brand-700 dark:text-brand-300' : 'border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800'}`}>
            ZIP
          </button>
          <button onClick={() => setFormat('tar.gz')}
            className={`px-3 py-1.5 text-sm rounded border ${format === 'tar.gz' ? 'bg-brand-50 dark:bg-brand-900/20 border-brand-500 text-brand-700 dark:text-brand-300' : 'border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800'}`}>
            TAR.GZ
          </button>
        </div>
        <p className="text-xs text-slate-500 dark:text-slate-500 mt-2">Çıktı: <code className="font-mono">{ad}.{format}</code></p>
        <div className="flex justify-end gap-2 mt-4">
          <button onClick={onIptal} className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
          <button onClick={() => onTamam(ad, format)} disabled={!ad}
            className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 text-sm rounded">Arşivle</button>
        </div>
      </div>
    </div>
  )
}

function YeniDosyaModal({ onTamam, onIptal }: { onTamam: (ad: string) => void; onIptal: () => void }) {
  const [ad, setAd] = useState('yeni-dosya.txt')
  return (
    <div className="fixed inset-0 z-50 bg-black/40 flex items-center justify-center p-4" onClick={onIptal}>
      <div className="bg-white dark:bg-slate-800 rounded-2xl w-full max-w-md p-5 shadow-xl" onClick={e => e.stopPropagation()}>
        <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3">📄 Yeni Dosya</h3>
        <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Dosya adı (uzantı dahil)</label>
        <input value={ad} onChange={e => setAd(e.target.value)} autoFocus
          className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded font-mono text-sm" />
        <p className="text-xs text-slate-500 dark:text-slate-500 mt-2">Boş dosya oluşturulur, ardından kod editörü açılır.</p>
        <div className="flex justify-end gap-2 mt-4">
          <button onClick={onIptal} className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 text-sm rounded">İptal</button>
          <button onClick={() => onTamam(ad)} disabled={!ad}
            className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 text-sm rounded">Oluştur ve Düzenle</button>
        </div>
      </div>
    </div>
  )
}