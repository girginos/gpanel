import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { Job, DurumIkon, IslemRozet, TurRozet, fmtByte } from '@/pages/BackupYonetimiPage'

type Kalem = { backup_id: number; domain_id: number; alan_adi: string; sistem_kullanici: string; boyut_b: number; tip: string }
type Sonuc = { domain_id: number; alan_adi: string; durum: string; mesaj: string }
type Detay = { is: Job; domainler?: Kalem[]; sonuclar?: Sonuc[] }


const BJOB_EN: Record<string, string> = {
  "Backup Yöneticisi": "Backup Manager",
  "Başlangıç:": "Start:",
  "Bitiş:": "End:",
  "Boş": "Empty",
  "Geri Yükleme": "Restore",
  "Geri Yükleme Sonuçları": "Restore Results",
  "Geri yükleme başlatılamadı": "Failed to start restore",
  "Geri yükleniyor": "Restoring",
  "Görünenlerin tümünü kaldır": "Deselect all visible",
  "Görünenlerin tümünü seç": "Select all visible",
  "Seçilen hiçbir nesne yok": "No objects selected",
  "Seçili": "Selected",
  "Sonuç yok.": "No results.",
  "Yalnız Dosyalar": "Files Only",
  "Yalnız SQL": "SQL Only",
  "detaylı": "detailed",
  "← İş listesi": "← Job list",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (BJOB_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function BackupJobDetayPage() {
  useTranslation() // dil re-render aboneligi
  const { jid } = useParams()
  const nav = useNavigate()
  const [d, setD] = useState<Detay | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  // Varsayılan: HİÇBİRİ seçili değil — tüm domainler solda cevir("Boş") panelinde başlar,
  // yalnız kullanıcının sağa taşıdıkları geri yüklenir. Poll (setD) seçime DOKUNMAZ.
  const [secili, setSecili] = useState<Set<number>>(new Set())
  const [mod, setMod] = useState<'tam' | 'dosyalar' | 'veritabani'>('tam')
  const [temiz, setTemiz] = useState(false)
  const [gonderiliyor, setGonderiliyor] = useState(false)
  const timer = useRef<number | null>(null)

  function yukle() {
    if (!jid) return
    api.get<Detay>(`/admin/backups/jobs/${jid}`).then(r => setD(r.data)).catch(e => setHata(apiHata(e)))
  }
  useEffect(() => {
    yukle()
    timer.current = window.setInterval(yukle, 2500)
    return () => { if (timer.current) window.clearInterval(timer.current) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [jid])

  const j = d?.is
  const kalemler = d?.domainler || []

  async function geriYukle() {
    if (secili.size === 0) { setHata(cevir("Sağdaki “Seçili” listesine en az bir domain taşıyın")); return }
    const items = kalemler.filter(k => secili.has(k.domain_id)).map(k => ({ domain_id: k.domain_id, backup_id: k.backup_id }))
    setGonderiliyor(true); setHata(null)
    try {
      const { data } = await api.post('/admin/backups/restore', { mod, temiz, items })
      nav(`/backup-yonetimi/is/${data.job_id}`)
    } catch (e) { setHata(apiHata(e, cevir("Geri yükleme başlatılamadı"))) }
    finally { setGonderiliyor(false) }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: cevir("Araçlar ve Ayarlar"), href: '/araclar-ayarlar' },
        { etiket: cevir("Backup Yöneticisi"), href: '/backup-yonetimi' },
        { etiket: cevirT(cevir("İş #{0}"), jid) },
      ]} />

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {!j ? (
        <div className="text-sm text-slate-400 py-8">{cevir("Yükleniyor…")}</div>
      ) : (
        <>
          <div className="rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60 p-5 mb-4">
            <div className="flex items-center gap-3 flex-wrap">
              <DurumIkon durum={j.durum} />
              <h1 className="text-lg font-semibold text-slate-900 dark:text-slate-100">İş #{j.id}</h1>
              <IslemRozet islem={j.islem} mod={j.mod} />
              <TurRozet tur={j.tur} />
              <Link to="/backup-yonetimi" className="ml-auto text-sm text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200">{cevir("← İş listesi")}</Link>
            </div>
            <div className="mt-2 flex flex-wrap gap-x-5 gap-y-1 text-xs text-slate-500 dark:text-slate-400">
              <span>{j.tamamlanan}/{j.toplam} tamamlandı{j.durum === 'calisiyor' && j.aktif_domain ? cevirT(cevir(" · şu an: {0}"), j.aktif_domain) : ''}</span>
              <span>{cevir("Başlangıç:")} <span className="font-mono">{j.baslangic}</span></span>
              {j.bitis && <span>{cevir("Bitiş:")} <span className="font-mono">{j.bitis}</span></span>}
              {j.boyut_b > 0 && <span>Boyut: <span className="font-mono">{fmtByte(j.boyut_b)}</span></span>}
              {j.basari > 0 && <span className="text-emerald-600 dark:text-emerald-400">✓ {j.basari} başarılı</span>}
              {j.hata > 0 && <span className="text-red-600 dark:text-red-400">✕ {j.hata} hata</span>}
              {j.baslatan && <span>Başlatan: {j.baslatan}</span>}
            </div>
            {/* Çalışırken canlı ilerleme çubuğu + bildirim (bitince gizlenir). */}
            {j.durum === 'calisiyor' && (
              <div className="mt-3">
                <div className="flex items-center justify-between text-xs text-amber-600 dark:text-amber-400 mb-1">
                  <span className="inline-flex items-center gap-1.5">
                    <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
                    {j.islem === 'geri' ? 'Geri yükleniyor' : 'Yedekleniyor'}…{j.aktif_domain ? ` · ${j.aktif_domain}` : ''}
                  </span>
                  <span>{j.toplam ? Math.round((j.tamamlanan / j.toplam) * 100) : 0}%</span>
                </div>
                <div className="h-2 rounded-full bg-slate-100 dark:bg-slate-700 overflow-hidden">
                  <div className="h-full rounded-full bg-amber-400 animate-pulse transition-all duration-500"
                    style={{ width: `${j.toplam ? Math.round((j.tamamlanan / j.toplam) * 100) : 0}%` }} />
                </div>
              </div>
            )}
          </div>

          {j.islem === 'geri' ? (
            <div className="rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60 overflow-hidden">
              <div className="px-4 py-3 border-b border-slate-100 dark:border-slate-700/60 text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Geri Yükleme Sonuçları")}</div>
              <div className="divide-y divide-slate-100 dark:divide-slate-700/60">
                {(d?.sonuclar || []).map(s => (
                  <div key={s.domain_id} className="flex items-center gap-3 px-4 py-2.5 text-sm">
                    <DurumIkon durum={s.durum} kucuk />
                    <span className="text-slate-800 dark:text-slate-200">{s.alan_adi}</span>
                    <span className="ml-auto text-xs text-slate-400 truncate max-w-[50%]">{s.mesaj}</span>
                  </div>
                ))}
                {(!d?.sonuclar || d.sonuclar.length === 0) && <div className="px-4 py-6 text-center text-sm text-slate-400">{j.durum === 'calisiyor' ? 'İşleniyor…' : cevir("Sonuç yok.")}</div>}
              </div>
            </div>
          ) : (
            <div className="rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60 p-4">
              <div className="flex flex-wrap items-center gap-2 mb-3">
                <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Geri Yükleme")}</h3>
                <span className="text-xs font-semibold text-slate-500 ml-2">mod:</span>
                {([['tam', 'Tam'], ['dosyalar', cevir("Yalnız Dosyalar")], ['veritabani', cevir("Yalnız SQL")]] as const).map(([v, et]) => (
                  <button key={v} onClick={() => setMod(v)} className={`text-xs px-2.5 py-1 rounded-full border ${mod === v ? 'border-slate-900 dark:border-slate-100 bg-slate-900 dark:bg-slate-100 text-white dark:text-slate-900' : 'border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300'}`}>{et}</button>
                ))}
                {(mod === 'tam' || mod === 'dosyalar') && (
                  <label className="flex items-center gap-1 text-[11px] text-amber-700 dark:text-amber-300 ml-1">
                    <input type="checkbox" checked={temiz} onChange={e => setTemiz(e.target.checked)} /> temiz (ezerek)
                  </label>
                )}
                <button onClick={geriYukle} disabled={gonderiliyor || secili.size === 0}
                  className="ml-auto text-xs px-3 py-1.5 rounded-lg bg-slate-900 dark:bg-slate-100 text-white dark:text-slate-900 font-medium disabled:opacity-50">
                  {gonderiliyor ? 'Başlatılıyor…' : cevirT(cevir("Seçilenleri Geri Yükle ({0})"), secili.size)}
                </button>
              </div>
              <TransferListe items={kalemler} secili={secili} onDegis={setSecili} />
              <p className="text-[11px] text-slate-400 mt-2">Domainleri soldan sağa taşıyın (tıklayın ya da başlıktaki kutuyla tümünü taşıyın). Yalnız <b>{cevir("Seçili")}</b> listesindekiler geri yüklenir. Dosya/DB bazlı hassas geri yükleme için “detaylı”.</p>
            </div>
          )}
        </>
      )}
    </div>
  )
}

