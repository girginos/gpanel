import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'
import { useToast } from '@/components/Toast'

type Domain = { id: number; alan_adi: string; sistem_kullanici: string; ftp_host: string; ftp_user: string }


const FTP_EN: Record<string, string> = {
  "Not:": "Note:",
  "doğrulama kullanıyor (DB local). Şifrelemek için SFTP (port 22) kullanılabilir.": "authentication (DB local). Use SFTP (port 22) to encrypt.",
  "Bu Parolayı Ayarla": "Set This Password",
  "FTP Hesabı": "FTP Account",
  "FTP şu anda": "FTP is currently",
  "Parola Sıfırlama": "Password Reset",
  "Parola sıfırlama başarısız": "Password reset failed",
  "Yeni FTP parolası girin": "Enter new FTP password",
  "✓ FTP parolası güncellendi": "✓ FTP password updated",
  "İşlem başarısız": "Operation failed",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (FTP_EN[tr] || ORTAK_EN[tr] || tr) : tr)

export default function DomainFTPPage() {
  useTranslation() // dil re-render aboneligi
  const { id } = useParams()
  const toast = useToast()
  const [domain, setDomain] = useState<Domain | null>(null)
  const [, setHata] = useState<string | null>(null)
  const [isleniyor, setIsleniyor] = useState(false)
  const [ozelPw, setOzelPw] = useState('')

  useEffect(() => {
    let iptal = false
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => { if (iptal) return; setDomain(r.data) }).catch(e => { if (iptal) return; const m = apiHata(e); setHata(m); toast.hata(cevir("İşlem başarısız"), m) })
    return () => { iptal = true }
  }, [id])

  async function parolaSifirla() {
    if (!ozelPw) return
    setIsleniyor(true); setHata(null)
    try {
      await api.put(`/domains/${id}/ftp/password`, { parola: ozelPw })
      setOzelPw('')
      toast.basari(cevir("✓ FTP parolası güncellendi"))
    } catch (e) {
      const m = apiHata(e, cevir("Parola sıfırlama başarısız")); setHata(m); toast.hata(cevir("İşlem başarısız"), m)
    } finally {
      setIsleniyor(false)
    }
  }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5 max-w-[900px]">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' }, { etiket: cevir("Domainler"), href: '/domainler' },
        { etiket: domain?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: cevir("FTP Hesabı") },
      ]} />

      <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100 mb-1">{cevir("FTP Hesabı")}</h1>
      {domain && <p className="text-sm text-slate-500 dark:text-slate-500 mb-5"><Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{domain.alan_adi}</Link></p>}

      {domain && (
        <div className="bg-white dark:bg-dark-700 border border-slate-200 dark:border-dark-600 rounded-lg p-6">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-y-3 mb-6 text-sm">
            <span className="text-slate-500 dark:text-slate-500">{cevir("Sunucu")}</span><span className="font-mono text-slate-800 dark:text-slate-200">{domain.ftp_host}</span>
            <span className="text-slate-500 dark:text-slate-500">Port</span><span className="font-mono text-slate-800 dark:text-slate-200">21 (FTP) / 22 (SFTP)</span>
            <span className="text-slate-500 dark:text-slate-500">{cevir("Kullanıcı adı")}</span><span className="font-mono text-slate-800 dark:text-slate-200">{domain.ftp_user}</span>
            <span className="text-slate-500 dark:text-slate-500">Ev dizini</span><span className="font-mono text-slate-800 dark:text-slate-200 text-xs">/home/{domain.sistem_kullanici}</span>
          </div>

          <div className="border-t border-slate-200 dark:border-dark-600 pt-5">
            <h3 className="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-3">{cevir("Parola Sıfırlama")}</h3>
            <div className="flex flex-wrap items-center gap-2 mb-4">
              <input
                type="text"
                value={ozelPw}
                onChange={e => setOzelPw(e.target.value)}
                placeholder={cevir("Yeni FTP parolası girin")}
                className="flex-1 px-3 py-2 border border-slate-300 dark:border-slate-600 rounded-md text-sm font-mono focus:border-brand-500 focus:ring-2 focus:ring-brand-500/20 outline-none"
              />
              <button onClick={() => parolaSifirla()} disabled={isleniyor || !ozelPw} className="px-3 py-2 bg-white dark:bg-dark-700 border border-slate-300 dark:border-slate-600 hover:bg-slate-50 dark:bg-dark-800 dark:hover:bg-dark-700 disabled:opacity-50 text-sm rounded-md">{cevir("Bu Parolayı Ayarla")}</button>
            </div>
          </div>

          <div className="border-t border-slate-200 dark:border-dark-600 pt-5 mt-5 text-xs text-slate-500 dark:text-slate-500">
            <p><strong>{cevir("Not:")}</strong> {cevir("FTP şu anda")} <code className="font-mono">cleartext</code> {cevir("doğrulama kullanıyor (DB local). Şifrelemek için SFTP (port 22) kullanılabilir.")}</p>
          </div>
        </div>
      )}
    </div>
  )
}