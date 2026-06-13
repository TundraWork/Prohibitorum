import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import en from '@/locales/en'
import EditProfileDialog from './EditProfileDialog.vue'
import { useAuthStore } from '@/stores/auth'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn(), upload: vi.fn(), del: vi.fn() } }))
import { api } from '@/lib/api'
const put = vi.mocked(api.put)
const upload = vi.mocked(api.upload)
const del = vi.mocked(api.del)

// jsdom has no URL.createObjectURL / revokeObjectURL
URL.createObjectURL = vi.fn(() => 'blob:x')
URL.revokeObjectURL = vi.fn()

// Stub AvatarCropper so jsdom doesn't attempt canvas rendering.
const AvatarCropperStub = defineComponent({
  name: 'AvatarCropper',
  props: ['src'],
  emits: ['crop', 'cancel'],
  setup(_props, { emit }) {
    return () => h('div', { 'data-test': 'cropper-stub' }, [
      h('button', { 'data-test': 'stub-crop', onClick: () => emit('crop', new Blob([1], { type: 'image/jpeg' })) }, 'crop'),
      h('button', { 'data-test': 'stub-cancel', onClick: () => emit('cancel') }, 'cancel'),
    ])
  },
})

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function seedUser(displayName = 'Alex Smith', avatarUrl?: string) {
  const auth = useAuthStore()
  auth.me = { id: 1, username: 'alex', displayName, role: 'user', avatarUrl }
  return auth
}

function mountOpen() {
  return mount(EditProfileDialog, {
    props: { open: true },
    attachTo: document.body,
    global: { plugins: [i18n()], stubs: { AvatarCropper: AvatarCropperStub } },
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

beforeEach(() => { setActivePinia(createPinia()); put.mockReset(); upload.mockReset(); del.mockReset(); document.body.innerHTML = '' })

describe('EditProfileDialog — display name', () => {
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

describe('EditProfileDialog — avatar', () => {
  function fileInput() {
    return document.body.querySelector('[data-test="avatar-file"]') as HTMLInputElement
  }
  function uploadBtn() {
    return document.body.querySelector('[data-test="avatar-upload"]') as HTMLButtonElement
  }
  function removeBtn() {
    return document.body.querySelector('[data-test="avatar-remove"]') as HTMLButtonElement
  }

  function simulateFile(f: File) {
    const el = fileInput()
    Object.defineProperty(el, 'files', { value: [f], configurable: true })
    el.dispatchEvent(new Event('change'))
  }

  it('shows the Upload button', async () => {
    seedUser(); mountOpen(); await flushPromises()
    expect(uploadBtn()).not.toBeNull()
  })

  it('hides the Remove button when there is no avatarUrl', async () => {
    seedUser('Alex Smith', undefined); mountOpen(); await flushPromises()
    expect(removeBtn()).toBeNull()
  })

  it('shows the Remove button when avatarUrl is set', async () => {
    seedUser('Alex Smith', '/api/prohibitorum/me/avatar'); mountOpen(); await flushPromises()
    expect(removeBtn()).not.toBeNull()
  })

  it('rejects a file >5MB, does NOT enter crop mode, and shows the error', async () => {
    seedUser(); mountOpen(); await flushPromises()
    const big = new File([new ArrayBuffer(6 * 1024 * 1024)], 'big.png', { type: 'image/png' })
    simulateFile(big)
    await flushPromises()
    expect(upload).not.toHaveBeenCalled()
    expect(document.body.querySelector('[data-test="cropper-stub"]')).toBeNull()
    expect(document.body.textContent).toContain(en.accountMenu.avatarTooLargeClient)
  })

  it('selecting a valid file shows the crop UI and does NOT upload yet', async () => {
    seedUser(); mountOpen(); await flushPromises()
    const small = new File([new ArrayBuffer(100)], 'a.png', { type: 'image/png' })
    simulateFile(small)
    await flushPromises()
    expect(URL.createObjectURL).toHaveBeenCalledWith(small)
    expect(document.body.querySelector('[data-test="cropper-stub"]')).not.toBeNull()
    expect(upload).not.toHaveBeenCalled()
  })

  it('confirming crop calls api.upload with the Blob then reloads the store', async () => {
    seedUser()
    upload.mockResolvedValue({})
    vi.mocked(api.get).mockResolvedValue({ id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' })
    mountOpen(); await flushPromises()
    const small = new File([new ArrayBuffer(100)], 'a.png', { type: 'image/png' })
    simulateFile(small)
    await flushPromises()
    // Click the stub crop button (emits a Blob)
    ;(document.body.querySelector('[data-test="stub-crop"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(upload).toHaveBeenCalledWith('/api/prohibitorum/me/avatar', expect.any(Blob))
    expect(vi.mocked(api.get)).toHaveBeenCalledWith('/api/prohibitorum/me')
    // Cropper dismissed after upload
    expect(document.body.querySelector('[data-test="cropper-stub"]')).toBeNull()
  })

  it('cancelling crop returns to the form without uploading', async () => {
    seedUser(); mountOpen(); await flushPromises()
    const small = new File([new ArrayBuffer(100)], 'a.png', { type: 'image/png' })
    simulateFile(small)
    await flushPromises()
    expect(document.body.querySelector('[data-test="cropper-stub"]')).not.toBeNull()
    ;(document.body.querySelector('[data-test="stub-cancel"]') as HTMLButtonElement).click()
    await flushPromises()
    expect(upload).not.toHaveBeenCalled()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:x')
    expect(document.body.querySelector('[data-test="cropper-stub"]')).toBeNull()
    // Form is back (the displayname input should be visible again)
    expect(document.body.querySelector('[data-test="edit-displayname-input"]')).not.toBeNull()
  })

  it('calls api.del on remove and reloads the store', async () => {
    seedUser('Alex Smith', '/api/prohibitorum/me/avatar')
    del.mockResolvedValue({})
    vi.mocked(api.get).mockResolvedValue({ id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' })
    mountOpen(); await flushPromises()
    removeBtn().click()
    await flushPromises()
    expect(del).toHaveBeenCalledWith('/api/prohibitorum/me/avatar')
    expect(vi.mocked(api.get)).toHaveBeenCalledWith('/api/prohibitorum/me')
  })
})
