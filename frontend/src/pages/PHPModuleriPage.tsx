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
    { ad: 'IMAP', anahtar: 'imap', aciklama: 'IMAP/POP3 e-posta erişimi' },
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

export default function PHPModuleriPage({ gomulu }: { gomulu?: boolean } = {}) {
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
  const [peclIlerleme, setPeclIlerleme] = useState<{ paket: string; adim: string; yuzde: number; yontem: string } | null>(null)

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
      setBasari(`✓ ${e.adi} ${yeniAktif ? 'aktif edildi' : 'devre dışı'} · PHP-FPM yeniden başlatıldı`)
      setTimeout(() => setBasari(null), 3000)
      yukle()
    } catch (err) {
      setHata(apiHata(err, 'Toggle başarısız'))
    }
  }

  async function ioncubeKur() {
    if (!(await onay({ baslik: 'Onay gerekiyor', mesaj: `IonCube Loader PHP ${aktifSurum} için kurulacak. Devam?` }))) return
    setYuk(true); setHata(null)
    try {
      const r = await api.post('/php-extensions/ioncube-kur', { surum: aktifSurum })
      setBasari(`✓ IonCube kuruldu — ${r.data.yuklendi ? 'LOADED' : 'ini yazıldı ancak runtime\'da görünmedi'}`)
      setTimeout(() => setBasari(null), 5000)
      yukle()
    } catch (err) {
      setHata(apiHata(err, 'IonCube kurulum başarısız')); setYuk(false)
    }
  }

  async function ioncubeKaldir() {
    if (!(await onay({ baslik: 'Emin misiniz?', mesaj: `IonCube Loader PHP ${aktifSurum}'ten kaldırılacak. Devam?`, tehlike: true }))) return
    setYuk(true); setHata(null)
    try {
      await api.post('/php-extensions/ioncube-kaldir', { surum: aktifSurum })
      setBasari('✓ IonCube kaldırıldı'); setTimeout(() => setBasari(null), 3000); yukle()
    } catch (err) {
      setHata(apiHata(err, 'IonCube kaldırma başarısız')); setYuk(false)
    }
  }

  // Katalogtan kurulum — mevcut async PECL-Kur akışı (bundled dnf/pecl/derleme).
  async function kur(ek: KatalogEk) {
    if (peclIlerleme) return
    if (!(await onay({ baslik: 'Eklenti kur', mesaj: `${ek.ad} (${ek.anahtar}) PHP ${aktifSurum} için kurulacak. Hazır paket yoksa kaynaktan derlenir (birkaç dakika sürebilir). Devam?`, onayEtiketi: 'Kur' }))) return
    setHata(null); setBasari(null)
    setPeclIlerleme({ paket: ek.anahtar, adim: 'Başlatılıyor…', yuzde: 2, yontem: '' })
    try {
      const { data } = await api.post('/php-extensions/pecl-install', { surum: aktifSurum, paket: ek.anahtar })
      if (data.is_id) {
        for (;;) {
          await new Promise(r => setTimeout(r, 1500))
          const p = await api.get('/php-extensions/pecl-durum', { params: { id: data.is_id } })
          setPeclIlerleme({ paket: ek.anahtar, adim: p.data.adim || '…', yuzde: p.data.yuzde || 0, yontem: p.data.yontem || '' })
          if (p.data.durum === 'hata') throw new Error(p.data.hata || 'Kurulum başarısız')
          if (p.data.durum === 'tamam') break
        }
      }
      setBasari(`✓ ${ek.ad} kuruldu`)
      yukle()
    } catch (err) {
      setHata(apiHata(err, `${ek.ad} kurulumu başarısız`))
    } finally {
      setPeclIlerleme(null)
    }
  }

  const ioncubeKurlu = exts.some(e => e.adi.toLowerCase().includes('ioncube'))
  const f = filtre.toLowerCase().trim()
  const kategoriler = KATALOG
    .map(k => ({ ...k, ekler: f ? k.ekler.filter(e => e.ad.toLowerCase().includes(f) || e.anahtar.includes(f) || e.aciklama.toLowerCase().includes(f)) : k.ekler }))
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
          { etiket: 'Sistem Yönetimi' },
          { etiket: 'PHP Modülleri' },
        ]} />
      )}

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between mb-1">
        {!gomulu && <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">PHP Modülleri</h1>}
        <button onClick={() => ioncubeKurlu ? ioncubeKaldir() : ioncubeKur()}
          className="px-3 py-2 sm:px-4 text-xs sm:text-sm whitespace-nowrap bg-amber-600 hover:bg-amber-700 text-white rounded-md self-start">
          {ioncubeKurlu ? '⊗ IonCube Kaldır' : <span className="inline-flex items-center gap-1.5"><Ikon d={I.kilit} />IonCube Loader Yükle</span>}
        </button>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        Hazır listeden PHP eklentisi seçip kurun; kurulu olanları toggle ile aç/kapatın. <strong>Sunucu bazında</strong> — tüm domain'leri etkiler, FPM otomatik yeniden başlatılır.
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

      <div className="flex items-center justify-end mb-4">
        <input type="text" value={filtre} onChange={e => setFiltre(e.target.value)} placeholder="🔍 Eklenti ara..."
          className="px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded text-sm w-64 focus:border-brand-500 outline-none" />
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}
      {peclIlerleme && (
        <div className="mb-3 rounded-lg border border-brand-200 dark:border-brand-800 bg-brand-50 dark:bg-brand-950/40 px-4 py-3">
          <div className="flex items-center justify-between mb-1.5">
            <span className="text-sm font-medium text-brand-800 dark:text-brand-200">
              <span className="inline-block w-3.5 h-3.5 mr-2 align-[-2px] rounded-full border-2 border-brand-400 border-t-transparent animate-spin" />
              <span className="font-mono">{peclIlerleme.paket}</span> kuruluyor
              {peclIlerleme.yontem && <span className="ml-1.5 text-[10px] uppercase tracking-wide text-brand-500">({peclIlerleme.yontem === 'dnf' ? 'hazır paket' : 'derleme'})</span>}
            </span>
            <span className="text-xs tabular-nums text-brand-700 dark:text-brand-300">%{peclIlerleme.yuzde}</span>
          </div>
          <div className="text-xs text-brand-700 dark:text-brand-300 mb-2">{peclIlerleme.adim}</div>
          <div className="h-2 rounded-full bg-brand-100 dark:bg-brand-900 overflow-hidden">
            <div className="h-full rounded-full bg-brand-500 transition-all duration-500" style={{ width: `${Math.min(100, peclIlerleme.yuzde)}%` }} />
          </div>
        </div>
      )}

      {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div> : (
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
                        <button onClick={() => kur(ek)} disabled={!!peclIlerleme}
                          className="flex-shrink-0 px-2.5 py-1 text-xs font-medium rounded-md bg-brand-600 hover:bg-brand-700 text-white disabled:opacity-50">Kur</button>
                      )}
                    </div>
                  )
                })}
              </div>
            </section>
          ))}

          {ekstraKurulu.length > 0 && (
            <section>
              <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">Kurulu (katalog dışı)</h3>
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
          Aradığınız eklenti listede yoksa <Link to="/araclar-ayarlar" className="text-brand-600 dark:text-brand-400 underline">Araçlar</Link> ekibiyle iletişime geçin; katalog genişletilebilir.
        </p>
      )}
    </div>
  )
}
