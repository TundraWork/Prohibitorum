import { createContext } from "react";

/**
 * True inside the identity-check dialog. While that dialog is open it owns the
 * one spinner on screen, so a pending button behind it stays locked without
 * spinning; see `Button`.
 */
export const SudoScopeContext = createContext(false);
