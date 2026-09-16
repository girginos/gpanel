import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button, Badge } from '@/components/ui'

type Durum = {
  aktif: boolean
  host: string
  port: number
  kullanici: string
  prefix: string
  wp_baglandi?: number
}

// Sir: parola + wp_snippet artik Durum'da (GET) GELMEZ (CWE-200) ve reveal ucu KALDIRILDI.
// Yalniz Ac/enable (POST /domains/{id}/redis) yanitinda TEK SEFER doner; state'te tutulup
// aktiflestirme/yeniden-uretme aninda bir kez gosterilir, sayfa yenilenince kaybolur.
type Sir = {
  parola: string
  wp_snippet: string
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
  "Redis cache etkinleştirildi ve {0} WordPress kurulumu otomatik bağlandı — ekstra bir şey yapmanıza gerek yok.": "Redis cache enabled and {0} WordPress installation(s) connected automatically — you don't need to do anything extra.",
  "Emin misiniz?": "Are you sure?",
  "Anasayfa": "Home",
  "● Aktif": "● Active",
  "Bu domaine": "For this domain, an",
  "izole (kendine ait) bir Redis nesne cache'i": "isolated (dedicated) Redis object cache",
  "tahsis eder — WordPress ve dinamik uygulamalar veritabanı yükünü azaltıp hızlanır. Diğer siteler bu cache'e erişemez.": "is allocated — WordPress and dynamic apps reduce database load and speed up. Other sites cannot access this cache.",
  "Sunucu": "Server",
  "WordPress kurulumu": "WordPress setup",
  "Diğer uygulamalar için elle kurulum": "Manual setup for other applications",
  "Kopyala": "Copy",
  "1) Aşağıdaki satırları": "1) Add the lines below to your",
  "dosyanıza ekleyin.": "file.",
  "2) WordPress panelinden": "2) From the WordPress panel, install the",
  " eklentisini kurup \"Enable Object Cache\" deyin.": " plugin and click \"Enable Object Cache\".",
  "göster": "show",
  "gizle": "hide",
  "kopyala": "copy",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
  "Bu parola yalnızca şimdi gösteriliyor — güvenli bir yere kaydedin. Sayfadan ayrıldığınızda tekrar gösterilemez.": "This password is shown only now — save it somewhere safe. It cannot be shown again once you leave this page.",
  "Parola yalnızca oluşturma anında gösterilir.": "The password is shown only at creation time.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (REDIS_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function RedisPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const { id } = useParams()
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [mesgul, setMesgul] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [kopyalandi, setKopyalandi] = useState<string | null>(null)
  // Snippet karti: WP otomatik baglandiysa varsayilan KATLI acilir (bilgi SILINMEZ).
  // null = kullanici dokunmadi -> varsayilani otoBagli belirler.
  const [snippetAcik, setSnippetAcik] = useState<boolean | null>(null)
  const [snippetGoster, setSnippetGoster] = useState(false)
  // Sir (parola + wp_snippet) yalniz Ac/enable yanitindan gelir; tek-seferlik gosterim icin
  // state'te tutulur (reveal ucu yok). yukle()/kapat() ile sifirlanir -> tekrar gosterilmez.
  const [sir, setSir] = useState<Sir | null>(null)

  function yukle() {
    setYuk(true); setSir(null) // domain/durum degisince sir yeniden gizlensin
    api.get<Durum>(`/domains/${id}/redis`)
      .then(r => setD(r.data))
      .catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function ac() {
    setHata(null); setBasari(null); setMesgul(true)
    try {
      // Ac/enable (ve yeniden-uret) yaniti parola + wp_snippet'i TEK SEFER doner.
      const { data } = await api.post<Durum & Sir>(`/domains/${id}/redis`, {})
      setD(data)
      // Tek-seferlik siri state'e koy: panelde hemen bir kez gosterilir; reveal cagrisi YOK.
      setSir({ parola: data.parola, wp_snippet: data.wp_snippet })
      const ok = data.wp_baglandi && data.wp_baglandi > 0
        ? cevirT(cevir("Redis cache etkinleştirildi ve {0} WordPress kurulumu otomatik bağlandı — ekstra bir şey yapmanıza gerek yok."), data.wp_baglandi)
        : cevir("Redis cache etkinleştirildi. WordPress dışı uygulamalar için aşağıdaki bilgileri tanımlayın.")
      setBasari(ok)
      toast.basari(cevir("Kaydedildi"), ok)
    } catch (e) {
      const m = apiHata(e, cevir("Etkinleştirilemedi"))
      setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setMesgul(false) }
  }
  async function kapat() {
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevir("Redis cache kapatılsın mı? Bu domaine ait ACL kullanıcısı silinir."), tehlike: true }))) return
    setHata(null); setBasari(null); setMesgul(true)
    try {
      await api.delete(`/domains/${id}/redis`)
      yukle()
      setBasari(cevir("Redis cache kapatıldı."))
      toast.basari(cevir("Redis cache kapatıldı."))
    } catch (e) {
      const m = apiHata(e, cevir("Kapatılamadı"))
      setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setMesgul(false) }
  }

  function kopyala(metin: string, etiket: string) {
    navigator.clipboard?.writeText(metin)
    setKopyalandi(etiket)
    setTimeout(() => setKopyalandi(null), 1500)
  }

  // Parola snippet'te DUZ METIN geliyor (Ac/enable yanitindan) — ekranda maskelenir.
  // Kopyala HAM sir.wp_snippet'i kopyalar: kopyalanan icerik BOZULMAZ.
  const otoBagli = (d?.wp_baglandi || 0) > 0
  const snippetAcikGercek = snippetAcik === null ? !otoBagli : snippetAcik
  const snippetGorunen = !sir?.wp_snippet
    ? ''
    : (snippetGoster || !sir.parola ? sir.wp_snippet : sir.wp_snippet.split(sir.parola).join('••••••••'))

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' }, { etiket: 'Redis Cache' }]} />
      <div className="flex items-center gap-3 mb-1">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-6 w-6 text-amber-500"><path d="M13 2L3 14h7l-1 8 10-12h-7l1-8z" /></svg>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">Redis Cache</h1>
        {d && (
          <Badge color={d.aktif ? 'success' : 'neutral'} variant="soft" className="text-xs px-2 py-0.5">
            {d.aktif ? cevir("● Aktif") : cevir("Kapalı")}
          </Badge>
        )}
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
        {cevir("Bu domaine")} <strong>{cevir("izole (kendine ait) bir Redis nesne cache'i")}</strong> {cevir("tahsis eder — WordPress ve dinamik uygulamalar veritabanı yükünü azaltıp hızlanır. Diğer siteler bu cache'e erişemez.")}
      </p>

      {yuk ? (
        <div className="py-12 text-center text-sm text-slate-400">{cevir("Yükleniyor…")}</div>
      ) : !d?.aktif ? (
        <div className="bg-white dark:bg-dark-700/60 border border-slate-200 dark:border-dark-600/60 rounded-lg p-6 text-center">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round" className="h-7 w-7 mx-auto mb-2 text-slate-400"><path d="M13 2L3 14h7l-1 8 10-12h-7l1-8z" /></svg>
          <p className="text-sm text-slate-600 dark:text-slate-300 mb-1">{cevir("Bu domain için Redis cache kapalı.")}</p>
          <p className="text-xs text-slate-400 mb-4">{cevir("Etkinleştirince izole bir ACL kullanıcısı + bağlantı bilgisi oluşturulur.")}</p>
          <Button onClick={ac} disabled={mesgul} className="px-4 py-2 text-sm">
            {mesgul ? cevir("Etkinleştiriliyor…") : cevir("Redis Cache Etkinleştir")}
          </Button>
        </div>
      ) : (
        <>
          {/* Bağlantı bilgisi */}
          <div className="bg-white dark:bg-dark-700/60 border border-slate-200 dark:border-dark-600/60 rounded-lg overflow-hidden mb-4">
            <div className="px-4 py-3 border-b border-slate-100 dark:border-dark-600/60 flex items-center justify-between">
              <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Bağlantı Bilgisi")}</h3>
              <Button onClick={kapat} disabled={mesgul} color="error" variant="outlined" className="text-xs px-2.5 py-1">
                {cevir("Kapat")}
              </Button>
            </div>
            <div className="divide-y divide-slate-100 dark:divide-dark-600/60">
              {sir && (
                <div className="px-4 py-2.5 bg-amber-50 dark:bg-amber-500/10">
                  <p className="text-xs text-amber-700 dark:text-amber-400">{cevir("Bu parola yalnızca şimdi gösteriliyor — güvenli bir yere kaydedin. Sayfadan ayrıldığınızda tekrar gösterilemez.")}</p>
                </div>
              )}
              <SatirKopya etiket={cevir("Sunucu")} deger={`${d.host}:${d.port}`} onKopya={kopyala} kopyalandi={kopyalandi} />
              <SatirKopya etiket={cevir("Kullanıcı")} deger={d.kullanici} onKopya={kopyala} kopyalandi={kopyalandi} />
              {sir ? (
                <SatirKopya etiket={cevir("Parola")} deger={sir.parola} gizli baslangicGoster onKopya={kopyala} kopyalandi={kopyalandi} />
              ) : (
                <div className="flex flex-wrap items-center gap-3 px-4 py-2.5">
                  <span className="text-xs text-slate-500 dark:text-slate-400 w-28 shrink-0">{cevir("Parola")}</span>
                  <span className="flex-1 font-mono text-xs text-slate-400 dark:text-slate-500 truncate">••••••••</span>
                  <span className="text-xs text-slate-400 dark:text-slate-500">{cevir("Parola yalnızca oluşturma anında gösterilir.")}</span>
                </div>
              )}
              <SatirKopya etiket={cevir("Anahtar öneki")} deger={d.prefix} onKopya={kopyala} kopyalandi={kopyalandi} />
            </div>
          </div>

          {/* WordPress snippet — otomatik baglandiysa "elle kurulum" olarak katli */}
          {sir?.wp_snippet && (
            <div className="bg-white dark:bg-dark-700/60 border border-slate-200 dark:border-dark-600/60 rounded-lg overflow-hidden">
              <div className="px-4 py-3 border-b border-slate-100 dark:border-dark-600/60 flex items-center justify-between gap-2">
                <button type="button" onClick={() => setSnippetAcik(!snippetAcikGercek)}
                  className="flex items-center gap-1.5 text-sm font-semibold text-slate-700 dark:text-slate-200">
                  <span className={`text-slate-400 transition-transform ${snippetAcikGercek ? 'rotate-90' : ''}`}>›</span>
                  {otoBagli ? cevir("Diğer uygulamalar için elle kurulum") : cevir("WordPress kurulumu")}
                </button>
                {snippetAcikGercek && (
                  <span className="flex items-center gap-2 shrink-0">
                    <button type="button" onClick={() => setSnippetGoster(g => !g)} className="text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-200">
                      {snippetGoster ? cevir("gizle") : cevir("göster")}
                    </button>
                    <Button onClick={() => kopyala(sir!.wp_snippet, 'wp')} className="text-xs px-2.5 py-1">
                      {kopyalandi === 'wp' ? cevir("Kopyalandı ✓") : cevir("Kopyala")}
                    </Button>
                  </span>
                )}
              </div>
              {snippetAcikGercek && (
              <div className="p-4">
                <p className="text-xs text-slate-500 dark:text-slate-400 mb-2">
                  {cevir("1) Aşağıdaki satırları")} <code className="font-mono bg-slate-100 dark:bg-dark-800 px-1 rounded">wp-config.php</code> {cevir("dosyanıza ekleyin.")}
                  {cevir("2) WordPress panelinden")} <strong>Redis Object Cache</strong>{cevir(" eklentisini kurup \"Enable Object Cache\" deyin.")}
                </p>
                <pre className="text-[11px] font-mono bg-slate-50 dark:bg-dark-800 border border-slate-200 dark:border-dark-600 rounded-lg p-3 overflow-x-auto text-slate-700 dark:text-slate-200 whitespace-pre">{snippetGorunen}</pre>
              </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}

function SatirKopya({ etiket, deger, gizli, baslangicGoster, onKopya, kopyalandi }: {
  etiket: string; deger: string; gizli?: boolean; baslangicGoster?: boolean
  onKopya: (m: string, e: string) => void; kopyalandi: string | null
}) {
  const [goster, setGoster] = useState(!!baslangicGoster)
  const gorunen = gizli && !goster ? '•'.repeat(Math.min(deger.length, 20)) : deger
  return (
    <div className="flex flex-wrap items-center gap-3 px-4 py-2.5">
      <span className="text-xs text-slate-500 dark:text-slate-400 w-28 shrink-0">{etiket}</span>
      <span className="flex-1 font-mono text-xs text-slate-800 dark:text-slate-200 truncate">{gorunen}</span>
      {gizli && (
        <button onClick={() => setGoster(g => !g)} className="text-xs text-slate-400 hover:text-slate-600 dark:hover:text-slate-200">
          {goster ? cevir("gizle") : cevir("göster")}
        </button>
      )}
      <Button onClick={() => onKopya(deger, etiket)} variant="outlined" className="text-xs px-2 py-0.5">
        {kopyalandi === etiket ? '✓' : cevir("kopyala")}
      </Button>
    </div>
  )
}
