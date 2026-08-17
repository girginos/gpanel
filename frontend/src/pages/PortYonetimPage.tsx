import type { FormEvent } from 'react'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

/*
 * Port Değiştirme — admin only.
 *
 * 🔴 EN KRİTİK ÖZELLİK. Yanlış port → panel kendi altından çekilir → lockout.
 * Backend kilitlenme koruması ile aşağıdaki savunmalar var:
 *   1. Yasak port listesi (web/mail/DB/DNS/sistem — reddedilir)
 *   2. Meşgul port kontrolü (ss -tln)
 *   3. Async iş: env yedekle → conf yedekle → uygula → curl-verify → başarısızsa
 *      OTOMATİK GERI AL. Kilitlenme koruması bu curl-verify adımıdır.
 *
 * UI iki port için ayrı form: backend (127.0.0.1:X) ve dış (nginx :X).
 * Değişiklik sonrası panel adresi değişebilir — kullanıcıya UYARI ver.
 */

type Adim = { etiket: string; ok: boolean; bilgi?: string; ts: string }
type Is = {
  tip: string; eski_port: number; yeni_port: number
  baslama_ts: string; bitis_ts: string
  basarili: boolean; rollback: boolean
  adimlar: Adim[]; son_hata?: string
}
type Durum = {
  backend_port: number; dis_port: number
  uygulama: string; aktif_is?: Is
}
type Gecmis = { id: number; tip: string; eski_port: number; yeni_port: number
  basarili: boolean; rollback: boolean; son_hata: string; created_at: string }
type YasakliBilgi = { portlar: { port: number; aciklama: string }[]; sistem_alt: number; not: string }

