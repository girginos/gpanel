import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'

type IPKaydi = {
  ip: string
  aktif: boolean
  etiket: string
  arayuzde?: boolean
  arayuz?: string
  warmup?: boolean
  warmup_gun?: number
  agirlik?: number
  dnsbl?: string[] | null
  son_kontrol?: string
  rotasyonda?: boolean
}
type SunucuAdres = { ip: string; arayuz: string; havuzda: boolean }
type Yanit = { havuz: IPKaydi[]; arayuz: string; sunucu_ipleri?: SunucuAdres[] }
type Dedicated = { domain: string; ip: string }
type DedYanit = { dedicated: Dedicated[]; aktif_ip: string[] }

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/
function gecerliIP(s: string): boolean {
  const m = IPV4.exec(s.trim())
  return !!m && m.slice(1).every(o => +o >= 0 && +o <= 255)
}

// SaatlikBar — son 24 saatlik giden mail mini bar grafiği (sparkline).
function SaatlikBar({ veri, max }: { veri: number[]; max: number }) {
  const dizi = veri.length ? veri : Array(24).fill(0)
  const toplam = dizi.reduce((a, b) => a + b, 0)
  return (
    <div className="w-full min-w-[90px]" title={`Son 24 saat: ${toplam} giden mail`}>
      <div className="flex items-end gap-px h-7">
        {dizi.map((c, i) => (
          <span key={i} className="flex-1 rounded-t-sm bg-brand-500/60 dark:bg-brand-400/60 min-h-[2px]"
            style={{ height: max > 0 ? `${Math.max(8, (c / max) * 100)}%` : '8%' }} title={`${c} mail`} />
        ))}
      </div>
      <div className="mt-0.5 text-[10px] text-slate-400 text-center tabular-nums">24s: {toplam}</div>
    </div>
  )
}

