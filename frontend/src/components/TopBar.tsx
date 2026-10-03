import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
// gosp-mobil-v1
// gosp-global-arama-v1
import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAuth, adinaBilgi } from '@/store/auth'
import { adinaCik } from '@/lib/adina'
import { musteriDomainID } from '@/lib/rol'
import { MUSTERI_ARACLAR, aracCevir, aracEslesir, aramaSozcukleri, eklentiKapisi } from '@/components/musteri-ana/araclar'
import { eklentileriGetir, type EklentiKayit } from '@/lib/eklenti'
import { api } from '@/lib/api'
import DilSecici from '@/components/DilSecici'
import TemaDugmesi from '@/components/TemaDugmesi'
import BildirimZili from '@/components/BildirimZili'
import { Button } from '@/components/ui'
import { Ikon, I } from '@/components/Ikon'

type AramaDomain = { id: number; alan_adi: string; sistem_kullanici: string }
type AramaSub = { id: number; tam_ad: string; parent_id: number; sistem_kullanici: string }
type Sonuc = { tip: 'sayfa' | 'domain' | 'alt'; ad: string; alt: string; yol: string; key: string }
type Rol = 'admin' | 'reseller' | 'user'

// Panel sayfaları — global arama için statik dizin. `roller` yoksa yalnız admin görür.
type SayfaTanim = { etiket: string; yol: string; anahtar: string[]; roller?: Rol[] }
const HERKES: Rol[] = ['admin', 'reseller', 'user']
const SAYFALAR: SayfaTanim[] = [
  { etiket: 'Domainler', yol: '/domainler', roller: HERKES, anahtar: ['domain', 'hosting', 'hesap', 'site', 'alan adı', 'alan adi'] },
  { etiket: 'Hosting Planları', yol: '/hizmet-planlari', roller: HERKES, anahtar: ['plan', 'paket', 'hosting plan', 'hizmet'] },
  { etiket: 'DNS Şablonu', yol: '/araclar/dns-sablonu', roller: HERKES, anahtar: ['dns', 'şablon', 'sablon', 'template', 'zone'] },
  { etiket: 'Denetim Kaydı', yol: '/denetim', roller: HERKES, anahtar: ['denetim', 'audit', 'log', 'kayıt', 'kayit', 'aktivite', 'işlem geçmişi'] },
  { etiket: 'Profil ve Tercihler', yol: '/profil', roller: HERKES, anahtar: ['profil', 'ayar', 'settings', 'tercih', 'parola', 'şifre', 'sifre', 'hesap', 'tema'] },
  { etiket: 'Bayiler', yol: '/bayiler', anahtar: ['bayi', 'reseller', 'bayiler'] },
  { etiket: 'Bayi Planları', yol: '/bayi-planlari', anahtar: ['bayi plan', 'reseller plan', 'bayi planları'] },
  { etiket: 'Yedek Yönetimi', yol: '/backup-yonetimi', anahtar: ['backup', 'yedek', 'yedekleme', 'restore', 'geri yükleme', 'geri yukleme', 'yedek yönetimi'] },
  { etiket: 'Güvenlik Duvarı', yol: '/firewall', anahtar: ['firewall', 'güvenlik', 'guvenlik', 'security', 'iptables', 'ban', 'duvar', 'kara liste', 'ip engel'] },
  { etiket: 'WordPress', yol: '/wordpress', anahtar: ['wordpress', 'wp', 'toolkit', 'wp toolkit'] },
  { etiket: 'İzleme', yol: '/izleme', anahtar: ['izleme', 'monitoring', 'monitor', 'metrik', 'cpu', 'ram', 'yük', 'load', 'kaynak'] },
  { etiket: 'Site Taşıma', yol: '/araclar/tasima', roller: ['admin', 'reseller'], anahtar: ['taşıma', 'tasima', 'migration', 'transfer', 'göç', 'goc', 'cpanel', 'plesk', 'directadmin', 'aktar', 'import'] },
  { etiket: 'Sunucu Optimize', yol: '/araclar/optimize', anahtar: ['optimize', 'optimizasyon', 'hızlandır', 'hizlandir', 'performans', 'performance', 'temizlik'] },
  { etiket: 'Eklentiler', yol: '/eklentiler', anahtar: ['eklenti', 'plugin', 'addon', 'marketplace', 'eklentiler'] },
  { etiket: 'Araçlar ve Ayarlar', yol: '/araclar-ayarlar', anahtar: ['araç', 'arac', 'ayar', 'tool', 'araçlar', 'ayarlar'] },
  { etiket: 'Paketler', yol: '/araclar/paketler', anahtar: ['paket', 'package', 'rpm', 'dnf', 'yum', 'paketler'] },
  { etiket: 'PHP Sürümleri', yol: '/araclar/php-surumler', anahtar: ['php sürüm', 'php surum', 'php version', 'php sürümleri'] },
  { etiket: 'PHP Modülleri', yol: '/sistem/php-modulleri', anahtar: ['php modül', 'php modul', 'extension', 'php eklenti', 'php modülleri'] },
  { etiket: 'Servisler', yol: '/araclar/servisler', anahtar: ['servis', 'service', 'systemd', 'daemon', 'servisler'] },
  { etiket: 'Panel Güncelleme', yol: '/araclar/guncelleme', anahtar: ['güncelleme', 'guncelleme', 'update', 'panel güncelle', 'sürüm', 'surum'] },
]