export default function PortYonetimPage() {
  const [durum, setDurum] = useState<Durum | null>(null)
  const [gecmis, setGecmis] = useState<Gecmis[]>([])
  const [yasakli, setYasakli] = useState<YasakliBilgi | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [yeniDis, setYeniDis] = useState('')
  const [gonderiliyor, setGonderiliyor] = useState<string | null>(null)
  const dialog = useDialog()

  const yukle = async () => {
    setHata(null)
    try {
      const [d, g] = await Promise.all([
        api.get<Durum>('/portlar'),
        api.get<{ items: Gecmis[] }>('/portlar/gecmis'),
      ])
      setDurum(d.data)
      setGecmis(g.data.items)
    } catch (e) { setHata(apiHata(e, 'Yüklenemedi')) }
    finally { setYukleniyor(false) }
  }

  const yasakliYukle = async () => {
    try {
      const r = await api.get<YasakliBilgi>('/portlar/yasakli')
      setYasakli(r.data)
    } catch { /* opsiyonel */ }
  }

  useEffect(() => { void yukle(); void yasakliYukle() }, [])

  // Aktif iş varsa poll et
  useEffect(() => {
    if (!durum?.aktif_is || durum.aktif_is.bitis_ts !== '0001-01-01T00:00:00Z') return
    const t = setInterval(async () => {
      try {
        const r = await api.get<{ is: Is | null }>('/portlar/is')
        if (r.data.is) {
          setDurum((d) => d ? { ...d, aktif_is: r.data.is || undefined } : d)
          // Bittiyse — durum + geçmiş yeniden yükle
          if (r.data.is.bitis_ts !== '0001-01-01T00:00:00Z') {
            clearInterval(t)
            await yukle()
          }
        }
      } catch { /* poll fail — bırak */ }
    }, 1500)
    return () => clearInterval(t)
  }, [durum?.aktif_is])


  const disUygula = async (ev: FormEvent) => {
    ev.preventDefault()
    const p = parseInt(yeniDis, 10)
    if (!p) return
    const ok = await dialog.onay({
      baslik: `Dış SSL portu ${durum?.dis_port} → ${p} olsun mu?`,
      mesaj: `🔴 UYARI: Değişiklik sonrası panel adresi https://sunucu:${p} olur. Mevcut sekmeniz kırılır. Curl-verify başarısız olursa OTOMATİK GERI ALINIR. Panel yeni portu firewall'da OTOMATİK açar, eski portu OTOMATİK siler. Bulut sağlayıcı FW (Hetzner Cloud vb.) ayrı katmandır — orayı elle açman gerekir.`,
      onayEtiketi: 'Uygula', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    setGonderiliyor('dis')
    try {
      await api.post('/portlar/dis', { yeni_port: p })
      setYeniDis('')
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: 'Hata', mesaj: apiHata(e, 'İstek başarısız') })
    } finally { setGonderiliyor(null) }
  }

  const aktifCalisiyor = durum?.aktif_is && durum.aktif_is.bitis_ts === '0001-01-01T00:00:00Z'

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { href: '/araclar-ayarlar', etiket: 'Araçlar ve Ayarlar' },
        { etiket: 'Port Değiştirme' },
      ]} />

      <div className="mt-4 mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Port Değiştirme</h1>
        <div className="mt-3 rounded-xl border border-amber-300 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
          <div className="font-medium">🔴 Firewall + lockout uyarısı</div>
          <ul className="mt-1 space-y-0.5 text-xs list-disc pl-5">
            <li>Curl-verify sadece <code className="font-mono">127.0.0.1</code>&apos;dan yapılır — firewalld / nftables / Hetzner Cloud FW yeni portu engelliyorsa yerel test geçer, dışardan erişemezsiniz. Yeni port için firewall&apos;ı ÖNCEDEN açın.</li>
            <li>Bağlantı düşerse durum için: SSH&apos;tan <code className="font-mono">journalctl -u girginospanel</code> veya panel geri geldiğinde geçmiş tablosu.</li>
          </ul>
        </div>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
          Panelin dış SSL portunu değiştir.
          <span className="ml-1 font-medium text-slate-800 dark:text-slate-200">
            Curl-verify başarısız olursa otomatik geri alınır.
          </span>
        </p>
      </div>

      {yukleniyor ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">Yükleniyor…</div>
      ) : hata ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">{hata}</div>
      ) : durum && (
        <>
          {/* Aktif iş barı */}
          {aktifCalisiyor && durum.aktif_is && (
            <div className="mb-6 rounded-2xl border border-amber-300 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/30">
              <div className="mb-2 flex items-center gap-2 text-sm font-medium text-amber-900 dark:text-amber-200">
                <span className="inline-block h-2 w-2 animate-pulse rounded-full bg-amber-500" />
                Port değişikliği devam ediyor: {durum.aktif_is.tip} {durum.aktif_is.eski_port} → {durum.aktif_is.yeni_port}
              </div>
              <ol className="space-y-1 text-xs">
                {durum.aktif_is.adimlar.map((a, i) => (
                  <li key={i} className={a.ok ? 'text-emerald-700 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'}>
                    {a.ok ? '✓' : '✗'} {a.etiket}{a.bilgi ? ` — ${a.bilgi}` : ''}
                  </li>
                ))}
              </ol>
            </div>
          )}

          <div className="mx-auto max-w-2xl">
            {/* Dış port */}
            <form onSubmit={disUygula} className="rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900">
              <h2 className="mb-1 text-base font-semibold">Dış SSL portu</h2>
              <p className="mb-4 text-xs text-slate-500">Panele tarayıcıdan bağlanma portu (nginx listen).</p>
              <div className="mb-3 flex items-center gap-3">
                <span className="text-sm text-slate-500">Mevcut:</span>
                <span className="rounded-md bg-slate-100 px-2 py-1 font-mono text-sm dark:bg-slate-800">:{durum.dis_port}</span>
              </div>
              <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">Yeni port</label>
              <input type="number" value={yeniDis} onChange={(e) => setYeniDis(e.target.value)}
                min={1024} max={65535} placeholder={`örn. ${durum.dis_port === 8443 ? 9443 : 8443}`}
                className="mb-4 w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-mono text-sm dark:border-slate-700 dark:bg-slate-950" />
              <button type="submit" disabled={!!gonderiliyor || !!aktifCalisiyor || !yeniDis}
                className="w-full rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50">
                {gonderiliyor === 'dis' ? 'Uygulanıyor…' : 'Dış portu değiştir'}
              </button>
            </form>
          </div>

          {/* Yasaklı portlar (bilgi) */}
          {yasakli && (
            <details className="mt-6 rounded-2xl border border-slate-200 bg-white p-4 text-sm dark:border-slate-800 dark:bg-slate-900">
              <summary className="cursor-pointer font-medium text-slate-700 dark:text-slate-300">Yasaklı portlar ({yasakli.portlar.length} + 1-1023 sistem)</summary>
              <p className="mt-2 text-xs text-slate-500">{yasakli.not}</p>
              <div className="mt-3 grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-3 md:grid-cols-4">
                {[...yasakli.portlar].sort((a, b) => a.port - b.port).map((y) => (
                  <div key={y.port} className="flex items-baseline gap-2">
                    <span className="font-mono text-slate-700 dark:text-slate-300">{y.port}</span>
                    <span className="truncate text-slate-500" title={y.aciklama}>{y.aciklama}</span>
                  </div>
                ))}
              </div>
            </details>
          )}

          {/* Geçmiş */}
          {gecmis.length > 0 && (
            <div className="mt-6 overflow-hidden rounded-2xl border border-slate-200 dark:border-slate-800">
              <div className="border-b border-slate-200 bg-slate-50 px-4 py-2 text-sm font-medium dark:border-slate-800 dark:bg-slate-900">Geçmiş (son 20)</div>
              <div className="overflow-x-auto">
                <table className="min-w-[640px] w-full text-left text-sm">
                  <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                    <tr>
                      <th className="px-3 py-2">Tarih</th>
                      <th className="px-3 py-2">Tip</th>
                      <th className="px-3 py-2">Eski → Yeni</th>
                      <th className="px-3 py-2">Sonuç</th>
                      <th className="px-3 py-2">Hata</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                    {gecmis.map((g) => (
                      <tr key={g.id} className="bg-white dark:bg-slate-950">
                        <td className="px-3 py-2 font-mono text-xs text-slate-500">{g.created_at}</td>
                        <td className="px-3 py-2 text-xs">{g.tip}</td>
                        <td className="px-3 py-2 font-mono">{g.eski_port} → {g.yeni_port}</td>
                        <td className="px-3 py-2">
                          {g.basarili ? (
                            <span className="rounded-full bg-emerald-100 px-2 py-0.5 text-[11px] font-medium text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300">başarılı</span>
                          ) : g.rollback ? (
                            <span className="rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-medium text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">geri alındı</span>
                          ) : (
                            <span className="rounded-full bg-red-100 px-2 py-0.5 text-[11px] font-medium text-red-800 dark:bg-red-950/40 dark:text-red-300">başarısız</span>
                          )}
                        </td>
                        <td className="px-3 py-2 text-xs text-red-600 dark:text-red-400" title={g.son_hata}>
                          {g.son_hata ? (g.son_hata.length > 40 ? g.son_hata.slice(0, 40) + '…' : g.son_hata) : '—'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
