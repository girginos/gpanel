import { useEffect, useMemo, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { T } from '@/lib/tablo'

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

export default function FirewallPage() {
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
    if (!confirm(`"${s.ad}" şablonu uygulansın mı?\nKapatılacak port(lar): ${s.portlar}\nBu portlara internetten erişim engellenir.`)) return
    setHata(null); setBasari(null); setMesgul('sablon:' + s.key)
    try {
      const { data } = await api.post('/firewall/sablon', { sablon: s.key })
      setBasari(data.eklenen > 0 ? `"${s.ad}" uygulandı — ${data.eklenen} kural eklendi.` : `"${s.ad}" zaten uygulanmış (yeni kural yok).`)
      yukle()
    } catch (err) { setHata(apiHata(err, 'Şablon uygulanamadı')) }
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
      setBasari("Kural eklendi ve firewall'a uygulandı.")
      setIp(''); setPort(''); setAciklama('')
      yukle()
    } catch (err) { setHata(apiHata(err, 'Kural eklenemedi')) }
    finally { setMesgul(null) }
  }

  async function sil(k: Kural) {
    const ozet = k.tip === 'kapat' ? `port ${k.port} kapatma` : `${k.ip}${k.port ? ':' + k.port : ''} ${k.tip}`
    if (!confirm(`"${ozet}" kuralı silinsin mi?`)) return
    setHata(null); setBasari(null); setMesgul('sil:' + k.id)
    try { await api.delete(`/firewall/${k.id}`); yukle() }
    catch (err) { setHata(apiHata(err, 'Silinemedi')) }
    finally { setMesgul(null) }
  }

  const ipGerekli = tip !== 'kapat'
  const mod = MODLAR[tip]
  const korumaliMetin = useMemo(() => korumali.slice().sort((a, b) => a - b).join(', '), [korumali])

  // canlı önizleme cümlesi
  const onizleme = useMemo(() => {
    if (tip === 'kapat') return port ? `Port ${port} HERKESE kapatılacak (beyaz listedekiler hariç).` : 'Kapatılacak portu girin.'
    const kim = ip.trim() || '(IP girin)'
    if (tip === 'ban') {
      const hedef = port ? `port ${port}'a` : 'tüm portlara'
      return `${kim} adresinin ${hedef} erişimi ENGELLENECEK.`
    }
    // whitelist
    if (port) return `Port ${port} yalnızca ${kim} adresine açık olacak — diğer herkes ENGELLENİR (allowlist).`
    return `${kim} adresi tüm portlara İZİNLİ olacak (öncelikli erişim).`
  }, [tip, ip, port])

  // whitelist + port → allowlist kısıt: dinamik IP uyarısı
  const kisitUyari = tip === 'whitelist' && port.trim() !== ''

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Güvenlik Duvarı' }]} />
      <div className="flex items-center gap-3 mb-1">
        <span className="text-2xl">🛡️</span>
        <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">Güvenlik Duvarı</h1>
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
        Sunucunuza <strong>internetten kimin erişebileceğini</strong> kontrol edin. Hazır bir şablon uygulayın veya kendi kuralınızı ekleyin.
      </p>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      <div className="mb-5 px-4 py-2.5 rounded-lg bg-sky-50 dark:bg-sky-900/20 border border-sky-200 dark:border-sky-800 text-xs text-sky-800 dark:text-sky-200">
        ℹ️ Kurallar yalnızca <strong>yeni bağlantıları</strong> etkiler — açık oturumunuz (SSH/panel) kopmaz. Kritik portlar <span className="font-mono">{korumaliMetin || '22, 53, 80, 443, 8080, 8443'}</span> güvenlik için kapatılamaz.
      </div>

      {/* ---------- HAZIR ŞABLONLAR ---------- */}
      <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-2 flex items-center gap-2">⚡ Hazır Şablonlar <span className="text-xs font-normal text-slate-400">tek tıkla uygula</span></h2>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-6">
        {SABLONLAR.map(s => (
          <div key={s.key} className="flex items-start gap-3 p-4 rounded-2xl border border-slate-200 dark:border-slate-700/60 bg-white dark:bg-slate-800/60">
            <div className="w-10 h-10 rounded-lg bg-slate-100 dark:bg-slate-700 flex items-center justify-center text-xl shrink-0">{s.ikon}</div>
            <div className="flex-1 min-w-0">
              <div className="text-sm font-semibold text-slate-800 dark:text-slate-100">{s.ad}</div>
              <div className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">{s.aciklama}</div>
              <div className="text-[11px] font-mono text-slate-400 mt-1">Port: {s.portlar}</div>
            </div>
            <button onClick={() => sablonUygula(s)} disabled={!!mesgul}
              className="shrink-0 self-center px-3 py-1.5 text-xs font-medium bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 rounded-lg disabled:opacity-50">
              {mesgul === 'sablon:' + s.key ? '…' : 'Uygula'}
            </button>
          </div>
        ))}
      </div>

      {/* ---------- MANUEL KURAL ---------- */}
      <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200 mb-2">✍️ Kendi Kuralın</h2>
      <form onSubmit={ekle} className="bg-white dark:bg-slate-800/60 border border-slate-200 dark:border-slate-700/60 rounded-2xl p-4 mb-6">
        {/* 1) ne yapmak istiyorsun */}
        <div className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-2">1 · Ne yapmak istiyorsun?</div>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2 mb-3">
          {(['ban', 'whitelist', 'kapat'] as const).map(t => (
            <button key={t} type="button" onClick={() => setTip(t)}
              className={`px-3 py-3 text-sm font-medium rounded-lg border text-center transition ${
                tip === t ? MODLAR[t].aktifRenk + ' text-white'
                  : 'bg-white dark:bg-slate-800 border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700'
              }`}>
              <div className="text-lg leading-none mb-1">{MODLAR[t].ikon}</div>
              {MODLAR[t].ad}
            </button>
          ))}
        </div>
        {/* seçili modun açıklaması */}
        <div className="mb-4 px-3 py-2 rounded-lg bg-slate-50 dark:bg-slate-900/40 text-xs text-slate-600 dark:text-slate-300">
          {mod.aciklama}<br /><span className="text-slate-400">{mod.ornek}</span>
        </div>

        {/* 2) detaylar */}
        <div className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-2">2 · Detaylar</div>
        <div className="grid grid-cols-1 sm:grid-cols-4 gap-3">
          {ipGerekli && (
            <label className="block sm:col-span-2">
              <span className="text-[11px] text-slate-500 dark:text-slate-400">IP adresi veya aralığı</span>
              <input value={ip} onChange={e => setIp(e.target.value)} required placeholder="1.2.3.4  ·  1.2.3.0/24"
                className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            </label>
          )}
          <label className="block">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">Port {ipGerekli && <span className="text-slate-400">(boş = tümü)</span>}</span>
            <input value={port} onChange={e => setPort(e.target.value.replace(/[^0-9]/g, ''))} required={tip === 'kapat'} placeholder={tip === 'kapat' ? '3306' : 'örn. 22'}
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          </label>
          <label className="block">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">Protokol</span>
            <select value={protokol} onChange={e => setProtokol(e.target.value as 'tcp' | 'udp')}
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none">
              <option value="tcp">TCP</option><option value="udp">UDP</option>
            </select>
          </label>
          <label className="block sm:col-span-4">
            <span className="text-[11px] text-slate-500 dark:text-slate-400">Not (isteğe bağlı)</span>
            <input value={aciklama} onChange={e => setAciklama(e.target.value)} placeholder="ör. SSH brute-force yapan IP"
              className="mt-1 w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-sm focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          </label>
        </div>

        {/* canlı önizleme */}
        <div className="mt-3 flex items-center gap-2 px-3 py-2 rounded-lg bg-slate-100 dark:bg-slate-900/60 text-xs">
          <span className="text-slate-400">Önizleme:</span>
          <span className="font-medium text-slate-700 dark:text-slate-200">{onizleme}</span>
        </div>

        {/* dinamik IP uyarısı — allowlist kısıt aktifken */}
        {kisitUyari && (
          <div className="mt-2 px-3 py-2 rounded-lg bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 text-xs text-amber-800 dark:text-amber-200">
            ⚠️ <strong>Dikkat:</strong> Bu port artık yalnızca yukarıdaki IP'ye açılacak. IP'niz <strong>dinamikse</strong> (ev/mobil internet gibi değişebilen), IP değişince bu porta erişimi kaybedersiniz.
            SSH (22) açık kaldığı için kilitlenirseniz sunucuya SSH ile girip bu kuralı silebilirsiniz — ya da sabit (statik) bir IP kullanın.
          </div>
        )}

        <button disabled={mesgul === 'manuel'} className="mt-3 px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-lg disabled:opacity-50">
          {mesgul === 'manuel' ? 'Uygulanıyor…' : 'Kuralı Ekle ve Uygula'}
        </button>
      </form>

      {/* ---------- AKTİF KURALLAR ---------- */}
      {/* Kapsayıcı çerçeve yalnız masaüstünde: mobilde satırlar zaten kart, ikinci çerçeve iç içe görünürdü. */}
      <div className="lg:bg-white dark:lg:bg-slate-800/60 lg:border lg:border-slate-200 dark:lg:border-slate-700/60 lg:rounded-2xl lg:overflow-hidden">
        <div className="flex items-center justify-between px-0 lg:px-4 py-3 border-b border-slate-100 dark:border-slate-700/60">
          <h3 className="text-sm font-semibold text-slate-700 dark:text-slate-200">Aktif Kurallar {!yuk && <span className="text-slate-400 font-normal">· {kurallar.length}</span>}</h3>
          <button onClick={yukle} disabled={yuk} className="text-xs px-2.5 py-1 border border-slate-200 dark:border-slate-700 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700 disabled:opacity-50">↻ Yenile</button>
        </div>
        {/* Mobilde yatay kaydırma yok — satırlar kart olarak diziliyor. */}
        <div className="lg:overflow-x-auto">
          <table className={`${T.tablo} text-sm`}>
            <thead className={`${T.baslikGrubu} bg-slate-50 dark:bg-slate-900/50 text-slate-500 dark:text-slate-400 border-b border-slate-200 dark:border-slate-700/60`}>
              <tr>
                <th className={T.baslik}>Tür</th>
                <th className={T.baslik}>IP / CIDR</th>
                <th className={T.baslik}>Port</th>
                <th className={T.baslik}>Proto</th>
                <th className={`${T.baslik} w-full`}>Not</th>
                <th className={`${T.baslik} text-right`}>İşlem</th>
              </tr>
            </thead>
            <tbody className={`${T.govde} lg:divide-y lg:divide-slate-100 dark:lg:divide-slate-700/60`}>
              {yuk ? (
                <tr className={T.satir}><td colSpan={6} className={T.hucreDurum}>Yükleniyor…</td></tr>
              ) : kurallar.length === 0 ? (
                <tr className={T.satir}><td colSpan={6} className={T.hucreDurum}>
                  <div className="text-2xl mb-1">🛡️</div>
                  <p className="text-sm text-slate-500 dark:text-slate-400">Henüz kural yok — sunucu tüm bağlantılara açık.</p>
                  <p className="text-xs text-slate-400 mt-1">Yukarıdan bir şablon uygulayarak başlayabilirsiniz.</p>
                </td></tr>
              ) : (
                kurallar.map(k => (
                  <tr key={k.id} className={`${T.satir} lg:hover:bg-slate-50 dark:lg:hover:bg-slate-800/40`}>
                    <td className={T.hucre} data-etiket="Tür"><TurRozet tip={k.tip} /></td>
                    {/* Birincil tanımlayıcı: IP / CIDR — mobilde kart başlığı olur.
                        Kolon sırası masaüstündeki <th> sırasıyla birebir aynı kalıyor. */}
                    <td className={`${T.hucreBaslik} font-mono lg:font-normal lg:text-xs lg:text-slate-700 dark:lg:text-slate-200`}>
                      {k.ip || <span className="text-slate-400">herkes</span>}
                    </td>
                    <td className={T.hucre} data-etiket="Port">
                      <span className="font-mono text-xs text-slate-600 dark:text-slate-300">{k.port || <span className="text-slate-400">tümü</span>}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Proto">
                      <span className="font-mono text-[11px] text-slate-500 uppercase">{k.protokol}</span>
                    </td>
                    <td className={T.hucre} data-etiket="Not">
                      <span className="text-xs text-slate-500 dark:text-slate-400 text-right lg:text-left break-words">{k.aciklama || '—'}</span>
                    </td>
                    <td className={`${T.hucreAksiyon} lg:text-right`}>
                      <button disabled={!!mesgul} onClick={() => sil(k)} className="text-xs px-2.5 py-1 border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 rounded-md hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50">{mesgul === 'sil:' + k.id ? '…' : 'Sil'}</button>
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
    ban: ['🚫 Yasak', 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300'],
    whitelist: ['✅ İzin', 'bg-emerald-100 dark:bg-emerald-900/40 text-emerald-700 dark:text-emerald-300'],
    kapat: ['🔒 Kapalı', 'bg-amber-100 dark:bg-amber-900/40 text-amber-800 dark:text-amber-200'],
  }[tip]
  return <span className={`inline-block text-xs px-2 py-0.5 rounded-full font-medium ${m[1]}`}>{m[0]}</span>
}
