import type { FormEvent } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

/*
 * Host Uygulamaları — admin only.
 *
 * Docker-siz native app kurucu. Katalog'tan bir recipe seç → tek tıkla
 * indir + SHA doğrula + system user + systemd hardening + nginx proxy +
 * start + healthcheck. Fail → tam rollback.
 *
 * Faz 3.1 MVP.
 */

type PortTarifi = { ad: string; protokol: string; zorunlu: number }
type NginxProxy = { subdomain: boolean; sub_path_on: string; upgrade_ws: boolean }
type Tarif = {
  Kod: string; Ad: string; Aciklama: string; Kategori: string; Ikon: string; Surum: string; logo_url?: string
  IndirmeURL: string; SHA256: string; IcerikTuru: string
  Portlar: PortTarifi[]; NginxProxy: NginxProxy | null
  hazir?: boolean // KatalogSatir.Hazir — false ise UI'da "yakında" göster
}
type PortKayit = { port: number; protokol: string; aciklama: string; firewall_acik: boolean }
type Uygulama = {
  id: number; kod: string; ornek_ad: string; surum: string; kurulum_yolu: string
  sistem_kullanici: string; systemd_unit: string; durum: string; son_hata: string
  created_at: string; portlar?: PortKayit[]; unit_durumu?: string
}
type Adim = { zaman: string; mesaj: string; basari: boolean }
type Is = {
  id: string; tip: string; kod: string; uygulama_id?: number; durum: string
  basla: string; bitis?: string; hata?: string; adimlar: Adim[]
}
type Metrik = {
  uygulama_id: number; unit_ad: string
  bellek_byte: number; bellek_peak_byte: number
  cpu_toplam_usec: number; cpu_yuzde: number
  task_sayisi: number; task_max: number
  disk_byte: number
  aktif_durum: string; alt_durum: string
  uptime?: string; restart_sayi: number
  zaman: string
}
type Yedek = {
  id: number; uygulama_id: number; dosya: string
  boyut_byte: number; sha256: string
  aciklama: string; olusturma: string
}

