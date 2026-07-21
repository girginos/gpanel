import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Durum = {
  alan_adi: string
  kullanici: string
  aktif: boolean
  shell: string
  ssh_host: string
  ssh_port: number
  anahtar_var: boolean
  is_demo: boolean
}

export default function DomainSSHPage() {
  const { id } = useParams()
  const [d, setD] = useState<Durum | null>(null)
  const [yuk, setYuk] = useState(true)
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [anahtar, setAnahtar] = useState('')

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    api.get<Durum>(`/domains/${id}/ssh`)
      .then(r => setD(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  useEffect(yukle, [id])

  async function toggle(aktif: boolean) {
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      await api.put(`/domains/${id}/ssh`, { aktif })
      setBasari(aktif ? 'SSH erişimi açıldı.' : 'SSH erişimi kapatıldı.')
      setTimeout(() => setBasari(null), 4000)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'İşlem başarısız'))
    } finally { setIsleniyor(false) }
  }

  async function anahtarKaydet() {
    setIsleniyor(true); setHata(null); setBasari(null)
    try {
      const { data } = await api.put(`/domains/${id}/ssh/anahtar`, { anahtar })
      setBasari(data.anahtar_var ? '✓ SSH anahtarı kaydedildi.' : '✓ SSH anahtarları temizlendi.')
      setTimeout(() => setBasari(null), 4000)
      setAnahtar('')
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Anahtar kaydedilemedi'))
    } finally { setIsleniyor(false) }
  }

  if (yuk) return <div className="px-6 py-5 text-slate-400">Yükleniyor…</div>
  if (!d) return <div className="px-6 py-5"><div className="text-sm text-red-600">{hata || 'Bulunamadı'}</div></div>

  const sshKomut = `ssh ${d.kullanici}@${d.ssh_host} -p ${d.ssh_port}`

  return (
    <div className="px-6 py-5">
      <div className="max-w-3xl mx-auto">
        <Breadcrumb items={[
          { etiket: 'Anasayfa', href: '/' },
          { etiket: 'Domainler', href: '/domainler' },
          { etiket: d.alan_adi, href: `/abonelikler/${id}` },
          { etiket: 'SSH Erişimi' },
        ]} />

        <div className="flex items-start justify-between gap-4 mb-1">
          <div>
            <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">SSH Erişimi</h1>
            <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">
              <span className="font-mono">{d.alan_adi}</span> hesabı için kabuk (shell) erişimi.
            </p>
          </div>
          <span className={`shrink-0 inline-flex items-center gap-1.5 text-xs font-semibold px-2.5 py-1 rounded-full ${
            d.aktif ? 'bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300' : 'bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-300'
          }`}>
            <span className={`w-2 h-2 rounded-full ${d.aktif ? 'bg-emerald-500' : 'bg-slate-400'}`} />
            {d.aktif ? 'SSH AÇIK' : 'SSH KAPALI'}
          </span>
        </div>

        {hata && <div className="my-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300 whitespace-pre-wrap">{hata}</div>}
        {basari && <div className="my-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-lg text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

        {/* Durum + toggle */}
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4 shadow-sm">
          <div className="flex items-center justify-between gap-4">
            <div>
              <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Kabuk Erişimi</h3>
              <p className="text-xs text-slate-500 dark:text-slate-400 mt-1">
                Açık: <code className="font-mono">/bin/bash</code> · Kapalı: <code className="font-mono">/usr/sbin/nologin</code>.
                Mevcut: <code className="font-mono">{d.shell || '—'}</code>
              </p>
            </div>
            {d.aktif ? (
              <button onClick={() => toggle(false)} disabled={isleniyor || d.is_demo}
                className="shrink-0 px-4 py-2 border border-red-300 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 disabled:opacity-50 text-sm font-medium rounded-lg">
                SSH'i Kapat
              </button>
            ) : (
              <button onClick={() => toggle(true)} disabled={isleniyor || d.is_demo}
                className="shrink-0 px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-lg">
                SSH'i Aç
              </button>
            )}
          </div>
          {d.is_demo && <p className="mt-3 text-xs text-amber-600 dark:text-amber-400">Demo domainde SSH değiştirilemez.</p>}
        </div>

        {/* Bağlantı bilgisi */}
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 mb-4 shadow-sm">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Bağlantı Bilgisi</h3>
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
            <Bilgi etiket="Kullanıcı" deger={d.kullanici} />
            <Bilgi etiket="Sunucu" deger={d.ssh_host} />
            <Bilgi etiket="Port" deger={String(d.ssh_port)} />
          </div>
          <div className="mt-3">
            <label className="text-xs font-medium text-slate-600 dark:text-slate-400">Bağlantı komutu</label>
            <div className="mt-1 flex items-center gap-2">
              <code className="flex-1 px-3 py-2 bg-slate-900 text-slate-100 rounded-lg text-xs font-mono overflow-x-auto">{sshKomut}</code>
              <button onClick={() => navigator.clipboard?.writeText(sshKomut)} className="shrink-0 text-xs px-2.5 py-2 border border-slate-300 dark:border-slate-600 rounded-lg hover:bg-slate-50 dark:hover:bg-slate-700">Kopyala</button>
            </div>
          </div>
          <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">🔑 Parola: <strong>FTP hesabınızla aynı</strong> — SSH açıkken otomatik eşitlenir. Alternatif olarak aşağıya SSH genel anahtarı ekleyebilirsiniz.</p>
        </div>

        {/* SSH Public Key */}
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-5 shadow-sm">
          <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1">SSH Genel Anahtarı (authorized_keys)</h3>
          <p className="text-xs text-slate-500 dark:text-slate-400 mb-3">
            Parola yerine anahtarla giriş için genel anahtarınızı ekleyin. {d.anahtar_var
              ? <span className="text-emerald-600 dark:text-emerald-400">Şu an bir anahtar tanımlı.</span>
              : <span className="text-slate-500">Henüz anahtar tanımlı değil.</span>}
          </p>
          <textarea
            value={anahtar}
            onChange={e => setAnahtar(e.target.value)}
            rows={4}
            spellCheck={false}
            placeholder="ssh-ed25519 AAAA... kullanici@makine"
            className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded-lg text-xs font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
          />
          <div className="mt-3 flex items-center justify-between">
            <p className="text-xs text-slate-400">Boş bırakıp kaydederseniz tüm anahtarlar silinir.</p>
            <button onClick={anahtarKaydet} disabled={isleniyor || d.is_demo}
              className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-lg">
              Anahtarı Kaydet
            </button>
          </div>
        </div>

        <div className="mt-4">
          <Link to={`/abonelikler/${id}`} className="text-sm text-brand-600 dark:text-brand-400">← Aboneliğe dön</Link>
        </div>
      </div>
    </div>
  )
}

function Bilgi({ etiket, deger }: { etiket: string; deger: string }) {
  return (
    <div className="px-3 py-2 bg-slate-50 dark:bg-slate-900/40 rounded-lg border border-slate-200 dark:border-slate-700">
      <div className="text-[10px] uppercase tracking-wider text-slate-400">{etiket}</div>
      <div className="font-mono text-slate-800 dark:text-slate-200 truncate">{deger}</div>
    </div>
  )
}
