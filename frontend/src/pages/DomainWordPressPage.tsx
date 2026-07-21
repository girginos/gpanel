import { useCallback, useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Kurulum = { dizin: string; site_url: string; admin_url: string; surum: string }
type Sonuc = { site_url: string; admin_url: string; admin_kullanici: string; admin_parola: string; surum: string }
type Durum = { surum: string; guncelleme_var: boolean; hedef_surum: string; php: string; db_mb: string; bakim: boolean }
type Paket = { name: string; status: string; version: string; update: string; update_version: string }
type Kullanici = { ID: number; user_login: string; user_email: string; display_name: string; roles: string }

export default function DomainWordPressPage() {
  const { id } = useParams()
  const [liste, setListe] = useState<Kurulum[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [kuruyor, setKuruyor] = useState(false)
  const [sonuc, setSonuc] = useState<Sonuc | null>(null)
  const [formAcik, setFormAcik] = useState(false)

  const [alanAdi, setAlanAdi] = useState('')
  const [altDizin, setAltDizin] = useState('')
  const [baslik, setBaslik] = useState('')
  const [adminK, setAdminK] = useState('admin')
  const [adminE, setAdminE] = useState('')

  useEffect(() => {
    if (!id) return
    api.get<{ alan_adi: string }>(`/domains/${id}`).then(r => setAlanAdi(r.data.alan_adi || '')).catch(() => {})
  }, [id])

  const listele = useCallback(() => {
    if (!id) return
    setYuk(true)
    api.get<Kurulum[]>(`/domains/${id}/wordpress`).then(r => setListe(r.data || [])).catch(() => setListe([])).finally(() => setYuk(false))
  }, [id])
  useEffect(() => { listele() }, [listele])

  async function kur(e: React.FormEvent) {
    e.preventDefault()
    setHata(null); setSonuc(null); setKuruyor(true)
    try {
      const { data } = await api.post<Sonuc>(`/domains/${id}/wordpress`, {
        alt_dizin: altDizin.trim(), site_basligi: baslik.trim(), admin_kullanici: adminK.trim(), admin_email: adminE.trim(),
      })
      setSonuc(data); setBaslik(''); setAltDizin(''); setFormAcik(false)
      listele()
    } catch (err) { setHata(apiHata(err, 'Kurulum başarısız')) }
    finally { setKuruyor(false) }
  }

  const bosDurum = !yuk && liste.length === 0

  return (
    <div className="px-6 py-6 max-w-5xl">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: alanAdi || 'Abonelik', href: `/abonelikler/${id}` },
        { etiket: 'WordPress' },
      ]} />
      <div className="flex items-center justify-between gap-4 mb-6 flex-wrap">
        <div>
          <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 tracking-tight">WordPress Toolkit</h1>
          <p className="text-sm text-slate-500 dark:text-slate-400 mt-1">Sürüm, eklenti, tema ve kullanıcıları tek yerden yönetin.</p>
        </div>
        {!bosDurum && !formAcik && (
          <button onClick={() => { setFormAcik(true); setSonuc(null) }}
            className="inline-flex items-center gap-1.5 px-4 py-2.5 rounded-full bg-slate-900 dark:bg-slate-700 text-white dark:text-slate-100 text-sm font-medium hover:bg-slate-800 dark:hover:bg-slate-600 transition">
            <span className="text-base leading-none">+</span> Yeni WordPress
          </button>
        )}
      </div>

      {hata && <div className="mb-4 px-4 py-3 bg-red-50 dark:bg-red-900/20 border border-red-100 dark:border-red-800/60 rounded-2xl text-sm text-red-600 dark:text-red-300">{hata}</div>}

      {sonuc && <KurulumSonuc s={sonuc} kapat={() => setSonuc(null)} />}

      {yuk ? (
        <div className="rounded-2xl border border-slate-200/70 dark:border-slate-700/60 bg-white dark:bg-slate-800/40 p-10 text-center text-sm text-slate-400">Yükleniyor…</div>
      ) : bosDurum ? (
        <div className="rounded-2xl border border-dashed border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800/40 p-12 text-center mb-5">
          <div className="w-12 h-12 mx-auto rounded-2xl bg-slate-100 dark:bg-slate-700/50 flex items-center justify-center text-2xl mb-3">📝</div>
          <p className="text-base font-medium text-slate-800 dark:text-slate-100">Bu domainde henüz WordPress yok</p>
          <p className="text-sm text-slate-400 mt-1">Aşağıdaki formdan tek tıkla kurabilirsiniz.</p>
        </div>
      ) : (
        <div className="space-y-5">
          {liste.map(k => <Toolkit key={k.dizin} id={id!} kurulum={k} onDegisti={listele} />)}
        </div>
      )}

      {(bosDurum || formAcik) && (
        <div className="mt-5">
          <KurulumFormu baslik={baslik} setBaslik={setBaslik} altDizin={altDizin} setAltDizin={setAltDizin}
            adminK={adminK} setAdminK={setAdminK} adminE={adminE} setAdminE={setAdminE}
            kur={kur} kuruyor={kuruyor} kapat={bosDurum ? undefined : () => setFormAcik(false)} />
        </div>
      )}

      <div className="mt-6"><Link to={`/abonelikler/${id}`} className="text-sm text-slate-500 hover:text-slate-800 dark:hover:text-slate-200 transition">← Aboneliğe dön</Link></div>
    </div>
  )
}

