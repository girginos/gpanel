import type { FormEvent } from 'react'
import { useEffect, useState } from 'react'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useDialog } from '@/components/Dialog'

/*
 * IP Yönetimi — admin only.
 *
 * Sunucuya ek IP adresleri ekle/sil. Runtime ip addr add + systemd oneshot
 * unit ile reboot sonrası restore.
 *
 * 🔴 GÜVENLIK: sadece panel-etiketli IP'ler silinebilir. Kullanıcının kendi
 * ISP-yerleşik primary IP'si SİLİNMEZ, arayüzde tabloda "silinemez" olarak
 * işaretlenir.
 */

type SunucuIP = {
  ip: string; iface: string; cidr: number; label: string
  panel_ip: boolean; silinebilir: boolean; primary_mi: boolean
}
type DBKayit = { id: number; ip: string; iface: string; cidr: number; note: string; created_at: string }
type Liste = { sunucu_ipler: SunucuIP[]; db_kayitlar: DBKayit[]; varsayilan_arayuz: string }

export default function IPYonetimPage() {
  const [liste, setListe] = useState<Liste | null>(null)
  const [yukleniyor, setYukleniyor] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [taslakIP, setTaslakIP] = useState('')
  const [taslakIface, setTaslakIface] = useState('')
  const [taslakCidr, setTaslakCidr] = useState(32)
  const [taslakNot, setTaslakNot] = useState('')
  const [gonderiliyor, setGonderiliyor] = useState(false)
  const [ekleHata, setEkleHata] = useState<string | null>(null)
  const dialog = useDialog()

  const yukle = async () => {
    setYukleniyor(true); setHata(null)
    try {
      const r = await api.get<Liste>('/ipler')
      setListe(r.data)
      if (!taslakIface) setTaslakIface(r.data.varsayilan_arayuz || '')
    } catch (e) { setHata(apiHata(e, 'Yüklenemedi')) }
    finally { setYukleniyor(false) }
  }
  useEffect(() => { void yukle() }, [])

  const ekle = async (ev: FormEvent) => {
    ev.preventDefault(); setEkleHata(null)
    if (!taslakIP.trim()) { setEkleHata('IP boş olamaz'); return }
    setGonderiliyor(true)
    try {
      await api.post('/ipler', { ip: taslakIP.trim(), iface: taslakIface, cidr: taslakCidr, note: taslakNot.trim() })
      setTaslakIP(''); setTaslakNot('')
      await yukle()
    } catch (e) { setEkleHata(apiHata(e, 'Eklenemedi')) }
    finally { setGonderiliyor(false) }
  }

  const sil = async (s: SunucuIP) => {
    if (!s.silinebilir) return
    const ok = await dialog.onay({
      baslik: `${s.ip} silinsin mi?`,
      mesaj: `Bu IP sunucudan kaldırılacak (${s.iface}/${s.cidr}). Bu IP'ye yönlendirilen tüm trafik düşer. Reboot sonrası da yüklenmez.`,
      onayEtiketi: 'Sil', iptalEtiketi: 'Vazgeç', tehlike: true,
    })
    if (!ok) return
    try {
      await api.delete('/ipler/' + encodeURIComponent(s.ip))
      await yukle()
    } catch (e) {
      await dialog.bilgi({ baslik: 'Silinemedi', mesaj: apiHata(e, 'Silme hatası') })
    }
  }

  return (
    <div className="mx-auto max-w-5xl px-4 py-6 md:px-6 md:py-8">
      <Breadcrumb items={[
        { href: '/araclar-ayarlar', etiket: 'Araçlar ve Ayarlar' },
        { etiket: 'IP Yönetimi' },
      ]} />

      <div className="mt-4 mb-6">
        <h1 className="text-xl font-semibold tracking-tight text-slate-900 dark:text-slate-100">IP Yönetimi</h1>
        <p className="mt-1.5 text-sm text-slate-600 dark:text-slate-400">
          Sunucuya ek IP adresi ekle. Reboot sonrası otomatik geri yüklenir.
          <span className="ml-1 font-medium text-slate-800 dark:text-slate-200">
            Kullanıcı IP'leri ve primary IP silinemez.
          </span>
        </p>
      </div>

      {yukleniyor ? (
        <div className="rounded-2xl border border-slate-200 py-10 text-center text-sm text-slate-500 dark:border-slate-800">Yükleniyor…</div>
      ) : hata ? (
        <div className="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700">{hata}</div>
      ) : liste && (
        <>
          {/* Ekleme formu */}
          <form onSubmit={ekle} className="mb-6 rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(180px,1fr)_140px_100px_1fr_auto]">
              <div>
                <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">IPv4</label>
                <input value={taslakIP} onChange={(e) => setTaslakIP(e.target.value)}
                  placeholder="148.251.169.182" spellCheck={false} autoComplete="off"
                  className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-mono text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">Arayüz</label>
                <input value={taslakIface} onChange={(e) => setTaslakIface(e.target.value)}
                  placeholder="pub0" spellCheck={false}
                  className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 font-mono text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">CIDR</label>
                <input type="number" value={taslakCidr} onChange={(e) => setTaslakCidr(Number(e.target.value))}
                  min={1} max={32}
                  className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100" />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-slate-600 dark:text-slate-400">Açıklama</label>
                <input value={taslakNot} onChange={(e) => setTaslakNot(e.target.value)}
                  placeholder="ör. mail dış IP havuzu" maxLength={255}
                  className="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm outline-none focus:border-slate-500 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100" />
              </div>
              <div className="flex items-end">
                <button type="submit" disabled={gonderiliyor || !taslakIP.trim()}
                  className="h-[38px] rounded-lg bg-slate-900 px-4 text-sm font-medium text-white transition-colors hover:bg-slate-800 disabled:opacity-50 dark:bg-slate-100 dark:text-slate-900 dark:hover:bg-white">
                  {gonderiliyor ? 'Ekleniyor…' : 'Ekle'}
                </button>
              </div>
            </div>
            {ekleHata && <p className="mt-3 text-sm text-red-600 dark:text-red-400">{ekleHata}</p>}
          </form>

          {/* Sunucu IP tablosu — TÜM IP'ler; renk kodları rolleri gösterir */}
          <div className="overflow-hidden rounded-2xl border border-slate-200 dark:border-slate-800">
            <div className="overflow-x-auto">
              <table className="min-w-[720px] w-full text-left text-sm">
                <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                  <tr>
                    <th className="px-3 py-3 font-semibold">IP</th>
                    <th className="px-3 py-3 font-semibold">Arayüz / CIDR</th>
                    <th className="px-3 py-3 font-semibold">Rol</th>
                    <th className="px-3 py-3 font-semibold">Label</th>
                    <th className="px-3 py-3 text-right font-semibold">İşlem</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
                  {liste.sunucu_ipler.map((s) => (
                    <tr key={s.ip} className="bg-white dark:bg-slate-950">
                      <td className="px-3 py-2.5 font-mono">{s.ip}</td>
                      <td className="px-3 py-2.5 font-mono text-slate-600 dark:text-slate-400">{s.iface} / {s.cidr}</td>
                      <td className="px-3 py-2.5">
                        {s.primary_mi ? (
                          <span className="rounded-full bg-blue-100 px-2 py-0.5 text-[11px] font-medium text-blue-800 dark:bg-blue-950/40 dark:text-blue-300">primary</span>
                        ) : s.panel_ip ? (
                          <span className="rounded-full bg-emerald-100 px-2 py-0.5 text-[11px] font-medium text-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300">panel</span>
                        ) : (
                          <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[11px] font-medium text-slate-700 dark:bg-slate-800 dark:text-slate-300">kullanıcı</span>
                        )}
                      </td>
                      <td className="px-3 py-2.5 font-mono text-xs text-slate-500">{s.label || '—'}</td>
                      <td className="px-3 py-2.5 text-right">
                        {s.silinebilir ? (
                          <button onClick={() => sil(s)}
                            className="rounded-md px-2.5 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-950/40">
                            Sil
                          </button>
                        ) : (
                          <span className="text-xs text-slate-400" title={s.primary_mi ? 'Primary IP silinemez' : 'Panel ekletmediği için silinemez'}>silinemez</span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>

          <p className="mt-4 text-xs text-slate-500">
            {liste.sunucu_ipler.length} IP · Varsayılan arayüz: <span className="font-mono">{liste.varsayilan_arayuz || '—'}</span> · Reboot sonrası panel-etiketli IP'ler otomatik geri yüklenir
          </p>
        </>
      )}
    </div>
  )
}
