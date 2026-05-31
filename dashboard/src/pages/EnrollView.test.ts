import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory } from 'vue-router'
import zh from '../locales/zh'
import en from '../locales/en'
import EnrollView from './EnrollView.vue'

const get = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a) } }))

const passkeyRegister = vi.fn()
vi.mock('../lib/webauthn', () => ({ passkeyRegister: (...a: unknown[]) => passkeyRegister(...a) }))

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
function makeRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [
    { path: '/enroll/:token', component: EnrollView },
    { path: '/error', component: { template: '<div/>' } },
  ] })
}

beforeEach(() => { get.mockReset(); passkeyRegister.mockReset() })

async function mountAt(token: string) {
  const router = makeRouter()
  router.push(`/enroll/${token}`)
  await router.isReady()
  return mount(EnrollView, { global: { plugins: [makeI18n(), router] } })
}

describe('EnrollView', () => {
  it('bootstrap shows username + displayName inputs and registers', async () => {
    get.mockResolvedValueOnce({ intent: 'bootstrap', expiresAt: '2026-02-01T00:00:00Z' })
    passkeyRegister.mockResolvedValueOnce({ id: 1, username: 'admin', displayName: 'Admin', role: 'admin' })
    const assign = vi.fn()
    Object.defineProperty(window, 'location', { value: { assign }, writable: true })

    const wrapper = await mountAt('tok')
    await flushPromises()
    const inputs = wrapper.findAll('input')
    expect(inputs.length).toBeGreaterThanOrEqual(2) // username + displayName (+ optional nickname)
    await inputs[0].setValue('admin')
    await inputs[1].setValue('Admin')
    await wrapper.find('[data-test="register"]').trigger('click')
    await flushPromises()
    expect(passkeyRegister).toHaveBeenCalledWith('tok', expect.objectContaining({ username: 'admin', displayName: 'Admin' }))
    expect(assign).toHaveBeenCalledWith('/')
  })

  it('reset shows the target name and no identity inputs', async () => {
    get.mockResolvedValueOnce({ intent: 'reset', target: { username: 'bob', displayName: 'Bob' }, expiresAt: '2026-02-01T00:00:00Z' })
    const wrapper = await mountAt('tok')
    await flushPromises()
    expect(wrapper.text()).toContain('Bob')
    const textInputs = wrapper.findAll('input[type="text"]')
    expect(textInputs.length).toBeLessThanOrEqual(1) // only the optional nickname (also text); no username/displayName
  })
})