export default function HostUygulamalarPage() {
  const [katalog, setKatalog] = useState<Tarif[]>([])
  const [kurulu, setKurulu] = useState<Uygulama[]>([])
  const [yukleniyor, setYukleniyor] = useState(true)
  const [aktifIs, setAktifIs] = useState<Is | null>(null)
  const [kurTaslak, setKurTaslak] = useState<{ kod: string; ornek_ad: string } | null>(null)
  const [gonderiliyor, setGonderiliyor] = useState(false)
  const [logKayit, setLogKayit] = useState<{ id: number; kod: string; log: string } | null>(null)
  const [logYukleniyor, setLogYukleniyor] = useState(false)
  const nav = useNavigate()
  const [metrikKayit, setMetrikKayit] = useState<{ id: number; kod: string; m: Metrik | null } | null>(null)
  const [yedekKayit, setYedekKayit] = useState<{ id: number; kod: string; items: Yedek[] } | null>(null)
  const [yedekIslem, setYedekIslem] = useState(false)
  const dialog = useDialog()

  const yedekGoster = async (u: Uygulama) => {
    setYedekKayit({ id: u.id, kod: u.ornek_ad, items: [] })
    try {
      const r = await api.get<{ items: Yedek[] }>(`/hostuyg/${u.id}/yedekler`)
      setYedekKayit({ id: u.id, kod: u.ornek_ad, items: r.data.items || [] })
    } catch (e) {
      await dialog.bilgi({ baslik: 'Yedekler yüklenemedi', mesaj: apiHata(e, '') })
    }
  }
  const yedekAl = async (u: Uygulama) => {
    const ac = window.prompt(`${u.ornek_ad} için yedek açıklaması (opsiyonel):`, '')
    if (ac === null) return
    setYedekIslem(true)
    try {
      const r = await api.post<{ is_id: string }>(`/hostuyg/${u.id}/yedek`, { aciklama: ac })
      // Async — is polling ile bitmesini bekle
      setAktifIs({ id: r.data.is_id, tip: 'yedek', kod: u.ornek_ad, durum: 'kosuyor',
        basla: new Date().toISOString(), adimlar: [] })
      for (let i = 0; i < 1200; i++) { // 30dk max (Prometheus TSDB büyük olabilir)
        await new Promise((r) => setTimeout(r, 1500))
        const s = await api.get<Is>(`/hostuyg/is/${r.data.is_id}`)
        setAktifIs(s.data)
        if (s.data.durum !== 'kosuyor') break
      }
      await yedekGoster(u)
    } catch (e) {
      await dialog.bilgi({ baslik: 'Yedek alınamadı', mesaj: apiHata(e, '') })
    } finally { setYedekIslem(false) }
  }
  const yedekRestore = async (u: Uygulama, y: Yedek) => {
    const ok = await dialog.onay({
      baslik: `${u.ornek_ad}'i yedeğe geri yükle?`,
      mesaj: `Servis durdurulur, mevcut kurulum ${new Date(y.olusturma).toLocaleString()} anındaki durumuna döner. Fail durumunda otomatik rollback.`,
      onayEtiketi: 'Geri Yükle', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    setYedekIslem(true)
    try {
      const r = await api.post<{ is_id: string }>(`/hostuyg/${u.id}/yedek/geriyukle`, { yedek_id: y.id })
      // Async — is polling ile bitmesini bekle (30dk max, aktifIs banner göster)
      setAktifIs({ id: r.data.is_id, tip: 'restore', kod: u.ornek_ad, durum: 'kosuyor',
        basla: new Date().toISOString(), adimlar: [] })
      let sonSnap: Is | null = null
      for (let i = 0; i < 1200; i++) { // 30dk = 1200 × 1.5s
        await new Promise((r) => setTimeout(r, 1500))
        const s = await api.get<Is>(`/hostuyg/is/${r.data.is_id}`)
        setAktifIs(s.data)
        sonSnap = s.data
        if (s.data.durum !== 'kosuyor') break
      }
      if (sonSnap?.durum === 'bitti') {
        await dialog.bilgi({ baslik: 'Geri yüklendi', mesaj: 'Servis aktif' })
        setYedekKayit(null)
        await yukle()
      } else {
        await dialog.bilgi({ baslik: 'Geri yükleme başarısız', mesaj: sonSnap?.hata || 'bilinmeyen hata' })
      }
    } catch (e) {
      await dialog.bilgi({ baslik: 'Geri yükleme başarısız', mesaj: apiHata(e, '') })
    } finally { setYedekIslem(false) }
  }
  const yedekSil = async (y: Yedek) => {
    const ok = await dialog.onay({
      baslik: 'Yedek silinsin mi?',
      mesaj: `${new Date(y.olusturma).toLocaleString()} yedeği kalıcı silinir.`,
      onayEtiketi: 'Sil', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    try {
      await api.delete(`/hostuyg/yedek/${y.id}`)
      if (yedekKayit) setYedekKayit({ ...yedekKayit, items: yedekKayit.items.filter((x) => x.id !== y.id) })
    } catch (e) { await dialog.bilgi({ baslik: 'Silinemedi', mesaj: apiHata(e, '') }) }
  }

  const metrikGoster = async (u: Uygulama) => {
    setMetrikKayit({ id: u.id, kod: u.ornek_ad, m: null })
    try {
      const r = await api.get<Metrik>(`/hostuyg/${u.id}/metrik`)
      setMetrikKayit({ id: u.id, kod: u.ornek_ad, m: r.data })
    } catch (e) {
      await dialog.bilgi({ baslik: 'Metrik alınamadı', mesaj: apiHata(e, '') })
      setMetrikKayit(null)
    }
  }

  const byteFmt = (b: number): string => {
    if (!b) return '—'
    if (b < 1024) return b + 'B'
    if (b < 1024 * 1024) return (b / 1024).toFixed(1) + 'KB'
    if (b < 1024 * 1024 * 1024) return (b / 1024 / 1024).toFixed(1) + 'MB'
    return (b / 1024 / 1024 / 1024).toFixed(2) + 'GB'
  }
  const taskFmt = (max: number): string => max < 0 ? '∞' : (max === 0 ? '—' : String(max))

  const logGoster = async (u: Uygulama) => {
    setLogKayit({ id: u.id, kod: u.ornek_ad, log: '' })
    setLogYukleniyor(true)
    try {
      const r = await api.get<{ log: string }>(`/hostuyg/${u.id}/log?satir=200`)
      setLogKayit({ id: u.id, kod: u.ornek_ad, log: r.data.log || '(log boş)' })
    } catch (e) {
      setLogKayit({ id: u.id, kod: u.ornek_ad, log: 'Log alınamadı: ' + apiHata(e, '') })
    } finally { setLogYukleniyor(false) }
  }

  const yukle = async () => {
    try {
      const [k, l] = await Promise.all([
        api.get<{ items: Tarif[] }>('/hostuyg/katalog'),
        api.get<{ items: Uygulama[] }>('/hostuyg'),
      ])
      setKatalog(k.data.items || [])
      setKurulu(l.data.items || [])
    } finally { setYukleniyor(false) }
  }
  useEffect(() => { void yukle() }, [])

  // Aktif iş varsa poll et
  useEffect(() => {
    if (!aktifIs || aktifIs.durum !== 'kosuyor') return
    const t = setInterval(async () => {
      try {
        const r = await api.get<Is>(`/hostuyg/is/${aktifIs.id}`)
        setAktifIs(r.data)
        if (r.data.durum !== 'kosuyor') {
          clearInterval(t)
          await yukle()
        }
      } catch { /* poll fail — bırak */ }
    }, 1500)
    return () => clearInterval(t)
  }, [aktifIs?.id, aktifIs?.durum])

  const kurBaslat = async (e: FormEvent) => {
    e.preventDefault()
    if (!kurTaslak) return
    setGonderiliyor(true)
    try {
      const r = await api.post<{ is_id: string }>('/hostuyg', kurTaslak)
      // Iş id'yi hemen fetch et
      const s = await api.get<Is>(`/hostuyg/is/${r.data.is_id}`)
      setAktifIs(s.data)
      setKurTaslak(null)
    } catch (e) {
      await dialog.bilgi({ baslik: 'Kurulum başlatılamadı', mesaj: apiHata(e, 'İstek başarısız') })
    } finally { setGonderiliyor(false) }
  }

  const baslat = async (u: Uygulama) => {
    try { await api.post(`/hostuyg/${u.id}/baslat`, {}); await yukle() }
    catch (e) { await dialog.bilgi({ baslik: 'Başlatılamadı', mesaj: apiHata(e, 'Hata') }) }
  }
  const durdur = async (u: Uygulama) => {
    try { await api.post(`/hostuyg/${u.id}/durdur`, {}); await yukle() }
    catch (e) { await dialog.bilgi({ baslik: 'Durdurulamadı', mesaj: apiHata(e, 'Hata') }) }
  }
  const restart = async (u: Uygulama) => {
    try { await api.post(`/hostuyg/${u.id}/restart`, {}); await yukle() }
    catch (e) { await dialog.bilgi({ baslik: 'Restart başarısız', mesaj: apiHata(e, 'Hata') }) }
  }
  const kaldir = async (u: Uygulama) => {
    const ok = await dialog.onay({
      baslik: `${u.ornek_ad} kaldırılsın mı?`,
      mesaj: `Servis durdurulur, sistem user + dizin + DB kaydı silinir, nginx proxy kaldırılır, firewall kural silinir. Kalıcı olarak geri alınamaz.`,
      onayEtiketi: 'Kaldır', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    try {
      const r = await api.delete<{ is_id: string }>(`/hostuyg/${u.id}`)
      const s = await api.get<Is>(`/hostuyg/is/${r.data.is_id}`)
      setAktifIs(s.data)
    } catch (e) { await dialog.bilgi({ baslik: 'Kaldırılamadı', mesaj: apiHata(e, 'Hata') }) }
  }

  const kategoriler = useMemo(() => {
    const set = new Set(katalog.map((t) => t.Kategori))
    return Array.from(set).sort()
  }, [katalog])
  const [seciliKategori, setSeciliKategori] = useState<string>('')

  const gorunenKatalog = useMemo(() =>
    seciliKategori ? katalog.filter((t) => t.Kategori === seciliKategori) : katalog,
    [katalog, seciliKategori])

  return (
    <div className="mx-auto max-w-6xl px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { href: '/araclar-ayarlar', etiket: 'Araçlar ve Ayarlar' },
        { etiket: 'Host Uygulamaları' },
      ]} />

      <div className="mt-4 mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Host Uygulamaları</h1>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
          Sunucuya Docker-siz native uygulama kur (systemd hardening + nginx proxy + otomatik rollback).
          <span className="ml-1 font-medium">Yalnız admin.</span>
        </p>
      </div>

      {yukleniyor ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">Yükleniyor…</div>
      ) : (
        <>
          {/* Aktif iş */}
          {aktifIs && (
            <div className={`mb-6 rounded-2xl border p-4 ${
              aktifIs.durum === 'kosuyor' ? 'border-amber-300 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/30' :
              aktifIs.durum === 'bitti'   ? 'border-emerald-300 bg-emerald-50 dark:border-emerald-800 dark:bg-emerald-950/30' :
                                            'border-red-300 bg-red-50 dark:border-red-800 dark:bg-red-950/30'
            }`}>
              <div className="mb-2 flex items-center justify-between text-sm font-medium">
                <span>
                  {aktifIs.durum === 'kosuyor' && (
                    <span className="mr-2 inline-block h-2 w-2 animate-pulse rounded-full bg-amber-500" />
                  )}
                  {aktifIs.tip === 'kur' ? 'Kurulum' : 'Kaldırma'}: <span className="font-mono">{aktifIs.kod}</span> — {aktifIs.durum}
                </span>
                {aktifIs.durum !== 'kosuyor' && (
                  <button onClick={() => setAktifIs(null)} className="text-xs text-slate-500 hover:text-slate-700">kapat</button>
                )}
              </div>
              <ol className="space-y-0.5 text-xs">
                {aktifIs.adimlar.map((a, i) => (
                  <li key={i} className={a.basari ? 'text-emerald-700 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}>
                    {a.basari ? '✓' : '✗'} {a.mesaj}
                  </li>
                ))}
              </ol>
              {aktifIs.hata && <p className="mt-2 text-xs font-medium text-red-600 dark:text-red-400">Hata: {aktifIs.hata}</p>}
            </div>
          )}

          {/* Kurulu uygulamalar */}
          <section className="mb-8">
            <h2 className="mb-3 text-sm font-semibold uppercase tracking-wider text-slate-500">Kurulu ({kurulu.length})</h2>
            {kurulu.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-slate-300 py-8 text-center text-sm text-slate-500 dark:border-slate-700">
                Henüz uygulama kurulu değil. Aşağıdaki katalogdan seç.
              </div>
            ) : (
              <div className="overflow-hidden rounded-2xl border border-slate-200 dark:border-slate-800">
                <table className="w-full text-left text-sm">
                  <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                    <tr>
                      <th className="px-3 py-3 font-semibold">Kod / Örnek</th>
                      <th className="px-3 py-3 font-semibold">Durum</th>
                      <th className="px-3 py-3 font-semibold">Portlar</th>
                      <th className="px-3 py-3 font-semibold">Sürüm</th>
                      <th className="px-3 py-3 text-right font-semibold">İşlem</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {kurulu.map((u) => (
                      <tr key={u.id} className="bg-white dark:bg-slate-950">
                        <td className="px-3 py-2.5 font-mono text-xs">
                          <div className="font-medium text-slate-900 dark:text-slate-100">{u.ornek_ad}</div>
                          <div className="text-slate-500">{u.kod}</div>
                        </td>
                        <td className="px-3 py-2.5">
                          <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                            u.durum === 'aktif'     ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' :
                            u.durum === 'durduruldu' ? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' :
                            u.durum === 'kurulmakta' ? 'bg-amber-100 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300' :
                                                       'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300'
                          }`}>{u.durum}</span>
                          {u.unit_durumu && u.unit_durumu !== 'active' && u.durum === 'aktif' && (
                            <span className="ml-2 text-[10px] text-red-500">(unit={u.unit_durumu})</span>
                          )}
                        </td>
                        <td className="px-3 py-2.5 font-mono text-xs text-slate-600 dark:text-slate-400">
                          {u.portlar?.map((p) => `${p.port}/${p.protokol}`).join(', ') || '—'}
                        </td>
                        <td className="px-3 py-2.5 font-mono text-xs text-slate-500">{u.surum}</td>
                        <td className="px-3 py-2.5 text-right">
                          <button onClick={() => metrikGoster(u)}
                            className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800">Metrik</button>
                          <button onClick={() => logGoster(u)}
                            className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800">Log</button>
                          <button onClick={() => yedekGoster(u)}
                            className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800">Yedek</button>
                          {(u.durum === 'aktif' || u.durum === 'hata') && (
                            <button onClick={() => restart(u)} title="Restart (kurtarma denemesi)"
                              className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800">↻</button>
                          )}
                          {u.durum === 'aktif' ? (
                            <button onClick={() => durdur(u)}
                              className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800">Durdur</button>
                          ) : u.durum === 'durduruldu' && (
                            <button onClick={() => baslat(u)} className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-emerald-600 hover:bg-emerald-50 dark:text-emerald-400 dark:hover:bg-emerald-950/40">Başlat</button>
                          )}
                          <button onClick={() => kaldir(u)} className="rounded-md px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/40">Kaldır</button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>

          {/* Katalog */}
          <section>
            <div className="mb-3 flex items-center justify-between">
              <h2 className="text-sm font-semibold uppercase tracking-wider text-slate-500">Katalog ({gorunenKatalog.length})</h2>
              {kategoriler.length > 0 && (
                <select value={seciliKategori} onChange={(e) => setSeciliKategori(e.target.value)}
                  className="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs dark:border-slate-700 dark:bg-slate-950">
                  <option value="">Tüm kategoriler</option>
                  {kategoriler.map((k) => <option key={k} value={k}>{k}</option>)}
                </select>
              )}
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {gorunenKatalog.map((t) => (
                <div key={t.Kod} className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
                  <div className="mb-2 flex items-start gap-3">
                    <TarifLogo logoUrl={t.logo_url} ikon={t.Ikon} ad={t.Ad} />
                    <div className="flex-1">
                      <div className="font-medium text-slate-900 dark:text-slate-100">{t.Ad}</div>
                      <div className="text-[11px] font-mono text-slate-500">{t.Kod} · v{t.Surum}</div>
                    </div>
                  </div>
                  <p className="mb-3 text-xs leading-relaxed text-slate-600 dark:text-slate-400">{t.Aciklama}</p>
                  <div className="mb-3 flex flex-wrap gap-1 text-[10px]">
                    <span className="rounded bg-slate-100 px-1.5 py-0.5 text-slate-700 dark:bg-slate-800 dark:text-slate-300">{t.Kategori}</span>
                    {t.Portlar.map((p) => (
                      <span key={p.ad} className="rounded bg-slate-100 px-1.5 py-0.5 text-slate-700 dark:bg-slate-800 dark:text-slate-300">
                        {p.ad}/{p.protokol}
                      </span>
                    ))}
                    {t.NginxProxy && (
                      <span className="rounded bg-blue-100 px-1.5 py-0.5 text-blue-700 dark:bg-blue-950/40 dark:text-blue-300">nginx proxy</span>
                    )}
                    {t.hazir === false && (
                      <span className="rounded bg-amber-100 px-1.5 py-0.5 text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">yakında</span>
                    )}
                  </div>
                  {(() => {
                    const kuruluInstance = kurulu.find((u) => u.kod === t.Kod)
                    if (kuruluInstance) {
                      return (
                        <button onClick={() => nav(`/araclar/host-uygulamalari/${kuruluInstance.id}`)}
                          className="w-full rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-700">
                          Yönet →
                        </button>
                      )
                    }
                    return (
                      <button onClick={() => setKurTaslak({ kod: t.Kod, ornek_ad: t.Kod })}
                        disabled={aktifIs?.durum === 'kosuyor' || t.hazir === false}
                        title={t.hazir === false ? 'Bu recipe henüz hazır değil' : ''}
                        className="w-full rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-800 disabled:opacity-50 disabled:cursor-not-allowed dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white">
                        {t.hazir === false ? 'Yakında' : 'Kur'}
                      </button>
                    )
                  })()}
                </div>
              ))}
            </div>
          </section>
        </>
      )}

      {/* Yedek modal */}
      {yedekKayit && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          onClick={(e) => { if (e.target === e.currentTarget) setYedekKayit(null) }}>
          <div className="w-full max-w-2xl rounded-2xl bg-white p-5 shadow-xl dark:bg-slate-900">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-base font-semibold">Yedekler: <span className="font-mono">{yedekKayit.kod}</span></h3>
              <div className="flex items-center gap-2">
                <button onClick={() => { const u = kurulu.find((k) => k.id === yedekKayit.id); if (u) void yedekAl(u) }}
                  disabled={yedekIslem}
                  className="rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900">
                  {yedekIslem ? '…' : '+ Yedek Al'}
                </button>
                <button onClick={() => setYedekKayit(null)} className="text-slate-500 hover:text-slate-700">✕</button>
              </div>
            </div>
            {yedekKayit.items.length === 0 ? (
              <div className="rounded-lg border border-dashed border-slate-300 py-8 text-center text-sm text-slate-500 dark:border-slate-700">
                Henüz yedek yok. "+ Yedek Al" ile başlat.
              </div>
            ) : (
              <div className="overflow-hidden rounded-lg border border-slate-200 dark:border-slate-800">
                <table className="w-full text-left text-sm">
                  <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                    <tr>
                      <th className="px-3 py-2">Tarih</th>
                      <th className="px-3 py-2">Boyut</th>
                      <th className="px-3 py-2">Açıklama</th>
                      <th className="px-3 py-2 text-right">İşlem</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {yedekKayit.items.map((y) => (
                      <tr key={y.id} className="bg-white dark:bg-slate-950">
                        <td className="px-3 py-2 font-mono text-xs">{new Date(y.olusturma).toLocaleString('tr-TR')}</td>
                        <td className="px-3 py-2 font-mono text-xs">{byteFmt(y.boyut_byte)}</td>
                        <td className="px-3 py-2 text-xs text-slate-600">{y.aciklama || '—'}</td>
                        <td className="px-3 py-2 text-right">
                          <button onClick={() => { const u = kurulu.find((k) => k.id === yedekKayit.id); if (u) void yedekRestore(u, y) }}
                            disabled={yedekIslem}
                            className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-emerald-600 hover:bg-emerald-50 disabled:opacity-50 dark:text-emerald-400 dark:hover:bg-emerald-950/40">Geri Yükle</button>
                          <button onClick={() => yedekSil(y)}
                            className="rounded-md px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/40">Sil</button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <p className="mt-3 text-[11px] text-slate-500">Son 5 yedek tutulur (rotasyon). Yedek al servisi kısa süre durdurur.</p>
          </div>
        </div>
      )}

      {/* Metrik modal */}
      {metrikKayit && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          onClick={(e) => { if (e.target === e.currentTarget) setMetrikKayit(null) }}>
          <div className="w-full max-w-lg rounded-2xl bg-white p-5 shadow-xl dark:bg-slate-900">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="text-base font-semibold">Metrik: <span className="font-mono">{metrikKayit.kod}</span></h3>
              <button onClick={() => setMetrikKayit(null)} className="text-slate-500 hover:text-slate-700">✕</button>
            </div>
            {!metrikKayit.m ? (
              <div className="py-8 text-center text-sm text-slate-500">Yükleniyor…</div>
            ) : (
              <div className="grid grid-cols-2 gap-3 text-sm">
                <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-800">
                  <div className="text-[11px] uppercase tracking-wider text-slate-500">Durum</div>
                  <div className="mt-1 font-mono">{metrikKayit.m.aktif_durum} / {metrikKayit.m.alt_durum}</div>
                </div>
                <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-800">
                  <div className="text-[11px] uppercase tracking-wider text-slate-500">Uptime</div>
                  <div className="mt-1 font-mono">{metrikKayit.m.uptime || '—'}</div>
                </div>
                <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-800">
                  <div className="text-[11px] uppercase tracking-wider text-slate-500">Bellek</div>
                  <div className="mt-1 font-mono">{byteFmt(metrikKayit.m.bellek_byte)}</div>
                  <div className="text-[10px] text-slate-500">peak: {byteFmt(metrikKayit.m.bellek_peak_byte)}</div>
                </div>
                <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-800">
                  <div className="text-[11px] uppercase tracking-wider text-slate-500">CPU %</div>
                  <div className="mt-1 font-mono">{metrikKayit.m.cpu_yuzde.toFixed(2)}%</div>
                  <div className="text-[10px] text-slate-500">toplam: {(metrikKayit.m.cpu_toplam_usec / 1e6).toFixed(1)}s</div>
                </div>
                <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-800">
                  <div className="text-[11px] uppercase tracking-wider text-slate-500">Task</div>
                  <div className="mt-1 font-mono">{metrikKayit.m.task_sayisi} / {taskFmt(metrikKayit.m.task_max)}</div>
                </div>
                <div className="rounded-lg border border-slate-200 p-3 dark:border-slate-800">
                  <div className="text-[11px] uppercase tracking-wider text-slate-500">Disk</div>
                  <div className="mt-1 font-mono">{byteFmt(metrikKayit.m.disk_byte)}</div>
                </div>
                <div className="col-span-2 rounded-lg border border-slate-200 p-3 dark:border-slate-800">
                  <div className="text-[11px] uppercase tracking-wider text-slate-500">Yeniden başlatma</div>
                  <div className="mt-1 font-mono">{metrikKayit.m.restart_sayi} kez (systemd)</div>
                </div>
              </div>
            )}
            <div className="mt-4 flex justify-end gap-2">
              <button onClick={() => { const u = kurulu.find((k) => k.id === metrikKayit.id); if (u) void metrikGoster(u) }}
                className="rounded-lg px-3 py-1.5 text-xs text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800">Yenile</button>
              <button onClick={() => setMetrikKayit(null)}
                className="rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white dark:bg-slate-100 dark:text-slate-900">Kapat</button>
            </div>
          </div>
        </div>
      )}

      {/* Log modal */}
      {logKayit && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          onClick={(e) => { if (e.target === e.currentTarget) setLogKayit(null) }}>
          <div className="w-full max-w-4xl rounded-2xl bg-white p-4 shadow-xl dark:bg-slate-900">
            <div className="mb-3 flex items-center justify-between">
              <h3 className="text-base font-semibold">Log: <span className="font-mono">{logKayit.kod}</span></h3>
              <button onClick={() => setLogKayit(null)} className="text-slate-500 hover:text-slate-700">✕</button>
            </div>
            {logYukleniyor ? (
              <div className="py-8 text-center text-sm text-slate-500">Yükleniyor…</div>
            ) : (
              <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-slate-950 p-3 text-[11px] font-mono leading-tight text-slate-100">
                {logKayit.log}
              </pre>
            )}
            <div className="mt-3 flex justify-end gap-2">
              <button onClick={() => { const u = kurulu.find((k) => k.id === logKayit.id); if (u) void logGoster(u) }}
                className="rounded-lg px-3 py-1.5 text-xs text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800">Yenile</button>
              <button onClick={() => setLogKayit(null)}
                className="rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white dark:bg-slate-100 dark:text-slate-900">Kapat</button>
            </div>
          </div>
        </div>
      )}

      {/* Kur dialog */}
      {kurTaslak && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <form onSubmit={kurBaslat} className="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl dark:bg-slate-900">
            <h3 className="mb-4 text-lg font-semibold">Kur: {kurTaslak.kod}</h3>
            <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">Örnek adı (unique, aynı recipe'i birden fazla kez kurmak için)</label>
            <input value={kurTaslak.ornek_ad} onChange={(e) => setKurTaslak({ ...kurTaslak, ornek_ad: e.target.value })}
              className="mb-4 w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-mono text-sm dark:border-slate-700 dark:bg-slate-950"
              placeholder="ör. main, staging, test" />
            <div className="flex gap-2 justify-end">
              <button type="button" onClick={() => setKurTaslak(null)}
                className="rounded-lg px-4 py-2 text-sm text-slate-600 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800">Vazgeç</button>
              <button type="submit" disabled={gonderiliyor || !kurTaslak.ornek_ad}
                className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white">
                {gonderiliyor ? 'Başlatılıyor…' : 'Kur'}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  )
}


/* Katalog logosu — SimpleIcons SVG'si; yuklenemezse (CDN kapali, CSP engeli,
   slug yok) emoji'ye duser. Boylece ikon alani hicbir zaman bos kalmaz. */
function TarifLogo({ logoUrl, ikon, ad }: { logoUrl?: string; ikon: string; ad: string }) {
  const [hata, setHata] = useState(false)
  if (!logoUrl || hata) return <span className="text-2xl leading-none">{ikon}</span>
  return (
    <img
      src={logoUrl}
      alt={ad}
      width={28}
      height={28}
      loading="lazy"
      onError={() => setHata(true)}
      className="h-7 w-7 shrink-0 object-contain"
    />
  )
}
