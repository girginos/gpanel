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

type Kural = {
  id: number; tip: 'ban' | 'whitelist' | 'kapat'; ip: string; port: number
  protokol: string; aciklama: string; aktif: boolean; created_at: string
}
type ListeResp = { kurallar: Kural[]; korumali_portlar: number[] }

// Hazır şablonlar — tek tıkla yaygın açık portları kapat
const SABLONLAR = [
  { key: 'mysql_kapat', ikon: '🗄️', ad: "MySQL'i Dışa Kapat", portlar: '3306',
    aciklama: 'Veritabanı portunu (3306) internete kapatır. MySQL yalnız sunucu içinden erişilir.' },
  { key: 'ftp_kapat', ikon: '📁', ad: "FTP'yi Kapat", portlar: '21',
    aciklama: 'FTP portunu (21) kapatır. SFTP kullanıyorsanız FTP güvenle kapatılabilir.' },
  { key: 'mail_kapat', ikon: '📧', ad: 'Mail Portlarını Kapat', portlar: '25, 465, 587, 110, 143',
    aciklama: 'SMTP/POP3/IMAP portlarını kapatır. Mail sunucusu yoksa spam-relay riskini azaltır.' },
  { key: 'rpc_kapat', ikon: '🔗', ad: 'RPC / NFS Kapat', portlar: '111, 2049',
    aciklama: 'rpcbind (111) ve NFS (2049) portlarını kapatır. Dosya paylaşımı kullanmıyorsanız kapatın.' },
] as const

