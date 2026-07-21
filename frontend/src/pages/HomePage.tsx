import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  DndContext, DragOverlay, KeyboardSensor, PointerSensor, TouchSensor,
  closestCorners, useSensor, useSensors, useDroppable,
} from '@dnd-kit/core'
import type { DragStartEvent, DragOverEvent, DragEndEvent } from '@dnd-kit/core'
import {
  SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy,
} from '@dnd-kit/sortable'
import { CSS } from '@dnd-kit/utilities'
import { restrictToWindowEdges } from '@dnd-kit/modifiers'
import { api } from '@/lib/api'
import { useAuth } from '@/store/auth'
import LoadHistoryChart from '@/components/LoadHistoryChart'
import MemoryHistoryChart from '@/components/MemoryHistoryChart'
import CveWidget from '@/components/CveWidget'

type SistemInfo = {
  hostname: string; ip: string; os_adi: string; kernel: string
  mimari: string; cpu_modeli: string; cpu_cekirdek: number; panel_surum: string
}
type CPU = { yuzde: number; cekirdek: number; yuk_1dk: number; yuk_5dk: number; yuk_15dk: number }
type Bellek = { toplam_kb: number; kullanilan_kb: number; bos_kb: number; yuzde: number }
type Swap = { toplam_kb: number; kullanilan_kb: number; yuzde: number }
type Disk = { toplam_byte: number; kullanilan_byte: number; bos_byte: number; yuzde: number; mount: string; fs?: string }
type Ag = { arayuz: string; rx_bytes_sn: number; tx_bytes_sn: number; rx_toplam_byte: number; tx_toplam_byte: number }
type Servis = { ad: string; etiket: string; aktif: boolean }
type Sistem = {
  sistem: SistemInfo; cpu: CPU; bellek: Bellek; swap: Swap
  disk: Disk; diskler: Disk[]; ag: Ag; servisler: Servis[]; uptime_sn: number
  kota_reboot_gerekli?: boolean
  izolasyon_kaybi?: string[]
}
type Domain = { id: number; alan_adi: string; ssl: boolean; durum: string }

/* --- yeni uçlar --- */
type Guncelleme = { arac_var: boolean; calisiyor: boolean; durum: string }
type Optimize = { calisiyor: boolean; durum: string }
type YedekSatir = { domain_id: number; alan_adi: string; sayi: number; toplam_b: number; son_yedek: string }
type YedekOzet = {
  domainler: YedekSatir[]; toplam_boyut_b: number; toplam_yedek: number
  hedef_sayisi: number; zamanlama: string
}
type WpKurulum = {
  domain_id: number; alan_adi: string; dizin: string; surum: string
  son_surum: string; durum: 'guncel' | 'eski' | 'bilinmiyor'; kurulum_tarihi: string
  site_url: string; admin_url: string
}

/* ================= PANO DÜZENİ (sürükle-bırak) ================= */
type Duzen = { columns: string[][] }

// Varsayılan düzen — mevcut sıralamayı BİREBİR korur (ilk açılış aynı görünür)
const VARSAYILAN_DUZEN: Duzen = {
  columns: [
    ['yuk-grafik', 'wordpress', 'panel-guncelleme', 'son-yedek', 'performans'],
    ['bellek-grafik', 'cve-guvenlik', 'servisler', 'domainler'],
    ['sunucu-bilgi', 'saglik', 'canli-kaynak', 'abonelikler', 'ag'],
  ],
}
const WIDGET_IDS: string[] = VARSAYILAN_DUZEN.columns.flat()
const WIDGET_SET = new Set(WIDGET_IDS)
const VARSAYILAN_KOLON: Record<string, number> = (() => {
  const m: Record<string, number> = {}
  VARSAYILAN_DUZEN.columns.forEach((c, i) => c.forEach((id) => { m[id] = i }))
  return m
})()
const WIDGET_ADI: Record<string, string> = {
  'yuk-grafik': 'Yük Grafiği', 'bellek-grafik': 'Bellek Grafiği', 'wordpress': 'WordPress Siteleri',
  'panel-guncelleme': 'Panel Güncelleme', 'son-yedek': 'Son Sunucu Yedeklemesi', 'performans': 'Performans / Optimize',
  'cve-guvenlik': 'Güvenlik Açıkları (CVE)', 'servisler': 'Servisler', 'domainler': 'Domainler', 'sunucu-bilgi': 'Sunucu Bilgileri',
  'saglik': 'Sistem Sağlığı', 'canli-kaynak': 'Canlı Kaynaklar', 'abonelikler': 'Aboneliklerim', 'ag': 'Ağ Trafiği',
}

// Kayıtlı düzeni koda göre birleştir: bilinmeyen id'leri at, eksik (yeni) widget'ları
// varsayılan kolonuna ekle — bayat kayıt yüzünden hiçbir widget kaybolmaz.
function birlestirDuzen(kayit: unknown): Duzen {
  const src = (kayit as { columns?: unknown })?.columns
  const kaynak: unknown[] = Array.isArray(src) ? src : []
  const cols: string[][] = [[], [], []]
  const yerlesti = new Set<string>()
  for (let i = 0; i < 3; i++) {
    const arr = Array.isArray(kaynak[i]) ? (kaynak[i] as unknown[]) : []
    for (const id of arr) {
      if (typeof id === 'string' && WIDGET_SET.has(id) && !yerlesti.has(id)) {
        cols[i].push(id); yerlesti.add(id)
      }
    }
  }
  for (const id of WIDGET_IDS) {
    if (!yerlesti.has(id)) { cols[VARSAYILAN_KOLON[id]].push(id); yerlesti.add(id) }
  }
  return { columns: cols }
}

function kolonIndeksi(cols: string[][], id: string): number {
  if (id.startsWith('col-')) return parseInt(id.slice(4), 10)
  return cols.findIndex((c) => c.includes(id))
}

function usePrefersReducedMotion(): boolean {
  const [r, setR] = useState(false)
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    const on = () => setR(mq.matches)
    on()
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [])
  return r
}

