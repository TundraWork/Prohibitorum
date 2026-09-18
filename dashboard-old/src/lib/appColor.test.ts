import { describe, it, expect } from 'vitest'
import { appTintHue, srgbToOklch } from './appColor'

// The reserved STATE hues (Sage success, Amber caution, Rose danger). An app
// identity tint must never land near these — a tint that reads as a status
// signal would violate the State-Has-a-Colour rule.
const STATE_HUES = [150, 75, 22]

describe('appTintHue', () => {
  it('is deterministic — the same seed always maps to the same hue', () => {
    expect(appTintHue('Traefik')).toBe(appTintHue('Traefik'))
    expect(appTintHue('Downstream federation')).toBe(appTintHue('Downstream federation'))
  })

  it('returns a hue from the cool harmonised ramp', () => {
    const allowed = new Set([188, 205, 222, 240, 258, 276, 294, 312])
    for (const name of ['Traefik', 'Manual test RP', 'Downstream federation', 'Grafana', 'a', '']) {
      expect(allowed.has(appTintHue(name))).toBe(true)
    }
  })

  it('never collides with a reserved state hue', () => {
    for (const name of ['Traefik', 'Manual test RP', 'Downstream federation', 'Grafana', 'Vault', 'Wiki']) {
      const h = appTintHue(name)
      for (const s of STATE_HUES) {
        expect(Math.abs(h - s)).toBeGreaterThan(20)
      }
    }
  })

  it('spreads distinct names across more than one hue', () => {
    const names = ['Alpha', 'Bravo', 'Charlie', 'Delta', 'Echo', 'Foxtrot', 'Golf', 'Hotel']
    expect(new Set(names.map(appTintHue)).size).toBeGreaterThan(1)
  })

  it('falls back to a stable hue for an empty seed', () => {
    expect(appTintHue('')).toBe(appTintHue('   '))
  })
})

describe('srgbToOklch', () => {
  it('returns null for malformed input', () => {
    expect(srgbToOklch('nope')).toBeNull()
    expect(srgbToOklch('#fff')).toBeNull()
  })

  it('reports near-zero chroma for grayscale', () => {
    const g = srgbToOklch('#808080')!
    expect(g.c).toBeLessThan(0.01)
  })

  it('puts primaries in the right hue neighbourhood', () => {
    // OKLCH hues: red ≈ 29°, green ≈ 142°, blue ≈ 264°.
    expect(Math.abs(srgbToOklch('#ff0000')!.h - 29)).toBeLessThan(15)
    expect(Math.abs(srgbToOklch('#0000ff')!.h - 264)).toBeLessThan(15)
    expect(srgbToOklch('#00ff00')!.c).toBeGreaterThan(0.1)
  })

  it('accepts hex without a leading hash', () => {
    expect(srgbToOklch('2563eb')).not.toBeNull()
  })
})
