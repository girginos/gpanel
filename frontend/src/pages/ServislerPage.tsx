import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { Button } from '@/components/ui'
import { useToast } from '@/components/Toast'

type Servis = {
  birim: string
  etiket: string
  grup: string
  reload: boolean
  durum: string // active | inactive | failed | absent
}

const DURUM_STIL: Record<string, string> = {
  active:   'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300',
  inactive: 'bg-slate-100 text-slate-500 dark:bg-dark-700 dark:text-slate-400',
  failed:   'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300',
  absent:   'bg-slate-100 text-slate-400 dark:bg-dark-700 dark:text-slate-500',
}
const DURUM_ETIKET: Record<string, string> = {
  active: '● Çalışıyor', inactive: '○ Durmuş', failed: '✕ Hatalı', absent: '— Kurulu değil',
}
// Grup ikonları — inline stroke SVG (currentColor ile çevre metninden renk alır).
const GRUP_IKON: Record<string, string> = {
  'Web Sunucusu': 'M12 3a9 9 0 100 18 9 9 0 000-18zM3 12h18M12 3c2.5 2.4 3.8 5.6 3.8 9s-1.3 6.6-3.8 9c-2.5-2.4-3.8-5.6-3.8-9S9.5 5.4 12 3z',
  'Veritabanı & Önbellek': 'M12 3c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3zM4 6v6c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6',
  'DNS': 'M4.9 16.1a10 10 0 010-14.2m2.83 2.83a6 6 0 000 8.48M12 12h.01m4.24 4.24a10 10 0 000-14.2m-2.83 2.83a6 6 0 010 8.48M12 12l-3 9m6 0l-3-9',
  'PHP-FPM': 'M4 5h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1V6a1 1 0 011-1zM4 13h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1v-4a1 1 0 011-1zM7 8h.01M7 16h.01',
  'Diğer': 'M10.3 4.3c.4-1.8 2.9-1.8 3.3 0a1.7 1.7 0 002.6 1.1c1.5-.9 3.3.8 2.4 2.4a1.7 1.7 0 001 2.5c1.8.4 1.8 2.9 0 3.3a1.7 1.7 0 00-1 2.6c.9 1.5-.8 3.3-2.4 2.4a1.7 1.7 0 00-2.6 1c-.4 1.8-2.9 1.8-3.3 0a1.7 1.7 0 00-2.6-1c-1.5.9-3.3-.8-2.4-2.4a1.7 1.7 0 00-1-2.6c-1.8-.4-1.8-2.9 0-3.3a1.7 1.7 0 001-2.5c-.9-1.6.8-3.3 2.4-2.4 1 .6 2.3.2 2.6-1zM15 12a3 3 0 11-6 0 3 3 0 016 0z',
}


