import { createRouter, createWebHistory } from 'vue-router'
import { getAccessToken } from './modules/auth/api/auth'
import { getCurrentAdminAccount } from './modules/admin/api/adminAccounts'
import {
  resetWorkspaceCheck,
  markWorkspaceActive,
  isWorkspaceChecked,
  isWorkspaceActive,
  setWorkspaceChecked,
} from './lib/workspaceGuard'

// 安全入口路径检查
let cachedSecurityPath: string | null = null

async function fetchSecurityEntryPath(): Promise<string> {
  if (cachedSecurityPath !== null) {
    return cachedSecurityPath
  }
  try {
    const response = await fetch('/api/settings/security/public')
    if (response.ok) {
      const data = await response.json()
      cachedSecurityPath = data.securityEntryPath || ''
      return cachedSecurityPath
    }
  } catch {
    // 如果获取失败，默认不启用安全入口限制
  }
  cachedSecurityPath = ''
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
    beforeEnter: async (to) => {
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

router.beforeEach(async (to) => {
  // 安全入口检查：如果配置了安全入口路径，必须通过该路径访问登录页
  if (to.path === '/login') {
    const securityPath = await fetchSecurityEntryPath()
    if (securityPath) {
      const hasSecurityEntry = sessionStorage.getItem('security_entry_verified') === 'true'
      if (!hasSecurityEntry) {
        // 返回404页面或空白页，避免暴露登录入口
        return { path: '/404', replace: true }
      }
    }
  }

  if (to.matched.some((route) => route.meta.requiresAuth) && !getAccessToken()) {
    return { path: '/login' }
  }

  if (to.matched.some((route) => route.meta.requiresWorkspace)) {
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

export { resetWorkspaceCheck, markWorkspaceActive }
