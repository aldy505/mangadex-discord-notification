import { createApp } from 'vue'
import './style.css'
import App from './App.vue'
import router from './router'
import { setUnauthorizedHandler } from './lib/api'
import { clearSession } from './lib/auth'

setUnauthorizedHandler(() => {
  clearSession()
  const current = router.currentRoute.value
  if (current.name !== 'login') {
    void router.replace({ name: 'login', query: { redirect: current.fullPath } })
  }
})

createApp(App).use(router).mount('#app')
