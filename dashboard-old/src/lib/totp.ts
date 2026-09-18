const BASE32_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
const TOTP_SECRET_BYTES = 20

export interface TotpSettings {
  issuer: string
  algorithm: string
  digits: number
  period: number
}

export interface TotpEnrollment {
  secretBase32: string
  otpauthUri: string
}

function encodeBase32(bytes: Uint8Array): string {
  let buffer = 0
  let bits = 0
  let encoded = ''

  for (const byte of bytes) {
    buffer = (buffer << 8) | byte
    bits += 8
    while (bits >= 5) {
      bits -= 5
      encoded += BASE32_ALPHABET[(buffer >>> bits) & 31]
    }
    buffer &= (1 << bits) - 1
  }

  if (bits > 0) encoded += BASE32_ALPHABET[(buffer << (5 - bits)) & 31]
  return encoded
}

export function createTotpEnrollment(accountLabel: string, settings: TotpSettings): TotpEnrollment {
  const secretBytes = new Uint8Array(TOTP_SECRET_BYTES)
  crypto.getRandomValues(secretBytes)
  const secretBase32 = encodeBase32(secretBytes)
  const label = `${encodeURIComponent(settings.issuer)}:${encodeURIComponent(accountLabel)}`
  const query = new URLSearchParams({
    secret: secretBase32,
    issuer: settings.issuer,
    algorithm: settings.algorithm,
    digits: String(settings.digits),
    period: String(settings.period),
  })

  return {
    secretBase32,
    otpauthUri: `otpauth://totp/${label}?${query.toString()}`,
  }
}
