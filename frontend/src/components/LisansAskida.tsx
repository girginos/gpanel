import { Link } from 'react-router-dom'

// Lisans askıda / süresi dolmuş bir eklentinin sekmesinde YÖNETİM YERİNE
// gösterilen landing. 🔴 Eklenti bundle'ı MOUNT EDİLMEZ — yönetim arayüzü hiç
// yüklenmez, dolayısıyla kullanıcı ürünü yönetemez. Gerçek engel zaten
// SERVER-SIDE'dır (eklenti proxy'si aktif=0 => 402); bu ekran yalnızca sekme
// kaybolmak yerine NEDEN'i söyler + yenileme yolunu verir.
export default function LisansAskida({ baslik }: { baslik: string }) {
  return (
    <div className="mx-auto mt-6 max-w-xl rounded-xl border border-amber-300/60 bg-amber-50 p-8 text-center dark:border-amber-500/30 dark:bg-amber-500/10">
      <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-500/20">
        <svg className="h-6 w-6 text-amber-600 dark:text-amber-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M12 9v4M12 17h.01M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
        </svg>
      </div>
      <h2 className="text-lg font-semibold text-amber-900 dark:text-amber-200">{baslik} — Lisans askıda</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-amber-800/90 dark:text-amber-300/90">
        Bu eklentinin lisansı şu anda askıda: süresi dolmuş, ödeme bekliyor veya başka bir sunucuya taşınmış olabilir. Yönetim, lisans yenilenene kadar kullanılamaz.
      </p>
      <div className="mt-5 flex flex-wrap items-center justify-center gap-3">
        <Link to="/eklentiler" className="rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white transition hover:bg-brand-700">
          Eklentiler — Lisansı Doğrula
        </Link>
        <a href="https://app.girginos.io/licenses" target="_blank" rel="noopener" className="rounded-lg border border-amber-400/50 px-4 py-2 text-sm font-medium text-amber-800 transition hover:bg-amber-100/50 dark:text-amber-300 dark:hover:bg-amber-500/10">
          Lisansı Yenile →
        </a>
      </div>
    </div>
  )
}
