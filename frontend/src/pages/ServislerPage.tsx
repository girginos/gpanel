import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { Button, Badge, type Color } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { useToast } from '@/components/Toast'

type Servis = {
  birim: string
  etiket: string
  grup: string
  reload: boolean
  durum: string // active | inactive | failed | absent
}

// Durum rozeti: renk + nokta (çalışıyor = dolu, durmuş = içi boş, hatalı = dolu kırmızı) + metin.
const DURUM_RENK: Record<string, Color> = {
  active: 'success', inactive: 'neutral', failed: 'error', absent: 'neutral',
}
const DURUM_NOKTA: Record<string, string> = {
  active: 'bg-current', inactive: 'border border-current', failed: 'bg-current',
}
const DURUM_ETIKET: Record<string, string> = {
  active: 'Çalışıyor', inactive: 'Durmuş', failed: 'Hatalı', absent: 'Kurulu değil',
}
// Grup ikonları — ortak SVG ikon setinden (currentColor ile çevre metninden renk alır).
const GRUP_IKON: Record<string, string> = {
  'Web Sunucusu': I.kure,
  'Veritabanı & Önbellek': I.db,
  'DNS': I.anten,
  'PHP-FPM': I.sunucu,
  'Diğer': I.ayar,
}


const SERVIS_EN: Record<string, string> = {
  "Servis Yönetimi": "Service Management",
  "Servisler alınamadı": "Failed to get services",
  "Veritabanı & Önbellek": "Database & Cache",
  "Kurulu değil": "Not installed",
  "Durmuş": "Stopped",
  "Çalışıyor": "Running",
  "Hatalı": "Failed",
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
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
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
    <div>
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
                  <Ikon d={GRUP_IKON[g.ad] || I.parca} className="h-4 w-4" />
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
                        <Badge variant="soft" color={DURUM_RENK[s.durum] || 'neutral'} className="w-28 gap-1.5">
                          {DURUM_NOKTA[s.durum] && <span className={`inline-block size-2 shrink-0 rounded-full ${DURUM_NOKTA[s.durum]}`} aria-hidden />}
                          {cevir(DURUM_ETIKET[s.durum] || s.durum)}
                        </Badge>
                        <div className="flex flex-wrap items-center gap-2 shrink-0">
                          {/* Reload slotu her satırda yer kaplar → Restart hizalı kalır */}
                          <Button variant="outlined" boyut="sm" ikon={I.yenile} disabled={!s.reload || absent || mesgul} onClick={() => islem(s, 'reload')}
                            className={`w-24 ${s.reload ? '' : 'invisible'}`}>
                            Reload
                          </Button>
                          <Button color="primary" boyut="sm" ikon={I.sifirla} disabled={absent} yukleniyor={mesgul} onClick={() => islem(s, 'restart')}
                            className="w-24">
                            Restart
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