export default function HomePage() {
  const kullanici = useAuth((s) => s.kullanici)
  const [s, setS] = useState<Sistem | null>(null)
  const [domainler, setDomainler] = useState<Domain[]>([])
  const [guncelleme, setGuncelleme] = useState<Guncelleme | null>(null)
  const [optimize, setOptimize] = useState<Optimize | null>(null)
  const [yedek, setYedek] = useState<YedekOzet | null>(null)
  const [wp, setWp] = useState<WpKurulum[] | null>(null)

  // --- pano düzeni durumu ---
  const [duzen, setDuzen] = useState<Duzen>(VARSAYILAN_DUZEN)
  const [aktifId, setAktifId] = useState<string | null>(null)
  const [kayit, setKayit] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const reduced = usePrefersReducedMotion()

  const duzenRef = useRef(duzen)
  useEffect(() => { duzenRef.current = duzen }, [duzen])

  const kayitTimer = useRef<number | null>(null)
  const kayitSifirla = useRef<number | null>(null)

  const kaydet = (lay: Duzen, hemen = false) => {
    if (kayitTimer.current) { clearTimeout(kayitTimer.current); kayitTimer.current = null }
    const isle = () => {
      setKayit('saving')
      api.put('/dashboard-duzen', { duzen: JSON.stringify(lay) })
        .then(() => {
          setKayit('saved')
          if (kayitSifirla.current) clearTimeout(kayitSifirla.current)
          kayitSifirla.current = window.setTimeout(() => setKayit('idle'), 2000)
        })
        .catch(() => {
          setKayit('error')
          if (kayitSifirla.current) clearTimeout(kayitSifirla.current)
          kayitSifirla.current = window.setTimeout(() => setKayit('idle'), 4000)
        })
    }
    if (hemen) isle()
    else kayitTimer.current = window.setTimeout(isle, 600)
  }

  // Kayıtlı düzeni yükle (yoksa varsayılan) + eksik widget'ları birleştir
  useEffect(() => {
    api.get<{ duzen: string }>('/dashboard-duzen')
      .then((r) => {
        const ham = r.data?.duzen
        if (ham && ham.trim()) {
          try { setDuzen(birlestirDuzen(JSON.parse(ham))) }
          catch { setDuzen(VARSAYILAN_DUZEN) }
        } else {
          setDuzen(birlestirDuzen(VARSAYILAN_DUZEN))
        }
      })
      .catch(() => setDuzen(VARSAYILAN_DUZEN))
  }, [])

  useEffect(() => () => {
    if (kayitTimer.current) clearTimeout(kayitTimer.current)
    if (kayitSifirla.current) clearTimeout(kayitSifirla.current)
  }, [])

  useEffect(() => {
    const cekKullanim = () => {
      if (typeof document !== 'undefined' && document.hidden) return // sekme gizliyken poll'u duraklat
      api.get<Sistem>('/system/usage').then((r) => setS(r.data)).catch(() => {})
    }
    const cekBakim = () => {
      if (typeof document !== 'undefined' && document.hidden) return
      api.get<Guncelleme>('/system/guncelleme').then((r) => setGuncelleme(r.data)).catch(() => {})
      api.get<Optimize>('/system/optimize').then((r) => setOptimize(r.data)).catch(() => {})
    }
    cekKullanim()
    cekBakim()
    // Bir kez okunanlar (dosya sistemi / DB / wp-cli ağırlıklı — poll edilmez)
    api.get<Domain[]>('/domains').then((r) => setDomainler(r.data || [])).catch(() => {})
    api.get<YedekOzet>('/admin/backups/ozet').then((r) => setYedek(r.data)).catch(() => {})
    api.get<WpKurulum[]>('/wordpress/tumu').then((r) => setWp(r.data || [])).catch(() => setWp([]))

    const idK = setInterval(cekKullanim, 5000)   // kaynak kullanımı — 5 sn
    const idB = setInterval(cekBakim, 20000)     // bakım durumu — 20 sn
    const onVis = () => { if (!document.hidden) { cekKullanim(); cekBakim() } } // geri gelince tazele
    document.addEventListener('visibilitychange', onVis)
    return () => { clearInterval(idK); clearInterval(idB); document.removeEventListener('visibilitychange', onVis) }
  }, [])

  const aktif = domainler.filter((d) => d.durum === 'aktif').length
  const sslli = domainler.filter((d) => d.ssl).length
  const diskList = s ? (s.diskler?.length ? s.diskler : [s.disk]) : []
  const anaDisk = s ? (diskList[0] || s.disk) : null
  const servisAktif = s ? s.servisler.filter((x) => x.aktif).length : 0
  const servisToplam = s ? s.servisler.length : 0
  const servisDown = servisToplam - servisAktif
  const izoAdet = s?.izolasyon_kaybi?.length ?? 0

  const ad = (kullanici?.ad_soyad || kullanici?.adi || '').trim()
  const saglik = hesaplaSaglik(s, servisDown, izoAdet)

  // en yeni yedek zamanı (YYYY-MM-DD HH:MM leksikografik sıralanır)
  const sonYedek = yedek?.domainler?.reduce((a, r) => (r.son_yedek > a ? r.son_yedek : a), '') || ''
  const yedekliDomain = yedek?.domainler?.filter((r) => r.sayi > 0).length ?? 0

  // WordPress türetimleri
  const wpToplam = wp?.length ?? 0
  const wpEski = wp?.filter((x) => x.durum === 'eski').length ?? 0
  const wpGuncel = wp?.filter((x) => x.durum === 'guncel').length ?? 0
  const wpBilinmiyor = wp?.filter((x) => x.durum === 'bilinmiyor').length ?? 0

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 180, tolerance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  )

  const onDragStart = (e: DragStartEvent) => setAktifId(String(e.active.id))

  // Kolonlar arası taşımayı sürükleme sırasında uygula
  const onDragOver = (e: DragOverEvent) => {
    const { active, over } = e
    if (!over) return
    const activeId = String(active.id)
    const overId = String(over.id)
    if (activeId === overId) return
    setDuzen((prev) => {
      const cols = prev.columns
      const from = kolonIndeksi(cols, activeId)
      const to = kolonIndeksi(cols, overId)
      if (from === -1 || to === -1 || from === to) return prev
      const next = cols.map((c) => c.slice())
      const fromItems = next[from]
      const toItems = next[to]
      const ai = fromItems.indexOf(activeId)
      if (ai === -1) return prev
      fromItems.splice(ai, 1)
      let overIndex: number
      if (overId.startsWith('col-')) overIndex = toItems.length
      else {
        const oi = toItems.indexOf(overId)
        overIndex = oi === -1 ? toItems.length : oi
      }
      toItems.splice(overIndex, 0, activeId)
      return { columns: next }
    })
  }

  // Aynı kolon içi son sıralama + kalıcılaştır
  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e
    setAktifId(null)
    if (!over) return
    const activeId = String(active.id)
    const overId = String(over.id)
    const cols = duzenRef.current.columns
    const from = kolonIndeksi(cols, activeId)
    const to = kolonIndeksi(cols, overId)
    if (from === -1 || to === -1) return
    let next = duzenRef.current
    if (from === to) {
      const items = cols[to]
      const oldIndex = items.indexOf(activeId)
      let newIndex = overId.startsWith('col-') ? items.length - 1 : items.indexOf(overId)
      if (newIndex === -1) newIndex = items.length - 1
      if (oldIndex !== newIndex && oldIndex !== -1) {
        const nc = cols.map((c) => c.slice())
        nc[to] = arrayMove(items, oldIndex, newIndex)
        next = { columns: nc }
      }
    }
    setDuzen(next)
    kaydet(next)
  }

  const varsayilanaDon = () => {
    setDuzen(VARSAYILAN_DUZEN)
    kaydet(VARSAYILAN_DUZEN, true)
  }

  /* ---------- widget id → JSX eşlemesi ---------- */
  const widgets: Record<string, React.ReactNode> = {
    'yuk-grafik': <LoadHistoryChart />,

    'bellek-grafik': <MemoryHistoryChart />,

    'cve-guvenlik': <CveWidget />,

    'wordpress': (
      <Kart baslik="WordPress Siteleri" alt="Tüm hesaplardaki kurulumlar" ikon={I.wp}
        sag={<Link to="/wordpress" className="text-xs font-medium text-brand-600 hover:underline dark:text-brand-400">Daha fazlası →</Link>}>
        {wp === null ? (
          <Yukleniyor />
        ) : (
          <>
            <div className="mb-3 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
              <MiniIstatistik deger={wpToplam} etiket="Kurulum" renk="slate" />
              <MiniIstatistik deger={wpEski} etiket="Güncelleme" renk={wpEski > 0 ? 'amber' : 'emerald'} />
              <MiniIstatistik deger={wpGuncel} etiket="Güncel" renk="emerald" />
            </div>
            {wpEski > 0 && (
              <div className="mb-3 flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50 px-3 py-2 text-[11px] text-amber-700 dark:border-amber-800/50 dark:bg-amber-900/15 dark:text-amber-300">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} className="mt-0.5 h-3.5 w-3.5 shrink-0"><path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m0 3.75h.008M10.36 3.6 2.26 17.66A1.5 1.5 0 0 0 3.56 19.9h16.88a1.5 1.5 0 0 0 1.3-2.25L13.64 3.6a1.5 1.5 0 0 0-2.6 0Z" /></svg>
                <span><strong>{wpEski}</strong> kurulumda güncelleme mevcut — eski sürümler güvenlik açığı taşır.</span>
              </div>
            )}
            {wpToplam === 0 ? (
              <div className="py-5 text-center text-xs text-slate-400">Kurulu WordPress bulunamadı</div>
            ) : (
              <div className="space-y-0.5">
                {wp!.slice(0, 5).map((k) => (
                  <Link key={`${k.domain_id}-${k.dizin}`} to="/wordpress"
                    className="-mx-2 flex items-center justify-between rounded-xl px-2 py-2 transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/50">
                    <span className="flex min-w-0 items-center gap-2.5">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6}
                        className={`h-4 w-4 shrink-0 ${k.durum === 'eski' ? 'text-amber-500' : k.durum === 'guncel' ? 'text-emerald-500' : 'text-slate-400'}`}><path d={I.wp} /></svg>
                      <span className="min-w-0">
                        <span className="block truncate font-mono text-[13px] text-slate-700 dark:text-slate-200">{k.alan_adi}</span>
                        <span className="block truncate text-[10px] text-slate-400 dark:text-slate-500">{k.dizin === '/ (kök)' ? 'kök dizin' : k.dizin}{k.surum ? ` · v${k.surum}` : ''}</span>
                      </span>
                    </span>
                    <span className="shrink-0">
                      {k.durum === 'eski'
                        ? <Rozet renk="amber" metin={k.son_surum ? `→ v${k.son_surum}` : 'Güncelle'} />
                        : k.durum === 'guncel'
                          ? <Rozet renk="emerald" metin="Güncel" />
                          : <Rozet renk="slate" metin="Bilinmiyor" />}
                    </span>
                  </Link>
                ))}
                {wpToplam > 5 && (
                  <Link to="/wordpress" className="block pt-1.5 text-center text-[11px] text-slate-400 transition-colors hover:text-brand-600 dark:hover:text-brand-400">
                    +{wpToplam - 5} kurulum daha →
                  </Link>
                )}
              </div>
            )}
            {wpBilinmiyor > 0 && (
              <div className="mt-2 text-[10px] text-slate-400 dark:text-slate-500">{wpBilinmiyor} kurulumun durumu belirlenemedi (wp-cli zaman aşımı).</div>
            )}
          </>
        )}
      </Kart>
    ),

    'panel-guncelleme': (
      <Kart baslik="Panel Güncelleme" alt="Sürüm ve sistem paketleri" ikon={I.guncelle}
        sag={<Link to="/araclar/guncelleme" className="text-xs font-medium text-brand-600 hover:underline dark:text-brand-400">Daha fazlası →</Link>}>
        <div className="flex items-center gap-3">
          <span className={`grid h-11 w-11 shrink-0 place-items-center rounded-xl ${
            guncelleme?.calisiyor ? 'bg-sky-50 text-sky-600 dark:bg-sky-900/25 dark:text-sky-300'
              : guncelleme?.arac_var === false ? 'bg-amber-50 text-amber-600 dark:bg-amber-900/25 dark:text-amber-300'
                : 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/25 dark:text-emerald-300'}`}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-6 w-6"><path d={I.guncelle} /></svg>
          </span>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-slate-800 dark:text-slate-100">
              {guncelleme?.calisiyor ? 'Güncelleme çalışıyor' : guncelleme?.arac_var === false ? 'Güncelleme aracı yok' : 'Panel güncel'}
            </div>
            <div className="mt-0.5 truncate text-xs text-slate-500 dark:text-slate-400" title={guncelleme?.durum}>
              {guncelleme?.durum || (guncelleme ? 'Durum bilgisi yok' : 'Yükleniyor…')}
            </div>
          </div>
          <span className="ml-auto shrink-0">
            <Rozet renk={guncelleme?.calisiyor ? 'sky' : guncelleme?.arac_var === false ? 'amber' : 'emerald'}
              metin={guncelleme?.calisiyor ? 'Çalışıyor' : guncelleme?.arac_var === false ? 'Araç yok' : 'Güncel'} />
          </span>
        </div>
        <Link to="/araclar/paketler" className="-mx-2 mt-3 flex items-center justify-between rounded-xl border-t border-slate-100 px-2 pt-3 text-xs transition-colors hover:bg-slate-50 dark:border-slate-800 dark:hover:bg-slate-800/50">
          <span className="flex items-center gap-2 text-slate-600 dark:text-slate-300">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4 text-slate-400"><path d={I.paket} /></svg>
            Sistem paketleri
          </span>
          <span className="text-brand-600 dark:text-brand-400">Yönet →</span>
        </Link>
      </Kart>
    ),

    'son-yedek': (
      <Kart baslik="Son Sunucu Yedeklemesi" alt="Otomatik günlük yedek" ikon={I.yedek}
        sag={<Link to="/backup-yonetimi" className="text-xs font-medium text-brand-600 hover:underline dark:text-brand-400">Daha fazlası →</Link>}>
        {!yedek ? (
          <div className="py-6 text-center text-xs text-slate-400">Yedek özeti alınamadı</div>
        ) : (
          <>
            <div className="flex items-baseline gap-2">
              <span className="text-3xl font-bold tracking-tight tabular-nums text-slate-900 dark:text-slate-100">{yedek.toplam_yedek}</span>
              <span className="text-sm text-slate-500 dark:text-slate-400">yedek · {fmtByteGB(yedek.toplam_boyut_b)}</span>
            </div>
            <div className="mt-3 space-y-0">
              <KV etiket="Son yedek" deger={sonYedek || '—'} />
              <KV etiket="Yedekli site" deger={`${yedekliDomain} / ${yedek.domainler.length}`} />
              <KV etiket="Uzak hedef" deger={yedek.hedef_sayisi > 0 ? `${yedek.hedef_sayisi} aktif` : 'Yok'} />
              <KV etiket="Zamanlama" deger={yedek.zamanlama} />
            </div>
          </>
        )}
      </Kart>
    ),

    'performans': (
      <Kart baslik="Performans / Optimize" alt="Sunucu ayarlarını iyileştir" ikon={I.optimize}
        sag={<Link to="/araclar/optimize" className="text-xs font-medium text-brand-600 hover:underline dark:text-brand-400">Daha fazlası →</Link>}>
        <div className="flex items-center gap-3">
          <span className={`grid h-11 w-11 shrink-0 place-items-center rounded-xl ${optimize?.calisiyor ? 'bg-sky-50 text-sky-600 dark:bg-sky-900/25 dark:text-sky-300' : 'bg-brand-50 text-brand-600 dark:bg-brand-900/20 dark:text-brand-300'}`}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-6 w-6"><path d={I.optimize} /></svg>
          </span>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-slate-800 dark:text-slate-100">
              {optimize?.calisiyor ? 'Optimizasyon çalışıyor' : 'Optimizasyon hazır'}
            </div>
            <div className="mt-0.5 truncate text-xs text-slate-500 dark:text-slate-400" title={optimize?.durum}>
              {optimize?.durum || (optimize ? 'MariaDB · nginx · PHP ayarları' : 'Yükleniyor…')}
            </div>
          </div>
          <span className="ml-auto shrink-0">
            <Rozet renk={optimize?.calisiyor ? 'sky' : 'slate'} metin={optimize?.calisiyor ? 'Çalışıyor' : 'Boşta'} />
          </span>
        </div>
      </Kart>
    ),

    'servisler': (
      <Kart baslik="Servisler" alt={s ? `${servisAktif}/${servisToplam} servis çalışıyor` : 'servis durumu'} ikon={I.servis}
        sag={s ? <Rozet renk={servisDown === 0 ? 'emerald' : 'amber'} metin={servisDown === 0 ? 'Hepsi aktif' : `${servisDown} kapalı`} /> : undefined}>
        {!s ? <Yukleniyor /> : (
          <div className="grid grid-cols-1 gap-x-5 gap-y-0.5 sm:grid-cols-2">
            {s.servisler.map((sv) => (
              <div key={sv.ad} title={sv.ad}
                className="-mx-1 flex items-center justify-between rounded-lg px-1.5 py-1.5 transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/40">
                <span className="flex min-w-0 items-center gap-2">
                  <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${sv.aktif ? 'bg-emerald-500' : 'bg-red-500'}`} />
                  <span className="truncate text-[13px] text-slate-700 dark:text-slate-200">{sv.etiket}</span>
                </span>
                <span className={`shrink-0 text-[11px] font-medium ${sv.aktif ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-500 dark:text-red-400'}`}>
                  {sv.aktif ? 'Aktif' : 'Kapalı'}
                </span>
              </div>
            ))}
          </div>
        )}
      </Kart>
    ),

    'domainler': (
      <Kart baslik="Domainler" alt="Barındırılan siteler ve SSL durumu" ikon={I.domain}
        sag={<Link to="/domainler" className="text-xs font-medium text-brand-600 hover:underline dark:text-brand-400">Daha fazlası →</Link>}>
        <div className="mb-4 grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
          <MiniIstatistik deger={domainler.length} etiket="Toplam" renk="slate" />
          <MiniIstatistik deger={aktif} etiket="Aktif" renk="emerald" />
          <MiniIstatistik deger={sslli} etiket="SSL" renk="sky" />
        </div>
        {domainler.length === 0 ? (
          <div className="py-6 text-center text-xs text-slate-400">Henüz domain yok</div>
        ) : (
          <div className="space-y-0.5">
            {domainler.slice(0, 7).map((d) => (
              <Link key={d.id} to={`/abonelikler/${d.id}`}
                className="-mx-2 flex items-center justify-between rounded-xl px-2 py-2.5 transition-colors hover:bg-slate-50 dark:hover:bg-slate-800/50">
                <span className="flex min-w-0 items-center gap-2.5">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7}
                    className={`h-4 w-4 shrink-0 ${d.ssl ? 'text-emerald-500' : 'text-slate-400 dark:text-slate-500'}`}>
                    {d.ssl
                      ? <><rect x="5" y="11" width="14" height="9" rx="2" /><path strokeLinecap="round" d="M8 11V8a4 4 0 0 1 8 0v3" /></>
                      : <><rect x="5" y="11" width="14" height="9" rx="2" /><path strokeLinecap="round" d="M8 11V6a4 4 0 0 1 7-2.6" /></>}
                  </svg>
                  <span className="truncate font-mono text-[13px] text-slate-700 dark:text-slate-200">{d.alan_adi}</span>
                </span>
                <span className="flex shrink-0 items-center gap-2">
                  {!d.ssl && <Rozet renk="amber" metin="SSL yok" />}
                  <Rozet renk={d.durum === 'aktif' ? 'emerald' : 'slate'} metin={d.durum === 'aktif' ? 'Aktif' : d.durum} />
                </span>
              </Link>
            ))}
            {domainler.length > 7 && (
              <Link to="/domainler" className="block pt-1.5 text-center text-[11px] text-slate-400 transition-colors hover:text-brand-600 dark:hover:text-brand-400">
                +{domainler.length - 7} domain daha →
              </Link>
            )}
          </div>
        )}
      </Kart>
    ),

    'sunucu-bilgi': (
      <Kart baslik="Sunucu Bilgileri" alt="Donanım ve sistem" ikon={I.sunucu}>
        {!s ? <Yukleniyor /> : (
          <div className="space-y-0">
            <KV etiket="Sunucu adı" deger={s.sistem.hostname} />
            <KV etiket="IP adresi" deger={s.sistem.ip || '—'} />
            <KV etiket="İşletim sistemi" deger={s.sistem.os_adi || '—'} />
            <KV etiket="Çekirdek" deger={s.sistem.kernel || '—'} />
            <KV etiket="İşlemci" deger={s.sistem.cpu_modeli || '—'} />
            <KV etiket="Çekirdek sayısı" deger={`${s.sistem.cpu_cekirdek} vCPU`} />
            <KV etiket="Çalışma süresi" deger={formatUptime(s.uptime_sn)} />
            <KV etiket="Panel sürümü" deger={s.sistem.panel_surum || '—'} />
          </div>
        )}
      </Kart>
    ),

    'saglik': (
      <div className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900/60">
        <div className="mb-4 flex items-center gap-2">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4 text-slate-400 dark:text-slate-500"><path d={I.saglik} /></svg>
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Sistem Sağlığı</h3>
        </div>
        <div className="flex items-center gap-4">
          <SaglikHalka skor={saglik.skor} renk={saglik.renk} hazir={!!s} />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className={`h-2.5 w-2.5 shrink-0 rounded-full ${saglik.nokta}`} />
              <span className="text-base font-semibold tracking-tight text-slate-900 dark:text-slate-100">{s ? saglik.baslik : '—'}</span>
            </div>
            <p className="mt-1 text-[12px] leading-snug text-slate-500 dark:text-slate-400">{s ? saglik.aciklama : 'Sağlık verileri yükleniyor…'}</p>
            <div className="mt-2.5 flex flex-wrap gap-1.5">
              <Cip renk={servisDown === 0 ? 'emerald' : 'amber'} metin={s ? `${servisAktif}/${servisToplam} servis` : '…'} />
              <Cip renk={izoAdet === 0 ? 'emerald' : 'rose'} metin={izoAdet === 0 ? 'izolasyon sağlam' : `${izoAdet} izolasyon kaybı`} />
            </div>
          </div>
        </div>
      </div>
    ),

    'canli-kaynak': (
      <Kart baslik="Canlı Kaynaklar" alt="Anlık CPU · RAM · Disk" ikon={I.grafik}>
        {!s ? <Yukleniyor /> : (
          <div className="space-y-3.5">
            <KaynakBar etiket="İşlemci" ikon={I.cpu} yuzde={s.cpu.yuzde} alt={`${s.cpu.cekirdek} çekirdek · yük ${s.cpu.yuk_1dk.toFixed(2)}`} />
            <KaynakBar etiket="Bellek" ikon={I.ram} yuzde={s.bellek.yuzde} alt={`${fmtGB(s.bellek.kullanilan_kb)} / ${fmtGB(s.bellek.toplam_kb)}`} />
            <KaynakBar etiket="Disk" ikon={I.disk} yuzde={anaDisk?.yuzde ?? 0} alt={anaDisk ? `${fmtByteGB(anaDisk.kullanilan_byte)} / ${fmtByteGB(anaDisk.toplam_byte)}` : '…'} />
            {s.swap.toplam_kb > 0 && (
              <KaynakBar etiket="Takas" ikon={I.ram} yuzde={s.swap.yuzde} alt={`${fmtGB(s.swap.kullanilan_kb)} / ${fmtGB(s.swap.toplam_kb)}`} />
            )}
          </div>
        )}
      </Kart>
    ),

    'abonelikler': (
      <Kart baslik="Aboneliklerim" alt="Barındırma abonelikleri" ikon={I.abonelik}
        sag={<Link to="/domainler" className="text-xs font-medium text-brand-600 hover:underline dark:text-brand-400">Daha fazlası →</Link>}>
        <div className="flex items-baseline gap-2">
          <span className="text-3xl font-bold tracking-tight tabular-nums text-slate-900 dark:text-slate-100">{domainler.length}</span>
          <span className="text-sm text-slate-500 dark:text-slate-400">abonelik</span>
        </div>
        <div className="mt-3 space-y-0">
          <KV etiket="Aktif abonelik" deger={`${aktif} / ${domainler.length}`} />
          <KV etiket="SSL sertifikalı" deger={`${sslli} / ${domainler.length}`} />
          <KV etiket="WordPress kurulumu" deger={wp === null ? '…' : `${wpToplam}`} />
        </div>
      </Kart>
    ),

    'ag': (
      <Kart baslik="Ağ Trafiği" alt={s?.ag.arayuz ? s.ag.arayuz : 'arayüz'} ikon={I.ag}>
        {!s ? <Yukleniyor /> : !s.ag.arayuz ? (
          <div className="py-4 text-xs italic text-slate-400 dark:text-slate-500">Arayüz bulunamadı</div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="rounded-xl border border-emerald-100 bg-emerald-50 p-3 dark:border-emerald-800/40 dark:bg-emerald-900/15">
              <div className="text-[10px] font-semibold uppercase tracking-wide text-emerald-600 dark:text-emerald-400">↓ Gelen</div>
              <div className="mt-1 font-mono text-lg font-bold text-emerald-700 dark:text-emerald-300">{fmtRate(s.ag.rx_bytes_sn)}</div>
              <div className="mt-1 text-[10px] text-slate-400 dark:text-slate-500">Σ {fmtByteGB(s.ag.rx_toplam_byte)}</div>
            </div>
            <div className="rounded-xl border border-sky-100 bg-sky-50 p-3 dark:border-sky-800/40 dark:bg-sky-900/15">
              <div className="text-[10px] font-semibold uppercase tracking-wide text-sky-600 dark:text-sky-400">↑ Giden</div>
              <div className="mt-1 font-mono text-lg font-bold text-sky-700 dark:text-sky-300">{fmtRate(s.ag.tx_bytes_sn)}</div>
              <div className="mt-1 text-[10px] text-slate-400 dark:text-slate-500">Σ {fmtByteGB(s.ag.tx_toplam_byte)}</div>
            </div>
          </div>
        )}
      </Kart>
    ),
  }

  return (
    <div className="mx-auto max-w-[1600px] px-4 py-6 sm:px-6">
      <style>{ANIM_CSS}</style>

      {/* Üst şerit: kırıntı + canlı durum + Özelleştir */}
      <header className="rise mb-4 flex flex-col gap-2 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-[11px] font-medium uppercase tracking-wider text-slate-400 dark:text-slate-500">
            <span className="font-semibold text-slate-500 dark:text-slate-400">GirginOSPanel</span>
            <span className="h-1 w-1 rounded-full bg-slate-300 dark:bg-slate-700" />
            Ana Sayfa
          </div>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">
            {selamla()}{ad ? `, ${ad}` : ''}
          </h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            {s ? (
              <>
                <span className="font-mono text-slate-600 dark:text-slate-300">{s.sistem.hostname}</span>
                <span className="mx-1.5 text-slate-300 dark:text-slate-600">·</span>
                {formatUptime(s.uptime_sn)} kesintisiz çalışıyor
              </>
            ) : 'Sistem verileri yükleniyor…'}
          </p>
        </div>

        <div className="flex shrink-0 flex-wrap items-center gap-2 self-start">
          {/* kayıt göstergesi */}
          {kayit !== 'idle' && (
            <span className={`text-xs font-medium ${
              kayit === 'saved' ? 'text-emerald-600 dark:text-emerald-400'
                : kayit === 'error' ? 'text-rose-600 dark:text-rose-400'
                  : 'text-slate-400 dark:text-slate-500'}`}>
              {kayit === 'saving' ? 'Kaydediliyor…' : kayit === 'saved' ? 'Kaydedildi ✓' : 'Kaydedilemedi'}
            </span>
          )}

          <button type="button" onClick={varsayilanaDon} title="Widget düzenini varsayılana döndür"
            className="inline-flex items-center gap-1.5 rounded-full border border-slate-200 bg-white px-3 py-1.5 text-xs font-medium text-slate-600 transition-colors hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300 dark:hover:bg-slate-800">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-3.5 w-3.5">
              <path d="M4 12a8 8 0 0 1 13.7-5.7L20 8M20 4v4h-4" />
            </svg>
            Varsayılan düzen
          </button>

          <div className="flex items-center gap-2 rounded-full border border-slate-200 bg-white/70 px-3 py-1.5 text-xs font-medium text-slate-500
                          dark:border-slate-700 dark:bg-slate-900/70 dark:text-slate-400">
            <span className="relative flex h-2 w-2">
              {s && <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" />}
              <span className={`relative inline-flex h-2 w-2 rounded-full ${s ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`} />
            </span>
            {s ? 'Canlı izleme' : 'Bekleniyor'}
          </div>
        </div>
      </header>

      {/* Kartlar köşedeki tutamaçtan sürüklenerek yeniden düzenlenir; değişiklik otomatik kaydedilir */}

      {/* GÜVENLİK: tenant CageFS izolasyon kaybı — tam genişlik, kolonların üstünde */}
      {!!s?.izolasyon_kaybi?.length && (
        <div className="rise mb-4 flex items-start gap-3 rounded-2xl border border-rose-300 bg-rose-50 px-4 py-3.5 dark:border-rose-800/60 dark:bg-rose-900/15">
          <svg className="mt-0.5 h-5 w-5 shrink-0 text-rose-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m0 3.75h.008M10.363 3.591 2.257 17.657a1.5 1.5 0 0 0 1.302 2.25h16.882a1.5 1.5 0 0 0 1.302-2.25L13.638 3.591a1.5 1.5 0 0 0-2.598 0Z" />
          </svg>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-rose-800 dark:text-rose-200">Güvenlik: hesap izolasyonu kaybı</div>
            <div className="mt-0.5 text-xs text-rose-700 dark:text-rose-300">
              Aşağıdaki hesaplar paylaşılan PHP-FPM düzenine indirildi; kendilerine özel dosya sistemi
              izolasyonu (CageFS) şu anda <span className="font-semibold">etkin değil</span> — komşu hesapların
              yüzeyine açıklar. Ayrıntı için denetim kaydına bakın (<span className="font-mono">guvenlik.izolasyon_kaybi</span>).
            </div>
            <div className="mt-2 flex flex-wrap gap-1.5">
              {s.izolasyon_kaybi!.map((sk) => (
                <span key={sk} className="rounded-lg bg-rose-100 px-2 py-0.5 font-mono text-[11px] text-rose-800 dark:bg-rose-900/40 dark:text-rose-200">{sk}</span>
              ))}
            </div>
          </div>
        </div>
      )}

      {/* Disk kotası reboot uyarısı — tam genişlik */}
      {s?.kota_reboot_gerekli && (
        <div className="rise mb-4 flex items-start gap-3 rounded-2xl border border-amber-300 bg-amber-50 px-4 py-3.5 dark:border-amber-800/60 dark:bg-amber-900/15">
          <svg className="mt-0.5 h-5 w-5 shrink-0 text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v3.75m0 3.75h.008M10.363 3.591 2.257 17.657a1.5 1.5 0 0 0 1.302 2.25h16.882a1.5 1.5 0 0 0 1.302-2.25L13.638 3.591a1.5 1.5 0 0 0-2.598 0Z" />
          </svg>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-amber-800 dark:text-amber-200">Disk kotası aktif değil</div>
            <div className="mt-0.5 text-xs text-amber-700 dark:text-amber-300">
              Etkinleştirmek için tek seferlik sunucu yeniden başlatması gerekli.
            </div>
          </div>
        </div>
      )}

      {/* ================= 3 KOLONLU YOĞUN PANO (sürükle-bırak) ================= */}
      <DndContext
        sensors={sensors}
        collisionDetection={closestCorners}
        modifiers={[restrictToWindowEdges]}
        onDragStart={onDragStart}
        onDragOver={onDragOver}
        onDragEnd={onDragEnd}
        onDragCancel={() => setAktifId(null)}
      >
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3 lg:items-start">
          {duzen.columns.map((colIds, ci) => (
            <SortableContext key={`col-${ci}`} items={colIds} strategy={verticalListSortingStrategy}>
              <DroppableKolon id={`col-${ci}`} dragging={aktifId != null}>
                {colIds.map((wid, wi) => (
                  <SortableWidget key={wid} id={wid} index={ci + wi} reduced={reduced}>
                    {widgets[wid] ?? null}
                  </SortableWidget>
                ))}
              </DroppableKolon>
            </SortableContext>
          ))}
        </div>

        <DragOverlay dropAnimation={reduced ? null : undefined}>
          {aktifId ? (
            <div className="rounded-2xl shadow-2xl shadow-slate-900/25 ring-2 ring-brand-400/60">
              {widgets[aktifId] ?? null}
            </div>
          ) : null}
        </DragOverlay>
      </DndContext>
    </div>
  )
}

