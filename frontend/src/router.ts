import { createRouter, createWebHistory } from 'vue-router'
import { ref } from 'vue'
import { getAccessToken } from './modules/auth/api/auth'
import { getCurrentAdminAccount } from './modules/admin/api/adminAccounts'
import {
  resetWorkspaceCheck,
  markWorkspaceActive,
  isWorkspaceChecked,
  isWorkspaceActive,
  setWorkspaceChecked,
} from './lib/workspaceGuard'

// Security entry path check
let cachedSecurityPath: string = ''
let isCacheFetched = false
const SECURITY_ENTRY_REQUEST_TIMEOUT_MS = 5_000

async function fetchSecurityEntryPath(): Promise<string> {
  if (isCacheFetched) {
    return cachedSecurityPath
  }
  const controller = new AbortController()
  let timeoutId: ReturnType<typeof setTimeout> | undefined
  const timeoutPromise = new Promise<never>((_, reject) => {
    timeoutId = setTimeout(() => {
      controller.abort()
      reject(new Error('security entry request timed out'))
    }, SECURITY_ENTRY_REQUEST_TIMEOUT_MS)
  })
  try {
    const response = await Promise.race([
      fetch('/api/settings/security/public', { signal: controller.signal }),
      timeoutPromise,
    ])
    if (response.ok) {
      const data = await Promise.race([response.json(), timeoutPromise])
      cachedSecurityPath = data.securityEntryPath || ''
      isCacheFetched = true
      return cachedSecurityPath
    }
  } catch {
    // 如果获取失败，默认不启用安全入口限制
  } finally {
    if (timeoutId !== undefined) clearTimeout(timeoutId)
  }
  cachedSecurityPath = ''
  isCacheFetched = true
  return ''
}

const routes = [
  {
    path: '/',
    redirect: '/login'
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('./modules/auth/LoginPage.vue')
  },
  {
    path: '/register',
    redirect: '/login'
  },
  {
    path: '/404',
    name: 'NotFound',
    component: () => import('./modules/error/NotFoundPage.vue')
  },
  {
    path: '/:securityPath',
    name: 'SecurityEntry',
    beforeEnter: async (to: any) => {
      const securityPath = await fetchSecurityEntryPath()
      if (securityPath && to.params.securityPath === securityPath) {
        sessionStorage.setItem('security_entry_verified', 'true')
        return { path: '/login', replace: true }
      }
      return { path: '/404', replace: true }
    },
    component: () => import('./modules/error/NotFoundPage.vue')
  },
  {
    path: '/embed/tickets',
    name: 'EmbedTickets',
    component: () => import('./modules/embed/tickets/TicketEmbedPage.vue')
  },
  {
    path: '/embed/leaderboard',
    name: 'EmbedLeaderboard',
    component: () => import('./modules/embed/leaderboard/LeaderboardEmbedPage.vue')
  },
  {
    path: '/embed/lottery',
    name: 'EmbedLottery',
    component: () => import('./modules/embed/lottery/LotteryEmbedPage.vue')
  },
  {
    path: '/admin',
    component: () => import('./modules/admin/layout/AdminLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        name: 'AdminDashboard',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/DashboardView.vue')
      },
      {
        path: 'upstream',
        name: 'AdminUpstream',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/UpstreamView.vue')
      },
      {
        path: 'group-rates',
        name: 'AdminGroupRates',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/GroupRatesView.vue')
      },
      {
        path: 'group-associations',
        name: 'AdminGroupAssociations',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/GroupAssociationsView.vue')
      },
      {
        path: 'connection-health',
        name: 'AdminConnectionHealth',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/ConnectionHealthView.vue')
      },
      {
        path: 'group-rate-campaigns',
        name: 'AdminGroupRateCampaigns',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/GroupRateCampaignsView.vue')
      },
      {
        path: 'settings',
        name: 'AdminSettings',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/SettingsView.vue')
      },
      {
        path: 'tickets',
        name: 'AdminTickets',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/TicketsView.vue')
      },
      {
        path: 'leaderboard',
        name: 'AdminLeaderboard',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/leaderboard/LeaderboardAdminPage.vue')
      },
      {
        path: 'lottery',
        name: 'AdminLottery',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/lottery/LotteryAdminPage.vue')
      },
      {
        path: 'mass-email',
        name: 'AdminMassEmail',
        meta: { requiresWorkspace: true },
        component: () => import('./modules/admin/views/MassEmailView.vue')
      },
      {
        path: 'accounts',
        name: 'AdminAccounts',
        component: () => import('./modules/admin/views/AdminAccountsView.vue')
      }
    ]
  }
]

export const router = createRouter({
  history: createWebHistory(),
  routes
})

// Lazy-loaded admin views and route guards can take a moment on a cold
// connection. Expose a small reactive state so the admin shell can show
// immediate feedback while navigation is pending.
export const isNavigationLoading = ref(false)

router.beforeEach(async (to: any) => {
  isNavigationLoading.value = true
  // 安全入口检查：如果配置了安全入口路径，必须通过该路径访问登录页
  if (to.path === '/login') {
    const securityPath = await fetchSecurityEntryPath()
    if (securityPath) {
      const hasSecurityEntry = sessionStorage.getItem('security_entry_verified') === 'true'
      if (!hasSecurityEntry) {
        // Hide the login entry when the security path is not verified
        return { path: '/404', replace: true }
      }
    }
  }

  if (to.matched.some((route: any) => route.meta.requiresAuth) && !getAccessToken()) {
    return { path: '/login' }
  }

  if (to.matched.some((route: any) => route.meta.requiresWorkspace)) {
    if (!isWorkspaceChecked()) {
      try {
        await getCurrentAdminAccount()
        setWorkspaceChecked(true, true)
      } catch {
        setWorkspaceChecked(true, false)
      }
    }
    if (!isWorkspaceActive()) {
      return { name: 'AdminAccounts' }
    }
  }

  return true
})

router.afterEach(() => {
  isNavigationLoading.value = false
})

router.onError(() => {
  isNavigationLoading.value = false
})

export { resetWorkspaceCheck, markWorkspaceActive }


