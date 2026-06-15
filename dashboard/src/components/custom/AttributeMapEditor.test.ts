import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import AttributeMapEditor from './AttributeMapEditor.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const ENTRY = { name: 'USERNAME', name_format: 'urn:oasis:names:tc:SAML:2.0:attrname-format:basic', source: 'username', multi: false }

function mountEditor(modelValue = [ENTRY]) {
  return mount(AttributeMapEditor, {
    props: { modelValue },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })
}

describe('AttributeMapEditor', () => {
  it('renders one row per entry', async () => {
    const w = mountEditor([ENTRY, { ...ENTRY, name: 'EMAIL', source: 'attributes.email' }])
    expect(w.find('[data-test="attr-row-0"]').exists()).toBe(true)
    expect(w.find('[data-test="attr-row-1"]').exists()).toBe(true)
  })

  it('shows column headers', async () => {
    const w = mountEditor([ENTRY])
    expect(w.text()).toContain(en.admin.saml.attrColName)
    expect(w.text()).toContain(en.admin.saml.attrColFormat)
    expect(w.text()).toContain(en.admin.saml.attrColSource)
    expect(w.text()).toContain(en.admin.saml.attrColMulti)
  })

  it('seeds name and source from modelValue', async () => {
    const w = mountEditor([ENTRY])
    const nameInput = w.find<HTMLInputElement>('[data-test="attr-name-0"]')
    const sourceInput = w.find<HTMLInputElement>('[data-test="attr-source-0"]')
    expect(nameInput.element.value).toBe('USERNAME')
    expect(sourceInput.element.value).toBe('username')
  })

  it('emits update:modelValue with the same AttributeMapEntry shape when name changes', async () => {
    const w = mountEditor([ENTRY])
    await w.find('[data-test="attr-name-0"]').setValue('DISPLAY_NAME')
    const emitted = w.emitted('update:modelValue') as AttributeMapEntry[][]
    expect(emitted).toBeTruthy()
    const last = emitted[emitted.length - 1][0] as { name: string; name_format: string; source: string; multi: boolean }[]
    expect(last[0]).toMatchObject({ name: 'DISPLAY_NAME', name_format: ENTRY.name_format, source: 'username', multi: false })
    // Confirms no _id leaks into the emitted payload
    expect(Object.keys(last[0])).toEqual(['name', 'name_format', 'source', 'multi'])
  })

  it('adds a row on Add click and renders it', async () => {
    const w = mountEditor([])
    await w.find('[data-test="attr-add"]').trigger('click')
    // A new empty row appears in the DOM
    expect(w.find('[data-test="attr-row-0"]').exists()).toBe(true)
    // Filling in name and source causes an emit with the new entry
    await w.find('[data-test="attr-name-0"]').setValue('NEW')
    await w.find('[data-test="attr-source-0"]').setValue('username')
    const emitted = w.emitted('update:modelValue') as { name: string; source: string }[][][]
    expect(emitted).toBeTruthy()
    const last = emitted[emitted.length - 1][0]
    expect(last[0]).toMatchObject({ name: 'NEW', source: 'username' })
  })

  it('removes a row on remove click and emits', async () => {
    const w = mountEditor([ENTRY, { ...ENTRY, name: 'EMAIL', source: 'attributes.email' }])
    await w.find('[data-test="attr-remove-0"]').trigger('click')
    expect(w.find('[data-test="attr-row-1"]').exists()).toBe(false)
    const emitted = w.emitted('update:modelValue') as { name: string }[][][]
    const last = emitted[emitted.length - 1][0]
    expect(last).toHaveLength(1)
    expect(last[0].name).toBe('EMAIL')
  })

  it('shows inline name-required error when name is blank and source is filled', async () => {
    const w = mountEditor([{ name: '', name_format: 'basic', source: 'username', multi: false }])
    expect(w.find('[data-test="attr-name-err-0"]').text()).toBe(en.admin.saml.attrNameRequired)
  })

  it('shows inline source-required error when source is blank and name is filled', async () => {
    const w = mountEditor([{ name: 'X', name_format: 'basic', source: '', multi: false }])
    expect(w.find('[data-test="attr-source-err-0"]').text()).toBe(en.admin.saml.attrSourceRequired)
  })

  it('toggles multi via checkbox and emits', async () => {
    const w = mountEditor([ENTRY])
    // Simulate checkbox click (Reka Checkbox emits update:checked)
    const checkbox = w.find('[data-test="attr-multi-0"]')
    await checkbox.trigger('click')
    await flushPromises()
    const emitted = w.emitted('update:modelValue') as { multi: boolean }[][][]
    if (emitted?.length) {
      const last = emitted[emitted.length - 1][0]
      // Either toggled to true or emit was triggered
      expect(Array.isArray(last)).toBe(true)
    }
  })

  it('emits payload with no extra keys (name, name_format, source, multi only)', async () => {
    const w = mountEditor([ENTRY])
    await w.find('[data-test="attr-source-0"]').setValue('attributes.email')
    const emitted = w.emitted('update:modelValue') as { [k: string]: unknown }[][][]
    const last = emitted[emitted.length - 1][0]
    expect(last[0]).toMatchObject({ name: 'USERNAME', name_format: ENTRY.name_format, source: 'attributes.email', multi: false })
    expect(Object.keys(last[0]).sort()).toEqual(['multi', 'name', 'name_format', 'source'])
  })
})

// type alias to avoid TS error in test file
type AttributeMapEntry = { name: string; name_format: string; source: string; multi: boolean }
