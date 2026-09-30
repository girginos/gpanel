import axios, { AxiosError } from 'axios'
import { useAuth } from '@/store/auth'
import { useHesapAski } from '@/store/hesapAski'

const baseURL = (import.meta.env.VITE_API_BASE as string) || '/api/v1'

export const api = axios.create({
  baseURL,
  timeout: 30_000,
})

api.interceptors.request.use((cfg) => {
  const tok = useAuth.getState().token
  if (tok) {
    cfg.headers = cfg.headers || {}
    cfg.headers.Authorization = `Bearer ${tok}`
  }
  return cfg
})

// Askıdaki hesabın ortak 403'ü (sunucu: httpx.WriteHesapAskida). Metin değil KOD
// eşleştirilir; askı dışı 403'ler (başka kiracı, yetki yok) kodsuzdur.
export const HESAP_ASKIDA = 'hesap_askida'

export function hesapAskidaMi(err: unknown): boolean {
  const y = (err as AxiosError<{ kod?: unknown }>)?.response
  return y?.status === 403 && y.data?.kod === HESAP_ASKIDA
}

api.interceptors.response.use(
  (r) => r,
  (err: AxiosError<{ hata?: string }>) => {
    if (err.response?.status === 401) {
      const s = useAuth.getState()
      if (s.token) s.cikis()
    } else if (hesapAskidaMi(err)) {
      // Açık oturumda hesap askıya alındı: uygulama tam ekran askı sayfasına geçer.
      useHesapAski.getState().askiyaGec(useAuth.getState().kullanici?.rol === 'user' ? 'musteri' : 'yonetim')
    }
    return Promise.reject(err)
  },
)

export function apiHata(err: unknown, varsayilan = 'Beklenmeyen bir hata oluştu'): string {
  const e = err as AxiosError<{ hata?: unknown; error?: unknown }>
  // Ortak gövde {"hata": …} (httpx.WriteError); bazı paketler (portyonetim, hostuyg,
  // panelhost, ipyonetim…) {"error": …} döner — okunmazsa 409 gerekçesi gibi sunucu
  // mesajı kaybolup "Request failed with status code 409" görünürdü.
  const govde = e?.response?.data
  for (const m of [govde?.hata, govde?.error]) {
    if (typeof m === 'string' && m !== '') return m
  }
  // Axios'un ham İngilizce taşıyıcı hataları ("Network Error", "timeout of
  // 0ms exceeded") Türkçe panelde kullanıcıya hiçbir şey anlatmıyordu.
  const kod = (e as { code?: string })?.code
  if (kod === 'ECONNABORTED' || /timeout/i.test(e?.message || '')) {
    return 'İşlem zaman aşımına uğradı — sunucu yanıt vermedi'
  }
  if (kod === 'ERR_NETWORK' || e?.message === 'Network Error') {
    return 'Sunucuya ulaşılamadı — bağlantınızı kontrol edin'
  }
  const durum = e?.response?.status
  if (durum === 413) return 'Dosya çok büyük — sunucu sınırını aşıyor'
  if (durum === 502 || durum === 504) return 'Sunucu şu an yanıt vermiyor, biraz sonra tekrar deneyin'
  if (e?.message) return e.message
  return varsayilan
}
