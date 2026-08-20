import React, { useEffect, useMemo, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import Breadcrumb from '@/components/Breadcrumb'
import SunucuOptimize from '@/components/SunucuOptimize'
import { api } from '@/lib/api'

/*
 * Sunucu Optimize v2 — sistem panosu + servis-bazlı analiz + öneri + uygula + yedek/rollback.
 * Endpoint'ler: /optimize/analiz, /optimize/uygula, /optimize/yedekler, /optimize/rollback.
 * Mevcut "paketleri güncelle + tune script" bloğu (SunucuOptimize component) sayfanın en altında BAKIM kartı olarak kalır.
 */

type Seviye = 'bilgi' | 'onemli' | 'kritik'

type Oneri = {
  id: string; servis: string; param: string; etiket: string
  mevcut: string; onerilen: string; gerekce: string
  seviye: Seviye; dosya: string; etki: 'reload' | 'restart' | 'yok'
}

type ServisDurum = { aktif: boolean; durum: string; memory_mb: number; start_time: string; restarts: number }

type ServisAnaliz = {
  kod: string; ad: string; ikon: string
  durum: ServisDurum
  log_sinyal: string[]
  oneriler: Oneri[]
  not_yok?: string
  son_uygulama?: { yedek_id: number; tarih: string; uygulananlar: string; geri_alindi: boolean }[]
}

type Sistem = {
  ram_toplam_mb: number; ram_kullan_mb: number; ram_buf_cache_mb: number
  swap_toplam_mb: number; swap_kullan_mb: number; swap_kullanim_yuzde: number
  cpu_cekirdek: number; load1: number; load5: number; load15: number
  disk_toplam_gb: number; disk_kullan_gb: number; disk_yuzde: number
  uptime_saniye: number; kernel: string; dagitim: string; profil: string
}

type AnalizRaporu = { sistem: Sistem; servisler: ServisAnaliz[]; zaman: string }

type YedekKayit = { id: number; servis: string; yedek: string; hedef: string; aciklama: string; rolled_back: boolean; created_at: string }

const SERVIS_SIRA = ['mariadb', 'nginx', 'apache', 'phpfpm', 'sysctl']

export default function SunucuOptimizePage() {
  const [rapor, setRapor] = useState<AnalizRaporu | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [aktif, setAktif] = useState<string>('mariadb')
  const [hata, setHata] = useState<string | null>(null)

  const yukle = async () => {
    setHata(null)
    try {
      const r = await api.get<AnalizRaporu>('/optimize/analiz')
      // Servisleri sabit sıraya diz
      const ordered = SERVIS_SIRA.map(k => r.data.servisler.find((s: ServisAnaliz) => s.kod === k)).filter(Boolean) as ServisAnaliz[]
      setRapor({ ...r.data, servisler: ordered })
    } catch (e: any) {
      setHata(e?.response?.data?.hata || 'Analiz alınamadı')
    } finally {
      setYukleniyor(false)
    }
  }
  useEffect(() => { void yukle() }, [])

  const seciliServis = rapor?.servisler.find(s => s.kod === aktif)
  const toplamOneri = rapor?.servisler.reduce((n, s) => n + (s.oneriler?.length || 0), 0) || 0

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Araçlar ve Ayarlar', href: '/araclar-ayarlar' },
        { etiket: 'Sunucu Optimize' },
      ]} />

      <div className="mb-5 flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Sunucu Optimize</h1>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            Sistem loglarını + kaynak durumunu okuyarak MariaDB / Nginx / Apache / PHP-FPM / kernel için değişiklik önerir.
            Her uygulama önce yedek alır, servisi -t doğrular, sonra reload eder.
          </p>
        </div>
        <button onClick={yukle} disabled={yukleniyor}
          className="rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-900 px-3 py-1.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-800 disabled:opacity-50">
          {yukleniyor ? 'Analiz ediliyor…' : <span className="inline-flex items-center gap-1.5"><Ikon d={I.yenile} />Yeniden analiz et</span>}
        </button>
      </div>

      {hata && <div className="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700">{hata}</div>}

      {rapor && (
        <>
          <SistemPanosu s={rapor.sistem} toplamOneri={toplamOneri} />

          <div className="mt-6 grid grid-cols-1 gap-5 lg:grid-cols-[240px_1fr]">
            {/* Sol servis listesi */}
            <div className="space-y-1.5">
              {rapor.servisler.map(s => (
                <button key={s.kod} onClick={() => setAktif(s.kod)}
                  className={`w-full text-left rounded-xl border p-3 transition ${
                    aktif === s.kod
                      ? 'border-blue-300 bg-blue-50 dark:border-blue-800 dark:bg-blue-950/30'
                      : 'border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900 hover:border-slate-300'
                  }`}>
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2.5">
                      <ServisLogo kod={s.kod} boy={22} />
                      <span className="font-medium text-sm text-slate-900 dark:text-slate-100">{s.ad}</span>
                    </div>
                    {(s.oneriler?.length || 0) > 0 && (
                      <span className={`text-[11px] font-medium rounded-full px-2 py-0.5 ${
                        s.oneriler?.some(o => o.seviye === 'kritik') ? 'bg-red-100 text-red-800 dark:bg-red-950/50 dark:text-red-200' :
                        s.oneriler?.some(o => o.seviye === 'onemli') ? 'bg-amber-100 text-amber-800 dark:bg-amber-950/50 dark:text-amber-200' :
                        'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300'
                      }`}>
                        {s.oneriler.length}
                      </span>
                    )}
                  </div>
                  <div className="mt-1 flex items-center gap-2 text-[11px] text-slate-500">
                    <span className={`inline-block h-1.5 w-1.5 rounded-full ${s.durum?.aktif ? 'bg-emerald-500' : 'bg-slate-400'}`}></span>
                    {s.durum?.aktif ? 'aktif' : (s.not_yok || 'kapalı')}
                    {s.durum?.memory_mb > 0 && <span>· {s.durum.memory_mb}MB</span>}
                  </div>
                </button>
              ))}
            </div>

            {/* Sağ detay */}
            {seciliServis && <ServisDetay analiz={seciliServis} yenile={yukle} />}
          </div>

          <YedeklerBolumu />
        </>
      )}

      {/* Mevcut BAKIM tune script bloğu (backward-compat) */}
      <div className="mt-8 pt-6 border-t border-slate-200 dark:border-slate-800">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-slate-500 mb-3">Bakım: Paket güncelle + Tune script</h2>
        <div className="max-w-3xl">
          <SunucuOptimize />
        </div>
      </div>
    </div>
  )
}

