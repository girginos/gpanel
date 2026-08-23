import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'

type Tarama = { id: number; alan_adi: string; durum: string; kaynak: string; taranan: number; enfekte: number; bitis: string }
type Durum = {
  ajan_kurulu: boolean; izleyici_aktif: boolean; slice_aktif: boolean
  kural_surum: number; kural_uretim: string; kural_sayisi: number
  toplam_karantina: number; toplam_bulgu: number; taranan_domain: number
  son_taramalar: Tarama[]
}
type Kar = { id: number; alan_adi: string; domain_id: number; orijinal_yol: string; imza: string; seviye: string; puan: number; durum: string; tarih: string; mevcut: boolean }
type Gecmis = { id: number; alan_adi: string; domain_id: number; dosya: string; imza: string; motor: string; seviye: string; puan: number; durum: string; tarih: string }
type Kara = { domain_id: number; alan_adi: string; durum: string; kaynak: string }
type Domain = { id: number; alan_adi: string; sistem_kullanici: string; son_tarama: string; son_taranan: number; son_enfekte: number; aktif_bulgu: number; karantina: number }
type Ayar = {
  gercek_zamanli: boolean; zamanli_tarama: boolean; wp_butunluk: boolean; kural_motoru: boolean
  konum_sezgileri: boolean; oto_karantina: boolean; surec_izleme: boolean; esik_kritik: number; kapsam: string
  haric_yollar: string; cpu_yuzde: number; ram_mb: number; io_agirlik: number
  is_parcacigi: number; dosya_hiz_sn: number; zamanli_saat: string; yuk_esigi: number
}
type AyarYanit = { ayarlar: Ayar; kapasite: { cpu_cekirdek: number; ram_toplam_mb: number; oneri_cpu_yuzde: number; oneri_ram_mb: number; oneri_is_parcacigi: number } }

type Sekme = 'genel' | 'domainler' | 'karantina' | 'gecmis' | 'itibar' | 'ayarlar'


const AVPANEL_EN: Record<string, string> = {
  "Türkçe": "English",
  "Geri yükleme": "Restore",
  "Kalıcı silme": "Permanent delete",
  "Antivirüs": "Antivirus",
  "İtibar": "Reputation",
  "Taranıyor…": "Scanning…",
  "Tümü": "All",
  "Karantinalı": "Quarantined",
  "Hazır": "Ready",
  "Kapalı": "Off",
  "Genel Bakış": "Overview",
  "Domainler": "Domains",
  "Yükleniyor…": "Loading…",
  "Yükle": "Load",
  "Ayarlar kaydedilemedi": "Failed to save settings",
  "Veritabanı taraması: {0} WP kurulumu, {1} zararlı kayıt{2}.": "Database scan: {0} WP installs, {1} malicious records{2}.",
  ", {0} bağlanılamadı": ", {0} could not connect",
  "Dosya orijinal konumuna geri yüklensin mi?\n{0}\n({1})": "Restore the file to its original location?\n{0}\n({1})",
  "Karantinadaki dosya KALICI silinsin mi?\n{0}\n({1})\n\nGeri alınamaz.": "Permanently delete the quarantined file?\n{0}\n({1})\n\nCannot be undone.",
  "[ikili dosya]": "[binary file]",
  "Tehdit var": "Threat present",
  "Korunuyor": "Protected",
  "Karantina": "Quarantine",
  "Bulgular": "Findings",
  "Ayarlar": "Settings",
  "Kendi motorumuz": "Our own engine",
  "sürüm": "version",
  "kural": "rules",
  "imzalı": "signed",
  "Başlatılıyor…": "Starting…",
  "Karantinada": "In quarantine",
  "Toplam Bulgu": "Total Findings",
  "Taranan Domain": "Scanned Domains",
  "Domain-bazlı tarama": "Domain-based scan",
  "Enfekte": "Infected",
  "Temiz": "Clean",
  "{0} domain seçili": "{0} domains selected",
  " ({0} filtre dışı)": " ({0} outside filter)",
  "Seçilenleri Tara ({0})": "Scan Selected ({0})",
  "Domain yok.": "No domains.",
  "Son tarama": "Last scan",
  "Aktif bulgu": "Active findings",
  "{0} seç": "Select {0}",
  "dosya": "files",
  "Tara": "Scan",
  "Kurulu": "Installed",
  "Yok": "None",
  "Aktif": "Active",
  "Kaynak Dilimi": "Resource Slice",
  "{0} kural · RapidScan": "{0} rules · RapidScan",
  "fanotify izleme": "fanotify monitoring",
  "Son taramalar": "Recent scans",
  "Kaynak": "Source",
  "Taranan": "Scanned",
  "— (sunucu)": "— (server)",
  "Dosya": "File",
  "Tespit": "Detection",
  "Geri yüklendi": "Restored",
  "Silindi": "Deleted",
  "İncele": "Inspect",
  "Geri yükle": "Restore",
  "Sil": "Delete",
  "Olay günlüğü": "Event log",
  "({0} kayıt)": "({0} records)",
  "Kayıt yok.": "No records.",
  "Kara listede": "Blacklisted",
  "kontrol edilemedi": "could not check",
  "Kontrol ediliyor…": "Checking…",
  "Yenile": "Refresh",
  "Kaydediliyor…": "Saving…",
  "Kaydet": "Save",
  "Koruma": "Protection",
  "Saat": "Time",
  "Otomatik karantina": "Automatic quarantine",
  "Şüpheli süreç zincirlerini yakalar (php-fpm→kabuk, indir-çalıştır, webroot binary). Açınca izleme için root + CAP_SYS_PTRACE'li bir servis başlar (yalnız kimlik doğrulama; ptrace çağrısı engelli). Bildirim modu — süreç öldürmez.": "Catches suspicious process chains (php-fpm→shell, download-run, webroot binary). When enabled, a service with root + CAP_SYS_PTRACE starts for monitoring (authentication only; ptrace calls are blocked). Notification mode — does not kill processes.",
  "Konum sezgileri": "Location heuristics",
  "Kapsam": "Scope",
  "Dengeli": "Balanced",
  "% çekirdek · 0 = kapalı. Sistem 1-dk yükü bu değeri (ör. 80 = ×0.8 çekirdek) aşarsa tarama kendini duraklatır.": "% core · 0 = off. If the system's 1-min load exceeds this value (e.g. 80 = ×0.8 core), the scan pauses itself.",
  "otomatik": "automatic",
  "çekirdek": "cores",
  "0 = otomatik. Sunucu:": "0 = automatic. Server:",
  ". Örn. 2 = iki tam çekirdek.": ". e.g. 2 = two full cores.",
  "RAM limiti (GB)": "RAM limit (GB)",
  "0 = otomatik. Toplam:": "0 = automatic. Total:",
  ". Aşılırsa tarayıcı OOM ile durur (site etkilenmez).": ". If exceeded, the scanner stops with OOM (site not affected).",
  "Ayarlar kaydedildi ve uygulandı.": "Settings saved and applied.",
  "CPU limiti (çekirdek)": "CPU limit (cores)",
  "DB taraması başarısız": "DB scan failed",
  "Dinamik yük eşiği": "Dynamic load threshold",
  "Domain / kullanıcı ara…": "Search domain / user…",
  "Domain itibarı — Spamhaus DBL": "Domain reputation — Spamhaus DBL",
  "Dosya Tarayıcı": "File Scanner",
  "Dosya hız/sn (0=sınırsız)": "File rate/s (0=unlimited)",
  "Dosya taraması başlatıldı — sonuçlar birkaç dakikada listeye düşer.": "File scan started — results appear in the list within a few minutes.",
  "Düşük (sunucuyu az yorar)": "Low (less load on the server)",
  "Eşleşen domain yok.": "No matching domain.",
  "Gerçek Zamanlı": "Real-time",
  "Gerçek zamanlı koruma": "Real-time protection",
  "Günlük tam tarama.": "Daily full scan.",
  "Güvenlik Duruşu": "Security Posture",
  "Hariç yollar (virgülle — tarama dışı tutulacak yollar)": "Excluded paths (comma-separated — paths to exclude from scanning)",
  "Henüz tarama yok.": "No scan yet.",
  "IO ağırlığı (1-10000)": "IO weight (1-10000)",
  "Karantina — tüm sunucu": "Quarantine — whole server",
  "Kesik gösterim — yalnızca ilk 64 KB.": "Truncated view — first 64 KB only.",
  "Kritik bulunca dosyayı otomatik karantinaya alır (WP çekirdeği hariç). KAPALIYKEN sadece bildirir.": "Automatically quarantines the file on a critical finding (except WP core). When OFF it only notifies.",
  "Kritik eşik (≥20)": "Critical threshold (≥20)",
  "Kısmi koruma": "Partial protection",
  "Kural Sayısı": "Rule Count",
  "Resmî md5 ile değişmiş/yabancı çekirdek dosyası.": "Core file changed/foreign vs official md5.",
  "Süreç davranış izleme": "Process behavior monitoring",
  "Tarama başlatılamadı (başka tarama sürüyor olabilir)": "Failed to start scan (another scan may be running)",
  "Tarama durumu güncellenmedi (zaman aşımı).": "Scan status not updated (timeout).",
  "Tarama yoğunluğu (dinamik kaynak)": "Scan intensity (dynamic resource)",
  "Tespit katmanları": "Detection layers",
  "Tüm /home dizini arka planda taranacak (RapidScan ile hızlandırılır). Başlatılsın mı?": "The entire /home directory will be scanned in the background (accelerated with RapidScan). Start it?",
  "Tüm Sunucuyu Tara": "Scan Whole Server",
  "Tüm sunucuyu tara": "Scan the whole server",
  "Veritabanı Tara": "Scan Database",
  "Veritabanı Tarayıcı": "Database Scanner",
  "WordPress çekirdek bütünlüğü": "WordPress core integrity",
  "Yeni/değişen dosyayı anında tarar (fanotify). Açınca izleyici servisi başlar.": "Scans new/changed files instantly (fanotify). Enabling starts the watcher service.",
  "Yüksek (hızlı)": "High (fast)",
  "Zamanlı tarama": "Scheduled scan",
  "cgroup + dinamik yük": "cgroup + dynamic load",
  "host (/home — müşteri siteleri)": "host (/home — customer sites)",
  "sunucu (/ — tüm sistem)": "server (/ — whole system)",
  "uploads/*.php, çift uzantı, gizli dizin.": "uploads/*.php, double extension, hidden directory.",
  "İmza/örüntü zinciri (eval+superglobal, shell, webshell).": "Signature/pattern chain (eval+superglobal, shell, webshell).",
  "İş parçacığı (0=otomatik)": "Threads (0=automatic)",
  "▸ Gelişmiş limitler (iş parçacığı, hız)": "▸ Advanced limits (threads, rate)",
  "▾ Gelişmiş limitler": "▾ Advanced limits",
  "geçici kısıtlı": "temporarily restricted",
  "Motor kararlı olana dek geçici olarak devre dışı (yanlış-pozitif önlemi).": "Temporarily disabled until the engine stabilizes (false-positive mitigation).",
  "Kural motoru": "Rule engine",
  "Kendi motorumuz (imza/örüntü zinciri). KAPALIYKEN yalnız clamav imzaları kullanılır — framework dosyalarında yanlış-pozitif azalır.": "Our own engine (signature/pattern chain). When OFF, only clamav signatures are used — fewer false positives on framework files.",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (AVPANEL_EN[tr] || ORTAK_EN[tr] || tr) : tr)

