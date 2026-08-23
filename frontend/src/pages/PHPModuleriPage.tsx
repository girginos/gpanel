import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

type Surum = { surum: string; ini_dir: string; service: string }
type Ext = { adi: string; aktif: boolean; ini_dosya: string }

const ZORUNLU = new Set([
  'core', 'date', 'standard', 'pdo', 'mysqlnd', 'phar', 'spl', 'reflection',
  'session', 'pcre', 'tokenizer', 'json', 'hash', 'random', 'libxml',
])

// KATALOG — curated (hazır seç-kur) eklenti listesi. Serbest-metin PECL yerine
// yaygın/anlamlı eklentiler kategorili + açıklamalı. Kurulum mevcut async
// PECL-Kur akışını kullanır (bundled dnf → pecl dnf → kaynaktan derleme).
// anahtar = kurulum için gönderilen ad (backend aday paketleri kendisi arar).
type KatalogEk = { ad: string; anahtar: string; aciklama: string }
const KATALOG: { kategori: string; ekler: KatalogEk[] }[] = [
  { kategori: 'Veritabanı', ekler: [
    { ad: 'PostgreSQL', anahtar: 'pgsql', aciklama: 'PostgreSQL bağlantısı (PDO dahil)' },
    { ad: 'Redis', anahtar: 'redis', aciklama: 'Redis in-memory veri deposu istemcisi' },
    { ad: 'MongoDB', anahtar: 'mongodb', aciklama: 'MongoDB NoSQL sürücüsü' },
    { ad: 'SQLite3', anahtar: 'sqlite3', aciklama: 'Gömülü SQLite veritabanı' },
    { ad: 'OCI8 (Oracle)', anahtar: 'oci8', aciklama: 'Oracle veritabanı bağlantısı' },
    { ad: 'ODBC', anahtar: 'odbc', aciklama: 'ODBC üzerinden veritabanı erişimi' },
    { ad: 'DBA', anahtar: 'dba', aciklama: 'Anahtar-değer veritabanı katmanı' },
  ]},
  { kategori: 'Önbellek & Performans', ekler: [
    { ad: 'APCu', anahtar: 'apcu', aciklama: 'Kullanıcı verisi in-memory önbelleği' },
    { ad: 'Memcached', anahtar: 'memcached', aciklama: 'Memcached dağıtık önbellek istemcisi' },
    { ad: 'OPcache', anahtar: 'opcache', aciklama: 'PHP bytecode önbelleği (genelde kurulu)' },
    { ad: 'igbinary', anahtar: 'igbinary', aciklama: 'Hızlı ikili serileştirme' },
  ]},
  { kategori: 'Görüntü & Medya', ekler: [
    { ad: 'ImageMagick', anahtar: 'imagick', aciklama: 'Gelişmiş görüntü işleme' },
    { ad: 'GD', anahtar: 'gd', aciklama: 'Temel görüntü oluşturma/işleme' },
    { ad: 'EXIF', anahtar: 'exif', aciklama: 'Görüntü meta verisi okuma' },
  ]},
  { kategori: 'Uluslararasılaştırma', ekler: [
    { ad: 'intl', anahtar: 'intl', aciklama: 'Unicode, yerelleştirme, çeviri' },
    { ad: 'gettext', anahtar: 'gettext', aciklama: 'GNU gettext çeviri sistemi' },
    { ad: 'mbstring', anahtar: 'mbstring', aciklama: 'Çok baytlı dize işleme' },
  ]},
  { kategori: 'Geliştirme & Hata Ayıklama', ekler: [
    { ad: 'Xdebug', anahtar: 'xdebug', aciklama: 'Adım adım hata ayıklama + profil' },
    { ad: 'SPX', anahtar: 'spx', aciklama: 'Basit performans profilleyici' },
    { ad: 'AST', anahtar: 'ast', aciklama: 'Soyut sözdizim ağacı (statik analiz)' },
  ]},
  { kategori: 'Ağ & Servisler', ekler: [
    { ad: 'SOAP', anahtar: 'soap', aciklama: 'SOAP web servisleri' },
    { ad: 'IMAP', anahtar: 'imap', aciklama: 'IMAP/POP3 e-posta erişimi (sunucuda paket varsa)' },
    { ad: 'SSH2', anahtar: 'ssh2', aciklama: 'SSH / SFTP istemcisi' },
    { ad: 'Sockets', anahtar: 'sockets', aciklama: 'Düşük seviye soket erişimi' },
    { ad: 'AMQP', anahtar: 'amqp', aciklama: 'RabbitMQ / AMQP mesaj kuyruğu' },
    { ad: 'gRPC', anahtar: 'grpc', aciklama: 'gRPC uzak yordam çağrısı' },
  ]},
  { kategori: 'Sıkıştırma', ekler: [
    { ad: 'Zip', anahtar: 'zip', aciklama: 'ZIP arşiv okuma/yazma' },
    { ad: 'BZ2', anahtar: 'bz2', aciklama: 'bzip2 sıkıştırma' },
    { ad: 'Brotli', anahtar: 'brotli', aciklama: 'Brotli sıkıştırma' },
    { ad: 'LZ4', anahtar: 'lz4', aciklama: 'LZ4 hızlı sıkıştırma' },
  ]},
  { kategori: 'Matematik & Serileştirme', ekler: [
    { ad: 'GMP', anahtar: 'gmp', aciklama: 'Büyük tam sayı matematiği (WHMCS gerektirir)' },
    { ad: 'BCMath', anahtar: 'bcmath', aciklama: 'Keyfi hassasiyetli matematik' },
    { ad: 'msgpack', anahtar: 'msgpack', aciklama: 'MessagePack serileştirme' },
    { ad: 'YAML', anahtar: 'yaml', aciklama: 'YAML ayrıştırma/üretme' },
  ]},
  { kategori: 'Diğer', ekler: [
    { ad: 'LDAP', anahtar: 'ldap', aciklama: 'LDAP dizin erişimi' },
    { ad: 'UUID', anahtar: 'uuid', aciklama: 'UUID üretimi' },
    { ad: 'Swoole', anahtar: 'swoole', aciklama: 'Yüksek performanslı coroutine sunucu' },
    { ad: 'Data Structures', anahtar: 'ds', aciklama: 'Verimli veri yapıları' },
  ]},
]

