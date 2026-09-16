import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import './styles.css'
import { bootTheme } from '@/lib/theme'
import { DialogSaglayici } from '@/components/Dialog'
import { ToastSaglayici } from '@/components/Toast'
import { HataSiniri } from '@/components/HataSiniri'
import { suresiGecenleriTemizle } from '@/lib/kalici'
import { markaYukle } from '@/lib/marka'
import '@/lib/i18n' // coklu dil altyapisi (musteri paneli + landing)

bootTheme()
suresiGecenleriTemizle() // süresi dolmuş form taslaklarını süpür

// 🔴 render'dan ONCE beklenir: marka sonradan gelseydi giris sayfasi once
// "GirginOSPanel" yazip sonra musterinin markasina atlardi (gorunur sicrama).
// markaYukle KENDI ICINDE 2 sn zaman asimi + varsayilana dusme uygular, bu
// yuzden uc olu/yavas olsa bile panel burada ASILI KALMAZ.
//
// 🔴 UST DUZEY `await` KULLANILMAZ: derleme hedefine bagli olarak kirilgandir.
// `.finally` her hedefte calisir ve marka HATA verse de render'i garanti eder.
markaYukle().finally(() => {
  ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      {/* Tema uyumlu onay/soru/bilgi kutuları — native confirm/prompt/alert yerine */}
      <HataSiniri>
        <DialogSaglayici>
          <ToastSaglayici>
          <App />
          </ToastSaglayici>
        </DialogSaglayici>
      </HataSiniri>
    </BrowserRouter>
    </React.StrictMode>,
  )
})
