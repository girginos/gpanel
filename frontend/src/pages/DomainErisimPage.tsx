import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'

type Kural = { id: number; tip: 'izin' | 'red'; cidr: string; aciklama: string; sira: number }
type Yanit = {
  aktif: boolean
  varsayilan: 'izin' | 'red'
  kurallar: Kural[]
  istemci_ip: string
  vekil_sayisi: number
  tam_ad?: string   // yalnizca alt alan yanitinda
}

const ERISIM_EN: Record<string, string> = {
  "Türkçe": "English",
  "İşlem başarısız": "Operation failed",
  "Erişim Kısıtlama": "Access Restrictions",
  "Alt Alan Adları": "Subdomains",
  "Alt alan adı": "Subdomain",
  "Listedeki {0} kuralı tüm adresleri kapsıyor ({1}). Kara liste modunda bu, siteye HİÇ KİMSENİN erişemeyeceği anlamına gelir.": "Rule {0} on the list covers every address ({1}). In block-list mode this means NOBODY can reach the site.",
  "Alan adı bazlı erişim kısıtlama": "Domain-level access restrictions",
  "Bu siteye hangi IP adreslerinin erişebileceğini belirler. Kapalıyken hiçbir kural uygulanmaz.":
    "Controls which IP addresses can reach this site. While off, no rule is applied.",
  "Kısıtlama": "Restriction",
  "Açık": "On",
  "Kapalı": "Off",
  "Beyaz liste": "Allow list",
  "Kara liste": "Block list",
  "Yalnızca listedeki adresler girebilir; diğer herkes engellenir.":
    "Only listed addresses can enter; everyone else is blocked.",
  "Listedeki adresler engellenir; diğer herkes girebilir.":
    "Listed addresses are blocked; everyone else can enter.",
  "Kural ekle": "Add rule",
  "İzin ver": "Allow",
  "Reddet": "Block",
  "IP veya CIDR": "IP or CIDR",
  "Açıklama (isteğe bağlı)": "Description (optional)",
  "Ekle": "Add",
  "Ekleniyor…": "Adding…",
  "Sil": "Delete",
  "Henüz kural yok.": "No rules yet.",
  "Şu anki IP adresiniz": "Your current IP address",
  "Kendi IP'mi ekle": "Add my IP",
  "Kurallar yukarıdan aşağı değerlendirilir; ilk eşleşen kazanır.":
    "Rules are evaluated top to bottom; the first match wins.",
  "Kaydediliyor…": "Saving…",
  "Kaydet": "Save",
  "Yükleniyor…": "Loading…",
  "Kural silinsin mi?": "Delete this rule?",
  "Sonuç": "Result",
  "Bu ayarla site herkese açık kalır.": "With this setting the site stays open to everyone.",
  "Bu ayarla siteye YALNIZCA {0} adres erişebilir.": "With this setting ONLY {0} address(es) can reach the site.",
  "Bu ayarla {0} adres engellenir, diğer herkes girebilir.": "With this setting {0} address(es) are blocked, everyone else can enter.",
  "Bu ayarla siteye HİÇ KİMSE erişemez.": "With this setting NOBODY can reach the site.",
  "Kendinizi kilitleyeceksiniz": "You are about to lock yourself out",
  "Şu anki IP adresiniz ({0}) izin listesinde yok. Kaydederseniz bu siteye erişiminizi kaybedersiniz.":
    "Your current IP ({0}) is not on the allow list. If you save, you will lose access to this site.",
  "Şu anki IP adresiniz ({0}) reddedilenler arasında. Kaydederseniz bu siteye erişiminizi kaybedersiniz.":
    "Your current IP ({0}) is on the block list. If you save, you will lose access to this site.",
  "CDN arkasındaysanız bu kısıtlama yanlış çalışır": "Behind a CDN this restriction will not work correctly",
  "Sunucuda güvenilir vekil aralığı tanımlı değil. Siteniz Cloudflare gibi bir CDN arkasındaysa nginx ziyaretçinin değil CDN'in IP'sini görür — kurallarınız yanlış adrese uygulanır. Yöneticinizin güvenilir vekil aralıklarını tanımlaması gerekir.":
    "No trusted proxy range is defined on the server. If your site sits behind a CDN such as Cloudflare, nginx sees the CDN's IP rather than the visitor's — your rules would be applied to the wrong address. Your administrator needs to define the trusted proxy ranges.",
  "Let's Encrypt doğrulaması bu kısıtlamadan muaftır; sertifikanız yenilenmeye devam eder.":
    "Let's Encrypt validation is exempt from this restriction; your certificate will keep renewing.",
  "kural": "rule",
  "Değişiklik kaydedildi.": "Changes saved.",
  "Kural eklendi.": "Rule added.",
  "Kural silindi.": "Rule deleted.",
  "açıklama yok": "no description",
  "Yükleme başarısız": "Loading failed",
  "Kural eklenemedi": "Could not add rule",
  "Kural silinemedi": "Could not delete rule",
  "Kaydetme başarısız": "Save failed",
  "Bu kural herkesi engelliyor": "This rule blocks everyone",
  "Bu kural kısıtlamayı etkisiz kılıyor": "This rule disables the restriction",
  "Listedeki {0} kuralı tüm adresleri kapsıyor ({1}). Beyaz liste modunda bu, herkesin girebileceği anlamına gelir — kısıtlama fiilen kapalıdır.": "Rule {0} on the list covers every address ({1}). In allow-list mode this means everyone can enter — the restriction is effectively off.",
  "Kaydedilmemiş değişiklik var — Kaydet'e basmadan uygulanmaz.": "You have unsaved changes — they are not applied until you press Save.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (ERISIM_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// ── Adres kapsama hesabi ────────────────────────────────────────────────────
//
// Bu blok YALNIZCA "kendini kilitliyorsun" uyarisi icindir; yetki karari
// nginx'te verilir. Ama yanlis bir cevap kullaniciya YANLIS GUVENCE verir --
// ve verdigi olculdu. Denetimde ucu de ayni kokten uc kusur cikti: IPv6
// tarayicida hic hesaplanmiyordu.
//
//   kara liste + deny 2a01:4f8:1c::/48        -> uyari CIKMIYORDU (kilitlenirken)
//   2a01:04f8:...:0005 ile 2a01:4f8:1c::5     -> esitlik goremiyordu
//   beyaz listede IPv6 CIDR kapsasa bile      -> uyari SUSMUYORDU (kalici gurultu)
//
// Cozum: iki aileyi de gercekten coz, bit bazinda karsilastir. 'bilinmiyor'
// artik yalniz GERCEKTEN ayristirilamayan girdi icin.

type Kapsama = 'evet' | 'hayir' | 'bilinmiyor'

function ipv4Bayt(s: string): number[] | null {
  const p = s.split('.')
  if (p.length !== 4) return null
  const out: number[] = []
  for (const x of p) {
    if (!/^\d{1,3}$/.test(x)) return null
    const v = Number(x)
    if (v > 255) return null
    out.push(v)
  }
  return out
}

function ipv6Bayt(s: string): number[] | null {
  let metin = s.trim()
  const yuzde = metin.indexOf('%')
  if (yuzde >= 0) metin = metin.slice(0, yuzde)   // zone id
  if (metin === '') return null

  // IPv4 kuyruklu bicim (::ffff:1.2.3.4)
  let kuyruk: number[] | null = null
  const sonIki = metin.lastIndexOf(':')
  if (sonIki >= 0 && metin.slice(sonIki + 1).indexOf('.') >= 0) {
    kuyruk = ipv4Bayt(metin.slice(sonIki + 1))
    if (!kuyruk) return null
    metin = metin.slice(0, sonIki + 1) + '0:0'
  }

  const cift = metin.split('::')
  if (cift.length > 2) return null
  const bol = (t: string) => (t === '' ? [] : t.split(':'))
  const sol = bol(cift[0]), sag = cift.length === 2 ? bol(cift[1]) : []
  const toplam = sol.length + sag.length
  if (cift.length === 1 ? toplam !== 8 : toplam > 7) return null

  const grup: number[] = []
  const ekle = (l: string[]) => {
    for (const g of l) {
      if (!/^[0-9a-fA-F]{1,4}$/.test(g)) return false
      grup.push(parseInt(g, 16))
    }
    return true
  }
  if (!ekle(sol)) return null
  for (let i = 0; i < 8 - toplam; i++) grup.push(0)
  if (!ekle(sag)) return null
  if (grup.length !== 8) return null

  const bayt: number[] = []
  for (const g of grup) { bayt.push((g >> 8) & 0xff); bayt.push(g & 0xff) }
  if (kuyruk) { bayt[12] = kuyruk[0]; bayt[13] = kuyruk[1]; bayt[14] = kuyruk[2]; bayt[15] = kuyruk[3] }
  return bayt
}

function adresCoz(s: string): { aile: 4 | 6; bayt: number[] } | null {
  const t = s.trim()
  if (t === '') return null
  if (t.indexOf(':') >= 0) { const b = ipv6Bayt(t); return b ? { aile: 6, bayt: b } : null }
  const b = ipv4Bayt(t)
  return b ? { aile: 4, bayt: b } : null
}

function kuralCoz(s: string): { aile: 4 | 6; bayt: number[]; bit: number } | null {
  const t = s.trim()
  const egik = t.indexOf('/')
  const a = adresCoz(egik < 0 ? t : t.slice(0, egik))
  if (!a) return null
  const tam = a.aile === 4 ? 32 : 128
  if (egik < 0) return { aile: a.aile, bayt: a.bayt, bit: tam }
  const bitStr = t.slice(egik + 1)
  if (!/^\d{1,3}$/.test(bitStr)) return null   // "/00" da gecerli, nginx kabul ediyor
  const bit = Number(bitStr)
  if (bit > tam) return null
  return { aile: a.aile, bayt: a.bayt, bit }
}

function ilkBitlerEsit(a: number[], b: number[], bit: number): boolean {
  const tamBayt = bit >> 3
  for (let i = 0; i < tamBayt; i++) if (a[i] !== b[i]) return false
  const kalan = bit & 7
  if (kalan === 0) return true
  const maske = (0xff << (8 - kalan)) & 0xff
  return (a[tamBayt] & maske) === (b[tamBayt] & maske)
}

function kapsiyorMu(kural: string, ip: string): Kapsama {
  const k = kuralCoz(kural), c = adresCoz(ip)
  if (!k || !c) return 'bilinmiyor'
  // Farkli aile asla eslesmez -- IPv6 kural IPv4 istemciyi kapsayamaz.
  // (Onceki surum burada 'bilinmiyor' donup sahte alarm uretiyordu.)
  if (k.aile !== c.aile) return 'hayir'
  if (k.bit === 0) return 'evet'
  return ilkBitlerEsit(k.bayt, c.bayt, k.bit) ? 'evet' : 'hayir'
}

// Belirsizlik uyari yonunde; kesinlik uyariyi susturur.
function kapsayabilir(kural: string, ip: string): boolean { return kapsiyorMu(kural, ip) !== 'hayir' }
function kesinKapsiyor(kural: string, ip: string): boolean { return kapsiyorMu(kural, ip) === 'evet' }

// 🔴 "Herkesi kapsiyor mu" testi PREFIX'e bakar, METNE degil.
// Onceki surum `ag === '0.0.0.0' || ag === '::'` diye string esliyordu; sunlar
// siziyordu ve ekran "YALNIZCA 1 adres erisebilir" diyordu: 0.0.0.0/00,
// 0.0.0.0/000, ::/00, 0:0:0:0:0:0:0:0/0, 0000::/0, ::0/0, 1.2.3.4/0.
// Hepsi nginx'te TUM interneti kapsar.
function herkesiKapsar(cidr: string): boolean {
  const k = kuralCoz(cidr)
  return !!k && k.bit === 0
}

// Girdi dogrulamasi (D8): sunucuya gitmeden once yaz-hatasini yakala.
function kuralBicimiGecerli(s: string): boolean {
  const t = s.trim()
  if (t === '' || t.length > 64) return false
  if (/[\s;{}#'"\\]/.test(t)) return false
  return kuralCoz(t) !== null
}

export default function DomainErisimPage() {
  useTranslation()
  // 🔴 AYNI SAYFA iki yerde kullanilir: domain ve alt alan.
  //
  // Iki ayri sayfa yazmak, iki ayri davranis demektir -- ve bu ozellikte
  // ayrisma dogrudan guvenlik farki uretir (kilitlenme uyarisi, /0 tespiti,
  // IPv6 kapsama, kaydedilmemis-degisiklik bandi). Backend'de de tek motor
  // paylasildi; on yuzde de oyle olmali.
  const { id, sid } = useParams()
  const altAlanMi = !!sid
  const temel = altAlanMi
    ? `/domains/${id}/subdomain/${sid}/erisim`
    : `/domains/${id}/erisim`
  const [y, setY] = useState<Yanit | null>(null)
  const [aktif, setAktif] = useState(false)
  const [varsayilan, setVarsayilan] = useState<'izin' | 'red'>('red')
  const [yeniTip, setYeniTip] = useState<'izin' | 'red'>('izin')
  const [yeniCidr, setYeniCidr] = useState('')
  const [yeniAciklama, setYeniAciklama] = useState('')
  const [yuk, setYuk] = useState(true)
  const [isleniyor, setIsleniyor] = useState(false)
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const toast = useToast()

  // 🔴 sadeceKurallar: kural ekleme/silme sonrasi SADECE liste tazelenir.
  //
  // Onceden bu fonksiyon her cagrildiginda sunucudan gelen `aktif` ve
  // `varsayilan` degerlerini forma geri yaziyordu. Sonuc: kullanici anahtari
  // acip "beyaz liste" secip kendi IP'sini ekleyince, ekleme sonrasi tazeleme
  // ONUN HENUZ KAYDEDILMEMIS secimlerini sessizce geri aliyordu. Ekranda
  // "Kural eklendi" yaziyor, anahtar ise kapaniyordu; kullanici Kaydet'e
  // bastiginda aktif=false kaydediliyor ve ozellik hicbir sey yapmadan
  // "kaydedildi" diyordu. Basarisizligin guven olarak render edilmesi.
  // Hangi bağlamın verisi yüklendi? Bayat yanıtı ve bayat veriyi ayıklamak
  // için kullanılır.
  const sonIstek = useRef('')

  function yukle(sadeceKurallar = false) {
    const bu = temel
    sonIstek.current = bu
    setHata(null)
    // 🔴 BAĞLAM DEĞİŞİNCE VERİYİ AT.
    //
    // Ölçüldü: yeni bağlamın isteği 403/404 dönünce `y` önceki bağlamın
    // verisi olarak kalıyordu ve ekran, ÖNCEKİ sitenin kurallarını YENİ
    // sitenin adı ve ekmek kırıntısı altında gösteriyordu — üstelik Kaydet
    // düğmesi etkin kalıyor, yani kullanıcı o bayat ayarı YENİ bağlama
    // yazabiliyordu. Başarılı yüklemede bile ~3 ms'lik bir kare boyunca
    // aynı şey görünüyordu.
    //
    // `basari` da temizlenir: "Kural eklendi." bandı öbür bağlama taşınıyordu.
    if (!sadeceKurallar) {
      setY(null)
      setBasari(null)
      setYuk(true)
    }
    api.get<Yanit>(bu)
      .then(r => {
        // Bayat yanıt koruması: kullanıcı yanıt gelmeden başka bağlama
        // geçtiyse bu veri artık YANLIŞ sayfaya aittir.
        if (sonIstek.current !== bu) return
        setY(r.data)
        if (!sadeceKurallar) { setAktif(r.data.aktif); setVarsayilan(r.data.varsayilan) }
      })
      .catch(e => {
        if (sonIstek.current !== bu) return
        const m = apiHata(e, cevir("Yükleme başarısız")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
      })
      .finally(() => { if (!sadeceKurallar && sonIstek.current === bu) setYuk(false) })
  }
  useEffect(() => { yukle() }, [id, sid])

  async function kaydet() {
    setHata(null); setBasari(null); setIsleniyor(true)
    try {
      await api.put(temel, { aktif, varsayilan })
      setBasari(cevir("Değişiklik kaydedildi.")); toast.basari(cevir("Değişiklik kaydedildi.")); yukle()
    } catch (e) { const m = apiHata(e, cevir("Kaydetme başarısız")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) } finally { setIsleniyor(false) }
  }

  async function kuralEkle(cidr?: string) {
    const deger = (cidr ?? yeniCidr).trim()
    if (!deger) return
    setHata(null); setBasari(null); setIsleniyor(true)
    try {
      await api.post(`${temel}/kural`, { tip: yeniTip, cidr: deger, aciklama: yeniAciklama })
      setYeniCidr(''); setYeniAciklama('')
      setBasari(cevir("Kural eklendi.")); toast.basari(cevir("Kural eklendi.")); yukle(true)
    } catch (e) { const m = apiHata(e, cevir("Kural eklenemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) } finally { setIsleniyor(false) }
  }

  async function kuralSil(kid: number) {
    if (!window.confirm(cevir("Kural silinsin mi?"))) return
    setHata(null); setBasari(null); setIsleniyor(true)
    try {
      await api.delete(`${temel}/kural/${kid}`)
      setBasari(cevir("Kural silindi.")); toast.basari(cevir("Kural silindi.")); yukle(true)
    } catch (e) { const m = apiHata(e, cevir("Kural silinemedi")); setHata(m); toast.hata(cevir("İşlem başarısız"), m) } finally { setIsleniyor(false) }
  }

  const kurallar = y?.kurallar ?? []
  const izinliler = kurallar.filter(k => k.tip === 'izin')
  const redliler = kurallar.filter(k => k.tip === 'red')
  const benimIP = y?.istemci_ip ?? ''

  // Kendini kilitleme: beyaz listede kendi IP'si yoksa, kara listede varsa.
  // Belirsizlik HER IKI modda da UYARI yonunde degerlendirilir:
  //  - beyaz listede: yalnizca KESIN kapsama uyariyi susturur
  //  - kara listede : OLASI kapsama bile uyariyi tetikler
  const kilitRiski = aktif && benimIP !== '' && (
    varsayilan === 'red'
      ? !izinliler.some(k => kesinKapsiyor(k.cidr, benimIP))
      : redliler.some(k => kapsayabilir(k.cidr, benimIP))
  )

  // 🔴 "/0" prefix'i TUM adres uzayini kapsar. Beyaz liste modunda tek bir
  // `allow 0.0.0.0/0` kurali, altindaki `deny all`'i ULASILMAZ kilar: panel
  // "kisitlama aktif" gosterirken hic kimse engellenmez. Gecerli bir kural
  // oldugu icin API reddetmez (ve reddetmemeli -- kullanici bunu bilerek de
  // isteyebilir), ama SESSIZ kalmamali.
  const hepsiniKapsayan = izinliler.find(k => herkesiKapsar(k.cidr))
  const etkisiz = aktif && varsayilan === 'red' && !!hepsiniKapsayan
  // 🔴 KARA LISTEDE AYNA DURUM: `deny 0.0.0.0/0` HERKESI engeller, ama sonuc
  // cumlesi "1 adres engellendi, diger herkes girebilir" diyordu -- gercegin
  // TAM TERSI. Uyari kutusu ciksa da, "kaydetmeden once ne olacagini oku"
  // diye var olan cumle yalan soyluyordu.
  const hepsiniEngelleyen = redliler.find(k => herkesiKapsar(k.cidr))
  const tamKapali = aktif && varsayilan === 'izin' && !!hepsiniEngelleyen

  // Sonuc cumlesi — kullanici kaydetmeden ONCE ne olacagini okuyabilsin.
  let sonuc: string
  if (!aktif) sonuc = cevir("Bu ayarla site herkese açık kalır.")
  else if (etkisiz) sonuc = cevir("Bu ayarla site herkese açık kalır.")
  else if (tamKapali) sonuc = cevir("Bu ayarla siteye HİÇ KİMSE erişemez.")
  else if (varsayilan === 'red') {
    sonuc = izinliler.length === 0
      ? cevir("Bu ayarla siteye HİÇ KİMSE erişemez.")
      : cevirT(cevir("Bu ayarla siteye YALNIZCA {0} adres erişebilir."), String(izinliler.length))
  } else {
    sonuc = redliler.length === 0
      ? cevir("Bu ayarla site herkese açık kalır.")
      : cevirT(cevir("Bu ayarla {0} adres engellenir, diğer herkes girebilir."), String(redliler.length))
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[1100px]">
      <Breadcrumb items={[
        { etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: String(id), href: `/abonelikler/${id}` },
        ...(altAlanMi
          ? [{ etiket: cevir("Alt Alan Adları"), href: `/abonelikler/${id}/subdomainler` },
             { etiket: y?.tam_ad || String(sid), href: `/abonelikler/${id}/subdomainler/${sid}` }]
          : []),
        { etiket: cevir("Erişim Kısıtlama") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("Erişim Kısıtlama")}</h1>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
        <Link to={altAlanMi ? `/abonelikler/${id}/subdomainler/${sid}` : `/abonelikler/${id}`}
          className="text-brand-700 dark:text-brand-400 hover:text-brand-800 font-medium">
          {altAlanMi ? (y?.tam_ad || cevir("Alt alan adı")) : cevir("Alan adı bazlı erişim kısıtlama")}
        </Link>
        {' · '}{cevir("Bu siteye hangi IP adreslerinin erişebileceğini belirler. Kapalıyken hiçbir kural uygulanmaz.")}
      </p>

      {yuk || !y ? (
        <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
      ) : (
        <>
          {/* ── Ana toggle ─────────────────────────────────────────────── */}
          <div className="mb-4 bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5">
            <div className="flex flex-wrap items-center gap-4">
              <span className={`inline-flex items-center justify-center h-10 w-10 rounded-lg ${aktif ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-400' : 'bg-slate-100 dark:bg-dark-600 text-slate-400'}`}>
                <Ikon d={aktif ? I.kilit : I.kilitAcik} className="h-5 w-5" />
              </span>
              <div className="min-w-0">
                <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Kısıtlama")}</div>
                <div className="text-xs text-slate-500 dark:text-slate-400">{sonuc}</div>
              </div>

              {/* Anahtar: gercek buton + aria-pressed (ekran okuyucu icin durum) */}
              <button
                type="button" role="switch" aria-checked={aktif}
                aria-label={cevir("Kısıtlama")}
                onClick={() => setAktif(!aktif)}
                className={`ml-auto relative inline-flex h-11 w-[76px] shrink-0 items-center rounded-full transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-500 ${
                  aktif ? 'bg-emerald-500' : 'bg-slate-300 dark:bg-slate-600'}`}>
                <span className={`inline-block h-9 w-9 transform rounded-full bg-white shadow transition-transform ${aktif ? 'translate-x-[35px]' : 'translate-x-1'}`} />
              </button>
              <span className={`text-sm font-semibold w-[52px] ${aktif ? 'text-emerald-700 dark:text-emerald-400' : 'text-slate-500 dark:text-slate-400'}`}>
                {aktif ? cevir("Açık") : cevir("Kapalı")}
              </span>
            </div>
          </div>

          {aktif && (
            <>
              {/* ── Kilitlenme uyarisi ─────────────────────────────────── */}
              {kilitRiski && (
                <div className="mb-4 px-4 py-3 bg-red-50 dark:bg-red-900/20 border-2 border-red-300 dark:border-red-700 rounded-lg">
                  <div className="flex items-start gap-2.5">
                    <span className="text-red-600 dark:text-red-400 mt-0.5"><Ikon d={I.uyari} className="h-5 w-5" /></span>
                    <div>
                      <div className="text-sm font-bold text-red-800 dark:text-red-200">{cevir("Kendinizi kilitleyeceksiniz")}</div>
                      <div className="text-xs text-red-700 dark:text-red-300 mt-1">
                        {cevirT(cevir(varsayilan === 'red'
                          ? "Şu anki IP adresiniz ({0}) izin listesinde yok. Kaydederseniz bu siteye erişiminizi kaybedersiniz."
                          : "Şu anki IP adresiniz ({0}) reddedilenler arasında. Kaydederseniz bu siteye erişiminizi kaybedersiniz."), benimIP)}
                      </div>
                      {varsayilan === 'red' && (
                        <button type="button" onClick={() => { setYeniTip('izin'); kuralEkle(benimIP) }}
                          className="mt-2 inline-flex items-center gap-1.5 min-h-[44px] px-3 rounded-lg bg-red-600 hover:bg-red-700 text-white text-xs font-semibold">
                          <Ikon d={I.arti} className="h-4 w-4" />{cevir("Kendi IP'mi ekle")}
                        </button>
                      )}
                    </div>
                  </div>
                </div>
              )}

              {/* ── "/0" etkisizlik uyarisi ─────────────────────────────── */}
              {(etkisiz || tamKapali) && (etkisiz ? hepsiniKapsayan : hepsiniEngelleyen) && (
                <div className="mb-4 px-4 py-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-300 dark:border-amber-700 rounded-lg">
                  <div className="flex items-start gap-2.5">
                    <span className="text-amber-600 dark:text-amber-400 mt-0.5"><Ikon d={I.uyari} className="h-5 w-5" /></span>
                    <div>
                      <div className="text-sm font-semibold text-amber-800 dark:text-amber-200">{etkisiz ? cevir("Bu kural kısıtlamayı etkisiz kılıyor") : cevir("Bu kural herkesi engelliyor")}</div>
                      <div className="text-xs text-amber-700 dark:text-amber-300 mt-1">
{/* 🔴 Gövde MODA GÖRE. Önceki sürümde başlık moda göre değişiyor ama
                          gövde sabit beyaz-liste metni kalıyordu: kara listede
                          "Bu kural herkesi engelliyor" başlığının altında
                          "kısıtlama fiilen kapalıdır" yazıyordu — aynı kutuda
                          birbirinin tersi iki cümle, üstelik herkesi kilitleyen
                          bir ayarı "kapalı" diye güvenceye çeviriyordu. */}
                        {cevirT(cevir(etkisiz
                          ? "Listedeki {0} kuralı tüm adresleri kapsıyor ({1}). Beyaz liste modunda bu, herkesin girebileceği anlamına gelir — kısıtlama fiilen kapalıdır."
                          : "Listedeki {0} kuralı tüm adresleri kapsıyor ({1}). Kara liste modunda bu, siteye HİÇ KİMSENİN erişemeyeceği anlamına gelir."),
                          (etkisiz ? hepsiniKapsayan! : hepsiniEngelleyen!).cidr,
                          (etkisiz ? hepsiniKapsayan! : hepsiniEngelleyen!).aciklama || cevir("açıklama yok"))}
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* ── CDN uyarisi ────────────────────────────────────────── */}
              {y.vekil_sayisi === 0 && (
                <div className="mb-4 px-4 py-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg">
                  <div className="flex items-start gap-2.5">
                    <span className="text-amber-600 dark:text-amber-400 mt-0.5"><Ikon d={I.bulut} className="h-5 w-5" /></span>
                    <div>
                      <div className="text-sm font-semibold text-amber-800 dark:text-amber-200">{cevir("CDN arkasındaysanız bu kısıtlama yanlış çalışır")}</div>
                      <div className="text-xs text-amber-700 dark:text-amber-300 mt-1">
                        {cevir("Sunucuda güvenilir vekil aralığı tanımlı değil. Siteniz Cloudflare gibi bir CDN arkasındaysa nginx ziyaretçinin değil CDN'in IP'sini görür — kurallarınız yanlış adrese uygulanır. Yöneticinizin güvenilir vekil aralıklarını tanımlaması gerekir.")}
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* ── Mod secici ─────────────────────────────────────────── */}
              <Kart baslik={cevir("Kısıtlama")}>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  {([
                    { key: 'red' as const, ad: cevir("Beyaz liste"), ikon: I.kalkan,
                      aciklama: cevir("Yalnızca listedeki adresler girebilir; diğer herkes engellenir.") },
                    { key: 'izin' as const, ad: cevir("Kara liste"), ikon: I.ban,
                      aciklama: cevir("Listedeki adresler engellenir; diğer herkes girebilir.") },
                  ]).map(m => {
                    const sec = varsayilan === m.key
                    return (
                      <button key={m.key} type="button" onClick={() => setVarsayilan(m.key)}
                        className={`text-left p-4 border rounded-lg transition min-h-[44px] ${sec
                          ? 'border-brand-500 bg-brand-50 dark:bg-brand-900/20 ring-2 ring-brand-500/20'
                          : 'border-slate-200 dark:border-dark-600 hover:border-brand-300'}`}>
                        <div className="flex items-center gap-2 mb-1">
                          <Ikon d={m.ikon} className="h-4 w-4 text-slate-600 dark:text-slate-300" />
                          <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">{m.ad}</span>
                          {sec && <span className="ml-auto text-slate-500 dark:text-slate-400"><Ikon d={I.onay} className="h-4 w-4" /></span>}
                        </div>
                        <div className="text-[11px] text-slate-600 dark:text-slate-400 leading-snug">{m.aciklama}</div>
                      </button>
                    )
                  })}
                </div>
              </Kart>

              {/* ── Kural listesi ──────────────────────────────────────── */}
              <Kart baslik={cevir("Kural ekle")}>
                <div className="flex flex-wrap gap-2 items-start">
                  <select value={yeniTip} onChange={e => setYeniTip(e.target.value as 'izin' | 'red')}
                    className="min-h-[44px] px-3 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 text-sm text-slate-900 dark:text-slate-100">
                    <option value="izin">{cevir("İzin ver")}</option>
                    <option value="red">{cevir("Reddet")}</option>
                  </select>
                  <input value={yeniCidr} onChange={e => setYeniCidr(e.target.value)}
                    placeholder="203.0.113.45 / 198.51.100.0/24"
                    aria-invalid={yeniCidr.trim() !== '' && !kuralBicimiGecerli(yeniCidr)}
                    aria-label={cevir("IP veya CIDR")}
                    className={`min-h-[44px] px-3 rounded-lg border bg-white dark:bg-dark-800 text-sm font-mono text-slate-900 dark:text-slate-100 flex-1 min-w-[190px] ${
                      yeniCidr.trim() !== '' && !kuralBicimiGecerli(yeniCidr)
                        ? 'border-red-500 dark:border-red-500'
                        : 'border-slate-300 dark:border-slate-600'}`} />
                  <input value={yeniAciklama} onChange={e => setYeniAciklama(e.target.value)}
                    placeholder={cevir("Açıklama (isteğe bağlı)")}
                    className="min-h-[44px] px-3 rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-dark-800 text-sm text-slate-900 dark:text-slate-100 flex-1 min-w-[160px]" />
                  <button type="button"
                    disabled={isleniyor || !kuralBicimiGecerli(yeniCidr)}
                    onClick={() => kuralEkle()}
                    className="inline-flex items-center gap-1.5 min-h-[44px] px-4 rounded-lg bg-brand-600 hover:bg-brand-700 disabled:opacity-50 text-white text-sm font-semibold">
                    <Ikon d={I.arti} className="h-4 w-4" />{isleniyor ? cevir("Ekleniyor…") : cevir("Ekle")}
                  </button>
                </div>
                {benimIP && (
                  <div className="mt-3 flex flex-wrap items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
                    <Ikon d={I.kure} className="h-4 w-4" />
                    {cevir("Şu anki IP adresiniz")}: <code className="font-mono text-slate-800 dark:text-slate-200">{benimIP}</code>
                    <button type="button" onClick={() => { setYeniTip('izin'); setYeniCidr(benimIP) }}
                      className="inline-flex items-center gap-1 min-h-[44px] px-2.5 rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-dark-600 font-medium">
                      <Ikon d={I.arti} className="h-3.5 w-3.5" />{cevir("Kendi IP'mi ekle")}
                    </button>
                  </div>
                )}

                <p className="mt-3 text-[11px] text-slate-500 dark:text-slate-400">{cevir("Kurallar yukarıdan aşağı değerlendirilir; ilk eşleşen kazanır.")}</p>

                <div className="mt-4 overflow-x-auto">
                  {kurallar.length === 0 ? (
                    <div className="py-8 text-center text-sm text-slate-500 dark:text-slate-400">{cevir("Henüz kural yok.")}</div>
                  ) : (
                    <table className="w-full text-sm tabular-nums">
                      <tbody>
                        {kurallar.map(k => (
                          <tr key={k.id} className="border-b border-slate-100 dark:border-dark-600 last:border-0">
                            <td className="py-2 pr-3 w-[92px]">
                              <span className={`inline-flex items-center gap-1 px-2 py-1 rounded-md text-[11px] font-semibold ${
                                k.tip === 'izin'
                                  ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300'
                                  : 'bg-rose-100 dark:bg-rose-900/30 text-rose-700 dark:text-rose-300'}`}>
                                <Ikon d={k.tip === 'izin' ? I.onay : I.ban} className="h-3.5 w-3.5" />
                                {k.tip === 'izin' ? cevir("İzin ver") : cevir("Reddet")}
                              </span>
                            </td>
                            <td className="py-2 pr-3 font-mono text-slate-900 dark:text-slate-100 whitespace-nowrap">{k.cidr}</td>
                            <td className="py-2 pr-3 text-slate-500 dark:text-slate-400">{k.aciklama}</td>
                            <td className="py-2 text-right">
                              <button type="button" onClick={() => kuralSil(k.id)} disabled={isleniyor}
                                aria-label={cevir("Sil")}
                                className="inline-flex items-center justify-center h-11 w-11 rounded-lg text-slate-400 hover:text-rose-600 hover:bg-rose-50 dark:hover:bg-rose-900/20 disabled:opacity-50">
                                <Ikon d={I.cop} className="h-4 w-4" />
                              </button>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  )}
                </div>
              </Kart>

              <p className="mb-4 px-3 py-2 bg-slate-50 dark:bg-dark-700/60 border border-slate-200 dark:border-dark-600 rounded-lg text-xs text-slate-600 dark:text-slate-400 flex items-start gap-2">
                <span className="mt-0.5 text-slate-400"><Ikon d={I.bilgi} className="h-4 w-4" /></span>
                {cevir("Let's Encrypt doğrulaması bu kısıtlamadan muaftır; sertifikanız yenilenmeye devam eder.")}
              </p>
            </>
          )}

          {y && (aktif !== y.aktif || varsayilan !== y.varsayilan) && (
            <div className="mb-3 px-3 py-2 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-xs text-amber-800 dark:text-amber-200 flex items-center gap-2">
              <Ikon d={I.bilgi} className="h-4 w-4" />
              {cevir("Kaydedilmemiş değişiklik var — Kaydet'e basmadan uygulanmaz.")}
            </div>
          )}

          <div className="flex gap-2">
            <button type="button" onClick={kaydet} disabled={isleniyor}
              className="inline-flex items-center gap-2 min-h-[44px] px-5 rounded-lg bg-brand-600 hover:bg-brand-700 disabled:opacity-50 text-white text-sm font-semibold">
              <Ikon d={I.disket} className="h-4 w-4" />{isleniyor ? cevir("Kaydediliyor…") : cevir("Kaydet")}
            </button>
            <button type="button" onClick={() => yukle()} disabled={isleniyor}
              className="inline-flex items-center gap-2 min-h-[44px] px-4 rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-dark-600 text-sm font-medium text-slate-700 dark:text-slate-200">
              <Ikon d={I.yenile} className="h-4 w-4" />{cevir("Yeniden Yükle")}
            </button>
          </div>
        </>
      )}
    </div>
  )
}

function Kart({ baslik, children }: { baslik: string; children: any }) {
  return (
    <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-5 mb-4">
      <h3 className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-3 pb-2 border-b border-slate-100 dark:border-dark-600">{baslik}</h3>
      {children}
    </div>
  )
}
