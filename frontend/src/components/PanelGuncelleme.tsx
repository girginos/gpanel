import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'

// Panel içi güncelleme kartı.
// Güncelleme sırasında panel servisi yeniden başlar → API kısa süre kesilir.
// Bu yüzden poll hataları YUTULUR (bağlantı kopması = normal), log dosyası
// sunucuda kaldığı için servis dönünce kaldığı yerden okunur.

type Durum = { arac_var: boolean; calisiyor: boolean; durum: string }
type LogYanit = { log: string; calisiyor: boolean; durum: string }

export default function PanelGuncelleme() {
  const [durum, setDurum] = useState<Durum | null>(null)
  const [log, setLog] = useState('')
  const [calisiyor, setCalisiyor] = useState(false)
  const [baslatiliyor, setBaslatiliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [onay, setOnay] = useState(false)
  const logRef = useRef<HTMLPreElement>(null)

  const durumYukle = useCallback(async () => {
    try {
      const r = await api.get<Durum>('/system/guncelleme')
      setDurum(r.data)
      setCalisiyor(r.data.calisiyor)
    } catch { /* panel restart olabilir — yut */ }
  }, [])

  useEffect(() => { durumYukle() }, [durumYukle])

  // çalışırken log poll — panel restart'ında hata yutulur, dönünce devam eder
  useEffect(() => {
    if (!calisiyor) return
    let dur = false
    const tik = async () => {
      try {
        const r = await api.get<LogYanit>('/system/guncelleme/log')
        if (dur) return
        setLog(r.data.log)
        if (!r.data.calisiyor) { setCalisiyor(false); durumYukle() }
      } catch { /* servis yeniden başlıyor — normal, poll'e devam */ }
    }
    const id = window.setInterval(tik, 2000)
    tik()
    return () => { dur = true; window.clearInterval(id) }
  }, [calisiyor, durumYukle])

  useEffect(() => { logRef.current?.scrollTo({ top: logRef.current.scrollHeight }) }, [log])

  async function baslat() {
    setHata(null); setBaslatiliyor(true); setOnay(false)
    try {
      await api.post('/system/guncelleme/baslat')
      setLog('Güncelleme başlatıldı…\n')
      setCalisiyor(true)
    } catch (e: any) {
      setHata(e?.response?.data?.hata || e?.message || 'güncelleme başlatılamadı')
    } finally { setBaslatiliyor(false) }
  }

  return (
    <div className="mb-6 p-4 border rounded-2xl bg-emerald-50 dark:bg-emerald-900/15 border-emerald-200 dark:border-emerald-800/50">
      <div className="flex items-start gap-3">
        <div className="w-10 h-10 rounded-lg flex items-center justify-center text-xl flex-shrink-0 bg-emerald-100 dark:bg-emerald-900/40">⬆️</div>
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">Panel Güncellemesi</span>
            <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300">Sunucu</span>
          </div>
          <div className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
            Paneli GitHub'daki son sürüme günceller. Veriler (env, veritabanı, siteler) korunur; yeni sürüm sağlıklı başlamazsa otomatik geri alınır.
            {durum && !durum.arac_var && ' Güncelleme aracı sunucuda yok — ilk çalıştırmada otomatik indirilecek.'}
          </div>

          {hata && <div className="mt-2 px-3 py-2 rounded-lg bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 text-xs">{hata}</div>}

          {calisiyor && (
            <div className="mt-2 inline-flex items-center gap-2 text-xs text-emerald-700 dark:text-emerald-300">
              <span className="w-3 h-3 rounded-full border-2 border-emerald-500 border-t-transparent animate-spin" />
              Güncelleme çalışıyor — panel kısa süre yeniden başlayabilir, sayfayı kapatmayın.
            </div>
          )}

          {log && (
            <pre ref={logRef} className="mt-2 text-[11px] font-mono bg-slate-900 text-slate-300 rounded-lg p-2.5 max-h-56 overflow-auto whitespace-pre-wrap leading-relaxed">{log}</pre>
          )}

          <div className="mt-3 flex items-center gap-2">
            {!onay ? (
              <button onClick={() => setOnay(true)} disabled={calisiyor || baslatiliyor}
                className="text-xs px-3 py-1.5 rounded-lg bg-slate-900 dark:bg-white text-white dark:text-slate-900 hover:opacity-90 transition font-medium disabled:opacity-40 disabled:cursor-not-allowed">
                Güncellemeleri denetle ve kur
              </button>
            ) : (
              <>
                <span className="text-xs text-slate-600 dark:text-slate-300">Panel güncellenecek ve servis yeniden başlayacak. Onaylıyor musunuz?</span>
                <button onClick={baslat} disabled={baslatiliyor}
                  className="text-xs px-3 py-1.5 rounded-lg bg-emerald-600 text-white hover:bg-emerald-700 transition font-medium disabled:opacity-40">
                  {baslatiliyor ? 'Başlatılıyor…' : 'Evet, güncelle'}
                </button>
                <button onClick={() => setOnay(false)} className="text-xs px-3 py-1.5 rounded-lg border border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800 transition">
                  Vazgeç
                </button>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
