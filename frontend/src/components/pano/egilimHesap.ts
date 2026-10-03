// Eğilim hesapları (saf): seri seçimi, günlük trafik farkı, değişim metni.
import { pt } from './ceviri'

export type GunNokta = { tarih: string; hesap: number; askida: number; yeni: number; disk_kb: number; trafik_kb: number }
export type EgilimVeri = { noktalar: GunNokta[]; disk_olcum: string }
export type EgilimMetrik = 'hesap' | 'disk_kb' | 'askida' | 'trafik'

// trafik_kb ay içi KÜMÜLATİF (domains.trafik_kb): günlük trafik = ardışık fark;
// ay başında sayaç sıfırlanınca (fark < 0) o günün kendi değeri alınır.
export function gunlukTrafik(n: GunNokta[]): number[] {
  return n.map((x, i) => (i === 0 ? NaN : x.trafik_kb - n[i - 1].trafik_kb >= 0 ? x.trafik_kb - n[i - 1].trafik_kb : x.trafik_kb)).slice(1)
}

export function seri(n: GunNokta[], m: EgilimMetrik): number[] {
  if (m === 'trafik') return gunlukTrafik(n)
  return n.map((x) => x[m])
}

// degisimMetni — ilk ve son nokta arası değişim (yüzde; taban 0 ise mutlak).
export function degisimMetni(d: number[], gun: number): string | null {
  if (d.length < 2) return null
  const ilk = d[0], son = d[d.length - 1]
  if (ilk === son) return `${pt('değişim yok')} · ${pt('son {0} günde', gun)}`
  const fark = ilk === 0 ? `${son - ilk > 0 ? '+' : ''}${son - ilk}` : `${son >= ilk ? '+' : ''}${(((son - ilk) / ilk) * 100).toFixed(Math.abs((son - ilk) / ilk) < 0.1 ? 1 : 0)}%`
  return `${fark} · ${pt('son {0} günde', gun)}`
}
