// gosp-dark-swept
// gosp-dark-swept-v2
// gosp-mobil-v1
// gosp-global-arama-v1
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/store/auth'
import { getTheme, setTheme, type Theme } from '@/lib/theme'
import { api } from '@/lib/api'

type Bildirim = { id: number; seviye: string; kategori: string; baslik: string; mesaj: string; domain_id: number | null; okundu: boolean; tarih: string }
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
  { etiket: 'İstatistikler', yol: '/istatistikler', anahtar: ['istatistik', 'statistics', 'trafik', 'kullanım', 'kullanim', 'grafik'] },
  { etiket: 'Site Taşıma', yol: '/araclar/tasima', anahtar: ['taşıma', 'tasima', 'migration', 'transfer', 'göç', 'goc', 'cpanel', 'plesk', 'directadmin', 'aktar', 'import'] },
  { etiket: 'Sunucu Optimize', yol: '/araclar/optimize', anahtar: ['optimize', 'optimizasyon', 'hızlandır', 'hizlandir', 'performans', 'performance', 'temizlik'] },
  { etiket: 'Eklentiler', yol: '/eklentiler', anahtar: ['eklenti', 'plugin', 'addon', 'marketplace', 'eklentiler'] },
  { etiket: 'Araçlar ve Ayarlar', yol: '/araclar-ayarlar', anahtar: ['araç', 'arac', 'ayar', 'tool', 'araçlar', 'ayarlar'] },
  { etiket: 'Paketler', yol: '/araclar/paketler', anahtar: ['paket', 'package', 'rpm', 'dnf', 'yum', 'paketler'] },
  { etiket: 'PHP Sürümleri', yol: '/araclar/php-surumler', anahtar: ['php sürüm', 'php surum', 'php version', 'php sürümleri'] },
  { etiket: 'PHP Modülleri', yol: '/sistem/php-modulleri', anahtar: ['php modül', 'php modul', 'extension', 'php eklenti', 'php modülleri'] },
  { etiket: 'Servisler', yol: '/araclar/servisler', anahtar: ['servis', 'service', 'systemd', 'daemon', 'servisler'] },
  { etiket: 'Panel Güncelleme', yol: '/araclar/guncelleme', anahtar: ['güncelleme', 'guncelleme', 'update', 'panel güncelle', 'sürüm', 'surum'] },
]