// Müşteri (rol 'user') araması: YALNIZ ana sayfasındaki araç kataloğu (TEK KAYNAK:
// components/musteri-ana/araclar). Yukarıdaki HERKES sayfaları (Domainler, Planlar,
// DNS Şablonu, Denetim) müşteriye 403 döndüğü için müşteri listesinde yok; domain/alt
// alan araması da yapılmaz. Eklentiye bağlı araçlar (mail vb.) ana sayfayla AYNI kapıdan
// geçer (araclar.ts eklentiKapisi): arama ilk açıldığında TEK bir GET /eklentiler çekilir;
// liste gelmeden ya da alınamazsa eklenti aracı önerilmez (ölü bağlantı yok). Ana
// sayfanın kendi araması olduğundan orada bu kutu çizilmez.
const MUSTERI_ANA_ANAHTAR = ['ana sayfa', 'anasayfa', 'araçlar', 'genel', 'pano', 'özet', 'home', 'tools', 'dashboard']

const CMP_EN: Record<string, string> = {
  "Domainler": "Domains",
  "Hosting Planları": "Hosting Plans",
  "DNS Şablonu": "DNS Template",
  "Denetim Kaydı": "Audit Log",
  "Profil ve Tercihler": "Profile and Preferences",
  "Bayiler": "Resellers",
  "Bayi Planları": "Reseller Plans",
  "Yedek Yönetimi": "Backup Management",
  "Güvenlik Duvarı": "Firewall",
  "İzleme": "Monitoring",
  "İstatistikler": "Statistics",
  "Site Taşıma": "Site Migration",
  "Sunucu Optimize": "Server Optimize",
  "Eklentiler": "Plugins",
  "Araçlar ve Ayarlar": "Tools and Settings",
  "Paketler": "Packages",
  "PHP Sürümleri": "PHP Versions",
  "PHP Modülleri": "PHP Modules",
  "Servisler": "Services",
  "Panel Güncelleme": "Panel Update",
  "sayfa": "page",
  "domain": "domain",
  "alt alan": "subdomain",
  "Menüyü aç": "Open menu",
  "Ara: domain, alt alan, sayfa (ör. yedek, firewall)…": "Search: domain, subdomain, page (e.g. backup, firewall)…",
  "Ara": "Search",
  "Yükleniyor…": "Loading…",
  "Hesap menüsü": "Account menu",
  "Çıkış Yap": "Log out",
  "Ara: sayfa (ör. yedek, dns, ssl)…": "Search: page (e.g. backup, dns, ssl)…",
  "Hosting kullanıcısı": "Hosting user",
  "Aramayı aç": "Open search",
  "Aramayı kapat": "Close search",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (CMP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

// onMenuAc verilmezse (müşteri: kenar çubuğu yok) hamburger çizilmez; `marka` (müşteri)
// sol başta ana sayfa bağlantısı olarak durur.
export default function TopBar({ onMenuAc, menuAcik, marka }: { onMenuAc?: () => void; menuAcik?: boolean; marka?: ReactNode }) {
  useTranslation() // dil re-render aboneligi
  const kullanici = useAuth((s) => s.kullanici)
  const cikis = useAuth((s) => s.cikis)
  const navigate = useNavigate()
  const konum = useLocation()
  // Rol tek kaynaktan (useAuth kullanici.rol); 'user' = hosting müşterisi.
  const musteri = kullanici?.rol === 'user'
  const musteriID = musteri ? musteriDomainID() : null
  const musteriKok = musteriID !== null ? `/abonelikler/${musteriID}` : null
  // Müşterinin ana sayfasında sayfanın kendi araç araması var: burada ikinci kutu çizilmez.
  const aramaGizli = musteriKok !== null && konum.pathname.replace(/\/+$/, '') === musteriKok
  const [menuAcikProfil, setMenuAcik] = useState(false)

  // ── Global arama ──
  const [q, setQ] = useState('')
  const [acik, setAcik] = useState(false)
  const [aktif, setAktif] = useState(0)
  const veri = useRef<{ domains: AramaDomain[]; subler: AramaSub[] } | null>(null)
  // Değeri okunmaz; yalnız veri.current dolunca yeniden çizim tetikler.
  const [, setYuklendi] = useState(false)
  // Müşteri: eklenti listesi (arama ilk kullanıldığında bir kez; null = henüz yok).
  const [eklentiler, setEklentiler] = useState<EklentiKayit[] | null>(null)
  const eklentiIstendi = useRef(false)
  const kutuRef = useRef<HTMLDivElement>(null)
  // Dar ekran (< sm) + marka (müşteri: hamburger yerine marka bağlantısı): marka ile sağ
  // küme arasında arama kutusuna yazı alanı kalmıyordu (375px'de ~20-40px, pl-9 = 36px).
  // Orada kutu yerine ikon düğmesi çizilir; tıklayınca arama üst çubuğu tam genişlik kaplar.
  const darMod = !!marka
  const [darAcik, setDarAcik] = useState(false)
  const darIkon = !aramaGizli && darMod && !darAcik
  const girdiRef = useRef<HTMLInputElement>(null)
  const darDugmeRef = useRef<HTMLButtonElement>(null)

  // dışarı tıklama → kapat
  useEffect(() => {
    const h = (e: MouseEvent) => {
      if (kutuRef.current && !kutuRef.current.contains(e.target as Node)) { setAcik(false); setDarAcik(false) }
    }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [])

  // Dar arama açılınca girdiye odak. Kapanış: sonuç seçimi (git), Escape, kapat düğmesi
  // ya da kutu dışına tıklama (alt çubuk gezinmesi dahil — yukarıdaki mousedown).
  useEffect(() => { if (darAcik) girdiRef.current?.focus() }, [darAcik])
  // Rota değişince (geri tuşu, kısayol, alt çubuk) dar arama kapanır: yoksa sonraki araç
  // sayfasına üst çubuğu kaplamış hâlde taşınırdı.
  useEffect(() => { setDarAcik(false); setAcik(false) }, [konum.pathname])
  function darKapat() {
    setDarAcik(false); setAcik(false)
    requestAnimationFrame(() => darDugmeRef.current?.focus())
  }

  function veriYukle() {
    // Müşteri domain/alt alan listesi alamaz (/domains, /subdomains → 403); araması
    // yalnız kendi sayfalarıdır — boşuna istek atılmaz. Yalnız eklenti kapısı için
    // müşteriye açık GET /eklentiler bir kez çekilir. Hata (ör. askıdaki hesapta 403):
    // eklenti aracı önerilmez; her tuşta yeniden istek atılmaz (sayfa yenilenince tekrar).
    if (musteri) {
      if (eklentiIstendi.current) return
      eklentiIstendi.current = true
      eklentileriGetir().then(setEklentiler).catch(() => { /* kapı kapalı kalır */ })
      return
    }
    if (veri.current) return
    Promise.all([
      api.get<AramaDomain[]>('/domains').then(r => r.data).catch(() => []),
      api.get<AramaSub[]>('/subdomains').then(r => r.data).catch(() => []),
    ]).then(([domains, subler]) => { veri.current = { domains, subler }; setYuklendi(y => !y) })
  }

  const rol: Rol = (kullanici?.rol as Rol) || 'user'
  const s = q.trim().toLowerCase()
  const sonuclar: Sonuc[] = (() => {
    if (!s) return []
    if (musteri) {
      if (musteriKok === null) return []
      const sozcukler = aramaSozcukleri(q)
      const ana: Sonuc[] = MUSTERI_ANA_ANAHTAR.some(k => k.includes(s))
        ? [{ tip: 'sayfa', ad: aracCevir('Ana sayfa'), alt: musteriKok, yol: musteriKok, key: 'm' + musteriKok }]
        : []
      const kapi = eklentiKapisi(eklentiler)
      const araclar = MUSTERI_ARACLAR
        .filter(a => kapi(a) && aracEslesir(a, sozcukler))
        .map<Sonuc>(a => {
          const yol = `${musteriKok}/${a.yol}`
          return { tip: 'sayfa', ad: aracCevir(a.ad), alt: yol, yol, key: 'm' + yol }
        })
      return [...ana, ...araclar].slice(0, 8)
    }
    // 1) Sayfalar (statik, veri yüklenmese de anında) — rol filtreli
    const p = SAYFALAR
      .filter(x => (x.roller ? x.roller.includes(rol) : rol === 'admin'))
      .filter(x => x.etiket.toLowerCase().includes(s) || x.anahtar.some(k => k.includes(s)))
      .slice(0, 5)
      .map<Sonuc>(x => ({ tip: 'sayfa', ad: cevir(x.etiket), alt: x.yol, yol: x.yol, key: 'p' + x.yol }))
    if (!veri.current) return p
    const d = veri.current.domains
      .filter(x => x.alan_adi.toLowerCase().includes(s) || (x.sistem_kullanici || '').toLowerCase().includes(s))
      .slice(0, 6)
      .map<Sonuc>(x => ({ tip: 'domain', ad: x.alan_adi, alt: x.sistem_kullanici, yol: `/abonelikler/${x.id}`, key: 'd' + x.id }))
    const a = veri.current.subler
      .filter(x => x.tam_ad.toLowerCase().includes(s) || (x.sistem_kullanici || '').toLowerCase().includes(s))
      .slice(0, 6)
      .map<Sonuc>(x => ({ tip: 'alt', ad: x.tam_ad, alt: x.sistem_kullanici, yol: `/abonelikler/${x.parent_id}/subdomainler/${x.id}`, key: 's' + x.id }))
    return [...p, ...d, ...a]
  })()

  function git(yol: string) {
    setAcik(false); setQ(''); setDarAcik(false)
    navigate(yol)
  }

  function tusla(e: React.KeyboardEvent) {
    if (e.key === 'Escape') { if (darAcik) darKapat(); else setAcik(false); return }
    if (e.key === 'ArrowDown') { e.preventDefault(); setAktif(i => Math.min(i + 1, sonuclar.length - 1)); return }
    if (e.key === 'ArrowUp') { e.preventDefault(); setAktif(i => Math.max(i - 1, 0)); return }
    if (e.key === 'Enter') {
      if (sonuclar[aktif]) git(sonuclar[aktif].yol)
      // Yedek (eşleşme yok → domain listesi) yalnız yönetici/bayi: müşteride /domainler yok.
      else if (s && !musteri) navigate('/domainler')
    }
  }

  function onCikis() {
    // Müşteri ADINA oturum: çıkış bayi paneline döner (bayinin kendi oturumu açık kalır).
    if (adinaBilgi()) { adinaCik(); return }
    // Hedef cikis()'ten ÖNCE belirlenir: cikis() müşteri bayraklarını siliyor.
    // Müşteri kendi giriş formuna döner (karma kapıda /giris yönetici formudur).
    const hedef = musteri ? '/cp/giris' : '/giris'
    cikis()
    navigate(hedef, { replace: true })
  }

  return (
    <header className="h-[60px] bg-white/85 dark:bg-dark-800/85 backdrop-blur-md border-b border-slate-200 dark:border-dark-600 flex items-center px-3 sm:px-4 sticky top-0 z-30 gap-2 sm:gap-4">
      {/* Hamburger — yalnız < lg; kenar çubuğu orada çekmeceye dönüşüyor */}
      {onMenuAc && (
        <Button
          isIcon
          variant="flat"
          onClick={onMenuAc}
          className="-ml-1 size-9 rounded-full lg:hidden"
          aria-label={cevir(cevir("Menüyü aç"))}
          title={cevir("Menüyü aç")}
          aria-expanded={!!menuAcik}
          aria-controls="gosp-kenar-cubugu"
        >
          <Ikon d={I.menu} kalinlik={1.8} className="w-5 h-5" />
        </Button>
      )}

      {/* Dar ekranda marka en çok %40 yer kaplar ve gerekirse daha da küçülür (sağ küme
          sabit genişlikte; logo oranını koruyarak küçülür, yazı markası kısaltılır). */}
      {marka && <div className="flex min-w-0 max-w-[40%] items-center sm:max-w-none [&_img]:max-w-full">{marka}</div>}

      {/* Dar ekranda ortalama boşluğu yok; lg'de eski ortalanmış arama korunur */}
      <div className="hidden lg:block flex-1" />

      {/* < sm + marka: arama kutusu yerine ikon; açılınca kutu üst çubuğu kaplar. */}
      {darIkon && (
        <Button
          ref={darDugmeRef}
          isIcon
          variant="flat"
          onClick={() => setDarAcik(true)}
          className="ms-auto size-9 rounded-full sm:hidden"
          aria-label={cevir("Aramayı aç")}
          title={cevir("Aramayı aç")}
          aria-expanded={false}
          aria-controls="gosp-ust-arama"
        >
          <Ikon d={I.ara} kalinlik={1.8} className="w-5 h-5" />
        </Button>
      )}

      {/* lg'de arama en çok xl genişlikte ortalanır ama DARALABİLİR (flex-initial): kenar
          çubuğu açıkken ~1280 px altında flex-none + w-full 576 px'te kalıp sağ kümeyi
          taşırıyor, HER sayfada yatay kaydırma çıkarıyordu (+251 px kök, +183 px bayi). */}
      {!aramaGizli && (
      <div
        id="gosp-ust-arama"
        ref={kutuRef}
        className={`flex-1 lg:flex-initial w-full lg:max-w-xl min-w-0 ${darMod
          ? (darAcik
            ? 'max-sm:absolute max-sm:inset-0 max-sm:z-40 max-sm:flex max-sm:items-center max-sm:gap-2 max-sm:bg-white max-sm:px-3 max-sm:dark:bg-dark-800'
            : 'max-sm:hidden')
          : ''}`}
      >
        <div className="relative min-w-0 flex-1">
          <Ikon d={I.ara} kalinlik={1.8} className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400 dark:text-slate-500 pointer-events-none" />
          <input
            ref={girdiRef}
            type="text"
            value={q}
            onChange={(e) => { setQ(e.target.value); setAktif(0); setAcik(true); veriYukle() }}
            onFocus={() => { veriYukle(); if (q) setAcik(true) }}
            onKeyDown={tusla}
            placeholder={musteri ? cevir("Ara: sayfa (ör. yedek, dns, ssl)…") : cevir(cevir("Ara: domain, alt alan, sayfa (ör. yedek, firewall)…"))}
            aria-label={cevir("Ara")}
            role="combobox"
            aria-expanded={acik && sonuclar.length > 0}
            aria-controls="gosp-arama-sonuc"
            autoComplete="off"
            className="w-full pl-9 pr-3 py-1.5 text-sm bg-slate-100 dark:bg-white/5 border border-slate-200 dark:border-dark-600 rounded-lg focus:bg-white dark:focus:bg-white/10 focus:border-brand-400 focus:ring-2 focus:ring-brand-500/15 outline-none transition"
          />

          {acik && s.length > 0 && (
            <div id="gosp-arama-sonuc" role="listbox"
              className="absolute left-0 right-0 mt-1 bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg shadow-lg z-50 py-1 max-h-80 overflow-auto">
              {/* Müşteri araması yerel katalogdur (yüklenecek bir şey yok): boşsa doğrudan "sonuç yok". */}
              {sonuclar.length === 0 && !veri.current && !musteri && (
                <div className="px-3 py-3 text-sm text-slate-400 dark:text-slate-500">{cevir("Yükleniyor…")}</div>
              )}
              {sonuclar.length === 0 && (veri.current || musteri) && (
                <div className="px-3 py-3 text-sm text-slate-400 dark:text-slate-500">{cevirT("\"{0}\" için sonuç yok", q)}</div>
              )}
              {sonuclar.map((r, i) => (
                <button
                  key={r.key}
                  role="option"
                  aria-selected={i === aktif}
                  onMouseEnter={() => setAktif(i)}
                  onClick={() => git(r.yol)}
                  className={`w-full text-left px-3 py-2 flex items-center gap-2.5 transition ${i === aktif ? 'bg-brand-50 dark:bg-brand-900/20' : 'hover:bg-slate-50 dark:hover:bg-dark-600/50'}`}
                >
                  <span className={`text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold flex-shrink-0 ${r.tip === 'sayfa' ? 'bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300' : r.tip === 'domain' ? 'bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300' : 'bg-teal-100 dark:bg-teal-900/30 text-teal-700 dark:text-teal-300'}`}>
                    {r.tip === 'sayfa' ? cevir('sayfa') : r.tip === 'domain' ? cevir('domain') : cevir('alt alan')}
                  </span>
                  <span className="min-w-0">
                    <span className="block text-sm text-slate-800 dark:text-slate-200 truncate">{r.ad}</span>
                    {r.alt && <span className="block text-[11px] font-mono text-slate-400 dark:text-slate-500 truncate">{r.alt}</span>}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
        {darMod && darAcik && (
          <Button
            isIcon
            variant="flat"
            onClick={darKapat}
            className="size-9 shrink-0 rounded-full sm:hidden"
            aria-label={cevir("Aramayı kapat")}
            title={cevir("Aramayı kapat")}
          >
            <Ikon d={I.kapat} kalinlik={1.8} className="w-5 h-5" />
          </Button>
        )}
      </div>
      )}

      {/* ms-auto yalnız bir öğede: dar ikon çizildiğinde boşluğu o alır (ikon sağ kümeye
          yapışık durur); aksi hâlde (ana sayfa, geniş ekran) sağ küme sağa itilir. */}
      <div className={`${darIkon ? 'max-sm:ms-0' : ''} ms-auto flex-none lg:flex-1 flex items-center justify-end gap-0.5 sm:gap-1`}>
        <DilSecici />
        <TemaDugmesi />
        <BildirimZili />

        <div className="relative">
          <button
            onClick={() => setMenuAcik((v) => !v)}
            className="flex items-center gap-2 px-1.5 sm:px-2 py-1.5 hover:bg-slate-100 dark:hover:bg-white/5 rounded-md transition"
            aria-label={cevir(cevir("Hesap menüsü"))}
          >
            <div className="w-7 h-7 rounded-full bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 font-semibold text-xs flex items-center justify-center flex-shrink-0">
              {(kullanici?.ad_soyad || kullanici?.adi || '?').slice(0, 1).toUpperCase()}
            </div>
            {/* İsim dar ekranda gizli — avatar zaten kimliği taşıyor */}
            <span className="hidden md:inline text-sm font-medium text-slate-700 dark:text-slate-300 max-w-[12rem] truncate">{kullanici?.ad_soyad || kullanici?.adi}</span>
            <Ikon d={I.chevronAsagi} kalinlik={2} className="hidden sm:block w-4 h-4 text-slate-400 dark:text-slate-500 flex-shrink-0" />
          </button>

          {menuAcikProfil && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setMenuAcik(false)} />
              <div className="absolute right-0 mt-1 w-56 max-w-[calc(100vw-1.5rem)] bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg shadow-lg z-50 py-1">
                <div className="px-3 py-2 border-b border-slate-100 dark:border-dark-600">
                  <div className="text-sm font-medium text-slate-900 dark:text-slate-100 truncate">{kullanici?.ad_soyad || kullanici?.adi}</div>
                  <div className="text-xs text-slate-500 dark:text-slate-500 capitalize">{musteri ? cevir("Hosting kullanıcısı") : kullanici?.rol}</div>
                </div>
                <button
                  onClick={() => { setMenuAcik(false); navigate('/profil') }}
                  className="w-full text-left px-3 py-2 text-sm text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-dark-600/50"
                >
                  {cevir("Profil ve Tercihler")}
                </button>
                <div className="border-t border-slate-100 dark:border-dark-600 my-1"></div>
                <button
                  onClick={onCikis}
                  className="w-full text-left px-3 py-2 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30"
                >
                  {cevir(cevir("Çıkış Yap"))}
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </header>
  )
}