/* ---------- sürükle-bırak sarmalayıcıları ---------- */
function DroppableKolon({ id, dragging, children }: { id: string; dragging: boolean; children: React.ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id })
  return (
    <div ref={setNodeRef}
      className={`flex flex-col gap-4 ${dragging ? `min-h-[120px] rounded-2xl transition-colors ${isOver ? 'bg-brand-50/40 dark:bg-brand-900/10' : ''}` : ''}`}>
      {children}
    </div>
  )
}

function SortableWidget({ id, index, reduced, children }:
  { id: string; index: number; reduced: boolean; children: React.ReactNode }) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id })
  const style: React.CSSProperties = {
    transform: CSS.Translate.toString(transform),
    transition: reduced ? undefined : transition,
    opacity: isDragging ? 0.4 : undefined,
    zIndex: isDragging ? 40 : undefined,
  }
  return (
    <div ref={setNodeRef} style={style} className="group relative">
      <div className="rise" style={{ animationDelay: `${index * 55}ms` }}>
        {children}
      </div>
      {/* sürükleme tutamacı — her zaman görünür, köşede yüzer; kartın geri kalanı tıklanabilir kalır */}
      <button
        type="button"
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
        aria-label={`${WIDGET_ADI[id] ?? id} kartını sürükleyerek taşı`}
        title="Sürükleyerek taşı"
        className="absolute -right-2.5 -top-2.5 z-20 flex h-7 w-7 cursor-grab touch-none items-center justify-center rounded-full border border-slate-200 bg-white text-slate-400 opacity-60 shadow-sm transition hover:text-slate-700 hover:opacity-100 focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400 group-hover:opacity-100 active:cursor-grabbing dark:border-slate-700 dark:bg-slate-800 dark:text-slate-500 dark:hover:text-slate-200">
        <svg viewBox="0 0 24 24" width="13" height="13" fill="currentColor" aria-hidden="true">
          <circle cx="9" cy="6" r="1.4" /><circle cx="15" cy="6" r="1.4" />
          <circle cx="9" cy="12" r="1.4" /><circle cx="15" cy="12" r="1.4" />
          <circle cx="9" cy="18" r="1.4" /><circle cx="15" cy="18" r="1.4" />
        </svg>
      </button>
    </div>
  )
}