// Manuel kural modları — açıklama + örnek
const MODLAR = {
  ban: { ikon: '🚫', ad: 'IP Yasakla', aktifRenk: 'bg-red-600 border-red-600',
    aciklama: 'Belirli bir IP adresini engelle. Port yazarsan sadece o porta, boş bırakırsan TÜM portlara erişimi kesilir.',
    ornek: 'Örnek: Sürekli SSH deneyen 45.9.1.2 adresini tamamen engelle.' },
  whitelist: { ikon: '✅', ad: 'İzin Ver', aktifRenk: 'bg-emerald-600 border-emerald-600',
    aciklama: 'Port yazarsan o port SADECE bu IP(ler)e açılır — diğer herkes engellenir (allowlist). Portu boş bırakırsan bu IP tüm portlara öncelikli erişir (yasaklardan önce değerlendirilir).',
    ornek: "Örnek: Port 8443 yazıp ofis IP'nizi girin → panele yalnız siz erişebilirsiniz." },
  kapat: { ikon: '🔒', ad: 'Port Kapat', aktifRenk: 'bg-amber-600 border-amber-600',
    aciklama: 'Bir portu HERKESE kapat (beyaz listedekiler hariç). Kritik portlar (SSH/web/panel/DNS) korunur; kapatılamaz.',
    ornek: "Örnek: Veritabanı portu 3306'yı dışarıya kapat." },
} as const


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
  "🚫 Yasak": "🚫 Ban",
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
  "⚡ Hazır Şablonlar": "⚡ Ready Templates",
  "✅ İzin": "✅ Allow",
  "🔒 Kapalı": "🔒 Closed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (FW_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function FirewallPage() {
  useTranslation() // dil re-render aboneligi
  const { onay } = useDialog()
  const [kurallar, setKurallar] = useState<Kural[]>([])
  const [korumali, setKorumali] = useState<number[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
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
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [])

  async function sablonUygula(s: typeof SABLONLAR[number]) {
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevirT(cevir("\"{0}\" şablonu uygulansın mı?\nKapatılacak port(lar): {1}\nBu portlara internetten erişim engellenir."), cevir(s.ad), s.portlar), tehlike: true }))) return
    setHata(null); setBasari(null); setMesgul('sablon:' + s.key)
    try {
      const { data } = await api.post('/firewall/sablon', { sablon: s.key })
      setBasari(data.eklenen > 0 ? cevirT(cevir("\"{0}\" uygulandı — {1} kural eklendi."), cevir(s.ad), data.eklenen) : cevirT(cevir("\"{0}\" zaten uygulanmış (yeni kural yok)."), cevir(s.ad)))
      yukle()
    } catch (err) { setHata(apiHata(err, cevir("Şablon uygulanamadı"))) }
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
      setIp(''); setPort(''); setAciklama('')
      yukle()
    } catch (err) { setHata(apiHata(err, cevir("Kural eklenemedi"))) }
    finally { setMesgul(null) }
  }

  async function sil(k: Kural) {
    const ozet = k.tip === 'kapat' ? cevirT(cevir("port {0} kapatma"), k.port) : `${k.ip}${k.port ? ':' + k.port : ''} ${k.tip}`
    if (!(await onay({ baslik: cevir("Emin misiniz?"), mesaj: cevirT(cevir("\"{0}\" kuralı silinsin mi?"), ozet), tehlike: true }))) return
    setHata(null); setBasari(null); setMesgul('sil:' + k.id)
    try { await api.delete(`/firewall/${k.id}`); yukle() }
    catch (err) { setHata(apiHata(err, cevir("Silinemedi"))) }
    finally { setMesgul(null) }
  }

  const ipGerekli = tip !== 'kapat'
  const mod = MODLAR[tip]
  const korumaliMetin = useMemo(() => korumali.slice().sort((a, b) => a - b).join(', '), [korumali])

  // canlı önizleme cümlesi
  const onizleme = useMemo(() => {
    if (tip === 'kapat') return port ? cevirT(cevir("Port {0} HERKESE kapatılacak (beyaz listedekiler hariç)."), port) : cevir("Kapatılacak portu girin.")
    const kim = ip.trim() || '(IP girin)'
    if (tip === 'ban') {
      const hedef = port ? `port ${port}'a` : cevir("tüm portlara")
      return cevirT(cevir("{0} adresinin {1} erişimi ENGELLENECEK."), kim, hedef)
    }
    // whitelist
    if (port) return cevirT(cevir("Port {0} yalnızca {1} adresine açık olacak — diğer herkes ENGELLENİR (allowlist)."), port, kim)
    return cevirT(cevir("{0} adresi tüm portlara İZİNLİ olacak (öncelikli erişim)."), kim)
  }, [tip, ip, port])

  // whitelist + port → allowlist kısıt: dinamik IP uyarısı
  const kisitUyari = tip === 'whitelist' && port.trim() !== ''

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: cevir("Anasayfa"), href: '/' }, { etiket: cevir("Güvenlik Duvarı") }]} />
      <div className="flex items-center gap-3 mb-1">
        <span className="text-2xl"><Ikon d={I.kalkan} className="h-6 w-6" /></span>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">{cevir("Güvenlik Duvarı")}</h1>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
        {cevir("Sunucunuza")} <strong>{cevir("internetten kimin erişebileceğini")}</strong> {cevir("kontrol edin. Hazır bir şablon uygulayın veya kendi kuralınızı ekleyin.")}
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      <div className="mb-5 px-4 py-2.5 rounded-lg bg-sky-50 dark:bg-sky-900/20 border border-sky-200 dark:border-sky-800 text-xs text-sky-800 dark:text-sky-200">
        ℹ️ {cevir("Kurallar yalnızca")} <strong>{cevir("yeni bağlantıları")}</strong> {cevir("etkiler — açık oturumunuz (SSH/panel) kopmaz. Kritik portlar")} <span className="font-mono">{korumaliMetin || '22, 53, 80, 443, 8080, 8443'}</span> {cevir("güvenlik için kapatılamaz.")}
      </div>

      {/* ---------- HAZIR ŞABLONLAR ---------- */}
      <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-2 flex items-center gap-2">{cevir("⚡ Hazır Şablonlar")} <span className="text-xs font-normal text-slate-400">{cevir("tek tıkla uygula")}</span></h2>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-6">
        {SABLONLAR.map(s => (
          <div key={s.key} className="flex items-start gap-3 p-4 rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60">
            <div className="w-10 h-10 rounded-lg bg-slate-100 dark:bg-slate-700 flex items-center justify-center text-xl shrink-0">{s.ikon}</div>
            <div className="flex-1 min-w-0">
              <div className="text-sm font-semibold text-slate-800 dark:text-slate-100">{cevir(s.ad)}</div>
              <div className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">{cevir(s.aciklama)}</div>
              <div className="text-[11px] font-mono text-slate-400 mt-1">Port: {s.portlar}</div>
            </div>
            <button onClick={() => sablonUygula(s)} disabled={!!mesgul}
              className="shrink-0 self-center px-3 py-1.5 text-xs font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
              {mesgul === 'sablon:' + s.key ? '…' : cevir("Uygula")}
            </button>
          </div>
        ))}
      </div>

      {/* ---------- MANUEL KURAL ---------- */}
      <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-2 flex items-center gap-1.5"><Ikon d={I.kalem} />{cevir("Kendi Kuralın")}</h2>
      <form onSubmit={ekle} className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl p-4 mb-6">
        {/* 1) ne yapmak istiyorsun */}
        <div className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-2">{cevir("1 · Ne yapmak istiyorsun?")}</div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2 mb-3">
          {(['ban', 'whitelist', 'kapat'] as const).map(t => (
            <button key={t} type="button" onClick={() => setTip(t)}
              className={`px-3 py-3 text-sm font-medium rounded-lg border text-center transition ${
                tip === t ? MODLAR[t].aktifRenk + ' text-white'
                  : 'bg-white dark:bg-slate-800 border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700'
              }`}>
              <div className="text-lg leading-none mb-1">{MODLAR[t].ikon}</div>
              {cevir(MODLAR[t].ad)}
            </button>
          ))}
        </div>
        {/* seçili modun açıklaması */}
        <div className="mb-4 px-3 py-2 rounded-lg bg-slate-50 dark:bg-slate-900/40 text-xs text-slate-600 dark:text-slate-300">
          {cevir(mod.aciklama)}<br /><span className="text-slate-400">{cevir(mod.ornek)}</span>
        </div>

        {/* 2) detaylar */}
        <div className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-2">{cevir("2 · Detaylar")}</div>
        <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
          {ipGerekli && (
            <label className="block sm:col-span-2">
              <span className="text-[11px] text-slate-500 dark:text-slate-400">{cevir("IP adresi veya aralığı")}</span>
              <input value={ip} onChange={e => setIp(e.target.value)} required placeholder="1.2.3.4  ·  1.2.3.0/24"
                className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            </label>
          )}
          <label className="block">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">Port {ipGerekli && <span className="text-slate-400">{cevir("(boş = tümü)")}</span>}</span>
            <input value={port} onChange={e => setPort(e.target.value.replace(/[^0-9]/g, ''))} required={tip === 'kapat'} placeholder={tip === 'kapat' ? '3306' : cevir("örn. 22")}
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          </label>
          <label className="block">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">{cevir("Protokol")}</span>
            <select value={protokol} onChange={e => setProtokol(e.target.value as 'tcp' | 'udp')}
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none">
              <option value="tcp">TCP</option><option value="udp">UDP</option>
            </select>
          </label>
          <label className="block sm:col-span-4">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">{cevir("Not (isteğe bağlı)")}</span>
            <input value={aciklama} onChange={e => setAciklama(e.target.value)} placeholder={cevir("ör. SSH brute-force yapan IP")}
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          </label>
        </div>

        {/* canlı önizleme */}
        <div className="mt-3 flex items-center gap-2 px-3 py-2 rounded-lg bg-slate-100 dark:bg-slate-900/60 text-xs">
          <span className="text-slate-400">{cevir("Önizleme:")}</span>
          <span className="font-medium text-slate-700 dark:text-slate-200">{onizleme}</span>
        </div>

        {/* dinamik IP uyarısı — allowlist kısıt aktifken */}
        {kisitUyari && (
          <div className="mt-2 px-3 py-2 rounded-lg bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 text-xs text-amber-800 dark:text-amber-200">
            ⚠️ <strong>{cevir("Dikkat:")}</strong> {cevir("Bu port artık yalnızca yukarıdaki IP'ye açılacak. IP'niz")} <strong>{cevir("dinamikse")}</strong> {cevir("(ev/mobil internet gibi değişebilen), IP değişince bu porta erişimi kaybedersiniz.")}
            {cevir("SSH (22) açık kaldığı için kilitlenirseniz sunucuya SSH ile girip bu kuralı silebilirsiniz — ya da sabit (statik) bir IP kullanın.")}
          </div>
        )}

        <button disabled={mesgul === 'manuel'} className="mt-3 px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-lg disabled:opacity-50">
          {mesgul === 'manuel' ? cevir("Uygulanıyor…") : cevir("Kuralı Ekle ve Uygula")}
        </button>
      </form>

      {/* ---------- AKTİF KURALLAR ---------- */}
      {/* Kapsayıcı çerçeve yalnız masaüstünde: mobilde satırlar zaten kart, ikinci çerçeve iç içe görünürdü. */}
      <div className="lg:bg-white dark:lg:bg-slate-800/60 lg:border lg:border-slate-200 dark:lg:border-slate-700/60 lg:rounded-2xl lg:overflow-hidden">
        <div className="flex items-center justify-between px-0 lg:px-4 py-3 border-b border-slate-100 dark:border-slate-700/60">
          <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{cevir("Aktif Kurallar")} {!yuk && <span className="text-slate-400 font-normal">· {kurallar.length}</span>}</h3>
          <button onClick={yukle} disabled={yuk} className="text-xs px-2.5 py-1 border border-slate-200 dark:border-slate-700 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50"><span className="inline-flex items-center gap-1.5"><Ikon d={I.yenile} className="h-3.5 w-3.5" />{cevir("Yenile")}</span></button>
        </div>
        {/* Mobilde yatay kaydırma yok — satırlar kart olarak diziliyor. */}
        <div className="lg:overflow-x-auto">
          <table className={`${T.tablo} text-sm`}>
            <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900/50 text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-slate-700/60`}>
              <tr>
                <th className={T.baslik}>{cevir("Tür")}</th>
                <th className={T.baslik}>IP / CIDR</th>
                <th className={T.baslik}>Port</th>
                <th className={T.baslik}>Proto</th>
                <th className={`${T.baslik} w-full`}>{cevir("Not")}</th>
                <th className={`${T.baslik} text-right`}>{cevir("İşlem")}</th>
              </tr>
            </thead>
            <tbody className={`${T.govde} lg:divide-y lg:divide-slate-100 dark:lg:divide-slate-700/60`}>
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
                  <tr key={k.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800/40`}>
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
                      <button disabled={!!mesgul} onClick={() => sil(k)} className="text-xs px-2.5 py-1 border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 rounded-md hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50">{mesgul === 'sil:' + k.id ? '…' : cevir("Sil")}</button>
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
    ban: [cevir("🚫 Yasak"), 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300'],
    whitelist: [cevir("✅ İzin"), 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300'],
    kapat: [cevir("🔒 Kapalı"), 'bg-amber-100 dark:bg-amber-900/40 text-amber-800 dark:text-amber-200'],
  }[tip]
  return <span className={`inline-block text-xs px-2 py-0.5 rounded-full font-medium ${m[1]}`}>{m[0]}</span>
}
