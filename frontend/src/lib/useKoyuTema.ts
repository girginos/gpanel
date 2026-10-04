// Etkin temanın koyu olup olmadığını izler (React aboneliği).
//
// Kaynak <html class="dark"> — lib/theme.ts hem elle seçimde (üst çubuk düğmesi) hem
// "sistem" modunda OS tercihi değişince bu sınıfı günceller; MutationObserver ikisini de
// yakalar, yani yeniden yükleme gerekmez. recharts renkleri JS değeri olduğundan (CSS
// değişkeni değil) tema değişince bileşenin YENİDEN çizilip paleti yeniden seçmesi gerekir.
import { useSyncExternalStore } from 'react'

function abone(bildir: () => void): () => void {
  const mo = new MutationObserver(bildir)
  mo.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
  return () => mo.disconnect()
}
const anlik = () => document.documentElement.classList.contains('dark')

export function useKoyuTema(): boolean {
  return useSyncExternalStore(abone, anlik, () => false)
}
