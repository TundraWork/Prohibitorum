/** A key's id, set apart so it reads as the thing being named. */
export function KeyId({ kid }: { kid: string }) {
  return <code className="font-mono text-[0.9em] break-all">{kid}</code>;
}