// ================= Toolkit: tek kurulum kartı =================

type AltTab = 'genel' | 'eklentiler' | 'temalar' | 'kullanicilar'
const TABLAR: { k: AltTab; ad: string }[] = [
  { k: 'genel', ad: 'Genel' }, { k: 'eklentiler', ad: 'Eklentiler' },
  { k: 'temalar', ad: 'Temalar' }, { k: 'kullanicilar', ad: 'Kullanıcılar' },
]

function Toolkit({ id, kurulum, onDegisti }: { id: string; kurulum: Kurulum; onDegisti: () => void }) {
  const dizin = kurulum.dizin
  const kok = dizin.includes('kök')
  const [tab, setTab] = useState<AltTab>('genel')
  const [durum, setDurum] = useState<Durum | null>(null)
  const [eklentiler, setEklentiler] = useState<Paket[] | null>(null)
  const [temalar, setTemalar] = useState<Paket[] | null>(null)
  const [kullanicilar, setKullanicilar] = useState<Kullanici[] | null>(null)
  const [mesgul, setMesgul] = useState<string | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [basari, setBasari] = useState<string | null>(null)
  const [cikti, setCikti] = useState<string | null>(null)
  const [parolaSonuc, setParolaSonuc] = useState<{ kullanici: string; parola: string } | null>(null)

  const qp = { params: { dizin } }

  const durumYukle = useCallback(() => {
    api.get<Durum>(`/domains/${id}/wordpress/durum`, qp).then(r => setDurum(r.data)).catch(() => setDurum(null))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, dizin])
  useEffect(() => { durumYukle() }, [durumYukle])

  useEffect(() => {
    if (tab === 'eklentiler' && eklentiler === null) api.get<Paket[]>(`/domains/${id}/wordpress/eklentiler`, qp).then(r => setEklentiler(r.data || [])).catch(() => setEklentiler([]))
    if (tab === 'temalar' && temalar === null) api.get<Paket[]>(`/domains/${id}/wordpress/temalar`, qp).then(r => setTemalar(r.data || [])).catch(() => setTemalar([]))
    if (tab === 'kullanicilar' && kullanicilar === null) api.get<Kullanici[]>(`/domains/${id}/wordpress/kullanicilar`, qp).then(r => setKullanicilar(r.data || [])).catch(() => setKullanicilar([]))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab])

  async function calistir(anahtar: string, istek: () => Promise<{ cikti?: string }>, basariMsj: string, sonra?: () => void) {
    setMesgul(anahtar); setHata(null); setBasari(null); setCikti(null)
    try {
      const data = await istek()
      setBasari(basariMsj)
      if (data?.cikti) setCikti(data.cikti)
      sonra?.()
    } catch (err) { setHata(apiHata(err, 'İşlem başarısız')) }
    finally { setMesgul(null) }
  }

  const surumGuncelle = () => calistir('surum', async () => (await api.post(`/domains/${id}/wordpress/guncelle`, { dizin })).data, 'WordPress çekirdeği güncellendi.', () => { durumYukle(); onDegisti() })
  const tumunuGuncelle = () => calistir('tumu', async () => (await api.post(`/domains/${id}/wordpress/arac`, { dizin, islem: 'tumunu-guncelle' })).data, 'Çekirdek, eklenti ve temalar güncellendi.', () => { durumYukle(); setEklentiler(null); setTemalar(null); onDegisti() })
  const bakimTogle = () => calistir('bakim', async () => (await api.post(`/domains/${id}/wordpress/arac`, { dizin, islem: durum?.bakim ? 'bakim-kapat' : 'bakim-ac' })).data, durum?.bakim ? 'Bakım modu kapatıldı.' : 'Bakım modu açıldı.', durumYukle)
  const cacheTemizle = () => calistir('cache', async () => (await api.post(`/domains/${id}/wordpress/arac`, { dizin, islem: 'cache-temizle' })).data, 'Önbellek temizlendi.')
  const onar = () => calistir('onar', async () => (await api.post(`/domains/${id}/wordpress/onar`, { dizin })).data, 'Çekirdek onarımı tamamlandı.', durumYukle)

  const paketGuncelle = (tur: 'eklenti' | 'tema', ad: string) => calistir(`${tur}:${ad}`, async () => (await api.post(`/domains/${id}/wordpress/${tur}`, { dizin, islem: 'guncelle', ad })).data, `${ad} güncellendi.`, () => { tur === 'eklenti' ? setEklentiler(null) : setTemalar(null) })
  const paketTumu = (tur: 'eklenti' | 'tema') => calistir(`${tur}:tum`, async () => (await api.post(`/domains/${id}/wordpress/${tur}`, { dizin, islem: 'tumunu-guncelle' })).data, 'Tümü güncellendi.', () => { tur === 'eklenti' ? setEklentiler(null) : setTemalar(null) })
  const eklentiTogle = (p: Paket) => calistir(`ekl:${p.name}`, async () => (await api.post(`/domains/${id}/wordpress/eklenti`, { dizin, islem: p.status === 'active' ? 'pasif' : 'aktif', ad: p.name })).data, `${p.name} ${p.status === 'active' ? 'devre dışı bırakıldı' : 'etkinleştirildi'}.`, () => setEklentiler(null))
  const temaAktif = (p: Paket) => calistir(`tema:${p.name}`, async () => (await api.post(`/domains/${id}/wordpress/tema`, { dizin, islem: 'aktif', ad: p.name })).data, `${p.name} etkinleştirildi.`, () => setTemalar(null))

  async function parolaSifirla(u: Kullanici) {
    if (!confirm(`"${u.user_login}" kullanıcısı için yeni bir parola üretilsin mi?\nMevcut parola geçersiz olacak.`)) return
    setMesgul(`pw:${u.ID}`); setHata(null); setBasari(null)
    try {
      const { data } = await api.post<{ parola: string; kullanici: string }>(`/domains/${id}/wordpress/kullanici-parola`, { dizin, user_id: u.ID })
      setParolaSonuc({ kullanici: data.kullanici || u.user_login, parola: data.parola })
    } catch (err) { setHata(apiHata(err, 'Parola sıfırlanamadı')) }
    finally { setMesgul(null) }
  }

  async function sil() {
    if (kok) { alert('Kök dizindeki WordPress panelden silinemez.'); return }
    if (!confirm(`${dizin} altındaki WordPress silinsin mi?\nBu dizindeki tüm dosyalar ve veritabanı kaldırılır. Geri alınamaz.`)) return
    setMesgul('sil'); setHata(null)
    try { await api.delete(`/domains/${id}/wordpress`, { data: { dizin, db_sil: true } }); onDegisti() }
    catch (err) { setHata(apiHata(err, 'Silinemedi')) }
    finally { setMesgul(null) }
  }

  const eklGuncel = (eklentiler || []).filter(p => p.update === 'available').length
  const temaGuncel = (temalar || []).filter(p => p.update === 'available').length
  const rozet: Record<string, number> = { eklentiler: eklGuncel, temalar: temaGuncel }

  return (
    <div className="rounded-2xl border border-slate-200/70 dark:border-slate-700/60 bg-white dark:bg-slate-800/40 overflow-hidden">
      {/* başlık şeridi */}
      <div className="flex items-center justify-between gap-3 px-5 pt-5 pb-4 flex-wrap">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="w-9 h-9 rounded-xl bg-slate-100 dark:bg-slate-700/50 flex items-center justify-center text-lg shrink-0">📝</div>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-slate-900 dark:text-slate-100">WordPress <span className="text-slate-400 font-normal font-mono text-xs">· {dizin}</span></div>
            <div className="text-xs text-slate-400 mt-0.5 truncate">{kurulum.site_url}</div>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          {kurulum.admin_url && (
            <a href={kurulum.admin_url} target="_blank" rel="noreferrer"
              className="inline-flex items-center gap-1 px-3.5 py-2 rounded-full bg-slate-900 dark:bg-slate-700 text-white dark:text-slate-100 text-xs font-medium hover:bg-slate-800 dark:hover:bg-slate-600 transition">
              Yönetim paneli <span className="opacity-70">↗</span>
            </a>
          )}
          {!kok && <button disabled={!!mesgul} onClick={sil} className="px-3 py-2 rounded-full border border-slate-200 dark:border-slate-700 text-xs text-slate-500 hover:border-red-300 hover:text-red-600 dark:hover:border-red-800 dark:hover:text-red-400 disabled:opacity-50 transition">{mesgul === 'sil' ? '…' : 'Kaldır'}</button>}
        </div>
      </div>

      {/* metrikler */}
      <div className="px-5 grid grid-cols-2 lg:grid-cols-4 gap-3">
        <Metrik label="Sürüm" v={durum?.surum ? durum.surum : '…'}
          pill={durum ? (durum.guncelleme_var ? { t: `↑ ${durum.hedef_surum}`, c: 'amber' } : { t: 'güncel', c: 'green' }) : undefined} />
        <Metrik label="PHP" v={durum?.php || '…'} />
        <Metrik label="Veritabanı" v={durum ? `${durum.db_mb} MB` : '…'} />
        <Metrik label="Bakım modu" v={durum?.bakim ? 'Açık' : 'Kapalı'}
          pill={durum?.bakim ? { t: 'aktif', c: 'amber' } : undefined} />
      </div>

      {/* segment sekmeler */}
      <div className="px-5 pt-5">
        <div className="inline-flex items-center gap-1 p-1 rounded-full bg-slate-100 dark:bg-slate-900/50">
          {TABLAR.map(t => (
            <button key={t.k} onClick={() => setTab(t.k)}
              className={`px-3.5 py-1.5 rounded-full text-sm font-medium transition ${tab === t.k
                ? 'bg-white dark:bg-slate-700 text-slate-900 dark:text-slate-100 shadow-sm'
                : 'text-slate-500 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-200'}`}>
              {t.ad}
              {!!rozet[t.k] && rozet[t.k] > 0 && <span className="ml-1.5 text-[10px] px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/50 text-amber-700 dark:text-amber-300 font-semibold align-middle">{rozet[t.k]}</span>}
            </button>
          ))}
        </div>
      </div>

      <div className="p-5">
        {hata && <div className="mb-4 px-3.5 py-2.5 bg-red-50 dark:bg-red-900/20 border border-red-100 dark:border-red-800/60 rounded-xl text-xs text-red-600 dark:text-red-300">{hata}</div>}
        {basari && <div className="mb-4 px-3.5 py-2.5 bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-100 dark:border-emerald-800/60 rounded-xl text-xs text-emerald-600 dark:text-emerald-300">{basari}</div>}

        {tab === 'genel' && (
          <div>
            <div className="flex flex-wrap gap-2">
              {durum?.guncelleme_var && <Btn onClick={surumGuncelle} bekle={mesgul === 'surum'} tur="primary">Sürümü güncelle · v{durum.hedef_surum}</Btn>}
              <Btn onClick={tumunuGuncelle} bekle={mesgul === 'tumu'} tur={durum?.guncelleme_var ? 'outline' : 'primary'}>Tümünü güncelle</Btn>
              <Btn onClick={bakimTogle} bekle={mesgul === 'bakim'}>{durum?.bakim ? 'Bakım modunu kapat' : 'Bakım moduna al'}</Btn>
              <Btn onClick={cacheTemizle} bekle={mesgul === 'cache'}>Önbelleği temizle</Btn>
              <Btn onClick={onar} bekle={mesgul === 'onar'}>Çekirdeği onar</Btn>
            </div>
            {cikti && <Cikti metin={cikti} />}
            {!cikti && <p className="text-xs text-slate-400 mt-4">Hızlı bakım işlemleri. Sürüm güncellemesi varsa üstteki metrikte görünür.</p>}
          </div>
        )}

        {tab === 'eklentiler' && (
          <PaketTablo tur="eklenti" liste={eklentiler} mesgul={mesgul}
            onTumu={() => paketTumu('eklenti')} onGuncelle={(p) => paketGuncelle('eklenti', p.name)} onTogle={eklentiTogle} />
        )}
        {tab === 'temalar' && (
          <PaketTablo tur="tema" liste={temalar} mesgul={mesgul}
            onTumu={() => paketTumu('tema')} onGuncelle={(p) => paketGuncelle('tema', p.name)} onAktif={temaAktif} />
        )}
        {tab === 'kullanicilar' && <KullaniciListe liste={kullanicilar} mesgul={mesgul} onReset={parolaSifirla} />}

        {tab !== 'genel' && cikti && <Cikti metin={cikti} />}
      </div>

      {parolaSonuc && <ParolaModal s={parolaSonuc} kapat={() => setParolaSonuc(null)} />}
    </div>
  )
}

