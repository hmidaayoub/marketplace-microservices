import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Provider } from 'react-redux'
import { BrowserRouter } from 'react-router-dom'

import App from './App'
import { setUnauthorizedHandler } from './api/client'
import { installRefresh } from './auth/refresh'
import { store } from './store'
import './index.css'

// Lets any 401 refresh once and retry, without the client module knowing about the store.
setUnauthorizedHandler(installRefresh(store))

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Provider store={store}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </Provider>
  </StrictMode>,
)
