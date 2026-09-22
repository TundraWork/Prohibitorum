import { createFormHook } from "@tanstack/react-form";
import {
  AccountPicker,
  GroupPicker,
  IdentityProviderPicker,
} from "@/components/custom/EntityPicker";
import { Form } from "@/components/custom/Form";
import { FormError } from "@/components/custom/FormError";
import { FormField } from "@/components/custom/FormField";
import {
  NumberField,
  ReadOnlyField,
  SwitchField,
  TextAreaField,
} from "@/components/custom/FormFields";
import { OtpField } from "@/components/custom/OtpField";
import { SubmitButton } from "@/components/custom/SubmitButton";
import { fieldContext, formContext } from "@/forms/context";

export const { useAppForm } = createFormHook({
  fieldContext,
  formContext,
  fieldComponents: {
    FormField,
    OtpField,
    TextAreaField,
    SwitchField,
    NumberField,
    ReadOnlyField,
    AccountPicker,
    GroupPicker,
    IdentityProviderPicker,
  },
  formComponents: { Form, FormError, SubmitButton },
});
