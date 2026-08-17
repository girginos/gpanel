import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

type PortKayit = { port: number; protokol: string; aciklama: string; firewall_acik: boolean }
type Uygulama = {
  id: number; kod: string; ornek_ad: string; surum: string
  kurulum_yolu: string; sistem_kullanici: string; systemd_unit: string
  durum: string; son_hata: string; created_at: string
  portlar?: PortKayit[]; unit_durumu?: string
}
type Metrik = {
  bellek_byte: number; bellek_peak_byte: number
  cpu_yuzde: number; cpu_toplam_usec: number
  task_sayisi: number; task_max: number
  disk_byte: number
  aktif_durum: string; alt_durum: string
  uptime?: string; restart_sayi: number
}
type Yedek = {
  id: number; dosya: string; boyut_byte: number; sha256: string
  aciklama: string; olusturma: string
}

const byteFmt = (b: number): string => {
  if (!b) return '—'
  if (b < 1024) return b + 'B'
  if (b < 1024 * 1024) return (b / 1024).toFixed(1) + 'KB'
  if (b < 1024 * 1024 * 1024) return (b / 1024 / 1024).toFixed(1) + 'MB'
  return (b / 1024 / 1024 / 1024).toFixed(2) + 'GB'
}

export default function HostUygulamaDetayPage() {
  const { id } = useParams()
  const nav = useNavigate()
  const dialog = useDialog()
  const [uyg, setUyg] = useState<Uygulama | null>(null)
  const [metrik, setMetrik] = useState<Metrik | null>(null)
  const [yedekler, setYedekler] = useState<Yedek[]>([])
  const [log, setLog] = useState<string>('')
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [sekme, setSekme] = useState<'genel' | 'log' | 'yedek' | 'yonet'>('genel')
  const ozelYonetimVarmi = uyg && (uyg.kod === 'teamspeak3' || uyg.kod === 'headscale')
  const [islem, setIslem] = useState(false)

  const yukle = async () => {
    if (!id) return
    setHata(null)
    try {
      const [u, m, y] = await Promise.all([
        api.get<Uygulama>(`/hostuyg/${id}`),
        api.get<Metrik>(`/hostuyg/${id}/metrik`),
        api.get<{items: Yedek[]}>(`/hostuyg/${id}/yedekler`),
      ])
      setUyg(u.data); setMetrik(m.data); setYedekler(y.data.items || [])
    } catch (e) { setHata(apiHata(e, 'Yüklenemedi')) }
    finally { setYukleniyor(false) }
  }
  useEffect(() => { void yukle() }, [id])

  // Metrik 3s polling
  useEffect(() => {
    if (!uyg || sekme !== 'genel') return
    const t = setInterval(async () => {
      try {
        const m = await api.get<Metrik>(`/hostuyg/${uyg.id}/metrik`)
        setMetrik(m.data)
      } catch {}
    }, 3000)
    return () => clearInterval(t)
  }, [uyg?.id, sekme])

  const logYukle = async () => {
    if (!uyg) return
    try {
      const r = await api.get<{log: string}>(`/hostuyg/${uyg.id}/log?satir=200`)
      setLog(r.data.log || '(log boş)')
    } catch (e) { setLog('Log alınamadı: ' + apiHata(e, '')) }
  }
  useEffect(() => { if (sekme === 'log') void logYukle() }, [sekme, uyg?.id])

  const baslat = async () => { if (!uyg) return; setIslem(true); try { await api.post(`/hostuyg/${uyg.id}/baslat`, {}); await yukle() } finally { setIslem(false) } }
  const durdur = async () => { if (!uyg) return; setIslem(true); try { await api.post(`/hostuyg/${uyg.id}/durdur`, {}); await yukle() } finally { setIslem(false) } }
  const restart = async () => { if (!uyg) return; setIslem(true); try { await api.post(`/hostuyg/${uyg.id}/restart`, {}); await yukle() } finally { setIslem(false) } }

  const yedekAl = async () => {
    if (!uyg) return
    const ac = window.prompt('Yedek açıklaması (opsiyonel):', '')
    if (ac === null) return
    setIslem(true)
    try {
      const r = await api.post<{is_id: string}>(`/hostuyg/${uyg.id}/yedek`, { aciklama: ac })
      // 30dk polling
      for (let i = 0; i < 1200; i++) {
        await new Promise((r) => setTimeout(r, 1500))
        const s = await api.get<any>(`/hostuyg/is/${r.data.is_id}`)
        if (s.data.durum !== 'kosuyor') break
      }
      const y = await api.get<{items: Yedek[]}>(`/hostuyg/${uyg.id}/yedekler`)
      setYedekler(y.data.items || [])
    } catch (e) {
      await dialog.bilgi({ baslik: 'Yedek alınamadı', mesaj: apiHata(e, '') })
    } finally { setIslem(false) }
  }

  const yedekGeriYukle = async (y: Yedek) => {
    if (!uyg) return
    const ok = await dialog.onay({
      baslik: `${uyg.ornek_ad}'i yedeğe geri yükle?`,
      mesaj: `Servis durdurulur, kurulum ${new Date(y.olusturma).toLocaleString()} anına döner.`,
      onayEtiketi: 'Geri Yükle', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    setIslem(true)
    try {
      const r = await api.post<{is_id: string}>(`/hostuyg/${uyg.id}/yedek/geriyukle`, { yedek_id: y.id })
      for (let i = 0; i < 1200; i++) {
        await new Promise((r) => setTimeout(r, 1500))
        const s = await api.get<any>(`/hostuyg/is/${r.data.is_id}`)
        if (s.data.durum !== 'kosuyor') break
      }
      await yukle()
    } catch (e) { await dialog.bilgi({ baslik: 'Geri yükleme başarısız', mesaj: apiHata(e, '') }) }
    finally { setIslem(false) }
  }
  const yedekSil = async (y: Yedek) => {
    if (!(await dialog.onay({ baslik: 'Yedek silinsin mi?', mesaj: new Date(y.olusturma).toLocaleString(), onayEtiketi: 'Sil', iptalEtiketi: 'Vazgeç', tehlike: true }))) return
    try { await api.delete(`/hostuyg/yedek/${y.id}`); setYedekler(yedekler.filter((x) => x.id !== y.id)) }
    catch (e) { await dialog.bilgi({ baslik: 'Silinemedi', mesaj: apiHata(e, '') }) }
  }

  const kaldir = async () => {
    if (!uyg) return
    const ok = await dialog.onay({
      baslik: `${uyg.ornek_ad} kaldırılsın mı?`,
      mesaj: 'Servis durdurulur, sistem user + dizin + DB kaydı silinir, nginx proxy kaldırılır, firewall kural silinir.',
      onayEtiketi: 'Kaldır', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    try {
      await api.delete(`/hostuyg/${uyg.id}`)
      nav('/araclar/host-uygulamalari')
    } catch (e) { await dialog.bilgi({ baslik: 'Kaldırılamadı', mesaj: apiHata(e, '') }) }
  }

  if (yukleniyor) return <div className="p-8 text-center text-slate-500">Yükleniyor…</div>
  if (hata) return <div className="p-8 max-w-4xl mx-auto"><div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-red-700">{hata}</div></div>
  if (!uyg) return null

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { href: '/araclar-ayarlar', etiket: 'Araçlar ve Ayarlar' },
        { href: '/araclar/host-uygulamalari', etiket: 'Host Uygulamaları' },
        { etiket: uyg.ornek_ad },
      ]} />

      {/* Başlık kartı */}
      <div className="mt-4 mb-5 rounded-2xl border border-slate-200 bg-white p-5 dark:border-slate-800 dark:bg-slate-900">
        <div className="flex items-start justify-between gap-4">
          <div>
            <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100">{uyg.ornek_ad}</h1>
            <div className="mt-1 flex items-center gap-2 text-xs">
              <span className="font-mono text-slate-500">{uyg.kod}</span>
              <span className="text-slate-300">·</span>
              <span className="font-mono text-slate-500">v{uyg.surum}</span>
              <span className="text-slate-300">·</span>
              <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                uyg.durum === 'aktif'      ? 'bg-emerald-100 text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300' :
                uyg.durum === 'durduruldu' ? 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300' :
                                             'bg-red-100 text-red-800 dark:bg-red-950/40 dark:text-red-300'
              }`}>{uyg.durum}</span>
              {uyg.unit_durumu && uyg.unit_durumu !== uyg.durum && (
                <span className="text-[10px] text-slate-500">(systemd: {uyg.unit_durumu})</span>
              )}
            </div>
          </div>
          <div className="flex items-center gap-2">
            {uyg.durum === 'aktif' && (
              <>
                <button onClick={restart} disabled={islem} title="Restart"
                  className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs hover:bg-slate-100 dark:border-slate-700 dark:hover:bg-slate-800">↻ Restart</button>
                <button onClick={durdur} disabled={islem}
                  className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs hover:bg-slate-100 dark:border-slate-700 dark:hover:bg-slate-800">Durdur</button>
              </>
            )}
            {uyg.durum === 'durduruldu' && (
              <button onClick={baslat} disabled={islem}
                className="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-700 disabled:opacity-50">Başlat</button>
            )}
            {uyg.durum === 'hata' && (
              <button onClick={restart} disabled={islem}
                className="rounded-lg border border-amber-500 bg-amber-50 px-3 py-1.5 text-xs font-medium text-amber-800 hover:bg-amber-100">↻ Kurtarma dene</button>
            )}
            <button onClick={kaldir} disabled={islem}
              className="rounded-lg border border-red-300 px-3 py-1.5 text-xs font-medium text-red-600 hover:bg-red-50 dark:border-red-800 dark:hover:bg-red-950/40">Kaldır</button>
          </div>
        </div>
        <div className="mt-4 grid grid-cols-1 gap-2 text-xs text-slate-600 dark:text-slate-400 md:grid-cols-2">
          <div><span className="font-medium">Kurulum:</span> <span className="font-mono">{uyg.kurulum_yolu}</span></div>
          <div><span className="font-medium">Sistem user:</span> <span className="font-mono">{uyg.sistem_kullanici}</span></div>
          <div><span className="font-medium">Systemd:</span> <span className="font-mono">{uyg.systemd_unit}</span></div>
          <div><span className="font-medium">Portlar:</span> <span className="font-mono">{uyg.portlar?.map((p) => `${p.port}/${p.protokol}`).join(', ') || '—'}</span></div>
        </div>
        {uyg.son_hata && (
          <div className="mt-3 rounded-lg bg-red-50 p-2 text-xs text-red-700 dark:bg-red-950/30 dark:text-red-300">
            <span className="font-medium">Son hata:</span> {uyg.son_hata}
          </div>
        )}
      </div>

      {/* Sekmeler */}
      <div className="mb-4 flex items-center gap-5 border-b border-slate-200 dark:border-slate-700 flex-wrap">
        {(['genel', 'log', 'yedek', ...(ozelYonetimVarmi ? ['yonet' as const] : [])] as const).map((s) => (
          <button key={s} onClick={() => setSekme(s)}
            className={`pb-2 text-sm font-medium border-b-2 ${sekme === s ? 'border-slate-900 text-slate-900 dark:border-slate-100 dark:text-slate-100' : 'border-transparent text-slate-500 hover:text-slate-700 dark:hover:text-slate-300'}`}>
            {s === 'genel' ? 'Genel Durum' : s === 'log' ? 'Log' : s === 'yedek' ? `Yedekler (${yedekler.length})` :
              uyg.kod === 'teamspeak3' ? 'Voice Yönet' : uyg.kod === 'headscale' ? 'VPN Yönet' : 'Yönet'}
          </button>
        ))}
      </div>

      {/* İçerik */}
      {sekme === 'genel' && metrik && (
        <div>
          {/* Crash döngüsü uyarısı */}
          {metrik.restart_sayi > 5 && (
            <div className="mb-4 rounded-2xl border border-red-300 bg-gradient-to-r from-red-50 to-orange-50 p-4 dark:from-red-950/30 dark:to-orange-950/30 dark:border-red-800">
              <div className="flex items-start gap-3">
                <div className="text-2xl">⚠️</div>
                <div className="flex-1">
                  <div className="font-semibold text-red-900 dark:text-red-200">Crash döngüsü tespit edildi</div>
                  <div className="text-xs text-red-700 dark:text-red-300 mt-0.5">
                    Servis {metrik.restart_sayi} kez restart etti. Log sekmesinden hata sebebini incele.
                  </div>
                </div>
                <button onClick={() => setSekme('log')}
                  className="rounded-lg bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700">
                  Log'a git →
                </button>
              </div>
            </div>
          )}

          {/* Metrik grid — büyük 4 ana + küçük 3 ek */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4 mb-3">
            <MetrikKartV2 etiket="Bellek" ikon="🧠" deger={byteFmt(metrik.bellek_byte)}
              altBilgi={metrik.bellek_peak_byte > 0 ? `peak ${byteFmt(metrik.bellek_peak_byte)}` : undefined}
              renk="blue"
              trend={metrik.bellek_byte > 0 && metrik.bellek_peak_byte > 0
                ? Math.round((metrik.bellek_byte / metrik.bellek_peak_byte) * 100) : undefined} />
            <MetrikKartV2 etiket="CPU" ikon="⚡" deger={`${metrik.cpu_yuzde.toFixed(2)}%`}
              altBilgi={`${(metrik.cpu_toplam_usec / 1e6).toFixed(1)}s toplam`}
              renk={metrik.cpu_yuzde > 80 ? 'red' : metrik.cpu_yuzde > 40 ? 'amber' : 'emerald'}
              trend={metrik.cpu_yuzde} />
            <MetrikKartV2 etiket="Task" ikon="🧵"
              deger={`${metrik.task_sayisi}`}
              altBilgi={`limit ${metrik.task_max < 0 ? '∞' : metrik.task_max || '—'}`}
              renk="violet"
              trend={metrik.task_max > 0 ? (metrik.task_sayisi / metrik.task_max) * 100 : undefined} />
            <MetrikKartV2 etiket="Disk" ikon="💾" deger={byteFmt(metrik.disk_byte)}
              altBilgi="5dk cache" renk="slate" />
          </div>

          {/* Alt satır — durum + uptime + restart */}
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <DurumKart etiket="Durum" aktif={metrik.aktif_durum} alt={metrik.alt_durum} />
            <MetrikKartV2 etiket="Uptime" ikon="⏱️" deger={metrik.uptime || '—'} renk="slate" />
            <MetrikKartV2 etiket="Restart" ikon="🔄" deger={String(metrik.restart_sayi)}
              altBilgi="systemd NRestarts"
              renk={metrik.restart_sayi > 5 ? 'red' : metrik.restart_sayi > 0 ? 'amber' : 'emerald'} />
          </div>

          {/* Canlı badge */}
          <div className="mt-4 flex items-center justify-center gap-2 text-xs text-slate-500">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse" />
            Canlı — 3s'de bir yenilenir
          </div>
        </div>
      )}

      {sekme === 'log' && (
        <div>
          <div className="mb-2 flex items-center justify-between">
            <span className="text-xs text-slate-500">Son 200 satır · journalctl</span>
            <button onClick={logYukle} className="text-xs text-slate-600 hover:bg-slate-100 px-2 py-1 rounded">Yenile</button>
          </div>
          <pre className="max-h-[70vh] overflow-auto whitespace-pre-wrap break-all rounded-lg bg-slate-950 p-3 text-[11px] font-mono leading-tight text-slate-100">{log || 'Yükleniyor…'}</pre>
        </div>
      )}

      {sekme === 'yonet' && uyg.kod === 'headscale' && <HeadscaleYonet uygID={uyg.id} />}
      {sekme === 'yonet' && uyg.kod === 'teamspeak3' && <TeamSpeakYonet uygID={uyg.id} />}

      {sekme === 'yedek' && (
        <div>
          <div className="mb-3 flex items-center justify-between">
            <span className="text-xs text-slate-500">Son 5 yedek tutulur (rotasyon)</span>
            <button onClick={yedekAl} disabled={islem}
              className="rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900">
              {islem ? 'İşleniyor…' : '+ Yedek Al'}
            </button>
          </div>
          {yedekler.length === 0 ? (
            <div className="rounded-lg border border-dashed border-slate-300 py-8 text-center text-sm text-slate-500 dark:border-slate-700">Henüz yedek yok</div>
          ) : (
            <div className="overflow-hidden rounded-lg border border-slate-200 dark:border-slate-800">
              <table className="w-full text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900">
                  <tr>
                    <th className="px-3 py-2">Tarih</th>
                    <th className="px-3 py-2">Boyut</th>
                    <th className="px-3 py-2">Açıklama</th>
                    <th className="px-3 py-2 text-right">İşlem</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                  {yedekler.map((y) => (
                    <tr key={y.id} className="bg-white dark:bg-slate-950">
                      <td className="px-3 py-2 font-mono text-xs">{new Date(y.olusturma).toLocaleString('tr-TR')}</td>
                      <td className="px-3 py-2 font-mono text-xs">{byteFmt(y.boyut_byte)}</td>
                      <td className="px-3 py-2 text-xs text-slate-600">{y.aciklama || '—'}</td>
                      <td className="px-3 py-2 text-right">
                        <button onClick={() => yedekGeriYukle(y)} disabled={islem}
                          className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-emerald-600 hover:bg-emerald-50 dark:text-emerald-400 dark:hover:bg-emerald-950/40 disabled:opacity-50">Geri Yükle</button>
                        <button onClick={() => yedekSil(y)}
                          className="rounded-md px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/40">Sil</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/* ---------- Headscale VPN Yönet ---------- */
function HeadscaleYonet({ uygID }: { uygID: number }) {
  const [users, setUsers] = useState<any[]>([])
  const [nodes, setNodes] = useState<any[]>([])
  const [yeniUser, setYeniUser] = useState('')
  const [preauthUser, setPreauthUser] = useState('')
  const [preauthEphemeral, setPreauthEphemeral] = useState(false)
  const [preauthKey, setPreauthKey] = useState<string | null>(null)
  const [islem, setIslem] = useState(false)
  const [hata, setHata] = useState<string | null>(null)

  const eylem = async (fn: string, args: any = {}): Promise<any> => {
    const r = await api.post<any>(`/hostuyg/${uygID}/eylem`, { fn, args })
    return r.data
  }
  const yukle = async () => {
    setHata(null)
    try {
      const [u, n] = await Promise.all([
        eylem('user_list'), eylem('node_list'),
      ])
      setUsers(u.users || [])
      setNodes(n.nodes || [])
    } catch (e: any) { setHata(e?.response?.data?.error || 'Yüklenemedi') }
  }
  useEffect(() => { void yukle() }, [uygID])

  const userCreate = async () => {
    if (!yeniUser) return
    setIslem(true); setHata(null)
    try { await eylem('user_create', { ad: yeniUser }); setYeniUser(''); await yukle() }
    catch (e: any) { setHata(e?.response?.data?.error || 'Oluşturma başarısız') }
    finally { setIslem(false) }
  }
  const userDelete = async (ad: string) => {
    if (!confirm(`Kullanıcı "${ad}" silinsin mi? Node'ları da yetim kalır.`)) return
    setIslem(true)
    try { await eylem('user_delete', { ad }); await yukle() }
    catch (e: any) { setHata(e?.response?.data?.error || '') }
    finally { setIslem(false) }
  }
  const nodeDelete = async (id: number, ad: string) => {
    if (!confirm(`Node "${ad}" (id=${id}) silinsin mi?`)) return
    setIslem(true)
    try { await eylem('node_delete', { id }); await yukle() }
    catch (e: any) { setHata(e?.response?.data?.error || '') }
    finally { setIslem(false) }
  }
  const preauthCreate = async () => {
    if (!preauthUser) return
    setIslem(true); setPreauthKey(null)
    try {
      const r = await eylem('preauthkey_create', { user: preauthUser, reusable: false, ephemeral: preauthEphemeral, expiry: '24h' })
      setPreauthKey(r.key)
    } catch (e: any) { setHata(e?.response?.data?.error || '') }
    finally { setIslem(false) }
  }

  return (
    <div className="space-y-5">
      {hata && <div className="rounded-lg bg-red-50 border border-red-200 p-3 text-sm text-red-700">{hata}</div>}

      <section className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <h3 className="mb-3 text-sm font-semibold uppercase tracking-wider text-slate-500">Kullanıcılar ({users.length})</h3>
        <div className="mb-3 flex items-center gap-2">
          <input value={yeniUser} onChange={(e) => setYeniUser(e.target.value)} placeholder="yeni kullanıcı adı"
            className="flex-1 rounded-lg border border-slate-300 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-950" />
          <button onClick={userCreate} disabled={islem || !yeniUser}
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900">+ Oluştur</button>
        </div>
        {users.length === 0 ? (
          <div className="py-4 text-center text-sm text-slate-500">Kullanıcı yok. Yukarıdan oluştur.</div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-slate-500"><tr><th className="py-2">Ad</th><th className="py-2">ID</th><th className="py-2">Oluşturma</th><th className="py-2 text-right">İşlem</th></tr></thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {users.map((u) => (
                <tr key={u.id}>
                  <td className="py-2 font-medium">{u.name}</td>
                  <td className="py-2 font-mono text-xs text-slate-500">{u.id}</td>
                  <td className="py-2 text-xs text-slate-500">{u.createdAt || '—'}</td>
                  <td className="py-2 text-right">
                    <button onClick={() => userDelete(u.name)} disabled={islem}
                      className="rounded-md text-xs text-red-600 hover:bg-red-50 px-2 py-1">Sil</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <h3 className="mb-3 text-sm font-semibold uppercase tracking-wider text-slate-500">Preauth Key (Client bağlama)</h3>
        <div className="flex items-center gap-2 mb-3 flex-wrap">
          <select value={preauthUser} onChange={(e) => setPreauthUser(e.target.value)}
            className="rounded-lg border border-slate-300 px-3 py-2 text-sm dark:border-slate-700 dark:bg-slate-950">
            <option value="">Kullanıcı seç</option>
            {users.map((u) => <option key={u.id} value={u.name}>{u.name}</option>)}
          </select>
          <label className="text-xs flex items-center gap-1"><input type="checkbox" checked={preauthEphemeral} onChange={(e) => setPreauthEphemeral(e.target.checked)} /> ephemeral</label>
          <button onClick={preauthCreate} disabled={islem || !preauthUser}
            className="rounded-lg bg-slate-900 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900">Key Üret (24h)</button>
        </div>
        {preauthKey && (
          <div className="rounded-lg bg-emerald-50 border border-emerald-200 p-3 dark:bg-emerald-950/30 dark:border-emerald-800">
            <div className="text-xs font-medium text-emerald-900 mb-1 dark:text-emerald-200">Client komut:</div>
            <pre className="text-xs font-mono text-emerald-900 whitespace-pre-wrap break-all dark:text-emerald-100">tailscale up --login-server https://LAB4.GIRGINOS.APP:8443/headscale --authkey {preauthKey}</pre>
          </div>
        )}
      </section>

      <section className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <h3 className="mb-3 text-sm font-semibold uppercase tracking-wider text-slate-500">Node'lar ({nodes.length})</h3>
        {nodes.length === 0 ? (
          <div className="py-4 text-center text-sm text-slate-500">Node yok. Client'lar preauth key ile katıldıkça görünür.</div>
        ) : (
          <table className="w-full text-left text-sm">
            <thead className="text-xs uppercase tracking-wider text-slate-500"><tr><th className="py-2">Ad</th><th className="py-2">User</th><th className="py-2">IP</th><th className="py-2">Online</th><th className="py-2 text-right">İşlem</th></tr></thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {nodes.map((n) => (
                <tr key={n.id}>
                  <td className="py-2 font-medium">{n.givenName || n.name}</td>
                  <td className="py-2 text-xs text-slate-500">{n.user?.name || '—'}</td>
                  <td className="py-2 font-mono text-xs">{(n.ipAddresses || []).join(', ')}</td>
                  <td className="py-2 text-xs">{n.online ? <span className="text-emerald-600">●</span> : <span className="text-slate-400">●</span>}</td>
                  <td className="py-2 text-right">
                    <button onClick={() => nodeDelete(n.id, n.givenName)} disabled={islem}
                      className="rounded-md text-xs text-red-600 hover:bg-red-50 px-2 py-1">Sil</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  )
}

/* ---------- TeamSpeak 3 Voice Yönet ---------- */
function TeamSpeakYonet({ uygID }: { uygID: number }) {
  const [clients, setClients] = useState<any[]>([])
  const [bilgi, setBilgi] = useState<any>(null)
  const [kimlik, setKimlik] = useState<any>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [islem, setIslem] = useState(false)
  const [tokenAcik, setTokenAcik] = useState(false)
  const [kopyalandi, setKopyalandi] = useState<string | null>(null)

  const eylem = async (fn: string, args: any = {}): Promise<any> => {
    const r = await api.post<any>(`/hostuyg/${uygID}/eylem`, { fn, args })
    return r.data
  }
  const yukle = async () => {
    setHata(null)
    try {
      const [b, c] = await Promise.all([eylem('sunucu_bilgi'), eylem('client_liste')])
      setBilgi(b.parsed || {})
      // client_type: 0=voice, 1=serverquery — panel'in kendi bağlantısını gizle
      setClients((c.clients || []).filter((cl: any) => cl.clid && cl.client_type !== '1'))
    } catch (e: any) { setHata(e?.response?.data?.error || 'ServerQuery bağlantısı kurulamadı') }
  }
  const kimlikYukle = async () => {
    try { setKimlik(await eylem('kimlik')) } catch { /* ignore */ }
  }
  useEffect(() => {
    void yukle(); void kimlikYukle()
    const t = setInterval(yukle, 8000)
    return () => clearInterval(t)
  }, [uygID])

  const kopyala = async (val: string, etiket: string) => {
    try { await navigator.clipboard.writeText(val); setKopyalandi(etiket); setTimeout(() => setKopyalandi(null), 1500) } catch {/* */}
  }
  const kick = async (clid: string, nickname: string) => {
    const sebep = window.prompt(`${nickname} kick sebebi:`, 'panel kick')
    if (sebep === null) return
    setIslem(true)
    try { await eylem('client_kick', { CLID: clid, Sebep: sebep }); await yukle() }
    catch (e: any) { setHata(e?.response?.data?.error || 'Kick başarısız') }
    finally { setIslem(false) }
  }
  const yeniToken = async () => {
    if (!confirm('Yeni ServerAdmin privilege key üret?')) return
    setIslem(true)
    try {
      const r = await eylem('token_yenile')
      if (r?.token) {
        setKimlik((k: any) => ({ ...(k || {}), admin_token: r.token }))
        setTokenAcik(true)
      }
    } catch (e: any) { setHata(e?.response?.data?.error || 'Token üretilemedi') }
    finally { setIslem(false) }
  }

  const durum = bilgi?.virtualserver_status || 'unknown'
  const durumRengi = durum === 'online' ? 'emerald' : 'red'
  const uptimeSaat = bilgi?.virtualserver_uptime ? Number(bilgi.virtualserver_uptime) : 0
  const uptimeStr = uptimeSaat > 0 ? formatUptime(uptimeSaat) : '—'
  const onlineDoluluk = bilgi ? (Number(bilgi.virtualserver_clientsonline || 0) / Number(bilgi.virtualserver_maxclients || 32)) * 100 : 0

  return (
    <div className="space-y-5">
      {/* Hata */}
      {hata && (
        <div className="rounded-2xl border border-red-200 bg-gradient-to-r from-red-50 to-orange-50 p-4 dark:from-red-950/30 dark:to-orange-950/30 dark:border-red-800">
          <div className="flex items-start gap-3">
            <div className="text-xl shrink-0">🔴</div>
            <div className="flex-1 text-sm">
              <div className="font-semibold text-red-900 dark:text-red-200">ServerQuery bağlantısı başarısız</div>
              <div className="mt-1 text-red-700 dark:text-red-300">{hata}</div>
              <div className="mt-2 text-xs text-red-600 dark:text-red-400">
                Servis çalışıyor mu? Log sekmesinden hatayı incele.
              </div>
            </div>
            <button onClick={yukle} className="rounded-lg bg-red-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-red-700">Tekrar dene</button>
          </div>
        </div>
      )}

      {/* Serveradmin kimlik kartı */}
      {kimlik?.query_sifre && (
        <section className="rounded-2xl border border-violet-200 bg-gradient-to-br from-violet-50 to-white p-4 dark:border-violet-900/50 dark:from-violet-950/30 dark:to-slate-900">
          <div className="mb-3 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="w-8 h-8 rounded-lg bg-violet-100 dark:bg-violet-900/40 flex items-center justify-center text-lg">🔑</div>
              <h3 className="text-sm font-semibold text-violet-900 dark:text-violet-200">Server Query kimlikleri</h3>
            </div>
            <button onClick={yeniToken} disabled={islem}
              className="rounded-lg bg-violet-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-violet-700 disabled:opacity-50">
              Yeni admin token
            </button>
          </div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-sm">
            <KimlikSatir etiket="Kullanıcı" deger={kimlik.query_kullanici || 'serveradmin'} onCopy={kopyala} kopyalandi={kopyalandi === 'user'} kopyaAd="user" />
            <KimlikSatir etiket="Şifre" deger={kimlik.query_sifre} gizli onCopy={kopyala} kopyalandi={kopyalandi === 'pwd'} kopyaAd="pwd" />
            {kimlik.admin_token && (
              <div className="sm:col-span-2 rounded-lg bg-white/70 dark:bg-slate-800/50 border border-violet-200 dark:border-violet-900/50 p-3">
                <div className="flex items-center justify-between mb-1">
                  <div className="text-[11px] uppercase tracking-wider text-violet-700 dark:text-violet-300">ServerAdmin privilege key</div>
                  <div className="flex gap-2">
                    <button onClick={() => setTokenAcik(a => !a)} className="text-[11px] text-violet-700 hover:underline">{tokenAcik ? 'Gizle' : 'Göster'}</button>
                    <button onClick={() => kopyala(kimlik.admin_token, 'tok')} className="text-[11px] text-violet-700 hover:underline">{kopyalandi === 'tok' ? '✓ kopyalandı' : 'Kopyala'}</button>
                  </div>
                </div>
                <div className="font-mono text-xs break-all text-slate-800 dark:text-slate-200">
                  {tokenAcik ? kimlik.admin_token : '•'.repeat(Math.min(40, kimlik.admin_token.length))}
                </div>
                <div className="mt-1 text-[10px] text-violet-600 dark:text-violet-400">
                  Client'ta bağlan → Permissions → Use Privilege Key
                </div>
              </div>
            )}
          </div>
        </section>
      )}

      {/* Sunucu Bilgi — dashboard cards */}
      {bilgi && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <DashKart ikon="🎙️" etiket="Durum" deger={durum} renk={durumRengi as RenkTip}
            altBilgi={bilgi.virtualserver_platform || ''} />
          <DashKart ikon="👥" etiket="Online" deger={`${bilgi.virtualserver_clientsonline || 0}/${bilgi.virtualserver_maxclients || 32}`}
            renk={onlineDoluluk > 80 ? 'red' : onlineDoluluk > 50 ? 'amber' : 'emerald'} trend={onlineDoluluk} />
          <DashKart ikon="⏱️" etiket="Uptime" deger={uptimeStr} renk="blue"
            altBilgi={`v${(bilgi.virtualserver_version || '').split(' ')[0] || '—'}`} />
          <DashKart ikon="🔊" etiket="Kanal / Ping" deger={`${bilgi.virtualserver_channelsonline || 0} kanal`}
            renk="violet" altBilgi={`ort ping ${Math.round(Number(bilgi.virtualserver_total_ping || 0))}ms`} />
        </div>
      )}

      {bilgi && (
        <section className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <h3 className="mb-3 text-xs font-semibold uppercase tracking-wider text-slate-500">Detaylı sunucu bilgisi</h3>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-1.5 text-sm">
            <SatirBilgi etiket="Sunucu adı" deger={ts3Decode(bilgi.virtualserver_name)} />
            <SatirBilgi etiket="Unique ID" deger={bilgi.virtualserver_unique_identifier} mono />
            <SatirBilgi etiket="Voice port" deger={bilgi.virtualserver_port} />
            <SatirBilgi etiket="Codec encryption" deger={bilgi.virtualserver_codec_encryption_mode === '2' ? 'per-channel' : (bilgi.virtualserver_codec_encryption_mode === '1' ? 'globally on' : 'globally off')} />
            <SatirBilgi etiket="Anti-flood puanı" deger={`tick ${bilgi.virtualserver_antiflood_points_tick_reduce || '—'} / ban ${bilgi.virtualserver_antiflood_points_needed_command_block || '—'}`} />
            <SatirBilgi etiket="Karşılama" deger={ts3Decode(bilgi.virtualserver_welcomemessage)} />
          </div>
        </section>
      )}

      {/* Bağlı kullanıcılar */}
      <section className="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="mb-3 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-emerald-100 dark:bg-emerald-900/40 flex items-center justify-center">👥</div>
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Bağlı kullanıcılar
              <span className="ml-2 rounded-full bg-emerald-100 text-emerald-800 text-xs px-2 py-0.5 dark:bg-emerald-900/40 dark:text-emerald-200">{clients.length}</span>
            </h3>
          </div>
          <div className="flex items-center gap-2 text-xs text-slate-500">
            <span className="inline-block h-1.5 w-1.5 rounded-full bg-emerald-500 animate-pulse" />
            8s
            <button onClick={yukle} className="ml-2 text-emerald-700 hover:underline">Yenile</button>
          </div>
        </div>
        {clients.length === 0 ? (
          <div className="py-8 text-center">
            <div className="text-3xl mb-2 opacity-40">🎧</div>
            <div className="text-sm text-slate-500">Şu an kimse bağlı değil</div>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="text-[11px] uppercase tracking-wider text-slate-500 border-b border-slate-200 dark:border-slate-800">
                <tr>
                  <th className="py-2 pr-4">Kullanıcı</th>
                  <th className="py-2 pr-4">Platform</th>
                  <th className="py-2 pr-4">Bağlantı süresi</th>
                  <th className="py-2 pr-4">Ping</th>
                  <th className="py-2 text-right">İşlem</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                {clients.map((c) => (
                  <tr key={c.clid} className="hover:bg-slate-50 dark:hover:bg-slate-800/50">
                    <td className="py-2.5 pr-4">
                      <div className="font-medium text-slate-900 dark:text-slate-100">{ts3Decode(c.client_nickname) || '—'}</div>
                      <div className="text-[10px] text-slate-500 font-mono">clid {c.clid} · cid {c.cid}</div>
                    </td>
                    <td className="py-2.5 pr-4 text-xs text-slate-600 dark:text-slate-400">{c.client_platform || '—'}</td>
                    <td className="py-2.5 pr-4 text-xs font-mono">{c.connection_connected_time ? Math.round(Number(c.connection_connected_time) / 1000 / 60) + 'dk' : '—'}</td>
                    <td className="py-2.5 pr-4 text-xs font-mono">{c.connection_ping ? Math.round(Number(c.connection_ping)) + 'ms' : '—'}</td>
                    <td className="py-2.5 text-right">
                      <button onClick={() => kick(c.clid, ts3Decode(c.client_nickname) || 'client')} disabled={islem}
                        className="rounded-md text-xs text-red-600 hover:bg-red-50 dark:hover:bg-red-950/30 px-2.5 py-1 border border-red-200 dark:border-red-900/50 disabled:opacity-40">
                        Kick
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}

function KimlikSatir({ etiket, deger, gizli, onCopy, kopyalandi, kopyaAd }: {
  etiket: string; deger: string; gizli?: boolean; onCopy: (v: string, k: string) => void; kopyalandi: boolean; kopyaAd: string
}) {
  const [ac, setAc] = useState(false)
  return (
    <div className="rounded-lg bg-white/70 dark:bg-slate-800/50 border border-violet-200 dark:border-violet-900/50 p-2.5">
      <div className="flex items-center justify-between mb-1">
        <div className="text-[10px] uppercase tracking-wider text-violet-700 dark:text-violet-300">{etiket}</div>
        <div className="flex gap-2">
          {gizli && <button onClick={() => setAc(a => !a)} className="text-[10px] text-violet-700 hover:underline">{ac ? 'Gizle' : 'Göster'}</button>}
          <button onClick={() => onCopy(deger, kopyaAd)} className="text-[10px] text-violet-700 hover:underline">{kopyalandi ? '✓ kopyalandı' : 'Kopyala'}</button>
        </div>
      </div>
      <div className="font-mono text-sm text-slate-800 dark:text-slate-200">{gizli && !ac ? '•'.repeat(Math.min(16, deger.length)) : deger}</div>
    </div>
  )
}

function DashKart({ ikon, etiket, deger, renk = 'slate', altBilgi, trend }: {
  ikon: string; etiket: string; deger: string | number; renk?: RenkTip; altBilgi?: string; trend?: number
}) {
  const s = renkStyles[renk]
  return (
    <div className={`rounded-2xl border ${s.border} ${s.bg} p-4`}>
      <div className="flex items-center justify-between mb-2">
        <div className={`w-9 h-9 rounded-lg ${s.ikon} flex items-center justify-center text-lg`}>{ikon}</div>
        <div className="text-[10px] uppercase tracking-wider text-slate-500">{etiket}</div>
      </div>
      <div className={`text-xl font-bold ${s.deger} tabular-nums`}>{deger}</div>
      {altBilgi && <div className="text-[10px] text-slate-500 mt-0.5 truncate">{altBilgi}</div>}
      {trend !== undefined && trend >= 0 && (
        <div className="mt-2 h-1 rounded-full bg-slate-200 dark:bg-slate-800 overflow-hidden">
          <div className={`h-full ${s.bar}`} style={{ width: `${Math.min(100, trend)}%` }} />
        </div>
      )}
    </div>
  )
}

function SatirBilgi({ etiket, deger, mono }: { etiket: string; deger: any; mono?: boolean }) {
  const v = deger === undefined || deger === null || deger === '' ? '—' : String(deger)
  return (
    <div className="flex items-baseline gap-2 border-b border-slate-100 dark:border-slate-800 py-1">
      <span className="text-slate-500 shrink-0 text-xs">{etiket}:</span>
      <span className={`text-slate-800 dark:text-slate-200 truncate ${mono ? 'font-mono text-xs' : ''}`}>{v}</span>
    </div>
  )
}

function ts3Decode(s: any): string {
  if (!s) return ''
  return String(s).replace(/\\s/g, ' ').replace(/\\p/g, '|').replace(/\\\//g, '/').replace(/\\\\/g, '\\')
}

function formatUptime(sn: number): string {
  const g = Math.floor(sn / 86400); const s = Math.floor((sn % 86400) / 3600); const d = Math.floor((sn % 3600) / 60)
  if (g > 0) return `${g}g ${s}s`
  if (s > 0) return `${s}s ${d}dk`
  return `${d}dk`
}

function MetrikKart({ etiket, deger, altBilgi }: { etiket: string; deger: string; altBilgi?: string }) {
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900">
      <div className="text-[11px] uppercase tracking-wider text-slate-500">{etiket}</div>
      <div className="mt-1 font-mono text-lg font-semibold text-slate-900 dark:text-slate-100">{deger}</div>
      {altBilgi && <div className="text-[10px] text-slate-500">{altBilgi}</div>}
    </div>
  )
}

/* Kaliteli metrik kart — ikon + trend bar + renk-kod */
type RenkTip = 'blue' | 'emerald' | 'amber' | 'red' | 'violet' | 'slate'
const renkStyles: Record<RenkTip, { border: string; bg: string; ikon: string; deger: string; bar: string }> = {
  blue:    { border: 'border-blue-200 dark:border-blue-900/50',       bg: 'bg-gradient-to-br from-blue-50 to-white dark:from-blue-950/30 dark:to-slate-900',       ikon: 'bg-blue-100 dark:bg-blue-900/40',       deger: 'text-blue-950 dark:text-blue-100',       bar: 'bg-blue-500' },
  emerald: { border: 'border-emerald-200 dark:border-emerald-900/50', bg: 'bg-gradient-to-br from-emerald-50 to-white dark:from-emerald-950/30 dark:to-slate-900', ikon: 'bg-emerald-100 dark:bg-emerald-900/40', deger: 'text-emerald-950 dark:text-emerald-100', bar: 'bg-emerald-500' },
  amber:   { border: 'border-amber-200 dark:border-amber-900/50',     bg: 'bg-gradient-to-br from-amber-50 to-white dark:from-amber-950/30 dark:to-slate-900',     ikon: 'bg-amber-100 dark:bg-amber-900/40',     deger: 'text-amber-950 dark:text-amber-100',     bar: 'bg-amber-500' },
  red:     { border: 'border-red-200 dark:border-red-900/50',         bg: 'bg-gradient-to-br from-red-50 to-white dark:from-red-950/30 dark:to-slate-900',         ikon: 'bg-red-100 dark:bg-red-900/40',         deger: 'text-red-950 dark:text-red-100',         bar: 'bg-red-500' },
  violet:  { border: 'border-violet-200 dark:border-violet-900/50',   bg: 'bg-gradient-to-br from-violet-50 to-white dark:from-violet-950/30 dark:to-slate-900',   ikon: 'bg-violet-100 dark:bg-violet-900/40',   deger: 'text-violet-950 dark:text-violet-100',   bar: 'bg-violet-500' },
  slate:   { border: 'border-slate-200 dark:border-slate-800',        bg: 'bg-white dark:bg-slate-900',                                                            ikon: 'bg-slate-100 dark:bg-slate-800',        deger: 'text-slate-900 dark:text-slate-100',     bar: 'bg-slate-500' },
}

function MetrikKartV2({ etiket, ikon, deger, altBilgi, renk = 'slate', trend }: {
  etiket: string; ikon: string; deger: string; altBilgi?: string; renk?: RenkTip; trend?: number
}) {
  const s = renkStyles[renk]
  return (
    <div className={`rounded-2xl border ${s.border} ${s.bg} p-4 transition-all hover:shadow-md`}>
      <div className="flex items-start justify-between gap-2 mb-2">
        <div className={`w-9 h-9 rounded-lg ${s.ikon} flex items-center justify-center text-lg shrink-0`}>{ikon}</div>
        <div className="text-[10px] uppercase tracking-wider text-slate-500 text-right">{etiket}</div>
      </div>
      <div className={`text-2xl font-bold ${s.deger} tabular-nums`}>{deger}</div>
      {altBilgi && <div className="text-[11px] text-slate-500 mt-0.5">{altBilgi}</div>}
      {trend !== undefined && trend >= 0 && (
        <div className="mt-2 h-1.5 rounded-full bg-slate-200 dark:bg-slate-800 overflow-hidden">
          <div className={`h-full ${s.bar} transition-all duration-500`} style={{ width: `${Math.min(100, trend)}%` }} />
        </div>
      )}
    </div>
  )
}

function DurumKart({ etiket, aktif, alt }: { etiket: string; aktif: string; alt: string }) {
  const durumRenkleri: Record<string, { border: string; bg: string; nokta: string; yazi: string }> = {
    active:      { border: 'border-emerald-200 dark:border-emerald-900/50', bg: 'bg-gradient-to-br from-emerald-50 to-white dark:from-emerald-950/30 dark:to-slate-900', nokta: 'bg-emerald-500', yazi: 'text-emerald-800 dark:text-emerald-200' },
    activating:  { border: 'border-amber-200 dark:border-amber-900/50',     bg: 'bg-gradient-to-br from-amber-50 to-white dark:from-amber-950/30 dark:to-slate-900',     nokta: 'bg-amber-500 animate-pulse', yazi: 'text-amber-800 dark:text-amber-200' },
    inactive:    { border: 'border-slate-200 dark:border-slate-800',        bg: 'bg-slate-50 dark:bg-slate-900',                                                        nokta: 'bg-slate-400', yazi: 'text-slate-700 dark:text-slate-300' },
    failed:      { border: 'border-red-200 dark:border-red-900/50',         bg: 'bg-gradient-to-br from-red-50 to-white dark:from-red-950/30 dark:to-slate-900',         nokta: 'bg-red-500', yazi: 'text-red-800 dark:text-red-200' },
    deactivating:{ border: 'border-amber-200 dark:border-amber-900/50',     bg: 'bg-gradient-to-br from-amber-50 to-white dark:from-amber-950/30 dark:to-slate-900',     nokta: 'bg-amber-500', yazi: 'text-amber-800 dark:text-amber-200' },
  }
  const r = durumRenkleri[aktif] || durumRenkleri.inactive
  return (
    <div className={`rounded-2xl border ${r.border} ${r.bg} p-4`}>
      <div className="flex items-center justify-between mb-2">
        <div className="w-9 h-9 rounded-lg bg-white/70 dark:bg-slate-800/70 flex items-center justify-center text-lg">📡</div>
        <div className="text-[10px] uppercase tracking-wider text-slate-500">{etiket}</div>
      </div>
      <div className="flex items-center gap-2">
        <span className={`inline-block h-2.5 w-2.5 rounded-full ${r.nokta}`} />
        <div className={`text-lg font-bold ${r.yazi} capitalize`}>{aktif}</div>
      </div>
      {alt && alt !== aktif && (
        <div className="text-[11px] text-slate-500 mt-0.5">{alt}</div>
      )}
    </div>
  )
}
