import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

type Durum = {
  aktif: boolean
  host: string
  port: number
  kullanici: string
  parola?: string
  prefix: string
  wp_snippet?: string
  wp_baglandi?: number
}


const REDIS_EN: Record<string, string> = {
  "Anahtar öneki": "Key prefix",
  "Bu domain için Redis cache kapalı.": "Redis cache is off for this domain.",
  "Etkinleştirilemedi": "Failed to enable",
  "Etkinleştiriliyor…": "Enabling…",
  "Etkinleştirince izole bir ACL kullanıcısı + bağlantı bilgisi oluşturulur.": "Enabling creates an isolated ACL user + connection info.",
  "Kapatılamadı": "Failed to disable",
  "Kopyalandı ✓": "Copied ✓",
  "Redis Cache Etkinleştir": "Enable Redis Cache",
  "Redis cache etkinleştirildi. WordPress dışı uygulamalar için aşağıdaki bilgileri tanımlayın.": "Redis cache enabled. Define the info below for non-WordPress applications.",
  "Redis cache kapatıldı.": "Redis cache disabled.",
  "Redis cache kapatılsın mı? Bu domaine ait ACL kullanıcısı silinir.": "Disable Redis cache? The ACL user of this domain is deleted.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (REDIS_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function RedisPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const { id } = useParams()
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [mesgul, setMesgul] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [kopyalandi, setKopyalandi] = useState<string | null>(null)

  function yukle() {
    setYuk(true)
    api.get<Durum>(`/domains/${id}/redis`)
      .then(r => setD(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function ac() {
    setHata(null); setBasari(null); setMesgul(true)
    try {
      const { data } = await api.post<Durum>(`/domains/${id}/redis`, {})
      setD(data)
      setBasari(data.wp_baglandi && data.wp_baglandi > 0
        ? cevirT(cevir("Redis cache etkinleştirildi ve {0} WordPress kurulumu otomatik bağlandı — ekstra bir şey yapmanıza gerek yok."), data.wp_baglandi)
        : cevir("Redis cache etkinleştirildi. WordPress dışı uygulamalar için aşağıdaki bilgileri tanımlayın."))
    } catch (e) { setHata(apiHata(e, cevir("Etkinleştirilemedi"))) }
    finally { setMesgul(false) }
  }
  async function kapat() {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: cevir("Redis cache kapatılsın mı? Bu domaine ait ACL kullanıcısı silinir."), tehlike: true }))) return
    setHata(null); setBasari(null); setMesgul(true)
    try {
      await api.delete(`/domains/${id}/redis`)
      yukle()
      setBasari(cevir("Redis cache kapatıldı."))
    } catch (e) { setHata(apiHata(e, cevir("Kapatılamadı"))) }
    finally { setMesgul(false) }
  }

  function kopyala(metin: string, etiket: string) {
    navigator.clipboard?.writeText(metin)
    setKopyalandi(etiket)
    setTimeout(() => setKopyalandi(null), 1500)
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }, { etiket: 'Redis Cache' }]} />
      <div className="flex items-center gap-3 mb-1">
        <span className="text-2xl">⚡</span>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">Redis Cache</h1>
        {d && (
          <span className={`text-xs px-2 py-0.5 rounded-full font-medium ${d.aktif
            ? 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300'
            : 'bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-400'}`}>
            {d.aktif ? '● Aktif' : cevir("Kapalı")}
          </span>
        )}
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
        Bu domaine <strong>izole (kendine ait) bir Redis nesne cache'i</strong> tahsis eder — WordPress ve dinamik uygulamalar veritabanı yükünü azaltıp hızlanır. Diğer siteler bu cache'e erişemez.
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
      ) : !d?.aktif ? (
        <div className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl p-6 text-center">
          <div className="text-3xl mb-2">⚡</div>
          <p className="text-sm text-slate-600 dark:text-slate-300 mb-1">{cevir("Bu domain için Redis cache kapalı.")}</p>
          <p className="text-xs text-slate-400 mb-4">{cevir("Etkinleştirince izole bir ACL kullanıcısı + bağlantı bilgisi oluşturulur.")}</p>
          <button onClick={ac} disabled={mesgul}
            className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-lg disabled:opacity-50">
            {mesgul ? 'Etkinleştiriliyor…' : cevir("Redis Cache Etkinleştir")}
          </button>
        </div>
      ) : (
        <>
          {/* Bağlantı bilgisi */}
          <div className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl overflow-hidden mb-4">
            <div className="px-4 py-3 border-b border-slate-100 dark:border-slate-700/60 flex items-center justify-between">
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Bağlantı Bilgisi")}</h3>
              <button onClick={kapat} disabled={mesgul}
                className="text-xs px-2.5 py-1 border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 rounded-md hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50">
                {cevir("Kapat")}
              </button>
            </div>
            <div className="divide-y divide-slate-100 dark:divide-slate-700/60">
              <SatirKopya etiket={cevir(cevir("Sunucu"))} deger={`${d.host}:${d.port}`} onKopya={kopyala} kopyalandi={kopyalandi} />
              <SatirKopya etiket={cevir("Kullanıcı")} deger={d.kullanici} onKopya={kopyala} kopyalandi={kopyalandi} />
              <SatirKopya etiket={cevir("Parola")} deger={d.parola || ''} gizli onKopya={kopyala} kopyalandi={kopyalandi} />
              <SatirKopya etiket={cevir("Anahtar öneki")} deger={d.prefix} onKopya={kopyala} kopyalandi={kopyalandi} />
            </div>
          </div>

          {/* WordPress snippet */}
          {d.wp_snippet && (
            <div className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl overflow-hidden">
              <div className="px-4 py-3 border-b border-slate-100 dark:border-slate-700/60 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">WordPress kurulumu</h3>
                <button onClick={() => kopyala(d.wp_snippet!, 'wp')}
                  className="text-xs px-2.5 py-1 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-md">
                  {kopyalandi === 'wp' ? 'Kopyalandı ✓' : 'Kopyala'}
                </button>
              </div>
              <div className="p-4">
                <p className="text-xs text-slate-500 dark:text-slate-400 mb-2">
                  {cevir(cevir("1) Aşağıdaki satırları"))} <code className="font-mono bg-slate-100 dark:bg-slate-900 px-1 rounded">wp-config.php</code> {cevir("dosyanıza ekleyin.")}
                  2) WordPress panelinden <strong>Redis Object Cache</strong> eklentisini kurup "Enable Object Cache" deyin.
                </p>
                <pre className="text-[11px] font-mono bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg p-3 overflow-x-auto text-slate-700 dark:text-slate-200 whitespace-pre">{d.wp_snippet}</pre>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function SatirKopya({ etiket, deger, gizli, onKopya, kopyalandi }: {
  etiket: string; deger: string; gizli?: boolean
  onKopya: (m: string, e: string) => void; kopyalandi: string | null
}) {
  const [goster, setGoster] = useState(false)
  const gorunen = gizli && !goster ? '•'.repeat(Math.min(deger.length, 20)) : deger
  return (
    <div className="flex flex-wrap items-center gap-3 px-4 py-2.5">
      <span className="text-xs text-slate-500 dark:text-slate-400 w-28 shrink-0">{etiket}</span>
      <span className="flex-1 font-mono text-xs text-slate-800 dark:text-slate-200 truncate">{gorunen}</span>
      {gizli && (
        <button onClick={() => setGoster(g => !g)} className="text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-200">
          {goster ? 'gizle' : cevir("göster")}
        </button>
      )}
      <button onClick={() => onKopya(deger, etiket)}
        className="text-xs px-2 py-0.5 border border-slate-200 dark:border-slate-700 rounded text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700">
        {kopyalandi === etiket ? '✓' : 'kopyala'}
      </button>
    </div>
  )
}
