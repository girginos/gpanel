import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'

// Sunucu optimizasyonu kartı — sistem paket güncellemesi (dnf/yum -y update) +
// MariaDB/nginx/PHP performans ayarı. İş arka planda (ayrı systemd unit) koşar;
// UZUN sürebilir + servisleri kısa süre etkileyebilir. Log canlı akar.
// Panel güncellemesindeki (PanelGuncelleme) desenle aynı: poll hataları yutulur.

type Durum = { calisiyor: boolean; durum: string }
type LogYanit = { log: string; calisiyor: boolean; durum: string }

export default function SunucuOptimize() {
  const [log, setLog] = useState('')
  const [calisiyor, setCalisiyor] = useState(false)
  const [baslatiliyor, setBaslatiliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [onay, setOnay] = useState(false)
  const logRef = useRef<HTMLPreElement>(null)

  const durumYukle = useCallback(async () => {
    try {
      const r = await api.get<Durum>('/system/optimize')
      setCalisiyor(r.data.calisiyor)
    } catch { /* geçici — yut */ }
  }, [])

  useEffect(() => { durumYukle() }, [durumYukle])

  // çalışırken log poll
  useEffect(() => {
    if (!calisiyor) return
    let dur = false
    const tik = async () => {
      try {
        const r = await api.get<LogYanit>('/system/optimize/log')
        if (dur) return
        setLog(r.data.log)
        if (!r.data.calisiyor) { setCalisiyor(false); durumYukle() }
      } catch { /* geçici bağlantı hatası — poll'e devam */ }
    }
    const id = window.setInterval(tik, 2000)
    tik()
    return () => { dur = true; window.clearInterval(id) }
  }, [calisiyor, durumYukle])

  useEffect(() => { logRef.current?.scrollTo({ top: logRef.current.scrollHeight }) }, [log])

  async function baslat() {
    setHata(null); setBaslatiliyor(true); setOnay(false)
    try {
      await api.post('/system/optimize/baslat')
      setLog('Optimizasyon başlatıldı…\n')
      setCalisiyor(true)
    } catch (e: any) {
      setHata(e?.response?.data?.hata || e?.message || 'optimizasyon başlatılamadı')
    } finally { setBaslatiliyor(false) }
  }

  return (
    <div className="mb-6 p-4 border rounded-2xl bg-sky-50 dark:bg-sky-900/15 border-sky-200 dark:border-sky-800/50">
      <div className="flex items-start gap-3">
        <div className="w-10 h-10 rounded-lg flex items-center justify-center text-xl flex-shrink-0 bg-sky-100 dark:bg-sky-900/40">🚀</div>
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">Sunucuyu Optimize Et</span>
            <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium bg-sky-100 dark:bg-sky-900/40 text-sky-700 dark:text-sky-300">Bakım</span>
          </div>
          <div className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
            Sistem paketlerini günceller (<code className="text-[11px]">dnf -y update</code>) ve MariaDB / nginx / PHP ayarlarını sunucu kaynağına göre yeniden ayarlar. İş arka planda çalışır — uzun sürebilir ve servisleri kısa süre etkileyebilir.
          </div>

          {hata && <div className="mt-2 px-3 py-2 rounded-lg bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 text-xs">{hata}</div>}

          {calisiyor && (
            <div className="mt-2 inline-flex items-center gap-2 text-xs text-sky-700 dark:text-sky-300">
              <span className="w-3 h-3 rounded-full border-2 border-sky-500 border-t-transparent animate-spin" />
              Optimizasyon çalışıyor — paket güncellemesi uzun sürebilir, sayfayı kapatabilirsiniz (arka planda devam eder).
            </div>
          )}

          {log && (
            <pre ref={logRef} className="mt-2 text-[11px] font-mono bg-slate-900 text-slate-300 rounded-lg p-2.5 max-h-56 overflow-auto whitespace-pre-wrap leading-relaxed">{log}</pre>
          )}

          <div className="mt-3 flex items-center gap-2">
            {!onay ? (
              <button onClick={() => setOnay(true)} disabled={calisiyor || baslatiliyor}
                className="text-xs px-3 py-1.5 rounded-lg bg-slate-900 dark:bg-slate-700 text-white dark:text-slate-100 hover:opacity-90 transition font-medium disabled:opacity-40 disabled:cursor-not-allowed">
                Paketleri güncelle ve optimize et
              </button>
            ) : (
              <>
                <span className="text-xs text-slate-600 dark:text-slate-300">Sistem paketleri güncellenecek ve servisler yeniden ayarlanacak. Onaylıyor musunuz?</span>
                <button onClick={baslat} disabled={baslatiliyor}
                  className="text-xs px-3 py-1.5 rounded-lg bg-sky-600 text-white hover:bg-sky-700 transition font-medium disabled:opacity-40">
                  {baslatiliyor ? 'Başlatılıyor…' : 'Evet, başlat'}
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
