import { cevirT } from '@/lib/cevirT'
import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useEffect, useMemo, useState } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'
import { useDialog } from '@/components/Dialog'
import { useToast } from '@/components/Toast'
import { Button } from '@/components/ui/Button'

type Kural = {
  id: number; tip: 'ban' | 'whitelist' | 'kapat'; ip: string; port: number
  protokol: string; aciklama: string; aktif: boolean; created_at: string
}
type ListeResp = { kurallar: Kural[]; korumali_portlar: number[] }

// Hazır şablonlar — tek tıkla yaygın açık portları kapat
const SABLONLAR = [
  { key: 'mysql_kapat', ikon: 'db', ad: "MySQL'i Dışa Kapat", portlar: '3306',
    aciklama: 'Veritabanı portunu (3306) internete kapatır. MySQL yalnız sunucu içinden erişilir.' },
  { key: 'ftp_kapat', ikon: 'folder', ad: "FTP'yi Kapat", portlar: '21',
    aciklama: 'FTP portunu (21) kapatır. SFTP kullanıyorsanız FTP güvenle kapatılabilir.' },
  { key: 'mail_kapat', ikon: 'mail', ad: 'Mail Portlarını Kapat', portlar: '25, 465, 587, 110, 143',
    aciklama: 'SMTP/POP3/IMAP portlarını kapatır. Mail sunucusu yoksa spam-relay riskini azaltır.' },
  { key: 'rpc_kapat', ikon: 'share', ad: 'RPC / NFS Kapat', portlar: '111, 2049',
    aciklama: 'rpcbind (111) ve NFS (2049) portlarını kapatır. Dosya paylaşımı kullanmıyorsanız kapatın.' },
] as const

// Manuel kural modları — açıklama + örnek. renk: vurgu tonu (segment + rozet).
const MODLAR = {
  ban: { ikon: 'ban', ad: 'IP Yasakla', renk: 'red',
    aciklama: 'Belirli bir IP adresini engelle. Port yazarsan sadece o porta, boş bırakırsan TÜM portlara erişimi kesilir.',
    ornek: 'Örnek: Sürekli SSH deneyen 45.9.1.2 adresini tamamen engelle.' },
  whitelist: { ikon: 'allow', ad: 'İzin Ver', renk: 'emerald',
    aciklama: 'Port yazarsan o port SADECE bu IP(ler)e açılır — diğer herkes engellenir (allowlist). Portu boş bırakırsan bu IP tüm portlara öncelikli erişir (yasaklardan önce değerlendirilir).',
    ornek: "Örnek: Port 8443 yazıp ofis IP'nizi girin → panele yalnız siz erişebilirsiniz." },
  kapat: { ikon: 'lock', ad: 'Port Kapat', renk: 'amber',
    aciklama: 'Bir portu HERKESE kapat (beyaz listedekiler hariç). Kritik portlar (SSH/web/panel/DNS) korunur; kapatılamaz.',
    ornek: "Örnek: Veritabanı portu 3306'yı dışarıya kapat." },
} as const