export default function TopBar({ onMenuAc, menuAcik }: { onMenuAc?: () => void; menuAcik?: boolean }) {
  const kullanici = useAuth((s) => s.kullanici)
  const cikis = useAuth((s) => s.cikis)
  const navigate = useNavigate()
  async function bildirimYukle() {
    try {
      const { data } = await api.get<{ bildirimler: Bildirim[]; okunmamis: number }>('/bildirimler')
      setBildirimler(data.bildirimler || []); setOkunmamis(data.okunmamis || 0)
    } catch { /* sessiz — badge kritik degil */ }
  }
  useEffect(() => {
    bildirimYukle()
    const t = setInterval(bildirimYukle, 60000)
    return () => clearInterval(t)
  }, [])
  async function bildirimAc() {
    const yeni = !bAcik; setBAcik(yeni)
    if (yeni && okunmamis > 0) {
      try { await api.post('/bildirimler/0/okundu', {}); setOkunmamis(0) } catch { /* sessiz */ }
    }
  }
  function bildirimGit(b: Bildirim) {
    setBAcik(false)
    // Antivirus bildirimleri (DB tarama sunucu-geneli domain_id=0 dahil) merkezi
    // /antivirus paneline gider; domainli AV bulgusu da orada listelenir.
    // Domainli bildirim -> O DOMAININ kendi sayfasina git (AV bulgusunda o
    // domainin Antivirus sayfasi). domain_id yoksa (panel-geneli) merkezi sayfa.
    if (b.domain_id) { navigate(`/abonelikler/${b.domain_id}/imunify`); return }
    if (b.kategori === 'antivirus') { navigate('/antivirus'); return }
  }
  const [menuAcikProfil, setMenuAcik] = useState(false)
  const [tema, setTema] = useState<Theme>(getTheme())

  // ── Global arama ──
  const [q, setQ] = useState('')
  const [acik, setAcik] = useState(false)
  const [aktif, setAktif] = useState(0)
  const veri = useRef<{ domains: AramaDomain[]; subler: AramaSub[] } | null>(null)
  const [yuklendi, setYuklendi] = useState(false)
  const kutuRef = useRef<HTMLDivElement>(null)
  const [bildirimler, setBildirimler] = useState<Bildirim[]>([])
  const [okunmamis, setOkunmamis] = useState(0)
  const [bAcik, setBAcik] = useState(false)
  const bRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const h = (e: Event) => setTema((e as CustomEvent<Theme>).detail)
    window.addEventListener('gosp:theme-change', h)
    return () => window.removeEventListener('gosp:theme-change', h)
  }, [])

  // dışarı tıklama → kapat
  useEffect(() => {
    const h = (e: MouseEvent) => {
      if (kutuRef.current && !kutuRef.current.contains(e.target as Node)) setAcik(false)
      if (bRef.current && !bRef.current.contains(e.target as Node)) setBAcik(false)
    }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [])

  function veriYukle() {
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
    // 1) Sayfalar (statik, veri yüklenmese de anında) — rol filtreli
    const p = SAYFALAR
      .filter(x => (x.roller ? x.roller.includes(rol) : rol === 'admin'))
      .filter(x => x.etiket.toLowerCase().includes(s) || x.anahtar.some(k => k.includes(s)))
      .slice(0, 5)
      .map<Sonuc>(x => ({ tip: 'sayfa', ad: x.etiket, alt: x.yol, yol: x.yol, key: 'p' + x.yol }))
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
    setAcik(false); setQ('')
    navigate(yol)
  }

  function tusla(e: React.KeyboardEvent) {
    if (e.key === 'Escape') { setAcik(false); return }
    if (e.key === 'ArrowDown') { e.preventDefault(); setAktif(i => Math.min(i + 1, sonuclar.length - 1)); return }
    if (e.key === 'ArrowUp') { e.preventDefault(); setAktif(i => Math.max(i - 1, 0)); return }
    if (e.key === 'Enter') {
      if (sonuclar[aktif]) git(sonuclar[aktif].yol)
      else if (s) navigate('/domainler')
    }
  }

  function temaDegistir() {
    const siradaki: Theme = tema === 'light' ? 'dark' : tema === 'dark' ? 'system' : 'light'
    setTheme(siradaki)
    setTema(siradaki)
  }

  function onCikis() {
    cikis()
    navigate('/giris', { replace: true })
  }

  return (
    <header className="h-14 bg-white dark:bg-slate-800 dark:bg-slate-900 border-b border-slate-200 dark:border-slate-700 dark:border-slate-800 flex items-center px-3 sm:px-4 sticky top-0 z-30 gap-2 sm:gap-4">
      {/* Hamburger — yalnız < lg; kenar çubuğu orada çekmeceye dönüşüyor */}
      <button
        onClick={onMenuAc}
        className="lg:hidden -ml-1 p-2 text-slate-500 hover:text-slate-700 dark:text-slate-400 dark:hover:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-md transition flex-shrink-0"
        aria-label="Menüyü aç"
        aria-expanded={!!menuAcik}
        aria-controls="gosp-kenar-cubugu"
      >
        <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
          <path strokeLinecap="round" strokeLinejoin="round" d="M4 6h16M4 12h16M4 18h16" />
        </svg>
      </button>

      {/* Dar ekranda ortalama boşluğu yok; lg'de eski ortalanmış arama korunur */}
      <div className="hidden lg:block flex-1" />

      <div className="flex-1 lg:flex-none w-full lg:max-w-xl min-w-0" ref={kutuRef}>
        <div className="relative">
          <svg className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-slate-400 dark:text-slate-500 pointer-events-none" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            type="text"
            value={q}
            onChange={(e) => { setQ(e.target.value); setAktif(0); setAcik(true); veriYukle() }}
            onFocus={() => { veriYukle(); if (q) setAcik(true) }}
            onKeyDown={tusla}
            placeholder="Ara: domain, alt alan, sayfa (ör. yedek, firewall)…"
            aria-label="Ara"
            role="combobox"
            aria-expanded={acik && sonuclar.length > 0}
            aria-controls="gosp-arama-sonuc"
            autoComplete="off"
            className="w-full pl-9 pr-3 py-1.5 text-sm bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded-lg focus:bg-white dark:bg-slate-800 focus:border-brand-400 focus:ring-2 focus:ring-brand-500/15 outline-none transition"
          />

          {acik && s.length > 0 && (
            <div id="gosp-arama-sonuc" role="listbox"
              className="absolute left-0 right-0 mt-1 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg shadow-lg z-50 py-1 max-h-80 overflow-auto">
              {sonuclar.length === 0 && !veri.current && (
                <div className="px-3 py-3 text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
              )}
              {sonuclar.length === 0 && veri.current && (
                <div className="px-3 py-3 text-sm text-slate-400 dark:text-slate-500">"{q}" için sonuç yok</div>
              )}
              {sonuclar.map((r, i) => (
                <button
                  key={r.key}
                  role="option"
                  aria-selected={i === aktif}
                  onMouseEnter={() => setAktif(i)}
                  onClick={() => git(r.yol)}
                  className={`w-full text-left px-3 py-2 flex items-center gap-2.5 transition ${i === aktif ? 'bg-brand-50 dark:bg-brand-900/20' : 'hover:bg-slate-50 dark:hover:bg-slate-700/50'}`}
                >
                  <span className={`text-[9px] uppercase tracking-wider px-1.5 py-0.5 rounded font-semibold flex-shrink-0 ${r.tip === 'sayfa' ? 'bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300' : r.tip === 'domain' ? 'bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300' : 'bg-teal-100 dark:bg-teal-900/30 text-teal-700 dark:text-teal-300'}`}>
                    {r.tip === 'sayfa' ? 'sayfa' : r.tip === 'domain' ? 'domain' : 'alt alan'}
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
      </div>

      <div className="flex-none lg:flex-1 flex items-center justify-end gap-0.5 sm:gap-1">
        <button onClick={temaDegistir}
          className="p-2 text-slate-500 dark:text-slate-500 hover:text-slate-700 dark:hover:text-slate-300 dark:text-slate-300 hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800 dark:text-slate-400 dark:text-slate-500 dark:hover:text-slate-200 dark:hover:bg-slate-800 rounded-md transition"
          title={`Tema: ${tema} — tıkla değiştir`}>
          {tema === 'dark' ? (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
            </svg>
          ) : tema === 'light' ? (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
            </svg>
          ) : (
            <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
            </svg>
          )}
        </button>
        <div className="relative" ref={bRef}>
        <button onClick={bildirimAc} className="relative inline-flex p-2 text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800 rounded-md transition" title="Bildirimler">
          {okunmamis > 0 && (
            <span className="absolute top-1 right-1 min-w-[16px] h-4 px-1 flex items-center justify-center text-[10px] font-bold text-white bg-red-500 rounded-full">{okunmamis > 99 ? '99+' : okunmamis}</span>
          )}
          <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.8}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
          </svg>
        </button>
          {bAcik && (
            <div className="absolute right-0 mt-2 w-80 max-h-[70vh] overflow-y-auto bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-xl shadow-lg z-50">
              <div className="px-4 py-2.5 border-b border-slate-100 dark:border-slate-700 text-sm font-semibold text-slate-700 dark:text-slate-200">Bildirimler</div>
              {bildirimler.length === 0 ? (
                <div className="px-4 py-6 text-center text-sm text-slate-400 dark:text-slate-500">Bildirim yok</div>
              ) : bildirimler.map(b => (
                <button key={b.id} onClick={() => bildirimGit(b)} className="w-full text-left px-4 py-2.5 border-b border-slate-50 dark:border-slate-700/50 hover:bg-slate-50 dark:hover:bg-slate-700/50 transition flex gap-2.5">
                  <span className={`mt-0.5 w-2 h-2 rounded-full flex-shrink-0 ${b.seviye === 'kritik' ? 'bg-red-500' : b.seviye === 'uyari' ? 'bg-amber-500' : 'bg-slate-400'}`} />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-slate-800 dark:text-slate-100 truncate">{b.baslik}</span>
                    <span className="block text-xs text-slate-500 dark:text-slate-400 line-clamp-2">{b.mesaj}</span>
                    <span className="block text-[11px] text-slate-400 dark:text-slate-500 mt-0.5">{b.tarih}</span>
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>

        <div className="relative">
          <button
            onClick={() => setMenuAcik((v) => !v)}
            className="flex items-center gap-2 px-1.5 sm:px-2 py-1.5 hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800 rounded-md transition"
            aria-label="Hesap menüsü"
          >
            <div className="w-7 h-7 rounded-full bg-brand-100 dark:bg-brand-900/30 text-brand-700 dark:text-brand-300 font-semibold text-xs flex items-center justify-center flex-shrink-0">
              {(kullanici?.ad_soyad || kullanici?.adi || '?').slice(0, 1).toUpperCase()}
            </div>
            {/* İsim dar ekranda gizli — avatar zaten kimliği taşıyor */}
            <span className="hidden md:inline text-sm font-medium text-slate-700 dark:text-slate-300 max-w-[12rem] truncate">{kullanici?.ad_soyad || kullanici?.adi}</span>
            <svg className="hidden sm:block w-4 h-4 text-slate-400 dark:text-slate-500 flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
          </button>

          {menuAcikProfil && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setMenuAcik(false)} />
              <div className="absolute right-0 mt-1 w-56 max-w-[calc(100vw-1.5rem)] bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-lg shadow-lg z-50 py-1">
                <div className="px-3 py-2 border-b border-slate-100 dark:border-slate-800">
                  <div className="text-sm font-medium text-slate-900 dark:text-slate-100 truncate">{kullanici?.ad_soyad || kullanici?.adi}</div>
                  <div className="text-xs text-slate-500 dark:text-slate-500 capitalize">{kullanici?.rol}</div>
                </div>
                <button
                  onClick={() => { setMenuAcik(false); navigate('/profil') }}
                  className="w-full text-left px-3 py-2 text-sm text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800"
                >
                  Profil ve Tercihler
                </button>
                <div className="border-t border-slate-100 dark:border-slate-800 my-1"></div>
                <button
                  onClick={onCikis}
                  className="w-full text-left px-3 py-2 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20"
                >
                  Çıkış Yap
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </header>
  )
}
