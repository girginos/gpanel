// Alan adları kartındaki "ekle" eylemlerinin görünürlüğü (saf fonksiyon; test: internal/onyuzkapsam/ek_alan_test.go).
// Kural: limit 0 = sınırsız. alanKullanim ANA ALAN ADI DAHİL toplamdır (limit 1 → ana = 1 → eklenemez).
// Veri gelmeden (yuklendi=false) hiçbir eylem sunulmaz: ek alan sayısı bilinmeden vaat edilmez.
import { cevir } from './dil'

export type EklemeGirdisi = {
  alanLimit: number
  alanKullanim: number
  altLimit: number
  altKullanim: number
  yuklendi: boolean
}

export type EklemeEylemleri = { alanEkle: boolean; altEkle: boolean; bilgi: string | null }

export function eklemeEylemleri(g: EklemeGirdisi): EklemeEylemleri {
  if (!g.yuklendi) return { alanEkle: false, altEkle: false, bilgi: null }
  const alanEkle = g.alanLimit <= 0 || g.alanKullanim < g.alanLimit
  const altEkle = g.altLimit <= 0 || g.altKullanim < g.altLimit
  return { alanEkle, altEkle, bilgi: !alanEkle && !altEkle ? cevir('Paketinizde yeni alan adı hakkı yok') : null }
}

// Alan adı girişi için istemci doğrulaması. Boş string = geçerli; aksi halde TR hata anahtarı
// (çağıran cevir ile çevirir). Sunucu yine kendi doğrulamasını yapar; bu yalnız erken geri bildirim.
export function alanAdiHatasi(girdi: string): string {
  const s = girdi.trim()
  if (!s) return ''
  if (/\s/.test(s)) return 'Alan adında boşluk olamaz.'
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) return 'Protokol (http://) yazmayın; yalnız alan adını girin.'
  if (/[/?#@:]/.test(s)) return 'Yol, port ya da kullanıcı bilgisi yazmayın; yalnız alan adını girin.'
  if (s !== s.toLowerCase()) return 'Alan adı küçük harfle yazılmalıdır.'
  if (s.length > 253) return 'Alan adı en çok 253 karakter olabilir.'
  if (!s.includes('.')) return 'Alan adı en az bir nokta içermelidir (örn. ornek.com).'
  const etiketler = s.split('.')
  for (const e of etiketler) {
    if (!e) return 'Alan adında boş bölüm (ardışık ya da baştaki/sondaki nokta) olamaz.'
    if (e.length > 63) return 'Alan adının her bölümü en çok 63 karakter olabilir.'
    if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(e)) return 'Alan adı yalnız harf, rakam ve tire içerebilir; bölümler tireyle başlayıp bitemez.'
  }
  if (/^[0-9.]+$/.test(s)) return 'IP adresi değil, alan adı girin.'
  return ''
}

// Unicode girişi (ör. "Örnek.com") punycode'a çevirmek sunucuya aittir; istemci yalnız küçük harfe indirir.
export const alanAdiNormal = (girdi: string): string => girdi.trim().toLowerCase()
