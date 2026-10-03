import type { ReactNode } from 'react'
import { Outlet } from 'react-router-dom'

// ═══════════════════════════════════════════════════════════════════════════
// TEK SAYFA KABUĞU (kullanıcı kararı 2026-09-30)
//
// Panelin içindeki (DashboardLayout <main>) HER sayfa aynı genişlikte ve içerik
// alanının ORTASINDA durur: geniş ekranda iki yanda eşit boşluk, dar ekranda tam
// genişlik. Genişlik tek kaynaktan gelir: styles.css @theme `--container-sayfa`
// (1280px, dolgu DAHİL) → `max-w-sayfa`. Tablolu sayfalar da bu genişliktedir.
//
// 🔴 SAYFA KENDİ KÖK max-w-* / mx-auto / DIŞ DOLGUSUNU YAZMAZ. Kök kapsayıcı
// (ör. `<div>` + Breadcrumb) yalnız iç yerleşimi (space-y, grid…) taşır; form ve
// ayar içerikleri gerekiyorsa KARTLARIN İÇİNDE daralır, dış genişlik değişmez.
//
// Nasıl bağlanır: App.tsx'te panel rotalarının tamamı `<Route element={<SayfaKabugu />}>`
// altındadır — yeni rota o bloğa eklenince kabuğu kendiliğinden alır. Kabuk DIŞI
// kalması gereken rota sessizce kaçamaz: ayrı `<Route element={<KabukDisi />}>`
// bloğuna, gerekçesiyle birlikte yazılır (bkz. App.tsx).
// ═══════════════════════════════════════════════════════════════════════════

/** Kabuğun kendisi (rota dışı kullanım: KabukDisi bloğundaki bir rotanın yalnız
 *  bir dalı kabuk içinde çizilecekse — ör. /abonelikler/:id yönetici görünümü). */
export function SayfaCercevesi({ children }: { children: ReactNode }) {
  return (
    <div className="mx-auto w-full min-w-0 max-w-sayfa px-4 py-4 sm:px-6 sm:py-5">
      {children}
    </div>
  )
}

/** Varsayılan düzen rotası: altındaki her sayfa kabuk içinde çizilir. */
export default function SayfaKabugu() {
  return (
    <SayfaCercevesi>
      <Outlet />
    </SayfaCercevesi>
  )
}

/** Açık istisna düzen rotası: altındaki sayfa kabuksuz (tam genişlik) çizilir.
 *  Buraya yalnız App.tsx'te gerekçesi yazılı rota girer; sayfa tam genişlik
 *  ZEMİN kullansa bile içerik genişliği yine `--container-sayfa`dır. */
export function KabukDisi() {
  return <Outlet />
}
