import { createFormHook } from "@tanstack/react-form";
import { Form } from "@/components/custom/Form";
import { FormError } from "@/components/custom/FormError";
import { FormField } from "@/components/custom/FormField";
import { SubmitButton } from "@/components/custom/SubmitButton";
import { fieldContext, formContext } from "@/forms/context";

export const { useAppForm } = createFormHook({
  fieldContext,
  formContext,
  fieldComponents: { FormField },
  formComponents: { Form, FormError, SubmitButton },
});
