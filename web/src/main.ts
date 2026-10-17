import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './theme'
import { can, loadUser, user } from './auth'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/services' },
    { path: '/services', component: () => import('./views/ServicesView.vue'), meta: { title: 'Services' } },
    { path: '/services/:name', component: () => import('./views/ServiceView.vue'), props: true, meta: { title: 'Service' } },
    { path: '/logs', component: () => import('./views/LogsView.vue'), meta: { title: 'Logs' } },
    { path: '/traces', component: () => import('./views/TracesView.vue'), meta: { title: 'Traces' } },
    { path: '/traces/:id', component: () => import('./views/TraceView.vue'), props: true, meta: { title: 'Trace' } },
    { path: '/explore', component: () => import('./views/MetricsView.vue'), meta: { title: 'Explore' } },
    { path: '/dashboards', component: () => import('./views/DashboardsView.vue'), meta: { title: 'Dashboards' } },
    { path: '/dashboards/:id', component: () => import('./views/DashboardView.vue'), props: true, meta: { title: 'Dashboard' } },
    { path: '/alerts', component: () => import('./views/AlertsView.vue'), meta: { title: 'Alerts' } },
    { path: '/account', component: () => import('./views/AccountView.vue'), meta: { title: 'Account' } },
    { path: '/users', component: () => import('./views/UsersView.vue'), meta: { title: 'Users', role: 'admin' } },
    { path: '/keys', component: () => import('./views/KeysView.vue'), meta: { title: 'API keys', role: 'admin' } },
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
