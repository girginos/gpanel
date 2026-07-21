// Duyarlı tablo deseni — TEK KAYNAK.
//
// < lg  : her satır bir KART. Hücreler alt alta; her hücre kendi etiketini
//         `data-etiket` özniteliğinden ::before ile yazar. Yatay kaydırma YOK.
// >= lg : gerçek tablo (eski masaüstü görünümü aynen korunur).
//
// Kullanım:
//   <table className={T.tablo}>
//     <thead className={T.baslikGrubu}><tr>{...<th className={T.baslik}>}</tr></thead>
//     <tbody className={T.govde}>
//       <tr className={T.satir}>
//         <td className={T.hucreBaslik}>domain.com</td>
//         <td className={T.hucre} data-etiket="PHP">8.3</td>
//         <td className={T.hucreAksiyon}>...</td>
//       </tr>
//     </tbody>
//   </table>
//
// KURAL: data-etiket metni ilgili <th> metniyle AYNI olmalı — mobilde tek
// bilgi taşıyıcısı odur. Etiketsiz bırakılan hücre mobilde bağlamsız kalır.

export const T = {
  tablo: 'w-full lg:min-w-[640px]',

  // Başlık satırı yalnız masaüstünde görünür; mobilde etiketi ::before taşıyor.
  baslikGrubu: 'hidden lg:table-header-group',
  baslik:
    'px-3 py-2.5 text-left text-[11px] font-semibold uppercase tracking-wider ' +
    'text-slate-500 dark:text-slate-400',

  govde: 'block lg:table-row-group',

  // Mobilde kart; masaüstünde sade satır.
  satir:
    'relative lg:static block lg:table-row mb-3 lg:mb-0 rounded-xl lg:rounded-none ' +
    'border border-slate-200 dark:border-slate-700 lg:border-0 ' +
    'lg:border-b lg:border-slate-100 dark:lg:border-slate-800 ' +
    'bg-white dark:bg-slate-800 lg:bg-transparent dark:lg:bg-transparent ' +
    'p-3 lg:p-0 shadow-sm lg:shadow-none',

  // Normal hücre: mobilde "etiket ......... değer" satırı.
  hucre:
    'flex items-start justify-between gap-3 py-1.5 lg:table-cell ' +
    'lg:px-3 lg:py-2.5 text-sm text-slate-700 dark:text-slate-300 ' +
    'before:shrink-0 before:text-[11px] ' +
    'before:font-semibold before:uppercase before:tracking-wider ' +
    'before:text-slate-400 dark:before:text-slate-500 before:pt-0.5 lg:before:hidden',

  // Birincil tanımlayıcı (domain adı, dosya adı…): mobilde kart başlığı.
  hucreBaslik:
    'block lg:table-cell pb-2 mb-1 lg:pb-0 lg:mb-0 lg:px-3 lg:py-2.5 ' +
    'border-b border-slate-100 dark:border-slate-700/60 lg:border-b-0 ' +
    'text-base lg:text-sm font-semibold text-slate-900 dark:text-slate-100',

  // Secim kutusu OLAN tablolarda birincil hucre: mobilde sag ustteki
  // checkbox'in altina girmesin diye sag dolgu. (pr-8'i hucreBaslik'a genel
  // koymak, checkbox'i olmayan tablolarda ~2rem olu bosluk birakiyordu.)
  hucreBaslikSecimli:
    'block lg:table-cell pb-2 mb-1 lg:pb-0 lg:mb-0 pr-8 lg:pr-3 lg:px-3 lg:py-2.5 ' +
    'border-b border-slate-100 dark:border-slate-700/60 lg:border-b-0 ' +
    'text-base lg:text-sm font-semibold text-slate-900 dark:text-slate-100',

  // Aksiyon/buton hücresi: mobilde tam genişlik, üstte ince ayraç.
  hucreAksiyon:
    'flex flex-wrap items-center gap-2 pt-2.5 mt-1.5 lg:table-cell ' +
    'lg:pt-0 lg:mt-0 lg:px-3 lg:py-2.5 lg:whitespace-nowrap ' +
    'border-t border-slate-100 dark:border-slate-700/60 lg:border-t-0',

  // Seçim kutusu: mobilde kartın sağ üstüne sabitlenir (gizlenirse toplu
  // seçim mobilde tamamen kaybolurdu).
  hucreSecim:
    'absolute right-3 top-3 z-10 lg:static lg:table-cell ' +
    'lg:px-3 lg:py-2.5 lg:w-10 lg:text-center',

  // Boş/yükleniyor durumu (colSpan'lı tek hücre).
  hucreDurum:
    'block lg:table-cell py-10 text-center text-sm ' +
    'text-slate-500 dark:text-slate-400',
} as const
