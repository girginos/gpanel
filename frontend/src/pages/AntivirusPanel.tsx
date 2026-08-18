import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'

type Tarama = { id: number; alan_adi: string; durum: string; kaynak: string; taranan: number; enfekte: number; bitis: string }
type Durum = {
  ajan_kurulu: boolean; izleyici_aktif: boolean; slice_aktif: boolean
  kural_surum: number; kural_uretim: string; kural_sayisi: number
  toplam_karantina: number; toplam_bulgu: number; taranan_domain: number
  son_taramalar: Tarama[]
}
type Kar = { id: number; alan_adi: string; domain_id: number; orijinal_yol: string; imza: string; seviye: string; puan: number; durum: string; tarih: string; mevcut: boolean }
type Gecmis = { id: number; alan_adi: string; domain_id: number; dosya: string; imza: string; motor: string; seviye: string; puan: number; durum: string; tarih: string }
type Kara = { domain_id: number; alan_adi: string; durum: string; kaynak: string }
type Ayar = {
  gercek_zamanli: boolean; zamanli_tarama: boolean; wp_butunluk: boolean; kural_motoru: boolean
  konum_sezgileri: boolean; oto_karantina: boolean; esik_kritik: number; kapsam: string
  haric_yollar: string; cpu_yuzde: number; ram_mb: number; io_agirlik: number
  is_parcacigi: number; dosya_hiz_sn: number; zamanli_saat: string; yuk_esigi: number
}
type AyarYanit = { ayarlar: Ayar; kapasite: { cpu_cekirdek: number; ram_toplam_mb: number; oneri_cpu_yuzde: number; oneri_ram_mb: number; oneri_is_parcacigi: number } }

type Sekme = 'genel' | 'karantina' | 'gecmis' | 'itibar' | 'ayarlar'

