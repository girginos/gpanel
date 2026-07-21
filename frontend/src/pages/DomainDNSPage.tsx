// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import Modal from '@/components/Modal'
import ConfirmDialog from '@/components/ConfirmDialog'

type Kayit = {
  id: number
  domain_id: number
  ad: string
  tip: string
  deger: string
  ttl: number
  oncelik: number
  aktif: boolean
  olusturma: string
}

type Domain = { id: number; alan_adi: string; ipv4: string }

type DNSSEC = { aktif: boolean; imzali: boolean; ds: string[]; durum: string }

const TIPLER = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA', 'PTR', 'DS', 'TLSA', 'SSHFP', 'NAPTR']

// Her tip için Değer alanına gösterilecek format ipucu
const DEGER_IPUCU: Record<string, string> = {
  A:     'IPv4 adresi — ör. 203.0.113.10',
  AAAA:  'IPv6 adresi — ör. 2a01:4f8:1c1c::1',
  CNAME: 'Hedef alan adı — ör. hedef.example.com',
  MX:    'Mail sunucusu — ör. mail.example.com (öncelik ayrı alanda)',
  TXT:   'Serbest metin — ör. v=spf1 mx ~all',
  NS:    'Ad sunucusu — ör. ns1.example.com',
  SRV:   'ağırlık port hedef — ör. 5 5060 sip.example.com (öncelik ayrı alanda)',
  CAA:   'flags tag "değer" — ör. 0 issue "letsencrypt.org"',
  PTR:   'Hedef alan adı — ör. host.example.com',
  DS:    'keytag alg digest-tip digest — ör. 12345 13 2 49FD46E6C4B45C55D4AC…',
  TLSA:  'kullanım seçici eşleşme veri — ör. 3 1 1 0B9FA5A59EED715C26C1020C…',
  SSHFP: 'algoritma tip parmak-izi — ör. 4 2 123456789ABCDEF…',
  NAPTR: 'order pref "flags" "servis" "regexp" replacement',
}