// TransferListe: Plesk tarzı çift-liste aktarım (Boş ⇄ Seçili). Tıkla veya başlık kutusuyla toplu taşı.
function TransferListe({ items, secili, onDegis }: {
  items: Kalem[]; secili: Set<number>; onDegis: (s: Set<number>) => void
}) {
  const [araBos, setAraBos] = useState('')
  const [araSec, setAraSec] = useState('')
  const bos = items.filter(k => !secili.has(k.domain_id))
  const sec = items.filter(k => secili.has(k.domain_id))
  const f = (l: Kalem[], q: string) => { const t = q.trim().toLowerCase(); return t ? l.filter(k => k.alan_adi.toLowerCase().includes(t)) : l }
  const bosF = f(bos, araBos)
  const secF = f(sec, araSec)

  const tasi = (id: number, ekle: boolean) => { const n = new Set(secili); ekle ? n.add(id) : n.delete(id); onDegis(n) }
  const hepsiSaga = () => { const n = new Set(secili); bosF.forEach(k => n.add(k.domain_id)); onDegis(n) }
  const hepsiSola = () => { const n = new Set(secili); secF.forEach(k => n.delete(k.domain_id)); onDegis(n) }

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
      <Sutun baslik={cevir("Boş")} adet={bos.length} items={bosF} ara={araBos} setAra={setAraBos}
        yon="sag" onTasi={id => tasi(id, true)} onHepsi={hepsiSaga} bosMetin="Nesne yok" />
      <Sutun baslik={cevir("Seçili")} adet={sec.length} items={secF} ara={araSec} setAra={setAraSec}
        yon="sol" onTasi={id => tasi(id, false)} onHepsi={hepsiSola} bosMetin={cevir("Seçilen hiçbir nesne yok")} />
    </div>
  )
}

