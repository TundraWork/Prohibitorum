import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import en from '@/locales/en'
import AvatarCropper from './AvatarCropper.vue'

// Stub the real Cropper (canvas/DOM-heavy) and just capture the props it
// receives, so we can assert the default-size selection function.
const CropperStub = defineComponent({
  name: 'Cropper',
  props: ['src', 'stencilProps', 'canvas', 'defaultSize'],
  template: '<div data-test="cropper" />',
})

const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

function mountCropper() {
  return mount(AvatarCropper, {
    props: { src: 'blob:x' },
    global: { plugins: [i18n], stubs: { Cropper: CropperStub } },
  })
}

type DefaultSizeFn = (a: { imageSize: { width: number; height: number } }) => { width: number; height: number }

describe('AvatarCropper — default crop selection', () => {
  it('defaults the crop to the largest centered square = min(w,h)', () => {
    const w = mountCropper()
    const defaultSize = w.findComponent(CropperStub).props('defaultSize') as DefaultSizeFn
    expect(typeof defaultSize).toBe('function')
    expect(defaultSize({ imageSize: { width: 800, height: 600 } })).toEqual({ width: 600, height: 600 })
    expect(defaultSize({ imageSize: { width: 400, height: 900 } })).toEqual({ width: 400, height: 400 })
    expect(defaultSize({ imageSize: { width: 512, height: 512 } })).toEqual({ width: 512, height: 512 })
  })
})