/* ---------- animasyon ---------- */
const ANIM_CSS = `
@keyframes gospRise { from { opacity: 0; transform: translateY(12px) } to { opacity: 1; transform: none } }
.rise { animation: gospRise .5s cubic-bezier(.22,1,.36,1) both }
@media (prefers-reduced-motion: reduce) { .rise { animation: none } }
`

function selamla(): string {
  const h = new Date().getHours()
  if (h < 6) return 'İyi geceler'
  if (h < 12) return 'Günaydın'
  if (h < 18) return 'İyi günler'
  return 'İyi akşamlar'
}

/* ---------- ikonlar (stroke) ---------- */
const I = {
  cpu:     'M9 3v2m6-2v2M9 19v2m6-2v2M3 9h2M3 15h2m14-6h2m-2 6h2M7 7h10v10H7z',
  ram:     'M3 7h18v10H3zM7 7v10m5-10v10m5-10v10',
  disk:    'M4 6c0-1.7 3.6-3 8-3s8 1.3 8 3M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 6c0 1.7 3.6 3 8 3s8-1.3 8-3M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3',
  saat:    'M12 8v4l3 2M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18Z',
  domain:  'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18ZM3 12h18M12 3c2.6 2.5 2.6 15 0 18M12 3c-2.6 2.5-2.6 15 0 18',
  ag:      'M12 5v14M6 13l6 6 6-6M4 5h16',
  servis:  'M12 3 5 6v6c0 4 3 6.6 7 8 4-1.4 7-4 7-8V6l-7-3ZM9 12l2 2 4-4',
  grafik:  'M3 3v18h18M7 15l3-4 3 3 4-6',
  yedek:   'M4 5h16v5H4zM4 14h16v5H4zM8 7.5h.01M8 16.5h.01',
  guncelle:'M4 12a8 8 0 0 1 13.7-5.7L20 8M20 4v4h-4M20 12a8 8 0 0 1-13.7 5.7L4 16M4 20v-4h4',
  optimize:'M13 2 4 14h7l-1 8 9-12h-7l1-8Z',
  saglik:  'M12 3 5 6v6c0 4 3 6.6 7 8 4-1.4 7-4 7-8V6l-7-3ZM9 12l2 2 4-4',
  paket:   'M12 2 3 7v10l9 5 9-5V7l-9-5ZM3 7l9 5 9-5M12 12v10',
  sunucu:  'M4 5h16v6H4zM4 13h16v6H4zM8 8h.01M8 16h.01',
  abonelik:'M4 7h16v12H4zM4 7l4-4h8l4 4M9 12h6',
  wp:      'M12 3a9 9 0 1 0 0 18 9 9 0 0 0 0-18ZM4 8l4 11 3-8-2-3M20 8l-4 11-3-8',
}

