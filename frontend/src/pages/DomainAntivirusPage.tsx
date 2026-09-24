import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState } from 'react'
import { useParams, Link, useSearchParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'
import Modal from '@/components/Modal'
import { UyariKutusu } from '@/components/UyariKutusu'

type Bulgu = { dosya: string; imza: string; motor: string; karantina: number }
type Tarama = { id: number; durum: string; motor: string; taranan: number; enfekte: number; baslangic: string; bitis: string }
type Durum = { clamav: boolean; imza_tarihi: string; kullanici: string; son_tarama: Tarama | null; bulgular: Bulgu[] }
type KarantinaKayit = { id: number; orijinal_yol: string; imza: string; seviye: string; puan: number; durum: string; tarih: string; boyut: number; mevcut: boolean }

type Sekme = 'bulgular' | 'karantina'

// Görüntülenen yoldan kiracı ev-öneki (/home/<kullanıcı>/) çıkarılır: hem daha
// KISA hem de sunucu düzenini sızdırmaz — public_html/… göreli yol kalır.
const kisaYol = (p: string) => p.replace(/^\/home\/[^/]+\//, '')

// YolKutu — uzun dosya yolunu SATIR TAŞIRMADAN, kendi içinde yatay kaydırılan
// bordürlü bir kutuda gösterir (tablo satırı asla ikinci satıra düşmez).

const DOMAV_EN: Record<string, string> = {
  "Türkçe": "English",
  "İşlem başarısız": "Operation failed",
  "Onay gerekiyor": "Confirmation required",
  "Dosya karantinaya alınsın mı?\n{0}\n\n(Dosya ~/.karantina altına taşınır ve erişilemez hâle gelir.)": "Quarantine the file?\n{0}\n\n(The file is moved under ~/.karantina and becomes inaccessible.)",
  "Geri yükleme": "Restore",
  "Dosya orijinal konumuna geri yüklensin mi?\n{0}\n\n(Yanlış pozitifse güvenli; gerçek zararlıysa siteyi tekrar riske atar.)": "Restore the file to its original location?\n{0}\n\n(Safe if a false positive; if truly malicious it puts the site at risk again.)",
  "Karantinadaki dosya KALICI silinsin mi?\n{0}\n\n(Geri alınamaz.)": "Permanently delete the quarantined file?\n{0}\n\n(Cannot be undone.)",
  "[ikili dosya]": "[binary file]",
  "Aboneliğe dön": "Back to subscription",
  "dizini GirginOS'un kendi motoruyla taranır: kural zinciri + webshell heuristiği.": "directory is scanned with GirginOS's own engine: rule chain + webshell heuristics.",
  "Motor:": "Engine:",
  "Antivirüs geliştiriliyor — geçici olarak devre dışı (yakında). Tarama yapılmaz, yanlış-pozitif üretmez.": "Antivirus is under development — temporarily disabled (coming soon). No scanning, no false positives.",
  "— kural + heuristik": "— rules + heuristics",
  "Son tarama:": "Last scan:",
  "dosya": "files",
  "bulgu": "findings",
  "Tarama sürüyor… (büyük sitelerde birkaç dakika sürebilir)": "Scan in progress… (may take a few minutes on large sites)",
  "Bulgular": "Findings",
  "Karantina": "Quarantine",
  "Dosya": "File",
  "Motor": "Engine",
  "Karantinada": "Quarantined",
  "Aktif": "Active",
  "Karantinaya al": "Quarantine",
  "Yeniden dene": "Retry",
  "Karantinada dosya yok.": "No files in quarantine.",
  "Tespit": "Detection",
  "Geri yüklendi": "Restored",
  "Silindi": "Deleted",
  "Geri yükle": "Restore",
  "Antivirüs": "Antivirus",
  "G-AV antivirüs": "G-AV antivirus",
  "Kalıcı silme": "Permanent delete",
  "Karantina listesi yüklenemedi.": "Failed to load quarantine list.",
  "Karantinaya alınamadı": "Failed to quarantine",
  "Tarama başlatılamadı": "Failed to start scan",
  "Temiz — zararlı yazılım bulunmadı.": "Clean — no malware found.",
  "İmza": "Signature",
  "İncele": "Inspect",
  "Şimdi Tara": "Scan Now",
  "— Antivirüs": "— Antivirus",
  "Henüz tarama yapılmadı. “Şimdi Tara” ile başlayın.": "No scan yet. Start with “Scan Now”.",
  "Kesik gösterim — yalnızca ilk 64 KB. Dosya daha uzun; kalanı görünmüyor.": "Truncated view — first 64 KB only. The file is longer; the rest is not shown.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DOMAV_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function YolKutu({ yol }: { yol: string }) {
  return (
    <span
      title={yol}
      className="block max-w-full overflow-x-auto whitespace-nowrap font-mono text-xs bg-slate-50 dark:bg-dark-800/40 border border-slate-200 dark:border-dark-600 rounded-md px-2 py-1 text-slate-700 dark:text-slate-200"
    >
      {kisaYol(yol)}
    </span>
  )
}

// Rozet içi küçük ikon (paylaşılan set — components/Ikon).
const RozetIkon = ({ d }: { d: string }) => <Ikon d={d} className="size-3.5 shrink-0" />
// Satır içi buton: boyut="sm" (Tailux) + tek satır.
const SATIR_BTN = 'whitespace-nowrap'

// G-AV marka logosu — GirginOS'un KENDİ antivirüs motoru. (Imunify lisanslı bir
// markadır; hiçbir yerde kullanılmaz.) Gerçek, dolgulu kalkan SVG — jenerik
// tek-çizgi path değil.
function GavLogo({ className = 'w-11 h-11' }: { className?: string }) {
  return (
    <svg viewBox="0 0 48 48" className={className} xmlns="http://www.w3.org/2000/svg" role="img" aria-label={cevir("G-AV antivirüs")}>
      <defs>
        <linearGradient id="gav-shield" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#34d399" />
          <stop offset="1" stopColor="#059669" />
        </linearGradient>
      </defs>
      <path d="M24 3.5l15.5 4.8v10.4c0 10.2-6.6 19.6-15.5 22.3C15.1 38.3 8.5 28.9 8.5 18.7V8.3L24 3.5z" fill="url(#gav-shield)" />
      {/* sağ yarıya hafif gölge → hacim hissi */}
      <path d="M24 3.5l15.5 4.8v10.4c0 10.2-6.6 19.6-15.5 22.3V3.5z" fill="#000" opacity="0.07" />
      {/* onay işareti */}
      <path d="M16.3 24.2l5.2 5.1 9.8-11.4" fill="none" stroke="#fff" strokeWidth="3.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export default function DomainAntivirusPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [tarariyor, setTarariyor] = useState(false)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const [kliste, setKliste] = useState<KarantinaKayit[]>([])
  const [inceleModal, setInceleModal] = useState<{ ad: string; icerik: string; kesik?: boolean } | null>(null)
  const [klHata, setKlHata] = useState(false)
  // Sekme URL'de saklanır (?sekme=): yenilemede/geri gelişte korunur + paylaşılabilir.
  const [sp, setSp] = useSearchParams()
  const sekme: Sekme = sp.get('sekme') === 'karantina' ? 'karantina' : 'bulgular'
  const setSekme = (k: Sekme) => setSp(prev => { prev.set('sekme', k); return prev }, { replace: true })

  function yukle() {
    if (!id) return
    api.get<Durum>(`/domains/${id}/antivirus`).then(r => {
      setD(r.data)
      if (r.data.son_tarama?.durum === 'calisiyor') startPoll(r.data.son_tarama.id)
    }).catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }).finally(() => setYuk(false))
  }
  useEffect(() => { yukle(); kyukle(); return () => { if (pollRef.current) clearInterval(pollRef.current) } }, [id])

  function startPoll(sid: number) {
    setTarariyor(true)
    if (pollRef.current) clearInterval(pollRef.current)
    pollRef.current = setInterval(async () => {
      try {
        const { data } = await api.get<Tarama & { bulgular: Bulgu[] }>(`/domains/${id}/antivirus/tara/${sid}`)
        if (data.durum !== 'calisiyor') {
          if (pollRef.current) clearInterval(pollRef.current)
          setTarariyor(false)
          yukle()
        }
      } catch { if (pollRef.current) clearInterval(pollRef.current); setTarariyor(false) }
    }, 2500)
  }

  async function tara() {
    setHata(null); setTarariyor(true)
    try {
      const { data } = await api.post(`/domains/${id}/antivirus/tara`, {})
      startPoll(data.scan_id)
    } catch (e) { const m = apiHata(e, cevir("Tarama başlatılamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m); setTarariyor(false) }
  }

  async function karantina(b: Bulgu) {
    if (!(await onay({ baslik: cevir("Onay gerekiyor"), mesaj: cevirT(cevir("Dosya karantinaya alınsın mı?\n{0}\n\n(Dosya ~/.karantina altına taşınır ve erişilemez hâle gelir.)"), b.dosya), ton: 'uyari', onayIkonu: I.kilit }))) return
    setHata(null)
    try { await api.post(`/domains/${id}/antivirus/karantina`, { dosya: b.dosya }); yukle(); kyukle() }
    catch (e) { const m = apiHata(e, cevir("Karantinaya alınamadı")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
  }

  async function kyukle() {
    if (!id) return
    try { const { data } = await api.get<{ kayitlar: KarantinaKayit[] }>(`/domains/${id}/antivirus/karantina/liste`); setKliste(data.kayitlar || []); setKlHata(false) } catch { setKlHata(true) }
  }
  async function geriYukle(k: KarantinaKayit) {
    if (!(await onay({ baslik: cevir("Geri yükleme"), mesaj: cevirT(cevir("Dosya orijinal konumuna geri yüklensin mi?\n{0}\n\n(Yanlış pozitifse güvenli; gerçek zararlıysa siteyi tekrar riske atar.)"), k.orijinal_yol), ton: 'uyari', onayIkonu: I.geriAl }))) return
    try { await api.post(`/domains/${id}/antivirus/karantina/${k.id}/geri-yukle`, {}); kyukle(); yukle() }
    catch (e) { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
  }
  async function karSil(k: KarantinaKayit) {
    if (!(await onay({ baslik: cevir("Kalıcı silme"), mesaj: cevirT(cevir("Karantinadaki dosya KALICI silinsin mi?\n{0}\n\n(Geri alınamaz.)"), k.orijinal_yol), ton: 'tehlike', onayIkonu: I.cop }))) return
    try { await api.post(`/domains/${id}/antivirus/karantina/${k.id}/sil`, {}); kyukle(); yukle() }
    catch (e) { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
  }
  async function karIncele(k: KarantinaKayit) {
    try { const { data } = await api.get<{ icerik: string; ikili: boolean; kesik?: boolean }>(`/domains/${id}/antivirus/karantina/${k.id}/incele`); setInceleModal({ ad: k.orijinal_yol, icerik: data.ikili ? cevir("[ikili dosya]") : data.icerik, kesik: data.kesik }) }
    catch (e) { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) }
  }
  if (yuk) return <div className="px-4 py-4 sm:px-6 sm:py-5 text-slate-400">{cevir("Yükleniyor…")}</div>
  if (!d) return <div className="px-4 py-4 sm:px-6 sm:py-5"><div className="text-sm text-red-600">{hata || cevir("Bulunamadı")}</div></div>

  const aktif = d.bulgular.filter(b => !b.karantina)
  const karAktif = kliste.filter(k => k.durum === 'karantina' && k.mevcut).length

  const sekmeBtn = (k: Sekme, etiket: string, rozet: number, vurgu?: 'kirmizi' | 'kehribar') => (
    <button
      onClick={() => setSekme(k)}
      className={`relative px-4 py-2.5 text-sm font-medium border-b-2 -mb-px transition whitespace-nowrap ${
        sekme === k
          ? 'border-brand-500 text-brand-700 dark:text-brand-300'
          : 'border-transparent text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200'
      }`}
    >
      {etiket}
      {rozet > 0 && (
        <Badge variant="soft" color={vurgu === 'kirmizi' ? 'error' : vurgu === 'kehribar' ? 'warning' : 'neutral'}
          className="ml-2 min-w-[1.25rem] h-5 px-1.5 rounded-full text-[11px] font-semibold">{rozet}</Badge>
      )}
    </button>
  )

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="w-full">
        <Link to={`/abonelikler/${id}`} className="inline-flex items-center gap-1 text-sm text-brand-600 dark:text-brand-400 hover:underline mb-2"><Ikon d={I.geri} className="size-4 shrink-0" />{cevir("Aboneliğe dön")}</Link>
        <Breadcrumb items={[
          { etiket: cevir("Anasayfa"), href: '/' },
          { etiket: cevir("Domainler"), href: '/domainler' },
          { etiket: cevir("Antivirüs") },
        ]} />
        <UyariKutusu ton="uyari" className="mb-4">
          {cevir("Antivirüs geliştiriliyor — geçici olarak devre dışı (yakında). Tarama yapılmaz, yanlış-pozitif üretmez.")}
        </UyariKutusu>

        {/* Başlık — gerçek G-AV logo görseli */}
        <div className="flex items-start gap-3 mb-4">
          <GavLogo className="w-11 h-11 shrink-0" />
          <div>
            <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 leading-tight">
              G-AV <span className="text-slate-400 dark:text-slate-500 font-normal text-lg">{cevir("— Antivirüs")}</span>
            </h1>
            <p className="text-sm text-slate-500 dark:text-slate-400">
              <span className="font-mono">public_html</span> {cevir("dizini GirginOS'un kendi motoruyla taranır: kural zinciri + webshell heuristiği.")}
            </p>
          </div>
        </div>

        {/* Durum + eylemler — her zaman üstte */}
        <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-4 shadow-xs">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="text-sm space-y-0.5">
              <div className="flex flex-wrap items-center gap-2">
                <span className="w-2 h-2 rounded-full bg-emerald-500" />
                <span className="text-slate-700 dark:text-slate-200">{cevir("Motor:")} <span className="font-medium">G-AV</span> <span className="text-xs text-slate-400">{cevir("— kural + heuristik")}</span></span>
              </div>
              {d.son_tarama && <div className="text-xs text-slate-400 ml-4">
                {cevir("Son tarama:")} {d.son_tarama.bitis || d.son_tarama.baslangic} · {d.son_tarama.taranan} {cevir("dosya")} · {d.son_tarama.enfekte} {cevir("bulgu")}
              </div>}
            </div>
            <div className="flex gap-2">
              <Button color="primary" onClick={tara} yukleniyor={tarariyor} ikon={I.oynat}>
                {tarariyor ? cevir("Taranıyor…") : cevir("Şimdi Tara")}</Button>
            </div>
          </div>
          {tarariyor && (
            <div className="mt-3 flex items-center gap-2 text-sm text-brand-600 dark:text-brand-400">
              <span className="inline-block w-4 h-4 border-2 border-brand-500 border-t-transparent rounded-full animate-spin" />
              {cevir("Tarama sürüyor… (büyük sitelerde birkaç dakika sürebilir)")}
            </div>
          )}
        </div>

        {/* Sekme çubuğu — sayfa aşağı inmesin diye içerik sekmelere bölündü */}
        <div className="border-b border-slate-200 dark:border-dark-600 flex gap-1 mb-4 overflow-x-auto">
          {sekmeBtn('bulgular', cevir("Bulgular"), aktif.length, 'kirmizi')}
          {sekmeBtn('karantina', cevir("Karantina"), karAktif, 'kehribar')}
        </div>

        {/* ── BULGULAR SEKMESİ ── */}
        {sekme === 'bulgular' && (
          <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-4 sm:p-5 shadow-xs">
            {!d.son_tarama ? (
              <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">{cevir("Henüz tarama yapılmadı. “Şimdi Tara” ile başlayın.")}</div>
            ) : d.bulgular.length === 0 ? (
              <div className="text-center py-10">
                <GavLogo className="w-12 h-12 mx-auto mb-2 opacity-90" />
                <p className="text-sm text-emerald-600 dark:text-emerald-400 font-medium">{cevir("Temiz — zararlı yazılım bulunmadı.")}</p>
              </div>
            ) : (
              <div className="overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}>
                    <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-dark-600">
                      <th className={T.baslik}>{cevir("Dosya")}</th><th className={T.baslik}>{cevir("İmza")}</th><th className={T.baslik}>{cevir("Motor")}</th><th className={T.baslik}>{cevir("Durum")}</th><th className={T.baslik}></th>
                    </tr>
                  </thead>
                  <tbody className={T.govde}>
                    {d.bulgular.map((b, i) => (
                      <tr key={i} className={T.satir}>
                        <td className={`${T.hucreBaslik} lg:min-w-[22rem]`}><YolKutu yol={b.dosya} /></td>
                        <td className={T.hucre} data-etiket={cevir("İmza")}>
                          <span className="text-slate-700 dark:text-slate-200 text-right lg:text-left break-all">{b.imza}</span>
                        </td>
                        <td className={T.hucre} data-etiket={cevir("Motor")}><Badge variant="soft">{b.motor}</Badge></td>
                        <td className={T.hucre} data-etiket={cevir("Durum")}>
                          {b.karantina ? <Badge color="warning" variant="soft" className="gap-1"><RozetIkon d={I.kilit} /> {cevir("Karantinada")}</Badge>
                            : <Badge color="error" variant="soft">{cevir("Aktif")}</Badge>}
                        </td>
                        <td className={`${T.hucreAksiyon} lg:text-right ${b.karantina ? 'hidden lg:table-cell' : ''}`}>
                          {!b.karantina && (
                            <Button color="error" variant="soft" boyut="sm" ikon={I.kilit} onClick={() => karantina(b)} className={`${SATIR_BTN} lg:ml-auto`}>
                              {cevir("Karantinaya al")}
                            </Button>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ── KARANTİNA SEKMESİ ── */}
        {sekme === 'karantina' && (
          <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-4 sm:p-5 shadow-xs">
            {klHata && kliste.length === 0 && (
              <div className="text-sm text-amber-600 dark:text-amber-400 flex items-center gap-2 py-2">{cevir("Karantina listesi yüklenemedi.")} <Button variant="outlined" boyut="sm" ikon={I.yenile} onClick={kyukle}>{cevir("Yeniden dene")}</Button></div>
            )}
            {!klHata && kliste.length === 0 ? (
              <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">{cevir("Karantinada dosya yok.")}</div>
            ) : (
              <div className="overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}>
                    <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-dark-600">
                      <th className={T.baslik}>{cevir("Dosya")}</th><th className={T.baslik}>{cevir("Tespit")}</th><th className={T.baslik}>{cevir("Durum")}</th><th className={T.baslik}>{cevir("Tarih")}</th><th className={T.baslik}></th>
                    </tr>
                  </thead>
                  <tbody className={T.govde}>
                    {kliste.map(k => (
                      <tr key={k.id} className={T.satir}>
                        <td className={`${T.hucreBaslik} lg:min-w-[22rem]`}><YolKutu yol={k.orijinal_yol} /></td>
                        <td className={T.hucre} data-etiket={cevir("Tespit")}><span className="text-xs text-slate-600 dark:text-slate-300 break-all">{k.imza} <span className="text-slate-400">({k.puan})</span></span></td>
                        <td className={T.hucre} data-etiket={cevir("Durum")}>
                          {k.durum === 'karantina' ? <Badge color="warning" variant="soft" className="gap-1"><RozetIkon d={I.kilit} /> {cevir("Karantinada")}</Badge>
                            : k.durum === 'geri_yuklendi' ? <Badge color="success" variant="soft" className="gap-1"><RozetIkon d={I.geriAl} /> {cevir("Geri yüklendi")}</Badge>
                            : <Badge variant="soft" className="gap-1"><RozetIkon d={I.cop} /> {cevir("Silindi")}</Badge>}
                        </td>
                        <td className={T.hucre} data-etiket={cevir("Tarih")}><span className="text-xs text-slate-400">{k.tarih}</span></td>
                        <td className={`${T.hucreAksiyon} lg:text-right`}>
                          {k.durum === 'karantina' && k.mevcut && (
                            <span className="flex flex-wrap gap-1.5 lg:justify-end">
                              <Button variant="outlined" boyut="sm" ikon={I.goz} onClick={() => karIncele(k)} className={SATIR_BTN}>{cevir("İncele")}</Button>
                              <Button color="success" variant="soft" boyut="sm" ikon={I.geriAl} onClick={() => geriYukle(k)} className={SATIR_BTN}>{cevir("Geri yükle")}</Button>
                              <Button color="error" variant="soft" boyut="sm" ikon={I.cop} onClick={() => karSil(k)} className={SATIR_BTN}>{cevir("Sil")}</Button>
                            </span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* İnceleme modalı — dosya ÇALIŞTIRILMADAN düz metin gösterilir */}
        <Modal acik={!!inceleModal} genislik="xl" onKapat={() => setInceleModal(null)} baslik={cevir("İncele")}>
          {inceleModal && (
            <>
              <div className="mb-3 font-mono text-sm break-all text-slate-700 dark:text-slate-200">{inceleModal.ad}</div>
              {inceleModal.kesik && <UyariKutusu ton="uyari" className="mb-3">{cevir("Kesik gösterim — yalnızca ilk 64 KB. Dosya daha uzun; kalanı görünmüyor.")}</UyariKutusu>}
              <pre className="overflow-auto text-xs font-mono text-slate-800 dark:text-slate-200 whitespace-pre-wrap break-all">{inceleModal.icerik}</pre>
            </>
          )}
        </Modal>
      </div>
    </div>
  )
}
