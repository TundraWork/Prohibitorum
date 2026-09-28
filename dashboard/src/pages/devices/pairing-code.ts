import { z } from "zod";
import { optionalSearchText } from "@/app/search-params";

/** The server's code alphabet: base32 without 0, 1, I, L and O. */
export const pairingCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789";

export const pairingCodeLength = 8;

/** One slot's accepted characters. Either case: the server compares codes without it. */
export const pairingCodePattern = `^[${pairingCodeAlphabet}${pairingCodeAlphabet.toLowerCase()}]+$`;

/**
 * A whole pairing code, as the new device's QR link carries it in `code`:
 * eight characters of the alphabet, in either case.
 */
export const pairingCodeSchema = z
  .string()
  .regex(
    new RegExp(
      `^[${pairingCodeAlphabet}${pairingCodeAlphabet.toLowerCase()}]{${pairingCodeLength}}$`,
    ),
  );

/**
 * `/devices`' search: the code a new device's QR link carries. Anything that
 * is not a whole code is dropped, and the page opens as usual.
 */
export const devicesSearch = z.object({
  code: optionalSearchText(pairingCodeSchema),
});