/* ---------- sağlık türetimi ---------- */
type Saglik = { skor: number; baslik: string; aciklama: string; renk: string; nokta: string }
function hesaplaSaglik(s: Sistem | null, servisDown: number, izoAdet: number): Saglik {
  if (!s) return { skor: 0, baslik: '—', aciklama: '', renk: '#64748b', nokta: 'bg-slate-400' }
  const disk = s.diskler?.length ? Math.max(...s.diskler.map((d) => d.yuzde)) : s.disk.yuzde
  const enYuksek = Math.max(s.cpu.yuzde, s.bellek.yuzde, disk)
  let skor = 100 - Math.round(enYuksek * 0.32) - servisDown * 6 - (izoAdet > 0 ? 22 : 0) - (s.kota_reboot_gerekli ? 5 : 0)
  skor = Math.max(0, Math.min(100, skor))
  const kritik = enYuksek >= 85 || izoAdet > 0
  const dikkat = enYuksek >= 70 || servisDown > 0 || s.kota_reboot_gerekli
  if (kritik) {
    return {
      skor, baslik: 'Sistem Kritik', renk: '#ef4444', nokta: 'bg-red-500',
      aciklama: izoAdet > 0 ? 'Hesap izolasyonu kaybı var — güvenlik önceliği.' : 'Kaynak kullanımı çok yüksek, hemen inceleyin.',
    }
  }
  if (dikkat) {
    return {
      skor, baslik: 'Dikkat Gerekli', renk: '#f59e0b', nokta: 'bg-amber-500',
      aciklama: servisDown > 0 ? `${servisDown} servis çalışmıyor — kontrol edin.` : 'Bazı kaynaklar yükseliyor, göz atmakta fayda var.',
    }
  }
  return {
    skor, baslik: 'Sistem Sağlıklı', renk: '#10b981', nokta: 'bg-emerald-500',
    aciklama: 'Tüm kritik servisler çalışıyor, kaynak kullanımı normal seviyelerde.',
  }
}

