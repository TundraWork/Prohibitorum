import { useLingui } from "@lingui/react";
import { ServerFormError } from "@/forms/server-errors";

export function FormMessages({ errors }: { errors: readonly unknown[] }) {
  const { i18n } = useLingui();
  const messages = errors.flatMap((error): string[] => {
    if (Array.isArray(error)) return [];
    if (error instanceof ServerFormError) return [i18n._(error.message)];
    if (typeof error === "string") return [error];
    if (
      typeof error === "object" &&
      error !== null &&
      "id" in error &&
      typeof error.id === "string"
    ) {
      const descriptor = {
        id: error.id,
        message:
          "message" in error && typeof error.message === "string"
            ? error.message
            : undefined,
        // A message that names something, such as the provider a sign-in
        // went through, carries the name beside it.
        values:
          "values" in error &&
          typeof error.values === "object" &&
          error.values !== null
            ? (error.values as Record<string, unknown>)
            : undefined,
      };
      return [i18n._(descriptor)];
    }
    return [];
  });
  return (
    <>
      {[...new Set(messages)].map((message) => (
        <span className="block" key={message}>
          {message}
        </span>
      ))}
    </>
  );
}
