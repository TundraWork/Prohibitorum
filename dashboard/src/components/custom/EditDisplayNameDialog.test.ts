import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import EditDisplayNameDialog from './EditDisplayNameDialog.vue'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const put = vi.mocked(api.put)

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function seedUser(displayName = 'Alex Smith') {
  const auth = useAuthStore()
  auth.me = { id: 1, username: 'alex', displayName, role: 'user' }
  return auth
}

function mountOpen() {
  return mount(EditDisplayNameDialog, {
    props: { open: true }, attachTo: document.body, global: { plugins: [i18n()] },
  })
}

function input() {
  return document.body.querySelector('[data-test="edit-displayname-input"]') as HTMLInputElement
}
function setInput(v: string) {
  const el = input(); el.value = v; el.dispatchEvent(new Event('input'))
}
function saveBtn() {
  return document.body.querySelector('[data-test="edit-save"]') as HTMLButtonElement
}

beforeEach(() => { setActivePinia(createPinia()); put.mockReset(); document.body.innerHTML = '' })

describe('EditDisplayNameDialog', () => {
  it('prefills the input with the current displayName', async () => {
    seedUser('Alex Smith'); mountOpen(); await flushPromises()
    expect(input().value).toBe('Alex Smith')
  })

  it('disables Save when unchanged, empty, or too long; enables on a valid change', async () => {
    seedUser('Alex Smith'); mountOpen(); await flushPromises()
    expect(saveBtn().disabled).toBe(true)            // unchanged
    setInput(''); await flushPromises()
    expect(saveBtn().disabled).toBe(true)            // empty
    setInput('x'.repeat(129)); await flushPromises()
    expect(saveBtn().disabled).toBe(true)            // >128
    setInput('Alexander'); await flushPromises()
    expect(saveBtn().disabled).toBe(false)           // valid change
  })

  it('saves: PUT /me, patches store from RESPONSE, emits close', async () => {
    const auth = seedUser('Alex Smith')
    put.mockResolvedValue({ id: 1, username: 'alex', displayName: 'ALEXANDER', role: 'user' })
    const w = mountOpen(); await flushPromises()
    setInput('Alexander'); await flushPromises()
    saveBtn().click(); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/me', { displayName: 'Alexander' })
    expect(auth.me?.displayName).toBe('ALEXANDER')
    expect(w.emitted('update:open')?.some((e) => e[0] === false)).toBe(true)
  })

  it('keeps the dialog open and shows the mapped error on invalid_display_name', async () => {
    seedUser('Alex Smith')
    put.mockRejectedValue({ code: 'invalid_display_name', message: 'zh' })
    const w = mountOpen(); await flushPromises()
    setInput('Bad'); await flushPromises()
    saveBtn().click(); await flushPromises()
    expect(document.body.textContent).toContain(en.errors.invalid_display_name)
    expect(input().value).toBe('Bad')
    expect((w.emitted('update:open') ?? []).some((e) => e[0] === false)).toBe(false)
  })
})
