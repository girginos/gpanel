import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import './styles.css'
import { bootTheme } from '@/lib/theme'
import { DialogSaglayici } from '@/components/Dialog'
import { HataSiniri } from '@/components/HataSiniri'
import { suresiGecenleriTemizle } from '@/lib/kalici'

bootTheme()
suresiGecenleriTemizle() // süresi dolmuş form taslaklarını süpür

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      {/* Tema uyumlu onay/soru/bilgi kutuları — native confirm/prompt/alert yerine */}
      <HataSiniri>
        <DialogSaglayici>
          <App />
        </DialogSaglayici>
      </HataSiniri>
    </BrowserRouter>
  </React.StrictMode>,
)
