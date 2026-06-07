import { createRouter, createWebHistory } from 'vue-router'
import { defineComponent, h } from 'vue'

// Placeholder component — Task 3 replaces this with real page components.
const Placeholder = defineComponent({
  name: 'Placeholder',
  render() {
    return h('div', { style: 'padding: 2rem; font-family: sans-serif;' }, 'Prohibitorum — coming soon')
  },
})

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', component: Placeholder },
    // Catch-all: render placeholder for any unmatched path
    { path: '/:pathMatch(.*)*', component: Placeholder },
  ],
})

export default router