// ── Gerçek SVG ikon seti (emoji YOK) ─────────────────────────────────────
const FW_YOL: Record<string, string> = {
  ban: 'M4.9 4.9l14.2 14.2M12 3a9 9 0 100 18 9 9 0 000-18z',
  allow: 'M9 12.5l2.2 2.2L15.5 10M12 3l7 2.6v5.2c0 4.3-3 7-7 8.2-4-1.2-7-3.9-7-8.2V5.6L12 3z',
  lock: 'M7 10V8a5 5 0 0110 0v2M5.5 10h13a1 1 0 011 1v8a1 1 0 01-1 1h-13a1 1 0 01-1-1v-8a1 1 0 011-1zM12 14.5v2.5',
  db: 'M12 3c4.4 0 8 1.3 8 3s-3.6 3-8 3-8-1.3-8-3 3.6-3 8-3zM4 6v6c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12v6c0 1.7 3.6 3 8 3s8-1.3 8-3v-6',
  folder: 'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2V7z',
  mail: 'M3.5 6h17a1 1 0 011 1v10a1 1 0 01-1 1h-17a1 1 0 01-1-1V7a1 1 0 011-1zM3 7l9 6 9-6',
  share: 'M8.6 13.5l6.8 4M15.4 6.5l-6.8 4M18 7a2.5 2.5 0 10-5 0 2.5 2.5 0 005 0zM8 12a2.5 2.5 0 10-5 0 2.5 2.5 0 005 0zM18 17a2.5 2.5 0 10-5 0 2.5 2.5 0 005 0z',
  shield: 'M12 3l7 2.6v5.2c0 4.3-3 7-7 8.2-4-1.2-7-3.9-7-8.2V5.6L12 3z',
  globe: 'M12 3a9 9 0 100 18 9 9 0 000-18zM3 12h18M12 3c2.5 2.4 3.8 5.6 3.8 9s-1.3 6.6-3.8 9c-2.5-2.4-3.8-5.6-3.8-9S9.5 5.4 12 3z',
  server: 'M4 5h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1V6a1 1 0 011-1zM4 13h16a1 1 0 011 1v4a1 1 0 01-1 1H4a1 1 0 01-1-1v-4a1 1 0 011-1zM7 8h.01M7 16h.01',
  ok: 'M20 6L9 17l-5-5',
}
function FwIkon({ ad, className = 'h-5 w-5' }: { ad: string; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7"
      strokeLinecap="round" strokeLinejoin="round" className={className} aria-hidden="true">
      <path d={FW_YOL[ad] || FW_YOL.shield} />
    </svg>
  )
}
const RENK: Record<string, { seg: string; rail: string; rozet: string; ikon: string }> = {
  red:     { seg: 'bg-red-600 border-red-600',         rail: 'bg-red-500',     rozet: 'text-red-600 dark:text-red-400',         ikon: 'text-red-500' },
  emerald: { seg: 'bg-emerald-600 border-emerald-600', rail: 'bg-emerald-500', rozet: 'text-emerald-600 dark:text-emerald-400', ikon: 'text-emerald-500' },
  amber:   { seg: 'bg-amber-600 border-amber-600',     rail: 'bg-amber-500',   rozet: 'text-amber-700 dark:text-amber-300',     ikon: 'text-amber-500' },
}
const TIP_RENK: Record<string, string> = { ban: 'red', whitelist: 'emerald', kapat: 'amber' }


