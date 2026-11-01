import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { clearSession, refreshSession, session } from './lib/auth'

declare module 'vue-router' {
  interface RouteMeta {
    /** Reachable without a full session (login, invite acceptance). */
    publicRoute?: boolean
  }
}

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('./views/LoginView.vue'),
    meta: { publicRoute: true },
  },
  {
    path: '/invite/:token',
    name: 'invite',
    component: () => import('./views/InviteView.vue'),
    meta: { publicRoute: true },
  },
  {
    path: '/enroll',
    name: 'enroll',
    component: () => import('./views/EnrollView.vue'),
  },
  {
    path: '/',
    name: 'subscriptions',
    component: () => import('./views/SubscriptionsView.vue'),
  },
  {
    path: '/admins',
    name: 'admins',
    component: () => import('./views/AdminsView.vue'),
  },
  {
    path: '/audit',
    name: 'audit',
    component: () => import('./views/AuditView.vue'),
  },
  {
    path: '/account',
    name: 'account',
    component: () => import('./views/AccountView.vue'),
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/',
  },
]

const router = createRouter({
  history: createWebHistory('/'),
  routes,
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach(async (to) => {
  if (!session.loaded) {
    try {
      await refreshSession()
    } catch {
      clearSession()
    }
  }

  const { stage } = session

  if (stage === 'full') {
    // A signed-in admin has no business on login, invite, or enrollment screens.
    if (to.meta.publicRoute === true || to.name === 'enroll') return { name: 'subscriptions' }
    return true
  }

  if (stage === 'enroll') {
    return to.name === 'enroll' ? true : { name: 'enroll' }
  }

  if (stage === 'password') {
    // Password accepted, second factor outstanding: only the login screen can finish it.
    return to.name === 'login' ? true : { name: 'login' }
  }

  if (to.meta.publicRoute === true) return true

  return {
    name: 'login',
    query: to.fullPath === '/' ? {} : { redirect: to.fullPath },
  }
})

export default router
