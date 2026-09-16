// Login banner — DotGlobeHero uzerine kurulu.
//
// Demo'nun stil dili BIREBIR alindi: gradyan pill + iki ping noktasi, iri
// font-black baslik, bg-clip-text gradyan + bulanik kopya, kalin gradyan alt
// cizgi, vurgu zeminli paragraf. Demo `Inter, system-ui` yazi ailesini satir
// ici veriyor; aynen korundu.
//
// 🔴 PALET TEK RENK: siyah/beyaz/gri. Referans gorsel bastan sona notr;
// marka mavisi HIC gecmiyor. Bir ara `brand-500` ile mavi vurgu koymustum,
// referansa aykiriydi — hepsi cikarildi. Gri tonlarinda `slate` yerine
// `neutral` kullaniliyor: slate mavi tinti tasir ve notr zeminde belli olur.
//
// SHADCN TOKEN ESLESMESI (bu projede o tokenlar YOK):
//   primary          -> white
//   primary/xx       -> white/xx
//   foreground       -> white
//   muted-foreground -> neutral-400
//
// Demo'dan alinmayan TEK sey iki CTA butonu: yanindaki sutunda giris formu var,
// banner'a ikinci eylem cagrisi koymak kullaniciyi asil isten ayirir.
import { useEffect, useState } from 'react'
import { useMarka } from '@/lib/marka'
import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'
import { DotGlobeHero } from '@/components/ui/globe-hero'
import '@/globe-banner.css'

const YAZI = { fontFamily: 'Inter, system-ui, sans-serif' }

const GB_EN: Record<string, string> = {
  'KÜRESEL ALTYAPI': 'GLOBAL INFRASTRUCTURE',
  'Sunucularınız.': 'Your servers.',
  'Tek panel.': 'One panel.',
  'Alan adı, posta, yedek ve güvenliği': 'Domains, mail, backups and security',
  'tek panelden yönetin': 'managed from one panel',
  'Kurulumdan yedeğe kadar her katman aynı yerde.':
    'From provisioning to backups, every layer in one place.',
}
const cevir = (tr: string): string => (i18n.language === 'en' ? (GB_EN[tr] || tr) : tr)

export default function GlobeBanner() {
  useTranslation()
  const [azHareket, setAzHareket] = useState(false)

  useEffect(() => {
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)')
    const uygula = () => setAzHareket(mq.matches)
    uygula()
    mq.addEventListener('change', uygula)
    return () => mq.removeEventListener('change', uygula)
  }, [])

  // 🔴 MARKA SABİT KODLU DEĞİL. Burada "gPanel" gömülüydü; whitelabel panel
  // adını değiştirdiğinde giriş ekranındaki bu dev başlık ESKİ markayı
  // göstermeye devam ediyordu. Kısa ad yoksa panel adına düşeriz.
  const marka = useMarka()
  const markaAd = (marka.kisa_ad || marka.panel_adi || 'gPanel').trim()
  const markaIlk = markaAd.slice(0, 1)
  const markaKalan = markaAd.slice(1)

  return (
    <div className="relative hidden h-full min-h-[700px] overflow-hidden bg-gradient-to-br from-[#0a0d13] via-[#0a0d13] to-[#16181c] lg:block">
      <DotGlobeHero
        rotationSpeed={azHareket ? 0 : 0.004}
        globeRadius={1.15}
        // 🔴 Kaynakta `hsl(var(--foreground))` idi; THREE CSS degiskeni cozemez,
        // sessizce beyaza duserdi. Demo'daki notr acik ton dogrudan veriliyor.
        color="#dfe6f0"
        opacity={0.16}
        segments={64}
        className="bg-transparent"
      >
        {/* Demo'daki ortu katmanlari (z-0: icerik ustte kalir) */}
        <div className="pointer-events-none absolute inset-0 z-0 bg-gradient-to-t from-[#0a0d13]/85 via-transparent to-[#0a0d13]/60" />
        <div className="pointer-events-none absolute left-1/4 top-1/4 z-0 h-96 w-96 animate-pulse rounded-full bg-white/[0.05] blur-3xl" />
        <div className="pointer-events-none absolute bottom-1/4 right-1/4 z-0 h-64 w-64 animate-pulse rounded-full bg-white/[0.03] blur-3xl" />

        {/* Pill — kurenin USTUNDE, merkez blogun disinda */}
        <div className="pointer-events-none absolute inset-x-0 top-[7%] z-10 flex justify-center">
          <span className="gb-pill relative inline-flex items-center gap-3 rounded-full border border-white/20 bg-white/[0.08] px-6 py-3 shadow-2xl backdrop-blur-xl">
            <span className="absolute inset-0 animate-pulse rounded-full bg-gradient-to-r from-white/10 via-transparent to-white/10" />
            <span className="h-2 w-2 animate-ping rounded-full bg-white" />
            <span className="relative z-10 text-sm font-bold uppercase tracking-wider text-white">
              {cevir('KÜRESEL ALTYAPI')}
            </span>
            <span className="h-2 w-2 animate-ping rounded-full bg-white [animation-delay:500ms]" />
          </span>
        </div>

        <div className="pointer-events-none relative z-10 w-full space-y-8 px-8 text-center">
          {/* Marka — tek satir (senin istegin), demo'nun font-black olcegiyle */}
          <h1
            className="gb-baslik relative select-none whitespace-nowrap text-6xl font-black leading-[0.85] tracking-tighter text-white md:text-7xl lg:text-8xl"
            style={YAZI}
          >
            <span
              aria-hidden
              className="absolute inset-0 scale-105 font-black text-white opacity-35 blur-2xl"
              style={YAZI}
            >
              {markaAd}
            </span>
            <span className="relative z-10">
              {/* Vurgu markanin ayirt edici harfi 'g' uzerinde; uzun sozcuk
                  beyaz kalarak okunur oluyor. */}
              <span className="font-black text-white">
                {markaIlk}
              </span>
              <span className="font-light text-white/60">{markaKalan}</span>
            </span>
          </h1>

          {/* Demo'daki kalin gradyan alt cizgi */}
          <span className="gb-bar mx-auto block h-3 w-[70%] rounded-full bg-gradient-to-r from-white via-white/80 to-transparent shadow-lg shadow-white/25" />

          <div className="gb-metin mx-auto max-w-3xl space-y-4">
            <p className="text-xl font-medium leading-relaxed text-white md:text-2xl" style={YAZI}>
              {cevir('Alan adı, posta, yedek ve güvenliği')}{' '}
              <span className="rounded-md bg-white/[0.14] px-2 py-1 font-semibold text-white">
                {cevir('tek panelden yönetin')}
              </span>
            </p>
            <p className="text-lg leading-relaxed text-white/80">
              {cevir('Kurulumdan yedeğe kadar her katman aynı yerde.')}
            </p>
          </div>
        </div>
      </DotGlobeHero>
    </div>
  )
}
