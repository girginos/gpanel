// gosp-dark-swept
// gosp-dark-swept-v2
import { useNavigate } from 'react-router-dom'
import type { Domain } from './DomainList'
import ToolCard from './ToolCard'

const ICONS = {
  baglanti:  'M13.828 10.172a4 4 0 015.656 5.656l-3 3a4 4 0 01-5.656-5.656m.172-5.172a4 4 0 00-5.656 5.656l-3 3a4 4 0 005.656 5.656',
  dosyalar:  'M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v9a2 2 0 01-2 2H5a2 2 0 01-2-2V7z',
  db:        'M4 7c0-1.657 3.582-3 8-3s8 1.343 8 3-3.582 3-8 3-8-1.343-8-3zm0 0v10c0 1.657 3.582 3 8 3s8-1.343 8-3V7M4 12c0 1.657 3.582 3 8 3s8-1.343 8-3',
  ftp:       'M3 16V8a2 2 0 012-2h6l2 2h5a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2zM9 12l3-3 3 3M12 9v6',
  yedek:     'M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1M16 12l-4 4-4-4M12 16V4',
  kopya:     'M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z',
  php:       'M12 14l9-5-9-5-9 5 9 5zm0 0l6.16-3.422a12.083 12.083 0 01.665 6.479A11.952 11.952 0 0012 20.055a11.952 11.952 0 00-6.824-2.998 12.078 12.078 0 01.665-6.479L12 14z',
  log:       'M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
  cron:      'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z',
  git:       'M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1',
  composer:  'M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3v6M9 12h6',
  hizmet:    'M5 8h14M5 8a2 2 0 110-4h14a2 2 0 110 4M5 8v10a2 2 0 002 2h10a2 2 0 002-2V8m-9 4h4',
  ssl:       'M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z',
  kilit:     'M12 11c0 3.517-1.009 6.799-2.753 9.571m-3.44-2.04l.054-.09A13.916 13.916 0 008 11a4 4 0 118 0c0 1.017-.07 2.019-.203 3',
  istatistik:'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z',
  imunify:   'M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622',
  ssh:       'M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z',
  wordpress: 'M12 21a9 9 0 100-18 9 9 0 000 18zm0 0c2.5-2.5 3-6 3-9s-.5-6.5-3-9m0 18c-2.5-2.5-3-6-3-9s.5-6.5 3-9M3.6 9h16.8M3.6 15h16.8',
  subdomain: 'M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064',
  dns:       'M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01',
  redis:     'M13 10V3L4 14h7v7l9-11h-7z',
}

function Grup({ baslik, children }: { baslik: string; children: React.ReactNode }) {
  return (
    <section className="mb-5 last:mb-0">
      <h3 className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-500 mb-2">{baslik}</h3>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">{children}</div>
    </section>
  )
}

export default function DomainPano({ domain }: { domain: Domain }) {
  const navigate = useNavigate()
  const git = (slug: string) => () => navigate(`/abonelikler/${domain.id}/${slug}`)
  return (
    <div>
      <Grup baslik="Uygulamalar">
        <ToolCard etiket="WordPress" aciklama="1-tıkla kurulum · yönetim" ikon={ICONS.wordpress} renk="sky" onClick={git('wordpress')} />
      </Grup>

      <Grup baslik="Alan Adı ve DNS">
        <ToolCard etiket="DNS Yönetimi"          aciklama="A, MX, TXT, CNAME kayıtları" ikon={ICONS.dns}       renk="sky"  onClick={git('dns')} />
        <ToolCard etiket="Subdomainler"          aciklama="Alt alan adları"   ikon={ICONS.subdomain} renk="teal" onClick={git('subdomainler')} />
      </Grup>

      <Grup baslik="Dosyalar ve Veritabanları">
        <ToolCard etiket="Bağlantı Bilgisi"      aciklama="FTP, veri tabanı"  ikon={ICONS.baglanti} renk="emerald" onClick={git('baglanti')} />
        <ToolCard etiket="Dosyalar"              aciklama="Dosya yöneticisi"  ikon={ICONS.dosyalar} renk="amber"   faz="F6"  onClick={git('dosyalar')} />
        <ToolCard etiket="Veritabanları"         aciklama={domain.db_adi}     ikon={ICONS.db}       renk="violet"  faz="F5"  onClick={git('veritabanlari')} />
        <ToolCard etiket="FTP"                   aciklama="FTP hesapları"     ikon={ICONS.ftp}      renk="sky"     faz="F4"  onClick={git('ftp')} />
        <ToolCard etiket="Yedekle ve Geri Yükle" aciklama="Yedek yönetimi"    ikon={ICONS.yedek}    renk="rose"    faz="F12" onClick={git('yedekler')} />
        <ToolCard etiket="Web Sitesini Kopyala"  aciklama="Klonlama"          ikon={ICONS.kopya}    renk="sky"     onClick={git('kopyala')} />
      </Grup>

      <Grup baslik="Geliştirme Araçları">
        <ToolCard etiket="PHP"                   aciklama={`Sürüm ${domain.php_surum}`} ikon={ICONS.php}      renk="indigo" faz="F3" onClick={git('php')} />
        <ToolCard etiket="Günlükler"             aciklama="access, error"  ikon={ICONS.log}      renk="slate"  faz="F10" onClick={git('gunlukler')} />
        <ToolCard etiket="Zamanlanmış Görevler"  aciklama="Cron"            ikon={ICONS.cron}     renk="teal"   faz="F8"  onClick={git('cron')} />
        <ToolCard etiket="Git"                   aciklama="Depo entegrasyonu" ikon={ICONS.git}    renk="orange" faz="F9"  onClick={git('git')} />
        <ToolCard etiket="PHP Composer"          aciklama="Paket yöneticisi"  ikon={ICONS.composer} renk="amber" faz="F3"  onClick={git('composer')} />
        <ToolCard etiket="Performans"            aciklama="Hızlandırıcılar"   ikon={ICONS.hizmet} renk="emerald" onClick={git('performans')} />
        <ToolCard etiket="Redis Cache"           aciklama="İzole nesne cache · hızlandırıcı" ikon={ICONS.redis} renk="rose" onClick={git('redis')} />
      </Grup>

      <Grup baslik="Güvenlik">
        <ToolCard
          etiket="SSL/TLS Sertifikaları"
          aciklama={domain.ssl ? `Bitiş: ${domain.ssl_bitis || '—'}` : 'Let’s Encrypt'}
          ikon={ICONS.ssl}
          renk={domain.ssl ? 'emerald' : 'rose'}
          faz="F7"
          uyari={!domain.ssl ? 'Alan adı korunmadı' : undefined}
          onClick={git('ssl')}
        />
        <ToolCard etiket="Şifre Korumalı Dizinler" aciklama=".htpasswd"       ikon={ICONS.kilit}      renk="amber" faz="F7" onClick={git('sifre-koruma')} />
        <ToolCard etiket="İstatistikler"            aciklama="Trafik analizi"  ikon={ICONS.istatistik} renk="indigo" faz="F10" onClick={git('istatistik')} />
        <ToolCard etiket="Imunify"                  aciklama="Antivirüs"        ikon={ICONS.imunify}    renk="emerald" onClick={git('imunify')} />
        <ToolCard
          etiket="SSH Erişimi"
          aciklama={domain.ssh_erisim ? 'Açık' : 'Kapalı'}
          ikon={ICONS.ssh}
          renk={domain.ssh_erisim ? 'emerald' : 'slate'}
          onClick={git('ssh-erisim')}
        />
      </Grup>
    </div>
  )
}