function Anahtar({ acik, ayarla, etiket, aciklama, uyari }: { acik: boolean; ayarla: (v: boolean) => void; etiket: string; aciklama?: string; uyari?: boolean }) {
  return (
    <button type="button" onClick={() => ayarla(!acik)} className="flex items-start gap-3 text-left w-full py-1.5">
      <span className={`mt-0.5 relative inline-flex h-5 w-9 flex-shrink-0 rounded-full transition ${acik ? (uyari ? 'bg-amber-500' : 'bg-emerald-500') : 'bg-slate-300 dark:bg-slate-600'}`}>
        <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition ${acik ? 'left-4' : 'left-0.5'}`} />
      </span>
      <span className="min-w-0">
        <span className="block text-sm text-slate-800 dark:text-slate-100">{etiket}</span>
        {aciklama && <span className="block text-xs text-slate-400 dark:text-slate-500">{aciklama}</span>}
      </span>
    </button>
  )
}

const ANIM_CSS = `
@keyframes gosp-radar { 0% { transform: scale(.55); opacity:.7 } 100% { transform: scale(1.7); opacity:0 } }
@keyframes gosp-sweep { to { transform: rotate(360deg) } }
@keyframes gosp-draw  { to { stroke-dashoffset: 0 } }
@keyframes gosp-float { 0%,100% { transform: translateY(0) } 50% { transform: translateY(-5px) } }
@keyframes gosp-shimmer { 0% { transform: translateX(-100%) } 100% { transform: translateX(320%) } }
@keyframes gosp-spin { to { transform: rotate(360deg) } }
.gosp-ring  { transform-origin: 60px 60px; animation: gosp-radar 2.6s ease-out infinite }
.gosp-ring2 { animation-delay: 1.3s }
.gosp-sweepg { transform-origin: 60px 60px; animation: gosp-sweep 2.6s linear infinite }
.gosp-sweepg.fast { animation-duration: .9s }
.gosp-check { stroke-dasharray: 60; stroke-dashoffset: 60; animation: gosp-draw 1s ease-out .35s forwards }
.gosp-badge { animation: gosp-float 4.5s ease-in-out infinite }
.gosp-shimmer { animation: gosp-shimmer 1.3s ease-in-out infinite }
.gosp-modikon { transition: transform .2s }
.gosp-card:hover .gosp-modikon { transform: scale(1.12) rotate(-4deg) }
`

// Kalkan — animasyonlu güvenlik rozeti (radar pulse + tarama süpürme).
function Kalkan({ renk, tarama }: { renk: string; tarama: boolean }) {
  const c = renk === 'red' ? '#f87171' : renk === 'amber' ? '#fbbf24' : '#34d399'
  return (
    <svg viewBox="0 0 120 120" className="w-24 h-24 gosp-badge" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="60" cy="60" r="24" className="gosp-ring" stroke={c} strokeWidth="1.5" />
      <circle cx="60" cy="60" r="24" className="gosp-ring gosp-ring2" stroke={c} strokeWidth="1.5" />
      <defs>
        <linearGradient id="gospShield" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor={c} stopOpacity="0.28" />
          <stop offset="1" stopColor={c} stopOpacity="0.06" />
        </linearGradient>
        <radialGradient id="gospSweep" cx="0.5" cy="0.5" r="0.5">
          <stop offset="0" stopColor={c} stopOpacity="0.55" />
          <stop offset="1" stopColor={c} stopOpacity="0" />
        </radialGradient>
      </defs>
      <path d="M60 15 L96 28 V59 C96 81 79 98 60 105 C41 98 24 81 24 59 V28 Z" fill="url(#gospShield)" stroke={c} strokeWidth="2.5" strokeLinejoin="round" />
      {tarama && (
        <g className="gosp-sweepg fast">
          <path d="M60 60 L60 22 A38 38 0 0 1 93 46 Z" fill="url(#gospSweep)" />
        </g>
      )}
      <path d="M45 61 l10 10 l21 -24" stroke={c} strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" className="gosp-check" />
    </svg>
  )
}

const MODUL_SVG: Record<string, string> = {
  dosya: 'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z',
  realtime: 'M13 2L3 14h7l-1 8 10-12h-7l1-8z',
  db: 'M12 3c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3zM20 6v6c0 1.7-3.6 3-8 3s-8-1.3-8-3V6M20 12v6c0 1.7-3.6 3-8 3s-8-1.3-8-3v-6',
  slice: 'M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6',
}
function ModulIkon({ ad }: { ad: string }) {
  return (
    <svg viewBox="0 0 24 24" className="w-7 h-7 gosp-modikon" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">
      <path d={MODUL_SVG[ad] || MODUL_SVG.dosya} />
    </svg>
  )
}

export default function AntivirusPanel() {
  const { onay } = useDialog()
  const [d, setD] = useState<Durum | null>(null)
  const [kliste, setKliste] = useState<Kar[]>([])
  const [gecmis, setGecmis] = useState<Gecmis[]>([])
  const [kara, setKara] = useState<Kara[]>([])
  const [ayar, setAyar] = useState<Ayar | null>(null)
  const [kap, setKap] = useState<AyarYanit['kapasite'] | null>(null)
  const [sekme, setSekme] = useState<Sekme>('genel')
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [bilgi, setBilgi] = useState<string | null>(null)
  const [mesgul, setMesgul] = useState<string | null>(null) // hangi aksiyon calisiyor
  const [gelismis, setGelismis] = useState(false)
  const [inceleModal, setInceleModal] = useState<{ ad: string; icerik: string; kesik?: boolean } | null>(null)

  function durumYukle() {
    api.get<Durum>('/antivirus/durum').then(r => setD(r.data)).catch(e => setHata(apiHata(e)))
    api.get<{ kayitlar: Kar[] }>('/antivirus/karantina').then(r => setKliste(r.data.kayitlar || [])).catch(() => {})
    api.get<{ kayitlar: Gecmis[] }>('/antivirus/gecmis').then(r => setGecmis(r.data.kayitlar || [])).catch(() => {})
  }
  function ayarYukle() {
    api.get<AyarYanit>('/antivirus/ayarlar').then(r => { setAyar(r.data.ayarlar); setKap(r.data.kapasite) }).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  function itibarYukle() {
    setMesgul('itibar')
    api.get<{ kayitlar: Kara[] }>('/antivirus/kara-liste').then(r => setKara(r.data.kayitlar || [])).catch(e => setHata(apiHata(e))).finally(() => setMesgul(null))
  }
  useEffect(() => { durumYukle(); ayarYukle() }, [])
  useEffect(() => { if (sekme === 'itibar' && kara.length === 0) itibarYukle() }, [sekme])

  const set = (k: keyof Ayar, v: any) => setAyar(a => a ? { ...a, [k]: v } : a)
  function yogunluk(p: 'dusuk' | 'dengeli' | 'yuksek') {
    const on = p === 'dusuk' ? { cpu_yuzde: 25, is_parcacigi: 1, dosya_hiz_sn: 200, yuk_esigi: 50 }
      : p === 'dengeli' ? { cpu_yuzde: 0, is_parcacigi: 0, dosya_hiz_sn: 0, yuk_esigi: 80 }
      : { cpu_yuzde: 0, is_parcacigi: 0, dosya_hiz_sn: 0, yuk_esigi: 0 }
    setAyar(a => a ? { ...a, ...on } : a)
  }

  async function ayarKaydet() {
    if (!ayar) return
    setHata(null); setBilgi(null); setMesgul('ayar')
    try { await api.put('/antivirus/ayarlar', ayar); setBilgi('Ayarlar kaydedildi ve uygulandı.'); durumYukle(); ayarYukle() }
    catch (e) { setHata(apiHata(e, 'Ayarlar kaydedilemedi')) } finally { setMesgul(null) }
  }
  async function taraTumu() {
    if (!(await onay({ baslik: 'Tüm sunucuyu tara', mesaj: 'Tüm /home dizini arka planda taranacak (RapidScan ile hızlandırılır). Başlatılsın mı?' }))) return
    setHata(null); setMesgul('tara')
    try { await api.post('/antivirus/tara-tumu', {}); setBilgi('Dosya taraması başlatıldı — sonuçlar birkaç dakikada listeye düşer.'); setTimeout(durumYukle, 3000) }
    catch (e) { setHata(apiHata(e, 'Tarama başlatılamadı')) } finally { setMesgul(null) }
  }
  async function dbTara() {
    setHata(null); setBilgi(null); setMesgul('db')
    try { const { data } = await api.post<{ taranan_kurulum: number; bulunan: number; hatali_kurulum: number }>('/antivirus/db-tara', {})
      setBilgi(`Veritabanı taraması: ${data.taranan_kurulum} WP kurulumu, ${data.bulunan} zararlı kayıt${data.hatali_kurulum ? `, ${data.hatali_kurulum} bağlanılamadı` : ''}.`)
      durumYukle()
    } catch (e) { setHata(apiHata(e, 'DB taraması başarısız')) } finally { setMesgul(null) }
  }
  async function geriYukle(k: Kar) {
    if (!(await onay({ baslik: 'Geri yükleme', mesaj: `Dosya orijinal konumuna geri yüklensin mi?\n${k.orijinal_yol}\n(${k.alan_adi})` }))) return
    try { await api.post(`/antivirus/karantina/${k.id}/geri-yukle`, {}); durumYukle() } catch (e) { setHata(apiHata(e)) }
  }
  async function sil(k: Kar) {
    if (!(await onay({ baslik: 'Kalıcı silme', mesaj: `Karantinadaki dosya KALICI silinsin mi?\n${k.orijinal_yol}\n(${k.alan_adi})\n\nGeri alınamaz.` }))) return
    try { await api.post(`/antivirus/karantina/${k.id}/sil`, {}); durumYukle() } catch (e) { setHata(apiHata(e)) }
  }
  async function incele(k: Kar) {
    try { const { data } = await api.get<{ icerik: string; ikili: boolean; kesik?: boolean }>(`/antivirus/karantina/${k.id}/incele`); setInceleModal({ ad: `${k.orijinal_yol} (${k.alan_adi})`, icerik: data.ikili ? '[ikili dosya]' : data.icerik, kesik: data.kesik }) }
    catch (e) { setHata(apiHata(e)) }
  }

  if (yuk) return <div className="px-4 py-4 sm:px-6 sm:py-5 text-slate-400">Yükleniyor…</div>

  // Koruma duruşu skoru (0-100): açık katman/koruma oranı.
  const korumaAktif = !!(d?.izleyici_aktif && ayar?.gercek_zamanli)
  const tehdit = (d?.toplam_karantina || 0)
  const posture = tehdit > 0 ? { renk: 'red', metin: 'Tehdit var', ikon: '⚠' }
    : korumaAktif ? { renk: 'emerald', metin: 'Korunuyor', ikon: '🛡' }
      : { renk: 'amber', metin: 'Kısmi koruma', ikon: '🛡' }

  const alan = 'w-full px-2.5 py-1.5 text-sm rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-100'
  const sekmeler: { k: Sekme; e: string; s?: number }[] = [
    { k: 'genel', e: 'Genel Bakış' },
    { k: 'karantina', e: 'Karantina', s: kliste.filter(x => x.durum === 'karantina').length },
    { k: 'gecmis', e: 'Geçmiş', s: gecmis.length },
    { k: 'itibar', e: 'İtibar' },
    { k: 'ayarlar', e: 'Ayarlar' },
  ]

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-7xl mx-auto">
        <style>{ANIM_CSS}</style>
        <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Antivirüs' }]} />

        {/* ══ HERO: güvenlik duruşu konsolu ══ */}
        <div className="relative overflow-hidden rounded-3xl bg-gradient-to-br from-slate-900 via-slate-900 to-slate-800 dark:from-black dark:via-slate-950 dark:to-slate-900 text-white p-6 sm:p-8 mb-5 shadow-lg">
          <div className="absolute -right-16 -top-16 w-64 h-64 rounded-full blur-3xl opacity-20"
            style={{ background: posture.renk === 'red' ? '#ef4444' : posture.renk === 'amber' ? '#f59e0b' : '#10b981' }} />
          <div className="relative flex flex-wrap items-center gap-6 justify-between">
            <div className="flex items-center gap-5">
              <div className="w-24 h-24 flex items-center justify-center flex-shrink-0">
                <Kalkan renk={posture.renk} tarama={mesgul === 'tara' || mesgul === 'db'} />
              </div>
              <div>
                <div className="text-xs uppercase tracking-widest text-slate-400 mb-1">Güvenlik Duruşu</div>
                <div className="text-3xl font-semibold">{posture.metin}</div>
                <div className="text-sm text-slate-400 mt-1">
                  Kendi motorumuz · sürüm {d?.kural_surum} · {d?.kural_sayisi} kural{d?.kural_uretim && d.kural_uretim !== 'gomulu' ? ' · imzalı' : ''}
                </div>
              </div>
            </div>
            <div className="flex gap-3">
              <button onClick={taraTumu} disabled={!!mesgul}
                className="px-4 py-2.5 text-sm font-medium bg-white text-slate-900 rounded-xl hover:bg-slate-100 disabled:opacity-50">
                {mesgul === 'tara' ? 'Başlatılıyor…' : '⚡ Tüm Sunucuyu Tara'}</button>
              <button onClick={dbTara} disabled={!!mesgul}
                className="px-4 py-2.5 text-sm font-medium bg-white/10 text-white rounded-xl hover:bg-white/20 disabled:opacity-50 backdrop-blur">
                {mesgul === 'db' ? 'Taranıyor…' : '🗄 Veritabanı Tara'}</button>
            </div>
          </div>
          {(mesgul === 'tara' || mesgul === 'db') && (
            <div className="relative mt-5 h-1 rounded-full bg-white/10 overflow-hidden">
              <div className="gosp-shimmer absolute inset-y-0 w-1/4 rounded-full bg-white/60" />
            </div>
          )}
          {/* metrik seridi */}
          <div className="relative grid grid-cols-2 sm:grid-cols-4 gap-4 mt-6 pt-6 border-t border-white/10">
            {[
              { e: 'Karantinada', v: d?.toplam_karantina ?? 0, r: tehdit > 0 ? 'text-amber-400' : 'text-white' },
              { e: 'Toplam Bulgu', v: d?.toplam_bulgu ?? 0, r: 'text-white' },
              { e: 'Taranan Domain', v: d?.taranan_domain ?? 0, r: 'text-white' },
              { e: 'Kural Sayısı', v: d?.kural_sayisi ?? 0, r: 'text-white' },
            ].map((m, i) => (
              <div key={i}>
                <div className={`text-3xl font-semibold ${m.r}`}>{m.v}</div>
                <div className="text-xs uppercase tracking-wide text-slate-400 mt-0.5">{m.e}</div>
              </div>
            ))}
          </div>
        </div>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
        {bilgi && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bilgi}</div>}

        {/* ══ Sekme çubuğu ══ */}
        <div className="flex gap-1 p-1 bg-slate-100 dark:bg-slate-800/60 rounded-2xl mb-5 overflow-x-auto">
          {sekmeler.map(t => (
            <button key={t.k} onClick={() => setSekme(t.k)}
              className={`px-4 py-2 text-sm font-medium rounded-xl whitespace-nowrap transition ${sekme === t.k ? 'bg-white dark:bg-slate-700 text-slate-900 dark:text-white shadow-sm' : 'text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200'}`}>
              {t.e}{t.s ? <span className={`ml-1.5 text-xs px-1.5 py-0.5 rounded-full ${sekme === t.k ? 'bg-slate-100 dark:bg-slate-600' : 'bg-slate-200 dark:bg-slate-700'}`}>{t.s}</span> : null}
            </button>
          ))}
        </div>

        {/* ══ GENEL BAKIŞ: koruma modülleri + son taramalar ══ */}
        {sekme === 'genel' && d && (
          <div className="space-y-5">
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              {[
                { ad: 'Dosya Tarayıcı', ikon: 'dosya', aktif: d.ajan_kurulu, alt: `${d.kural_sayisi} kural · RapidScan`, drm: d.ajan_kurulu ? 'Kurulu' : 'Yok' },
                { ad: 'Gerçek Zamanlı', ikon: 'realtime', aktif: d.izleyici_aktif, alt: 'fanotify izleme', drm: d.izleyici_aktif ? 'Aktif' : 'Kapalı' },
                { ad: 'Veritabanı Tarayıcı', ikon: 'db', aktif: true, alt: 'wp_options + wp_posts', drm: 'Hazır' },
                { ad: 'Kaynak Dilimi', ikon: 'slice', aktif: d.slice_aktif, alt: 'cgroup + dinamik yük', drm: d.slice_aktif ? 'Aktif' : 'Kapalı' },
              ].map((m, i) => (
                <div key={i} className="gosp-card bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 shadow-sm hover:shadow-md hover:border-slate-300 dark:hover:border-slate-600 transition">
                  <div className="flex items-center justify-between mb-2">
                    <span className={m.aktif ? 'text-emerald-500' : 'text-slate-400'}><ModulIkon ad={m.ikon} /></span>
                    <span className={`text-xs px-2 py-0.5 rounded-full ${m.aktif ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-400' : 'bg-slate-100 dark:bg-slate-700 text-slate-500'}`}>{m.drm}</span>
                  </div>
                  <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{m.ad}</div>
                  <div className="text-xs text-slate-400 mt-0.5">{m.alt}</div>
                </div>
              ))}
            </div>

            <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Son taramalar</h3>
              {d.son_taramalar.length === 0 ? <div className="text-center py-6 text-sm text-slate-400">Henüz tarama yok.</div> : (
                <div className="lg:overflow-x-auto">
                  <table className={`${T.tablo} text-sm`}>
                    <thead className={T.baslikGrubu}><tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                      <th className={T.baslik}>Domain</th><th className={T.baslik}>Kaynak</th><th className={T.baslik}>Taranan</th><th className={T.baslik}>Enfekte</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Bitiş</th>
                    </tr></thead>
                    <tbody className={T.govde}>{d.son_taramalar.map(t => (
                      <tr key={t.id} className={T.satir}>
                        <td className={T.hucreBaslik}>{t.alan_adi || '— (sunucu)'}</td>
                        <td className={T.hucre} data-etiket="Kaynak"><span className="text-xs px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-500">{t.kaynak || 'panel'}</span></td>
                        <td className={T.hucre} data-etiket="Taranan">{t.taranan}</td>
                        <td className={T.hucre} data-etiket="Enfekte"><span className={t.enfekte > 0 ? 'text-red-600 dark:text-red-400 font-medium' : 'text-slate-400'}>{t.enfekte}</span></td>
                        <td className={T.hucre} data-etiket="Durum"><span className="text-xs text-slate-500">{t.durum}</span></td>
                        <td className={T.hucre} data-etiket="Bitiş"><span className="text-xs text-slate-400">{t.bitis || '—'}</span></td>
                      </tr>
                    ))}</tbody>
                  </table>
                </div>
              )}
            </div>
          </div>
        )}

        {/* ══ KARANTİNA ══ */}
        {sekme === 'karantina' && (
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">🔒 Karantina — tüm sunucu</h3>
            {kliste.length === 0 ? <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">Karantinada dosya yok.</div> : (
              <div className="lg:overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}><tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Domain</th><th className={T.baslik}>Dosya</th><th className={T.baslik}>Tespit</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Tarih</th><th className={T.baslik}></th>
                  </tr></thead>
                  <tbody className={T.govde}>{kliste.map(k => (
                    <tr key={k.id} className={T.satir}>
                      <td className={T.hucreBaslik}>{k.alan_adi || `#${k.domain_id}`}</td>
                      <td className={T.hucre} data-etiket="Dosya"><span className="font-mono text-xs break-all lg:max-w-xs inline-block">{k.orijinal_yol}</span></td>
                      <td className={T.hucre} data-etiket="Tespit"><span className="text-xs text-slate-600 dark:text-slate-300 break-all">{k.imza} <span className="text-slate-400">({k.puan})</span></span></td>
                      <td className={T.hucre} data-etiket="Durum">
                        {k.durum === 'karantina' ? <span className="text-xs text-amber-600 dark:text-amber-400">🔒 Karantinada</span>
                          : k.durum === 'geri_yuklendi' ? <span className="text-xs text-emerald-600 dark:text-emerald-400">↩ Geri yüklendi</span>
                          : <span className="text-xs text-slate-400">🗑 Silindi</span>}
                      </td>
                      <td className={T.hucre} data-etiket="Tarih"><span className="text-xs text-slate-400">{k.tarih}</span></td>
                      <td className={`${T.hucreAksiyon} lg:text-right`}>
                        {k.durum === 'karantina' && k.mevcut && (
                          <span className="flex gap-2 lg:justify-end whitespace-nowrap">
                            <button onClick={() => incele(k)} className="text-xs text-slate-500 hover:underline">İncele</button>
                            <button onClick={() => geriYukle(k)} className="text-xs text-emerald-600 dark:text-emerald-400 hover:underline">Geri yükle</button>
                            <button onClick={() => sil(k)} className="text-xs text-red-600 dark:text-red-400 hover:underline">Sil</button>
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}</tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ══ GEÇMİŞ ══ */}
        {sekme === 'gecmis' && (
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Olay günlüğü {gecmis.length > 0 && <span className="text-xs font-normal text-slate-400">({gecmis.length} kayıt)</span>}</h3>
            {gecmis.length === 0 ? <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">Kayıt yok.</div> : (
              <div className="lg:overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}><tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Tarih</th><th className={T.baslik}>Domain</th><th className={T.baslik}>Dosya</th><th className={T.baslik}>Tespit</th><th className={T.baslik}>Durum</th>
                  </tr></thead>
                  <tbody className={T.govde}>{gecmis.map(g => (
                    <tr key={g.id} className={T.satir}>
                      <td className={T.hucreBaslik}><span className="text-xs text-slate-500">{g.tarih}</span></td>
                      <td className={T.hucre} data-etiket="Domain">{g.alan_adi || `#${g.domain_id}`}</td>
                      <td className={T.hucre} data-etiket="Dosya"><span className="font-mono text-xs break-all lg:max-w-xs inline-block">{g.dosya}</span></td>
                      <td className={T.hucre} data-etiket="Tespit"><span className="text-xs text-slate-600 dark:text-slate-300 break-all">{g.imza} <span className="text-slate-400">({g.puan})</span></span></td>
                      <td className={T.hucre} data-etiket="Durum">
                        {g.durum === 'karantina' ? <span className="text-xs text-amber-600 dark:text-amber-400">🔒 Karantina</span>
                          : g.durum === 'geri_yuklendi' ? <span className="text-xs text-emerald-600 dark:text-emerald-400">↩ Geri yüklendi</span>
                          : g.durum === 'silindi' ? <span className="text-xs text-slate-400">🗑 Silindi</span>
                          : <span className="text-xs text-red-600 dark:text-red-400">⚠ Aktif</span>}
                      </td>
                    </tr>
                  ))}</tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ══ İTİBAR / KARA-LİSTE ══ */}
        {sekme === 'itibar' && (
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Domain itibarı — Spamhaus DBL</h3>
              <button onClick={itibarYukle} disabled={mesgul === 'itibar'} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50">{mesgul === 'itibar' ? 'Kontrol ediliyor…' : 'Yenile'}</button>
            </div>
            {kara.length === 0 ? <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">{mesgul === 'itibar' ? 'Kontrol ediliyor…' : 'Kayıt yok.'}</div> : (
              <div className="lg:overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}><tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Domain</th><th className={T.baslik}>Durum</th><th className={T.baslik}>Kaynak</th>
                  </tr></thead>
                  <tbody className={T.govde}>{kara.map(k => (
                    <tr key={k.domain_id} className={T.satir}>
                      <td className={T.hucreBaslik}>{k.alan_adi}</td>
                      <td className={T.hucre} data-etiket="Durum">
                        {k.durum === 'listeli' ? <span className="text-xs text-red-600 dark:text-red-400 font-medium">⛔ Kara listede</span>
                          : k.durum === 'kontrol_edilemedi' ? <span className="text-xs text-slate-400">— kontrol edilemedi</span>
                          : <span className="text-xs text-emerald-600 dark:text-emerald-400">✓ Temiz</span>}
                      </td>
                      <td className={T.hucre} data-etiket="Kaynak"><span className="text-xs text-slate-400">{k.kaynak}</span></td>
                    </tr>
                  ))}</tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ══ AYARLAR ══ */}
        {sekme === 'ayarlar' && ayar && (
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Ayarlar</h3>
              <button onClick={ayarKaydet} disabled={mesgul === 'ayar'} className="px-4 py-1.5 text-sm font-medium bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50">{mesgul === 'ayar' ? 'Kaydediliyor…' : 'Kaydet'}</button>
            </div>
            <div className="grid gap-x-8 gap-y-1 sm:grid-cols-2">
              <div>
                <div className="text-xs font-semibold text-slate-400 uppercase mt-1 mb-1">Koruma</div>
                <Anahtar acik={ayar.gercek_zamanli} ayarla={v => set('gercek_zamanli', v)} etiket="Gerçek zamanlı koruma" aciklama="Yeni/değişen dosyayı anında tarar (fanotify). Açınca izleyici servisi başlar." />
                <Anahtar acik={ayar.zamanli_tarama} ayarla={v => set('zamanli_tarama', v)} etiket="Zamanlı tarama" aciklama="Günlük tam tarama." />
                {ayar.zamanli_tarama && (
                  <div className="ml-12 mb-1 flex items-center gap-2"><span className="text-xs text-slate-400">Saat</span>
                    <input type="time" value={ayar.zamanli_saat} onChange={e => set('zamanli_saat', e.target.value)} className={`${alan} w-28`} /></div>
                )}
                <Anahtar acik={ayar.oto_karantina} ayarla={v => set('oto_karantina', v)} uyari etiket="Otomatik karantina" aciklama="Kritik bulunca dosyayı otomatik karantinaya alır (WP çekirdeği hariç). KAPALIYKEN sadece bildirir." />
              </div>
              <div>
                <div className="text-xs font-semibold text-slate-400 uppercase mt-1 mb-1">Tespit katmanları</div>
                <Anahtar acik={ayar.kural_motoru} ayarla={v => set('kural_motoru', v)} etiket="Kural motoru" aciklama="İmza/örüntü zinciri (eval+superglobal, shell, webshell)." />
                <Anahtar acik={ayar.konum_sezgileri} ayarla={v => set('konum_sezgileri', v)} etiket="Konum sezgileri" aciklama="uploads/*.php, çift uzantı, gizli dizin." />
                <Anahtar acik={ayar.wp_butunluk} ayarla={v => set('wp_butunluk', v)} etiket="WordPress çekirdek bütünlüğü" aciklama="Resmî md5 ile değişmiş/yabancı çekirdek dosyası." />
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-3 mt-4 pt-4 border-t border-slate-100 dark:border-slate-700">
              <label className="block"><span className="block text-xs text-slate-400 mb-1">Kapsam</span>
                <select value={ayar.kapsam} onChange={e => set('kapsam', e.target.value)} className={alan}>
                  <option value="host">host (/home — müşteri siteleri)</option>
                  <option value="sunucu">sunucu (/ — tüm sistem)</option>
                </select></label>
              <label className="block"><span className="block text-xs text-slate-400 mb-1">Kritik eşik (≥20)</span>
                <input type="number" min={20} value={ayar.esik_kritik} onChange={e => set('esik_kritik', Number(e.target.value))} className={alan} /></label>
              <label className="block"><span className="block text-xs text-slate-400 mb-1">Hariç yollar (virgülle)</span>
                <input type="text" value={ayar.haric_yollar} onChange={e => set('haric_yollar', e.target.value)} className={alan} /></label>
            </div>
            <div className="mt-4 pt-4 border-t border-slate-100 dark:border-slate-700">
              <div className="text-xs font-semibold text-slate-400 uppercase mb-2">Tarama yoğunluğu (dinamik kaynak)</div>
              <div className="flex flex-wrap items-center gap-2 mb-3">
                <button type="button" onClick={() => yogunluk('dusuk')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">Düşük (sunucuyu az yorar)</button>
                <button type="button" onClick={() => yogunluk('dengeli')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">Dengeli</button>
                <button type="button" onClick={() => yogunluk('yuksek')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">Yüksek (hızlı)</button>
              </div>
              <label className="flex items-center gap-3 flex-wrap">
                <span className="text-sm text-slate-700 dark:text-slate-200">Dinamik yük eşiği</span>
                <input type="number" min={0} max={400} value={ayar.yuk_esigi} onChange={e => set('yuk_esigi', Number(e.target.value))} className={`${alan} w-24`} />
                <span className="text-xs text-slate-400">% çekirdek · 0 = kapalı. Sistem 1-dk yükü bu değeri (ör. 80 = ×0.8 çekirdek) aşarsa tarama kendini duraklatır.</span>
              </label>
            </div>
            <button onClick={() => setGelismis(g => !g)} className="text-xs text-brand-600 dark:text-brand-400 mt-4">{gelismis ? '▾ Kaynak limitleri' : '▸ Kaynak limitleri (cgroup)'}</button>
            {gelismis && (
              <div className="grid gap-4 sm:grid-cols-4 mt-2">
                <label className="block"><span className="block text-xs text-slate-400 mb-1">CPU %{kap ? ` (öneri ${kap.oneri_cpu_yuzde})` : ''}</span><input type="number" min={0} value={ayar.cpu_yuzde} onChange={e => set('cpu_yuzde', Number(e.target.value))} className={alan} /></label>
                <label className="block"><span className="block text-xs text-slate-400 mb-1">RAM MB{kap ? ` (öneri ${kap.oneri_ram_mb})` : ''}</span><input type="number" min={0} value={ayar.ram_mb} onChange={e => set('ram_mb', Number(e.target.value))} className={alan} /></label>
                <label className="block"><span className="block text-xs text-slate-400 mb-1">İş parçacığı</span><input type="number" min={0} value={ayar.is_parcacigi} onChange={e => set('is_parcacigi', Number(e.target.value))} className={alan} /></label>
                <label className="block"><span className="block text-xs text-slate-400 mb-1">Dosya hız/sn (0=sınırsız)</span><input type="number" min={0} value={ayar.dosya_hiz_sn} onChange={e => set('dosya_hiz_sn', Number(e.target.value))} className={alan} /></label>
              </div>
            )}
          </div>
        )}

        {/* İnceleme modalı — dosya ÇALIŞTIRILMADAN düz metin */}
        {inceleModal && (
          <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4" onClick={() => setInceleModal(null)}>
            <div className="bg-white dark:bg-slate-800 rounded-2xl max-w-3xl w-full max-h-[80vh] flex flex-col shadow-xl" onClick={e => e.stopPropagation()}>
              <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between">
                <span className="text-sm font-mono text-slate-700 dark:text-slate-200 break-all">{inceleModal.ad}</span>
                <button onClick={() => setInceleModal(null)} className="text-slate-400 hover:text-slate-600 text-lg">×</button>
              </div>
              {inceleModal.kesik && <div className="px-4 pt-3 text-xs text-amber-600 dark:text-amber-400">⚠ Kesik gösterim — yalnızca ilk 64 KB.</div>}
              <pre className="p-4 overflow-auto text-xs font-mono text-slate-800 dark:text-slate-200 whitespace-pre-wrap break-all">{inceleModal.icerik}</pre>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
