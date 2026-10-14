import { ORTAK_EN } from '@/lib/cevirOrtak'
import i18n from '@/lib/i18n'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'

// KaynakHavuzAlanlari — bayi (ve bayi paketi) formlarında CPU / RAM / G-Ç havuzu alanları.
// Disk havuzu AYRI alan değildir: mevcut "Disk (MB)" alanı (users.max_disk_mb) bayinin disk havuzudur.
// 0 = sınırsız. RAM MB ya da GB olarak girilir; durumda her zaman MB tutulur (sunucu ram_mb bekler).
// Üç alan <div> olarak döner: çağıran sayfa kendi ızgarasına (grid) yerleştirir.

export type KaynakDeger = { cpu_cekirdek: number | string; ram_mb: number | string; io_agirlik: number | string }

const KH_EN: Record<string, string> = {
  "CPU (çekirdek)": "CPU (cores)",
  "RAM (fiziksel)": "RAM (physical)",
  "G/Ç ağırlığı": "I/O weight",
  "sınırsız": "unlimited",
  "çekirdek": "cores",
  "varsayılan (100)": "default (100)",
  "ondalık olabilir (1,5 = bir buçuk çekirdek)": "decimals allowed (1.5 = one and a half cores)",
  "RAM en az 64 MB olmalı": "RAM must be at least 64 MB",
  "CPU 0,1 ile 1024 arasında olmalı": "CPU must be between 0.1 and 1024",
  "G/Ç ağırlığı 1-10000 arasında olmalı": "I/O weight must be between 1 and 10000",
  "Bu değer seçili paketten gelir": "This value comes from the selected package",
  "RAM birimi": "RAM unit",
}
const cevir = (tr: string): string => (i18n.language === "en" ? (KH_EN[tr] || ORTAK_EN[tr] || tr) : tr)

const EN_AZ_RAM_MB = 64

type Props = {
  deger: KaynakDeger
  onDegis: (k: Partial<KaynakDeger>) => void
  kilitli?: boolean // paket seçiliyken değerler paketten gelir
  inp: string // sayfanın kendi girdi sınıfı
  lbl: string // sayfanın kendi etiket sınıfı
}

export default function KaynakHavuzAlanlari({ deger, onDegis, kilitli, inp, lbl }: Props) {
  useTranslation() // dil re-render aboneligi
  const cpu = Number(deger.cpu_cekirdek) || 0
  const ram = Number(deger.ram_mb) || 0
  const io = Number(deger.io_agirlik) || 0
  const [birim, setBirim] = useState<'MB' | 'GB'>(ram > 0 && ram % 1024 === 0 ? 'GB' : 'MB')
  const kat = birim === 'GB' ? 1024 : 1
  const sinif = `${inp} ${kilitli ? 'opacity-60 cursor-not-allowed' : ''}`
  const baslik = kilitli ? cevir('Bu değer seçili paketten gelir') : undefined
  const hata = 'text-[11px] mt-1 text-red-600 dark:text-red-400'
  const ipucu = 'text-[11px] mt-1 text-slate-400 dark:text-slate-500'

  return (
    <>
      <div>
        <label className={lbl}>{cevir("CPU (çekirdek)")}</label>
        <input className={sinif} type="number" min={0} step="any" value={deger.cpu_cekirdek} readOnly={kilitli} title={baslik}
               onChange={e => onDegis({ cpu_cekirdek: e.target.value })} />
        {cpu > 0 && (cpu < 0.1 || cpu > 1024)
          ? <p className={hata}>{cevir("CPU 0,1 ile 1024 arasında olmalı")}</p>
          : <p className={ipucu}>{cpu > 0 ? `${cpu} ${cevir('çekirdek')} = %${Math.round(cpu * 100)}` : cevir("sınırsız")}</p>}
      </div>
      <div>
        <label className={lbl}>{cevir("RAM (fiziksel)")}</label>
        <div className="flex gap-1">
          <input className={`${sinif} min-w-0 flex-1`} type="number" min={0} step="any" value={ram === 0 ? 0 : ram / kat} readOnly={kilitli} title={baslik}
                 onChange={e => onDegis({ ram_mb: Math.max(0, Math.round((Number(e.target.value) || 0) * kat)) })} />
          <select className={`${inp} !w-auto shrink-0`} value={birim} aria-label={cevir("RAM birimi")}
                  onChange={e => setBirim(e.target.value === 'GB' ? 'GB' : 'MB')}>
            <option value="MB">MB</option>
            <option value="GB">GB</option>
          </select>
        </div>
        {ram > 0 && ram < EN_AZ_RAM_MB
          ? <p className={hata}>{cevir("RAM en az 64 MB olmalı")}</p>
          : <p className={ipucu}>{ram > 0 ? `${ram} MB = ${(ram / 1024).toFixed(2)} GB` : cevir("sınırsız")}</p>}
      </div>
      <div>
        <label className={lbl}>{cevir("G/Ç ağırlığı")}</label>
        <input className={sinif} type="number" min={0} max={10000} step={1} value={deger.io_agirlik} readOnly={kilitli} title={baslik}
               onChange={e => onDegis({ io_agirlik: e.target.value })} />
        {io < 0 || io > 10000
          ? <p className={hata}>{cevir("G/Ç ağırlığı 1-10000 arasında olmalı")}</p>
          : <p className={ipucu}>{io > 0 ? `${io} / 10000` : cevir("varsayılan (100)")}</p>}
      </div>
    </>
  )
}
