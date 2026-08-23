import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import type { FormEvent } from 'react'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

/*
 * Oturum Boşta Süresi.
 *
 * Kullanıcı N dakika istek atmazsa, sonraki istekte token reddedilir ve
 * kullanıcı otomatik çıkışa itilir.
 *   - 0 = kapalı (yalnız JWT mutlak süresi geçerli)
 *   - 1-1440 = aktif eşik (dakika, üst sınır 24 saat)
 *
 * 🔴 JWT TTL'den AYRI bir kavram. JWT geçerli olduğu sürece boşta süresini
 * kontrol edilir. Panel operatörü genelde JWT'yi uzun (12 saat) tutar; idle'i
 * kısa (30 dk) tutmak ekran kilidi + otomatik çıkış davranışını sağlar.
 */


const OTURUM_EN: Record<string, string> = {
  "Türkçe": "English",
  "Araçlar ve Ayarlar": "Tools and Settings",
  "Yükleniyor…": "Loading…",
  "Oturum Boşta Süresi": "Session Idle Timeout",
  "Kullanıcı belirlenen süre boyunca istek atmazsa oturumu otomatik kapanır.": "If the user makes no requests for the set period, the session closes automatically.",
  "Boşta süresi (dakika)": "Idle timeout (minutes)",
  "Kapat": "Disable",
  "Oturum boşta süresi {0} dakika olarak kaydedildi.": "Session idle timeout saved as {0} minutes.",
  "Kaydedilemedi": "Could not save",
  "dakika": "minutes",
  "Kaydediliyor…": "Saving…",
  "Kaydet": "Save",
  "Mevcut:": "Current:",
  "kapalı": "off",
  "{0} dk": "{0} min",
  "Not:": "Note:",
  "Bu, JWT mutlak süresinden ayrı bir kavramdır. Kullanıcı her istekle boşta sayacını sıfırlar; hareketsiz kalırsa {0} dakika sonra otomatik çıkış olur. Değişiklik yeni istekten itibaren geçerlidir; hâlihazırda aktif olan kullanıcılar etkilenmez, ancak boşta kalırlarsa yeni eşik uygulanır.": "This is a separate concept from the JWT absolute lifetime. The user resets the idle counter with every request; if inactive, they are logged out automatically after {0} minutes. The change takes effect from the next request; currently active users are not affected, but the new threshold applies once they go idle.",
  "0 = kapalı.": "0 = off.",
  "0 ile 1440 arasında bir değer girin (0 = kapalı)": "Enter a value between 0 and 1440 (0 = off)",
  "Ayar okunamadı": "Failed to read setting",
  "Oturum Güvenliği": "Session Security",
  "Oturum boşta süresi kapatıldı — yalnız JWT mutlak süresi kaldı.": "Session idle timeout disabled — only the JWT absolute lifetime remains.",
  "Üst sınır 1440 dakika (24 saat).": "Maximum 1440 minutes (24 hours).",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (OTURUM_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function OturumAyarPage() {
  useTranslation() // dil re-render aboneligi
  const [mevcut, setMevcut] = useState<number | null>(null)
  const [taslak, setTaslak] = useState<string>('')
  const [yukleniyor, setYukleniyor] = useState(true)
  const [gonderiliyor, setGonderiliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [mesaj, setMesaj] = useState<string | null>(null)

  useEffect(() => { void yukle() }, [])

  const yukle = async () => {
    setYukleniyor(true); setHata(null)
    try {
      const r = await api.get<{ dakika: number }>('/settings/session-idle')
      setMevcut(r.data.dakika)
      setTaslak(String(r.data.dakika))
    } catch (e) {
      setHata(apiHata(e, cevir("Ayar okunamadı")))
    } finally { setYukleniyor(false) }
  }

  const kaydet = async (ev: FormEvent) => {
    ev.preventDefault()
    setHata(null); setMesaj(null)
    const dk = parseInt(taslak, 10)
    if (Number.isNaN(dk) || dk < 0 || dk > 1440) {
      setHata(cevir("0 ile 1440 arasında bir değer girin (0 = kapalı)")); return
    }
    setGonderiliyor(true)
    try {
      await api.put('/settings/session-idle', { dakika: dk })
      setMevcut(dk)
      setMesaj(dk === 0
        ? cevir("Oturum boşta süresi kapatıldı — yalnız JWT mutlak süresi kaldı.")
        : cevirT(cevir("Oturum boşta süresi {0} dakika olarak kaydedildi."), dk))
      setTimeout(() => setMesaj(null), 5000)
    } catch (e) {
      setHata(apiHata(e, cevir("Kaydedilemedi")))
    } finally { setGonderiliyor(false) }
  }

  const degistiMi = mevcut !== null && String(mevcut) !== taslak.trim()

  return (
    <div className="mx-auto max-w-2xl px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { href: '/araclar-ayarlar', etiket: cevir("Araçlar ve Ayarlar") },
        { etiket: cevir("Oturum Güvenliği") },
      ]} />

      <div className="mt-4 mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">
          {cevir("Oturum Boşta Süresi")}
        </h1>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
          {cevir("Kullanıcı belirlenen süre boyunca istek atmazsa oturumu otomatik kapanır.")}
          {' '}<span className="font-medium text-slate-800 dark:text-slate-200">{cevir("0 = kapalı.")}</span>
        </p>
      </div>

      {yukleniyor ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">{cevir("Yükleniyor…")}</div>
      ) : (
        <form onSubmit={kaydet} className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900">
          <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">
            {cevir("Boşta süresi (dakika)")}
          </label>
          <div className="flex items-center gap-3">
            <input
              type="number"
              min={0}
              max={1440}
              step={1}
              value={taslak}
              onChange={(e) => setTaslak(e.target.value)}
              className="w-32 rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
            />
            <span className="text-sm text-slate-500">{cevir("dakika")}</span>
            {[15, 30, 60, 120].map((v) => (
              <button
                key={v}
                type="button"
                onClick={() => setTaslak(String(v))}
                className="rounded-md border border-slate-200 px-2 py-1 text-xs text-slate-600 transition-colors hover:bg-slate-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800"
              >
                {v}
              </button>
            ))}
            <button
              type="button"
              onClick={() => setTaslak('0')}
              className="rounded-md border border-slate-200 px-2 py-1 text-xs text-slate-600 transition-colors hover:bg-slate-50 dark:border-slate-700 dark:text-slate-400 dark:hover:bg-slate-800"
              title={cevir("Kapat")}
            >
              {cevir("Kapat")}
            </button>
          </div>
          <p className="mt-2 text-xs text-slate-500">{cevir("Üst sınır 1440 dakika (24 saat).")}</p>

          <div className="mt-5 flex items-center gap-3">
            <button
              type="submit"
              disabled={!degistiMi || gonderiliyor}
              className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white"
            >
              {gonderiliyor ? cevir("Kaydediliyor…") : cevir("Kaydet")}
            </button>
            {mevcut !== null && (
              <span className="text-xs text-slate-500">
                {cevir("Mevcut:")} <span className="font-mono">{mevcut === 0 ? cevir("kapalı") : cevirT(cevir("{0} dk"), mevcut)}</span>
              </span>
            )}
          </div>

          {mesaj && (
            <p className="mt-4 rounded-lg border border-emerald-200 bg-emerald-50 p-3 text-sm text-emerald-700 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300">{mesaj}</p>
          )}
          {hata && (
            <p className="mt-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">{hata}</p>
          )}
        </form>
      )}

      <div className="mt-5 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-900 dark:border-amber-900/40 dark:bg-amber-950/30 dark:text-amber-200">
        <strong>{cevir("Not:")}</strong> {cevirT(cevir("Bu, JWT mutlak süresinden ayrı bir kavramdır. Kullanıcı her istekle boşta sayacını sıfırlar; hareketsiz kalırsa {0} dakika sonra otomatik çıkış olur. Değişiklik yeni istekten itibaren geçerlidir; hâlihazırda aktif olan kullanıcılar etkilenmez, ancak boşta kalırlarsa yeni eşik uygulanır."), mevcut ?? '?')}
      </div>
    </div>
  )
}