function Sutun({ baslik, adet, items, ara, setAra, yon, onTasi, onHepsi, bosMetin }: {
  baslik: string; adet: number; items: Kalem[]; ara: string; setAra: (v: string) => void
  yon: 'sag' | 'sol'; onTasi: (id: number) => void; onHepsi: () => void; bosMetin: string
}) {
  return (
    <div className="rounded-xl border border-slate-200 dark:border-slate-700 overflow-hidden flex flex-col">
      <div className="px-3 py-2 border-b border-slate-100 dark:border-slate-700/60 bg-slate-50 dark:bg-slate-800/50 flex items-center gap-2">
        <span className="text-xs font-semibold text-slate-600 dark:text-slate-300">{baslik}</span>
        <span className="text-[11px] text-slate-400">({adet})</span>
      </div>
      <div className="px-2 py-2 border-b border-slate-100 dark:border-slate-700/60 flex items-center gap-2">
        <input type="checkbox" checked={false} onChange={onHepsi} disabled={items.length === 0}
          title={yon === 'sag' ? 'Görünenlerin tümünü seç' : cevir("Görünenlerin tümünü kaldır")}
          className="shrink-0 disabled:opacity-40" />
        <div className="relative flex-1">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="w-3.5 h-3.5 absolute left-2 top-1/2 -translate-y-1/2 text-slate-400 pointer-events-none">
            <circle cx="11" cy="11" r="7" /><path d="m21 21-4.3-4.3" strokeLinecap="round" />
          </svg>
          <input value={ara} onChange={e => setAra(e.target.value)} placeholder={cevir("Ara…")}
            className="w-full rounded-lg border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 pl-7 pr-2 py-1.5 text-sm" />
        </div>
      </div>
      <div className="max-h-72 min-h-[8rem] overflow-y-auto divide-y divide-slate-100 dark:divide-slate-800 bg-white dark:bg-slate-800/40">
        {items.length === 0 && <div className="p-6 text-center text-xs text-slate-400">{bosMetin}</div>}
        {items.map(k => (
          <button key={k.domain_id} onClick={() => onTasi(k.domain_id)}
            className="w-full flex items-center gap-2 px-3 py-2 text-sm text-left hover:bg-brand-50 dark:hover:bg-brand-900/20 group">
            {yon === 'sol' && <span className="text-slate-300 dark:text-slate-600 group-hover:text-brand-500">‹</span>}
            <span className="text-slate-700 dark:text-slate-300 truncate">{k.alan_adi}</span>
            <span className="ml-auto font-mono text-[11px] text-slate-400">{fmtByte(k.boyut_b)}</span>
            <Link to={`/abonelikler/${k.domain_id}/yedekler`} onClick={e => e.stopPropagation()} className="text-[11px] text-brand-600 dark:text-brand-400 hover:underline shrink-0">{cevir("detaylı")}</Link>
            {yon === 'sag' && <span className="text-slate-300 dark:text-slate-600 group-hover:text-brand-500">›</span>}
          </button>
        ))}
      </div>
    </div>
  )
}
