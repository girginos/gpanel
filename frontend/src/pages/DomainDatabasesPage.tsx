// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import ConfirmDialog from '@/components/ConfirmDialog'
import Modal from '@/components/Modal'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string }
type DB = {
  id: number; domain_id: number; db_adi: string; db_kullanici: string;
  db_host: string; db_parola: string; olusturulma: string
}

export default function DomainDatabasesPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [dbler, setDbler] = useState<DB[]>([])
  const [yuk, setYuk] = useState(true)
  const [hata, setHata] = useState<string | null>(null)
  const [silinecek, setSilinecek] = useState<DB | null>(null)
  const [pwResetFor, setPwResetFor] = useState<DB | null>(null)
  const [paroliGoster, setParolaGoster] = useState<Record<number, boolean>>({})
  const [kopya, setKopya] = useState<number | null>(null)

  function yukle() {
    if (!id) return
    setYuk(true)
    api.get<DB[]>(`/domains/${id}/databases`)
      .then(r => setDbler(r.data))
      .catch(e => setHata(apiHata(e)))
      .finally(() => setYuk(false))
  }
  async function pmaAc(d: DB) {
    try {
      const { data } = await api.post<{ signon_url: string }>(`/databases/${d.id}/pma-token`)
      window.open(data.signon_url, '_blank', 'noopener')
    } catch (e) {
      alert(apiHata(e, 'phpMyAdmin token alınamadı'))
    }
  }

  useEffect(() => {
    if (id) api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(() => {})
    yukle()
  }, [id])

  async function ekle() {
    try {
      const { data } = await api.post(`/domains/${id}/databases`, {})
      alert(`Yeni DB:\n\nAd: ${data.db_adi}\nKullanıcı: ${data.db_kullanici}\nParola: ${data.db_parola}\n\nParolayı kaydedin!`)
      yukle()
    } catch (e) {
      alert(apiHata(e, 'Ekleme başarısız'))
    }
  }

  async function sil() {
    if (!silinecek) return
    try { await api.delete(`/databases/${silinecek.id}`); setSilinecek(null); yukle() }
    catch (e) { alert(apiHata(e, 'Silme başarısız')) }
  }

  function kopyala(d: DB) {
    navigator.clipboard.writeText(d.db_parola)
    setKopya(d.id)
    setTimeout(() => setKopya(null), 1500)
  }

  return (
    <div className="px-6 py-5 max-w-[1300px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'Veritabanları' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Veritabanları</h1>
      {domain && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5"><Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link></p>}

      <div className="flex items-center gap-2 mb-4">
        <button onClick={ekle} className="px-3.5 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 text-sm font-medium rounded-md">+ Yeni Veritabanı</button>
        <button onClick={yukle} className="px-3 py-2 bg-white dark:bg-slate-800 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 border border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300 text-sm rounded-md">↻ Yenile</button>
        <span className="ml-auto text-sm text-slate-500 dark:text-slate-500">{dbler.length} veritabanı</span>
      </div>

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl overflow-hidden">
        {yuk ? <div className="py-12 text-center text-sm text-slate-400 dark:text-slate-500">Yükleniyor…</div> :
         dbler.length === 0 ? <div className="py-12 text-center text-sm text-slate-500 dark:text-slate-500">Henüz veritabanı yok</div> :
        <table className="w-full">
          <thead className="bg-slate-50 dark:bg-slate-900 text-xs uppercase tracking-wider text-slate-500 dark:text-slate-500 border-b border-slate-200 dark:border-slate-700">
            <tr>
              <th className="text-left px-4 py-2.5">Veritabanı</th>
              <th className="text-left px-4 py-2.5">Kullanıcı</th>
              <th className="text-left px-4 py-2.5">Sunucu</th>
              <th className="text-left px-4 py-2.5">Parola</th>
              <th className="text-left px-4 py-2.5">Oluşturulma</th>
              <th className="text-right px-4 py-2.5">İşlemler</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {dbler.map(d => (
              <tr key={d.id} className="hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800">
                <td className="px-4 py-2.5 text-sm font-mono text-slate-800 dark:text-slate-200">{d.db_adi}</td>
                <td className="px-4 py-2.5 text-sm font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">{d.db_kullanici}</td>
                <td className="px-4 py-2.5 text-sm font-mono text-slate-600 dark:text-slate-400 dark:text-slate-500">{d.db_host}:3306</td>
                <td className="px-4 py-2.5 text-sm">
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => setParolaGoster({ ...paroliGoster, [d.id]: !paroliGoster[d.id] })}
                      className="font-mono text-xs px-1.5 py-0.5 bg-slate-100 dark:bg-slate-800 hover:bg-slate-200 rounded"
                      title={paroliGoster[d.id] ? 'Gizle' : 'Göster'}
                    >
                      {paroliGoster[d.id] ? d.db_parola : '••••••••'}
                    </button>
                    {paroliGoster[d.id] && (
                      <button onClick={() => kopyala(d)} className="text-xs px-1.5 py-0.5 bg-slate-100 dark:bg-slate-800 hover:bg-brand-100 dark:bg-brand-900/30 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 rounded" title="Kopyala">
                        {kopya === d.id ? '✓' : '⧉'}
                      </button>
                    )}
                  </div>
                </td>
                <td className="px-4 py-2.5 text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500">{d.olusturulma}</td>
                <td className="px-4 py-2.5 text-right space-x-1">
                  <button onClick={() => pmaAc(d)} className="text-sm text-indigo-600 dark:text-indigo-400 hover:bg-indigo-50 dark:bg-indigo-900/20 px-2 py-1 rounded" title="phpMyAdmin'de yeni sekmede aç">🔓 phpMyAdmin</button>
                  <button onClick={() => setPwResetFor(d)} className="text-sm text-brand-600 dark:text-brand-400 hover:bg-brand-50 dark:hover:bg-brand-900/30 dark:bg-brand-900/20 px-2 py-1 rounded">🔑 Parola Sıfırla</button>
                  <button onClick={() => setSilinecek(d)} className="text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/30 dark:bg-red-900/20 px-2 py-1 rounded">Sil</button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>}
      </div>

      {pwResetFor && (
        <PwResetModal
          db={pwResetFor}
          onKapat={() => setPwResetFor(null)}
          onTamam={() => { setPwResetFor(null); yukle() }}
        />
      )}

      <ConfirmDialog
        acik={!!silinecek}
        baslik="Veritabanını sil"
        mesaj={`"${silinecek?.db_adi}" veritabanı ve kullanıcısı kalıcı silinecek. Bu işlem geri alınamaz!`}
        tehlikeli
        onayMetni="Evet, sil"
        onOnay={sil}
        onIptal={() => setSilinecek(null)}
      />
    </div>
  )
}

function PwResetModal({ db, onKapat, onTamam }: { db: DB; onKapat: () => void; onTamam: () => void }) {
  const [ozelPw, setOzelPw] = useState('')
  const [isleniyor, setIsleniyor] = useState(false)
  const [hata, setHata] = useState<string | null>(null)
  const [yeniPw, setYeniPw] = useState<string | null>(null)

  async function sifirla(rastgele: boolean) {
    if (!rastgele && ozelPw.length < 6) {
      setHata('Parola en az 6 karakter olmalı')
      return
    }
    setIsleniyor(true); setHata(null)
    try {
      const body = rastgele ? {} : { parola: ozelPw }
      const { data } = await api.put(`/databases/${db.id}/password`, body)
      setYeniPw(data.db_parola)
    } catch (e) {
      setHata(apiHata(e, 'Sıfırlama başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <Modal acik={true} baslik={`Parola Sıfırla — ${db.db_adi}`} onKapat={yeniPw ? onTamam : onKapat} genislik="md">
      {!yeniPw ? (
        <div className="space-y-4">
          <div className="text-sm text-slate-600 dark:text-slate-400 dark:text-slate-500">
            <strong className="font-mono">{db.db_kullanici}</strong> kullanıcısının parolası MariaDB ve panel'de eşzamanlı güncellenir.
          </div>
          <div>
            <label className="block text-xs font-medium text-slate-600 dark:text-slate-400 dark:text-slate-500 mb-1">Özel parola (boş bırakırsanız rastgele)</label>
            <input
              type="text"
              value={ozelPw}
              onChange={e => setOzelPw(e.target.value)}
              placeholder="En az 6 karakter"
              className="w-full px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
            />
          </div>
          {hata && <div className="px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-sm text-red-700 dark:text-red-300">{hata}</div>}
          <div className="flex justify-end gap-2 pt-2">
            <button onClick={onKapat} disabled={isleniyor} className="px-4 py-2 border border-slate-200 dark:border-slate-700 rounded-md text-sm">İptal</button>
            <button onClick={() => sifirla(false)} disabled={isleniyor || !ozelPw} className="px-4 py-2 bg-white dark:bg-slate-800 border border-brand-600 text-brand-700 dark:text-brand-300 hover:bg-brand-50 dark:hover:bg-brand-900/30 dark:bg-brand-900/20 disabled:opacity-50 rounded-md text-sm">Bunu Ayarla</button>
            <button onClick={() => sifirla(true)} disabled={isleniyor} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 disabled:opacity-60 text-sm font-medium rounded-md">{isleniyor ? 'Sıfırlanıyor…' : 'Rastgele Üret'}</button>
          </div>
        </div>
      ) : (
        <div className="space-y-4">
          <div className="bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md p-4">
            <p className="text-sm text-emerald-800 dark:text-emerald-200 font-medium mb-2">✓ Parola güncellendi</p>
            <p className="text-xs text-emerald-700 dark:text-emerald-300 mb-2">Bunu güvenli bir yere kaydedin. Sonra göremezsiniz:</p>
            <div className="flex items-center gap-2">
              <code className="flex-1 bg-white dark:bg-slate-800 px-3 py-2 font-mono text-sm text-slate-900 dark:text-slate-100 rounded border border-emerald-200 dark:border-emerald-800 break-all">{yeniPw}</code>
              <button onClick={() => navigator.clipboard.writeText(yeniPw)} className="px-3 py-2 bg-emerald-100 dark:bg-emerald-900/30 hover:bg-emerald-200 text-emerald-800 dark:text-emerald-200 text-xs rounded">Kopyala</button>
            </div>
          </div>
          <div className="flex justify-end">
            <button onClick={onTamam} className="px-4 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-white dark:hover:bg-slate-100 text-white dark:text-slate-900 text-sm rounded-md">Tamam</button>
          </div>
        </div>
      )}
    </Modal>
  )
}