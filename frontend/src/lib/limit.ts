// Hostinge özel limitler (D1 "özel plan = üst yazım") — tip + API sözleşmesi.
//
// Sunucu: GET/PUT/DELETE /domains/{id}/limitler (SahipYonetim: yönetici her
// hostingte, bayi yalnız kendi hostingte, müşteri asla). Hosting katalog planında
// kalır; yalnız farklı alanlar hosting üzerinde "üst yazım" olarak durur ve plan
// değişince KORUNUR. Birim / sıfır anlamı / sınırlar sunucudan gelir (tek kaynak:
// internal/efektiflimit) — ön yüz kendi "0 = sınırsız" varsayımını yapmaz.
import { api } from '@/lib/api'

export type LimitSifir = 'sinirsiz' | 'otomatik' | 'yasak' | 'kullanilmaz'
export type LimitKaynak = 'ust' | 'alan' | 'plan' | 'yok'

export type LimitAlan = {
  ad: string
  grup: 'disk' | 'kaynak' | 'io' | 'db' | 'sayi' | 'posta'
  birim: string
  sifir: LimitSifir
  min: number
  max: number
  plan: number | null // planın değeri (gizliyse / plan yoksa null)
  ust: number | null // hostinge özel değer (yoksa null = plandan)
  efektif: number | null // geçerli değer (gizliyse null)
  kaynak: LimitKaynak
  gizli?: boolean // değer bu rol için gösterilmez (kökün planı/kilidi)
  kilitli?: boolean // yönetici belirledi — bayi değiştiremez
  ic?: boolean // kökün iç ayarı (IO/process/governor…) — bayi hiç yazamaz; gövdeye girmez
}

export type LimitYanit = {
  plan: { id: number; ad: string } | null
  eski_ozel_kopya?: boolean
  ozel_limit: boolean
  alanlar: LimitAlan[]
}

export const limitGetir = (id: string | number) =>
  api.get<LimitYanit>(`/domains/${id}/limitler`).then(r => r.data)

// TAM değiştirme: gönderilmeyen alan plana döner. Kilitli alanlar gönderilmez
// (sunucu mevcut değerinde tutar).
export const limitKaydet = (id: string | number, ust: Record<string, number | null>) =>
  api.put<LimitYanit>(`/domains/${id}/limitler`, { ust }).then(r => r.data)

export const limitSifirla = (id: string | number) =>
  api.delete<LimitYanit>(`/domains/${id}/limitler`).then(r => r.data)
