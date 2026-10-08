// Silinmiş hesap yedekleri — SAF yardımcılar (React / i18n / ağ bağımsız).
// Kullanan: components/YedekSilinenler (grup başlığı, onay metinleri), pages/BackupYonetimiPage
// (özet kartı), pages/DomainsPage (yedek devri bildirimi). Çeviri çağıranda yapılır; burada yalnız
// veri → metin dönüşümü vardır. Birim test altyapısı (vitest) yok; mantık bu yüzden saf tutuldu.

// Bir yedek grubunun (sk) sahibi = alan adı + bayi. reseller_id 0 = yönetici (kök hesap).
export type Sahip = { alan_adi: string; reseller_id: number; dosya_sayisi: number }

type SahipKaynagi = {
  alan_adi: string
  reseller_id?: number | null
  dosya_sayisi: number
  sahipler?: Sahip[] | null
}

const ad = (s: Sahip): string => String(s.alan_adi ?? '').trim() || '—'

// Gruptaki TÜM farklı sahipler. Aynı sistem kullanıcısı aynı müşteri demek değildir
// (girgin.com ve girgin.net ikisi de c_girgin grubuna düşebilir). Sunucu `sahipler` gönderir;
// yoksa ya da boşsa (eski sunucu) grubun kendi alan adı + bayisi tek sahip sayılır → liste hiç boş kalmaz.
export function grupSahipleri(g: SahipKaynagi): Sahip[] {
  if (Array.isArray(g.sahipler) && g.sahipler.length > 0) return g.sahipler
  return [{ alan_adi: g.alan_adi, reseller_id: g.reseller_id ?? 0, dosya_sayisi: g.dosya_sayisi }]
}

// Tekrarsız alan adları, ilk görünüş sırasıyla: "girgin.com, girgin.net".
export function sahipAlanAdlari(sahipler: Sahip[]): string {
  const gorulen = new Set<string>()
  const adlar: string[] = []
  for (const s of sahipler) {
    const a = ad(s)
    const k = a.toLowerCase()
    if (gorulen.has(k)) continue
    gorulen.add(k)
    adlar.push(a)
  }
  return adlar.join(', ')
}

// "girgin.com (yönetici), girgin.net (bayi #3)". Bayi etiketini (çevrilmiş) çağıran üretir.
// `adet` verilirse her sahibin yedek sayısı eklenir: "girgin.com (yönetici) · 3 yedek; girgin.net (bayi #3) · 2 yedek".
export function sahipOzeti(sahipler: Sahip[], bayiAdi: (resellerID: number) => string, adet?: (n: number) => string): string {
  return sahipler
    .map(s => `${ad(s)} (${bayiAdi(s.reseller_id)})${adet ? ` · ${adet(s.dosya_sayisi)}` : ''}`)
    .join(adet ? '; ' : ', ')
}

// bağla 409 (alan_adi_farkli) gövdesinden yalnız okunan alanlar.
export type FarkliGovde = {
  farklilar?: Sahip[] | null
  yedek_alan_adi?: string
  yedek_reseller_id?: number
}

// Onayda gösterilecek TÜM farklı (alan adı, bayi) çiftleri: sunucunun `farklilar[]` listesi; yoksa
// (yalnız ilk farklı çifti döndüren eski sunucu) `yedek_*` alanları; o da yoksa satırdaki bilinen sahipler.
export function farkliSahipler(govde: FarkliGovde | null | undefined, satirSahipleri: Sahip[]): Sahip[] {
  if (govde && Array.isArray(govde.farklilar) && govde.farklilar.length > 0) return govde.farklilar
  if (govde && govde.yedek_alan_adi) {
    return [{ alan_adi: govde.yedek_alan_adi, reseller_id: govde.yedek_reseller_id ?? 0, dosya_sayisi: 0 }]
  }
  return satirSahipleri
}

// "5 yedek" / "1 backup" / "5 backups". Türkçede sayıdan sonra çoğul eki yoktur; İngilizcede 1 dışında çoğul.
export function yedekAdedi(n: number, en: boolean): string {
  const k = Number.isFinite(n) ? n : 0
  return en ? `${k} ${k === 1 ? 'backup' : 'backups'}` : `${k} yedek`
}
