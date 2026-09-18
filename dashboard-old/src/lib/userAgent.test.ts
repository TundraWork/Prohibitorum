import { describe, it, expect } from 'vitest'
import { formatUserAgent } from './userAgent'

describe('formatUserAgent', () => {
  it('returns Unknown device for empty / null / undefined', () => {
    expect(formatUserAgent(undefined)).toBe('Unknown device')
    expect(formatUserAgent(null)).toBe('Unknown device')
    expect(formatUserAgent('')).toBe('Unknown device')
    expect(formatUserAgent('   ')).toBe('Unknown device')
  })

  it('detects Chrome on Windows', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36'
    )).toBe('Chrome on Windows')
  })

  it('detects Edge on Windows (Edg/ token comes before Chrome check)', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 Edg/125.0.0.0'
    )).toBe('Edge on Windows')
  })

  it('detects Firefox on macOS', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:127.0) Gecko/20100101 Firefox/127.0'
    )).toBe('Firefox on macOS')
  })

  it('detects Safari on iPhone', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1'
    )).toBe('Safari on iPhone')
  })

  it('detects Safari on iPad', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (iPad; CPU OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1'
    )).toBe('Safari on iPad')
  })

  it('detects Chrome on Android', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Mobile Safari/537.36'
    )).toBe('Chrome on Android')
  })

  it('detects Firefox on Linux', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0'
    )).toBe('Firefox on Linux')
  })

  it('detects Chrome on macOS (macOS in UA)', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36'
    )).toBe('Chrome on macOS')
  })

  it('handles Chrome on iOS (CriOS)', () => {
    expect(formatUserAgent(
      'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/124.0.6367.111 Mobile/15E148 Safari/604.1'
    )).toBe('Chrome on iPhone')
  })

  it('falls back to first token for completely unrecognised UA', () => {
    expect(formatUserAgent('curl/7.88.1')).toBe('curl')
  })

  it('short unrecognised string is returned as-is', () => {
    expect(formatUserAgent('Wget')).toBe('Wget')
  })
})
