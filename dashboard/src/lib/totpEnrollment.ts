import { queryClient } from '@/queries/client'
import { configQuery } from '@/queries/resources'
import { createTotpEnrollment, type TotpEnrollment } from '@/lib/totp'

/** Generate a TOTP enrollment from the server-authoritative public settings. */
export async function generateTotpEnrollment(accountLabel: string): Promise<TotpEnrollment> {
  const config = await queryClient.fetchQuery(configQuery())
  return createTotpEnrollment(accountLabel, config.totp)
}