const FW_EN: Record<string, string> = {
  "Türkçe": "English",
  "FTP'yi Kapat": "Close FTP",
  "RPC / NFS Kapat": "Close RPC / NFS",
  "IP Yasakla": "Ban IP",
  "Port Kapat": "Close Port",
  "Güvenlik Duvarı": "Firewall",
  "Sunucunuza": "Control",
  "internetten kimin erişebileceğini": "who can access your server from the internet",
  "kontrol edin. Hazır bir şablon uygulayın veya kendi kuralınızı ekleyin.": ". Apply a ready template or add your own rule.",
  "Kurallar yalnızca": "Rules only affect",
  "yeni bağlantıları": "new connections",
  "etkiler — açık oturumunuz (SSH/panel) kopmaz. Kritik portlar": "— your open session (SSH/panel) is not dropped. Critical ports",
  "güvenlik için kapatılamaz.": "cannot be closed for security.",
  "Port {0} HERKESE kapatılacak (beyaz listedekiler hariç).": "Port {0} will be closed to EVERYONE (except allowlisted).",
  "Kapatılacak portu girin.": "Enter the port to close.",
  "{0} adresinin {1} erişimi ENGELLENECEK.": "Access to {1} for {0} will be BLOCKED.",
  "Port {0} yalnızca {1} adresine açık olacak — diğer herkes ENGELLENİR (allowlist).": "Port {0} will be open only to {1} — everyone else is BLOCKED (allowlist).",
  "{0} adresi tüm portlara İZİNLİ olacak (öncelikli erişim).": "{0} will be ALLOWED on all ports (priority access).",
  "SSH (22) açık kaldığı için kilitlenirseniz sunucuya SSH ile girip bu kuralı silebilirsiniz — ya da sabit (statik) bir IP kullanın.": "Since SSH (22) stays open, if you lock yourself out you can SSH into the server and delete this rule — or use a fixed (static) IP.",
  "Dikkat:": "Warning:",
  "Port {0} kritik bir servise ait — güvenlik için kapatılamaz.": "Port {0} belongs to a critical service — it cannot be closed for security reasons.",
  "Port {0} kritik portlar listesinde — bu port kapatılamaz. Başka bir port girin.": "Port {0} is on the protected-ports list — it cannot be closed. Enter a different port.",
  "Bu port artık yalnızca yukarıdaki IP'ye açılacak. IP'niz": "This port will now open only to the IP above. If your IP is",
  "dinamikse": "dynamic",
  "(ev/mobil internet gibi değişebilen), IP değişince bu porta erişimi kaybedersiniz.": "(such as home/mobile internet that can change), you will lose access to this port when the IP changes.",
  "Uygula": "Apply",
  "1 · Ne yapmak istiyorsun?": "1 · What do you want to do?",
  "2 · Detaylar": "2 · Details",
  "Protokol": "Protocol",
  "Uygulanıyor…": "Applying…",
  "Aktif Kurallar": "Active Rules",
  "herkes": "everyone",
  "Sil": "Delete",
  "Yasak": "Ban",
  "İzin": "Allow",
  "Kapalı": "Closed",
  "kural": "rules",
  "Hazır Şablonlar": "Ready Templates",
  "İnternet": "Internet",
  "Sunucu": "Server",
  "Emin misiniz?": "Are you sure?",
  "\"{0}\" şablonu uygulansın mı?\nKapatılacak port(lar): {1}\nBu portlara internetten erişim engellenir.": "Apply the \"{0}\" template?\nPort(s) to close: {1}\nInternet access to these ports will be blocked.",
  "\"{0}\" uygulandı — {1} kural eklendi.": "\"{0}\" applied — {1} rule(s) added.",
  "\"{0}\" zaten uygulanmış (yeni kural yok).": "\"{0}\" already applied (no new rules).",
  "port {0} kapatma": "close port {0}",
  "\"{0}\" kuralı silinsin mi?": "Delete the \"{0}\" rule?",
  "Kural eklenemedi": "Failed to add rule",
  "Silinemedi": "Failed to delete",
  "(boş = tümü)": "(empty = all)",
  "Belirli bir IP adresini engelle. Port yazarsan sadece o porta, boş bırakırsan TÜM portlara erişimi kesilir.": "Block a specific IP address. If you enter a port, only that port; if left empty, access to ALL ports is cut.",
  "Bir portu HERKESE kapat (beyaz listedekiler hariç). Kritik portlar (SSH/web/panel/DNS) korunur; kapatılamaz.": "Close a port to EVERYONE (except allowlisted). Critical ports (SSH/web/panel/DNS) are protected; cannot be closed.",
  "FTP portunu (21) kapatır. SFTP kullanıyorsanız FTP güvenle kapatılabilir.": "Closes the FTP port (21). If you use SFTP, FTP can be safely closed.",
  "Henüz kural yok — sunucu tüm bağlantılara açık.": "No rules yet — the server is open to all connections.",
  "IP adresi veya aralığı": "IP address or range",
  "Kendi Kuralın": "Your Own Rule",
  "Kural eklendi ve firewall'a uygulandı.": "Rule added and applied to the firewall.",
  "Kuralı Ekle ve Uygula": "Add and Apply Rule",
  "Mail Portlarını Kapat": "Close Mail Ports",
  "MySQL'i Dışa Kapat": "Close MySQL Externally",
  "Not (isteğe bağlı)": "Note (optional)",
  "Port yazarsan o port SADECE bu IP(ler)e açılır — diğer herkes engellenir (allowlist). Portu boş bırakırsan bu IP tüm portlara öncelikli erişir (yasaklardan önce değerlendirilir).": "If you enter a port, that port opens ONLY to this IP(s) — everyone else is blocked (allowlist). If you leave the port empty, this IP gets priority access to all ports (evaluated before bans).",
  "SMTP/POP3/IMAP portlarını kapatır. Mail sunucusu yoksa spam-relay riskini azaltır.": "Closes the SMTP/POP3/IMAP ports. If there is no mail server, reduces spam-relay risk.",
  "Veritabanı portunu (3306) internete kapatır. MySQL yalnız sunucu içinden erişilir.": "Closes the database port (3306) to the internet. MySQL is accessible only from within the server.",
  "Yukarıdan bir şablon uygulayarak başlayabilirsiniz.": "You can start by applying a template above.",
  "rpcbind (111) ve NFS (2049) portlarını kapatır. Dosya paylaşımı kullanmıyorsanız kapatın.": "Closes the rpcbind (111) and NFS (2049) ports. Close them if you don't use file sharing.",
  "tek tıkla uygula": "apply with one click",
  "tüm portlara": "to all ports",
  "tümü": "all",
  "Önizleme:": "Preview:",
  "Örnek: Port 8443 yazıp ofis IP'nizi girin → panele yalnız siz erişebilirsiniz.": "Example: Enter port 8443 and your office IP → only you can access the panel.",
  "Örnek: Sürekli SSH deneyen 45.9.1.2 adresini tamamen engelle.": "Example: Fully block 45.9.1.2 that keeps trying SSH.",
  "Örnek: Veritabanı portu 3306'yı dışarıya kapat.": "Example: Close database port 3306 to the outside.",
  "ör. SSH brute-force yapan IP": "e.g. IP doing SSH brute-force",
  "örn. 22": "e.g. 22",
  "İzin Ver": "Allow",
  "Şablon uygulanamadı": "Failed to apply template",
  "Kaydedildi": "Saved",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (FW_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function FirewallPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const toast = useToast()
  const [kurallar, setKurallar] = useState<Kural[]>([])
  const [korumali, setKorumali] = useState<number[]>([])
  const [yuk, setYuk] = useState(true)
  // Hata/başarı artık sağ üst toast ile gösteriliyor; state yalnız akış için tutuluyor.
  const [, setHata] = useState<string | null>(null)
  const [, setBasari] = useState<string | null>(null)
  const [mesgul, setMesgul] = useState<string | null>(null)

  const [tip, setTip] = useState<'ban' | 'whitelist' | 'kapat'>('ban')
  const [ip, setIp] = useState('')
  const [port, setPort] = useState('')
  const [protokol, setProtokol] = useState<'tcp' | 'udp'>('tcp')
  const [aciklama, setAciklama] = useState('')

  function yukle() {
    setYuk(true)
    api.get<ListeResp>('/firewall')
      .then(r => { setKurallar(r.data.kurallar || []); setKorumali(r.data.korumali_portlar || []) })
      .catch(e => { const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  async function sablonUygula(s: typeof SABLONLAR[number]) {
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevirT(cevir("\"{0}\" şablonu uygulansın mı?\nKapatılacak port(lar): {1}\nBu portlara internetten erişim engellenir."), cevir(s.ad), s.portlar), tehlike: true }))) return
    setHata(null); setBasari(null); setMesgul('sablon:' + s.key)
    try {
      const { data } = await api.post('/firewall/sablon', { sablon: s.key })
      const m = data.eklenen > 0 ? cevirT(cevir("\"{0}\" uygulandı — {1} kural eklendi."), cevir(s.ad), data.eklenen) : cevirT(cevir("\"{0}\" zaten uygulanmış (yeni kural yok)."), cevir(s.ad))
      setBasari(m)
      toast.basari(cevir("Kaydedildi"), m)
      yukle()
    } catch (err) {
      const m = apiHata(err, cevir("Şablon uygulanamadı"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setMesgul(null) }
  }

  async function ekle(e: React.FormEvent) {
    e.preventDefault()
    setHata(null); setBasari(null); setMesgul('manuel')
    try {
      await api.post('/firewall', {
        tip, ip: tip === 'kapat' ? '' : ip.trim(),
        port: port.trim() ? parseInt(port, 10) : 0, protokol, aciklama: aciklama.trim(),
      })
      setBasari(cevir("Kural eklendi ve firewall'a uygulandı."))
      toast.basari(cevir("Kural eklendi ve firewall'a uygulandı."))
      setIp(''); setPort(''); setAciklama('')
      yukle()
    } catch (err) {
      const m = apiHata(err, cevir("Kural eklenemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setMesgul(null) }
  }

  async function sil(k: Kural) {
    const ozet = k.tip === 'kapat' ? cevirT(cevir("port {0} kapatma"), k.port) : `${k.ip}${k.port ? ':' + k.port : ''} ${k.tip}`
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevirT(cevir("\"{0}\" kuralı silinsin mi?"), ozet), tehlike: true }))) return
    setHata(null); setBasari(null); setMesgul('sil:' + k.id)
    try { await api.delete(`/firewall/${k.id}`); yukle() }
    catch (err) {
      const m = apiHata(err, cevir("Silinemedi"))
      setHata(m)
      toast.hata(cevir("İşlem başarısız"), m)
    }
    finally { setMesgul(null) }
  }

  const ipGerekli = tip !== 'kapat'
  const mod = MODLAR[tip]
  const korumaliMetin = useMemo(() => korumali.slice().sort((a, b) => a - b).join(', '), [korumali])
  // Girilen port korumali listedeyse backend 400 doner — onizlemede pesinen soyle,
  // ekle dugmesini kapat. (Liste API'den gelir; gelmediyse uyari da cikmaz.)
  const korumaliSecildi = tip === 'kapat' && port.trim() !== '' && korumali.includes(parseInt(port, 10))

  // canlı önizleme cümlesi
  const onizleme = useMemo(() => {
    if (tip === 'kapat') {
      if (!port) return cevir("Kapatılacak portu girin.")
      if (korumali.includes(parseInt(port, 10))) return cevirT(cevir("Port {0} kritik bir servise ait — güvenlik için kapatılamaz."), port)
      return cevirT(cevir("Port {0} HERKESE kapatılacak (beyaz listedekiler hariç)."), port)
    }
    const kim = ip.trim() || '(IP girin)'
    if (tip === 'ban') {
      const hedef = port ? `port ${port}'a` : cevir("tüm portlara")
      return cevirT(cevir("{0} adresinin {1} erişimi ENGELLENECEK."), kim, hedef)
    }
    // whitelist
    if (port) return cevirT(cevir("Port {0} yalnızca {1} adresine açık olacak — diğer herkes ENGELLENİR (allowlist)."), port, kim)
    return cevirT(cevir("{0} adresi tüm portlara İZİNLİ olacak (öncelikli erişim)."), kim)
  }, [tip, ip, port, korumali])

  // whitelist + port → allowlist kısıt: dinamik IP uyarısı
  const kisitUyari = tip === 'whitelist' && port.trim() !== ''

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-3xl mx-auto">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Güvenlik Duvarı") }]} />
      {/* Başlık — kalkan (gerçek SVG) + ad + tek satır açıklama */}
      <div className="mb-4 flex items-start gap-2.5">
        <span className="mt-0.5 text-slate-700 dark:text-slate-200"><FwIkon ad="shield" className="h-6 w-6" /></span>
        <div className="min-w-0">
          <h1 className="text-xl font-semibold leading-tight text-slate-900 dark:text-slate-100">{cevir("Güvenlik Duvarı")}</h1>
          <p className="text-xs text-slate-500 dark:text-slate-400">
            {cevir("Sunucunuza")} <strong className="font-medium">{cevir("internetten kimin erişebileceğini")}</strong> {cevir("kontrol edin. Hazır bir şablon uygulayın veya kendi kuralınızı ekleyin.")}
          </p>
        </div>
      </div>

      {/* ── SİGNATÜR: trafik geçidi — İnternet → [kalkan · N kural] → Sunucu ── */}
      <div className="mb-3 flex items-center gap-3 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-gradient-to-r from-slate-50 via-white to-slate-50 dark:from-slate-800/50 dark:via-slate-800/20 dark:to-slate-800/50 px-4 py-2.5">
        <span className="inline-flex items-center gap-1.5 text-slate-500 dark:text-slate-400 shrink-0"><FwIkon ad="globe" className="h-5 w-5" /><span className="hidden sm:inline text-xs font-medium">İnternet</span></span>
        <span className="flex flex-1 items-center gap-2 min-w-0">
          <span className="h-px flex-1 bg-slate-300 dark:bg-slate-600" />
          <span className="inline-flex items-center gap-1.5 rounded-full border border-brand-500/40 bg-white dark:bg-dark-700 px-2.5 py-1 text-[11px] font-semibold text-slate-700 dark:text-slate-200 shrink-0">
            <FwIkon ad="shield" className="h-3.5 w-3.5 text-brand-500" />{yuk ? '…' : kurallar.length} {cevir("kural")}
          </span>
          <span className="h-px flex-1 bg-slate-300 dark:bg-slate-600" />
        </span>
        <span className="inline-flex items-center gap-1.5 text-slate-500 dark:text-slate-400 shrink-0"><span className="hidden sm:inline text-xs font-medium">Sunucu</span><FwIkon ad="server" className="h-5 w-5" /></span>
      </div>

      {/* korumalı portlar — tek ince satır (kutu değil) */}
      <p className="mb-5 text-[11px] leading-relaxed text-slate-400 dark:text-slate-500">
        {cevir("Kurallar yalnızca")} <strong className="font-medium text-slate-500 dark:text-slate-400">{cevir("yeni bağlantıları")}</strong> {cevir("etkiler — açık oturumunuz (SSH/panel) kopmaz. Kritik portlar")} <span className="font-mono text-slate-500 dark:text-slate-400">{korumaliMetin || '22, 53, 80, 443, 8080, 8443'}</span> {cevir("güvenlik için kapatılamaz.")}
      </p>

      {/* ── HAZIR ŞABLONLAR — kompakt çipler (büyük kart değil) ── */}
      <div className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-slate-400">{cevir("Hazır Şablonlar")} <span className="normal-case font-normal">· {cevir("tek tıkla uygula")}</span></div>
      <div className="mb-6 grid grid-cols-1 sm:grid-cols-2 gap-2">
        {SABLONLAR.map(s => (
          <button key={s.key} onClick={() => sablonUygula(s)} disabled={!!mesgul} title={cevir(s.aciklama)}
            className="group flex items-center gap-2.5 rounded-lg border border-slate-200 dark:border-dark-600/60 bg-white dark:bg-dark-700/50 px-3 py-2.5 text-left transition hover:border-slate-300 dark:hover:border-slate-600 hover:bg-slate-50 dark:hover:bg-dark-700 disabled:opacity-50">
            <FwIkon ad={mesgul === 'sablon:' + s.key ? 'shield' : s.ikon} className={`h-5 w-5 shrink-0 ${mesgul === 'sablon:' + s.key ? 'animate-pulse text-brand-500' : 'text-slate-500 dark:text-slate-400 group-hover:text-brand-500'}`} />
            <span className="min-w-0">
              <span className="block truncate text-[13px] font-medium text-slate-800 dark:text-slate-100">{cevir(s.ad)}</span>
              <span className="block truncate font-mono text-[11px] text-slate-400">:{s.portlar}</span>
            </span>
          </button>
        ))}
      </div>

      {/* ---------- MANUEL KURAL ---------- */}
      <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-2 flex items-center gap-1.5"><Ikon d={I.kalem} />{cevir("Kendi Kuralın")}</h2>
      <form onSubmit={ekle} className="bg-white dark:bg-dark-700/60 border border-slate-200 dark:border-dark-600/60 rounded-lg p-4 mb-6">
        {/* 1) ne yapmak istiyorsun */}
        <div className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-2">{cevir("1 · Ne yapmak istiyorsun?")}</div>
        <div className="grid grid-cols-3 gap-2 mb-3">
          {(['ban', 'whitelist', 'kapat'] as const).map(t => (
            <button key={t} type="button" onClick={() => setTip(t)}
              className={`flex items-center justify-center gap-2 px-2 py-2 text-[13px] font-medium rounded-lg border transition ${
                tip === t ? RENK[TIP_RENK[t]].seg + ' text-white'
                  : 'bg-white dark:bg-dark-700 border-slate-200 dark:border-dark-600 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-dark-600'
              }`}>
              <FwIkon ad={MODLAR[t].ikon} className="h-4 w-4 shrink-0" />
              {cevir(MODLAR[t].ad)}
            </button>
          ))}
        </div>
        {/* seçili modun açıklaması */}
        <div className="mb-4 px-3 py-2 rounded-lg bg-slate-50 dark:bg-dark-800/40 text-xs text-slate-600 dark:text-slate-300">
          {cevir(mod.aciklama)}<br /><span className="text-slate-400">{cevir(mod.ornek)}</span>
        </div>

        {/* 2) detaylar */}
        <div className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-2">{cevir("2 · Detaylar")}</div>
        <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
          {ipGerekli && (
            <label className="block sm:col-span-2">
              <span className="text-[11px] text-slate-500 dark:text-slate-400">{cevir("IP adresi veya aralığı")}</span>
              <input value={ip} onChange={e => setIp(e.target.value)} required placeholder="1.2.3.4  ·  1.2.3.0/24"
                className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            </label>
          )}
          <label className="block">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">Port {ipGerekli && <span className="text-slate-400">{cevir("(boş = tümü)")}</span>}</span>
            <input value={port} onChange={e => setPort(e.target.value.replace(/[^0-9]/g, ''))} required={tip === 'kapat'} placeholder={tip === 'kapat' ? '3306' : cevir("örn. 22")}
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          </label>
          <label className="block">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">{cevir("Protokol")}</span>
            <select value={protokol} onChange={e => setProtokol(e.target.value as 'tcp' | 'udp')}
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none">
              <option value="tcp">TCP</option><option value="udp">UDP</option>
            </select>
          </label>
          <label className="block sm:col-span-4">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">{cevir("Not (isteğe bağlı)")}</span>
            <input value={aciklama} onChange={e => setAciklama(e.target.value)} placeholder={cevir("ör. SSH brute-force yapan IP")}
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-dark-800 rounded-lg text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          </label>
        </div>

        {/* canlı önizleme */}
        <div className="mt-3 flex items-center gap-2 px-3 py-2 rounded-lg bg-slate-100 dark:bg-dark-800/60 text-xs">
          <span className="text-slate-400">{cevir("Önizleme:")}</span>
          <span className="font-medium text-slate-700 dark:text-slate-200">{onizleme}</span>
        </div>

        {/* korumalı port — kural reddedilir; ekleme kapalı */}
        {korumaliSecildi && (
          <div className="mt-2 px-3 py-2 rounded-lg bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-xs text-red-800 dark:text-red-200">
            <strong>{cevir("Dikkat:")}</strong> {cevirT(cevir("Port {0} kritik portlar listesinde — bu port kapatılamaz. Başka bir port girin."), port)}
          </div>
        )}

        {/* dinamik IP uyarısı — allowlist kısıt aktifken */}
        {kisitUyari && (
          <div className="mt-2 px-3 py-2 rounded-lg bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 text-xs text-amber-800 dark:text-amber-200">
            <strong>{cevir("Dikkat:")}</strong> {cevir("Bu port artık yalnızca yukarıdaki IP'ye açılacak. IP'niz")} <strong>{cevir("dinamikse")}</strong> {cevir("(ev/mobil internet gibi değişebilen), IP değişince bu porta erişimi kaybedersiniz.")}
            {cevir("SSH (22) açık kaldığı için kilitlenirseniz sunucuya SSH ile girip bu kuralı silebilirsiniz — ya da sabit (statik) bir IP kullanın.")}
          </div>
        )}

        <Button type="submit" color="primary" disabled={mesgul === 'manuel' || korumaliSecildi} className="mt-3 text-sm">
          {mesgul === 'manuel' ? cevir("Uygulanıyor…") : cevir("Kuralı Ekle ve Uygula")}
        </Button>
      </form>

      {/* ---------- AKTİF KURALLAR ---------- */}
      {/* Kapsayıcı çerçeve yalnız masaüstünde: mobilde satırlar zaten kart, ikinci çerçeve iç içe görünürdü. */}
      <div className="lg:bg-white dark:lg:bg-dark-700/60 lg:border lg:border-slate-200 dark:lg:border-dark-600/60 lg:rounded-lg lg:overflow-hidden">
        <div className="flex items-center justify-between px-0 lg:px-4 py-3 border-b border-slate-100 dark:border-dark-600/60">
          <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Aktif Kurallar")} {!yuk && <span className="text-slate-400 font-normal">· {kurallar.length}</span>}</h3>
          <Button variant="outlined" onClick={yukle} disabled={yuk} className="text-xs px-2.5 py-1"><span className="inline-flex items-center gap-1.5"><Ikon d={I.yenile} className="h-3.5 w-3.5" />{cevir("Yenile")}</span></Button>
        </div>
        {/* Mobilde yatay kaydırma yok — satırlar kart olarak diziliyor. */}
        <div className="lg:overflow-x-auto">
          <table className={`${T.tablo} text-sm`}>
            <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-dark-800/50 text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-dark-600/60`}>
              <tr>
                <th className={T.baslik}>{cevir("Tür")}</th>
                <th className={T.baslik}>IP / CIDR</th>
                <th className={T.baslik}>Port</th>
                <th className={T.baslik}>Proto</th>
                <th className={`${T.baslik} w-full`}>{cevir("Not")}</th>
                <th className={`${T.baslik} text-right`}>{cevir("İşlem")}</th>
              </tr>
            </thead>
            <tbody className={`${T.govde} lg:divide-y lg:divide-slate-100 dark:lg:divide-dark-600/60`}>
              {yuk ? (
                <tr className={T.satir}><td colSpan={6} className={T.hucreDurum}>{cevir("Yükleniyor…")}</td></tr>
              ) : kurallar.length === 0 ? (
                <tr className={T.satir}><td colSpan={6} className={T.hucreDurum}>
                  <div className="text-2xl mb-1"><Ikon d={I.kalkan} className="h-6 w-6 mx-auto" /></div>
                  <p className="text-sm text-slate-500 dark:text-slate-400">{cevir("Henüz kural yok — sunucu tüm bağlantılara açık.")}</p>
                  <p className="text-xs text-slate-400 mt-1">{cevir("Yukarıdan bir şablon uygulayarak başlayabilirsiniz.")}</p>
                </td></tr>
              ) : (
                kurallar.map(k => (
                  <tr key={k.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-dark-700/40`}>
                    <td className={T.hucre} data-etiket={cevir("Tür")}><TurRozet tip={k.tip} /></td>
                    {/* Birincil tanımlayıcı: IP / CIDR — mobilde kart başlığı olur.
                        {cevir(cevir("Kolon sırası masaüstündeki"))} <th> sırasıyla birebir aynı kalıyor. */}
                    <td className={`${T.hucreBaslik} font-mono lg:font-normal lg:text-xs lg:text-slate-700 dark:lg:text-slate-200`}>
                      {k.ip || <span className="text-slate-400">{cevir("herkes")}</span>}
                    </td>
                    <td className={T.hucre} data-etiket="Port">
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-300">{k.port || <span className="text-slate-400">{cevir("tümü")}</span>}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Proto">
                      <span className="font-mono text-[11px] text-slate-500 uppercase">{k.protokol}</span>
                    </td>
                    <td className={T.hucre} data-etiket={cevir("Not")}>
                      <span className="text-xs text-slate-500 dark:text-slate-400 text-right lg:text-left break-words">{k.aciklama || '—'}</span>
                    </td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <Button color="error" variant="outlined" disabled={!!mesgul} onClick={() => sil(k)} className="text-xs px-2.5 py-1">{mesgul === 'sil:' + k.id ? '…' : cevir("Sil")}</Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )
}

function TurRozet({ tip }: { tip: Kural['tip'] }) {
  const m = {
    ban: { ikon: 'ban', ad: cevir("Yasak"), cls: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300' },
    whitelist: { ikon: 'allow', ad: cevir("İzin"), cls: 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300' },
    kapat: { ikon: 'lock', ad: cevir("Kapalı"), cls: 'bg-amber-100 dark:bg-amber-900/40 text-amber-800 dark:text-amber-200' },
  }[tip]
  return <span className={`inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full font-medium ${m.cls}`}><FwIkon ad={m.ikon} className="h-3.5 w-3.5" />{m.ad}</span>
}