export default function MailIPHavuzuPage() {
  const [havuz, setHavuz] = useState<IPKaydi[]>([])
  const [arayuz, setArayuz] = useState('')
  const [sunucuIP, setSunucuIP] = useState<SunucuAdres[]>([])
  const [dnsblKontrol, setDnsblKontrol] = useState(false)
  const [grafik, setGrafik] = useState<Record<string, number[]>>({})
  const [grafikMax, setGrafikMax] = useState(0)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [kaydediliyor, setKaydediliyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [bildirim, setBildirim] = useState<string | null>(null)

  // Dedicated (domaine özel IP) durumu.
  const [dedicated, setDedicated] = useState<Dedicated[]>([])
  const [aktifIP, setAktifIP] = useState<string[]>([])
  const [dedKaydediliyor, setDedKaydediliyor] = useState(false)
  const [dedHata, setDedHata] = useState<string | null>(null)
  const [dedBildirim, setDedBildirim] = useState<string | null>(null)

  function yukle() {
    api.get<Yanit>('/eklenti/mail/genel/ip-havuzu')
      .then(r => { setHavuz(r.data.havuz ?? []); setArayuz(r.data.arayuz ?? ''); setSunucuIP(r.data.sunucu_ipleri ?? []) })
      .catch(e => setHata(apiHata(e, 'IP havuzu yüklenemedi (mail eklentisi aktif mi?)')))
      .finally(() => setYukleniyor(false))
  }
  function dedYukle() {
    api.get<DedYanit>('/eklenti/mail/genel/dedicated-ip')
      .then(r => { setDedicated(r.data.dedicated ?? []); setAktifIP(r.data.aktif_ip ?? []) })
      .catch(() => { /* havuz hatası zaten gösteriliyor */ })
  }
  function grafikYukle() {
    api.get<{ ipler: { ip: string; saatlik: number[] }[]; max: number }>('/eklenti/mail/genel/ip-havuzu/grafik')
      .then(r => {
        const m: Record<string, number[]> = {}
        ;(r.data.ipler ?? []).forEach(x => { m[x.ip] = x.saatlik })
        setGrafik(m); setGrafikMax(r.data.max ?? 0)
      })
      .catch(() => { /* grafik opsiyonel; hata gösterme */ })
  }
  useEffect(() => { yukle(); dedYukle(); grafikYukle() }, [])

  function satirGuncelle(i: number, yama: Partial<IPKaydi>) {
    setHavuz(h => h.map((k, j) => j === i ? { ...k, ...yama } : k))
  }
  function satirEkle(ip = '', etiket = '') {
    setHavuz(h => (ip && h.some(k => k.ip.trim() === ip)) ? h : [...h, { ip, aktif: true, etiket, warmup: true }])
  }

  // Manuel kara liste (DNSBL) kontrolü — sunucudaki her havuz IP'sini tarar, sonucu günceller.
  async function dnsblKontrolEt() {
    setDnsblKontrol(true); setHata(null); setBildirim(null)
    try {
      const r = await api.post<Yanit>('/eklenti/mail/genel/ip-havuzu/dnsbl-kontrol', {})
      setHavuz(r.data.havuz ?? [])
      const kirli = (r.data.havuz ?? []).filter(k => (k.dnsbl?.length ?? 0) > 0)
      setBildirim(kirli.length === 0
        ? '✓ Kara liste kontrolü tamamlandı — tüm IP\'ler temiz (hiçbir kara listede değil).'
        : `⚠ ${kirli.length} IP kara listede: ${kirli.map(k => k.ip).join(', ')}`)
    } catch (e) { setHata(apiHata(e, 'Kara liste kontrolü yapılamadı')) }
    finally { setDnsblKontrol(false) }
  }
  function satirSil(i: number) { setHavuz(h => h.filter((_, j) => j !== i)) }

  async function kaydet() {
    setHata(null); setBildirim(null)
    const temiz = havuz.map(k => ({ ...k, ip: k.ip.trim() })).filter(k => k.ip !== '')
    const gecersiz = temiz.find(k => !gecerliIP(k.ip))
    if (gecersiz) { setHata(`Geçersiz IPv4 adresi: ${gecersiz.ip}`); return }
    setKaydediliyor(true)
    try {
      const r = await api.put<Yanit>('/eklenti/mail/genel/ip-havuzu', {
        havuz: temiz.map(k => ({ ip: k.ip, aktif: k.aktif, etiket: k.etiket, warmup: k.warmup ?? true })),
      })
      setHavuz(r.data.havuz ?? [])
      setBildirim('✓ IP havuzu kaydedildi, arayüze tanımlandı ve Postfix rotasyonu uygulandı.')
      dedYukle() // aktif IP listesi değişmiş olabilir
    } catch (e) { setHata(apiHata(e, 'Kaydedilemedi')) }
    finally { setKaydediliyor(false) }
  }

  function dedEkle() {
    setDedicated(d => [...d, { domain: '', ip: aktifIP[0] ?? '' }])
  }
  function dedGuncelle(i: number, yama: Partial<Dedicated>) {
    setDedicated(d => d.map((x, j) => j === i ? { ...x, ...yama } : x))
  }
  function dedSil(i: number) { setDedicated(d => d.filter((_, j) => j !== i)) }

  async function dedKaydet() {
    setDedHata(null); setDedBildirim(null)
    const temiz = dedicated
      .map(d => ({ domain: d.domain.trim().toLowerCase(), ip: d.ip.trim() }))
      .filter(d => d.domain !== '')
    const gecersizIP = temiz.find(d => !aktifIP.includes(d.ip))
    if (gecersizIP) { setDedHata(`"${gecersizIP.domain}" için seçilen IP aktif havuzda değil: ${gecersizIP.ip}`); return }
    setDedKaydediliyor(true)
    try {
      const r = await api.put<{ dedicated: Dedicated[] }>('/eklenti/mail/genel/dedicated-ip', { dedicated: temiz })
      setDedicated(r.data.dedicated ?? [])
      setDedBildirim('✓ Domaine özel IP eşlemeleri kaydedildi ve Postfix\'e uygulandı.')
    } catch (e) { setDedHata(apiHata(e, 'Kaydedilemedi')) }
    finally { setDedKaydediliyor(false) }
  }

  const kart = 'bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm'
  const inp = 'rounded-lg border border-slate-300 dark:border-slate-600 bg-white dark:bg-slate-900 px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-brand-500/40 focus:border-brand-400'
  const aktifSayisi = havuz.filter(k => k.aktif && k.ip.trim() !== '').length

  // Bir IP satırının warm-up + DNSBL + rotasyon durum rozetleri.
  function durumSatiri(k: IPKaydi) {
    const listeli = (k.dnsbl?.length ?? 0) > 0
    const agirlik = k.agirlik ?? (k.warmup === false ? 10 : 1)
    return (
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-500 dark:text-slate-400 pl-1">
        {/* warm-up */}
        {k.warmup === false
          ? <span title="Warm-up kapalı — tam ağırlık">warm-up: kapalı · ağırlık {agirlik}/10 (tam)</span>
          : <span title="IP eklendiğinden bu yana gün · rotasyon ağırlığı">
              warm-up: gün {k.warmup_gun ?? 0} · ağırlık {agirlik}/10
            </span>}
        {/* warm-up mini bar */}
        <span className="inline-flex h-1.5 w-16 rounded-full bg-slate-200 dark:bg-slate-700 overflow-hidden align-middle" title={`ağırlık ${agirlik}/10`}>
          <span className="h-full bg-brand-500" style={{ width: `${Math.round((agirlik / 10) * 100)}%` }} />
        </span>
        {/* kara liste (DNSBL) durumu — temizse "IP temiz", kirliyse yalnız kirli servisler */}
        {listeli
          ? <span className="text-red-600 dark:text-red-400 font-medium" title={`Son kontrol: ${k.son_kontrol || '—'}`}>
              🔴 Kirli: {k.dnsbl!.join(', ')}
            </span>
          : k.son_kontrol
            ? <span className="text-emerald-600 dark:text-emerald-400" title={`Son kontrol: ${k.son_kontrol}`}>✓ IP temiz — kara listede değil</span>
            : <span className="text-slate-400" title="Henüz kara liste kontrolü yapılmadı">kara liste: kontrol edilmedi</span>}
        {/* rotasyon durumu (ayrı gösterge) */}
        {k.rotasyonda
          ? <span className="text-emerald-600 dark:text-emerald-400">· rotasyonda</span>
          : listeli
            ? <span className="text-red-500">· rotasyon dışı</span>
            : <span className="text-slate-400">· rotasyon dışı (pasif)</span>}
      </div>
    )
  }

  return (
    <div>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-5">
        Giden mail trafiği için birden çok IP tanımlayın. Aktif IP'ler otomatik olarak sunucu arayüzüne
        {arayuz && <span className="font-mono"> ({arayuz})</span>} eklenir ve her e-posta havuzdaki IP'lerden
        ağırlıklı-rastgele gönderilir. Yeni IP'ler <span className="font-medium">kademeli ısınır</span> (warm-up),
        DNSBL'e düşen IP <span className="font-medium">otomatik rotasyondan çıkar</span>.
      </p>

      {bildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{bildirim}</div>}
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {yukleniyor ? <div className="py-12 text-center text-sm text-slate-400">Yükleniyor…</div> : (
        <div className="space-y-5">
          {/* Sunucudaki mevcut IP'ler — dinamik, tüm adaptörler otomatik algılanır */}
          <section className={kart}>
            <div className="flex items-center justify-between mb-3">
              <div>
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Sunucudaki IP Adresleri</h2>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">Sunucuya tanımlı tüm IP'ler otomatik algılanır (adaptörler farklı olabilir). Havuza tek tıkla ekleyin.</p>
              </div>
              <button onClick={() => { setYukleniyor(true); yukle() }} className="text-xs text-slate-500 hover:text-brand-600 border border-slate-200 dark:border-slate-700 rounded-lg px-2.5 py-1.5" title="Sunucu IP'lerini yeniden tara">↻ Yenile</button>
            </div>
            {sunucuIP.length === 0 ? (
              <div className="py-4 text-center text-sm text-slate-400">IP algılanamadı.</div>
            ) : (
              <div className="flex flex-wrap gap-2">
                {sunucuIP.map(s => (
                  <div key={s.ip} className="inline-flex items-center gap-2 rounded-lg border border-slate-200 dark:border-slate-700 bg-slate-50 dark:bg-slate-900/40 pl-3 pr-1.5 py-1.5">
                    <span className="font-mono text-sm text-slate-700 dark:text-slate-200">{s.ip}</span>
                    <span className="text-[11px] px-1.5 py-0.5 rounded bg-slate-200 dark:bg-slate-700 text-slate-600 dark:text-slate-300 font-mono" title="Ağ adaptörü">{s.arayuz}</span>
                    {s.havuzda
                      ? <span className="text-[11px] text-emerald-600 dark:text-emerald-400 px-1.5">✓ havuzda</span>
                      : <button onClick={() => satirEkle(s.ip)} className="text-[11px] font-medium text-brand-600 hover:text-white hover:bg-brand-600 border border-brand-200 dark:border-brand-800 rounded px-2 py-0.5 transition-colors">+ Havuza ekle</button>}
                  </div>
                ))}
              </div>
            )}
          </section>

          <section className={kart}>
            <div className="flex items-center justify-between mb-4">
              <div>
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Giden IP Adresleri</h2>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                  {aktifSayisi > 0 ? `${aktifSayisi} aktif IP — ağırlıklı rotasyon etkin` : 'Aktif IP yok — varsayılan sunucu IP\'si kullanılır'}
                </p>
              </div>
              <div className="flex items-center gap-2">
                <button onClick={dnsblKontrolEt} disabled={dnsblKontrol || havuz.length === 0}
                  className="text-sm font-medium text-slate-600 dark:text-slate-300 border border-slate-200 dark:border-slate-700 rounded-lg px-3 py-1.5 hover:border-brand-300 hover:text-brand-600 disabled:opacity-50 transition-colors"
                  title="Havuzdaki tüm IP'leri kara listelerde (DNSBL) tara">
                  {dnsblKontrol ? 'Kontrol ediliyor…' : '🛡 Kara Liste Kontrol Et'}
                </button>
                <button onClick={() => satirEkle()} className="text-sm font-medium text-brand-600 hover:text-brand-700 dark:text-brand-400 border border-brand-200 dark:border-brand-800 rounded-lg px-3 py-1.5">+ IP ekle</button>
              </div>
            </div>

            {havuz.length === 0 ? (
              <div className="py-8 text-center text-sm text-slate-400">Henüz IP eklenmedi. "+ IP ekle" ile başlayın.</div>
            ) : (
              <div className="space-y-3">
                <div className="hidden sm:grid grid-cols-[1.1fr_110px_1.4fr_auto_auto_auto_auto] gap-3 px-1 text-xs font-medium text-slate-400">
                  <span>IP adresi (IPv4)</span><span>Etiket</span><span className="text-center">Saatlik çıkış</span><span>Arayüz</span><span>Warm-up</span><span>Aktif</span><span></span>
                </div>
                {havuz.map((k, i) => (
                  <div key={i} className="border-b border-slate-100 dark:border-slate-700/60 pb-3 last:border-0 last:pb-0">
                    <div className="grid grid-cols-1 sm:grid-cols-[1.1fr_110px_1.4fr_auto_auto_auto_auto] gap-3 items-center">
                      <input value={k.ip} onChange={e => satirGuncelle(i, { ip: e.target.value })} placeholder="1.2.3.4"
                        className={inp + ' font-mono' + (k.ip.trim() !== '' && !gecerliIP(k.ip) ? ' border-red-400 focus:ring-red-500/40' : '')} />
                      <input value={k.etiket} onChange={e => satirGuncelle(i, { etiket: e.target.value })} placeholder="etiket" className={inp} />
                      <SaatlikBar veri={grafik[k.ip.trim()] ?? []} max={grafikMax} />
                      <span className="text-xs text-center whitespace-nowrap">
                        {k.arayuzde
                          ? <span className="text-emerald-600 dark:text-emerald-400 font-mono" title="Bu adaptörde tanımlı">✓ {k.arayuz || 'tanımlı'}</span>
                          : <span className="text-slate-400" title="Henüz uygulanmadı">✗ yok</span>}
                      </span>
                      <label className="inline-flex items-center gap-1.5 cursor-pointer justify-self-start sm:justify-self-center" title="Warm-up açık → kademeli ısınma; kapalı → tam ağırlık (10)">
                        <input type="checkbox" checked={k.warmup ?? true} onChange={e => satirGuncelle(i, { warmup: e.target.checked })}
                          className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
                        <span className="text-xs text-slate-500 sm:hidden">Warm-up</span>
                      </label>
                      <label className="inline-flex items-center gap-1.5 cursor-pointer justify-self-start sm:justify-self-center">
                        <input type="checkbox" checked={k.aktif} onChange={e => satirGuncelle(i, { aktif: e.target.checked })}
                          className="rounded border-slate-300 dark:border-slate-600 text-brand-600 focus:ring-brand-500/40" />
                        <span className="text-xs text-slate-500 sm:hidden">Aktif</span>
                      </label>
                      <button onClick={() => satirSil(i)} className="text-sm text-red-500 hover:text-red-600 justify-self-start sm:justify-self-center px-2" title="Sil">✕</button>
                    </div>
                    {k.ip.trim() !== '' && durumSatiri(k)}
                  </div>
                ))}
              </div>
            )}
          </section>

          <div className="flex justify-end">
            <button onClick={kaydet} disabled={kaydediliyor}
              className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-6 py-2.5 rounded-lg disabled:opacity-60 transition-colors">
              {kaydediliyor ? 'Uygulanıyor…' : 'Kaydet ve Uygula'}
            </button>
          </div>

          {/* ── Domaine Özel IP (dedicated) ── */}
          <section className={kart}>
            <div className="flex items-center justify-between mb-4">
              <div>
                <h2 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Domaine Özel IP (Dedicated)</h2>
                <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
                  Seçili domainlerin gideni rotasyon yerine <span className="font-medium">sabit</span> bir havuz IP'sinden çıkar.
                  Diğer tüm domainler ağırlıklı rotasyonu kullanmaya devam eder.
                </p>
              </div>
              <button onClick={dedEkle} disabled={aktifIP.length === 0}
                className="text-sm font-medium text-brand-600 hover:text-brand-700 dark:text-brand-400 border border-brand-200 dark:border-brand-800 rounded-lg px-3 py-1.5 disabled:opacity-50">+ Ekle</button>
            </div>

            {dedBildirim && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{dedBildirim}</div>}
            {dedHata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">{dedHata}</div>}

            {aktifIP.length === 0 ? (
              <div className="py-6 text-center text-sm text-slate-400">Önce yukarıda en az bir aktif IP ekleyip kaydedin.</div>
            ) : dedicated.length === 0 ? (
              <div className="py-6 text-center text-sm text-slate-400">Domaine özel IP tanımlı değil. "+ Ekle" ile başlayın.</div>
            ) : (
              <div className="space-y-2.5">
                <div className="hidden sm:grid grid-cols-[1fr_1fr_auto] gap-3 px-1 text-xs font-medium text-slate-400">
                  <span>Gönderen domaini</span><span>Çıkış IP'si (havuzdan)</span><span></span>
                </div>
                {dedicated.map((d, i) => (
                  <div key={i} className="grid grid-cols-1 sm:grid-cols-[1fr_1fr_auto] gap-3 items-center">
                    <input value={d.domain} onChange={e => dedGuncelle(i, { domain: e.target.value })} placeholder="firma.com" className={inp} />
                    <select value={d.ip} onChange={e => dedGuncelle(i, { ip: e.target.value })} className={inp + ' font-mono'}>
                      {!aktifIP.includes(d.ip) && d.ip && <option value={d.ip}>{d.ip} (havuzda değil)</option>}
                      {aktifIP.map(ip => <option key={ip} value={ip}>{ip}</option>)}
                    </select>
                    <button onClick={() => dedSil(i)} className="text-sm text-red-500 hover:text-red-600 justify-self-start sm:justify-self-center px-2" title="Sil">✕</button>
                  </div>
                ))}
              </div>
            )}

            {aktifIP.length > 0 && (
              <div className="flex justify-end mt-4">
                <button onClick={dedKaydet} disabled={dedKaydediliyor}
                  className="bg-brand-600 hover:bg-brand-700 text-white text-sm font-medium px-5 py-2 rounded-lg disabled:opacity-60 transition-colors">
                  {dedKaydediliyor ? 'Uygulanıyor…' : 'Dedicated IP\'leri Kaydet'}
                </button>
              </div>
            )}
          </section>
        </div>
      )}
    </div>
  )
}