// Curated anahtarların düz kümesi — "kurulu ekstra" bölümünü ayıklamak için.
const KATALOG_ANAHTARLAR = new Set(KATALOG.flatMap(k => k.ekler.map(e => e.anahtar)))

// Secim — sihirbazda biriktirilen "kurulacak" eklenti (Özet adımında toplu kurulur).
export type Secim = { surum: string; anahtar: string; ad: string }


const PHPMOD_EN: Record<string, string> = {
  "Adım adım hata ayıklama + profil": "Step-by-step debugging + profiling",
  "Anahtar-değer veritabanı katmanı": "Key-value database layer",
  "Ağ & Servisler": "Network & Services",
  "Brotli sıkıştırma": "Brotli compression",
  "Büyük tam sayı matematiği (WHMCS gerektirir)": "Large integer math (required by WHMCS)",
  "Düşük seviye soket erişimi": "Low-level socket access",
  "GNU gettext çeviri sistemi": "GNU gettext translation system",
  "Gelişmiş görüntü işleme": "Advanced image processing",
  "Geliştirme & Hata Ayıklama": "Development & Debugging",
  "Gömülü SQLite veritabanı": "Embedded SQLite database",
  "Görüntü & Medya": "Image & Media",
  "Görüntü meta verisi okuma": "Read image metadata",
  "Hızlı ikili serileştirme": "Fast binary serialization",
  "IMAP/POP3 e-posta erişimi (sunucuda paket varsa)": "IMAP/POP3 email access (if the package is on the server)",
  "IonCube Loader Yükle": "Install IonCube Loader",
  "IonCube kaldırma başarısız": "IonCube removal failed",
  "IonCube kurulum başarısız": "IonCube installation failed",
  "Kullanıcı verisi in-memory önbelleği": "In-memory cache of user data",
  "Kurulu (katalog dışı)": "Installed (outside catalog)",
  "LDAP dizin erişimi": "LDAP directory access",
  "LZ4 hızlı sıkıştırma": "LZ4 fast compression",
  "Matematik & Serileştirme": "Math & Serialization",
  "Memcached dağıtık önbellek istemcisi": "Memcached distributed cache client",
  "MessagePack serileştirme": "MessagePack serialization",
  "MongoDB NoSQL sürücüsü": "MongoDB NoSQL driver",
  "ODBC üzerinden veritabanı erişimi": "Database access via ODBC",
  "Oracle veritabanı bağlantısı": "Oracle database connection",
  "PHP bytecode önbelleği (genelde kurulu)": "PHP bytecode cache (usually installed)",
  "PostgreSQL bağlantısı (PDO dahil)": "PostgreSQL connection (including PDO)",
  "RabbitMQ / AMQP mesaj kuyruğu": "RabbitMQ / AMQP message queue",
  "Soyut sözdizim ağacı (statik analiz)": "Abstract syntax tree (static analysis)",
  "Sıkıştırma": "Compression",
  "Temel görüntü oluşturma/işleme": "Basic image creation/processing",
  "UUID üretimi": "UUID generation",
  "Uluslararasılaştırma": "Internationalization",
  "Unicode, yerelleştirme, çeviri": "Unicode, localization, translation",
  "Verimli veri yapıları": "Efficient data structures",
  "YAML ayrıştırma/üretme": "YAML parsing/generation",
  "Yüksek performanslı coroutine sunucu": "High-performance coroutine server",
  "ZIP arşiv okuma/yazma": "ZIP archive read/write",
  "bzip2 sıkıştırma": "bzip2 compression",
  "gRPC uzak yordam çağrısı": "gRPC remote procedure call",
  "Çok baytlı dize işleme": "Multi-byte string handling",
  "Önbellek & Performans": "Cache & Performance",
  "⊗ IonCube Kaldır": "⊗ Remove IonCube",
  "✓ IonCube kaldırıldı": "✓ IonCube removed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (PHPMOD_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function PHPModuleriPage({ gomulu, secilenler, setSecilenler }: {
  gomulu?: boolean
  secilenler?: Secim[]
  setSecilenler?: (fn: (s: Secim[]) => Secim[]) => void
} = {}) {
  useTranslation() // dil re-render aboneligi
  const { onay, bilgi } = useDialog()
  const [surumler, setSurumler] = useState<Surum[]>([])
  const [aktifSurum, setAktifSurumState] = useState(() => {
    try { return localStorage.getItem('gosp.phpModul.surum') || '8.3' } catch { return '8.3' }
  })
  const setAktifSurum = (s: string) => {
    setAktifSurumState(s)
    try { localStorage.setItem('gosp.phpModul.surum', s) } catch {}
  }
  const [exts, setExts] = useState<Ext[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [filtre, setFiltre] = useState('')
  // Dinamik katalog: anahtar -> bu sürümde kurulabilir mi (dnf paketi var). Boş/true
  // gösterilir; SADECE kesin false (paket yok) + kurulu değil olan GİZLENİR.
  const [kurulabilir, setKurulabilir] = useState<Record<string, boolean>>({})

  function yukle() {
    setYuk(true); setHata(null)
    api.get(`/php-extensions?surum=${aktifSurum}`)
      .then(r => {
        setExts(r.data.icerik || [])
        const srm = r.data.surumler || []
        setSurumler(srm)
        if (srm.length > 0 && !srm.some((x: Surum) => x.surum === aktifSurum)) {
          setAktifSurum(srm[0].surum)
        }
      })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
    // Bu sürümde hangi katalog bileşenlerinin HAZIR paketi var → desteklenmeyeni gizle.
    api.post('/php-extensions/kurulabilir', { surum: aktifSurum, anahtarlar: [...KATALOG_ANAHTARLAR] })
      .then(r => setKurulabilir(r.data?.kurulabilir || {}))
      .catch(() => setKurulabilir({}))
  }
  useEffect(yukle, [aktifSurum])

  // Bir katalog anahtarı için kurulu extension'ı bul (redis6→redis vb. gevşek eşleşme).
  function kuruluBul(anahtar: string): Ext | undefined {
    const a = anahtar.toLowerCase()
    return exts.find(e => {
      const n = e.adi.toLowerCase()
      return n === a || n.replace(/[0-9]+$/, '') === a || n === 'pdo_' + a || n.includes(a)
    })
  }

  async function toggle(e: Ext) {
    if (ZORUNLU.has(e.adi.toLowerCase())) {
      (await bilgi({ baslik: 'Bilgi', mesaj: 'Bu modül PHP\'nin temel parçasıdır, kapatılamaz.' }))
      return
    }
    const yeniAktif = !e.aktif
    try {
      await api.put('/php-extensions/toggle', { surum: aktifSurum, ini_dosya: e.ini_dosya, aktif: yeniAktif })
      setBasari(`✓ ${e.adi} ${yeniAktif ? 'aktif edildi' : cevir("devre dışı")} · PHP-FPM yeniden başlatıldı`)
      setTimeout(() => setBasari(null), 3000)
      yukle()
    } catch (err) {
      setHata(apiHata(err, cevir("Toggle başarısız")))
    }
  }

  async function ioncubeKur() {
    if (!(await onay({ baslik: 'Onay gerekiyor', mesaj: cevirT(cevir("IonCube Loader PHP {0} için kurulacak. Devam?"), aktifSurum) }))) return
    setYuk(true); setHata(null)
    try {
      const r = await api.post('/php-extensions/ioncube-kur', { surum: aktifSurum })
      setBasari(`✓ IonCube kuruldu — ${r.data.yuklendi ? 'LOADED' : 'ini yazıldı ancak runtime\'da görünmedi'}`)
      setTimeout(() => setBasari(null), 5000)
      yukle()
    } catch (err) {
      setHata(apiHata(err, cevir("IonCube kurulum başarısız"))); setYuk(false)
    }
  }

  async function ioncubeKaldir() {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: `IonCube Loader PHP ${aktifSurum}'ten kaldırılacak. Devam?`, tehlike: true }))) return
    setYuk(true); setHata(null)
    try {
      await api.post('/php-extensions/ioncube-kaldir', { surum: aktifSurum })
      setBasari(cevir("✓ IonCube kaldırıldı")); setTimeout(() => setBasari(null), 3000); yukle()
    } catch (err) {
      setHata(apiHata(err, cevir("IonCube kaldırma başarısız"))); setYuk(false)
    }
  }

  // Kurulmamış eklentiyi "kurulacak" olarak işaretle/kaldır (toggle). Gerçek
  // kurulum Özet adımında TOPLU yapılır (kullanıcı isteği: seç → özette toplu kur).
  const seciliMi = (anahtar: string) =>
    secilenler?.some(s => s.surum === aktifSurum && s.anahtar === anahtar) ?? false
  const secimToggle = (ek: KatalogEk) => {
    setSecilenler?.(prev =>
      seciliMi(ek.anahtar)
        ? prev.filter(s => !(s.surum === aktifSurum && s.anahtar === ek.anahtar))
        : [...prev, { surum: aktifSurum, anahtar: ek.anahtar, ad: ek.ad }],
    )
  }

  const ioncubeKurlu = exts.some(e => e.adi.toLowerCase().includes('ioncube'))
  const f = filtre.toLowerCase().trim()
  // Dinamik süzme: bu sürümde HAZIR paketi olmayan (kurulabilir===false) VE kurulu
  // olmayan bileşen listede gösterilmez (ör. EL10'da IMAP). Kurulu olan her zaman görünür.
  const desteklenir = (e: KatalogEk) => kurulabilir[e.anahtar] !== false || !!kuruluBul(e.anahtar)
  const gizliSayi = KATALOG.flatMap(k => k.ekler).filter(e => !desteklenir(e)).length
  const kategoriler = KATALOG
    .map(k => ({
      ...k,
      ekler: k.ekler
        .filter(desteklenir)
        .filter(e => f ? (e.ad.toLowerCase().includes(f) || e.anahtar.includes(f) || e.aciklama.toLowerCase().includes(f)) : true),
    }))
    .filter(k => k.ekler.length > 0)
  // Katalogda olmayan ama kurulu extension'lar (kullanıcının elle kurdukları).
  const ekstraKurulu = exts.filter(e => {
    const n = e.adi.toLowerCase()
    if (ZORUNLU.has(n) || n.includes('ioncube')) return false
    if ([...KATALOG_ANAHTARLAR].some(a => n === a || n.replace(/[0-9]+$/, '') === a || n.includes(a))) return false
    return f ? n.includes(f) : true
  })

  return (
    <div className={gomulu ? '' : 'px-4 py-4 sm:px-6 sm:py-5'}>
      {!gomulu && (
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: cevir("Sistem Yönetimi") },
          { etiket: cevir("PHP Modülleri") },
        ]} />
      )}

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between mb-1">
        {!gomulu && <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{cevir("PHP Modülleri")}</h1>}
        <button onClick={() => ioncubeKurlu ? ioncubeKaldir() : ioncubeKur()}
          className="px-3 py-2 sm:px-4 text-xs sm:text-sm whitespace-nowrap bg-amber-600 hover:bg-amber-700 text-white rounded-md self-start">
          {ioncubeKurlu ? '⊗ IonCube Kaldır' : <span className="inline-flex items-center gap-1.5"><Ikon d={I.kilit} />{cevir("IonCube Loader Yükle")}</span>}
        </button>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        {cevir(cevir("Hazır listeden PHP eklentisi seçip kurun; kurulu olanları toggle ile aç/kapatın."))} <strong>{cevir("Sunucu bazında")}</strong> — tüm domain'leri etkiler, FPM otomatik yeniden başlatılır.
      </p>

      {/* Sürüm sekmesi */}
      <div className="flex gap-2 mb-4 border-b border-slate-200 dark:border-slate-700 overflow-x-auto [&>*]:flex-shrink-0">
        {surumler.map(s => (
          <button key={s.surum} onClick={() => setAktifSurum(s.surum)}
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition ${
              aktifSurum === s.surum ? 'border-brand-500 text-brand-700 dark:text-brand-300' : 'border-transparent text-slate-500 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300'
            }`}>
            PHP {s.surum}
          </button>
        ))}
      </div>

      <div className="flex items-center justify-between gap-3 mb-4">
        <span className="text-xs text-slate-400 dark:text-slate-500">
          {gizliSayi > 0
            ? cevirT(cevir("Bu PHP sürümünde kurulamayan {0} bileşen gizlendi (sunucuda hazır paketi yok)."), gizliSayi)
            : ''}
        </span>
        <input type="text" value={filtre} onChange={e => setFiltre(e.target.value)} placeholder="🔍 Eklenti ara..."
          className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded text-sm w-64 focus:border-brand-500 outline-none shrink-0" />
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}
      {(secilenler?.length ?? 0) > 0 && (
        <div className="mb-3 px-3 py-2 bg-brand-50 dark:bg-brand-900/20 border border-brand-200 dark:border-brand-800 rounded-md text-sm text-brand-700 dark:text-brand-300">
          {secilenler!.length} eklenti kurulmak üzere seçildi — <strong>{cevir("Özet")}</strong> {cevir(cevir("adımında tümü birlikte kurulur."))}
        </div>
      )}

      {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div> : (
        <div className="space-y-6">
          {kategoriler.map(k => (
            <section key={k.kategori}>
              <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">{k.kategori}</h3>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
                {k.ekler.map(ek => {
                  const kurulu = kuruluBul(ek.anahtar)
                  return (
                    <div key={ek.anahtar}
                      className={`flex items-center justify-between gap-2 px-3 py-2.5 rounded-lg border ${
                        kurulu?.aktif ? 'bg-emerald-50 dark:bg-emerald-900/15 border-emerald-200 dark:border-emerald-800'
                        : kurulu ? 'bg-slate-50 dark:bg-slate-900 border-slate-200 dark:border-slate-700'
                        : seciliMi(ek.anahtar) ? 'bg-brand-50 dark:bg-brand-900/15 border-brand-300 dark:border-brand-700'
                        : 'bg-white dark:bg-slate-800 border-slate-200 dark:border-slate-700'
                      }`}>
                      <div className="min-w-0 flex-1">
                        <div className="text-sm font-medium text-slate-900 dark:text-slate-100 truncate">{ek.ad} <span className="font-mono text-[11px] text-slate-400 dark:text-slate-500">{ek.anahtar}</span></div>
                        <div className="text-[11px] text-slate-500 dark:text-slate-500 truncate">{ek.aciklama}</div>
                      </div>
                      {kurulu ? (
                        <button onClick={() => toggle(kurulu)} title={kurulu.aktif ? 'Devre dışı bırak' : 'Aktif et'}
                          className={`flex-shrink-0 relative inline-flex h-5 w-9 items-center rounded-full transition ${kurulu.aktif ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`}>
                          <span className={`inline-block h-3 w-3 transform rounded-full bg-white shadow transition ${kurulu.aktif ? 'translate-x-5' : 'translate-x-1'}`} />
                        </button>
                      ) : (
                        // Kurulmamış: toggle açınca "kurulacak" işaretlenir (Özet'te toplu kurulur).
                        <button onClick={() => secimToggle(ek)} title={seciliMi(ek.anahtar) ? 'Seçimi kaldır' : 'Kurulacaklara ekle'}
                          className={`flex-shrink-0 relative inline-flex h-5 w-9 items-center rounded-full transition ${seciliMi(ek.anahtar) ? 'bg-brand-500' : 'bg-slate-300 dark:bg-slate-600'}`}>
                          <span className={`inline-block h-3 w-3 transform rounded-full bg-white shadow transition ${seciliMi(ek.anahtar) ? 'translate-x-5' : 'translate-x-1'}`} />
                        </button>
                      )}
                    </div>
                  )
                })}
              </div>
            </section>
          ))}

          {ekstraKurulu.length > 0 && (
            <section>
              <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">{cevir("Kurulu (katalog dışı)")}</h3>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2">
                {ekstraKurulu.map(e => (
                  <div key={e.ini_dosya} className={`flex items-center justify-between gap-2 px-3 py-2.5 rounded-lg border ${e.aktif ? 'bg-emerald-50 dark:bg-emerald-900/15 border-emerald-200 dark:border-emerald-800' : 'bg-slate-50 dark:bg-slate-900 border-slate-200 dark:border-slate-700'}`}>
                    <div className="font-mono text-sm font-medium text-slate-900 dark:text-slate-100 truncate">{e.adi}</div>
                    <button onClick={() => toggle(e)} title={e.aktif ? 'Devre dışı bırak' : 'Aktif et'}
                      className={`flex-shrink-0 relative inline-flex h-5 w-9 items-center rounded-full transition ${e.aktif ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`}>
                      <span className={`inline-block h-3 w-3 transform rounded-full bg-white shadow transition ${e.aktif ? 'translate-x-5' : 'translate-x-1'}`} />
                    </button>
                  </div>
                ))}
              </div>
            </section>
          )}
        </div>
      )}

      {!gomulu && (
        <p className="mt-6 text-xs text-slate-400 dark:text-slate-500">
          {cevir(cevir("Aradığınız eklenti listede yoksa"))} <Link to="/araclar-ayarlar" className="text-brand-600 dark:text-brand-400 underline">{cevir("Araçlar")}</Link> {cevir("ekibiyle iletişime geçin; katalog genişletilebilir.")}
        </p>
      )}
    </div>
  )
}