// ================= parçalar =================

function Metrik({ label, v, pill }: { label: string; v: string; pill?: { t: string; c: 'green' | 'amber' | 'red' } }) {
  return (
    <div className="rounded-2xl bg-slate-50 dark:bg-slate-900/40 p-4">
      <div className="text-xs text-slate-400 font-medium">{label}</div>
      <div className="flex items-center gap-2 mt-1.5">
        <span className="text-xl font-semibold text-slate-900 dark:text-slate-100 tracking-tight">{v}</span>
        {pill && <StatusPill t={pill.t} c={pill.c} />}
      </div>
    </div>
  )
}

function StatusPill({ t, c }: { t: string; c: 'green' | 'amber' | 'red' | 'slate' }) {
  const cls = {
    green: 'bg-emerald-50 dark:bg-emerald-900/30 text-emerald-600 dark:text-emerald-300',
    amber: 'bg-amber-50 dark:bg-amber-900/30 text-amber-600 dark:text-amber-300',
    red: 'bg-red-50 dark:bg-red-900/30 text-red-600 dark:text-red-300',
    slate: 'bg-slate-100 dark:bg-slate-700 text-slate-500 dark:text-slate-300',
  }[c]
  return <span className={`text-[11px] font-medium px-2 py-0.5 rounded-full ${cls}`}>{t}</span>
}

