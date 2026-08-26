import { useEffect, useRef } from 'react'
import { useAuth } from '@/store/auth'
import Breadcrumb from '@/components/Breadcrumb'

// Tehdit İstihbarat (ETİS) eklenti ekranı — core, eklentinin bundle'ını yükler ve
// mount sözleşmesiyle (window.__gospTehditMount) kendi kök div'imize monte eder.
// Bundle auth-dışı servis edilir (<script src> JWT taşıyamaz); eklenti API'si
// (/api/v1/eklenti/tehdit/*) core proxy'sinden JWT ile korunur — token'ı geçiyoruz.
declare global {
  interface Window {
    __gospTehditMount?: (el: HTMLElement, opts: { apiBase: string; token: string }) => (() => void) | void
  }
}

export default function TehditPage() {
  const kok = useRef<HTMLDivElement>(null)
  const token = useAuth((s) => s.token)

  useEffect(() => {
    let temizle: (() => void) | void
    const mount = () => {
      if (window.__gospTehditMount && kok.current) {
        temizle = window.__gospTehditMount(kok.current, { apiBase: '/api/v1/eklenti/tehdit', token: token || '' })
      }
    }
    if (window.__gospTehditMount) {
      mount()
    } else {
      const id = 'etis-bundle'
      let s = document.getElementById(id) as HTMLScriptElement | null
      if (!s) {
        s = document.createElement('script')
        s.id = id
        s.src = '/api/v1/eklenti-bundle/tehdit/app.js'
        s.onload = mount
        s.onerror = () => {
          if (kok.current) kok.current.innerHTML =
            '<div style="color:#94a3b8;padding:30px;text-align:center">Eklenti yüklenemedi — Tehdit İstihbaratı kurulu/etkin değil.</div>'
        }
        document.body.appendChild(s)
      } else {
        s.addEventListener('load', mount)
      }
    }
    return () => {
      if (typeof temizle === 'function') temizle()
    }
  }, [token])

  return (
    <div>
      <Breadcrumb items={[{ etiket: 'Anasayfa', href: '/' }, { etiket: 'Tehdit İstihbaratı' }]} />
      <h1 className="text-xl font-semibold text-slate-900 dark:text-slate-100 mb-1">Tehdit İstihbaratı</h1>
      <p className="text-sm text-slate-500 dark:text-slate-400 mb-4">
        Erken tehdit istihbaratı — CVE/KEV beslemeleri, domain-bazlı otomatik yamalama kuralları ve canlı tehdit portalı.
      </p>
      <div ref={kok} />
    </div>
  )
}