function Anahtar({ acik, ayarla, etiket, aciklama, uyari, kilit }: { acik: boolean; ayarla: (v: boolean) => void; etiket: string; aciklama?: string; uyari?: boolean; kilit?: boolean }) {
  // kilit: geçici olarak kısıtlandı — tıklanamaz, kapalı görünür.
  const gorunurAcik = kilit ? false : acik
  return (
    <button type="button" disabled={kilit} onClick={() => { if (!kilit) ayarla(!acik) }}
      className={`flex items-start gap-3 text-left w-full py-1.5 ${kilit ? 'opacity-60 cursor-not-allowed' : ''}`}>
      <span className={`mt-0.5 relative inline-flex h-5 w-9 flex-shrink-0 rounded-full transition ${gorunurAcik ? (uyari ? 'bg-amber-500' : 'bg-emerald-500') : 'bg-slate-300 dark:bg-slate-600'}`}>
        <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white transition ${gorunurAcik ? 'left-4' : 'left-0.5'}`} />
      </span>
      <span className="min-w-0">
        <span className="block text-sm text-slate-800 dark:text-slate-100">{etiket}{kilit && <span className="ml-1.5 text-[10px] uppercase tracking-wide text-amber-600 dark:text-amber-500">{cevir("geçici kısıtlı")}</span>}</span>
        {aciklama && <span className="block text-xs text-slate-400 dark:text-slate-500">{aciklama}</span>}
      </span>
    </button>
  )
}

const ANIM_CSS = `
@keyframes gosp-radar { 0% { transform: scale(.55); opacity:.7 } 100% { transform: scale(1.7); opacity:0 } }
@keyframes gosp-sweep { to { transform: rotate(360deg) } }
@keyframes gosp-draw  { to { stroke-dashoffset: 0 } }
@keyframes gosp-float { 0%,100% { transform: translateY(0) } 50% { transform: translateY(-5px) } }
@keyframes gosp-shimmer { 0% { transform: translateX(-100%) } 100% { transform: translateX(320%) } }
@keyframes gosp-spin { to { transform: rotate(360deg) } }
.gosp-ring  { transform-origin: 60px 60px; animation: gosp-radar 2.6s ease-out infinite }
.gosp-ring2 { animation-delay: 1.3s }
.gosp-sweepg { transform-origin: 60px 60px; animation: gosp-sweep 2.6s linear infinite }
.gosp-sweepg.fast { animation-duration: .9s }
.gosp-check { stroke-dasharray: 60; stroke-dashoffset: 60; animation: gosp-draw 1s ease-out .35s forwards }
.gosp-badge { animation: gosp-float 4.5s ease-in-out infinite }
.gosp-shimmer { animation: gosp-shimmer 1.3s ease-in-out infinite }
.gosp-modikon { transition: transform .2s }
.gosp-card:hover .gosp-modikon { transform: scale(1.12) rotate(-4deg) }
`

// Kalkan — animasyonlu güvenlik rozeti (radar pulse + tarama süpürme).
function Kalkan({ renk, tarama }: { renk: string; tarama: boolean }) {
  const c = renk === 'red' ? '#f87171' : renk === 'amber' ? '#fbbf24' : '#34d399'
  return (
    <svg viewBox="0 0 120 120" className="w-24 h-24 gosp-badge" fill="none" xmlns="http://www.w3.org/2000/svg">
      <circle cx="60" cy="60" r="24" className="gosp-ring" stroke={c} strokeWidth="1.5" />
      <circle cx="60" cy="60" r="24" className="gosp-ring gosp-ring2" stroke={c} strokeWidth="1.5" />
      <defs>
        <linearGradient id="gospShield" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor={c} stopOpacity="0.28" />
          <stop offset="1" stopColor={c} stopOpacity="0.06" />
        </linearGradient>
        <radialGradient id="gospSweep" cx="0.5" cy="0.5" r="0.5">
          <stop offset="0" stopColor={c} stopOpacity="0.55" />
          <stop offset="1" stopColor={c} stopOpacity="0" />
        </radialGradient>
      </defs>
      <path d="M60 15 L96 28 V59 C96 81 79 98 60 105 C41 98 24 81 24 59 V28 Z" fill="url(#gospShield)" stroke={c} strokeWidth="2.5" strokeLinejoin="round" />
      {tarama && (
        <g className="gosp-sweepg fast">
          <path d="M60 60 L60 22 A38 38 0 0 1 93 46 Z" fill="url(#gospSweep)" />
        </g>
      )}
      <path d="M45 61 l10 10 l21 -24" stroke={c} strokeWidth="4" strokeLinecap="round" strokeLinejoin="round" className="gosp-check" />
    </svg>
  )
}

const MODUL_SVG: Record<string, string> = {
  dosya: 'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z',
  realtime: 'M13 2L3 14h7l-1 8 10-12h-7l1-8z',
  db: 'M12 3c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3zM20 6v6c0 1.7-3.6 3-8 3s-8-1.3-8-3V6M20 12v6c0 1.7-3.6 3-8 3s-8-1.3-8-3v-6',
  slice: 'M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6',
}
function ModulIkon({ ad }: { ad: string }) {
  return (
    <svg viewBox="0 0 24 24" className="w-7 h-7 gosp-modikon" fill="none" stroke="currentColor" strokeWidth={1.7} strokeLinecap="round" strokeLinejoin="round">
      <path d={MODUL_SVG[ad] || MODUL_SVG.dosya} />
    </svg>
  )
}

const IKON_SVG: Record<string, string> = {
  bolt: 'M13 2L3 14h7l-1 8 10-12h-7l1-8z',
  db: 'M12 3c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3zM20 6v6c0 1.7-3.6 3-8 3s-8-1.3-8-3V6M20 12v6c0 1.7-3.6 3-8 3s-8-1.3-8-3v-6',
  lock: 'M7 10V7a5 5 0 0110 0v3M6 10h12a1 1 0 011 1v8a1 1 0 01-1 1H6a1 1 0 01-1-1v-8a1 1 0 011-1z',
  undo: 'M9 14L4 9l5-5M4 9h11a5 5 0 010 10h-3',
  trash: 'M4 7h16M9 7V5a1 1 0 011-1h4a1 1 0 011 1v2M6 7l1 13a1 1 0 001 1h8a1 1 0 001-1l1-13',
  warn: 'M12 4l9 16H3L12 4zM12 10v4M12 17h.01',
  check: 'M4 12l5 5L20 6',
  ban: 'M5.6 5.6l12.8 12.8M12 3a9 9 0 100 18 9 9 0 000-18z',
  dash: 'M5 12h14',
}
function Ikon({ ad, className }: { ad: string; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className || 'w-4 h-4'} fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
      <path d={IKON_SVG[ad] || IKON_SVG.warn} />
    </svg>
  )
}
function Rozet({ ad, renk, metin }: { ad: string; renk: string; metin: string }) {
  return <span className={`inline-flex items-center gap-1 text-xs ${renk}`}><Ikon ad={ad} className="w-3.5 h-3.5" />{metin}</span>
}

export default function AntivirusPanel() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const [d, setD] = useState<Durum | null>(null)
  const [kliste, setKliste] = useState<Kar[]>([])
  const [gecmis, setGecmis] = useState<Gecmis[]>([])
  const [kara, setKara] = useState<Kara[]>([])
  const [domainler, setDomainler] = useState<Domain[]>([])
  const [tarananDom, setTarananDom] = useState<Set<number>>(new Set())
  const [secili, setSecili] = useState<Set<number>>(new Set())
  const [filtreMetin, setFiltreMetin] = useState('')
  const [filtreDurum, setFiltreDurum] = useState<'hepsi' | 'enfekte' | 'karantina' | 'temiz'>('hepsi')
  const pollRef = useRef<Set<ReturnType<typeof setInterval>>>(new Set())
  const [ayar, setAyar] = useState<Ayar | null>(null)
  const [kap, setKap] = useState<AyarYanit['kapasite'] | null>(null)
  const [sekme, setSekme] = useState<Sekme>(() => {
    try { const v = localStorage.getItem('gosp.av.sekme') as Sekme
      return (['genel','domainler','karantina','gecmis','itibar','ayarlar'] as Sekme[]).includes(v) ? v : 'genel' } catch { return 'genel' }
  })
  function sekmeSec(k: Sekme) { setSekme(k); try { localStorage.setItem('gosp.av.sekme', k) } catch { /* yoksay */ } }
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [bilgi, setBilgi] = useState<string | null>(null)
  const [mesgul, setMesgul] = useState<string | null>(null) // hangi aksiyon calisiyor
  const [gelismis, setGelismis] = useState(false)
  const [inceleModal, setInceleModal] = useState<{ ad: string; icerik: string; kesik?: boolean } | null>(null)

  function durumYukle() {
    api.get<Durum>('/antivirus/durum').then(r => setD(r.data)).catch(e => setHata(apiHata(e)))
    api.get<{ kayitlar: Kar[] }>('/antivirus/karantina').then(r => setKliste(r.data.kayitlar || [])).catch(() => {})
    api.get<{ kayitlar: Gecmis[] }>('/antivirus/gecmis').then(r => setGecmis(r.data.kayitlar || [])).catch(() => {})
  }
  function ayarYukle() {
    api.get<AyarYanit>('/antivirus/ayarlar').then(r => { setAyar(r.data.ayarlar); setKap(r.data.kapasite) }).catch(e => setHata(apiHata(e))).finally(() => setYuk(false))
  }
  function itibarYukle() {
    setMesgul('itibar')
    api.get<{ kayitlar: Kara[] }>('/antivirus/kara-liste').then(r => setKara(r.data.kayitlar || [])).catch(e => setHata(apiHata(e))).finally(() => setMesgul(null))
  }
  function domainlerYukle() {
    api.get<{ kayitlar: Domain[] }>('/antivirus/domainler').then(r => setDomainler(r.data.kayitlar || [])).catch(e => setHata(apiHata(e)))
  }
  // taraVeBekle — bir domaini tarar ve BİTENE kadar bekler (Promise). Toplu
  // tarama bunları SIRAYLA çağırır: sunucuda tek-tarama kilidi var, paralel
  // gönderilse 409 alırdı.
  function taraVeBekle(id: number): Promise<void> {
    return new Promise(async (resolve) => {
      try {
        const { data } = await api.post<{ scan_id: number }>(`/antivirus/domainler/${id}/tara`, {})
        const sid = data.scan_id
        let deneme = 0
        // 🔴 poll'u ref'te tut (unmount'ta temizlenir) + zaman aşımı (~9dk >
        // backend 8dk context timeout): durum kalıcı 'calisiyor' kalırsa (sunucu
        // yeniden başlarsa) UI sonsuza dek asılı kalmasın.
        let t: ReturnType<typeof setInterval>
        const bit = () => { clearInterval(t); pollRef.current.delete(t); resolve() }
        t = setInterval(async () => {
          deneme++
          if (deneme > 220) { setHata(cevir("Tarama durumu güncellenmedi (zaman aşımı).")); bit(); return }
          try {
            const { data: st } = await api.get<{ durum: string }>(`/antivirus/domainler/${id}/tara/${sid}`)
            if (st.durum !== 'calisiyor') bit()
          } catch { bit() }
        }, 2500)
        pollRef.current.add(t)
      } catch (e) { setHata(apiHata(e, cevir("Tarama başlatılamadı (başka tarama sürüyor olabilir)"))); resolve() }
    })
  }
  async function domainTara(dm: Domain) {
    setHata(null); setTarananDom(s => new Set(s).add(dm.id))
    await taraVeBekle(dm.id)
    setTarananDom(s => { const n = new Set(s); n.delete(dm.id); return n })
    domainlerYukle(); durumYukle()
  }
  async function topluTara() {
    const idler = [...secili]
    if (idler.length === 0) return
    setHata(null)
    for (const id of idler) {
      setTarananDom(s => new Set(s).add(id))
      await taraVeBekle(id)
      setTarananDom(s => { const n = new Set(s); n.delete(id); return n })
      domainlerYukle()
    }
    setSecili(new Set()); durumYukle()
  }
  function secToggle(id: number) { setSecili(s => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n }) }
  useEffect(() => { durumYukle(); ayarYukle() }, [])
  useEffect(() => () => { pollRef.current.forEach((t: ReturnType<typeof setInterval>) => clearInterval(t)); pollRef.current.clear() }, [])
  useEffect(() => {
    if (sekme === 'itibar' && kara.length === 0) itibarYukle()
    if (sekme === 'domainler') domainlerYukle()
  }, [sekme])

  const set = (k: keyof Ayar, v: any) => setAyar(a => a ? { ...a, [k]: v } : a)
  function yogunluk(p: 'dusuk' | 'dengeli' | 'yuksek') {
    const on = p === 'dusuk' ? { cpu_yuzde: 25, is_parcacigi: 1, dosya_hiz_sn: 200, yuk_esigi: 50 }
      : p === 'dengeli' ? { cpu_yuzde: 0, is_parcacigi: 0, dosya_hiz_sn: 0, yuk_esigi: 80 }
      : { cpu_yuzde: 0, is_parcacigi: 0, dosya_hiz_sn: 0, yuk_esigi: 0 }
    setAyar(a => a ? { ...a, ...on } : a)
  }

  async function ayarKaydet() {
    if (!ayar) return
    setHata(null); setBilgi(null); setMesgul('ayar')
    try { await api.put('/antivirus/ayarlar', ayar); setBilgi(cevir("Ayarlar kaydedildi ve uygulandı.")); durumYukle(); ayarYukle() }
    catch (e) { setHata(apiHata(e, cevir("Ayarlar kaydedilemedi"))) } finally { setMesgul(null) }
  }
  async function taraTumu() {
    if (!(await onay({ baslik: cevir("Tüm sunucuyu tara"), mesaj: cevir("Tüm /home dizini arka planda taranacak (RapidScan ile hızlandırılır). Başlatılsın mı?") }))) return
    setHata(null); setMesgul('tara')
    try { await api.post('/antivirus/tara-tumu', {}); setBilgi(cevir("Dosya taraması başlatıldı — sonuçlar birkaç dakikada listeye düşer.")); setTimeout(durumYukle, 3000) }
    catch (e) { setHata(apiHata(e, cevir("Tarama başlatılamadı"))) } finally { setMesgul(null) }
  }
  async function dbTara() {
    setHata(null); setBilgi(null); setMesgul('db')
    try { const { data } = await api.post<{ taranan_kurulum: number; bulunan: number; hatali_kurulum: number }>('/antivirus/db-tara', {})
      setBilgi(cevirT(cevir("Veritabanı taraması: {0} WP kurulumu, {1} zararlı kayıt{2}."), data.taranan_kurulum, data.bulunan, data.hatali_kurulum ? cevirT(cevir(", {0} bağlanılamadı"), data.hatali_kurulum) : ''))
      durumYukle()
    } catch (e) { setHata(apiHata(e, cevir("DB taraması başarısız"))) } finally { setMesgul(null) }
  }
  async function geriYukle(k: Kar) {
    if (!(await onay({ baslik: cevir("Geri yükleme"), mesaj: cevirT(cevir("Dosya orijinal konumuna geri yüklensin mi?\n{0}\n({1})"), k.orijinal_yol, k.alan_adi) }))) return
    try { await api.post(`/antivirus/karantina/${k.id}/geri-yukle`, {}); durumYukle() } catch (e) { setHata(apiHata(e)) }
  }
  async function sil(k: Kar) {
    if (!(await onay({ baslik: cevir("Kalıcı silme"), mesaj: cevirT(cevir("Karantinadaki dosya KALICI silinsin mi?\n{0}\n({1})\n\nGeri alınamaz."), k.orijinal_yol, k.alan_adi) }))) return
    try { await api.post(`/antivirus/karantina/${k.id}/sil`, {}); durumYukle() } catch (e) { setHata(apiHata(e)) }
  }
  async function incele(k: Kar) {
    try { const { data } = await api.get<{ icerik: string; ikili: boolean; kesik?: boolean }>(`/antivirus/karantina/${k.id}/incele`); setInceleModal({ ad: `${k.orijinal_yol} (${k.alan_adi})`, icerik: data.ikili ? cevir("[ikili dosya]") : data.icerik, kesik: data.kesik }) }
    catch (e) { setHata(apiHata(e)) }
  }

  if (yuk) return <div className="px-4 py-4 sm:px-6 sm:py-5 text-slate-400">{cevir("Yükleniyor…")}</div>

  // Koruma duruşu skoru (0-100): açık katman/koruma oranı.
  const korumaAktif = !!(d?.izleyici_aktif && ayar?.gercek_zamanli)
  const tehdit = (d?.toplam_karantina || 0)
  const posture = tehdit > 0 ? { renk: 'red', metin: cevir("Tehdit var") }
    : korumaAktif ? { renk: 'emerald', metin: cevir("Korunuyor") }
      : { renk: 'amber', metin: cevir("Kısmi koruma") }

  const alan = 'w-full px-2.5 py-1.5 text-sm rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-100'
  const dfiltre = domainler.filter(dm => {
    const q = filtreMetin.trim().toLowerCase()
    const eslesme = q === '' || dm.alan_adi.toLowerCase().includes(q) || dm.sistem_kullanici.toLowerCase().includes(q)
    const durumOk = filtreDurum === 'hepsi'
      || (filtreDurum === 'enfekte' && dm.aktif_bulgu > 0)
      || (filtreDurum === 'karantina' && dm.karantina > 0)
      || (filtreDurum === 'temiz' && dm.aktif_bulgu === 0 && dm.karantina === 0)
    return eslesme && durumOk
  })
  const sekmeler: { k: Sekme; e: string; s?: number }[] = [
    { k: 'genel', e: cevir("Genel Bakış") },
    { k: 'domainler', e: cevir("Domainler"), s: domainler.length },
    { k: 'karantina', e: cevir("Karantina"), s: kliste.filter(x => x.durum === 'karantina').length },
    { k: 'gecmis', e: cevir("Bulgular"), s: gecmis.length },
    { k: 'itibar', e: cevir("İtibar") },
    { k: 'ayarlar', e: cevir("Ayarlar") },
  ]

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <div className="max-w-7xl mx-auto">
        <style>{ANIM_CSS}</style>
        <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Antivirüs") }]} />

        {/* ══ HERO: güvenlik duruşu konsolu ══ */}
        <div className="relative overflow-hidden rounded-3xl bg-gradient-to-br from-slate-900 via-slate-900 to-slate-800 dark:from-black dark:via-slate-950 dark:to-slate-900 text-white p-6 sm:p-8 mb-5 shadow-lg">
          <div className="absolute -right-16 -top-16 w-64 h-64 rounded-full blur-3xl opacity-20"
            style={{ background: posture.renk === 'red' ? '#ef4444' : posture.renk === 'amber' ? '#f59e0b' : '#10b981' }} />
          <div className="relative flex flex-wrap items-center gap-6 justify-between">
            <div className="flex items-center gap-5">
              <div className="w-24 h-24 flex items-center justify-center flex-shrink-0">
                <Kalkan renk={posture.renk} tarama={mesgul === 'tara' || mesgul === 'db'} />
              </div>
              <div>
                <div className="text-xs uppercase tracking-widest text-slate-400 mb-1">{cevir("Güvenlik Duruşu")}</div>
                <div className="text-3xl font-semibold">{posture.metin}</div>
                <div className="text-sm text-slate-400 mt-1">
                  {cevir("Kendi motorumuz")} · {cevir("sürüm")} {d?.kural_surum} · {d?.kural_sayisi} {cevir("kural")}{d?.kural_uretim && d.kural_uretim !== 'gomulu' ? ' · ' + cevir("imzalı") : ''}
                </div>
              </div>
            </div>
            <div className="flex gap-3">
              <button onClick={taraTumu} disabled={!!mesgul}
                className="inline-flex items-center gap-2 px-4 py-2.5 text-sm font-medium bg-white text-slate-900 rounded-xl hover:bg-slate-100 disabled:opacity-50">
                <Ikon ad="bolt" />{mesgul === 'tara' ? cevir("Başlatılıyor…") : cevir("Tüm Sunucuyu Tara")}</button>
              <button onClick={dbTara} disabled={!!mesgul}
                className="inline-flex items-center gap-2 px-4 py-2.5 text-sm font-medium bg-white/10 text-white rounded-xl hover:bg-white/20 disabled:opacity-50 backdrop-blur">
                <Ikon ad="db" />{mesgul === 'db' ? cevir("Taranıyor…") : cevir("Veritabanı Tara")}</button>
            </div>
          </div>
          {(mesgul === 'tara' || mesgul === 'db') && (
            <div className="relative mt-5 h-1 rounded-full bg-white/10 overflow-hidden">
              <div className="gosp-shimmer absolute inset-y-0 w-1/4 rounded-full bg-white/60" />
            </div>
          )}
          {/* metrik seridi */}
          <div className="relative grid grid-cols-2 sm:grid-cols-4 gap-4 mt-6 pt-6 border-t border-white/10">
            {[
              { e: cevir("Karantinada"), v: d?.toplam_karantina ?? 0, r: tehdit > 0 ? 'text-amber-400' : 'text-white', git: 'karantina' as Sekme },
              { e: cevir("Toplam Bulgu"), v: d?.toplam_bulgu ?? 0, r: 'text-white', git: 'gecmis' as Sekme },
              { e: cevir("Taranan Domain"), v: d?.taranan_domain ?? 0, r: 'text-white', git: 'domainler' as Sekme },
              { e: cevir("Kural Sayısı"), v: d?.kural_sayisi ?? 0, r: 'text-white' },
            ].map((m, i) => (
              <button key={i} type="button" disabled={!m.git}
                onClick={() => m.git && sekmeSec(m.git)}
                className={`text-left ${m.git ? 'cursor-pointer hover:opacity-80 transition-opacity' : 'cursor-default'}`}>
                <div className={`text-3xl font-semibold ${m.r}`}>{m.v}</div>
                <div className="text-xs uppercase tracking-wide text-slate-400 mt-0.5">{m.e}{m.git ? ' →' : ''}</div>
              </button>
            ))}
          </div>
        </div>

        {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
        {bilgi && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bilgi}</div>}

        {/* ══ Sekme çubuğu ══ */}
        <div className="flex gap-1 p-1 bg-slate-100 dark:bg-slate-800/60 rounded-2xl mb-5 overflow-x-auto">
          {sekmeler.map(t => (
            <button key={t.k} onClick={() => sekmeSec(t.k)}
              className={`px-4 py-2 text-sm font-medium rounded-xl whitespace-nowrap transition ${sekme === t.k ? 'bg-white dark:bg-slate-700 text-slate-900 dark:text-white shadow-sm' : 'text-slate-500 dark:text-slate-400 hover:text-slate-800 dark:hover:text-slate-200'}`}>
              {t.e}{t.s ? <span className={`ml-1.5 text-xs px-1.5 py-0.5 rounded-full ${sekme === t.k ? 'bg-slate-100 dark:bg-slate-600' : 'bg-slate-200 dark:bg-slate-700'}`}>{t.s}</span> : null}
            </button>
          ))}
        </div>

        {/* ══ DOMAINLER: domain-bazlı tarama ══ */}
        {sekme === 'domainler' && (
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <div className="flex flex-wrap items-center justify-between gap-3 mb-3">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Domain-bazlı tarama")} {domainler.length > 0 && <span className="text-xs font-normal text-slate-400">({dfiltre.length}/{domainler.length})</span>}</h3>
              <div className="flex flex-wrap items-center gap-2">
                <input type="search" value={filtreMetin} onChange={e => setFiltreMetin(e.target.value)} placeholder={cevir("Domain / kullanıcı ara…")}
                  className="px-3 py-1.5 text-sm rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-100 w-48" />
                <select value={filtreDurum} onChange={e => setFiltreDurum(e.target.value as any)}
                  className="px-2.5 py-1.5 text-sm rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 text-slate-800 dark:text-slate-100">
                  <option value="hepsi">{cevir("Tümü")}</option>
                  <option value="enfekte">{cevir("Enfekte")}</option>
                  <option value="karantina">{cevir("Karantinalı")}</option>
                  <option value="temiz">{cevir("Temiz")}</option>
                </select>
                <button onClick={domainlerYukle} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">{cevir("Yenile")}</button>
              </div>
            </div>
            {secili.size > 0 && (
              <div className="flex flex-wrap items-center justify-between gap-3 mb-3 px-3 py-2 bg-brand-50 dark:bg-brand-900/20 border border-brand-200 dark:border-brand-800 rounded-lg">
                <span className="text-sm text-brand-700 dark:text-brand-300">{cevirT(cevir("{0} domain seçili"), secili.size)}{(() => { const gizli = [...secili].filter(x => !dfiltre.some(d => d.id === x)).length; return gizli > 0 ? cevirT(cevir(" ({0} filtre dışı)"), gizli) : '' })()}</span>
                <div className="flex gap-2">
                  <button onClick={() => setSecili(new Set())} className="text-xs text-slate-500 hover:underline">{cevir("Seçimi temizle")}</button>
                  <button onClick={topluTara} disabled={tarananDom.size > 0}
                    className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50">
                    {tarananDom.size > 0 ? <><span className="inline-block w-3 h-3 border-2 border-white/60 border-t-transparent rounded-full animate-spin" /> {cevir("Taranıyor…")}</> : <><Ikon ad="bolt" className="w-3.5 h-3.5" /> {cevirT(cevir("Seçilenleri Tara ({0})"), secili.size)}</>}
                  </button>
                </div>
              </div>
            )}
            {dfiltre.length === 0 ? <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">{domainler.length === 0 ? cevir("Domain yok.") : cevir("Eşleşen domain yok.")}</div> : (
              <div className="lg:overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}>
                    <tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                      <th className={`${T.baslik} w-8`}><input type="checkbox" aria-label={cevir("Tümünü seç")} className="accent-brand-600 align-middle" checked={dfiltre.length > 0 && dfiltre.every(x => secili.has(x.id))} onChange={e => setSecili(s => { const n = new Set(s); if (e.target.checked) dfiltre.forEach(x => n.add(x.id)); else dfiltre.forEach(x => n.delete(x.id)); return n })} /></th>
                      <th className={T.baslik}>Domain</th><th className={T.baslik}>{cevir("Kullanıcı")}</th><th className={T.baslik}>{cevir("Son tarama")}</th><th className={T.baslik}>{cevir("Aktif bulgu")}</th><th className={T.baslik}>{cevir("Karantina")}</th><th className={T.baslik}></th>
                    </tr>
                  </thead>
                  <tbody className={T.govde}>
                    {dfiltre.map(dm => (
                      <tr key={dm.id} className={`${T.satir} ${secili.has(dm.id) ? 'bg-brand-50/50 dark:bg-brand-900/10' : ''}`}>
                        <td className={T.hucre}><input type="checkbox" aria-label={cevirT(cevir("{0} seç"), dm.alan_adi)} className="accent-brand-600 align-middle" checked={secili.has(dm.id)} onChange={() => secToggle(dm.id)} /></td>
                        <td className={T.hucreBaslik}>{dm.alan_adi}</td>
                        <td className={T.hucre} data-etiket={cevir("Kullanıcı")}><span className="font-mono text-xs text-slate-500">{dm.sistem_kullanici}</span></td>
                        <td className={T.hucre} data-etiket={cevir("Son tarama")}><span className="text-xs text-slate-500">{dm.son_tarama || '—'}{dm.son_taranan > 0 ? ` · ${dm.son_taranan} ` + cevir("dosya") : ''}</span></td>
                        <td className={T.hucre} data-etiket={cevir("Aktif bulgu")}><span className={dm.aktif_bulgu > 0 ? 'text-red-600 dark:text-red-400 font-medium' : 'text-slate-400'}>{dm.aktif_bulgu}</span></td>
                        <td className={T.hucre} data-etiket={cevir("Karantina")}><span className={dm.karantina > 0 ? 'text-amber-600 dark:text-amber-400 font-medium' : 'text-slate-400'}>{dm.karantina}</span></td>
                        <td className={`${T.hucreAksiyon} lg:text-right`}>
                          <button onClick={() => domainTara(dm)} disabled={tarananDom.has(dm.id)}
                            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white rounded-lg disabled:opacity-50 whitespace-nowrap">
                            {tarananDom.has(dm.id) ? <><span className="inline-block w-3 h-3 border-2 border-white/60 border-t-transparent rounded-full animate-spin" /> {cevir("Taranıyor…")}</> : <><Ikon ad="bolt" className="w-3.5 h-3.5" /> {cevir("Tara")}</>}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ══ GENEL BAKIŞ: koruma modülleri + son taramalar ══ */}
        {sekme === 'genel' && d && (
          <div className="space-y-5">
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              {[
                { ad: cevir("Dosya Tarayıcı"), ikon: 'dosya', aktif: d.ajan_kurulu, alt: cevirT(cevir("{0} kural · RapidScan"), d.kural_sayisi), drm: d.ajan_kurulu ? cevir("Kurulu") : cevir("Yok") },
                { ad: cevir("Gerçek Zamanlı"), ikon: 'realtime', aktif: d.izleyici_aktif, alt: cevir("fanotify izleme"), drm: d.izleyici_aktif ? cevir("Aktif") : cevir("Kapalı") },
                { ad: cevir("Veritabanı Tarayıcı"), ikon: 'db', aktif: true, alt: 'wp_options + wp_posts', drm: cevir("Hazır") },
                { ad: cevir("Kaynak Dilimi"), ikon: 'slice', aktif: d.slice_aktif, alt: cevir("cgroup + dinamik yük"), drm: d.slice_aktif ? cevir("Aktif") : cevir("Kapalı") },
              ].map((m, i) => (
                <div key={i} className="gosp-card bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-4 shadow-sm hover:shadow-md hover:border-slate-300 dark:hover:border-slate-600 transition">
                  <div className="flex items-center justify-between mb-2">
                    <span className={m.aktif ? 'text-emerald-500' : 'text-slate-400'}><ModulIkon ad={m.ikon} /></span>
                    <span className={`text-xs px-2 py-0.5 rounded-full ${m.aktif ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-400' : 'bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300'}`}>{m.drm}</span>
                  </div>
                  <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">{m.ad}</div>
                  <div className="text-xs text-slate-400 mt-0.5">{m.alt}</div>
                </div>
              ))}
            </div>

            <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{cevir("Son taramalar")}</h3>
              {d.son_taramalar.length === 0 ? <div className="text-center py-6 text-sm text-slate-400">{cevir("Henüz tarama yok.")}</div> : (
                <div className="lg:overflow-x-auto">
                  <table className={`${T.tablo} text-sm`}>
                    <thead className={T.baslikGrubu}><tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                      <th className={T.baslik}>Domain</th><th className={T.baslik}>{cevir("Kaynak")}</th><th className={T.baslik}>{cevir("Taranan")}</th><th className={T.baslik}>{cevir("Enfekte")}</th><th className={T.baslik}>{cevir("Durum")}</th><th className={T.baslik}>{cevir("Bitiş")}</th>
                    </tr></thead>
                    <tbody className={T.govde}>{d.son_taramalar.map(t => (
                      <tr key={t.id} className={T.satir}>
                        <td className={T.hucreBaslik}>{t.alan_adi || cevir("— (sunucu)")}</td>
                        <td className={T.hucre} data-etiket={cevir("Kaynak")}><span className="text-xs px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-700 text-slate-600 dark:text-slate-300">{t.kaynak || 'panel'}</span></td>
                        <td className={T.hucre} data-etiket={cevir("Taranan")}>{t.taranan}</td>
                        <td className={T.hucre} data-etiket={cevir("Enfekte")}><span className={t.enfekte > 0 ? 'text-red-600 dark:text-red-400 font-medium' : 'text-slate-400'}>{t.enfekte}</span></td>
                        <td className={T.hucre} data-etiket={cevir("Durum")}><span className="text-xs text-slate-500">{t.durum}</span></td>
                        <td className={T.hucre} data-etiket={cevir("Bitiş")}><span className="text-xs text-slate-400">{t.bitis || '—'}</span></td>
                      </tr>
                    ))}</tbody>
                  </table>
                </div>
              )}
            </div>
          </div>
        )}

        {/* ══ KARANTİNA ══ */}
        {sekme === 'karantina' && (
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <h3 className="inline-flex items-center gap-1.5 text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3"><Ikon ad="lock" className="w-4 h-4" /> {cevir("Karantina — tüm sunucu")}</h3>
            {kliste.length === 0 ? <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">{cevir("Karantinada dosya yok.")}</div> : (
              <div className="lg:overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}><tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Domain</th><th className={T.baslik}>{cevir("Dosya")}</th><th className={T.baslik}>{cevir("Tespit")}</th><th className={T.baslik}>{cevir("Durum")}</th><th className={T.baslik}>{cevir("Tarih")}</th><th className={T.baslik}></th>
                  </tr></thead>
                  <tbody className={T.govde}>{kliste.map(k => (
                    <tr key={k.id} className={T.satir}>
                      <td className={T.hucreBaslik}>{k.alan_adi || `#${k.domain_id}`}</td>
                      <td className={T.hucre} data-etiket={cevir("Dosya")}><span className="font-mono text-xs break-all lg:max-w-xs inline-block">{k.orijinal_yol}</span></td>
                      <td className={T.hucre} data-etiket={cevir("Tespit")}><span className="text-xs text-slate-600 dark:text-slate-300 break-all">{k.imza} <span className="text-slate-400">({k.puan})</span></span></td>
                      <td className={T.hucre} data-etiket={cevir("Durum")}>
                        {k.durum === 'karantina' ? <Rozet ad="lock" renk="text-amber-600 dark:text-amber-400" metin={cevir("Karantinada")} />
                          : k.durum === 'geri_yuklendi' ? <Rozet ad="undo" renk="text-emerald-600 dark:text-emerald-400" metin={cevir("Geri yüklendi")} />
                          : <Rozet ad="trash" renk="text-slate-400" metin={cevir("Silindi")} />}
                      </td>
                      <td className={T.hucre} data-etiket={cevir("Tarih")}><span className="text-xs text-slate-400">{k.tarih}</span></td>
                      <td className={`${T.hucreAksiyon} lg:text-right`}>
                        {k.durum === 'karantina' && k.mevcut && (
                          <span className="flex gap-2 lg:justify-end whitespace-nowrap">
                            <button onClick={() => incele(k)} className="text-xs text-slate-500 hover:underline">{cevir("İncele")}</button>
                            <button onClick={() => geriYukle(k)} className="text-xs text-emerald-600 dark:text-emerald-400 hover:underline">{cevir("Geri yükle")}</button>
                            <button onClick={() => sil(k)} className="text-xs text-red-600 dark:text-red-400 hover:underline">{cevir("Sil")}</button>
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}</tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ══ GEÇMİŞ ══ */}
        {sekme === 'gecmis' && (
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{cevir("Olay günlüğü")} {gecmis.length > 0 && <span className="text-xs font-normal text-slate-400">{cevirT(cevir("({0} kayıt)"), gecmis.length)}</span>}</h3>
            {gecmis.length === 0 ? <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">{cevir("Kayıt yok.")}</div> : (
              <div className="lg:overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}><tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>{cevir("Tarih")}</th><th className={T.baslik}>Domain</th><th className={T.baslik}>{cevir("Dosya")}</th><th className={T.baslik}>{cevir("Tespit")}</th><th className={T.baslik}>{cevir("Durum")}</th>
                  </tr></thead>
                  <tbody className={T.govde}>{gecmis.map(g => (
                    <tr key={g.id} className={T.satir}>
                      <td className={T.hucreBaslik}><span className="text-xs text-slate-500">{g.tarih}</span></td>
                      <td className={T.hucre} data-etiket="Domain">{g.alan_adi || `#${g.domain_id}`}</td>
                      <td className={T.hucre} data-etiket={cevir("Dosya")}><span className="font-mono text-xs break-all lg:max-w-xs inline-block">{g.dosya}</span></td>
                      <td className={T.hucre} data-etiket={cevir("Tespit")}><span className="text-xs text-slate-600 dark:text-slate-300 break-all">{g.imza} <span className="text-slate-400">({g.puan})</span></span></td>
                      <td className={T.hucre} data-etiket={cevir("Durum")}>
                        {g.durum === 'karantina' ? <Rozet ad="lock" renk="text-amber-600 dark:text-amber-400" metin={cevir("Karantina")} />
                          : g.durum === 'geri_yuklendi' ? <Rozet ad="undo" renk="text-emerald-600 dark:text-emerald-400" metin={cevir("Geri yüklendi")} />
                          : g.durum === 'silindi' ? <Rozet ad="trash" renk="text-slate-400" metin={cevir("Silindi")} />
                          : <Rozet ad="warn" renk="text-red-600 dark:text-red-400" metin={cevir("Aktif")} />}
                      </td>
                    </tr>
                  ))}</tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ══ İTİBAR / KARA-LİSTE ══ */}
        {sekme === 'itibar' && (
          <div className="lg:bg-white dark:lg:bg-slate-800 lg:border lg:border-slate-200 dark:lg:border-slate-700 lg:rounded-2xl lg:p-5 lg:shadow-sm">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Domain itibarı — Spamhaus DBL")}</h3>
              <button onClick={itibarYukle} disabled={mesgul === 'itibar'} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50">{mesgul === 'itibar' ? cevir("Kontrol ediliyor…") : cevir("Yenile")}</button>
            </div>
            {kara.length === 0 ? <div className="text-center py-10 text-sm text-slate-500 dark:text-slate-400">{mesgul === 'itibar' ? cevir("Kontrol ediliyor…") : cevir("Kayıt yok.")}</div> : (
              <div className="lg:overflow-x-auto">
                <table className={`${T.tablo} text-sm`}>
                  <thead className={T.baslikGrubu}><tr className="text-left text-xs text-slate-400 border-b border-slate-100 dark:border-slate-700">
                    <th className={T.baslik}>Domain</th><th className={T.baslik}>{cevir("Durum")}</th><th className={T.baslik}>{cevir("Kaynak")}</th>
                  </tr></thead>
                  <tbody className={T.govde}>{kara.map(k => (
                    <tr key={k.domain_id} className={T.satir}>
                      <td className={T.hucreBaslik}>{k.alan_adi}</td>
                      <td className={T.hucre} data-etiket={cevir("Durum")}>
                        {k.durum === 'listeli' ? <Rozet ad="ban" renk="text-red-600 dark:text-red-400 font-medium" metin={cevir("Kara listede")} />
                          : k.durum === 'kontrol_edilemedi' ? <Rozet ad="dash" renk="text-slate-400" metin={cevir("kontrol edilemedi")} />
                          : <Rozet ad="check" renk="text-emerald-600 dark:text-emerald-400" metin={cevir("Temiz")} />}
                      </td>
                      <td className={T.hucre} data-etiket={cevir("Kaynak")}><span className="text-xs text-slate-400">{k.kaynak}</span></td>
                    </tr>
                  ))}</tbody>
                </table>
              </div>
            )}
          </div>
        )}

        {/* ══ AYARLAR ══ */}
        {sekme === 'ayarlar' && ayar && (
          <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm">
            <div className="flex items-center justify-between mb-3">
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">{cevir("Ayarlar")}</h3>
              <button onClick={ayarKaydet} disabled={mesgul === 'ayar'} className="px-4 py-1.5 text-sm font-medium bg-brand-600 hover:bg-brand-700 text-white rounded-lg disabled:opacity-50">{mesgul === 'ayar' ? cevir("Kaydediliyor…") : cevir("Kaydet")}</button>
            </div>
            <div className="grid gap-x-8 gap-y-1 sm:grid-cols-2">
              <div>
                <div className="text-xs font-semibold text-slate-400 uppercase mt-1 mb-1">{cevir("Koruma")}</div>
                <Anahtar kilit acik={ayar.gercek_zamanli} ayarla={v => set('gercek_zamanli', v)} etiket={cevir("Gerçek zamanlı koruma")} aciklama={cevir("Motor kararlı olana dek geçici olarak devre dışı (yanlış-pozitif önlemi).")} />
                <Anahtar acik={ayar.zamanli_tarama} ayarla={v => set('zamanli_tarama', v)} etiket={cevir("Zamanlı tarama")} aciklama={cevir("Günlük tam tarama.")} />
                {ayar.zamanli_tarama && (
                  <div className="ml-12 mb-1 flex items-center gap-2"><span className="text-xs text-slate-400">{cevir("Saat")}</span>
                    <input type="time" value={ayar.zamanli_saat} onChange={e => set('zamanli_saat', e.target.value)} className={`${alan} w-28`} /></div>
                )}
                <Anahtar acik={ayar.oto_karantina} ayarla={v => set('oto_karantina', v)} uyari etiket={cevir("Otomatik karantina")} aciklama={cevir("Kritik bulunca dosyayı otomatik karantinaya alır (WP çekirdeği hariç). KAPALIYKEN sadece bildirir.")} />
                <Anahtar acik={ayar.surec_izleme} ayarla={v => set('surec_izleme', v)} etiket={cevir("Süreç davranış izleme")} aciklama={cevir("Şüpheli süreç zincirlerini yakalar (php-fpm→kabuk, indir-çalıştır, webroot binary). Açınca izleme için root + CAP_SYS_PTRACE'li bir servis başlar (yalnız kimlik doğrulama; ptrace çağrısı engelli). Bildirim modu — süreç öldürmez.")} />
              </div>
              <div>
                <div className="text-xs font-semibold text-slate-400 uppercase mt-1 mb-1">{cevir("Tespit katmanları")}</div>
                <Anahtar acik={ayar.kural_motoru} ayarla={v => set('kural_motoru', v)} etiket={cevir("Kural motoru")} aciklama={cevir("Kendi motorumuz (imza/örüntü zinciri). KAPALIYKEN yalnız clamav imzaları kullanılır — framework dosyalarında yanlış-pozitif azalır.")} />
                <Anahtar acik={ayar.konum_sezgileri} ayarla={v => set('konum_sezgileri', v)} etiket={cevir("Konum sezgileri")} aciklama={cevir("uploads/*.php, çift uzantı, gizli dizin.")} />
                <Anahtar acik={ayar.wp_butunluk} ayarla={v => set('wp_butunluk', v)} etiket={cevir("WordPress çekirdek bütünlüğü")} aciklama={cevir("Resmî md5 ile değişmiş/yabancı çekirdek dosyası.")} />
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2 mt-4 pt-4 border-t border-slate-100 dark:border-slate-700">
              <label className="block"><span className="block text-xs text-slate-400 mb-1">{cevir("Kapsam")}</span>
                <select value={ayar.kapsam} onChange={e => set('kapsam', e.target.value)} className={alan}>
                  <option value="host">{cevir("host (/home — müşteri siteleri)")}</option>
                  <option value="sunucu">{cevir("sunucu (/ — tüm sistem)")}</option>
                </select></label>
              <label className="block"><span className="block text-xs text-slate-400 mb-1">{cevir("Kritik eşik (≥20)")}</span>
                <input type="number" min={20} value={ayar.esik_kritik} onChange={e => set('esik_kritik', Number(e.target.value))} className={alan} /></label>
            </div>
            <label className="block mt-4"><span className="block text-xs text-slate-400 mb-1">{cevir("Hariç yollar (virgülle — tarama dışı tutulacak yollar)")}</span>
              <textarea value={ayar.haric_yollar} onChange={e => set('haric_yollar', e.target.value)} rows={3} spellCheck={false}
                className={`${alan} font-mono text-xs leading-relaxed resize-y min-h-[80px]`}
                placeholder="/proc,/sys,/var/lib/mysql,node_modules,.git" /></label>
            <div className="mt-4 pt-4 border-t border-slate-100 dark:border-slate-700">
              <div className="text-xs font-semibold text-slate-400 uppercase mb-2">{cevir("Tarama yoğunluğu (dinamik kaynak)")}</div>
              <div className="flex flex-wrap items-center gap-2 mb-3">
                <button type="button" onClick={() => yogunluk('dusuk')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">{cevir("Düşük (sunucuyu az yorar)")}</button>
                <button type="button" onClick={() => yogunluk('dengeli')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">{cevir("Dengeli")}</button>
                <button type="button" onClick={() => yogunluk('yuksek')} className="px-3 py-1.5 text-xs rounded-lg border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:hover:bg-slate-700">{cevir("Yüksek (hızlı)")}</button>
              </div>
              <label className="flex items-center gap-3 flex-wrap">
                <span className="text-sm text-slate-700 dark:text-slate-200">{cevir("Dinamik yük eşiği")}</span>
                <input type="number" min={0} max={400} value={ayar.yuk_esigi} onChange={e => set('yuk_esigi', Number(e.target.value))} className={`${alan} w-24`} />
                <span className="text-xs text-slate-400">{cevir("% çekirdek · 0 = kapalı. Sistem 1-dk yükü bu değeri (ör. 80 = ×0.8 çekirdek) aşarsa tarama kendini duraklatır.")}</span>
              </label>

              {/* CPU + RAM limitleri — görünür ve dinamik (cgroup slice'a uygulanır) */}
              <div className="grid gap-5 sm:grid-cols-2 mt-4">
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-sm text-slate-700 dark:text-slate-200">{cevir("CPU limiti (çekirdek)")}</span>
                    <span className="text-xs font-mono text-slate-500 dark:text-slate-400">{ayar.cpu_yuzde === 0 ? cevir("otomatik") + (kap ? ` (~${(kap.oneri_cpu_yuzde / 100).toFixed(1)})` : '') : `${(ayar.cpu_yuzde / 100).toFixed(1)} ${cevir("çekirdek")}`}</span>
                  </div>
                  <div className="flex items-center gap-3">
                    <input type="range" min={0} max={kap ? kap.cpu_cekirdek : 8} step={0.5} value={ayar.cpu_yuzde / 100} onChange={e => set('cpu_yuzde', Math.round(Number(e.target.value) * 100))} className="flex-1 accent-brand-600" />
                    <input type="number" min={0} max={kap ? kap.cpu_cekirdek : 64} step={0.5} value={ayar.cpu_yuzde / 100} onChange={e => set('cpu_yuzde', Math.round(Number(e.target.value) * 100))} className={`${alan} w-20`} />
                  </div>
                  <span className="text-xs text-slate-400">{cevir("0 = otomatik. Sunucu:")} {kap ? `${kap.cpu_cekirdek} ${cevir("çekirdek")}` : '—'}{cevir(". Örn. 2 = iki tam çekirdek.")}</span>
                </div>
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-sm text-slate-700 dark:text-slate-200">{cevir("RAM limiti (GB)")}</span>
                    <span className="text-xs font-mono text-slate-500 dark:text-slate-400">{ayar.ram_mb === 0 ? cevir("otomatik") + (kap ? ` (~${(kap.oneri_ram_mb / 1024).toFixed(1)} GB)` : '') : `${(ayar.ram_mb / 1024).toFixed(2)} GB`}</span>
                  </div>
                  <div className="flex items-center gap-3">
                    <input type="range" min={0} max={kap ? Math.ceil(kap.ram_toplam_mb / 1024) : 4} step={0.25} value={ayar.ram_mb / 1024} onChange={e => set('ram_mb', Math.round(Number(e.target.value) * 1024))} className="flex-1 accent-brand-600" />
                    <input type="number" min={0} max={kap ? Math.ceil(kap.ram_toplam_mb / 1024) : undefined} step={0.25} value={Number((ayar.ram_mb / 1024).toFixed(2))} onChange={e => set('ram_mb', Math.round(Number(e.target.value) * 1024))} className={`${alan} w-24`} />
                  </div>
                  <span className="text-xs text-slate-400">{cevir("0 = otomatik. Toplam:")} {kap ? `${(kap.ram_toplam_mb / 1024).toFixed(1)} GB` : '—'}{cevir(". Aşılırsa tarayıcı OOM ile durur (site etkilenmez).")}</span>
                </div>
              </div>
            </div>
            <button onClick={() => setGelismis(g => !g)} className="text-xs text-brand-600 dark:text-brand-400 mt-4">{gelismis ? cevir("▾ Gelişmiş limitler") : cevir("▸ Gelişmiş limitler (iş parçacığı, hız)")}</button>
            {gelismis && (
              <div className="grid gap-4 sm:grid-cols-3 mt-2">
                <label className="block"><span className="block text-xs text-slate-400 mb-1">{cevir("İş parçacığı (0=otomatik)")}</span><input type="number" min={0} value={ayar.is_parcacigi} onChange={e => set('is_parcacigi', Number(e.target.value))} className={alan} /></label>
                <label className="block"><span className="block text-xs text-slate-400 mb-1">{cevir("Dosya hız/sn (0=sınırsız)")}</span><input type="number" min={0} value={ayar.dosya_hiz_sn} onChange={e => set('dosya_hiz_sn', Number(e.target.value))} className={alan} /></label>
                <label className="block"><span className="block text-xs text-slate-400 mb-1">{cevir("IO ağırlığı (1-10000)")}</span><input type="number" min={1} max={10000} value={ayar.io_agirlik} onChange={e => set('io_agirlik', Number(e.target.value))} className={alan} /></label>
              </div>
            )}
          </div>
        )}

        {/* İnceleme modalı — dosya ÇALIŞTIRILMADAN düz metin */}
        {inceleModal && (
          <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4" onClick={() => setInceleModal(null)}>
            <div className="bg-white dark:bg-slate-800 rounded-2xl max-w-3xl w-full max-h-[80vh] flex flex-col shadow-xl" onClick={e => e.stopPropagation()}>
              <div className="px-4 py-3 border-b border-slate-200 dark:border-slate-700 flex items-center justify-between">
                <span className="text-sm font-mono text-slate-700 dark:text-slate-200 break-all">{inceleModal.ad}</span>
                <button onClick={() => setInceleModal(null)} className="text-slate-400 hover:text-slate-600 text-lg">×</button>
              </div>
              {inceleModal.kesik && <div className="px-4 pt-3 text-xs text-amber-600 dark:text-amber-400 inline-flex items-center gap-1"><Ikon ad="warn" className="w-3.5 h-3.5" /> {cevir("Kesik gösterim — yalnızca ilk 64 KB.")}</div>}
              <pre className="p-4 overflow-auto text-xs font-mono text-slate-800 dark:text-slate-200 whitespace-pre-wrap break-all">{inceleModal.icerik}</pre>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
