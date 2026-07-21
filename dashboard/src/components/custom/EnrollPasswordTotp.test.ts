import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import en from '@/locales/en'
import EnrollPasswordTotp from './EnrollPasswordTotp.vue'

vi.mock('@/lib/api', () => ({ api: { post: vi.fn() } }))
import { api } from '@/lib/api'

const { hardRedirect } = vi.hoisted(() => ({ hardRedirect: vi.fn() }))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

const post = vi.mocked(api.post)
const TOKEN = 'tok_pwdtotp'
const base = `/api/prohibitorum/enrollments/${TOKEN}/password-totp`

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

// RecoveryCodesDisplay has a "saved" gate that's fiddly to drive; stub it to a
// button that emits confirmed, so we can assert the redirect happens on confirm.
const RecoveryCodesStub = {
  props: ['codes', 'regenerated'],
  emits: ['confirmed'],
  template: '<button data-test="rcd-done" @click="$emit(\'confirmed\')">done {{ codes.length }}</button>',
}

function mountCeremony(identity: { username: string; displayName: string } | null): VueWrapper {
  return mount(EnrollPasswordTotp, {
    props: { token: TOKEN, identity },
    global: {
      plugins: [makeI18n()],
      stubs: { RecoveryCodesDisplay: RecoveryCodesStub, TotpQr: true },
    },
  })
}

beforeEach(() => {
  setActivePinia(createPinia())
  post.mockReset()
  hardRedirect.mockReset()
})

describe('EnrollPasswordTotp', () => {
  it('validates the password client-side before calling begin', async () => {
    const wrapper = mountCeremony({ username: 'alex', displayName: 'Alex' })

    // Too short.
    await wrapper.get('#enroll-password').setValue('short')
    await wrapper.get('#enroll-password-confirm').setValue('short')
    await wrapper.get('[data-test="pwtotp-continue"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(en.enroll.pwTooShort)
    expect(post).not.toHaveBeenCalled()

    // Mismatch.
    await wrapper.get('#enroll-password').setValue('longenough1')
    await wrapper.get('#enroll-password-confirm').setValue('longenough2')
    await wrapper.get('[data-test="pwtotp-continue"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe(en.enroll.pwMismatch)
    expect(post).not.toHaveBeenCalled()
  })

  it('begin→verify sets password+TOTP and redirects to the app root on confirm', async () => {
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/begin')) return { secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x?secret=JBSWY3DP' }
      if (path.endsWith('/verify')) return { session: { id: 1 }, recoveryCodes: Array(10).fill('c') }
      throw new Error(`unexpected ${path}`)
    })
    const wrapper = mountCeremony({ username: 'alex', displayName: 'Alex Smith' })

    await wrapper.get('#enroll-password').setValue('supersecret')
    await wrapper.get('#enroll-password-confirm').setValue('supersecret')
    await wrapper.get('[data-test="pwtotp-continue"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(`${base}/begin`, {
      password: 'supersecret',
      username: 'alex',
      displayName: 'Alex Smith',
    })

    // Advanced to the TOTP phase (secret shown).
    expect(wrapper.text()).toContain('JBSWY3DP')

    await wrapper.get('#enroll-totp-code').setValue('123456')
    await wrapper.get('[data-test="pwtotp-verify"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(`${base}/verify`, { code: '123456' })

    // Recovery codes shown; confirming redirects to the app root.
    await wrapper.get('[data-test="rcd-done"]').trigger('click')
    expect(hardRedirect).toHaveBeenCalledWith('/')
  })

  it('omits username/displayName for a reset (identity=null)', async () => {
    post.mockResolvedValue({ secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x' })
    const wrapper = mountCeremony(null)

    await wrapper.get('#enroll-password').setValue('supersecret')
    await wrapper.get('#enroll-password-confirm').setValue('supersecret')
    await wrapper.get('[data-test="pwtotp-continue"]').trigger('click')
    await flushPromises()

    expect(post).toHaveBeenCalledWith(`${base}/begin`, { password: 'supersecret' })
  })

  it('emits federationRequired when begin is rejected as federation-bound', async () => {
    post.mockRejectedValue({ code: 'enrollment_federation_required' })
    const wrapper = mountCeremony({ username: 'alex', displayName: 'Alex' })

    await wrapper.get('#enroll-password').setValue('supersecret')
    await wrapper.get('#enroll-password-confirm').setValue('supersecret')
    await wrapper.get('[data-test="pwtotp-continue"]').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('federationRequired')).toBeTruthy()
  })

  it('emits back from the password phase', async () => {
    const wrapper = mountCeremony({ username: 'alex', displayName: 'Alex' })
    await wrapper.get('[data-test="pwtotp-back"]').trigger('click')
    expect(wrapper.emitted('back')).toBeTruthy()
  })
})
