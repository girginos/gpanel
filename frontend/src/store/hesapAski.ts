import { create } from 'zustand'

// Hesap askısı — uygulama düzeyinde tek bayrak. Sunucu askıdaki hesaba her yoldan aynı
// 403'ü verir ({"kod":"hesap_askida"}, bkz. lib/api hesapAskidaMi): giriş formları ve
// açık oturumdaki ilk askı yanıtı bu bayrağı kaldırır, App paneli (menü, üst çubuk,
// sayfalar) HİÇ çizmeden tam ekran HesapAskidaPage'i gösterir. Panel sökülünce onun
// istekleri ve yoklamaları da durur — askı sayfası kendisi istek atmaz (döngü yok).
//
// Bayrak yalnız bellekte: sayfa yenilenince panel yeniden açılmayı dener; hesap hâlâ
// askıdaysa ilk istek bayrağı yine kaldırır, askı kaldırılmışsa panel normal açılır.
//
// kaynak: çıkışta hangi giriş formuna dönüleceği (müşteri → /cp/giris, diğerleri /giris).
export type AskiKaynagi = 'musteri' | 'yonetim'

type AskiDurumu = {
  askida: boolean
  kaynak: AskiKaynagi
  askiyaGec: (kaynak: AskiKaynagi) => void
  kapat: () => void
}

export const useHesapAski = create<AskiDurumu>((set) => ({
  askida: false,
  kaynak: 'yonetim',
  // İlk kaynak kazanır: eşzamanlı birkaç askı yanıtı durumu yeniden yazmaz.
  askiyaGec: (kaynak) => set((s) => (s.askida ? s : { askida: true, kaynak })),
  kapat: () => set({ askida: false }),
}))
