import EklentiEkrani from '@/components/EklentiEkrani'

// Tehdit İstihbaratı (ETİS) eklenti ekranı.
//
// 🔴 Eskiden bu dosya bundle yükleme + mount mantığını KENDİ içinde taşıyordu.
// Aynı mantık her yeni eklenti için kopyalanıyordu; genel yükleyiciye taşındı.
// ETİS'in eski `window.__gospTehditMount` sözleşmesi yükleyicide DESTEKLENİYOR,
// yani eklenti bundle'ı değişmeden çalışmaya devam eder.
export default function TehditPage() {
  return (
    <EklentiEkrani
      ad="tehdit"
      baslik="Tehdit İstihbaratı"
      aciklama="Erken tehdit istihbaratı — CVE/KEV beslemeleri, domain-bazlı otomatik yamalama kuralları ve canlı tehdit portalı."
    />
  )
}
