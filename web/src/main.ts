import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
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
  ],
})

createApp(App).use(router).mount('#app')
