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
import { Button } from '@/components/ui'
import { useToast } from '@/components/Toast'

type Kayit = {
  id: number; zaman: string; aktor: string; aktor_rol: string; ip: string
  eylem: string; hedef: string; basarili: boolean
  // detay: HAM açıklama — yalnız ipucunda (title) ve genişletmede gösterilir.
  // aciklama: arka uçta (internal/denetim/aciklama.go) sadeleştirilmiş TR/EN cümle +
  // varsayılan gizli teknik ayrıntılar (iş kimliği, dosya yolu, parmak izi, ham hata).
  detay?: string; aciklama?: Aciklama
  kapsam_id: number; kapsam_ad: string
  // adina: işlem müşteri ADINA oturumda yapıldı; adina_alan: o hostingin alan adı
  adina?: boolean; adina_alan?: string
}
type Etiket = { tr: string; en: string }
type Ayrinti = { tr: string; en: string; deger: string }
type Aciklama = { tr: string; en: string; ayrinti?: Ayrinti[] }
type Yanit = {
  toplam: number; limit: number; offset: number
  kayitlar: Kayit[]; eylemler: string[]
  // Eylem kodu → insan dilinde ad. TEK kaynak arka uçtaki katalog (internal/denetim/eylemler.go);
  // ham kod kullanıcıya gösterilmez, yalnız ipucunda (title) durur.
  etiketler?: Record<string, Etiket>
  kapsam: { bayi_mi: boolean; bayi_id: number }
}
type Bayi = { id: number; kullanici: string }

