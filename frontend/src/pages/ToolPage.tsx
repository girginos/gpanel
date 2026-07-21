// gosp-dark-swept
// gosp-dark-swept-v2
import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, apiHata } from '@/lib/api'
import Breadcrumb from '@/components/Breadcrumb'

type Domain = { id: number; alan_adi: string }

const TOOL_META: Record<string, { etiket: string; faz?: string; aciklama: string }> = {
  'baglanti':        { etiket: 'Bağlantı Bilgisi',        aciklama: 'FTP sunucu, kullanıcı, veritabanı bağlantı dizesi ve hızlı kopyalama.' },
  'dosyalar':        { etiket: 'Dosya Yöneticisi',        faz: 'F6',  aciklama: 'public_html altında dosyaları listele, yükle, indir, izin değiştir.' },
  'veritabanlari':   { etiket: 'Veritabanları',           faz: 'F5',  aciklama: 'MySQL veritabanları, kullanıcı, phpMyAdmin entegrasyonu.' },
  'ftp':             { etiket: 'FTP Hesapları',            faz: 'F4',  aciklama: 'Pure-FTPd üzerinden virtual FTP hesapları, parola, ev dizini.' },
  'yedekler':        { etiket: 'Yedekle ve Geri Yükle',    faz: 'F12', aciklama: 'Tarball + DB dump → SFTP/S3/local hedefe yedek.' },
  'kopyala':         { etiket: 'Web Sitesini Kopyala',     aciklama: 'Mevcut bir siteyi başka bir alan adına klonlama.' },
  'php':             { etiket: 'PHP Ayarları',             faz: 'F3',  aciklama: 'PHP-FPM pool seçimi, sürüm değiştirme, php.ini parametreleri.' },
  'gunlukler':       { etiket: 'Günlükler',                 faz: 'F10', aciklama: 'access.log, error.log canlı izleme + WebSocket tail.' },
  'cron':            { etiket: 'Zamanlanmış Görevler',      faz: 'F8',  aciklama: 'Per-user crontab editörü.' },
  'git':             { etiket: 'Git',                       faz: 'F9',  aciklama: 'Repo bağla, deploy key, webhook ile otomatik pull.' },
  'composer':        { etiket: 'PHP Composer',              faz: 'F3',  aciklama: 'composer install/update web arayüzü.' },
  'performans':      { etiket: 'Performans',                aciklama: 'OPcache, gzip, lazy-load gibi hızlandırıcılar.' },
  'ssl':             { etiket: 'SSL/TLS Sertifikası',       faz: 'F7',  aciklama: 'Let\'s Encrypt otomatik kurulum + auto-renew.' },
  'sifre-koruma':    { etiket: 'Şifre Korumalı Dizinler',   faz: 'F7',  aciklama: '.htpasswd ile dizin koruma.' },
  'istatistik':      { etiket: 'İstatistikler',             faz: 'F10', aciklama: 'Disk, trafik, ziyaretçi raporları.' },
  'imunify':         { etiket: 'Imunify',                    aciklama: 'Antivirüs/WAF entegrasyonu.' },
}

export default function ToolPage() {
  const { id, slug } = useParams()
  const [d, setD] = useState<Domain | null>(null)
  const [hata, setHata] = useState<string | null>(null)

  useEffect(() => {
    if (!id) return
    api.get<Domain>(`/domains/${id}`).then(r => setD(r.data)).catch(e => setHata(apiHata(e)))
  }, [id])

  const meta = TOOL_META[slug || ''] || { etiket: slug || 'Araç', aciklama: 'Henüz uygulanmadı.' }

  return (
    <div className="px-4 py-4 sm:px-6 sm:py-5">
      <Breadcrumb items={[
        { etiket: 'Anasayfa', href: '/' },
        { etiket: 'Domainler', href: '/domainler' },
        { etiket: d?.alan_adi || '...', href: `/abonelikler/${id}` },
        { etiket: meta.etiket },
      ]} />

      <div className="flex items-center gap-3 mb-2">
        <h1 className="text-2xl font-semibold text-slate-900 dark:text-slate-100">{meta.etiket}</h1>
        {meta.faz && (
          <span className="text-[10px] font-semibold uppercase tracking-wider bg-amber-100 dark:bg-amber-900/30 text-amber-800 dark:text-amber-200 px-2 py-0.5 rounded">
            {meta.faz} · Hazır Değil
          </span>
        )}
      </div>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-1">
        {d ? <>Domain: <Link to={`/abonelikler/${id}`} className="text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">{d.alan_adi}</Link></> : '...'}
      </p>
      <p className="text-sm text-slate-500 dark:text-slate-500 mb-6">{meta.aciklama}</p>
      {hata && <div className="mb-3 px-3 py-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-sm text-red-700 dark:text-red-300">{hata}</div>}

      <div className="bg-white dark:bg-slate-800 border-2 border-dashed border-slate-200 dark:border-slate-700 rounded-2xl p-12 text-center">
        <div className="w-16 h-16 mx-auto rounded-full bg-slate-100 dark:bg-slate-800 flex items-center justify-center mb-3">
          <svg className="w-8 h-8 text-slate-400 dark:text-slate-500" fill="none" stroke="currentColor" viewBox="0 0 24 24" strokeWidth={1.5}>
            <path strokeLinecap="round" strokeLinejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
        <h3 className="text-base font-semibold text-slate-700 dark:text-slate-300 mb-1">Yapım aşamasında</h3>
        <p className="text-sm text-slate-500 dark:text-slate-500">
          Bu modül {meta.faz ? <span className="font-mono text-brand-700 dark:text-brand-300">{meta.faz}</span> : 'sonraki fazlarda'} devreye girecek.
        </p>
        <Link to={`/abonelikler/${id}`} className="inline-block mt-4 text-sm text-brand-600 dark:text-brand-400 hover:text-brand-700 dark:text-brand-300 dark:hover:text-brand-300 font-medium">
          ← Domain panosuna dön
        </Link>
      </div>
    </div>
  )
}