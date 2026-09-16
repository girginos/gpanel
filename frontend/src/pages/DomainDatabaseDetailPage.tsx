import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useRef, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { Ikon, I } from '@/components/Ikon'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import ConfirmDialog from '@/components/ConfirmDialog'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { DB, boyutFmt, PwResetModal } from './DomainDatabasesPage'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }


const DBDETAIL_EN: Record<string, string> = {
  "Listeye dön": "Back to list",
  "İşlem başarısız": "Operation failed",
  "Optimize Tamamlandı": "Optimization Complete",
  "Optimize başarısız": "Optimization failed",
  "Veritabanı adı": "Database name",
  "Veritabanı bulunamadı": "Database not found",
  "Veritabanını Optimize Et": "Optimize Database",
  "Veritabanını sil": "Delete database",
  "phpMyAdmin token alınamadı": "Failed to get phpMyAdmin token",
  "phpMyAdmin'de Aç": "Open in phpMyAdmin",
  "Bilgi": "Info",
  "\"{0}\" içindeki tüm tablolar optimize edilecek (fragmentasyon giderilir, kullanılmayan alan geri kazanılır). Devam edilsin mi?": "All tables in \"{0}\" will be optimized (fragmentation removed, unused space reclaimed). Continue?",
  "Optimize Et": "Optimize",
  "\"{0}\" optimize edildi. {1} alan geri kazanıldı ({2} → {3}).": "\"{0}\" optimized. {1} of space reclaimed ({2} → {3}).",
  "\"{0}\" optimize edildi. Tablolar zaten derli topluydu.": "\"{0}\" optimized. The tables were already compact.",
  "Anasayfa": "Home",
  "Sunucu": "Server",
  "Gizle": "Hide",
  "✓ Kopyalandı": "✓ Copied",
  "Kopyala": "Copy",
  "Optimize ediliyor…": "Optimizing…",
  "Evet, sil": "Yes, delete",
  "\"{0}\" veritabanı ve kullanıcısı kalıcı silinecek. Bu işlem geri alınamaz!": "The database \"{0}\" and its user will be permanently deleted. This action cannot be undone!",
  "Parola alınamadı": "Failed to get password",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (DBDETAIL_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainDatabaseDetailPage() {
  useTranslation() // dil re-render aboneligi
  const { id, dbid } = useParams()
  const nav = useNavigate()
  const { bilgi, onay } = useDialog()
  const toast = useToast()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [db, setDb] = useState<DB | null>(null)
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [optCalisiyor, setOptCalisiyor] = useState(false)
  const [pwReset, setPwReset] = useState(false)
  const [silOnay, setSilOnay] = useState(false)

  const yukleNesli = useRef(0)
  function yukle() {
    if (!id || !dbid) return
    setYuk(true); setHata(null)
    const _n = ++yukleNesli.current
    api.get<DB[]>(`/domains/${id}/databases`)
      .then(r => {
        if (_n !== yukleNesli.current) return
        const bulunan = r.data.find(x => String(x.id) === String(dbid))
        if (!bulunan) { setHata(cevir("Veritabanı bulunamadı")); toast.hata(cevir("Veritabanı bulunamadı")); setDb(null) }
        else setDb(bulunan)
      })
      .catch(e => { if (_n !== yukleNesli.current) return; const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => { if (_n === yukleNesli.current) setYuk(false) })
  }

  useEffect(() => {
    let iptal = false
    if (id) api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(() => {})
    yukle()
    return () => { iptal = true; yukleNesli.current++ }
  }, [id, dbid])

  async function pmaAc() {
    if (!db) return
    try {
      const { data } = await api.post<{ signon_url: string }>(`/databases/${db.id}/pma-token`)
      // Güvenlik (open-redirect / CWE-601): signon_url sunucudan gelen göreli yol
      // ("/pma-signon.php?t=..."). Yine de yalnız AYNI KÖKEN'e açılmasına izin ver —
      // "//evil.com" veya mutlak dış URL reddedilir.
      const hedef = new URL(data.signon_url, window.location.origin)
      if (hedef.origin !== window.location.origin) return
      window.open(hedef.href, '_blank', 'noopener')
    } catch (e) {
      (await bilgi({ baslik: cevir("Bilgi"), mesaj: apiHata(e, cevir("phpMyAdmin token alınamadı")) }))
    }
  }

  async function optimizeEt() {
    if (!db || optCalisiyor) return
    if (!(await onay({
      baslik: cevir("Veritabanını Optimize Et"),
      mesaj: cevirT(cevir("\"{0}\" içindeki tüm tablolar optimize edilecek (fragmentasyon giderilir, kullanılmayan alan geri kazanılır). Devam edilsin mi?"), db.db_adi),
      onayEtiketi: cevir("Optimize Et"),
    }))) return
    setOptCalisiyor(true)
    try {
      const { data } = await api.post(`/databases/${db.id}/optimize`)
      const kaz = Number(data?.kazanilan_bayt || 0)
      await bilgi({
        baslik: cevir("Optimize Tamamlandı"),
        mesaj: kaz > 0
          ? cevirT(cevir("\"{0}\" optimize edildi. {1} alan geri kazanıldı ({2} → {3})."), db.db_adi, boyutFmt(kaz), boyutFmt(Number(data?.once_bayt || 0)), boyutFmt(Number(data?.sonra_bayt || 0)))
          : cevirT(cevir("\"{0}\" optimize edildi. Tablolar zaten derli topluydu."), db.db_adi),
      })
      yukle()
    } catch (e) {
      await bilgi({ baslik: cevir("Bilgi"), mesaj: apiHata(e, cevir("Optimize başarısız")) })
    } finally {
      setOptCalisiyor(false)
    }
  }

  async function sil() {
    if (!db) return
    try {
      await api.delete(`/databases/${db.id}`)
      setSilOnay(false)
      nav(`/abonelikler/${id}/veritabanlari`)
    } catch (e) {
      (await bilgi({ baslik: cevir("Bilgi"), mesaj: apiHata(e, cevir("Silme başarısız")) }))
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[900px]">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("Veritabanları"), href: `/abonelikler/${id}/veritabanlari` },
        { etiket: db?.db_adi || '...' },
      ]} />

      <div className="flex items-center gap-3 mb-5">
        <Link to={`/abonelikler/${id}/veritabanlari`} className="text-slate-400 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300" title={cevir("Listeye dön")}>
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}><path strokeLinecap="round" strokeLinejoin="round" d="M15 19l-7-7 7-7" /></svg>
        </Link>
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 font-mono truncate">{db?.db_adi || cevir("Veritabanı")}</h1>
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div> : db && (
        <div className="space-y-4">
          {/* Bağlantı bilgileri */}
          <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5">
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">{cevir("Bağlantı Bilgileri")}</h2>
            <dl className="space-y-3">
              <Satir e={cevir("Veritabanı adı")} v={db.db_adi} mono />
              <Satir e={cevir("Kullanıcı")} v={db.db_kullanici || cevir("— tanımlı değil")} mono />
              <Satir e={cevir("Sunucu")} v={`${db.db_host}:3306`} mono />
              <Satir e={cevir("Parola")} v="••••••••••••" mono />
              <Satir e={cevir("Boyut")} v={boyutFmt(db.boyut)} mono />
              <Satir e={cevir("Oluşturulma")} v={db.olusturulma} />
            </dl>
          </div>

          {/* İşlemler */}
          <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5">
            <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-4">{cevir("İşlemler")}</h2>
            <div className="flex flex-wrap gap-2">
              <button onClick={pmaAc} className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-indigo-50 dark:bg-indigo-900/20 text-indigo-700 dark:text-indigo-300 hover:bg-indigo-100 dark:hover:bg-indigo-900/40 rounded-md"><Ikon d={I.kilitAcik} className="h-4 w-4" /> {cevir("phpMyAdmin'de Aç")}</button>
              <button onClick={() => setPwReset(true)} className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-brand-50 dark:bg-brand-900/20 text-brand-700 dark:text-brand-300 hover:bg-brand-100 dark:hover:bg-brand-900/40 rounded-md"><Ikon d={I.anahtar} className="h-4 w-4" /> {db.db_kullanici ? cevir("Parola Sıfırla") : cevir("Kullanıcı Oluştur")}</button>
              <button onClick={optimizeEt} disabled={optCalisiyor} className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-emerald-50 dark:bg-emerald-900/20 text-emerald-700 dark:text-emerald-300 hover:bg-emerald-100 dark:hover:bg-emerald-900/40 rounded-md disabled:opacity-50"><Ikon d={I.simsek} className={`h-4 w-4 ${optCalisiyor ? 'animate-pulse' : ''}`} /> {optCalisiyor ? cevir("Optimize ediliyor…") : cevir("Optimize Et")}</button>
              <button onClick={() => setSilOnay(true)} className="inline-flex items-center gap-1.5 px-3 py-2 text-sm bg-red-50 dark:bg-red-900/20 text-red-700 dark:text-red-300 hover:bg-red-100 dark:hover:bg-red-900/40 rounded-md ml-auto"><Ikon d={I.cop} className="h-4 w-4" /> {cevir("Sil")}</button>
            </div>
          </div>
        </div>
      )}

      {pwReset && db && (
        <PwResetModal db={db} onKapat={() => setPwReset(false)} onTamam={() => { setPwReset(false); yukle() }} />
      )}

      <ConfirmDialog
        acik={silOnay}
        baslik={cevir("Veritabanını sil")}
        mesaj={cevirT(cevir("\"{0}\" veritabanı ve kullanıcısı kalıcı silinecek. Bu işlem geri alınamaz!"), db?.db_adi)}
        tehlikeli
        onayMetni={cevir("Evet, sil")}
        onOnay={sil}
        onIptal={() => setSilOnay(false)}
      />
    </div>
  )
}

function Satir({ e, v, mono }: { e: string; v: string; mono?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-3 py-1.5">
      <dt className="text-sm text-slate-500 dark:text-slate-400">{e}</dt>
      <dd className={`text-sm text-slate-800 dark:text-slate-200 text-right break-all ${mono ? 'font-mono' : ''}`}>{v}</dd>
    </div>
  )
}
