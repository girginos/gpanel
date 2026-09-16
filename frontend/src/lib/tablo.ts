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
  // 🔴 `tabular-nums`: disk boyutu, tarih ve sayı sütunları ORANTILI rakamla
  // dizilince basamaklar satırdan satıra kayıyor ve göz kolonu takip
  // edemiyordu. Sabit genişlikli rakam, sayıları alt alta hizalar — tabloda
  // okunabilirliği en çok artıran tek değişiklik budur.
  tablo: 'w-full lg:min-w-[640px] [font-variant-numeric:tabular-nums]',

  // Başlık satırı yalnız masaüstünde görünür; mobilde etiketi ::before taşıyor.
  baslikGrubu: 'hidden lg:table-header-group',
  // Başlıklar biraz daha küçük ve daha AÇIK renkte: sütun adları veriyle
  // yarışmamalı, veriyi çerçevelemeli. Harf aralığı biraz açık (küçük punto
  // büyük harfte sıkışır), `whitespace-nowrap` ile başlık iki satıra bölünmez.
  baslik:
    'px-3 py-3 text-left text-[10.5px] font-semibold uppercase tracking-[0.09em] ' +
    'whitespace-nowrap text-slate-400 dark:text-slate-500',

  govde: 'block lg:table-row-group',

  // Mobilde kart; masaüstünde sade satır.
  satir:
    'relative lg:static block lg:table-row mb-3 lg:mb-0 rounded-xl lg:rounded-none ' +
    'border border-slate-200 dark:border-dark-600 lg:border-0 ' +
    'lg:border-b lg:border-slate-100 dark:lg:border-dark-600 ' +
    'bg-white dark:bg-dark-700 lg:bg-transparent dark:lg:bg-transparent ' +
    'p-3 lg:p-0 shadow-xs lg:shadow-none ' +
    // Masaüstünde imlecin bulunduğu satır belirginleşir: uzun listelerde göz
    // satırı kaybediyordu. Mobilde kart zaten ayrı olduğu için uygulanmaz.
    'lg:hover:bg-slate-50/70 dark:lg:hover:bg-dark-700/40 transition-colors',

  // Normal hücre: mobilde "etiket ......... değer" satırı.
  hucre:
    'flex items-start justify-between gap-3 py-1.5 lg:table-cell ' +
    'lg:px-3 lg:py-3 text-[13.5px] text-slate-600 dark:text-slate-300 ' +
    'before:shrink-0 before:text-[11px] ' +
    'before:font-semibold before:uppercase before:tracking-wider ' +
    'before:text-slate-400 dark:before:text-slate-500 before:pt-0.5 lg:before:hidden',

  // Birincil tanımlayıcı (domain adı, dosya adı…): mobilde kart başlığı.
  hucreBaslik:
    'block lg:table-cell pb-2 mb-1 lg:pb-0 lg:mb-0 lg:px-3 lg:py-2.5 ' +
    'border-b border-slate-100 dark:border-dark-600/60 lg:border-b-0 ' +
    'text-base lg:text-[14px] font-semibold tracking-[-0.01em] ' +
    'text-slate-900 dark:text-slate-100',

  // Secim kutusu OLAN tablolarda birincil hucre: mobilde sag ustteki
  // checkbox'in altina girmesin diye sag dolgu. (pr-8'i hucreBaslik'a genel
  // koymak, checkbox'i olmayan tablolarda ~2rem olu bosluk birakiyordu.)
  hucreBaslikSecimli:
    'block lg:table-cell pb-2 mb-1 lg:pb-0 lg:mb-0 pr-8 lg:pr-3 lg:px-3 lg:py-2.5 ' +
    'border-b border-slate-100 dark:border-dark-600/60 lg:border-b-0 ' +
    'text-base lg:text-[14px] font-semibold tracking-[-0.01em] ' +
    'text-slate-900 dark:text-slate-100',

  // Aksiyon/buton hücresi: mobilde tam genişlik, üstte ince ayraç.
  hucreAksiyon:
    'flex flex-wrap items-center gap-2 pt-2.5 mt-1.5 lg:table-cell ' +
    'lg:pt-0 lg:mt-0 lg:px-3 lg:py-3 lg:whitespace-nowrap ' +
    'border-t border-slate-100 dark:border-dark-600/60 lg:border-t-0',

  // Seçim kutusu: mobilde kartın sağ üstüne sabitlenir (gizlenirse toplu
  // seçim mobilde tamamen kaybolurdu).
  hucreSecim:
    'absolute right-3 top-3 z-10 lg:static lg:table-cell ' +
    'lg:px-3 lg:py-2.5 lg:w-10 lg:text-center',

  // İşlem düğmeleri.
  //
  // 🔴 DÜZ BAĞLANTI DEĞİL, DÜĞME. "İşlemler" sütunundaki eylemler düz metin
  // bağlantısıydı; tablodaki diğer metinlerden ayırt edilemiyor, tıklanabilir
  // oldukları ancak imleç üzerine gelince anlaşılıyordu. Kenarlıklı bir yüzey,
  // "buraya basılır"ı bakışta söyler ve dokunmatik hedefi de büyütür.
  //
  // İkincil (çerçeveli) ve birincil (dolgulu) iki biçim: bir satırda birden
  // çok eylem varken hepsi aynı ağırlıkta olursa hangisinin ASIL eylem olduğu
  // kaybolur — "Yönet" birincil, "+ Subdomain" ikincildir.
  aksiyon:
    'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-medium ' +
    'border border-slate-200 dark:border-dark-600 ' +
    'text-slate-600 dark:text-slate-300 ' +
    'hover:bg-slate-50 hover:text-slate-900 hover:border-slate-300 ' +
    'dark:hover:bg-dark-600/60 dark:hover:text-slate-100 dark:hover:border-slate-600 ' +
    'focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/50 ' +
    'transition-colors',

  aksiyonBirincil:
    'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-xs font-semibold ' +
    'border border-brand-200 dark:border-brand-500/30 ' +
    'text-brand-700 dark:text-brand-300 bg-brand-50/70 dark:bg-brand-500/10 ' +
    'hover:bg-brand-100 hover:border-brand-300 ' +
    'dark:hover:bg-brand-500/20 dark:hover:border-brand-500/50 ' +
    'focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/50 ' +
    'transition-colors',

  // Boş/yükleniyor durumu (colSpan'lı tek hücre).
  hucreDurum:
    'block lg:table-cell py-10 text-center text-sm ' +
    'text-slate-500 dark:text-slate-400',
} as const
