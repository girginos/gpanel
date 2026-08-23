import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import type { FormEvent } from 'react'
import { Ikon, I } from '@/components/Ikon'
import { useCallback, useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

/*
 * Panel Hostname & SSL — admin.
 *
 * İki adım:
 *   1. Hostname Ayarla → nginx server_name'e ekle (girginospanel-panelhost
 *      betiği + DNS check + auto-rollback)
 *   2. Let's Encrypt SSL Kur → acme.sh webroot mod ile cert al ve panel'e
 *      kur (nginx reload)
 *
 * 🔴 Kilitlenme koruması: betik `localhost` + IP'yi HER ZAMAN listede tutar,
 * DNS bu sunucuya çözülmüyorsa değişikliği uygulamaz. Yine de riskli — kısa
 * bir HTTP anlık kesintisi olabilir; dolu bir işletme saatinde yapmak yerine
 * daha sakin bir zamanda yapmak önerilir.
 */

type Durum = {
  durum: {
    hostname: string
    izinliler: string[]
    sunucu_ip4: string[]
    sunucu_ip6: string[]
    ssl_konu: string
    ssl_bitis: string
    ssl_kalan_gun: number
    ssl_le: boolean
    catchall_kurulu: boolean
  }
  betik_var: boolean
  acme_var: boolean
}

type Is = {
  id: string
  tip: string
  hostname: string
  durum: string // "kosuyor" | "bitti" | "hata"
  basla: string
  bitis: string
  adimlar: { zaman: string; mesaj: string; basari: boolean }[]
  hata: string
}


const PHOST_EN: Record<string, string> = {
  "Türkçe": "English",
  "Araçlar ve Ayarlar": "Tools and Settings",
  "Yüklenemedi": "Could not load",
  "Yükleniyor…": "Loading…",
  "hiç": "none",
  "DNS eşleşmiyor": "DNS mismatch",
  "DNS kontrol hatası": "DNS check error",
  "Hostname Uygulanıyor": "Applying Hostname",
  "Hostname bu sunucuya çözülmüyor. Yine de deneyeyim mi? (Betik kilitlenmeyi önlemek için değişikliği REDDEDECEK.)": "The hostname does not resolve to this server. Try anyway? (The script will REJECT the change to prevent a lockout.)",
  "SSL kurma hatası": "SSL installation error",
  "SSL sertifikası": "SSL certificate",
  "Uygulama hatası": "Application error",
  "acme.sh yüklü değil:": "acme.sh is not installed:",
  "localhost ve IP her zaman erişilebilir kalır.": "localhost and IP always remain accessible.",
  "İzinli isimler:": "Allowed names:",
  "— (IP ile erişim)": "— (access via IP)",
  "⚠ DNS uyuşmuyor. Çözülen:": "⚠ DNS mismatch. Resolved:",
  "✓ DNS bu sunucuya çözülüyor:": "✓ DNS resolves to this server:",
  "Yine de dene": "Try anyway",
  "Let's Encrypt kur?": "Install Let's Encrypt?",
  "DNS kontrol → hostname uygula → Let's Encrypt sertifikası; hepsi sırayla çalışır (30-90 sn). LE haftalık limit: 5 başarısız/hafta.": "DNS check → apply hostname → Let's Encrypt certificate; all run in sequence (30-90 s). LE weekly limit: 5 failures/week.",
  "Kur": "Install",
  "Hostname uygulanamadı": "Hostname could not be applied",
  "Hostname uygulama başarısız — SSL adımı atlandı.": "Hostname application failed — SSL step skipped.",
  "SSL kurulamadı": "SSL could not be installed",
  "Let's Encrypt sertifikası alınamadı.": "Could not obtain the Let's Encrypt certificate.",
  "Kurulum hatası": "Installation error",
  "DNS kontrol…": "DNS check…",
  "Kuruluyor…": "Installing…",
  "Let's Encrypt Kur": "Install Let's Encrypt",
  "Başlatılamadı": "Could not start",
  "✓ SSL kuruldu.": "✓ SSL installed.",
  "Yeni adreste paneli aç →": "Open the panel at the new address →",
  "Paneline özel bir alan adı bağla ve Let's Encrypt sertifikası kur. Kilitlenme koruması:": "Bind a custom domain to your panel and install a Let's Encrypt certificate. Lockout protection:",
  "Betik yok:": "Script missing:",
  "/usr/local/bin/girginospanel-panelhost kurulmamış. Hostname değişimi çalışmayacak.": "/usr/local/bin/girginospanel-panelhost is not installed. Hostname change will not work.",
  "Let's Encrypt SSL kurma bu sunucuda yapılamaz. `curl https://get.acme.sh | sh` ile kur.": "Let's Encrypt SSL cannot be installed on this server. Install it with `curl https://get.acme.sh | sh`.",
  "Mevcut hostname": "Current hostname",
  "Sunucu IP'leri": "Server IPs",
  "Sunucu:": "Server:",
  "Bitiş:": "Expiry:",
  "gün": "days",
  "Yakala-hepsi (bilinmeyen host → 444)": "Catch-all (unknown host → 444)",
  "✓ Aktif": "✓ Active",
  "✗ Yok": "✗ None",
  "Yeni Hostname": "New Hostname",
  "Registrar'da A kaydını sunucunun IP'sine çevir.": "Point the A record to the server's IP at your registrar.",
  "SSL Kuruluyor": "Installing SSL",
  "bekleniyor…": "waiting…",
  "Hata:": "Error:",
  "✓ İşlem başarılı": "✓ Operation successful",
  "sn": "s",
  // Backend iş adımları (statik) — SSL kurucu terminal çıktısı
  "girginospanel-panelhost betiği bulunamadı": "girginospanel-panelhost script not found",
  "girginospanel-panelhost betiği bulundu": "girginospanel-panelhost script found",
  "acme.sh yüklü değil (/root/.acme.sh/acme.sh)": "acme.sh not installed (/root/.acme.sh/acme.sh)",
  "acme.sh yüklü": "acme.sh installed",
  "hostname panel vhost'unda tanımlı değil — önce Hostname Ayarla": "hostname not defined in panel vhost — set Hostname first",
  "hostname panel vhost'unda tanımlı": "hostname defined in panel vhost",
  "nginx -t geçti": "nginx -t passed",
  "nginx reload OK": "nginx reload OK",
  "acme.sh --issue çağrılıyor…": "calling acme.sh --issue…",
  "sertifika zaten geçerli (yeniden alınmadı) — panele kuruluyor": "certificate already valid (not re-issued) — installing to panel",
  "cert alındı": "certificate obtained",
  "mevcut cert/key yedeklendi (.yedek-oncesi-le)": "existing cert/key backed up (.yedek-oncesi-le)",
  "install-cert fail → cert/key yedekten geri alındı": "install-cert fail → cert/key restored from backup",
  "yeni cert PEM olarak geçerli": "new cert valid as PEM",
  "acme.sh cron kaydı yenilendi (90 gün öncesi auto-renew)": "acme.sh cron entry refreshed (auto-renew before 90 days)",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (PHOST_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function PanelHostPage() {
  useTranslation() // dil re-render aboneligi
  const [d, setD] = useState<Durum | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [taslak, setTaslak] = useState('')
  const [dnsSonuc, setDnsSonuc] = useState<{ cozulen: string[]; eslesme: boolean; sunucu_ip4: string[] } | null>(null)
  const [dnsKontrol, setDnsKontrol] = useState(false)
  const [aktifIs, setAktifIs] = useState<Is | null>(null)
  const [yeniLink, setYeniLink] = useState<string | null>(null)
  const dialog = useDialog()

  const yukle = useCallback(async () => {
    try {
      const r = await api.get<Durum>('/panel-host')
      setD(r.data)
      if (!taslak) setTaslak(r.data.durum.hostname || '')
    } catch (e) {
      setHata(apiHata(e, cevir("Yüklenemedi")))
    } finally { setYukleniyor(false) }
  }, [taslak])
  useEffect(() => { void yukle() }, [])

  // İş polling — bir iş varsa 2 sn'de bir güncelle
  useEffect(() => {
    if (!aktifIs || aktifIs.durum !== 'kosuyor') return
    const t = setInterval(async () => {
      try {
        const r = await api.get<Is>('/panel-host/is?id=' + aktifIs.id)
        setAktifIs(r.data)
        if (r.data.durum !== 'kosuyor') {
          // Bitti → durumu tazele
          void yukle()
        }
      } catch { /* iş kayboldu; polling'i durdur */
        clearInterval(t)
      }
    }, 2000)
    return () => clearInterval(t)
  }, [aktifIs, yukle])

  // isBekle — bir işi bitene kadar (durum "kosuyor" değil) yoklar; son durumu döner.
  const isBekle = async (isId: string): Promise<Is> => {
    for (;;) {
      const r = await api.get<Is>('/panel-host/is?id=' + isId)
      setAktifIs(r.data)
      if (r.data.durum !== 'kosuyor') return r.data
      await new Promise(res => setTimeout(res, 2000))
    }
  }

  // tekTiklaKur — TEK BUTON: DNS kontrol → hostname uygula → Let's Encrypt kur,
  // hepsi sırayla. Başarılı olunca yeni https adresini yeni sekmede açar.
  const tekTiklaKur = async (ev?: FormEvent) => {
    ev?.preventDefault()
    if (!taslak.trim() || aktifIs?.durum === 'kosuyor') return
    setHata(null); setYeniLink(null)
    // 1) DNS kontrol
    setDnsSonuc(null); setDnsKontrol(true)
    let dnsR: any
    try { dnsR = (await api.post('/panel-host/dns', { hostname: taslak })).data; setDnsSonuc(dnsR) }
    catch (e) { setHata(apiHata(e, cevir("DNS kontrol hatası"))); setDnsKontrol(false); return }
    setDnsKontrol(false)
    // 2) DNS uyuşmuyorsa onay (betik kilitlenmeyi önlemek için yine de reddedebilir)
    if (!dnsR?.eslesme) {
      const yine = await dialog.onay({
        baslik: cevir("DNS eşleşmiyor"),
        mesaj: cevir("Hostname bu sunucuya çözülmüyor. Yine de deneyeyim mi? (Betik kilitlenmeyi önlemek için değişikliği REDDEDECEK.)"),
        onayEtiketi: cevir("Yine de dene"), tehlike: true,
      })
      if (!yine) return
    }
    // 3) onay
    const ok = await dialog.onay({
      baslik: cevir("Let's Encrypt kur?"),
      mesaj: cevir("DNS kontrol → hostname uygula → Let's Encrypt sertifikası; hepsi sırayla çalışır (30-90 sn). LE haftalık limit: 5 başarısız/hafta."),
      onayEtiketi: cevir("Kur"),
    })
    if (!ok) return
    setAktifIs(null)
    try {
      // 4) hostname uygula → bitene kadar bekle
      const ap = (await api.post<{ is_id: string }>('/panel-host/apply', { hostname: taslak })).data
      const apSon = await isBekle(ap.is_id)
      if (apSon.durum !== 'bitti') {
        await dialog.bilgi({ baslik: cevir("Hostname uygulanamadı"), mesaj: apSon.hata || cevir("Hostname uygulama başarısız — SSL adımı atlandı.") })
        return
      }
      // 5) Let's Encrypt kur → bitene kadar bekle
      const ss = (await api.post<{ is_id: string }>('/panel-host/ssl', { hostname: taslak })).data
      const ssSon = await isBekle(ss.is_id)
      await yukle()
      // 6) başarı → yeni https adresini yeni sekmede aç (popup engellenirse banner linki)
      if (ssSon.durum === 'bitti') {
        const url = 'https://' + taslak + '/'
        setYeniLink(url)
        window.open(url, '_blank', 'noopener')
      } else {
        await dialog.bilgi({ baslik: cevir("SSL kurulamadı"), mesaj: ssSon.hata || cevir("Let's Encrypt sertifikası alınamadı.") })
      }
    } catch (e) {
      await dialog.bilgi({ baslik: cevir("Başlatılamadı"), mesaj: apiHata(e, cevir("Kurulum hatası")) })
    }
  }

  const zaman = (s: string) => {
    if (!s) return '—'
    try { return new Date(s).toLocaleString('tr-TR', { dateStyle: 'short', timeStyle: 'short' }) } catch { return s }
  }

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { href: '/araclar-ayarlar', etiket: cevir("Araçlar ve Ayarlar") },
        { etiket: 'Panel Hostname & SSL' },
      ]} />

      <div className="mt-4 mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Panel Hostname & SSL</h1>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
          {cevir("Paneline özel bir alan adı bağla ve Let's Encrypt sertifikası kur. Kilitlenme koruması:")}
          <span className="font-medium text-slate-800 dark:text-slate-200"> {cevir("localhost ve IP her zaman erişilebilir kalır.")}</span>
        </p>
      </div>

      {yukleniyor ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">{cevir("Yükleniyor…")}</div>
      ) : hata ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">{hata}</div>
      ) : d && (
        <>
          {/* Ön koşul kontrol */}
          {!d.betik_var && (
            <div className="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
              <b>{cevir("Betik yok:")}</b> {cevir("/usr/local/bin/girginospanel-panelhost kurulmamış. Hostname değişimi çalışmayacak.")}
            </div>
          )}
          {!d.acme_var && (
            <div className="mb-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-300">
              <b>{cevir("acme.sh yüklü değil:")}</b> {cevir("Let's Encrypt SSL kurma bu sunucuda yapılamaz. `curl https://get.acme.sh | sh` ile kur.")}
            </div>
          )}

          {/* Mevcut durum kartı */}
          <div className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <div className="text-xs text-slate-500">{cevir("Mevcut hostname")}</div>
                <div className="mt-0.5 font-mono text-sm">{d.durum.hostname || <span className="text-slate-400">{cevir("— (IP ile erişim)")}</span>}</div>
                <div className="mt-1 text-xs text-slate-500">{cevir("İzinli isimler:")} <span className="font-mono">{(d.durum.izinliler ?? []).join(' ')}</span></div>
              </div>
              <div>
                <div className="text-xs text-slate-500">{cevir("Sunucu IP'leri")}</div>
                <div className="mt-0.5 font-mono text-sm">{(d.durum.sunucu_ip4 ?? []).join(', ') || '—'}</div>
                {(d.durum.sunucu_ip6?.length ?? 0) > 0 && <div className="mt-0.5 font-mono text-xs text-slate-500">v6: {(d.durum.sunucu_ip6 ?? []).join(', ')}</div>}
              </div>
              <div>
                <div className="text-xs text-slate-500">{cevir("SSL sertifikası")}</div>
                <div className="mt-0.5 flex items-center gap-2 text-sm">
                  <span className="font-mono">{d.durum.ssl_konu || '—'}</span>
                  {d.durum.ssl_le
                    ? <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-[11px] text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300">Let's Encrypt</span>
                    : <span className="rounded bg-amber-100 px-1.5 py-0.5 text-[11px] text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">self-signed</span>}
                </div>
                <div className="mt-0.5 text-xs text-slate-500">{cevir("Bitiş:")} {zaman(d.durum.ssl_bitis)} · <b>{d.durum.ssl_kalan_gun} {cevir("gün")}</b></div>
              </div>
              <div>
                <div className="text-xs text-slate-500">{cevir("Yakala-hepsi (bilinmeyen host → 444)")}</div>
                <div className="mt-0.5 text-sm">{d.durum.catchall_kurulu ? cevir('✓ Aktif') : cevir('✗ Yok')}</div>
              </div>
            </div>
          </div>

          {/* Form */}
          <form onSubmit={tekTiklaKur} className="mb-4 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
            <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">{cevir("Yeni Hostname")}</label>
            <div className="flex flex-wrap items-center gap-2">
              <input
                value={taslak}
                onChange={(e) => { setTaslak(e.target.value); setDnsSonuc(null) }}
                placeholder="panel.musteri.com"
                className="flex-1 min-w-[220px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                autoComplete="off" spellCheck={false}
              />
              <button type="submit" disabled={!d.acme_var || dnsKontrol || !taslak.trim() || aktifIs?.durum === 'kosuyor'}
                className="rounded-lg border border-emerald-500 bg-emerald-50 px-4 py-2 text-sm font-medium text-emerald-800 transition-colors hover:bg-emerald-100 disabled:opacity-50 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300 dark:hover:bg-emerald-950/60">
                <span className="inline-flex items-center gap-1.5"><Ikon d={I.kilit} />{dnsKontrol ? cevir("DNS kontrol…") : aktifIs?.durum === 'kosuyor' ? cevir("Kuruluyor…") : cevir("Let's Encrypt Kur")}</span>
              </button>
            </div>

            {dnsSonuc && (
              <div className={`mt-3 rounded-lg border p-3 text-sm ${dnsSonuc.eslesme
                ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300'
                : 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-300'}`}>
                {dnsSonuc.eslesme
                  ? <>{cevir("✓ DNS bu sunucuya çözülüyor:")} <span className="font-mono">{dnsSonuc.cozulen.join(', ')}</span></>
                  : <>{cevir("⚠ DNS uyuşmuyor. Çözülen:")} <span className="font-mono">{dnsSonuc.cozulen.join(', ') || cevir("hiç")}</span> · {cevir("Sunucu:")} <span className="font-mono">{dnsSonuc.sunucu_ip4.join(', ')}</span>. {cevir("Registrar'da A kaydını sunucunun IP'sine çevir.")}</>}
              </div>
            )}

            {yeniLink && (
              <div className="mt-3 rounded-lg border border-emerald-300 bg-emerald-50 p-3 text-sm text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300">
                {cevir("✓ SSL kuruldu.")} <a href={yeniLink} target="_blank" rel="noopener noreferrer" className="font-medium underline">{cevir("Yeni adreste paneli aç →")}</a>
              </div>
            )}
          </form>

          {/* İş ilerleme */}
          {aktifIs && (
            <div className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
              <div className="mb-2 flex items-center justify-between gap-2">
                <div>
                  <div className="text-sm font-medium">
                    {aktifIs.tip === 'ayarla' ? cevir('Hostname Uygulanıyor') : cevir('SSL Kuruluyor')}
                    <span className="ml-2 font-mono text-xs text-slate-500">{aktifIs.hostname}</span>
                  </div>
                  <div className="text-xs text-slate-500">{zaman(aktifIs.basla)} · {aktifIs.durum}</div>
                </div>
                {aktifIs.durum === 'kosuyor' && (
                  <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500" />
                )}
              </div>
              <div className="max-h-64 overflow-y-auto rounded border border-slate-100 bg-slate-50 p-2 font-mono text-xs dark:border-slate-800 dark:bg-slate-950">
                {aktifIs.adimlar.map((a, i) => (
                  <div key={i} className={a.basari ? 'text-slate-700 dark:text-slate-300' : 'text-red-700 dark:text-red-400'}>
                    <span className="text-slate-400">{new Date(a.zaman).toLocaleTimeString('tr-TR', { hour12: false })}</span>{' '}
                    {a.basari ? '✓' : '✗'} {cevir(a.mesaj)}
                  </div>
                ))}
                {aktifIs.adimlar.length === 0 && <div className="text-slate-500">{cevir("bekleniyor…")}</div>}
              </div>
              {aktifIs.hata && (
                <div className="mt-2 rounded border border-red-200 bg-red-50 p-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
                  {cevir("Hata:")} {aktifIs.hata}
                </div>
              )}
              {aktifIs.durum === 'bitti' && (
                <div className="mt-2 rounded border border-emerald-200 bg-emerald-50 p-2 text-sm text-emerald-800 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300">
                  {cevir("✓ İşlem başarılı")} ({Math.round((new Date(aktifIs.bitis).getTime() - new Date(aktifIs.basla).getTime()) / 1000)} {cevir("sn")})
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}
