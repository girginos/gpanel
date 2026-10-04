// SatirIskeleti.tsx — veri yüklenirken "boş sayfa/Yükleniyor…" yerine satır iskeleti.
// Erişilebilirlik: aria-busy + ekran okuyucu için gizli metin; hareket azaltma tercihine saygı.
export function SatirIskeleti({ satir = 3, yukseklik = 'h-9', className = '', etiket = 'Yükleniyor…' }: {
  satir?: number; yukseklik?: string; className?: string; etiket?: string
}) {
  return (
    <div role="status" aria-busy="true" className={`space-y-2.5 ${className}`}>
      <span className="sr-only">{etiket}</span>
      {Array.from({ length: satir }, (_, i) => (
        <div key={i} aria-hidden="true"
          className={`${yukseklik} rounded-md bg-slate-100 dark:bg-dark-600/50 animate-pulse motion-reduce:animate-none`} />
      ))}
    </div>
  )
}
