import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import ScopeVocabularyEditor from './ScopeVocabularyEditor.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

type ScopeEntry = { name: string; description: string }
const ENTRY: ScopeEntry = { name: 'read', description: 'Read access' }

function mountEditor(modelValue: ScopeEntry[] = [ENTRY]) {
  return mount(ScopeVocabularyEditor, {
    props: { modelValue },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
}

describe('ScopeVocabularyEditor', () => {
  it('renders one row per entry', async () => {
    const w = mountEditor([ENTRY, { name: 'write', description: 'Write access' }])
    expect(w.find('[data-test="scope-row-0"]').exists()).toBe(true)
    expect(w.find('[data-test="scope-row-1"]').exists()).toBe(true)
  })

  it('shows column headers', async () => {
    const w = mountEditor([ENTRY])
    expect(w.text()).toContain(en.admin.forwardAuth.scopeName)
    expect(w.text()).toContain(en.admin.forwardAuth.scopeDescription)
  })

  it('seeds name and description from modelValue', async () => {
    const w = mountEditor([ENTRY])
    const nameInput = w.find<HTMLInputElement>('[data-test="scope-name-0"]')
    const descInput = w.find<HTMLInputElement>('[data-test="scope-desc-0"]')
    expect(nameInput.element.value).toBe('read')
    expect(descInput.element.value).toBe('Read access')
  })

  it('emits update:modelValue with the ScopeEntry shape (no _id) when name changes', async () => {
    const w = mountEditor([ENTRY])
    await w.find('[data-test="scope-name-0"]').setValue('admin')
    const emitted = w.emitted('update:modelValue') as ScopeEntry[][]
    expect(emitted).toBeTruthy()
    const last = emitted[emitted.length - 1][0]
    expect(last[0]).toMatchObject({ name: 'admin', description: 'Read access' })
    expect(Object.keys(last[0]).sort()).toEqual(['description', 'name'])
  })

  it('adds a row on Add click and focuses the new name input', async () => {
    const w = mountEditor([])
    expect(w.find('[data-test="scope-row-0"]').exists()).toBe(false)
    await w.find('[data-test="scope-add"]').trigger('click')
    expect(w.find('[data-test="scope-row-0"]').exists()).toBe(true)
    // focus-on-add works even when the list started empty (ref on outer container)
    const nameInput = w.find('[data-test="scope-name-0"]').element
    expect(document.activeElement).toBe(nameInput)
  })

  it('removes a row on remove click and emits the remaining rows', async () => {
    const w = mountEditor([ENTRY, { name: 'write', description: 'Write access' }])
    await w.find('[data-test="scope-remove-0"]').trigger('click')
    expect(w.find('[data-test="scope-row-1"]').exists()).toBe(false)
    const emitted = w.emitted('update:modelValue') as ScopeEntry[][]
    const last = emitted[emitted.length - 1][0]
    expect(last).toHaveLength(1)
    expect(last[0].name).toBe('write')
  })

  it('re-seeds when modelValue prop changes externally', async () => {
    const w = mountEditor([ENTRY])
    await w.setProps({ modelValue: [{ name: 'reset', description: 'New' }] })
    const nameInput = w.find<HTMLInputElement>('[data-test="scope-name-0"]')
    expect(nameInput.element.value).toBe('reset')
  })
})
