// Müşteri ana sayfası — bileşen dışı yardımcılar (hook + DOM).
import { useEffect, useRef } from 'react'
import type { KeyboardEvent as ReactKeyboardEvent, RefObject } from 'react'

// Açılır panel: dışarı tıklama ya da Esc ile kapanır.
export function useDisariKapat(acik: boolean, ref: RefObject<HTMLElement>, kapat: () => void) {
  const kapatRef = useRef(kapat)
  useEffect(() => {
    kapatRef.current = kapat
  })
  useEffect(() => {
    if (!acik) return
    const fare = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) kapatRef.current()
    }
    const tus = (e: KeyboardEvent) => {
      if (e.key === 'Escape') kapatRef.current()
    }
    document.addEventListener('mousedown', fare)
    document.addEventListener('keydown', tus)
    return () => {
      document.removeEventListener('mousedown', fare)
      document.removeEventListener('keydown', tus)
    }
  }, [acik, ref])
}

// Ok tuşlarıyla bir listedeki düğmeler arasında gezinme.
export function okTusuylaGez(e: ReactKeyboardEvent<HTMLElement>) {
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
  e.preventDefault()
  const l = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('button:not([disabled])'))
  if (!l.length) return
  const i = l.indexOf(document.activeElement as HTMLButtonElement)
  const s = e.key === 'ArrowDown' ? (i + 1) % l.length : (i - 1 + l.length) % l.length
  l[s].focus()
}

// Panoya kopyala: Clipboard API, olmazsa (HTTP, eski tarayıcı) gizli textarea yedeği.
export async function panoyaKopyala(deger: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(deger)
      return true
    }
  } catch {
    /* yedeğe düş */
  }
  try {
    const alan = document.createElement('textarea')
    alan.value = deger
    alan.setAttribute('readonly', '')
    alan.style.position = 'fixed'
    alan.style.opacity = '0'
    document.body.appendChild(alan)
    alan.select()
    const ok = document.execCommand('copy')
    document.body.removeChild(alan)
    return ok
  } catch {
    return false
  }
}