/* ---------- sayaç ---------- */
function useCountUp(target: number, dur = 800): number {
  const [v, setV] = useState(0)
  const prev = useRef(0)
  useEffect(() => {
    const from = prev.current, to = target, t0 = performance.now()
    let raf = 0
    const tick = (now: number) => {
      const p = Math.min(1, (now - t0) / dur)
      const e = 1 - Math.pow(1 - p, 3)
      setV(Math.round(from + (to - from) * e))
      if (p < 1) raf = requestAnimationFrame(tick)
      else prev.current = to
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [target, dur])
  return v
}

/* ---------- bileşenler ---------- */
function esikRenk(y: number, taban: string): string {
  if (y >= 85) return '#ef4444'
  if (y >= 70) return '#f59e0b'
  const m: Record<string, string> = { brand: '#f97316', emerald: '#10b981', sky: '#0ea5e9', violet: '#8b5cf6' }
  return m[taban] || '#64748b'
}

function SaglikHalka({ skor, renk, hazir }: { skor: number; renk: string; hazir: boolean }) {
  const r = 42, c = 2 * Math.PI * r
  const val = Math.min(100, Math.max(0, skor))
  const off = c * (1 - val / 100)
  return (
    <div className="relative h-[104px] w-[104px] shrink-0">
      <svg viewBox="0 0 104 104" className="h-[104px] w-[104px] -rotate-90">
        <circle cx="52" cy="52" r={r} fill="none" className="stroke-slate-100 dark:stroke-slate-800" strokeWidth="8" />
        <circle cx="52" cy="52" r={r} fill="none" stroke={renk} strokeWidth="8" strokeLinecap="round"
          strokeDasharray={c} strokeDashoffset={hazir ? off : c}
          style={{ transition: 'stroke-dashoffset 900ms cubic-bezier(.22,1,.36,1), stroke 300ms' }} />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        <span className="text-2xl font-bold tabular-nums text-slate-900 dark:text-slate-100">{hazir ? skor : '—'}</span>
        <span className="mt-0.5 text-[8px] font-semibold uppercase tracking-[0.18em] text-slate-400 dark:text-slate-500">Sağlık</span>
      </div>
    </div>
  )
}

function KaynakBar({ etiket, ikon, yuzde, alt }: { etiket: string; ikon: string; yuzde: number; alt: string }) {
  const renk = esikRenk(yuzde, 'emerald')
  const bar = Math.min(100, Math.max(2, yuzde))
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between">
        <span className="flex items-center gap-1.5 text-xs font-medium text-slate-500 dark:text-slate-400">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round" className="h-3.5 w-3.5"><path d={ikon} /></svg>
          {etiket}
        </span>
        <span className="font-mono text-xs font-semibold tabular-nums text-slate-800 dark:text-slate-100">%{Math.round(yuzde)}</span>
      </div>
      <div className="h-1.5 overflow-hidden rounded-full bg-slate-100 dark:bg-slate-800">
        <div className="h-full rounded-full" style={{ width: `${bar}%`, background: renk, transition: 'width 900ms cubic-bezier(.22,1,.36,1)' }} />
      </div>
      <div className="mt-1 truncate font-mono text-[10px] text-slate-400 dark:text-slate-500" title={alt}>{alt}</div>
    </div>
  )
}

function Cip({ renk, metin }: { renk: string; metin: string }) {
  const m: Record<string, string> = {
    emerald: 'bg-emerald-500', amber: 'bg-amber-500', rose: 'bg-rose-500', sky: 'bg-sky-500', slate: 'bg-slate-400',
  }
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border border-slate-200 bg-slate-50 px-2.5 py-1 text-[11px] text-slate-500
                     dark:border-slate-700 dark:bg-slate-800/50 dark:text-slate-400">
      <span className={`h-1.5 w-1.5 rounded-full ${m[renk] || 'bg-slate-400'}`} />
      {metin}
    </span>
  )
}

