import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { Button } from '@/components/ui'
import { UyariKutusu } from '@/components/UyariKutusu'
import { api } from '@/lib/api'
import { hataYakala } from '@/lib/hata'

// Panel içi güncelleme kartı.
// Güncelleme sırasında panel servisi yeniden başlar → API kısa süre kesilir.
// Poll hataları YUTULUR (bağlantı kopması = normal). Bitiş, `calisiyor` bayrağına
// EK OLARAK log içeriğinden ("Güncelleme tamam" / hata) da algılanır — böylece
// restart anında akış kopsa bile UI otomatik "tamamlandı"ya geçer (sonsuz spinner yok).

type Durum = { arac_var: boolean; calisiyor: boolean; durum: string }
type LogYanit = { log: string; calisiyor: boolean; durum: string }
type SurumDurum = {
  acik: boolean; mevcut: string; son: string; guncelleme_var: boolean
  kurulu_yeni?: boolean; duyuru: string; kritik: boolean; son_kontrol?: string; hata: string
}

// Log metninden bitiş sonucunu çıkarır: 'tamam' | 'hata' | null (henüz bitmedi).

const CMP_EN: Record<string, string> = {
  "Sürüm kontrolü yapılamadı": "Version check failed",
  "Güncelleme günlüğü alınamadı": "Failed to get update log",
  "güncelleme başlatılamadı": "update could not be started",
  "Panel Güncellemesi": "Panel Update",
  "Sunucu": "Server",
  "Paneli yayınlanmış son sürüme günceller. Veriler (env, veritabanı, siteler) korunur; yeni sürüm sağlıklı başlamazsa otomatik geri alınır.": "Updates the panel to the latest published version. Data (env, database, sites) is preserved; if the new version does not start healthy it is automatically rolled back.",
  " Güncelleme aracı sunucuda yok — ilk çalıştırmada otomatik indirilecek.": " The update tool is not on the server — it will be downloaded automatically on first run.",
  "Kritik güvenlik güncellemesi": "Critical security update",
  "Yeni sürüm mevcut": "New version available",
  "kurulu": "installed",
  "Panel güncel": "Panel is up to date",
  "Sürüm kontrolü kapalı": "Version check is off",
  "Güncelleme çalışıyor — panel kısa süre yeniden başlayabilir, sayfayı kapatmayın.": "Update running — the panel may restart briefly, do not close the page.",
  "Güncelleme tamamlandı": "Update completed",
  "— panel güncel. Yeni özellikleri görmek için sayfayı yenileyin.": "— panel is up to date. Refresh the page to see the new features.",
  "Sayfayı yenile": "Refresh page",
  "Güncelleme başarısız": "Update failed",
  "— aşağıdaki günlüğü inceleyin (yeni sürüm sağlıksızsa otomatik geri alınmıştır).": "— review the log below (if the new version was unhealthy it has been rolled back automatically).",
  "Güncellemeleri denetle ve kur": "Check for updates and install",
  "Panel güncellenecek ve servis yeniden başlayacak. Onaylıyor musunuz?": "The panel will be updated and the service will restart. Do you confirm?",
  "Başlatılıyor…": "Starting…",
  "Evet, güncelle": "Yes, update",
  "Vazgeç": "Cancel",
  "Kurulu sürüm ({kurulu}) yayındaki sürümden ({yayin}) yeni — güncelleme gerekmiyor": "Installed version ({kurulu}) is newer than the published version ({yayin}) — no update needed",
  "Kurulu sürüm yayındakinden yeni; güncelleme geri dönüş kapısınca reddedilebilir. Yine de denensin mi?": "The installed version is newer than the published one; the update may be refused by the downgrade guard. Try anyway?",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CMP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// ANSI renk kaçış dizilerini (\x1b[..m) sil — terminal renkleri tarayıcıda
// çıplak '[32m' gibi görünmesin. Log salt-okunur gösterildiği için düz metin yeter.
const ansiSil = (t: string): string => t.replace(/\x1b\[[0-9;]*m/g, '')

function bitisSonucu(logMetin: string, calis: boolean, durum: string): 'tamam' | 'hata' | null {
  // 🔴 Verdict UNIT durumundan (kesin exit sinyali) alinir — log regex DEGIL.
  // Eski regex, aciklama metnindeki "auto-rollback" / "✗" kelimesini yakalayip
  // unit exit=0 (BASARILI) olsa bile YANLIS "basarisiz" veriyordu. Ozellikle panel
  // zaten guncelken "Güncelleme tamam" sentinel'i yazilmaz, sadece heal/aciklama
  // metni kalir. Simdi: durum='failed' => gercek hata; aksi halde bitti => tamam.
  if (calis) return null
  if (durum === 'failed') return 'hata'
  if (
    logMetin.includes('Güncelleme tamam') ||
    logMetin.includes('Güncelleme başlatıldı') ||
    durum === 'inactive' ||
    durum === 'dead'
  ) return 'tamam'
  return null
}

export default function PanelGuncelleme() {
  useTranslation() // dil re-render aboneligi
  const [durum, setDurum] = useState<Durum | null>(null)
  const [log, setLog] = useState('')
  const [calisiyor, setCalisiyor] = useState(false)
  const [sonuc, setSonuc] = useState<'tamam' | 'hata' | null>(null)
  const [baslatiliyor, setBaslatiliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [onay, setOnay] = useState(false)
  const logRef = useRef<HTMLPreElement>(null)
  const [surum, setSurum] = useState<SurumDurum | null>(null)

  const surumYukle = useCallback(() => {
    api.get<SurumDurum>('/system/surum-kontrol').then(r => setSurum(r.data)).catch(hataYakala(cevir("Sürüm kontrolü yapılamadı")))
  }, [])

  const durumYukle = useCallback(async () => {
    try {
      const r = await api.get<Durum>('/system/guncelleme')
      setDurum(r.data)
      setCalisiyor(r.data.calisiyor)
    } catch { /* panel restart olabilir — yut */ }
  }, [])

  // Log + bitiş değerlendirmesi (mount'ta ve poll'de kullanılır).
  const logYukle = useCallback(async () => {
    const r = await api.get<LogYanit>('/system/guncelleme/log')
    const temizLog = ansiSil(r.data.log)
    setLog(temizLog)
    const s = bitisSonucu(temizLog, r.data.calisiyor, r.data.durum)
    if (!r.data.calisiyor || s) {
      setCalisiyor(false)
      if (s) setSonuc(s)
      surumYukle()
    }
    return r.data
  }, [surumYukle])

  // Mount: durum + son log (sayfa yenilenince son sonucu da gösterir).
  useEffect(() => {
    durumYukle()
    logYukle().catch(hataYakala(cevir("Güncelleme günlüğü alınamadı")))
    surumYukle()
  }, [durumYukle, logYukle, surumYukle])

  // çalışırken log poll — restart'ta hata yutulur, dönünce devam; bitişte durur.
  useEffect(() => {
    if (!calisiyor) return
    let dur = false
    const tik = async () => {
      try {
        if (dur) return
        await logYukle()
      } catch { /* servis yeniden başlıyor — normal, poll'e devam */ }
    }
    const id = window.setInterval(tik, 2000)
    tik()
    return () => { dur = true; window.clearInterval(id) }
  }, [calisiyor, logYukle])

  useEffect(() => { logRef.current?.scrollTo({ top: logRef.current.scrollHeight }) }, [log])

  async function baslat() {
    setHata(null); setBaslatiliyor(true); setOnay(false); setSonuc(null)
    try {
      await api.post('/system/guncelleme/baslat')
      setLog('Güncelleme başlatıldı…\n')
      setCalisiyor(true)
    } catch (e: any) {
      setHata(e?.response?.data?.hata || e?.message || cevir("güncelleme başlatılamadı"))
    } finally { setBaslatiliyor(false) }
  }

  return (
    <div className="mb-6 p-4 border rounded-lg bg-emerald-50 dark:bg-emerald-900/15 border-emerald-200 dark:border-emerald-800/50">
      <div className="flex items-start gap-3">
        <div className="w-10 h-10 rounded-lg flex items-center justify-center text-xl shrink-0 bg-emerald-100 dark:bg-emerald-900/40"><Ikon d={I.yukle} className="h-5 w-5" /></div>
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Panel Güncellemesi")}</span>
            <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded font-medium bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300">{cevir("Sunucu")}</span>
          </div>
          <div className="text-xs text-slate-500 dark:text-slate-500 mt-0.5">
            {cevir("Paneli yayınlanmış son sürüme günceller. Veriler (env, veritabanı, siteler) korunur; yeni sürüm sağlıklı başlamazsa otomatik geri alınır.")}
            {durum && !durum.arac_var && cevir(" Güncelleme aracı sunucuda yok — ilk çalıştırmada otomatik indirilecek.")}
          </div>

          {surum && surum.acik && surum.guncelleme_var && (
            <UyariKutusu ton={surum.kritik ? 'tehlike' : 'uyari'} className="mt-2"
              baslik={<>{surum.kritik ? cevir("Kritik güvenlik güncellemesi") : cevir("Yeni sürüm mevcut")}: {surum.son}
                <span className="font-normal opacity-75"> ({cevir("kurulu")}: {surum.mevcut})</span></>}>
              {surum.duyuru && <span className="text-xs">{surum.duyuru}</span>}
            </UyariKutusu>
          )}
          {surum && surum.acik && surum.kurulu_yeni && (
            <div className="mt-2 flex items-center gap-1.5 text-xs text-slate-600 dark:text-slate-300"><Ikon d={I.bilgi} className="size-3.5 shrink-0" />{cevir("Kurulu sürüm ({kurulu}) yayındaki sürümden ({yayin}) yeni — güncelleme gerekmiyor").replace('{kurulu}', surum.mevcut).replace('{yayin}', surum.son)}</div>
          )}
          {surum && surum.acik && !surum.guncelleme_var && !surum.kurulu_yeni && surum.son && (
            <div className="mt-2 flex items-center gap-1.5 text-xs text-emerald-700 dark:text-emerald-300"><Ikon d={I.onay} className="size-3.5 shrink-0" />{cevir("Panel güncel")} ({surum.mevcut})</div>
          )}
          {surum && !surum.acik && (
            <div className="mt-2 text-xs text-slate-500 dark:text-slate-400">{cevir("Sürüm kontrolü kapalı")} (PANEL_SURUM_KONTROL=0)</div>
          )}

          {hata && <UyariKutusu ton="tehlike" className="mt-2" kucuk>{hata}</UyariKutusu>}

          {calisiyor && (
            <div className="mt-2 inline-flex flex-wrap items-center gap-2 text-xs text-emerald-700 dark:text-emerald-300">
              <span className="w-3 h-3 rounded-full border-2 border-emerald-500 border-t-transparent animate-spin" />
              {cevir(cevir("Güncelleme çalışıyor — panel kısa süre yeniden başlayabilir, sayfayı kapatmayın."))}
            </div>
          )}

          {!calisiyor && sonuc === 'tamam' && (
            <UyariKutusu ton="basari" className="mt-2" kucuk>
              <div className="flex flex-wrap items-center gap-2">
                <span><span className="font-semibold">{cevir("Güncelleme tamamlandı")}</span> {cevir("— panel güncel. Yeni özellikleri görmek için sayfayı yenileyin.")}</span>
                <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={() => window.location.reload()} className="ml-auto">{cevir("Sayfayı yenile")}</Button>
              </div>
            </UyariKutusu>
          )}
          {!calisiyor && sonuc === 'hata' && (
            <UyariKutusu ton="tehlike" className="mt-2" kucuk>
              <span className="font-semibold">{cevir("Güncelleme başarısız")}</span> {cevir("— aşağıdaki günlüğü inceleyin (yeni sürüm sağlıksızsa otomatik geri alınmıştır).")}
            </UyariKutusu>
          )}

          {log && (
            <pre ref={logRef} className="mt-2 text-[11px] font-mono bg-dark-800 text-slate-300 rounded-lg p-2.5 max-h-56 overflow-auto whitespace-pre-wrap leading-relaxed">{log}</pre>
          )}

          <div className="mt-3 flex flex-wrap items-center gap-2">
            {!onay ? (
              <Button color="primary" boyut="sm" ikon={I.yukle} onClick={() => setOnay(true)} disabled={calisiyor || baslatiliyor}>
                {cevir(cevir("Güncellemeleri denetle ve kur"))}
              </Button>
            ) : (
              <>
                <span className="text-xs text-slate-600 dark:text-slate-300">{surum?.kurulu_yeni ? cevir("Kurulu sürüm yayındakinden yeni; güncelleme geri dönüş kapısınca reddedilebilir. Yine de denensin mi?") : cevir("Panel güncellenecek ve servis yeniden başlayacak. Onaylıyor musunuz?")}</span>
                <Button color="primary" boyut="sm" ikon={I.onay} onClick={baslat} yukleniyor={baslatiliyor}>
                  {baslatiliyor ? cevir("Başlatılıyor…") : cevir("Evet, güncelle")}
                </Button>
                <Button variant="outlined" boyut="sm" ikon={I.kapat} onClick={() => setOnay(false)}>
                  {cevir("Vazgeç")}
                </Button>
              </>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