/* ---------- Sistem Panosu ---------- */

function SistemPanosu({ s, toplamOneri }: { s: Sistem; toplamOneri: number }) {
  const ramKullanimPct = s.ram_toplam_mb > 0 ? Math.round((s.ram_kullan_mb / s.ram_toplam_mb) * 100) : 0
  const loadPct = Math.round((s.load1 / s.cpu_cekirdek) * 100)
  const swapRenk: any = s.swap_kullanim_yuzde > 50 ? 'red' : s.swap_kullanim_yuzde > 20 ? 'amber' : 'emerald'
  const ramRenk: any = ramKullanimPct > 85 ? 'red' : ramKullanimPct > 65 ? 'amber' : 'emerald'
  const loadRenk: any = loadPct > 80 ? 'red' : loadPct > 40 ? 'amber' : 'emerald'
  const diskRenk: any = s.disk_yuzde > 85 ? 'red' : s.disk_yuzde > 70 ? 'amber' : 'emerald'

  return (
    <section>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
        <PanoKart ikon={<IcChip />} etiket="RAM" deger={`${(s.ram_kullan_mb / 1024).toFixed(1)}/${(s.ram_toplam_mb / 1024).toFixed(1)}G`} renk={ramRenk} trend={ramKullanimPct} altBilgi={`buf/cache ${(s.ram_buf_cache_mb / 1024).toFixed(1)}G`} />
        <PanoKart ikon={<IcBolt />} etiket="Load1" deger={`${s.load1.toFixed(2)} / ${s.cpu_cekirdek}`} renk={loadRenk} trend={Math.min(100, loadPct)} altBilgi={`5m ${s.load5.toFixed(2)} · 15m ${s.load15.toFixed(2)}`} />
        <PanoKart ikon={<IcSwap />} etiket="Swap" deger={s.swap_toplam_mb > 0 ? `${s.swap_kullanim_yuzde}%` : 'yok'} renk={swapRenk} trend={s.swap_kullanim_yuzde} altBilgi={`${s.swap_kullan_mb}/${s.swap_toplam_mb}M`} />
        <PanoKart ikon={<IcDisk />} etiket="Disk /" deger={`${s.disk_yuzde}%`} renk={diskRenk} trend={s.disk_yuzde} altBilgi={`${s.disk_kullan_gb.toFixed(0)}/${s.disk_toplam_gb.toFixed(0)}G`} />
        <PanoKart ikon={<IcServer />} etiket="Profil" deger={profilAd(s.profil)} renk="violet" altBilgi={`${s.cpu_cekirdek} CPU · ${(s.ram_toplam_mb / 1024).toFixed(1)}G RAM`} />
        <PanoKart ikon={<IcBulb />} etiket="Öneri" deger={String(toplamOneri)} renk={toplamOneri > 10 ? 'amber' : toplamOneri > 0 ? 'blue' : 'emerald'} altBilgi={toplamOneri === 0 ? 'sistem optimum' : 'inceleyip uygula'} />
      </div>
      <div className="mt-2 text-[11px] text-slate-500 flex items-center gap-4 flex-wrap">
        <span>Kernel {s.kernel}</span>
        <span>{s.dagitim}</span>
        <span>Uptime {formatUptime(s.uptime_saniye)}</span>
      </div>
    </section>
  )
}