function Rozet({ renk, metin }: { renk: string; metin: string }) {
  const m: Record<string, string> = {
    emerald: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300',
    amber: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300',
    sky: 'bg-sky-100 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300',
    slate: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
    rose: 'bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-300',
  }
  return <span className={`rounded-full px-2 py-0.5 text-[11px] font-semibold ${m[renk] || m.slate}`}>{metin}</span>
}

function Kart({ baslik, alt, ikon, children, sag }:
  { baslik: string; alt?: string; ikon?: string; children: React.ReactNode; sag?: React.ReactNode }) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900/60">
      <div className="mb-4 flex items-start justify-between gap-3">
        <div className="flex items-center gap-2">
          {ikon && (
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round"
              className="h-4 w-4 text-slate-400 dark:text-slate-500">
              <path d={ikon} />
            </svg>
          )}
          <div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{baslik}</h3>
            {alt && <p className="mt-0.5 text-[11px] text-slate-400 dark:text-slate-500">{alt}</p>}
          </div>
        </div>
        {sag && <div className="shrink-0">{sag}</div>}
      </div>
      {children}
    </div>
  )
}

function MiniIstatistik({ deger, etiket, renk }: { deger: number; etiket: string; renk: string }) {
  const say = useCountUp(deger)
  const r: Record<string, string> = {
    slate: 'text-slate-800 dark:text-slate-100', emerald: 'text-emerald-600 dark:text-emerald-400',
    sky: 'text-sky-600 dark:text-sky-400', amber: 'text-amber-600 dark:text-amber-400',
  }
  return (
    <div className="rounded-xl border border-slate-100 bg-slate-50 p-3 text-center dark:border-slate-800 dark:bg-slate-950/40">
      <div className={`text-2xl font-bold tabular-nums ${r[renk] || r.slate}`}>{say}</div>
      <div className="mt-0.5 text-[11px] text-slate-400 dark:text-slate-500">{etiket}</div>
    </div>
  )
}

