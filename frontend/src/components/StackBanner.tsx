// GirginOS Platforms — login banner: KATMAN YIGINI.
//
// Sahte dashboard maketi YOK. Panelin gercekte yonettigi bes katman gosterilir;
// en ust katman musterinin sitesi, en alt cekirdek. Katman adlari uydurma
// degil — gPanel'in gercek sorumluluk alanlari.
//
// 🔴 ANIMASYON JS'TE DEGIL CSS'TE, VE OPAKLIGI GECIKMEYE BAGLAMAZ.
// Onceki surum framer-motion ile `initial={{opacity:0}}` kullaniyordu: icerik
// ancak animasyon KOSARSA gorunur oluyordu. Giris sayfasinda bu kabul edilemez —
// parca yuklenmezse, animasyon motoru hata verirse veya tarayici kareleri
// kisarsa (arka plan sekmesi) banner BOMBOS kalirdi. Simdi dinlenme hali
// gorunur; animasyon yalnizca `transform` ekler. Hic kosmazsa da sayfa dogru.
import '@/stack-banner.css'
import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'
import type { CSSProperties } from 'react'

const SB_EN: Record<string, string> = {
  'Siteniz en üstte.': 'Your site sits on top.',
  'Altındaki her katman panelin işi.': 'Every layer beneath it is the panel’s job.',
  'siteniz': 'your site',
  'alan adı · posta · yedek': 'domains · mail · backups',
  'güvenlik': 'security',
  'güvenlik duvarı · SSL · antivirüs': 'firewall · SSL · antivirus',
  'çalışma': 'runtime',
  'PHP-FPM · nginx · MariaDB': 'PHP-FPM · nginx · MariaDB',
  'izolasyon': 'isolation',
  'systemd dilimi · kota · CageFS': 'systemd slice · quota · CageFS',
  'çekirdek': 'kernel',
  'AlmaLinux · kernel': 'AlmaLinux · kernel',
  'çalışıyor': 'live',
  'yönetilen': 'managed',
  'beş katman · tek panel': 'five layers · one panel',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (SB_EN[tr] || tr) : tr)

// Dipten tepeye. `p` = "size yakinlik" payi; CSS parlakligi bundan turetir.
const KATMANLAR = [
  { ad: 'çekirdek', alt: 'AlmaLinux · kernel', rozet: 'yönetilen', p: 0.08 },
  { ad: 'izolasyon', alt: 'systemd dilimi · kota · CageFS', rozet: 'yönetilen', p: 0.3 },
  { ad: 'çalışma', alt: 'PHP-FPM · nginx · MariaDB', rozet: 'yönetilen', p: 0.55 },
  { ad: 'güvenlik', alt: 'güvenlik duvarı · SSL · antivirüs', rozet: 'yönetilen', p: 0.78 },
  { ad: 'siteniz', alt: 'alan adı · posta · yedek', rozet: 'çalışıyor', p: 1 },
]

export default function StackBanner() {
  useTranslation()

  return (
    <section className="gp-stack">
      {/* sol: söz */}
      <div className="soz">
        <div className="marka">
          <h1>Girgin<em>OS</em></h1>
          <span>Platforms</span>
        </div>
        <p className="lead">
          <b>{cevir('Siteniz en üstte.')}</b>{' '}
          {cevir('Altındaki her katman panelin işi.')}
        </p>
      </div>

      {/* sağ: yığın — DOM sırası dipten tepeye, CSS column-reverse ile üste taşınır */}
      <div className="yigin">
        <div className="omurga" aria-hidden>
          <span className="sinyal" />
        </div>

        {KATMANLAR.map((k, i) => (
          <div
            key={k.ad}
            className={`kat${k.p === 1 ? ' tepe' : ''}`}
            style={{ '--p': k.p, '--i': i } as CSSProperties}
          >
            <span className="nokta" aria-hidden />
            <div>
              <h3>{cevir(k.ad)}</h3>
              <p>{cevir(k.alt)}</p>
            </div>
            <span className="rozet">{cevir(k.rozet)}</span>
          </div>
        ))}
      </div>

      <div className="altbilgi">{cevir('beş katman · tek panel')}</div>
    </section>
  )
}
