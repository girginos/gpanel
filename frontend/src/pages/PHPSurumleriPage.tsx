import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useCallback, useEffect, useRef, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui'

type Surum = {
  surum: string; kod: string; kaynak: 'remi' | 'appstream'
  yuklu: boolean
  eol?: boolean            // backend: guncelleme almayan (EOL) surum
  pool_dir?: string; sock_dir?: string; service?: string; php_bin?: string
  gercek_surum?: string; modul_sayi?: number; aciklama?: string
}

// Detached iş (systemd-run transient unit) — kur/kaldir arka planda koşar; sekme
// kapansa bile PID 1 altında devam eder. Durum + log poll ile izlenir.
type AktifOp = { surum: string; kaynak: string; islem: 'kur' | 'kaldir' }
type OpDurum = { calisiyor: boolean; surum?: string; kaynak?: string; islem?: 'kur' | 'kaldir'; durum?: string }
type LogYanit = { log: string; calisiyor: boolean; surum?: string; kaynak?: string; islem?: 'kur' | 'kaldir' }

// SurumSecim — sihirbazda biriktirilen "kurulacak" PHP sürümü (Özet'te toplu kurulur).
export type SurumSecim = { surum: string; kaynak: string }


const PHPSUR_EN: Record<string, string> = {
  "Anasayfa": "Home",
  "Bilgi": "Info",
  "Onay gerekiyor": "Confirmation required",
  "Emin misiniz?": "Are you sure?",
  "Filtre:": "Filter:",
  "Bu sürüm artık güvenlik güncellemesi almıyor (EOL)": "This version no longer receives security updates (EOL)",
  "Servis:": "Service:",
  "arka planda": "in the background",
  "kuruldu": "installed",
  "kuruluyor": "installing",
  "KURULUYOR": "INSTALLING",
  "KALDIRILIYOR": "REMOVING",
  "kurulumu başlatıldı…": "installation started…",
  "kaldırma başlatıldı…": "removal started…",
  "Kurulacaklara ekle": "Add to install list",
  "Kurmak için toggle'ı aç": "Turn on the toggle to install",
  "AppStream PHP sistem default'u, kaldırılamaz": "AppStream is the system default PHP, cannot be removed",
  "PHP {0} (Remi) ve TÜM ekstension'ları KALDIRILACAK.\nBu sürümü kullanan domain varsa işlem reddedilir. Devam?": "PHP {0} (Remi) and ALL its extensions will be REMOVED.\nIf a domain uses this version, the operation is rejected. Continue?",
  "Kurulum 14 paket içerir (fpm, cli, mysqlnd, mbstring, bcmath, intl, gd, soap, opcache, pdo, xml, zip, pgsql, ldap).": "The installation includes 14 packages (fpm, cli, mysqlnd, mbstring, bcmath, intl, gd, soap, opcache, pdo, xml, zip, pgsql, ldap).",
  "— bu işlem uzun sürebilir.": "— this operation may take a while.",
  "Kaldırma başlatılamadı": "Failed to start removal",
  "Kurulacak — Özet adımında": "To be installed — in the Summary step",
  "Kurulum başlatılamadı": "Failed to start installation",
  "PHP Sürümleri": "PHP Versions",
  "Seçimi kaldır": "Deselect",
  "Sistem varsayılanı (sabit)": "System default (fixed)",
  "Sistem varsayılanı, kaldırılamaz": "System default, cannot be removed",
  "Yüklü — kapatmak için toggle": "Installed — toggle to disable",
  "Zaten bir PHP işlemi sürüyor — bitmesini bekleyin.": "A PHP operation is already running — wait for it to finish.",
  "kaldırıldı": "removed",
  "kaldırılıyor": "removing",
  "sayfayı kapatabilirsiniz": "you can close the page",
  "sayfayı kapatabilirsiniz, işlem devam eder": "you can close the page, the operation continues",
  "çalışır —": "runs —",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (PHPSUR_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function PHPSurumleriPage({ gomulu, secilenSurumler, setSecilenSurumler }: {
  gomulu?: boolean
  secilenSurumler?: SurumSecim[]
  setSecilenSurumler?: (fn: (s: SurumSecim[]) => SurumSecim[]) => void
} = {}) {
  useTranslation() // dil re-render aboneligi
  const { onay, bilgi } = useDialog()
  const toast = useToast()
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
      .catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => setYuk(false))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // İlk açılış: sürüm listesi + devam eden işi yakala (resume-on-reopen).
  useEffect(() => {
    let iptal = false
    yukle()
    api.get<OpDurum>('/php-surumler/durum')
      .then(r => {
        if (iptal) return
        if (r.data.calisiyor && r.data.surum) {
          setAktifOp({ surum: r.data.surum, kaynak: r.data.kaynak || 'remi', islem: r.data.islem || 'kur' })
        }
      })
      .catch(() => { /* geçici — yut */ })
    return () => { iptal = true }
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
          const ok = `✓ PHP ${aktifOp.surum} ${aktifOp.islem === 'kaldir' ? cevir("kaldırıldı") : cevir("kuruldu")}`
          setBasari(ok)
          toast.basari(ok)
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

  // Sihirbaz (gomulu) modunda: kurulmamış sürümü "kurulacak" işaretle/kaldır.
  // Gerçek kurulum Özet adımında TOPLU yapılır (tutarlılık: eklentiler gibi).
  const surumSecili = (s: Surum) =>
    secilenSurumler?.some(x => x.surum === s.surum && x.kaynak === s.kaynak) ?? false
  const surumSecimToggle = (s: Surum) => {
    setSecilenSurumler?.(prev =>
      surumSecili(s)
        ? prev.filter(x => !(x.surum === s.surum && x.kaynak === s.kaynak))
        : [...prev, { surum: s.surum, kaynak: s.kaynak }],
    )
  }

  async function kur(s: Surum) {
    if (aktifOp) { (await bilgi({ baslik: cevir("Bilgi"), mesaj: cevir("Zaten bir PHP işlemi sürüyor — bitmesini bekleyin.") })); return }
    if (!(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevirT(cevir("PHP {0} ({1}) için 14 paket kurulacak (fpm + cli + mysqlnd + 12 ekstension). Devam?"), s.surum, s.kaynak) }))) return
    setHata(null); setBasari(null); setOpLog('')
    try {
      await api.post('/php-surumler/kur', { surum: s.surum, kaynak: s.kaynak })
      setOpLog(`PHP ${s.surum} ${cevir("kurulumu başlatıldı…")}\n`)
      setAktifOp({ surum: s.surum, kaynak: s.kaynak, islem: 'kur' })
    } catch (e) {
      const m = apiHata(e, cevir("Kurulum başlatılamadı"))
      setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    }
  }

  async function kaldir(s: Surum) {
    if (s.kaynak === 'appstream') {
      (await bilgi({ baslik: cevir("Bilgi"), mesaj: cevir("AppStream PHP sistem default'u, kaldırılamaz") }))
      return
    }
    if (aktifOp) { (await bilgi({ baslik: cevir("Bilgi"), mesaj: cevir("Zaten bir PHP işlemi sürüyor — bitmesini bekleyin.") })); return }
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevirT(cevir("PHP {0} (Remi) ve TÜM ekstension'ları KALDIRILACAK.\nBu sürümü kullanan domain varsa işlem reddedilir. Devam?"), s.surum), tehlike: true }))) return
    setHata(null); setBasari(null); setOpLog('')
    try {
      await api.post('/php-surumler/kaldir', { surum: s.surum, kaynak: s.kaynak })
      setOpLog(`PHP ${s.surum} ${cevir("kaldırma başlatıldı…")}\n`)
      setAktifOp({ surum: s.surum, kaynak: s.kaynak, islem: 'kaldir' })
    } catch (e) {
      const m = apiHata(e, cevir("Kaldırma başlatılamadı"))
      setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    }
  }

  const filtreli = surumler.filter(s => {
    if (filtre === 'yuklu') return s.yuklu
    if (filtre === 'yuklenebilir') return !s.yuklu
    return true
  })
  const yukluSayi = surumler.filter(s => s.yuklu).length

  return (
    <div className={gomulu ? '' : 'px-4 py-4 sm:px-6 sm:py-5'}>
      {!gomulu && (
        <>
          <Breadcrumb items={[
            { etiket: cevir("Anasayfa"), href: '/' },
            { etiket: cevir("Araçlar ve Ayarlar"), href: '/araclar-ayarlar' },
            { etiket: cevir("PHP Sürümleri") },
          ]} />

          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("PHP Sürümleri")}</h1>
          <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
            {cevir(cevir("Sunucuya istediğiniz PHP sürümünü ekleyin veya kaldırın. Her sürüm bağımsız PHP-FPM havuzunda çalışır; domain bazında seçilebilir."))}
            {cevir("Kurulum 14 paket içerir (fpm, cli, mysqlnd, mbstring, bcmath, intl, gd, soap, opcache, pdo, xml, zip, pgsql, ldap).")}
            {cevir(cevir("Kurulum/kaldırma"))} <strong>{cevir("arka planda")}</strong> {cevir("çalışır —")} <strong>{cevir("sayfayı kapatabilirsiniz, işlem devam eder")}</strong>.
          </p>
        </>
      )}

      {/* Aktif iş bandı — canlı ilerleme + cevir("sayfayı kapatabilirsiniz") güvencesi */}
      {aktifOp && (
        <div className="mb-4 p-4 border rounded-lg bg-sky-50 dark:bg-sky-900/15 border-sky-200 dark:border-sky-800/50">
          <div className="inline-flex items-center gap-2 text-sm font-medium text-sky-700 dark:text-sky-300">
            <span className="w-3 h-3 rounded-full border-2 border-sky-500 border-t-transparent animate-spin" />
            PHP {aktifOp.surum} {aktifOp.islem === 'kaldir' ? cevir("kaldırılıyor") : cevir("kuruluyor")} {cevir("— bu işlem uzun sürebilir.")}
          </div>
          <div className="text-xs text-sky-700/80 dark:text-sky-300/80 mt-0.5">
            {cevir(cevir("İş arka planda (ayrı sistem servisi) çalışır. Sayfayı kapatabilirsiniz — işlem devam eder, tekrar açtığınızda ilerleme kaldığı yerden görünür."))}
          </div>
          {opLog && (
            <pre ref={logRef} className="mt-2 text-[11px] font-mono bg-dark-800 text-slate-300 rounded-lg p-2.5 max-h-72 overflow-auto whitespace-pre-wrap leading-relaxed">{opLog}</pre>
          )}
        </div>
      )}

      {/* Filtre */}
      <div className="flex items-center gap-2 mb-4">
        <span className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500 mr-2">{cevir("Filtre:")}</span>
        {(['tumu', 'yuklu', 'yuklenebilir'] as const).map(f => (
          <Button key={f} onClick={() => setFiltre(f)}
            color={filtre === f ? 'primary' : 'neutral'} variant={filtre === f ? 'filled' : 'outlined'}
            className="px-3 py-1 text-sm">
            {f === 'tumu' ? cevir("Tümü") : f === 'yuklu' ? cevirT(cevir("Yüklü ({0})"), yukluSayi) : cevirT(cevir("Yüklenebilir ({0})"), surumler.length - yukluSayi)}
          </Button>
        ))}
      </div>

      {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div> : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
          {filtreli.map(s => {
            const key = s.surum + ':' + s.kaynak
            const buOp = aktifOp?.surum === s.surum && aktifOp?.kaynak === s.kaynak
            const meşgul = aktifOp !== null // tek-iş: her işlemde tüm butonlar kilitlenir
            return (
              <div key={key}
                className={`border rounded-lg p-4 transition ${buOp ? 'border-sky-300 dark:border-sky-700 bg-sky-50 dark:bg-sky-900/20 ring-1 ring-sky-300 dark:ring-sky-700' : s.yuklu ? 'border-emerald-200 dark:border-emerald-800 bg-emerald-50 dark:bg-emerald-900/20' : 'border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-700'}`}>
                <div className="flex items-start justify-between mb-2 gap-2">
                  <div className="min-w-0">
                    <div className="text-lg font-mono font-bold text-slate-900 dark:text-slate-100">PHP {s.surum}</div>
                    <div className="flex items-center gap-1.5 mt-0.5 flex-wrap">
                      <span className={`text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium ${
                        s.kaynak === 'appstream'
                          ? 'bg-sky-100 text-sky-700'
                          : 'bg-violet-100 dark:bg-violet-900/30 text-violet-700 dark:text-violet-300'
                      }`}>{s.kaynak}</span>
                      {s.eol && <span title={cevir("Bu sürüm artık güvenlik güncellemesi almıyor (EOL)")} className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300">EOL</span>}
                      {buOp && <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium bg-sky-100 dark:bg-sky-900/30 text-sky-700 dark:text-sky-300">{aktifOp?.islem === 'kaldir' ? cevir("KALDIRILIYOR") : cevir("KURULUYOR")}</span>}
                    </div>
                  </div>
                  {/* Toggle: açık=yüklü VEYA "kurulacak" (sihirbazda Özet'te toplu kurulur).
                      AppStream (sistem default) sabittir; kaldırılabilir değil. */}
                  {(() => {
                    const sabit = s.kaynak === 'appstream' && s.yuklu
                    const secili = !s.yuklu && surumSecili(s)
                    const acik = s.yuklu || secili
                    const kilit = sabit || (s.yuklu && meşgul)
                    return (
                      <button
                        onClick={() => {
                          if (sabit) return
                          if (s.yuklu) { if (!meşgul) kaldir(s); return }
                          if (setSecilenSurumler) surumSecimToggle(s); else if (!meşgul) kur(s)
                        }}
                        disabled={kilit}
                        title={sabit ? cevir("Sistem varsayılanı, kaldırılamaz") : s.yuklu ? cevir("Kaldır") : (secili ? cevir("Seçimi kaldır") : cevir("Kurulacaklara ekle"))}
                        className={`shrink-0 relative inline-flex h-6 w-11 items-center rounded-full transition ${
                          buOp ? 'bg-sky-400 animate-pulse' : s.yuklu ? 'bg-emerald-500' : secili ? 'bg-brand-500' : 'bg-slate-300 dark:bg-slate-600'
                        } ${kilit ? 'opacity-60 cursor-not-allowed' : ''}`}
                      >
                        <span className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition ${acik ? 'translate-x-6' : 'translate-x-1'}`} />
                      </button>
                    )
                  })()}
                </div>

                {s.aciklama && <div className="text-xs text-slate-500 dark:text-slate-500 mb-2">{s.aciklama}</div>}

                {s.yuklu && (
                  <div className="text-xs text-slate-600 dark:text-slate-400 dark:text-slate-500 space-y-0.5 mb-3 font-mono bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded p-2">
                    {s.gercek_surum && <div>{cevir(cevir("Sürüm:"))} <span className="text-slate-900 dark:text-slate-100">{s.gercek_surum}</span></div>}
                    {s.modul_sayi !== undefined && <div>{cevir(cevir("Modül:"))} <span className="text-slate-900 dark:text-slate-100">{s.modul_sayi}</span></div>}
                    {s.service && <div className="truncate">{cevir("Servis:")} <span className="text-slate-700 dark:text-slate-300">{s.service}</span></div>}
                  </div>
                )}

                <div className="text-xs text-slate-500 dark:text-slate-500">
                  {s.yuklu ? (s.kaynak === 'appstream' ? cevir("Sistem varsayılanı (sabit)") : cevir("Yüklü — kapatmak için toggle"))
                    : buOp ? cevir("İşleniyor…") : surumSecili(s) ? <span className="text-brand-600 dark:text-brand-400">{cevir("Kurulacak — Özet adımında")}</span> : cevir("Kurmak için toggle'ı aç")}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