const DEN_EN: Record<string, string> = {
  "İşlem başarısız": "Operation failed",
  "Bu süzgeçlerle eşleşen denetim kaydı bulunamadı. Süzgeçleri temizleyip tekrar deneyin.": "No audit records match these filters. Clear the filters and try again.",
  "Denetim Kaydı": "Audit Log",
  "Kayıtlarda ara": "Search records",
  "Yalnızca sizin yaptığınız işlemler.": "Only the operations you performed.",
  "Kullanıcı, hedef, IP veya eylem ara…": "Search user, target, IP or action…",
  "Sunucudaki tüm panel işlemleri — kök ve bayiler dâhil. Kapsam süzgeciyle tek bir bayiye daraltabilirsiniz.": "All panel operations on the server — including root and resellers. You can narrow to a single reseller with the scope filter.",
  "Tüm eylemler": "All actions",
  "Tüm kapsamlar": "All scopes",
  "Yalnız kök (panel sahibi)": "Root only (panel owner)",
  "Yıkıcı işlem —": "Destructive operation —",
  "yıkıcı": "destructive",
  "Kapsam": "Scope",
  "Bayi": "Reseller",
  "kayıt": "records",
  "Zaman": "Time",
  "Eylem": "Action",
  "Hedef": "Target",
  "Sayfa": "Page",
  "Sonraki": "Next",
  "Önceki": "Previous",
  "başarılı": "successful",
  "yönetici": "admin",
  "bayi": "reseller",
  "kök": "root",
  "Silinmiş bayi #{0}": "Deleted reseller #{0}",
  "Anasayfa": "Homepage",
  "müşteri": "customer",
  "sistem": "system",
  "silinmiş": "deleted",
  "Müşteri": "Customer",
  "müşteri adına": "on behalf of customer",
  "Müşteri paneline girerek yaptı: {0}": "Done from the customer panel: {0}",
  "Müşteri paneline girerek yaptı": "Done from the customer panel",
  "Ayrıntıyı gizle": "Hide details",
  "Ham kayıt": "Raw record",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DEN_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// okunurKod: katalogda olmayan (yeni) bir kod için okunur geri dönüş —
// "musteri.adina_cikis" → "Musteri adina cikis". Arka uçtaki okunurKod ile aynı kural.
function okunurKod(kod: string): string {
  const s = kod.split(/[._-]+/).filter(Boolean).join(' ')
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : kod
}

function etiketle(e: string, etiketler?: Record<string, Etiket>): string {
  const et = etiketler?.[e]
  if (et) return i18n.language === 'en' ? (et.en || et.tr) : et.tr
  return okunurKod(e)
}

const dilde = (e: { tr: string; en: string }): string => (i18n.language === 'en' ? (e.en || e.tr) : e.tr)

// Satırda gösterilecek sade açıklama: arka uçtaki sade cümle; o yoksa (eski arka uç)
// ham metin. Sade hâli bilerek boşsa (yalnız teknik ayrıntı var) satırda bir şey yazmaz.
function sadeAciklama(k: Kayit): string {
  if (k.aciklama) return dilde(k.aciklama)
  return k.detay || ''
}

// AciklamaHucre: sade açıklama + (varsa) "Ayrıntılar" ile açılan teknik ayrıntılar ve
// ham kayıt. Ham metin varsayılan gösterimde yalnız ipucundadır.
function AciklamaHucre({ k, acik, degistir }: { k: Kayit; acik: boolean; degistir: () => void }) {
  const metin = sadeAciklama(k)
  const ayr = k.aciklama?.ayrinti || []
  const hamFarkli = !!k.detay && k.detay !== metin
  if (!metin && ayr.length === 0 && !hamFarkli) return null
  const panelId = `denetim-ayr-${k.id}`
  return (
    <div className="mt-0.5 max-w-[42ch]">
      {metin && (
        // Uzun açıklama satırı şişirmesin: iki satırda kırpılır; tamamı genişletmede.
        <div title={k.detay || metin} className="text-[11px] text-slate-500 dark:text-slate-400 break-words line-clamp-2">
          {metin}
        </div>
      )}
      {(ayr.length > 0 || hamFarkli) && (
        <button type="button" onClick={degistir} aria-expanded={acik} aria-controls={panelId}
          className="mt-0.5 inline-flex items-center gap-0.5 rounded text-[11px] font-medium text-brand-600 dark:text-brand-300 hover:underline focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/40">
          {cevir(acik ? 'Ayrıntıyı gizle' : 'Ayrıntılar')}
          <Ikon d={acik ? I.chevronYukari : I.chevronAsagi} className="h-3 w-3" />
        </button>
      )}
      {acik && (
        <dl id={panelId} className="mt-1 space-y-0.5 rounded-md border border-slate-200 dark:border-dark-600 bg-slate-50 dark:bg-dark-800/60 px-2 py-1.5 text-[11px]">
          {ayr.map((a, i) => (
            <div key={i} className="flex flex-wrap gap-x-1.5">
              <dt className="text-slate-500 dark:text-slate-400">{dilde(a)}:</dt>
              <dd className="font-mono text-slate-700 dark:text-slate-300 break-all">{a.deger}</dd>
            </div>
          ))}
          {hamFarkli && (
            <div className="flex flex-wrap gap-x-1.5">
              <dt className="text-slate-500 dark:text-slate-400">{cevir('Ham kayıt')}:</dt>
              <dd className="font-mono text-slate-600 dark:text-slate-400 break-all">{k.detay}</dd>
            </div>
          )}
        </dl>
      )}
    </div>
  )
}

const ROL_AD: Record<string, string> = { admin: 'yönetici', reseller: 'bayi' }

// Yıkıcı işlemler listede göz ile ayrılsın (renk TEK başına anlam taşımasın diye
// ayrıca cevir("yıkıcı") ibaresi title'da verilir).
const YIKICI = new Set(['hosting.sil', 'db.sil', 'bayi.sil', 'hosting.askiya_al', 'bayi.askiya_al'])

// Eylem türüne göre ikon + renk — düz metin yerine görsel kategori ayrımı (tıpkı yıkıcı
// "Hosting silindi" gibi ama her tür için). Kod DESENİNE göre kategori seçilir; böylece yeni
// eylem kodları (ör. yeni bir *.sil / *.olustur) otomatik doğru kategoriye düşer. Renk TEK
// sinyal değildir — ikon + etiket + title da anlam taşır (erişilebilirlik).
function eylemStil(e: string): { ikon: string; renk: string } {
  if (e === 'musteri.adina_cikis')      return { ikon: I.cikis, renk: 'text-indigo-600 dark:text-indigo-300' }
  if (e.startsWith('musteri.adina_'))   return { ikon: I.giris, renk: 'text-indigo-600 dark:text-indigo-300' }
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
function EylemRozet({ eylem, etiket }: { eylem: string; etiket: string }) {
  const s = eylemStil(eylem)
  return (
    <span className={`inline-flex items-center gap-1.5 font-medium ${s.renk}`}
      title={(YIKICI.has(eylem) ? cevir('Yıkıcı işlem —') + ' ' : '') + eylem}>
      <Ikon d={s.ikon} className="h-3.5 w-3.5 shrink-0" />
      {etiket}
    </span>
  )
}

export default function DenetimPage() {
  useTranslation() // dil re-render aboneligi
  const toast = useToast()
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
  // Ayrıntısı açık satırlar (kayıt id'si).
  const [acikSatir, setAcikSatir] = useState<Set<number>>(() => new Set())
  const satirDegistir = (id: number) => setAcikSatir(s => {
    const y = new Set(s)
    if (y.has(id)) y.delete(id); else y.add(id)
    return y
  })
  const [, setHata] = useState<string | null>(null)
  const limit = 50

  useEffect(() => {
    let iptal = false
    if (bayiMi) return
    api.get<Bayi[]>('/resellers').then(r => { if (iptal) return; setBayiler(r.data || []) }).catch(() => { /* opsiyonel */ })
    return () => { iptal = true }
  }, [bayiMi])

  useEffect(() => {
    let iptal = false
    setYuk(true)
    const p = new URLSearchParams({ limit: String(limit), offset: String(sayfa * limit) })
    if (ara.trim()) p.set('ara', ara.trim())
    if (eylem) p.set('eylem', eylem)
    if (kapsam) p.set('kapsam', kapsam)
    api.get<Yanit>(`/denetim?${p}`)
      .then(r => { if (iptal) return; setVeri(r.data); setHata(null) })
      .catch(e => { if (iptal) return; const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => { if (!iptal) setYuk(false) })
    return () => { iptal = true }
  }, [ara, eylem, kapsam, sayfa])

  const kayitlar = veri?.kayitlar || []
  const toplam = veri?.toplam || 0
  const sonSayfa = Math.max(0, Math.ceil(toplam / limit) - 1)

  return (
    <div className="space-y-5">
      <Breadcrumb items={[{ etiket: cevir('Anasayfa'), href: '/' }, { etiket: cevir("Denetim Kaydı") }]} />
      <div>
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Denetim Kaydı")}</h1>
        <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
          {bayiMi
            ? cevir("Yalnızca sizin yaptığınız işlemler.")
            : cevir("Sunucudaki tüm panel işlemleri — kök ve bayiler dâhil. Kapsam süzgeciyle tek bir bayiye daraltabilirsiniz.")}
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2.5 rounded-lg border border-slate-200/70 dark:border-dark-600/60 bg-white dark:bg-dark-700/40 p-3">
        <label className="sr-only" htmlFor="denetim-ara">{cevir("Kayıtlarda ara")}</label>
        <div className="relative min-w-[200px] flex-1">
          <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-slate-400">
            <Ikon d={I.ara} className="h-4 w-4" />
          </span>
          <input id="denetim-ara" value={ara} onChange={e => { setSayfa(0); setAra(e.target.value) }}
            placeholder={cevir("Kullanıcı, hedef, IP veya eylem ara…")}
            className="w-full rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-800/60 pl-9 pr-3 py-2 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15" />
        </div>
        <label className="sr-only" htmlFor="denetim-eylem">{cevir("Eylem türü")}</label>
        <select id="denetim-eylem" value={eylem} onChange={e => { setSayfa(0); setEylem(e.target.value) }}
          className="shrink-0 rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-800/60 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15">
          <option value="">{cevir("Tüm eylemler")}</option>
          {(veri?.eylemler || []).map(e => <option key={e} value={e}>{etiketle(e, veri?.etiketler)}</option>)}
        </select>
        {!bayiMi && (
          <>
            <label className="sr-only" htmlFor="denetim-kapsam">{cevir("Kapsam")}</label>
            <select id="denetim-kapsam" value={kapsam} onChange={e => { setSayfa(0); setKapsam(e.target.value) }}
              className="shrink-0 rounded-lg border border-slate-200 dark:border-dark-600 bg-white dark:bg-dark-800/60 px-3 py-2 text-sm text-slate-900 dark:text-slate-100 focus:border-brand-400 focus:outline-none focus:ring-2 focus:ring-brand-500/15">
              <option value="">{cevir("Tüm kapsamlar")}</option>
              <option value="kok">{cevir("Yalnız kök (panel sahibi)")}</option>
              {bayiler.map(b => <option key={b.id} value={String(b.id)}>{cevir("Bayi")}: {b.kullanici}</option>)}
            </select>
          </>
        )}
        <span className="ml-auto shrink-0 whitespace-nowrap rounded-full bg-slate-100 dark:bg-dark-700 px-2.5 py-1 text-[11px] font-medium text-slate-500 dark:text-slate-400 tabular-nums">{toplam.toLocaleString('tr-TR')} {cevir("kayıt")}</span>
      </div>

      {yuk && !veri ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
      ) : kayitlar.length === 0 ? (
        <EmptyState baslik={cevir("Kayıt yok")}
          aciklama={cevir("Bu süzgeçlerle eşleşen denetim kaydı bulunamadı. Süzgeçleri temizleyip tekrar deneyin.")} />
      ) : (
        <div className="lg:bg-white dark:lg:bg-dark-700 lg:border lg:border-slate-200 dark:lg:border-dark-600 lg:rounded-lg lg:overflow-hidden">
          <div className="lg:overflow-x-auto">
            <table className={T.tablo}>
              <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800 border-b border-slate-200 dark:border-dark-600`}>
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
                  <tr key={k.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-dark-700 transition`}>
                    <td className={T.hucre} data-etiket={cevir("Zaman")}>
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-400 whitespace-nowrap tabular-nums">{k.zaman}</span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Kullanıcı")}>
                      <span className="inline-flex flex-wrap items-center gap-1.5">
                        <span className="text-slate-700 dark:text-slate-300 break-all">
                          {k.aktor || (k.aktor_rol === 'müşteri' ? cevir('Müşteri') : '—')}
                        </span>
                        {/* Bayi görünümünde her satırın aktörü bayinin kendisidir — rol rozeti tekrar olur. */}
                        {k.aktor_rol && !bayiMi && (
                          <span className="text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded-full font-semibold bg-slate-100 dark:bg-dark-700 text-slate-600 dark:text-slate-400">
                            {cevir(ROL_AD[k.aktor_rol] || k.aktor_rol)}
                          </span>
                        )}
                        {k.adina && (
                          // Müşteri paneline girerek (adına-oturum) yapılan işlem. Hangi
                          // hosting olduğu Hedef kolonunda; alan adı ipucunda da durur.
                          <span title={k.adina_alan ? cevirT(cevir("Müşteri paneline girerek yaptı: {0}"), k.adina_alan) : cevir("Müşteri paneline girerek yaptı")}
                                className="text-[10px] px-1.5 py-0.5 rounded-full font-medium bg-indigo-50 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300 whitespace-nowrap">
                            {cevir("müşteri adına")}
                          </span>
                        )}
                      </span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Eylem")}>
                      <EylemRozet eylem={k.eylem} etiket={etiketle(k.eylem, veri?.etiketler)} />
                      <AciklamaHucre k={k} acik={acikSatir.has(k.id)} degistir={() => satirDegistir(k.id)} />
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
                                      : 'bg-slate-100 dark:bg-dark-700 text-slate-500 dark:text-slate-400 line-through'}`}>
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
          <Button variant="outlined" boyut="sm" ikon={I.geri} onClick={() => setSayfa(s => Math.max(0, s - 1))} disabled={sayfa === 0}>
            {cevir("Önceki")}
          </Button>
          <span className="text-xs text-slate-500 dark:text-slate-400 tabular-nums">{cevir("Sayfa")} {sayfa + 1} / {sonSayfa + 1}</span>
          <Button variant="outlined" boyut="sm" ikonSonda={I.ileri} onClick={() => setSayfa(s => Math.min(sonSayfa, s + 1))} disabled={sayfa >= sonSayfa}>
            {cevir("Sonraki")}
          </Button>
        </div>
      )}
    </div>
  )
}
