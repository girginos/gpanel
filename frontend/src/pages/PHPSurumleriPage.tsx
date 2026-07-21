// gosp-dark-swept
// gosp-dark-swept-v2
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Surum = {
  surum: string; kod: string; kaynak: 'remi' | 'appstream'
  yuklu: boolean
  pool_dir?: string; sock_dir?: string; service?: string; php_bin?: string
  gercek_surum?: string; modul_sayi?: number; aciklama?: string
}

// Detached iş (systemd-run transient unit) — kur/kaldir arka planda koşar; sekme
// kapansa bile PID 1 altında devam eder. Durum + log poll ile izlenir.
type AktifOp = { surum: string; kaynak: string; islem: 'kur' | 'kaldir' }
type OpDurum = { calisiyor: boolean; surum?: string; kaynak?: string; islem?: 'kur' | 'kaldir'; durum?: string }
type LogYanit = { log: string; calisiyor: boolean; surum?: string; kaynak?: string; islem?: 'kur' | 'kaldir' }

export default function PHPSurumleriPage() {
  const [surumler, setSurumler] = useState<Surum[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [aktifOp, setAktifOp] = useState<AktifOp | null>(null)
  const [opLog, setOpLog] = useState('')
  const [filtre, setFiltre] = useState<'tumu' | 'yuklu' | 'yuklenebilir'>('tumu')
  const logRef = useRef<HTMLPreElement>(null)

  const yukle = useCallback(() => {
    setYuk(true)
    api.get<{ surumler: Surum[] }>('/php-surumler')
      .then(r => setSurumler(r.data.surumler || []))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }, [])

  // İlk açılış: sürüm listesi + devam eden işi yakala (resume-on-reopen).
  useEffect(() => {
    yukle()
    api.get<OpDurum>('/php-surumler/durum')
      .then(r => {
        if (r.data.calisiyor && r.data.surum) {
          setAktifOp({ surum: r.data.surum, kaynak: r.data.kaynak || 'remi', islem: r.data.islem || 'kur' })
        }
      })
      .catch(() => { /* geçici — yut */ })
  }, [yukle])

  // Aktif işi 2sn'de bir poll et — log akar, bitince listeyi tazele.
  useEffect(() => {
    if (!aktifOp) return
    let dur = false
    const tik = async () => {
      try {
        const r = await api.get<LogYanit>('/php-surumler/log')
        if (dur) return
        setOpLog(r.data.log || '')
        if (!r.data.calisiyor) {
          setBasari(`✓ PHP ${aktifOp.surum} ${aktifOp.islem === 'kaldir' ? 'kaldırıldı' : 'kuruldu'}`)
          setTimeout(() => setBasari(null), 6000)
          setAktifOp(null)
          yukle()
        }
      } catch { /* geçici bağlantı hatası — poll'e devam */ }
    }
    const id = window.setInterval(tik, 2000)
    tik()
    return () => { dur = true; window.clearInterval(id) }
  }, [aktifOp, yukle])

  useEffect(() => { logRef.current?.scrollTo({ top: logRef.current.scrollHeight }) }, [opLog])

  async function kur(s: Surum) {
    if (aktifOp) { alert('Zaten bir PHP işlemi sürüyor — bitmesini bekleyin.'); return }
    if (!confirm(`PHP ${s.surum} (${s.kaynak}) için 14 paket kurulacak (fpm + cli + mysqlnd + 12 ekstension). Devam?`)) return
    setHata(null); setBasari(null); setOpLog('')
    try {
      await api.post('/php-surumler/kur', { surum: s.surum, kaynak: s.kaynak })
      setOpLog(`PHP ${s.surum} kurulumu başlatıldı…\n`)
      setAktifOp({ surum: s.surum, kaynak: s.kaynak, islem: 'kur' })
    } catch (e) { setHata(apiHata(e, 'Kurulum başlatılamadı')) }
  }

  async function kaldir(s: Surum) {
    if (s.kaynak === 'appstream') {
      alert('AppStream PHP sistem default\'u, kaldırılamaz')
      return
    }
    if (aktifOp) { alert('Zaten bir PHP işlemi sürüyor — bitmesini bekleyin.'); return }
    if (!confirm(`PHP ${s.surum} (Remi) ve TÜM ekstension'ları KALDIRILACAK.\nBu sürümü kullanan domain varsa işlem reddedilir. Devam?`)) return
    setHata(null); setBasari(null); setOpLog('')
    try {
      await api.post('/php-surumler/kaldir', { surum: s.surum, kaynak: s.kaynak })
      setOpLog(`PHP ${s.surum} kaldırma başlatıldı…\n`)
      setAktifOp({ surum: s.surum, kaynak: s.kaynak, islem: 'kaldir' })
    } catch (e) { setHata(apiHata(e, 'Kaldırma başlatılamadı')) }
  }

  const filtreli = surumler.filter(s => {
    if (filtre === 'yuklu') return s.yuklu
    if (filtre === 'yuklenebilir') return !s.yuklu
    return true
  })
  const yukluSayi = surumler.filter(s => s.yuklu).length

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar', href: '/araclar-ayarlar' },
        { etiket: 'PHP Sürümleri' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">PHP Sürümleri</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        Sunucuya istediğiniz PHP sürümünü ekleyin veya kaldırın. Her sürüm bağımsız PHP-FPM havuzunda çalışır; domain bazında seçilebilir.
        Kurulum 14 paket içerir (fpm, cli, mysqlnd, mbstring, bcmath, intl, gd, soap, opcache, pdo, xml, zip, pgsql, ldap).
        Kurulum/kaldırma <strong>arka planda</strong> çalışır — <strong>sayfayı kapatabilirsiniz, işlem devam eder</strong>.
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {/* Aktif iş bandı — canlı ilerleme + "sayfayı kapatabilirsiniz" güvencesi */}
      {aktifOp && (
        <div className="mb-4 p-4 border rounded-2xl bg-sky-50 dark:bg-sky-900/15 border-sky-200 dark:border-sky-800/50">
          <div className="inline-flex items-center gap-2 text-sm font-medium text-sky-700 dark:text-sky-300">
            <span className="w-3 h-3 rounded-full border-2 border-sky-500 border-t-transparent animate-spin" />
            PHP {aktifOp.surum} {aktifOp.islem === 'kaldir' ? 'kaldırılıyor' : 'kuruluyor'} — bu işlem uzun sürebilir.
          </div>
          <div className="text-xs text-sky-700/80 dark:text-sky-300/80 mt-0.5">
            İş arka planda (ayrı sistem servisi) çalışır. Sayfayı kapatabilirsiniz — işlem devam eder, tekrar açtığınızda ilerleme kaldığı yerden görünür.
          </div>
          {opLog && (
            <pre ref={logRef} className="mt-2 text-[11px] font-mono bg-slate-900 text-slate-300 rounded-lg p-2.5 max-h-72 overflow-auto whitespace-pre-wrap leading-relaxed">{opLog}</pre>
          )}
        </div>
      )}

      {/* Filtre */}
      <div className="flex items-center gap-2 mb-4">
        <span className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500 mr-2">Filtre:</span>
        {(['tumu', 'yuklu', 'yuklenebilir'] as const).map(f => (
          <button key={f} onClick={() => setFiltre(f)}
            className={`px-3 py-1 text-sm rounded ${filtre === f ? 'bg-brand-600 text-white' : 'border border-slate-300 dark:border-slate-600 text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800'}`}>
            {f === 'tumu' ? 'Tümü' : f === 'yuklu' ? `Yüklü (${yukluSayi})` : `Yüklenebilir (${surumler.length - yukluSayi})`}
          </button>
        ))}
      </div>

      {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div> : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          {filtreli.map(s => {
            const key = s.surum + ':' + s.kaynak
            const buOp = aktifOp?.surum === s.surum && aktifOp?.kaynak === s.kaynak
            const meşgul = aktifOp !== null // tek-iş: her işlemde tüm butonlar kilitlenir
            return (
              <div key={key}
                className={`border rounded-2xl p-4 transition ${buOp ? 'border-sky-300 dark:border-sky-700 bg-sky-50 dark:bg-sky-900/20 ring-1 ring-sky-300 dark:ring-sky-700' : s.yuklu ? 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20' : 'border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800'}`}>
                <div className="flex items-start justify-between mb-2">
                  <div>
                    <div className="text-lg font-mono font-bold text-slate-900 dark:text-slate-100">PHP {s.surum}</div>
                    <div className="flex items-center gap-1.5 mt-0.5">
                      <span className={`text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium ${
                        s.kaynak === 'appstream'
                          ? 'bg-sky-100 text-sky-700'
                          : 'bg-violet-100 dark:bg-violet-900/30 text-violet-700 dark:text-violet-300'
                      }`}>{s.kaynak}</span>
                      {s.yuklu && <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300">YÜKLÜ</span>}
                      {parseInt(s.surum) < 8 && <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300">EOL</span>}
                      {buOp && <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium bg-sky-100 dark:bg-sky-900/30 text-sky-700 dark:text-sky-300">{aktifOp?.islem === 'kaldir' ? 'KALDIRILIYOR' : 'KURULUYOR'}</span>}
                    </div>
                  </div>
                </div>

                {s.aciklama && <div className="text-xs text-slate-500 dark:text-slate-500 mb-2">{s.aciklama}</div>}

                {s.yuklu && (
                  <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 space-y-0.5 mb-3 font-mono bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded p-2">
                    {s.gercek_surum && <div>Sürüm: <span className="text-slate-900 dark:text-slate-100">{s.gercek_surum}</span></div>}
                    {s.modul_sayi !== undefined && <div>Modül: <span className="text-slate-900 dark:text-slate-100">{s.modul_sayi}</span></div>}
                    {s.service && <div className="truncate">Servis: <span className="text-slate-700 dark:text-slate-300">{s.service}</span></div>}
                  </div>
                )}

                {s.yuklu ? (
                  s.kaynak === 'appstream' ? (
                    <button disabled className="w-full px-3 py-1.5 bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-500 text-sm rounded cursor-not-allowed">
                      Sabit (sistem default)
                    </button>
                  ) : (
                    <button onClick={() => kaldir(s)} disabled={meşgul}
                      className="w-full px-3 py-1.5 bg-red-600 hover:bg-red-700 disabled:bg-slate-300 disabled:cursor-not-allowed text-white text-sm rounded">
                      {buOp && aktifOp?.islem === 'kaldir' ? '⏳ Kaldırılıyor…' : '🗑 Kaldır'}
                    </button>
                  )
                ) : (
                  <button onClick={() => kur(s)} disabled={meşgul}
                    className="w-full px-3 py-1.5 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 disabled:cursor-not-allowed text-sm font-medium rounded">
                    {buOp && aktifOp?.islem === 'kur' ? '⏳ Kuruluyor…' : '⬇ Kur'}
                  </button>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
