import type { FormEvent } from 'react'
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

export default function PanelHostPage() {
  const [d, setD] = useState<Durum | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [taslak, setTaslak] = useState('')
  const [dnsSonuc, setDnsSonuc] = useState<{ cozulen: string[]; eslesme: boolean; sunucu_ip4: string[] } | null>(null)
  const [dnsKontrol, setDnsKontrol] = useState(false)
  const [aktifIs, setAktifIs] = useState<Is | null>(null)
  const dialog = useDialog()

  const yukle = useCallback(async () => {
    try {
      const r = await api.get<Durum>('/panel-host')
      setD(r.data)
      if (!taslak) setTaslak(r.data.durum.hostname || '')
    } catch (e) {
      setHata(apiHata(e, 'Yüklenemedi'))
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

  const dns = async (ev: FormEvent) => {
    ev.preventDefault()
    setDnsSonuc(null); setHata(null)
    setDnsKontrol(true)
    try {
      const r = await api.post('/panel-host/dns', { hostname: taslak })
      setDnsSonuc(r.data)
    } catch (e) { setHata(apiHata(e, 'DNS kontrol hatası')) }
    finally { setDnsKontrol(false) }
  }

  const uygula = async () => {
    if (!dnsSonuc?.eslesme) {
      const yine = await dialog.onay({
        baslik: 'DNS eşleşmiyor',
        mesaj: 'Hostname bu sunucuya çözülmüyor. Yine de deneyeyim mi? (Betik kilitlenmeyi önlemek için değişikliği REDDEDECEK.)',
        onayEtiketi: 'Yine de dene', tehlike: true,
      })
      if (!yine) return
    }
    setAktifIs(null)
    try {
      const r = await api.post<{ is_id: string }>('/panel-host/apply', { hostname: taslak })
      const rr = await api.get<Is>('/panel-host/is?id=' + r.data.is_id)
      setAktifIs(rr.data)
    } catch (e) {
      await dialog.bilgi({ baslik: 'Başlatılamadı', mesaj: apiHata(e, 'Uygulama hatası') })
    }
  }

  const sslKur = async () => {
    const ok = await dialog.onay({
      baslik: 'Let\'s Encrypt SSL kur?',
      mesaj: `Hostname: ${taslak}\n\nBu işlem 30-90 saniye sürer. LE'nin haftalık limitleri var (5 fail/haftalık); başarısız olursa hemen tekrar deneme. DNS bu sunucuya yönlendirilmiş olmalı.`,
      onayEtiketi: 'SSL Kur',
    })
    if (!ok) return
    setAktifIs(null)
    try {
      const r = await api.post<{ is_id: string }>('/panel-host/ssl', { hostname: taslak })
      const rr = await api.get<Is>('/panel-host/is?id=' + r.data.is_id)
      setAktifIs(rr.data)
    } catch (e) {
      await dialog.bilgi({ baslik: 'Başlatılamadı', mesaj: apiHata(e, 'SSL kurma hatası') })
    }
  }

  const zaman = (s: string) => {
    if (!s) return '—'
    try { return new Date(s).toLocaleString('tr-TR', { dateStyle: 'short', timeStyle: 'short' }) } catch { return s }
  }

  return (
    <div className="mx-auto max-w-4xl px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { href: '/araclar-ayarlar', etiket: 'Araçlar ve Ayarlar' },
        { etiket: 'Panel Hostname & SSL' },
      ]} />

      <div className="mt-4 mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">Panel Hostname & SSL</h1>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
          Paneline özel bir alan adı bağla ve Let's Encrypt sertifikası kur. Kilitlenme koruması:
          <span className="font-medium text-slate-800 dark:text-slate-200"> localhost ve IP her zaman erişilebilir kalır.</span>
        </p>
      </div>

      {yukleniyor ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">Yükleniyor…</div>
      ) : hata ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">{hata}</div>
      ) : d && (
        <>
          {/* Ön koşul kontrol */}
          {!d.betik_var && (
            <div className="mb-4 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
              <b>Betik yok:</b> /usr/local/bin/girginospanel-panelhost kurulmamış. Hostname değişimi çalışmayacak.
            </div>
          )}
          {!d.acme_var && (
            <div className="mb-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-300">
              <b>acme.sh yüklü değil:</b> Let's Encrypt SSL kurma bu sunucuda yapılamaz. `curl https://get.acme.sh | sh` ile kur.
            </div>
          )}

          {/* Mevcut durum kartı */}
          <div className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <div className="text-xs text-slate-500">Mevcut hostname</div>
                <div className="mt-0.5 font-mono text-sm">{d.durum.hostname || <span className="text-slate-400">— (IP ile erişim)</span>}</div>
                <div className="mt-1 text-xs text-slate-500">İzinli isimler: <span className="font-mono">{(d.durum.izinliler ?? []).join(' ')}</span></div>
              </div>
              <div>
                <div className="text-xs text-slate-500">Sunucu IP'leri</div>
                <div className="mt-0.5 font-mono text-sm">{(d.durum.sunucu_ip4 ?? []).join(', ') || '—'}</div>
                {(d.durum.sunucu_ip6?.length ?? 0) > 0 && <div className="mt-0.5 font-mono text-xs text-slate-500">v6: {(d.durum.sunucu_ip6 ?? []).join(', ')}</div>}
              </div>
              <div>
                <div className="text-xs text-slate-500">SSL sertifikası</div>
                <div className="mt-0.5 flex items-center gap-2 text-sm">
                  <span className="font-mono">{d.durum.ssl_konu || '—'}</span>
                  {d.durum.ssl_le
                    ? <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-[11px] text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300">Let's Encrypt</span>
                    : <span className="rounded bg-amber-100 px-1.5 py-0.5 text-[11px] text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">self-signed</span>}
                </div>
                <div className="mt-0.5 text-xs text-slate-500">Bitiş: {zaman(d.durum.ssl_bitis)} · <b>{d.durum.ssl_kalan_gun} gün</b></div>
              </div>
              <div>
                <div className="text-xs text-slate-500">Yakala-hepsi (bilinmeyen host → 444)</div>
                <div className="mt-0.5 text-sm">{d.durum.catchall_kurulu ? '✓ Aktif' : '✗ Yok'}</div>
              </div>
            </div>
          </div>

          {/* Form */}
          <form onSubmit={dns} className="mb-4 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
            <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">Yeni Hostname</label>
            <div className="flex flex-wrap items-center gap-2">
              <input
                value={taslak}
                onChange={(e) => { setTaslak(e.target.value); setDnsSonuc(null) }}
                placeholder="panel.musteri.com"
                className="flex-1 min-w-[220px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
                autoComplete="off" spellCheck={false}
              />
              <button type="submit" disabled={dnsKontrol || !taslak.trim()}
                className="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 transition-colors hover:bg-slate-50 disabled:opacity-50 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-300 dark:hover:bg-slate-800">
                {dnsKontrol ? 'Kontrol…' : 'DNS Kontrol'}
              </button>
              <button type="button" onClick={uygula} disabled={!dnsSonuc || (aktifIs?.durum === 'kosuyor')}
                className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white">
                Uygula
              </button>
              <button type="button" onClick={sslKur} disabled={!d.acme_var || (aktifIs?.durum === 'kosuyor')}
                className="rounded-lg border border-emerald-500 bg-emerald-50 px-4 py-2 text-sm font-medium text-emerald-800 transition-colors hover:bg-emerald-100 disabled:opacity-50 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300 dark:hover:bg-emerald-950/60">
                🔒 Let's Encrypt Kur
              </button>
            </div>

            {dnsSonuc && (
              <div className={`mt-3 rounded-lg border p-3 text-sm ${dnsSonuc.eslesme
                ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300'
                : 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-300'}`}>
                {dnsSonuc.eslesme
                  ? <>✓ DNS bu sunucuya çözülüyor: <span className="font-mono">{dnsSonuc.cozulen.join(', ')}</span></>
                  : <>⚠ DNS uyuşmuyor. Çözülen: <span className="font-mono">{dnsSonuc.cozulen.join(', ') || 'hiç'}</span> · Sunucu: <span className="font-mono">{dnsSonuc.sunucu_ip4.join(', ')}</span>. Registrar'da A kaydını sunucunun IP'sine çevir.</>}
              </div>
            )}
          </form>

          {/* İş ilerleme */}
          {aktifIs && (
            <div className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
              <div className="mb-2 flex items-center justify-between gap-2">
                <div>
                  <div className="text-sm font-medium">
                    {aktifIs.tip === 'ayarla' ? 'Hostname Uygulanıyor' : 'SSL Kuruluyor'}
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
                    {a.basari ? '✓' : '✗'} {a.mesaj}
                  </div>
                ))}
                {aktifIs.adimlar.length === 0 && <div className="text-slate-500">bekleniyor…</div>}
              </div>
              {aktifIs.hata && (
                <div className="mt-2 rounded border border-red-200 bg-red-50 p-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
                  Hata: {aktifIs.hata}
                </div>
              )}
              {aktifIs.durum === 'bitti' && (
                <div className="mt-2 rounded border border-emerald-200 bg-emerald-50 p-2 text-sm text-emerald-800 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-300">
                  ✓ İşlem başarılı ({Math.round((new Date(aktifIs.bitis).getTime() - new Date(aktifIs.basla).getTime()) / 1000)} sn)
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}
