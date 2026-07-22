/**
 * providerBrand — the single source of truth for per-protocol brand identity
 * (logo mark + colours). Consumed by BrandButton (the login sign-in buttons)
 * AND AppIcon (identity/provider icon chips), so Steam/VRChat look identical
 * wherever they appear — login page, Connected Accounts, anywhere else.
 *
 * Only protocols with a bundled brand mark live here. Everything else falls
 * back to its uploaded icon or an initial letter (AppIcon) / neutral button.
 */
import SteamLogo from '@/assets/steam-logo.svg'
import VRChatLogo from '@/assets/vrchat-logo.svg'

export interface ProviderBrand {
  /** Bundled brand mark (SVG asset URL). */
  logo: string
  /** Brand background colour for the button/chip. */
  bg: string
  /** Background on hover. */
  hoverBg: string
  /** Foreground (text/label) colour on `bg`. */
  fg: string
  /** Foreground on hover — set when `hoverBg` flips light↔dark. Defaults to `fg`. */
  hoverFg?: string
}

export const providerBrands: Record<string, ProviderBrand> = {
  steam: { logo: SteamLogo, bg: '#232730', hoverBg: '#171a21', fg: '#ffffff' },
  vrchat: { logo: VRChatLogo, bg: '#6ae3f9', hoverBg: '#064b5c', fg: '#0B1A21', hoverFg: '#ffffff' },
}

/** Returns the brand for a protocol, or undefined when there's no bundled mark. */
export function providerBrand(protocol?: string | null): ProviderBrand | undefined {
  return protocol ? providerBrands[protocol] : undefined
}