const SERVIS_EN: Record<string, string> = {
  "Servis Yönetimi": "Service Management",
  "Servisler alınamadı": "Failed to get services",
  "Veritabanı & Önbellek": "Database & Cache",
  "— Kurulu değil": "— Not installed",
  "○ Durmuş": "○ Stopped",
  "● Çalışıyor": "● Running",
  "✕ Hatalı": "✕ Failed",
  "yeniden yüklendi": "reloaded",
  "yeniden başlatıldı": "restarted",
  "Araçlar & Ayarlar": "Tools & Settings",
  "Servisler": "Services",
  "Web, veritabanı, DNS ve PHP servislerini buradan yeniden başlatın. Ayar değişikliği sonrası kullanışlıdır.": "Restart the web, database, DNS and PHP services from here. Useful after a configuration change.",
  "Yükleniyor…": "Loading…",
  "{0} işlemi başarısız": "{0} operation failed",
  "Web Sunucusu": "Web Server",
  "Diğer": "Other",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (SERVIS_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function ServislerPage() {
  useTranslation() // dil re-render aboneligi
  const [liste, setListe] = useState<Servis[]>([])
  const [yukleniyor, setYukleniyor] = useState(true)
  const [islemBirim, setIslemBirim] = useState<string | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const toast = useToast()

  async function getir() {
    try {
      const r = await api.get<Servis[]>('/system/servisler')
      setListe(r.data)
    } catch (e) {
      const m = apiHata(e, cevir("Servisler alınamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setYukleniyor(false)
    }
  }
  useEffect(() => { getir() }, [])

  async function islem(s: Servis, aksiyon: 'restart' | 'reload') {
    setIslemBirim(s.birim); setHata(null); setBasari(null)
    try {
      await api.post('/system/servis-islem', { birim: s.birim, aksiyon })
      const ok = `${s.etiket} ${aksiyon === 'reload' ? cevir("yeniden yüklendi") : cevir("yeniden başlatıldı")}.`
      setBasari(ok)
      toast.basari(ok)
      await getir()
    } catch (e) {
      const m = apiHata(e, cevirT(cevir("{0} işlemi başarısız"), s.etiket))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIslemBirim(null)
    }
  }

  // Grupları görülme sırasına göre, grup içindeki servis sırasını koruyarak topla
  const gruplar: { ad: string; servisler: Servis[] }[] = []
  for (const s of liste) {
    let g = gruplar.find(x => x.ad === s.grup)
    if (!g) { g = { ad: s.grup, servisler: [] }; gruplar.push(g) }
    g.servisler.push(s)
  }

  return (
    <div className="max-w-4xl mx-auto px-4 py-6">
      <Breadcrumb items={[
        { etiket: cevir("Araçlar & Ayarlar"), href: '/araclar-ayarlar' },
        { etiket: cevir("Servisler") },
      ]} />
      <div className="mb-5">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Servis Yönetimi")}</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
          {cevir(cevir("Web, veritabanı, DNS ve PHP servislerini buradan yeniden başlatın. Ayar değişikliği sonrası kullanışlıdır."))}
        </p>
      </div>

      {yukleniyor ? (
        <div className="p-8 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
      ) : (
        <div className="space-y-6">
          {gruplar.map(g => (
            <section key={g.ad}>
              <h2 className="flex items-center gap-2 text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500 mb-2 px-1">
                <span className="text-slate-400 dark:text-slate-500">
                  {GRUP_IKON[g.ad]
                    ? <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4"><path d={GRUP_IKON[g.ad]} /></svg>
                    : <span className="text-sm">•</span>}
                </span>{cevir(g.ad)}
              </h2>
              <div className="bg-white dark:bg-dark-800 border border-slate-200 dark:border-dark-600 rounded-lg overflow-hidden">
                <ul className="divide-y divide-slate-100 dark:divide-dark-600">
                  {g.servisler.map(s => {
                    const absent = s.durum === 'absent'
                    const mesgul = islemBirim === s.birim
                    return (
                      <li key={s.birim} className="flex items-center gap-4 px-5 py-3.5">
                        <div className="flex-1 min-w-0">
                          <div className="font-medium text-slate-900 dark:text-slate-100">{s.etiket}</div>
                          <div className="text-xs font-mono text-slate-400 dark:text-slate-500">{s.birim}</div>
                        </div>
                        <span className={`w-28 text-center text-xs px-2.5 py-1 rounded-full font-medium ${DURUM_STIL[s.durum] || DURUM_STIL.inactive}`}>
                          {cevir(DURUM_ETIKET[s.durum] || s.durum)}
                        </span>
                        <div className="flex flex-wrap items-center gap-2 shrink-0">
                          {/* Reload slotu her satırda yer kaplar → Restart hizalı kalır */}
                          <Button variant="outlined" disabled={!s.reload || absent || mesgul} onClick={() => islem(s, 'reload')}
                            className={`w-20 px-3 py-1.5 text-sm ${s.reload ? '' : 'invisible'}`}>
                            Reload
                          </Button>
                          <Button disabled={absent || mesgul} onClick={() => islem(s, 'restart')}
                            className="w-20 px-3.5 py-1.5 text-sm">
                            {mesgul ? '…' : 'Restart'}
                          </Button>
                        </div>
                      </li>
                    )
                  })}
                </ul>
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  )
}