export default function DomainDNSPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [kayitlar, setKayitlar] = useState<Kayit[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [duzenle, setDuzenle] = useState<Kayit | null>(null)
  const [silinecek, setSilinecek] = useState<Kayit | null>(null)
  const [secili, setSecili] = useState<Set<number>>(new Set())
  const [topluSilOnay, setTopluSilOnay] = useState(false)
  const [soa, setSoa] = useState<{ primary_ns: string; hostmaster: string; refresh: number; retry: number; expire: number; minimum: number; ttl: number } | null>(null)
  const [soaAcik, setSoaAcik] = useState(false)
  const [soaKaydediyor, setSoaKaydediyor] = useState(false)
  const [dnssec, setDnssec] = useState<DNSSEC | null>(null)
  const [dnssecIsliyor, setDnssecIsliyor] = useState(false)
  const [dnssecKapatOnay, setDnssecKapatOnay] = useState(false)
  const [dsKopyalandi, setDsKopyalandi] = useState(false)

  function yukle() {
    if (!id) return
    setYuk(true); setHata(null)
    api.get<Kayit[]>(`/domains/${id}/dns`)
      .then(r => { setKayitlar(r.data); setSecili(new Set()) })
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }

  function secimDegistir(rid: number) {
    setSecili(prev => {
      const n = new Set(prev)
      if (n.has(rid)) n.delete(rid); else n.add(rid)
      return n
    })
  }
  function hepsiniSec() {
    setSecili(prev => prev.size === kayitlar.length ? new Set() : new Set(kayitlar.map(k => k.id)))
  }

  async function topluSil() {
    if (!id || secili.size === 0) return
    setHata(null); setBasari(null); setTopluSilOnay(false)
    try {
      const { data } = await api.post(`/domains/${id}/dns/toplu-sil`, { ids: [...secili] })
      setBasari(`${data.silinen} kayıt silindi`)
      yukle()
    } catch (e) { setHata(apiHata(e, 'Toplu silme başarısız')) }
  }
  async function topluDurum(aktif: boolean) {
    if (!id || secili.size === 0) return
    setHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/domains/${id}/dns/toplu-durum`, { ids: [...secili], aktif })
      setBasari(`${data.guncellenen} kayıt ${aktif ? 'aktif' : 'pasif'} yapıldı`)
      yukle()
    } catch (e) { setHata(apiHata(e, 'Toplu güncelleme başarısız')) }
  }
  useEffect(() => {
    if (id) {
      api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(() => {})
      api.get<typeof soa>(`/domains/${id}/dns/soa`).then(r => setSoa(r.data)).catch(() => {})
      api.get<DNSSEC>(`/domains/${id}/dns/dnssec`).then(r => setDnssec(r.data)).catch(() => {})
    }
    yukle()
  }, [id])

  async function dnssecDegistir(aktif: boolean) {
    if (!id) return
    setHata(null); setBasari(null); setDnssecKapatOnay(false); setDnssecIsliyor(true)
    try {
      const { data } = await api.post<DNSSEC>(`/domains/${id}/dns/dnssec`, { aktif })
      setDnssec(data)
      setBasari(aktif
        ? 'DNSSEC etkinleştirildi. Aşağıdaki DS kaydını alan adı operatörünüze (registrar) girin.'
        : 'DNSSEC kapatıldı.')
    } catch (e) { setHata(apiHata(e, 'DNSSEC güncellenemedi')) }
    finally { setDnssecIsliyor(false) }
  }
  async function dnssecDurumYenile() {
    if (!id) return
    try { const { data } = await api.get<DNSSEC>(`/domains/${id}/dns/dnssec`); setDnssec(data) } catch { /* yut */ }
  }

  async function soaKaydet(e: React.FormEvent) {
    e.preventDefault()
    if (!id || !soa) return
    setHata(null); setBasari(null); setSoaKaydediyor(true)
    try {
      const { data } = await api.put(`/domains/${id}/dns/soa`, soa)
      setSoa(data)
      setBasari('SOA ayarları kaydedildi ve zone yeniden yazıldı.')
    } catch (e) { setHata(apiHata(e, 'SOA kaydedilemedi')) }
    finally { setSoaKaydediyor(false) }
  }

  async function sablonUygula() {
    if (!id) return
    setHata(null); setBasari(null)
    try {
      const { data } = await api.post(`/domains/${id}/dns/sablon`)
      setBasari(`${data.eklenen} varsayılan kayıt eklendi`)
      yukle()
    } catch (e) {
      setHata(apiHata(e, 'Şablon uygulanamadı'))
    }
  }

  async function sil() {
    if (!silinecek || !id) return
    try {
      await api.delete(`/domains/${id}/dns/${silinecek.id}`)
      setSilinecek(null); yukle()
    } catch (e) {
      alert(apiHata(e, 'Silme başarısız'))
    }
  }

  return (
    <div className="px-6 py-5 max-w-[1300px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'DNS Ayarları' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">DNS Ayarları</h1>
      {domain && (
        <p className="text-sm text-slate-500 dark:text-slate-500 mb-5">
          <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link>
          {' · '}IP: <span className="font-mono">{domain.ipv4}</span>
        </p>
      )}

      <div className="bg-sky-50 dark:bg-sky-900/20 border border-sky-200 dark:border-sky-800 rounded-md px-3 py-2 text-xs text-sky-800 dark:text-sky-200 mb-4">
        <strong>Bilgi:</strong> Bu sunucu <strong>authoritative DNS</strong>'tir (BIND) — kayıtlar kaydedildiği anda yayınlanır. Domainin çalışması için alan adı operatöründe NS kayıtlarını bu sunucunun <span className="font-mono">ns1.{domain?.alan_adi || 'alaniniz'}</span> / <span className="font-mono">ns2.{domain?.alan_adi || 'alaniniz'}</span> adreslerine yönlendirin.
      </div>

      {soa && (
        <div className="border border-slate-200 dark:border-slate-800 rounded-xl mb-4 overflow-hidden">
          <button onClick={() => setSoaAcik(v => !v)} className="w-full flex items-center justify-between px-4 py-2.5 text-sm font-medium text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-800/50 transition">
            <span>⚙️ SOA Ayarları <span className="text-xs text-slate-400 font-normal">(başlangıç yetki kaydı — refresh/retry/expire/NS)</span></span>
            <span className="text-slate-400 text-xs">{soaAcik ? '▲ gizle' : '▼ düzenle'}</span>
          </button>
          {soaAcik && (
            <form onSubmit={soaKaydet} className="px-4 pb-4 pt-3 grid grid-cols-2 md:grid-cols-4 gap-3 border-t border-slate-100 dark:border-slate-800">
              <label className="col-span-2">
                <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold">Birincil NS</span>
                <input value={soa.primary_ns} onChange={e => setSoa({ ...soa, primary_ns: e.target.value })}
                  className="mt-1 w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded text-sm font-mono outline-none focus:border-brand-500" />
              </label>
              <label className="col-span-2">
                <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold">Hostmaster (e-posta)</span>
                <input value={soa.hostmaster} onChange={e => setSoa({ ...soa, hostmaster: e.target.value })} placeholder="admin@alan.com"
                  className="mt-1 w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded text-sm font-mono outline-none focus:border-brand-500" />
              </label>
              {(['refresh', 'retry', 'expire', 'minimum', 'ttl'] as const).map(f => (
                <label key={f}>
                  <span className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold">{f} (sn)</span>
                  <input type="number" min={0} value={soa[f]} onChange={e => setSoa({ ...soa, [f]: parseInt(e.target.value) || 0 })}
                    className="mt-1 w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 dark:bg-slate-900 rounded text-sm font-mono outline-none focus:border-brand-500" />
                </label>
              ))}
              <div className="col-span-2 md:col-span-4 flex justify-end">
                <button disabled={soaKaydediyor} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-md disabled:opacity-50">
                  {soaKaydediyor ? 'Kaydediliyor…' : 'SOA Kaydet'}
                </button>
              </div>
            </form>
          )}
        </div>
      )}

      {dnssec && (
        <div className="border border-slate-200 dark:border-slate-800 rounded-xl mb-4 overflow-hidden">
          <div className="flex items-center justify-between px-4 py-3 gap-3 flex-wrap">
            <div>
              <div className="text-sm font-medium text-slate-700 dark:text-slate-200 flex items-center gap-2">
                🔐 DNSSEC
                {dnssec.aktif ? (
                  dnssec.imzali
                    ? <span className="text-xs px-1.5 py-0.5 rounded bg-emerald-100 dark:bg-emerald-900/30 text-emerald-700 dark:text-emerald-300 font-medium">imzalı</span>
                    : <span className="text-xs px-1.5 py-0.5 rounded bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 font-medium">imzalanıyor…</span>
                ) : (
                  <span className="text-xs px-1.5 py-0.5 rounded bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 font-medium">kapalı</span>
                )}
              </div>
              <p className="text-xs text-slate-400 dark:text-slate-500 mt-0.5">Zone imzalama (BIND inline-signing). Açtıktan sonra oluşan DS kaydını alan adı operatörünüze girin.</p>
            </div>
            <div className="flex items-center gap-2">
              {dnssec.aktif && (
                <button onClick={dnssecDurumYenile} className="px-2.5 py-1.5 text-xs bg-white dark:bg-slate-800 hover:bg-slate-50 dark:hover:bg-slate-700 border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 rounded-md transition">↻ Durum</button>
              )}
              {dnssec.aktif ? (
                <button disabled={dnssecIsliyor} onClick={() => setDnssecKapatOnay(true)} className="px-3 py-1.5 text-sm bg-white dark:bg-slate-800 border border-red-200 dark:border-red-800 text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30 rounded-md transition disabled:opacity-50">Kapat</button>
              ) : (
                <button disabled={dnssecIsliyor} onClick={() => dnssecDegistir(true)} className="px-3 py-1.5 text-sm bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 font-medium rounded-md transition disabled:opacity-50">{dnssecIsliyor ? 'Etkinleştiriliyor…' : 'Etkinleştir'}</button>
              )}
            </div>
          </div>
          {dnssec.aktif && (
            <div className="px-4 pb-4 border-t border-slate-100 dark:border-slate-800 pt-3">
              {dnssec.ds && dnssec.ds.length > 0 ? (
                <div>
                  <div className="text-[11px] uppercase tracking-wide text-slate-400 font-semibold mb-1">DS Kaydı — registrar'a girin</div>
                  {dnssec.ds.map((d, i) => (
                    <div key={i} className="flex items-center gap-2 mb-1">
                      <code className="flex-1 text-xs font-mono bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-700 rounded px-2 py-1 break-all text-slate-800 dark:text-slate-200">{d}</code>
                      <button onClick={() => { navigator.clipboard?.writeText(d); setDsKopyalandi(true); setTimeout(() => setDsKopyalandi(false), 1500) }}
                        className="px-2 py-1 text-xs bg-white dark:bg-slate-800 hover:bg-slate-50 dark:hover:bg-slate-700 border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 rounded transition whitespace-nowrap">{dsKopyalandi ? '✓ Kopyalandı' : 'Kopyala'}</button>
                    </div>
                  ))}
                  <p className="text-[11px] text-slate-400 dark:text-slate-500 mt-1">Bu DS kaydını alan adınızın operatöründe (registrar) DNSSEC/DS alanına girin. Yayılması TTL süresi kadar sürebilir.</p>
                </div>
              ) : (
                <p className="text-xs text-amber-600 dark:text-amber-400">İmzalama sürüyor; DS kaydı henüz hazır değil. Birkaç saniye sonra “↻ Durum”a basın.</p>
              )}
              {dnssec.durum && (
                <pre className="mt-2 text-[10px] font-mono text-slate-500 dark:text-slate-400 bg-slate-50 dark:bg-slate-900 border border-slate-200 dark:border-slate-800 rounded p-2 overflow-x-auto max-h-44">{dnssec.durum}</pre>
              )}
            </div>
          )}
        </div>
      )}

      <div className="flex items-center gap-2 mb-4">
        <button
          onClick={() => setDuzenle({} as Kayit)}
          className="inline-flex items-center gap-1.5 px-3.5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-md shadow-sm transition"
        >
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={2.5}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
          </svg>
          Kayıt Ekle
        </button>
        <button
          onClick={sablonUygula}
          className="px-3 py-2 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 text-sm rounded-md transition"
          title="A/MX/TXT/NS varsayılan kayıtlarını ekler (idempotent)"
        >
          📋 Varsayılan Şablonu Uygula
        </button>
        <button onClick={yukle} className="px-3 py-2 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 text-sm rounded-md transition">↻ Yenile</button>
        <span className="ml-auto text-sm text-slate-500 dark:text-slate-500">{kayitlar.length} kayıt</span>
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}
      {basari && <div className="mb-3 px-3 py-2 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md text-sm text-emerald-700 dark:text-emerald-300">{basari}</div>}

      {secili.size > 0 && (
        <div className="mb-3 px-3 py-2 bg-brand-50 dark:bg-brand-900/20 border border-brand-200 dark:border-brand-800 rounded-md flex items-center gap-2 flex-wrap">
          <span className="text-sm font-medium text-brand-800 dark:text-brand-200">{secili.size} kayıt seçildi</span>
          <div className="ml-auto flex items-center gap-2 flex-wrap">
            <button onClick={() => topluDurum(true)} className="px-3 py-1.5 text-sm bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md text-emerald-700 dark:text-emerald-300 hover:bg-emerald-50 dark:hover:bg-emerald-900/30 transition">Aktif Yap</button>
            <button onClick={() => topluDurum(false)} className="px-3 py-1.5 text-sm bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-md text-slate-600 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-700 transition">Pasif Yap</button>
            <button onClick={() => setTopluSilOnay(true)} className="px-3 py-1.5 text-sm bg-red-600 hover:bg-red-700 text-white rounded-md transition">Seçilenleri Sil ({secili.size})</button>
            <button onClick={() => setSecili(new Set())} className="px-2 py-1.5 text-sm text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200 transition">Seçimi Temizle</button>
          </div>
        </div>
      )}

      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden">
        {yuk ? (
          <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div>
        ) : kayitlar.length === 0 ? (
          <div className="py-12 text-center">
            <p className="text-sm text-slate-500 dark:text-slate-500 mb-3">Henüz DNS kaydı yok.</p>
            <button onClick={sablonUygula} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 text-sm font-medium rounded-md">
              Varsayılan Şablonu Uygula
            </button>
          </div>
        ) : (
          <table className="w-full">
            <thead className="bg-slate-50 dark:bg-slate-900 text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500 border-b border-slate-200 dark:border-slate-700">
              <tr>
                <th className="px-4 py-2.5 w-10">
                  <input type="checkbox" aria-label="Tümünü seç" checked={kayitlar.length > 0 && secili.size === kayitlar.length}
                    ref={el => { if (el) el.indeterminate = secili.size > 0 && secili.size < kayitlar.length }}
                    onChange={hepsiniSec} className="rounded border-slate-300 dark:border-slate-600 cursor-pointer" />
                </th>
                <th className="text-left px-4 py-2.5">Ad</th>
                <th className="text-left px-4 py-2.5">Tip</th>
                <th className="text-left px-4 py-2.5">Değer</th>
                <th className="text-left px-4 py-2.5">TTL</th>
                <th className="text-left px-4 py-2.5">Öncelik</th>
                <th className="text-left px-4 py-2.5">Durum</th>
                <th className="text-right px-4 py-2.5">İşlemler</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
              {kayitlar.map(k => (
                <tr key={k.id} className={secili.has(k.id) ? 'bg-brand-50/60 dark:bg-brand-900/10' : 'hover:bg-slate-50 dark:hover:bg-slate-800/60'}>
                  <td className="px-4 py-2.5">
                    <input type="checkbox" aria-label={`${k.ad} ${k.tip} seç`} checked={secili.has(k.id)} onChange={() => secimDegistir(k.id)}
                      className="rounded border-slate-300 dark:border-slate-600 cursor-pointer" />
                  </td>
                  <td className="px-4 py-2.5 text-sm font-mono">{k.ad}</td>
                  <td className="px-4 py-2.5">
                    <span className="text-xs px-1.5 py-0.5 bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300 rounded font-mono font-semibold">{k.tip}</span>
                  </td>
                  <td className="px-4 py-2.5 text-sm font-mono text-slate-800 dark:text-slate-200 break-all">{k.deger}</td>
                  <td className="px-4 py-2.5 text-sm font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">{k.ttl}</td>
                  <td className="px-4 py-2.5 text-sm font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">{k.tip === 'MX' || k.tip === 'SRV' ? k.oncelik : '—'}</td>
                  <td className="px-4 py-2.5">
                    {k.aktif ? (
                      <span className="text-xs text-emerald-700 dark:text-emerald-300 inline-flex items-center gap-1"><span className="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>Aktif</span>
                    ) : (
                      <span className="text-xs text-slate-500 dark:text-slate-500">Pasif</span>
                    )}
                  </td>
                  <td className="px-4 py-2.5 text-right space-x-1">
                    <button onClick={() => setDuzenle(k)} className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500 hover:text-slate-900 dark:hover:text-slate-100 dark:text-slate-100 px-2 py-1 rounded hover:bg-slate-100 dark:bg-slate-800 dark:hover:bg-slate-800">Düzenle</button>
                    <button onClick={() => setSilinecek(k)} className="text-sm text-red-600 dark:text-red-400 hover:text-red-700 dark:text-red-300 px-2 py-1 rounded hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20">Sil</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {duzenle && (
        <KayitModal
          mevcut={duzenle}
          domainId={Number(id)}
          ipv4={domain?.ipv4 || ''}
          onKapat={() => setDuzenle(null)}
          onKayit={() => { setDuzenle(null); yukle() }}
        />
      )}

      <ConfirmDialog
        acik={!!silinecek}
        baslik="DNS kaydını sil"
        mesaj={`"${silinecek?.ad} ${silinecek?.tip} ${silinecek?.deger.slice(0,40)}" silinsin mi?`}
        tehlikeli
        onayMetni="Evet, sil"
        onOnay={sil}
        onIptal={() => setSilinecek(null)}
      />

      <ConfirmDialog
        acik={topluSilOnay}
        baslik="Seçili DNS kayıtlarını sil"
        mesaj={`${secili.size} DNS kaydı kalıcı olarak silinecek. Bu işlem geri alınamaz. Devam edilsin mi?`}
        tehlikeli
        onayMetni={`Evet, ${secili.size} kaydı sil`}
        onOnay={topluSil}
        onIptal={() => setTopluSilOnay(false)}
      />

      <ConfirmDialog
        acik={dnssecKapatOnay}
        baslik="DNSSEC'i kapat"
        mesaj="ÖNEMLİ: DNSSEC'i kapatmadan ÖNCE alan adı operatörünüzden (registrar) DS kaydını SİLİN ve TTL süresi kadar bekleyin. Aksi halde alan adınız çözümlenemez hale gelir (SERVFAIL). DS kaydını sildiyseniz devam edin."
        tehlikeli
        onayMetni="DS'i sildim, kapat"
        onOnay={() => dnssecDegistir(false)}
        onIptal={() => setDnssecKapatOnay(false)}
      />
    </div>
  )
}

function KayitModal({ mevcut, domainId, ipv4, onKapat, onKayit }: {
  mevcut: Kayit; domainId: number; ipv4: string; onKapat: () => void; onKayit: () => void
}) {
  const yeni = !mevcut.id
  const [form, setForm] = useState<Kayit>({
    id: mevcut.id || 0,
    domain_id: domainId,
    ad: mevcut.ad || '@',
    tip: mevcut.tip || 'A',
    deger: mevcut.deger || ipv4,
    ttl: mevcut.ttl || 3600,
    oncelik: mevcut.oncelik || 0,
    aktif: mevcut.aktif !== false,
    olusturma: '',
  })
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)

  async function gonder(e: React.FormEvent) {
    e.preventDefault()
    setIsleniyor(true); setHata(null)
    try {
      if (yeni) await api.post(`/domains/${domainId}/dns`, form)
      else      await api.put(`/domains/${domainId}/dns/${form.id}`, form)
      onKayit()
    } catch (e) {
      setHata(apiHata(e, 'Kayıt başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={true} baslik={yeni ? 'Yeni DNS Kaydı' : 'DNS Kaydını Düzenle'} onKapat={onKapat} genislik="md">
      <form onSubmit={gonder} className="space-y-3">
        <div className="grid grid-cols-3 gap-3">
          <div className="col-span-2">
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Ad (alt-ad)</label>
            <input type="text" value={form.ad} onChange={e => setForm({ ...form, ad: e.target.value })} required
              className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
            <p className="text-[10px] text-slate-500 dark:text-slate-500 mt-0.5">"@" = ana domain, "www", "mail" vs.</p>
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Tip</label>
            <select value={form.tip} onChange={e => { const t = e.target.value; setForm(f => ({ ...f, tip: t, oncelik: (t === 'MX' || t === 'SRV') ? (f.oncelik || 10) : 0 })) }}
              className="w-full px-2 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono bg-white dark:bg-slate-800">
              {TIPLER.map(t => <option key={t} value={t}>{t}</option>)}
            </select>
          </div>
        </div>

        <div>
          <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Değer</label>
          <input type="text" value={form.deger} onChange={e => setForm({ ...form, deger: e.target.value })} required
            className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none" />
          {DEGER_IPUCU[form.tip] && <p className="text-[10px] text-slate-500 dark:text-slate-500 mt-0.5">{DEGER_IPUCU[form.tip]}</p>}
        </div>

        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">TTL (sn)</label>
            <input type="number" min={60} value={form.ttl} onChange={e => setForm({ ...form, ttl: parseInt(e.target.value) || 3600 })}
              className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono" />
          </div>
          {(form.tip === 'MX' || form.tip === 'SRV') && (
            <div>
              <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Öncelik</label>
              <input type="number" min={0} value={form.oncelik} onChange={e => setForm({ ...form, oncelik: parseInt(e.target.value) || 0 })}
                className="w-full px-3 py-1.5 border border-slate-300 dark:border-slate-600 rounded text-sm font-mono" />
            </div>
          )}
        </div>

        <label className="flex items-center gap-2 text-sm text-slate-700 dark:text-slate-300 cursor-pointer">
          <input type="checkbox" checked={form.aktif} onChange={e => setForm({ ...form, aktif: e.target.checked })} className="rounded" />
          Aktif
        </label>

        {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-sm text-red-700 dark:text-red-300">{hata}</div>}

        <div className="flex justify-end gap-2 pt-2">
          <button type="button" onClick={onKapat} className="px-4 py-2 border border-slate-200 dark:border-slate-700 rounded-md text-sm">İptal</button>
          <button type="submit" disabled={isleniyor || !form.deger.trim()} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm rounded-md">{isleniyor ? 'Kaydediliyor…' : (yeni ? 'Ekle' : 'Güncelle')}</button>
        </div>
      </form>
    </Modal>
  )
}