/* ---------- Servis Detay ---------- */

function ServisDetay({ analiz, yenile }: { analiz: ServisAnaliz; yenile: () => Promise<void> }) {
  const [secili, setSecili] = useState<Set<string>>(new Set())
  const [uyguluyor, setUyguluyor] = useState(false)
  const [sonuc, setSonuc] = useState<string | null>(null)

  const oneriler = analiz.oneriler || []
  const seviyeSira = { kritik: 0, onemli: 1, bilgi: 2 } as const
  const sirali = useMemo(() => [...oneriler].sort((a, b) => seviyeSira[a.seviye] - seviyeSira[b.seviye]), [oneriler])

  const tumunuSec = () => setSecili(new Set(sirali.map(o => o.id)))
  const tumunuBirak = () => setSecili(new Set())
  const toggle = (id: string) => setSecili(s => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n })

  const geriAlUygulama = async (yedekID: number, ozet: string) => {
    if (!confirm(`"${ozet}" değişikliğini geri al?

Servis ${analiz.kod === 'mariadb' ? 'RESTART' : 'RELOAD'} edilecek.`)) return
    try {
      await api.post('/optimize/rollback', { yedek_id: yedekID })
      setSonuc('Geri alındı — yeniden analiz ediliyor')
      await yenile()
    } catch (e: any) {
      setSonuc('Rollback hatası: ' + (e?.response?.data?.hata || 'bilinmeyen'))
    }
  }

  const uygula = async () => {
    if (secili.size === 0) return
    if (!confirm(`${secili.size} öneri uygulanacak. Servisler ${analiz.kod === 'mariadb' ? 'RESTART' : 'reload'} edilecek. Emin misin?`)) return
    setUyguluyor(true); setSonuc(null)
    try {
      const r = await api.post<any>('/optimize/uygula', { servis: analiz.kod, oneri_id: Array.from(secili) })
      setSonuc(r.data?.mesaj || 'Uygulandı')
      setSecili(new Set())
      await yenile()
    } catch (e: any) {
      setSonuc('Hata: ' + (e?.response?.data?.hata || 'bilinmeyen'))
    } finally { setUyguluyor(false) }
  }

  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900">
      <div className="flex items-start justify-between gap-4 mb-4">
        <div>
          <div className="flex items-center gap-2.5">
            <ServisLogo kod={analiz.kod} boy={32} />
            <h2 className="text-lg font-semibold">{analiz.ad}</h2>
            {analiz.durum?.aktif && <span className="text-xs bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200 rounded-full px-2 py-0.5">aktif</span>}
          </div>
          {analiz.durum && analiz.durum.memory_mb > 0 && (
            <div className="mt-1 text-xs text-slate-500">
              Bellek {analiz.durum.memory_mb} MB · Restart {analiz.durum.restarts} kez
            </div>
          )}
        </div>
        {sirali.length > 0 && (
          <div className="flex items-center gap-2">
            {secili.size < sirali.length && <button onClick={tumunuSec} className="text-xs text-slate-600 hover:underline">Tümünü seç</button>}
            {secili.size > 0 && <button onClick={tumunuBirak} className="text-xs text-slate-600 hover:underline">Temizle</button>}
            <button onClick={uygula} disabled={secili.size === 0 || uyguluyor}
              className="rounded-lg bg-emerald-600 px-3.5 py-1.5 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-40">
              {uyguluyor ? 'Uygulanıyor…' : `Uygula (${secili.size})`}
            </button>
          </div>
        )}
      </div>

      {sonuc && <div className={`mb-3 rounded-lg border p-3 text-sm ${sonuc.startsWith('Hata') ? 'border-red-200 bg-red-50 text-red-700' : 'border-emerald-200 bg-emerald-50 text-emerald-700'}`}>{sonuc}</div>}

      {analiz.not_yok && (
        <div className="rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm text-slate-600 dark:border-slate-800 dark:bg-slate-800/50 dark:text-slate-300">
          {analiz.not_yok}
        </div>
      )}

      {(analiz.son_uygulama?.length || 0) > 0 && (
        <div className="mb-4 rounded-xl border border-emerald-200 bg-emerald-50 p-3 dark:border-emerald-900/50 dark:bg-emerald-950/20">
          <div className="flex items-center gap-1.5 text-xs font-semibold text-emerald-900 dark:text-emerald-200 uppercase tracking-wider mb-2">
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" strokeWidth="2" viewBox="0 0 24 24" strokeLinecap="round" strokeLinejoin="round">
              <path d="M9 12l2 2 4-4M12 22C6.48 22 2 17.52 2 12S6.48 2 12 2s10 4.48 10 10-4.48 10-10 10z" />
            </svg>
            Son uygulananlar
          </div>
          <div className="text-[11px] text-emerald-700 dark:text-emerald-300 mb-2 -mt-1">
            Değişiklik ters tepti mi? Yanındaki <span className="font-mono">↶ Geri al</span> ile tek tıkla eski hale döndür.
          </div>
          <div className="space-y-2">
            {analiz.son_uygulama!.map(u => (
              <div key={u.yedek_id} className={`text-sm ${u.geri_alindi ? 'opacity-50 line-through' : ''}`}>
                <div className="flex items-baseline justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    <div className="flex flex-wrap gap-1.5">
                      {u.uygulananlar.split(', ').map((kv, i) => (
                        <span key={i} className="font-mono text-[11px] rounded bg-white/70 dark:bg-slate-800/70 border border-emerald-200 dark:border-emerald-900/50 px-1.5 py-0.5 text-emerald-800 dark:text-emerald-200">{kv}</span>
                      ))}
                    </div>
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    <span className="text-[10px] text-slate-500">{u.tarih}</span>
                    {u.geri_alindi
                      ? <span className="text-[10px] text-slate-400 italic">geri alındı</span>
                      : <button onClick={() => geriAlUygulama(u.yedek_id, u.uygulananlar)}
                          className="text-[10px] rounded-md border border-amber-300 dark:border-amber-800 text-amber-700 dark:text-amber-200 hover:bg-amber-50 dark:hover:bg-amber-900/30 px-2 py-0.5 whitespace-nowrap">
                          ↶ Geri al
                        </button>}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {(analiz.log_sinyal?.length || 0) > 0 && (
        <div className="mb-4 rounded-xl border border-amber-200 bg-amber-50 p-3 dark:border-amber-900/50 dark:bg-amber-950/20">
          <div className="flex items-center gap-1.5 text-xs font-semibold text-amber-900 dark:text-amber-200 uppercase tracking-wider mb-1.5">
            <IcSearch className="w-3.5 h-3.5" /> Log/Metric sinyalleri
          </div>
          <ul className="text-sm text-amber-800 dark:text-amber-100 space-y-0.5 list-disc pl-5">
            {analiz.log_sinyal.map((s, i) => <li key={i}>{s}</li>)}
          </ul>
        </div>
      )}

      {sirali.length === 0 && !analiz.not_yok && (
        <div className="py-8 text-center text-sm text-slate-500">
          <div className="text-2xl mb-2 opacity-40">✨</div>
          Bu servis için öneri yok — mevcut ayarlar optimum.
        </div>
      )}

      {sirali.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-[11px] uppercase tracking-wider text-slate-500 border-b border-slate-200 dark:border-slate-800">
              <tr>
                <th className="py-2 pr-2 w-8"></th>
                <th className="py-2 pr-3 text-left">Parametre</th>
                <th className="py-2 pr-3 text-left">Mevcut</th>
                <th className="py-2 pr-3 text-left">Önerilen</th>
                <th className="py-2 pr-3 text-left">Gerekçe</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {sirali.map(o => (
                <tr key={o.id} className="hover:bg-slate-50 dark:hover:bg-slate-800/40">
                  <td className="py-2 pr-2">
                    <input type="checkbox" checked={secili.has(o.id)} onChange={() => toggle(o.id)}
                      className="w-4 h-4 rounded border-slate-300 text-emerald-600 focus:ring-emerald-500" />
                  </td>
                  <td className="py-2 pr-3">
                    <div className="font-medium text-slate-900 dark:text-slate-100">{o.etiket}</div>
                    <div className="text-[11px] text-slate-500 font-mono">{o.param}</div>
                  </td>
                  <td className="py-2 pr-3 font-mono text-xs text-slate-600 dark:text-slate-400">{o.mevcut || '—'}</td>
                  <td className="py-2 pr-3">
                    <span className="font-mono text-xs bg-emerald-50 dark:bg-emerald-950/40 text-emerald-800 dark:text-emerald-200 rounded px-1.5 py-0.5">{o.onerilen}</span>
                  </td>
                  <td className="py-2 pr-3 text-xs text-slate-600 dark:text-slate-400">
                    <div className="flex items-start gap-1.5">
                      <SeviyeNokta s={o.seviye} />
                      <span>{o.gerekce}</span>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {sirali.length > 0 && (
        <div className="mt-3 text-[11px] text-slate-500">
          Hedef dosya: <code className="font-mono">{sirali[0].dosya}</code> · Etki: {sirali[0].etki}
        </div>
      )}
    </div>
  )
}

/* ---------- Yedekler ---------- */

function YedeklerBolumu() {
  const [yedekler, setYedekler] = useState<YedekKayit[] | null>(null)
  const [acik, setAcik] = useState(false)
  const yukle = async () => {
    try { const r = await api.get<{ items: YedekKayit[] }>('/optimize/yedekler'); setYedekler(r.data.items || []) } catch { /* */ }
  }
  useEffect(() => { if (acik) void yukle() }, [acik])

  const geriYukle = async (id: number) => {
    if (!confirm('Bu yedeği geri yükle ve servisi reload et?')) return
    try {
      await api.post('/optimize/rollback', { yedek_id: id })
      await yukle()
    } catch (e: any) { alert('Hata: ' + (e?.response?.data?.hata || 'bilinmeyen')) }
  }

  return (
    <section className="mt-6 rounded-2xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
      <button onClick={() => setAcik(a => !a)} className="w-full flex items-center justify-between p-4 text-left">
        <div className="flex items-center gap-2">
          <IcArchive className="w-5 h-5 text-slate-500" />
          <span className="font-medium">Yedek geçmişi</span>
          {yedekler && <span className="text-xs text-slate-500">({yedekler.length})</span>}
        </div>
        <span className="text-slate-400">{acik ? '▴' : '▾'}</span>
      </button>
      {acik && yedekler && (
        <div className="border-t border-slate-200 dark:border-slate-800 p-4">
          {yedekler.length === 0 ? (
            <div className="text-sm text-slate-500 text-center py-4">Henüz yedek yok — henüz hiçbir öneri uygulanmamış.</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="text-[11px] uppercase tracking-wider text-slate-500 border-b border-slate-200 dark:border-slate-800">
                  <tr><th className="py-2 pr-3 text-left">Servis</th><th className="py-2 pr-3 text-left">Hedef</th><th className="py-2 pr-3 text-left">Yedek yolu</th><th className="py-2 pr-3 text-left">Tarih</th><th className="py-2 text-right">İşlem</th></tr>
                </thead>
                <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                  {yedekler.map(y => (
                    <tr key={y.id}>
                      <td className="py-2 pr-3"><span className="text-xs bg-slate-100 dark:bg-slate-800 rounded px-2 py-0.5 uppercase">{y.servis}</span></td>
                      <td className="py-2 pr-3 font-mono text-[11px] text-slate-600 truncate max-w-[280px]">{y.hedef}</td>
                      <td className="py-2 pr-3 font-mono text-[11px] text-slate-500 truncate max-w-[280px]">{y.yedek}</td>
                      <td className="py-2 pr-3 text-xs text-slate-500">{y.created_at}</td>
                      <td className="py-2 text-right">
                        {y.rolled_back
                          ? <span className="text-xs text-slate-400">geri alındı</span>
                          : <button onClick={() => geriYukle(y.id)} className="text-xs rounded-md border border-amber-200 dark:border-amber-800 text-amber-700 dark:text-amber-200 hover:bg-amber-50 dark:hover:bg-amber-950/30 px-2 py-1">Geri yükle</button>}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

/* ---------- Ortak ---------- */

type Renk = 'blue' | 'emerald' | 'amber' | 'red' | 'violet' | 'slate'
const renkler: Record<Renk, { border: string; bg: string; deger: string; bar: string; ikonBg: string }> = {
  blue: { border: 'border-blue-200 dark:border-blue-900/50', bg: 'bg-gradient-to-br from-blue-50 to-white dark:from-blue-950/30 dark:to-slate-900', deger: 'text-blue-950 dark:text-blue-100', bar: 'bg-blue-500', ikonBg: 'bg-blue-100 dark:bg-blue-900/40' },
  emerald: { border: 'border-emerald-200 dark:border-emerald-900/50', bg: 'bg-gradient-to-br from-emerald-50 to-white dark:from-emerald-950/30 dark:to-slate-900', deger: 'text-emerald-950 dark:text-emerald-100', bar: 'bg-emerald-500', ikonBg: 'bg-emerald-100 dark:bg-emerald-900/40' },
  amber: { border: 'border-amber-200 dark:border-amber-900/50', bg: 'bg-gradient-to-br from-amber-50 to-white dark:from-amber-950/30 dark:to-slate-900', deger: 'text-amber-950 dark:text-amber-100', bar: 'bg-amber-500', ikonBg: 'bg-amber-100 dark:bg-amber-900/40' },
  red: { border: 'border-red-200 dark:border-red-900/50', bg: 'bg-gradient-to-br from-red-50 to-white dark:from-red-950/30 dark:to-slate-900', deger: 'text-red-950 dark:text-red-100', bar: 'bg-red-500', ikonBg: 'bg-red-100 dark:bg-red-900/40' },
  violet: { border: 'border-violet-200 dark:border-violet-900/50', bg: 'bg-gradient-to-br from-violet-50 to-white dark:from-violet-950/30 dark:to-slate-900', deger: 'text-violet-950 dark:text-violet-100', bar: 'bg-violet-500', ikonBg: 'bg-violet-100 dark:bg-violet-900/40' },
  slate: { border: 'border-slate-200 dark:border-slate-800', bg: 'bg-white dark:bg-slate-900', deger: 'text-slate-900 dark:text-slate-100', bar: 'bg-slate-500', ikonBg: 'bg-slate-100 dark:bg-slate-800' },
}

function PanoKart({ ikon, etiket, deger, renk, trend, altBilgi }: { ikon: React.ReactNode; etiket: string; deger: string; renk: Renk; trend?: number; altBilgi?: string }) {
  const s = renkler[renk]
  return (
    <div className={`rounded-xl border ${s.border} ${s.bg} p-3`}>
      <div className="flex items-center justify-between mb-1.5">
        <div className={`w-9 h-9 rounded-lg ${s.ikonBg} ${s.deger} flex items-center justify-center`}>{ikon}</div>
        <div className="text-[10px] uppercase tracking-wider text-slate-500">{etiket}</div>
      </div>
      <div className={`text-lg font-bold ${s.deger} tabular-nums`}>{deger}</div>
      {altBilgi && <div className="text-[10px] text-slate-500 truncate">{altBilgi}</div>}
      {trend !== undefined && trend >= 0 && (
        <div className="mt-1.5 h-1 rounded-full bg-slate-200 dark:bg-slate-800 overflow-hidden">
          <div className={`h-full ${s.bar}`} style={{ width: `${Math.min(100, trend)}%` }} />
        </div>
      )}
    </div>
  )
}

function SeviyeNokta({ s }: { s: Seviye }) {
  const renk = s === 'kritik' ? 'bg-red-500' : s === 'onemli' ? 'bg-amber-500' : 'bg-slate-300'
  return <span className={`inline-block h-1.5 w-1.5 rounded-full ${renk} mt-1.5 shrink-0`}></span>
}

function profilAd(k: string) {
  return k === 'kucuk' ? 'Küçük' : k === 'orta' ? 'Orta' : k === 'buyuk' ? 'Büyük' : k === 'cok_buyuk' ? 'Çok büyük' : k
}
function formatUptime(sn: number): string {
  const g = Math.floor(sn / 86400); const s = Math.floor((sn % 86400) / 3600)
  if (g > 0) return `${g}g ${s}s`
  return `${s}s`
}

/* ---------- Servis brand logoları — SimpleIcons CDN + kernel için tux ---------- */

function ServisLogo({ kod, boy = 20 }: { kod: string; boy?: number }) {
  const boyStil = { width: boy, height: boy } as React.CSSProperties
  // Simple Icons: brand rengiyle otomatik render (`/mariadb` → orijinal renk)
  if (kod === 'mariadb') return <img src="https://cdn.simpleicons.org/mariadb/003545" alt="MariaDB" style={boyStil} className="dark:invert-[0.85]" />
  if (kod === 'nginx')   return <img src="https://cdn.simpleicons.org/nginx/009639"   alt="Nginx"   style={boyStil} />
  if (kod === 'apache')  return <img src="https://cdn.simpleicons.org/apache/D22128"  alt="Apache"  style={boyStil} />
  if (kod === 'phpfpm')  return <img src="https://cdn.simpleicons.org/php/777BB4"     alt="PHP"     style={boyStil} />
  if (kod === 'sysctl')  return <IcCog width={boy} height={boy} className="text-slate-600 dark:text-slate-300" />
  return null
}

/* ---------- Pano ikonları (heroicons inline) ---------- */

type SvgP = React.SVGProps<SVGSVGElement>
const svgProps: SvgP = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.7, strokeLinecap: 'round', strokeLinejoin: 'round', viewBox: '0 0 24 24' }

function IcChip(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={`w-5 h-5 ${p.className || ''}`}>
      <rect x="6.5" y="6.5" width="11" height="11" rx="1.5" />
      <path d="M9.5 6.5V4M14.5 6.5V4M9.5 20V17.5M14.5 20V17.5M6.5 9.5H4M6.5 14.5H4M20 9.5H17.5M20 14.5H17.5" />
      <rect x="9.5" y="9.5" width="5" height="5" rx="0.5" />
    </svg>
  )
}
function IcBolt(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={`w-5 h-5 ${p.className || ''}`}>
      <path d="M3.75 13.5l10.5-11.25L12 10.5h8.25L9.75 21.75 12 13.5H3.75z" />
    </svg>
  )
}
function IcSwap(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={`w-5 h-5 ${p.className || ''}`}>
      <path d="M7.5 21L3 16.5m0 0L7.5 12M3 16.5h13.5m0-13L21 7.5m0 0L16.5 12M21 7.5H7.5" />
    </svg>
  )
}
function IcDisk(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={`w-5 h-5 ${p.className || ''}`}>
      <ellipse cx="12" cy="6.5" rx="8" ry="3" />
      <path d="M4 6.5v11c0 1.7 3.6 3 8 3s8-1.3 8-3v-11" />
      <path d="M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3" />
    </svg>
  )
}
function IcServer(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={`w-5 h-5 ${p.className || ''}`}>
      <rect x="3" y="4" width="18" height="7" rx="1.5" />
      <rect x="3" y="13" width="18" height="7" rx="1.5" />
      <circle cx="7" cy="7.5" r="0.7" fill="currentColor" />
      <circle cx="7" cy="16.5" r="0.7" fill="currentColor" />
      <path d="M10 7.5h8M10 16.5h8" />
    </svg>
  )
}
function IcBulb(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={`w-5 h-5 ${p.className || ''}`}>
      <path d="M12 2.5a6 6 0 00-3.5 10.9V16a1 1 0 001 1h5a1 1 0 001-1v-2.6A6 6 0 0012 2.5z" />
      <path d="M10 20h4M10.5 22h3" />
    </svg>
  )
}
function IcSearch(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={p.className || ''}>
      <circle cx="10.5" cy="10.5" r="6.5" />
      <path d="M21 21l-6-6" />
    </svg>
  )
}
function IcArchive(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={p.className || ''}>
      <rect x="3" y="4" width="18" height="4" rx="1" />
      <path d="M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8" />
      <path d="M10 12h4" />
    </svg>
  )
}
function IcCog(p: SvgP) {
  return (
    <svg {...svgProps} {...p} className={p.className || ''}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33h0a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51h0a1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82v0a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z" />
    </svg>
  )
}
