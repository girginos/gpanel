import { useEffect, useMemo, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import EmptyState from '@/components/EmptyState'
import { T } from '@/lib/tablo'

type Kayit = {
  id: number; zaman: string; aktor: string; aktor_rol: string; ip: string
  eylem: string; hedef: string; detay?: string; basarili: boolean
  kapsam_id: number; kapsam_ad: string
}
type Yanit = {
  toplam: number; limit: number; offset: number
  kayitlar: Kayit[]; eylemler: string[]
  kapsam: { bayi_mi: boolean; bayi_id: number }
}
type Bayi = { id: number; kullanici: string }

// Eylem kodlarını okunur etikete çevirir. Bilinmeyen kod olduğu gibi gösterilir —
// yeni bir eylem eklendiğinde sayfa bozulmaz, sadece ham kodu görünür.
const ETIKET: Record<string, string> = {
  'auth.login': 'Giriş', 'auth.parola': 'Parola değişimi', 'auth.2fa': '2FA doğrulama',
  'auth.2fa.enable': '2FA açıldı', 'auth.2fa.disable': '2FA kapatıldı',
  'hosting.olustur': 'Hosting oluşturuldu', 'hosting.sil': 'Hosting silindi',
  'hosting.askiya_al': 'Hosting askıya alındı', 'hosting.askidan_al': 'Hosting askıdan alındı',
  'hosting.plan': 'Plan değiştirildi', 'hosting.ozel_plan': 'Özel plan uygulandı',
  'hosting.toplu_durum': 'Toplu durum değişimi',
  'db.olustur': 'Veritabanı oluşturuldu', 'db.sil': 'Veritabanı silindi', 'db.parola': 'DB parolası değişti',
  'bayi.olustur': 'Bayi oluşturuldu', 'bayi.guncelle': 'Bayi güncellendi', 'bayi.sil': 'Bayi silindi',
  'bayi.askiya_al': 'Bayi askıya alındı', 'bayi.askidan_al': 'Bayi askıdan alındı',
  'bayi.parola': 'Bayi parolası değişti',
  'plan.olustur': 'Plan oluşturuldu',
  'guvenlik.izolasyon_kaybi': 'İzolasyon uyarısı',
}
function etiketle(e: string) { return ETIKET[e] || e }

// Yıkıcı işlemler listede göz ile ayrılsın (renk TEK başına anlam taşımasın diye
// ayrıca "yıkıcı" ibaresi title'da verilir).
const YIKICI = new Set(['hosting.sil', 'db.sil', 'bayi.sil', 'hosting.askiya_al', 'bayi.askiya_al'])

export default function DenetimPage() {
  const bayiMi = useMemo(() => {
    try { return JSON.parse(localStorage.getItem('gosp.user') || '{}').rol === 'reseller' } catch { return false }
  }, [])
  const [veri, setVeri] = useState<Yanit | null>(null)
  const [bayiler, setBayiler] = useState<Bayi[]>([])
  const [ara, setAra] = useState('')
  const [eylem, setEylem] = useState('')
  const [kapsam, setKapsam] = useState('')
  const [sayfa, setSayfa] = useState(0)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const limit = 50

  useEffect(() => {
    if (bayiMi) return
    api.get<Bayi[]>('/resellers').then(r => setBayiler(r.data || [])).catch(() => { /* opsiyonel */ })
  }, [bayiMi])

  useEffect(() => {
    setYuk(true)
    const p = new URLSearchParams({ limit: String(limit), offset: String(sayfa * limit) })
    if (ara.trim()) p.set('ara', ara.trim())
    if (eylem) p.set('eylem', eylem)
    if (kapsam) p.set('kapsam', kapsam)
    api.get<Yanit>(`/denetim?${p}`)
      .then(r => { setVeri(r.data); setHata(null) })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }, [ara, eylem, kapsam, sayfa])

  const kayitlar = veri?.kayitlar || []
  const toplam = veri?.toplam || 0
  const sonSayfa = Math.max(0, Math.ceil(toplam / limit) - 1)

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 space-y-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Denetim Kaydı' }]} />
      <div>
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">Denetim Kaydı</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
          {bayiMi
            ? 'Kendi hosting hesaplarınızda yapılan işlemler. Yönetici bir işlem yaptığında da burada görünür.'
            : 'Sunucudaki tüm panel işlemleri — kök ve bayiler dâhil. Kapsam süzgeciyle tek bir bayiye daraltabilirsiniz.'}
        </p>
      </div>

      {hata && (
        <div role="alert" className="rounded-xl border border-rose-200 dark:border-rose-900 bg-rose-50 dark:bg-rose-950/40 px-4 py-3 text-sm text-rose-700 dark:text-rose-300">
          {hata}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2.5 rounded-2xl border border-slate-200/70 dark:border-slate-700/60 bg-white dark:bg-slate-800/40 p-3">
        <label className="sr-only" htmlFor="denetim-ara">Kayıtlarda ara</label>
        <div className="relative min-w-[200px] flex-1">
          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4"><circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" /></svg>
          </span>
          <input id="denetim-ara" value={ara} onChange={e => { setSayfa(0); setAra(e.target.value) }}
            placeholder="Kullanıcı, hedef, IP veya eylem ara…"
            className="w-full rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900/60 pl-9 pr-3 py-2 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15" />
        </div>
        <label className="sr-only" htmlFor="denetim-eylem">Eylem türü</label>
        <select id="denetim-eylem" value={eylem} onChange={e => { setSayfa(0); setEylem(e.target.value) }}
          className="shrink-0 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900/60 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15">
          <option value="">Tüm eylemler</option>
          {(veri?.eylemler || []).map(e => <option key={e} value={e}>{etiketle(e)}</option>)}
        </select>
        {!bayiMi && (
          <>
            <label className="sr-only" htmlFor="denetim-kapsam">Kapsam</label>
            <select id="denetim-kapsam" value={kapsam} onChange={e => { setSayfa(0); setKapsam(e.target.value) }}
              className="shrink-0 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900/60 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15">
              <option value="">Tüm kapsamlar</option>
              <option value="kok">Yalnız kök (panel sahibi)</option>
              {bayiler.map(b => <option key={b.id} value={String(b.id)}>Bayi: {b.kullanici}</option>)}
            </select>
          </>
        )}
        <span className="ml-auto shrink-0 whitespace-nowrap rounded-full bg-slate-100 dark:bg-slate-800 px-2.5 py-1 text-[11px] font-medium text-slate-500 dark:text-slate-400 tabular-nums">{toplam.toLocaleString('tr-TR')} kayıt</span>
      </div>

      {yuk && !veri ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
      ) : kayitlar.length === 0 ? (
        <EmptyState baslik="Kayıt yok"
          aciklama="Bu süzgeçlerle eşleşen denetim kaydı bulunamadı. Süzgeçleri temizleyip tekrar deneyin." />
      ) : (
        <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
                <tr>
                  <th className={T.baslik}>Zaman</th>
                  <th className={T.baslik}>Kullanıcı</th>
                  <th className={T.baslik}>Eylem</th>
                  <th className={T.baslik}>Hedef</th>
                  {!bayiMi && <th className={T.baslik}>Kapsam</th>}
                  <th className={T.baslik}>IP</th>
                  <th className={T.baslik}>Sonuç</th>
                </tr>
              </thead>
              <tbody className={T.govde}>
                {kayitlar.map(k => (
                  <tr key={k.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800 transition`}>
                    <td className={T.hucre} data-etiket="Zaman">
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400 whitespace-nowrap tabular-nums">{k.zaman}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Kullanıcı">
                      <span className="text-slate-700 dark:text-slate-300">{k.aktor || '—'}</span>
                      {k.aktor_rol && (
                        <span className="ml-1.5 text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded-full font-semibold bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400">
                          {k.aktor_rol === 'admin' ? 'yönetici' : k.aktor_rol === 'reseller' ? 'bayi' : k.aktor_rol}
                        </span>
                      )}
                    </td>
                    <td className={T.hucre} data-etiket="Eylem">
                      <span className={YIKICI.has(k.eylem) ? 'text-rose-700 dark:text-rose-300 font-medium' : 'text-slate-700 dark:text-slate-300'}
                        title={YIKICI.has(k.eylem) ? 'Yıkıcı işlem — ' + k.eylem : k.eylem}>
                        {YIKICI.has(k.eylem) && <span aria-hidden="true" className="mr-1">⚠</span>}
                        {etiketle(k.eylem)}
                      </span>
                      {k.detay && (
                        // Uzun sistem kayitlari satiri sismesin: iki satirda kirpilir,
                        // tamami title ile erisilebilir kalir.
                        <div title={k.detay}
                             className="text-[11px] text-slate-400 dark:text-slate-500 mt-0.5 break-words line-clamp-2 max-w-[42ch]">
                          {k.detay}
                        </div>
                      )}
                    </td>
                    <td className={T.hucre} data-etiket="Hedef">
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400 break-all">{k.hedef || '—'}</span>
                    </td>
                    {!bayiMi && (
                      <td className={T.hucre} data-etiket="Kapsam">
                        {k.kapsam_id === 0
                          ? <span className="text-xs text-slate-500 dark:text-slate-400">kök</span>
                          : <span title={k.kapsam_ad ? `Bayi: ${k.kapsam_ad}` : `Silinmiş bayi #${k.kapsam_id}`}
                                  className={`text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded-full font-semibold ${
                                    k.kapsam_ad
                                      ? 'bg-violet-100 dark:bg-violet-900/30 text-violet-700 dark:text-violet-300'
                                      : 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 line-through'}`}>
                              {k.kapsam_ad || ('#' + k.kapsam_id)}
                            </span>}
                      </td>
                    )}
                    <td className={T.hucre} data-etiket="IP">
                      <span className="font-mono text-xs text-slate-500 dark:text-slate-400">{k.ip || '—'}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Sonuç">
                      <span className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded-full font-semibold ${
                        k.basarili
                          ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300'
                          : 'bg-rose-100 dark:bg-rose-900/30 text-rose-700 dark:text-rose-300'}`}>
                        {k.basarili ? 'başarılı' : 'başarısız'}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {toplam > limit && (
        <div className="flex items-center justify-between gap-3">
          <button onClick={() => setSayfa(s => Math.max(0, s - 1))} disabled={sayfa === 0}
            className="px-3 py-1.5 text-sm rounded-lg border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-50 dark:hover:bg-slate-800">
            ← Önceki
          </button>
          <span className="text-xs text-slate-500 dark:text-slate-400 tabular-nums">Sayfa {sayfa + 1} / {sonSayfa + 1}</span>
          <button onClick={() => setSayfa(s => Math.min(sonSayfa, s + 1))} disabled={sayfa >= sonSayfa}
            className="px-3 py-1.5 text-sm rounded-lg border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-50 dark:hover:bg-slate-800">
            Sonraki →
          </button>
        </div>
      )}
    </div>
  )
}
