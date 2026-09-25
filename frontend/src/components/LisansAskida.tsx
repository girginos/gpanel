import { Link } from 'react-router-dom'
import { butonSinifi } from '@/components/ui'
import { I, Ikon } from '@/components/Ikon'
import { useRol, yoneticiMi } from '@/lib/rol'

// Lisans askıda / süresi dolmuş bir eklentinin sekmesinde YÖNETİM YERİNE
// gösterilen landing. 🔴 Eklenti bundle'ı MOUNT EDİLMEZ — yönetim arayüzü hiç
// yüklenmez, dolayısıyla kullanıcı ürünü yönetemez. Gerçek engel zaten
// SERVER-SIDE'dır (eklenti proxy'si aktif=0 => 402); bu ekran yalnızca sekme
// kaybolmak yerine NEDEN'i söyler + yenileme yolunu verir.
// Lisansı yalnız panel sahibi (yönetici) yenileyebilir: /eklentiler'in kataloğu
// (/eklenti-katalog) AdminOnly, lisans portalı panel sahibinin hesabıdır. Bayi ve
// müşteri yönetici bağlantılarını görmez, hizmet sağlayıcısına yönlendirilir.
export default function LisansAskida({ baslik }: { baslik: string }) {
  const yonetici = yoneticiMi(useRol())
  return (
    <div className="mx-auto mt-6 max-w-xl rounded-xl border border-amber-300/60 bg-amber-50 p-8 text-center dark:border-amber-500/30 dark:bg-amber-500/10">
      <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-amber-100 dark:bg-amber-500/20">
        <Ikon d={I.uyari} kalinlik={2} className="h-6 w-6 text-amber-600 dark:text-amber-400" />
      </div>
      <h2 className="text-lg font-semibold text-amber-900 dark:text-amber-200">{baslik} — Lisans askıda</h2>
      <p className="mx-auto mt-2 max-w-md text-sm text-amber-800/90 dark:text-amber-300/90">
        Bu eklentinin lisansı şu anda askıda: süresi dolmuş, ödeme bekliyor veya başka bir sunucuya taşınmış olabilir. Yönetim, lisans yenilenene kadar kullanılamaz.
      </p>
      {!yonetici ? (
        <p className="mx-auto mt-3 max-w-md text-sm font-medium text-amber-900 dark:text-amber-200">
          Bu hizmet için lütfen hizmet sağlayıcınızla iletişime geçin.
        </p>
      ) : (
        <div className="mt-5 flex flex-wrap items-center justify-center gap-3">
          <Link to="/eklentiler" className={butonSinifi({ color: 'primary' })}>
            <Ikon d={I.yenile} className="size-4 shrink-0 -ms-1 me-2" />
            Eklentiler — Lisansı Doğrula
          </Link>
          <a href="https://app.girginos.io/licenses" target="_blank" rel="noopener" className={butonSinifi({ variant: 'outlined' })}>
            Lisansı Yenile
            <Ikon d={I.harici} className="size-4 shrink-0 ms-2 -me-1" />
          </a>
        </div>
      )}
    </div>
  )
}
