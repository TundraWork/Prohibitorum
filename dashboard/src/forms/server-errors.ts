import type { MessageDescriptor } from "@lingui/core";
import type { AnyFormApi } from "@tanstack/react-form";
import { ApiError, describeError } from "@/api/errors";

export type ServerFieldMap<TField extends string> = {
  locations: Readonly<Record<string, TField>>;
  codes: Readonly<Record<string, TField>>;
};

export class ServerFormError {
  readonly message: MessageDescriptor;

  constructor(error: unknown) {
    this.message = describeError(error);
  }
}

export function mapServerError<TField extends string>(
  error: unknown,
  mapping: ServerFieldMap<TField>,
  registeredFields: readonly TField[],
): {
  form?: ServerFormError;
  fields?: Partial<Record<TField, ServerFormError>>;
} {
  const message = new ServerFormError(error);
  let field: TField | undefined;
  if (error instanceof ApiError) {
    if (error.code === "validation_failed") {
      const location = error.details?.location;
      if (
        typeof location === "string" &&
        Object.hasOwn(mapping.locations, location)
      ) {
        field = mapping.locations[location];
      }
    } else if (error.code && Object.hasOwn(mapping.codes, error.code)) {
      field = mapping.codes[error.code];
    }
  }
  if (field !== undefined && registeredFields.includes(field)) {
    const fields: Partial<Record<TField, ServerFormError>> = {};
    Object.defineProperty(fields, field, { value: message, enumerable: true });
    return { fields };
  }
  return { form: message };
}

export function withoutServerErrors(error: unknown): unknown {
  if (error instanceof ServerFormError) return undefined;
  if (!Array.isArray(error)) return error;
  const remaining = error
    .map(withoutServerErrors)
    .filter((item) => item !== undefined);
  return remaining.length ? remaining : undefined;
}

export function clearServerErrors(form: AnyFormApi) {
  form.setErrorMap({
    onSubmit: withoutServerErrors(form.state.errorMap.onSubmit),
  });
  for (const name of Object.keys(form.fieldInfo)) {
    const field = form.fieldInfo[name]?.instance;
    if (!field) continue;
    form.setFieldMeta(name, (meta) => ({
      ...meta,
      errorMap: {
        ...meta.errorMap,
        onSubmit: withoutServerErrors(meta.errorMap.onSubmit),
      },
    }));
  }
}

export function applyServerError<TField extends string>(
  form: AnyFormApi,
  error: unknown,
  mapping: ServerFieldMap<TField>,
) {
  const registeredFields = Object.keys(form.fieldInfo).filter(
    (name) => form.fieldInfo[name]?.instance,
  );
  const mapped = mapServerError<string>(error, mapping, registeredFields);
  const fields: Record<string, unknown> = {};
  for (const name of registeredFields) {
    const existing = withoutServerErrors(
      form.getFieldMeta(name)?.errorMap.onSubmit,
    );
    const server = mapped.fields?.[name];
    fields[name] =
      existing && server ? [existing, server] : (server ?? existing);
  }
  const existing = withoutServerErrors(form.state.errorMap.onSubmit);
  form.setErrorMap({
    onSubmit: {
      form:
        existing && mapped.form
          ? [existing, mapped.form]
          : (mapped.form ?? existing),
      fields,
    },
  });
}
