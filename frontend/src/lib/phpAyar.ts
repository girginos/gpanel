// PHP ayar gövdesi (PUT /domains/{id}/php-settings ve alt alan eşi) — kök sınırı.
//
// Sunucu kuralı (internal/php/kok_sinir.go): yönetici dışı oturum (müşteri, bayi,
// müşteri adına) kökün İÇ ALANLARINI (efektiflimit.PHPAyarIcAnahtarlari: işçi
// sayısı ve türevleri — paketten gelir) göremez ve gövdede gönderirse 403 alır;
// İZOLASYON alanlarını (kök korumalı) değiştiremez. Bu yüzden ön yüz yönetici
// dışında bu anahtarları GÖVDEYE KOYMAZ (sunucu değerleri kayıttan korur).
export const PHP_IC_ANAHTARLAR = ['pm_max_children', 'pm_start_servers', 'pm_min_spare_servers', 'pm_max_spare_servers'] as const
export const PHP_KOK_KORUMALI = ['disable_functions', 'open_basedir', 'session_save_path', 'mail_force_extra_parameters'] as const

export function phpAyarGovdesi<T extends object>(a: T, yonetici: boolean): Partial<T> {
  if (yonetici) return a
  const g = { ...a } as Record<string, unknown>
  for (const k of [...PHP_IC_ANAHTARLAR, ...PHP_KOK_KORUMALI]) delete g[k]
  return g as Partial<T>
}
