import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useMemo, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
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
  'plan.olustur': 'Plan oluşturuldu', 'plan.guncelle': 'Plan güncellendi',
  'yedek.geriyukle': 'Yedek geri yüklendi',
  'tasima_baslat': 'Taşıma başlatıldı', 'tasima_iptal': 'Taşıma iptal edildi',
  'guvenlik.izolasyon_kaybi': 'İzolasyon uyarısı',
}

const DEN_EN: Record<string, string> = {
  "2FA açıldı": "2FA enabled",
  "2FA doğrulama": "2FA verification",
  "2FA kapatıldı": "2FA disabled",
  "Bayi askıdan alındı": "Reseller unsuspended",
  "Bayi askıya alındı": "Reseller suspended",
  "Bayi güncellendi": "Reseller updated",
  "Bayi oluşturuldu": "Reseller created",
  "Bayi parolası değişti": "Reseller password changed",
  "Bu süzgeçlerle eşleşen denetim kaydı bulunamadı. Süzgeçleri temizleyip tekrar deneyin.": "No audit records match these filters. Clear the filters and try again.",
  "DB parolası değişti": "DB password changed",
  "Denetim Kaydı": "Audit Log",
  "Hosting askıdan alındı": "Hosting unsuspended",
  "Hosting askıya alındı": "Hosting suspended",
  "Hosting oluşturuldu": "Hosting created",
  "Kayıtlarda ara": "Search records",
  "Kendi hosting hesaplarınızda yapılan işlemler. Yönetici bir işlem yaptığında da burada görünür.": "Operations on your own hosting accounts. Also appears here when an admin performs an operation.",
  "Kullanıcı, hedef, IP veya eylem ara…": "Search user, target, IP or action…",
  "Parola değişimi": "Password change",
  "Plan değiştirildi": "Plan changed",
  "Plan güncellendi": "Plan updated",
  "Plan oluşturuldu": "Plan created",
  "Sunucudaki tüm panel işlemleri — kök ve bayiler dâhil. Kapsam süzgeciyle tek bir bayiye daraltabilirsiniz.": "All panel operations on the server — including root and resellers. You can narrow to a single reseller with the scope filter.",
  "Taşıma başlatıldı": "Migration started",
  "Taşıma iptal edildi": "Migration cancelled",
  "Toplu durum değişimi": "Bulk status change",
  "Tüm eylemler": "All actions",
  "Tüm kapsamlar": "All scopes",
  "Veritabanı oluşturuldu": "Database created",
  "Veritabanı silindi": "Database deleted",
  "Yalnız kök (panel sahibi)": "Root only (panel owner)",
  "Yedek geri yüklendi": "Backup restored",
  "Yıkıcı işlem —": "Destructive operation —",
  "yıkıcı": "destructive",
  "Özel plan uygulandı": "Custom plan applied",
  "İzolasyon uyarısı": "Isolation warning",
  "Giriş": "Login",
  "Hosting silindi": "Hosting deleted",
  "Bayi silindi": "Reseller deleted",
  "Kapsam": "Scope",
  "Bayi": "Reseller",
  "kayıt": "records",
  "Zaman": "Time",
  "Eylem": "Action",
  "Hedef": "Target",
  "Sayfa": "Page",
  "Sonraki →": "Next →",
  "← Önceki": "← Previous",
  "başarılı": "successful",
  "yönetici": "admin",
  "bayi": "reseller",
  "kök": "root",
  "Silinmiş bayi #{0}": "Deleted reseller #{0}",
  "Anasayfa": "Homepage",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DEN_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function etiketle(e: string) {
  return ETIKET[e] || e
}

// Yıkıcı işlemler listede göz ile ayrılsın (renk TEK başına anlam taşımasın diye
// ayrıca cevir("yıkıcı") ibaresi title'da verilir).
const YIKICI = new Set(['hosting.sil', 'db.sil', 'bayi.sil', 'hosting.askiya_al', 'bayi.askiya_al'])

// Eylem türüne göre ikon + renk — düz metin yerine görsel kategori ayrımı (tıpkı yıkıcı
// "Hosting silindi" gibi ama her tür için). Kod DESENİNE göre kategori seçilir; böylece yeni
// eylem kodları (ör. yeni bir *.sil / *.olustur) otomatik doğru kategoriye düşer. Renk TEK
// sinyal değildir — ikon + etiket + title da anlam taşır (erişilebilirlik).
function eylemStil(e: string): { ikon: string; renk: string } {
  if (e === 'guvenlik.izolasyon_kaybi') return { ikon: I.kalkan, renk: 'text-rose-700 dark:text-rose-300' }
  if (/\.sil$/.test(e))                 return { ikon: I.cop, renk: 'text-rose-700 dark:text-rose-300' }
  if (/iptal$/.test(e))                 return { ikon: I.kapat, renk: 'text-amber-700 dark:text-amber-300' }
  if (/askiya_al$/.test(e))             return { ikon: I.durdur, renk: 'text-amber-700 dark:text-amber-300' }
  if (/askidan_al$/.test(e))            return { ikon: I.oynat, renk: 'text-emerald-700 dark:text-emerald-300' }
  if (/\.olustur$/.test(e))             return { ikon: I.arti, renk: 'text-emerald-700 dark:text-emerald-300' }
  if (e.startsWith('yedek'))            return { ikon: I.disket, renk: 'text-violet-700 dark:text-violet-300' }
  if (e.startsWith('tasima'))           return { ikon: I.tasi, renk: 'text-sky-700 dark:text-sky-300' }
  if (e.startsWith('auth.'))            return { ikon: I.anahtar, renk: 'text-indigo-600 dark:text-indigo-300' }
  // güncelleme/plan/parola/toplu-durum vb. — düzenleme kategorisi
  return { ikon: I.kalem, renk: 'text-sky-700 dark:text-sky-300' }
}

// EylemRozet: ikon + kategori-renkli etiket. Tüm eylem türlerini "Hosting silindi" gibi
// makyajlar (düz metin yerine).
function EylemRozet({ eylem }: { eylem: string }) {
  const s = eylemStil(eylem)
  return (
    <span className={`inline-flex items-center gap-1.5 font-medium ${s.renk}`}
      title={(YIKICI.has(eylem) ? cevir('Yıkıcı işlem —') + ' ' : '') + eylem}>
      <Ikon d={s.ikon} className="h-3.5 w-3.5 shrink-0" />
      {cevir(etiketle(eylem))}
    </span>
  )
}

export default function DenetimPage() {
  useTranslation() // dil re-render aboneligi
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
      <Breadcrumb items={[{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir("Denetim Kaydı") }]} />
      <div>
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Denetim Kaydı")}</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
          {bayiMi
            ? cevir("Kendi hosting hesaplarınızda yapılan işlemler. Yönetici bir işlem yaptığında da burada görünür.")
            : cevir("Sunucudaki tüm panel işlemleri — kök ve bayiler dâhil. Kapsam süzgeciyle tek bir bayiye daraltabilirsiniz.")}
        </p>
      </div>

      {hata && (
        <div role="alert" className="rounded-xl border border-rose-200 dark:border-rose-900 bg-rose-50 dark:bg-rose-950/40 px-4 py-3 text-sm text-rose-700 dark:text-rose-300">
          {hata}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2.5 rounded-2xl border border-slate-200/70 dark:border-slate-700/60 bg-white dark:bg-slate-800/40 p-3">
        <label className="sr-only" htmlFor="denetim-ara">{cevir("Kayıtlarda ara")}</label>
        <div className="relative min-w-[200px] flex-1">
          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4"><circle cx="11" cy="11" r="8" /><path d="m21 21-4.3-4.3" /></svg>
          </span>
          <input id="denetim-ara" value={ara} onChange={e => { setSayfa(0); setAra(e.target.value) }}
            placeholder={cevir("Kullanıcı, hedef, IP veya eylem ara…")}
            className="w-full rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900/60 pl-9 pr-3 py-2 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15" />
        </div>
        <label className="sr-only" htmlFor="denetim-eylem">{cevir("Eylem türü")}</label>
        <select id="denetim-eylem" value={eylem} onChange={e => { setSayfa(0); setEylem(e.target.value) }}
          className="shrink-0 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900/60 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15">
          <option value="">{cevir("Tüm eylemler")}</option>
          {(veri?.eylemler || []).map(e => <option key={e} value={e}>{cevir(etiketle(e))}</option>)}
        </select>
        {!bayiMi && (
          <>
            <label className="sr-only" htmlFor="denetim-kapsam">{cevir("Kapsam")}</label>
            <select id="denetim-kapsam" value={kapsam} onChange={e => { setSayfa(0); setKapsam(e.target.value) }}
              className="shrink-0 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900/60 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15">
              <option value="">{cevir("Tüm kapsamlar")}</option>
              <option value="kok">{cevir("Yalnız kök (panel sahibi)")}</option>
              {bayiler.map(b => <option key={b.id} value={String(b.id)}>{cevir("Bayi")}: {b.kullanici}</option>)}
            </select>
          </>
        )}
        <span className="ml-auto shrink-0 whitespace-nowrap rounded-full bg-slate-100 dark:bg-slate-800 px-2.5 py-1 text-[11px] font-medium text-slate-500 dark:text-slate-400 tabular-nums">{toplam.toLocaleString('tr-TR')} {cevir("kayıt")}</span>
      </div>

      {yuk && !veri ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
      ) : kayitlar.length === 0 ? (
        <EmptyState baslik={cevir("Kayıt yok")}
          aciklama={cevir("Bu süzgeçlerle eşleşen denetim kaydı bulunamadı. Süzgeçleri temizleyip tekrar deneyin.")} />
      ) : (
        <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700`}>
                <tr>
                  <th className={T.baslik}>{cevir("Zaman")}</th>
                  <th className={T.baslik}>{cevir("Kullanıcı")}</th>
                  <th className={T.baslik}>{cevir("Eylem")}</th>
                  <th className={T.baslik}>{cevir("Hedef")}</th>
                  {!bayiMi && <th className={T.baslik}>{cevir("Kapsam")}</th>}
                  <th className={T.baslik}>IP</th>
                  <th className={T.baslik}>{cevir("Sonuç")}</th>
                </tr>
              </thead>
              <tbody className={T.govde}>
                {kayitlar.map(k => (
                  <tr key={k.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800 transition`}>
                    <td className={T.hucre} data-etiket={cevir("Zaman")}>
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400 whitespace-nowrap tabular-nums">{k.zaman}</span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Kullanıcı")}>
                      <span className="text-slate-700 dark:text-slate-300">{k.aktor || '—'}</span>
                      {k.aktor_rol && (
                        <span className="ml-1.5 text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded-full font-semibold bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400">
                          {k.aktor_rol === 'admin' ? cevir('yönetici') : k.aktor_rol === 'reseller' ? cevir('bayi') : k.aktor_rol}
                        </span>
                      )}
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Eylem")}>
                      <EylemRozet eylem={k.eylem} />
                      {k.detay && (
                        // Uzun sistem kayitlari satiri sismesin: iki satirda kirpilir,
                        // tamami title ile erisilebilir kalir.
                        <div title={k.detay}
                             className="text-[11px] text-slate-400 dark:text-slate-500 mt-0.5 break-words line-clamp-2 max-w-[42ch]">
                          {k.detay}
                        </div>
                      )}
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Hedef")}>
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400 break-all">{k.hedef || '—'}</span>
                    </td>
                    {!bayiMi && (
                      <td className={T.hucre} data-etiket={cevir("Kapsam")}>
                        {k.kapsam_id === 0
                          ? <span className="text-xs text-slate-500 dark:text-slate-400">{cevir("kök")}</span>
                          : <span title={k.kapsam_ad ? `${cevir("Bayi")}: ${k.kapsam_ad}` : cevirT(cevir("Silinmiş bayi #{0}"), k.kapsam_id)}
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
                    <td className={T.hucre} data-etiket={cevir("Sonuç")}>
                      <span className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded-full font-semibold ${
                        k.basarili
                          ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300'
                          : 'bg-rose-100 dark:bg-rose-900/30 text-rose-700 dark:text-rose-300'}`}>
                        {k.basarili ? cevir('başarılı') : cevir("başarısız")}
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
            {cevir(cevir("← Önceki"))}
          </button>
          <span className="text-xs text-slate-500 dark:text-slate-400 tabular-nums">{cevir("Sayfa")} {sayfa + 1} / {sonSayfa + 1}</span>
          <button onClick={() => setSayfa(s => Math.min(sonSayfa, s + 1))} disabled={sayfa >= sonSayfa}
            className="px-3 py-1.5 text-sm rounded-lg border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 disabled:opacity-40 hover:bg-slate-50 dark:hover:bg-slate-800">
            {cevir("Sonraki →")}
          </button>
        </div>
      )}
    </div>
  )
}