function Btn({ onClick, bekle, children, tur }: { onClick: () => void; bekle: boolean; children: React.ReactNode; tur?: 'primary' | 'outline' }) {
  const cls = tur === 'primary'
    ? 'bg-slate-900 dark:bg-slate-700 text-white dark:text-slate-100 hover:bg-slate-800 dark:hover:bg-slate-600 border-transparent'
    : 'bg-white dark:bg-slate-800 border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-200 hover:bg-slate-50 dark:hover:bg-slate-700/50'
  return (
    <button onClick={onClick} disabled={bekle} className={`text-sm px-4 py-2 rounded-full border font-medium disabled:opacity-50 transition ${cls}`}>
      {bekle ? 'İşleniyor…' : children}
    </button>
  )
}

function Cikti({ metin }: { metin: string }) {
  const temiz = metin.replace(/\[[0-9;]*m/g, '')
  return (
    <details className="mt-4 group" open>
      <summary className="text-xs text-slate-400 cursor-pointer select-none hover:text-slate-600 dark:hover:text-slate-300">İşlem çıktısı</summary>
      <pre className="mt-2 max-h-44 overflow-auto text-[12px] leading-relaxed bg-slate-50 dark:bg-slate-900/60 border border-slate-100 dark:border-slate-700/60 text-slate-600 dark:text-slate-300 rounded-xl p-3 whitespace-pre-wrap break-words">{temiz}</pre>
    </details>
  )
}

function PaketTablo({ tur, liste, mesgul, onTumu, onGuncelle, onTogle, onAktif }: {
  tur: 'eklenti' | 'tema'; liste: Paket[] | null; mesgul: string | null
  onTumu: () => void; onGuncelle: (p: Paket) => void; onTogle?: (p: Paket) => void; onAktif?: (p: Paket) => void
}) {
  if (liste === null) return <div className="text-sm text-slate-400 py-4">Yükleniyor…</div>
  if (liste.length === 0) return <div className="text-sm text-slate-400 py-4">{tur === 'eklenti' ? 'Eklenti' : 'Tema'} bulunamadı.</div>
  const guncellenebilir = liste.filter(p => p.update === 'available').length
  return (
    <div>
      {guncellenebilir > 0 && (
        <div className="flex items-center justify-between mb-4 px-4 py-3 rounded-2xl bg-amber-50 dark:bg-amber-900/15 border border-amber-100 dark:border-amber-800/50">
          <span className="text-sm text-amber-700 dark:text-amber-300 font-medium">{guncellenebilir} güncelleme mevcut</span>
          <button disabled={!!mesgul} onClick={onTumu} className="text-sm px-4 py-1.5 rounded-full bg-slate-900 dark:bg-slate-700 text-white dark:text-slate-100 font-medium hover:bg-slate-800 dark:hover:bg-slate-600 disabled:opacity-50 transition">{mesgul === `${tur}:tum` ? '…' : 'Tümünü güncelle'}</button>
        </div>
      )}
      <div className="divide-y divide-slate-100 dark:divide-slate-700/50">
        {liste.map(p => {
          const aktif = p.status === 'active'
          const guncel = p.update === 'available'
          return (
            <div key={p.name} className="flex items-center justify-between gap-3 py-3">
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span className="text-sm font-medium text-slate-800 dark:text-slate-100 truncate">{p.name}</span>
                  <StatusPill t={aktif ? 'Etkin' : 'Devre dışı'} c={aktif ? 'green' : 'slate'} />
                </div>
                <div className="text-xs text-slate-400 mt-0.5">
                  Sürüm {p.version}{guncel && <span className="text-amber-600 dark:text-amber-400"> → {p.update_version} mevcut</span>}
                </div>
              </div>
              <div className="flex items-center gap-2 shrink-0">
                {guncel && <button disabled={!!mesgul} onClick={() => onGuncelle(p)} className="text-xs px-3 py-1.5 rounded-full bg-amber-500 hover:bg-amber-600 text-white font-medium disabled:opacity-50 transition">{mesgul === `${tur}:${p.name}` ? '…' : 'Güncelle'}</button>}
                {onTogle && <button disabled={!!mesgul} onClick={() => onTogle(p)} className="text-xs px-3 py-1.5 rounded-full border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700/50 disabled:opacity-50 transition">{mesgul === `ekl:${p.name}` ? '…' : aktif ? 'Devre dışı bırak' : 'Etkinleştir'}</button>}
                {onAktif && !aktif && <button disabled={!!mesgul} onClick={() => onAktif(p)} className="text-xs px-3 py-1.5 rounded-full border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700/50 disabled:opacity-50 transition">{mesgul === `tema:${p.name}` ? '…' : 'Etkinleştir'}</button>}
                {onAktif && aktif && <StatusPill t="Aktif tema" c="green" />}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function KullaniciListe({ liste, mesgul, onReset }: { liste: Kullanici[] | null; mesgul: string | null; onReset: (u: Kullanici) => void }) {
  if (liste === null) return <div className="text-sm text-slate-400 py-4">Yükleniyor…</div>
  if (liste.length === 0) return <div className="text-sm text-slate-400 py-4">Kullanıcı bulunamadı.</div>
  return (
    <div className="divide-y divide-slate-100 dark:divide-slate-700/50">
      {liste.map(u => (
        <div key={u.ID} className="flex items-center justify-between gap-3 py-3">
          <div className="flex items-center gap-3 min-w-0">
            <div className="w-9 h-9 rounded-full bg-slate-100 dark:bg-slate-700 flex items-center justify-center text-xs font-semibold text-slate-500 dark:text-slate-300 shrink-0">
              {(u.display_name || u.user_login).slice(0, 1).toUpperCase()}
            </div>
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium text-slate-800 dark:text-slate-100 truncate">{u.user_login}</span>
                <StatusPill t={u.roles} c="slate" />
              </div>
              <div className="text-xs text-slate-400 truncate">{u.user_email}</div>
            </div>
          </div>
          <button disabled={!!mesgul} onClick={() => onReset(u)} className="text-xs px-3 py-1.5 rounded-full border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700/50 disabled:opacity-50 transition shrink-0">{mesgul === `pw:${u.ID}` ? '…' : 'Parola sıfırla'}</button>
        </div>
      ))}
    </div>
  )
}

// ================= kurulum formu · sonuç · modal =================

function KurulumFormu(p: {
  baslik: string; setBaslik: (s: string) => void; altDizin: string; setAltDizin: (s: string) => void
  adminK: string; setAdminK: (s: string) => void; adminE: string; setAdminE: (s: string) => void
  kur: (e: React.FormEvent) => void; kuruyor: boolean; kapat?: () => void
}) {
  return (
    <form onSubmit={p.kur} className="rounded-2xl border border-slate-200/70 dark:border-slate-700/60 bg-white dark:bg-slate-800/40 p-5">
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100">Yeni WordPress kurulumu</h3>
        {p.kapat && <button type="button" onClick={p.kapat} className="text-xs text-slate-400 hover:text-slate-600">✕ Kapat</button>}
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <Girdi et="Site başlığı" v={p.baslik} set={p.setBaslik} zorunlu ph="Benim Blogum" />
        <Girdi et="Alt dizin (isteğe bağlı)" v={p.altDizin} set={p.setAltDizin} ph="boş = kök · örn: blog" mono />
        <Girdi et="Admin kullanıcı" v={p.adminK} set={p.setAdminK} zorunlu mono />
        <Girdi et="Admin e-posta" v={p.adminE} set={p.setAdminE} zorunlu type="email" ph="admin@site.com" />
      </div>
      <button disabled={p.kuruyor} className="mt-5 px-5 py-2.5 rounded-full bg-slate-900 dark:bg-slate-700 text-white dark:text-slate-100 text-sm font-medium hover:bg-slate-800 dark:hover:bg-slate-600 disabled:opacity-50 transition">
        {p.kuruyor ? 'Kuruluyor… (~30 sn)' : 'WordPress kur'}
      </button>
    </form>
  )
}

function Girdi({ et, v, set, zorunlu, ph, mono, type }: { et: string; v: string; set: (s: string) => void; zorunlu?: boolean; ph?: string; mono?: boolean; type?: string }) {
  return (
    <label className="block">
      <span className="text-xs text-slate-500 dark:text-slate-400 font-medium">{et}</span>
      <input value={v} onChange={e => set(e.target.value)} required={zorunlu} placeholder={ph} type={type || 'text'}
        className={`mt-1.5 w-full px-3.5 py-2.5 rounded-xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 text-sm text-slate-800 dark:text-slate-100 placeholder:text-slate-300 dark:placeholder:text-slate-600 focus:border-slate-400 dark:focus:border-slate-500 focus:ring-4 focus:ring-slate-100 dark:focus:ring-slate-800 outline-none transition ${mono ? 'font-mono' : ''}`} />
    </label>
  )
}

function KurulumSonuc({ s, kapat }: { s: Sonuc; kapat: () => void }) {
  return (
    <div className="mb-5 rounded-2xl border border-emerald-100 dark:border-emerald-800/60 bg-emerald-50/60 dark:bg-emerald-900/15 p-5">
      <div className="flex items-center justify-between mb-3">
        <div className="text-sm font-semibold text-emerald-700 dark:text-emerald-300">WordPress {s.surum} kuruldu</div>
        <button onClick={kapat} className="text-xs text-emerald-600/70 hover:text-emerald-700">✕</button>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-8 gap-y-2 text-sm">
        <Satir et="Site" v={s.site_url} link />
        <Satir et="Yönetim" v={s.admin_url} link />
        <Satir et="Kullanıcı" v={s.admin_kullanici} mono />
        <Satir et="Parola" v={s.admin_parola} mono />
      </div>
      <p className="text-xs text-amber-700 dark:text-amber-400 mt-3">Parolayı şimdi kaydedin — tekrar gösterilmez.</p>
    </div>
  )
}

function Satir({ et, v, mono, link }: { et: string; v: string; mono?: boolean; link?: boolean }) {
  return (
    <div className="flex items-baseline gap-2 min-w-0">
      <span className="text-xs text-slate-400 shrink-0 w-16">{et}</span>
      {link ? <a href={v} target="_blank" rel="noreferrer" className="text-sm text-slate-700 dark:text-slate-200 hover:underline truncate">{v}</a>
        : <span className={`text-sm text-slate-800 dark:text-slate-100 truncate ${mono ? 'font-mono' : ''}`}>{v}</span>}
    </div>
  )
}

function ParolaModal({ s, kapat }: { s: { kullanici: string; parola: string }; kapat: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/30 backdrop-blur-sm p-4" onClick={kapat}>
      <div className="bg-white dark:bg-slate-800 rounded-2xl border border-slate-200 dark:border-slate-700 p-6 max-w-sm w-full shadow-xl" onClick={e => e.stopPropagation()}>
        <div className="text-base font-semibold text-slate-900 dark:text-slate-100 mb-1">Yeni parola oluşturuldu</div>
        <div className="text-xs text-slate-400 mb-4">Kullanıcı: <span className="font-mono text-slate-600 dark:text-slate-300">{s.kullanici}</span></div>
        <div className="flex items-center gap-2">
          <code className="flex-1 px-3.5 py-3 bg-slate-50 dark:bg-slate-900 rounded-xl text-sm font-mono text-slate-800 dark:text-slate-100 break-all border border-slate-100 dark:border-slate-700">{s.parola}</code>
          <button onClick={() => navigator.clipboard?.writeText(s.parola)} className="text-xs px-3.5 py-3 rounded-xl border border-slate-200 dark:border-slate-700 text-slate-600 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-700/50 transition">Kopyala</button>
        </div>
        <p className="text-xs text-amber-600 dark:text-amber-400 mt-3">Bu parola tekrar gösterilmez — şimdi kaydedin.</p>
        <button onClick={kapat} className="mt-5 w-full py-2.5 rounded-full bg-slate-900 dark:bg-slate-700 text-white dark:text-slate-100 text-sm font-medium hover:bg-slate-800 dark:hover:bg-slate-600 transition">Tamam</button>
      </div>
    </div>
  )
}
