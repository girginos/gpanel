import EklentiEkrani from '@/components/EklentiEkrani'

// Marka (Whitelabel) eklenti ekrani.
//
// Ekranin TAMAMI eklentiden gelir (app.js). Core burada yalniz genel
// yukleyiciyi cagirir — eklentinin adini bilir, icerigini bilmez.
export default function MarkaPage() {
  return (
    <EklentiEkrani
      ad="whitelabel"
      baslik="Marka"
      aciklama="Panelin gorunen kimligi: ad, logo, tarayici sekmesi basligi, vurgu rengi ve giris sayfasi metinleri."
    />
  )
}
