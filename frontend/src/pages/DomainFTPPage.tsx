// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; ftp_host: string; ftp_user: string }

export default function DomainFTPPage() {
  const { id } = useParams()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [hata, setHata] = useState<string | null>(null)
  const [yeniPw, setYeniPw] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [ozelPw, setOzelPw] = useState('')

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setDomain(r.data)).catch(e => setHata(apiHata(e)))
  }, [id])

  async function parolaSifirla(rastgele: boolean) {
    if (!rastgele && !ozelPw) return
    setIsleniyor(true); setYeniPw(null); setHata(null)
    try {
      const body = rastgele ? {} : { parola: ozelPw }
      const { data } = await api.put(`/domains/${id}/ftp/password`, body)
      setYeniPw(data.parola)
      setOzelPw('')
    } catch (e) {
      setHata(apiHata(e, 'Parola sıfırlama başarısız'))
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <div className="px-6 py-5 max-w-[900px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: 'Domainler', href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: 'FTP Hesabı' },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">FTP Hesabı</h1>
      {domain && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5"><Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link></p>}

      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      {domain && (
        <div className="bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded-2xl p-6">
          <div className="grid grid-cols-2 gap-y-3 mb-6 text-sm">
            <span className="text-slate-500 dark:text-slate-500">Sunucu</span><span className="font-mono text-slate-800 dark:text-slate-200">{domain.ftp_host}</span>
            <span className="text-slate-500 dark:text-slate-500">Port</span><span className="font-mono text-slate-800 dark:text-slate-200">21 (FTP) / 22 (SFTP)</span>
            <span className="text-slate-500 dark:text-slate-500">Kullanıcı adı</span><span className="font-mono text-slate-800 dark:text-slate-200">{domain.ftp_user}</span>
            <span className="text-slate-500 dark:text-slate-500">Ev dizini</span><span className="font-mono text-slate-800 dark:text-slate-200 text-xs">/home/{domain.sistem_kullanici}</span>
          </div>

          <div className="border-t border-slate-200 dark:border-slate-700 pt-5">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">Parola Sıfırlama</h3>
            <div className="flex items-center gap-2 mb-4">
              <input
                type="text"
                value={ozelPw}
                onChange={e => setOzelPw(e.target.value)}
                placeholder="Özel parola girin veya boş bırakın"
                className="flex-1 px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
              />
              <button onClick={() => parolaSifirla(false)} disabled={isleniyor || !ozelPw} className="px-3 py-2 bg-white dark:bg-slate-800 border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:bg-slate-900 dark:hover:bg-slate-800 disabled:opacity-50 text-sm rounded-md">Bu Parolayı Ayarla</button>
              <button onClick={() => parolaSifirla(true)} disabled={isleniyor} className="px-3 py-2 bg-slate-900 hover:bg-slate-800 dark:bg-slate-700 dark:hover:bg-slate-600 text-white dark:text-slate-100 disabled:opacity-60 text-sm font-medium rounded-md">Rastgele Üret</button>
            </div>

            {yeniPw && (
              <div className="bg-emerald-50 dark:bg-emerald-900/20 border border-emerald-200 dark:border-emerald-800 rounded-md p-4">
                <p className="text-sm text-emerald-800 dark:text-emerald-200 font-medium mb-1">✓ Yeni parola atandı</p>
                <p className="text-xs text-emerald-700 dark:text-emerald-300 mb-2">Bunu güvenli bir yere kaydedin, sonra göremezsiniz:</p>
                <div className="flex items-center gap-2">
                  <code className="flex-1 bg-white dark:bg-slate-800 px-3 py-2 font-mono text-sm text-slate-900 dark:text-slate-100 rounded border border-emerald-200 dark:border-emerald-800 break-all">{yeniPw}</code>
                  <button onClick={() => { navigator.clipboard.writeText(yeniPw); }} className="px-3 py-2 bg-emerald-100 dark:bg-emerald-900/30 hover:bg-emerald-200 text-emerald-800 dark:text-emerald-200 text-xs rounded">Kopyala</button>
                </div>
              </div>
            )}
          </div>

          <div className="border-t border-slate-200 dark:border-slate-700 pt-5 mt-5 text-xs text-slate-500 dark:text-slate-500">
            <p><strong>Not:</strong> FTP şu anda <code className="font-mono">cleartext</code> doğrulama kullanıyor (DB local). Şifrelemek için SFTP (port 22) kullanılabilir.</p>
          </div>
        </div>
      )}
    </div>
  )
}