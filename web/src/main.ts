import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import { can, loadUser, user } from './auth'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/services' },
    { path: '/services', component: () => import('./views/ServicesView.vue') },
    { path: '/services/:name', component: () => import('./views/ServiceView.vue'), props: true },
    { path: '/logs', component: () => import('./views/LogsView.vue') },
    { path: '/traces', component: () => import('./views/TracesView.vue') },
    { path: '/traces/:id', component: () => import('./views/TraceView.vue'), props: true },
    { path: '/explore', component: () => import('./views/MetricsView.vue') },
    { path: '/dashboards', component: () => import('./views/DashboardsView.vue') },
    { path: '/dashboards/:id', component: () => import('./views/DashboardView.vue'), props: true },
    { path: '/alerts', component: () => import('./views/AlertsView.vue') },
    { path: '/account', component: () => import('./views/AccountView.vue') },
    { path: '/users', component: () => import('./views/UsersView.vue'), meta: { role: 'admin' } },
    { path: '/login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
  ],
})

// Every page except /login needs a session; admin pages need the admin role.
router.beforeEach(async (to) => {
  if (to.meta.public) return true
  if (!user.value && !(await loadUser())) return { path: '/login', query: { next: to.fullPath } }
  if (to.meta.role === 'admin' && !can('admin')) return '/services'
  return true
})

createApp(App).use(router).mount('#app')