function KV({ etiket, deger }: { etiket: string; deger: string }) {
  return (
    <div className="flex items-center justify-between gap-3 border-t border-slate-100 py-2 text-xs first:border-t-0 dark:border-slate-800">
      <span className="shrink-0 text-slate-500 dark:text-slate-400">{etiket}</span>
      <span className="truncate text-right font-mono font-medium text-slate-700 dark:text-slate-200" title={deger}>{deger}</span>
    </div>
  )
}

function Yukleniyor() { return <div className="py-4 text-center text-xs text-slate-400">Yükleniyor…</div> }

function formatUptime(sn: number): string {
  const g = Math.floor(sn / 86400), sa = Math.floor((sn % 86400) / 3600), dk = Math.floor((sn % 3600) / 60)
  if (g > 0) return `${g}g ${sa}sa`
  if (sa > 0) return `${sa}sa ${dk}dk`
  return `${dk}dk`
}
function fmtGB(kb: number): string {
  const mb = kb / 1024
  return mb < 1024 ? `${mb.toFixed(0)} MB` : `${(mb / 1024).toFixed(1)} GB`
}
function fmtByteGB(b: number): string {
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(0)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(1)} GB`
}
function fmtRate(bps: number): string {
  if (bps < 1024) return `${bps.toFixed(0)} B/s`
  if (bps < 1024 * 1024) return `${(bps / 1024).toFixed(0)} KB/s`
  return `${(bps / 1024 / 1024).toFixed(1)} MB/s`
}
