import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Provider } from 'react-redux'
import { BrowserRouter } from 'react-router-dom'

import App from '@/App'
import { ThemeProvider } from '@/components/theme-provider'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { setUnauthorizedHandler } from '@/api/client'
import { installRefresh } from '@/auth/refresh'
import { store } from '@/store'
import '@/index.css'

// Lets any 401 refresh once and retry, without the client module knowing about the store.
setUnauthorizedHandler(installRefresh(store))

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Provider store={store}>
      <ThemeProvider>
        <TooltipProvider>
          <BrowserRouter>
            <App />
            <Toaster position="bottom-right" richColors closeButton />
          </BrowserRouter>
        </TooltipProvider>
      </ThemeProvider>
    </Provider>
  </StrictMode>,
)
