import { Description, Label, Link, Separator, Tooltip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { KeyRound } from "lucide-react";
import { useState } from "react";
import { ApiError, isCancellation } from "@/api/errors";
import {
  removeUnsplashKeyMutationOptions,
  updateLoginAppearanceMutationOptions,
} from "@/api/mutations";
import { loginAppearanceQueryOptions } from "@/api/queries";
import type { AdminLoginAppearance } from "@/api/raw-admin-paths";
import type { LoginAppearance } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { LoginImagesControl } from "@/pages/admin/settings/LoginImagesControl";
import { LoginPreview } from "@/pages/admin/settings/LoginPreview";
import {
  CardPositionControls,
  ColorControls,
  FieldGroup,
  GradientControls,
  IntervalField,
  MarketSelect,
  RotationControls,
  SourceTiles,
  SurfaceControls,
  ThemeControls,
} from "@/pages/admin/settings/SignInPageControls";
import {
  colorError,
  intervalError,
  keyError,
  queryError,
  type SignInPageValues,
  signInPageBody,
} from "@/pages/admin/settings/sign-in-page-form";

/** The server errors that belong to the access key field. */
const keyErrorCodes = ["unsplash_key_invalid", "unsplash_key_required"];

/**
 * How the sign-in page looks: its background, where its card sits, its theme,
 * and the surfaces of its card and toolbar, with the page itself previewed
 * beside the form.
 *
 * One Save sends the background source, its settings, the card's position, the
 * theme and the surfaces, and a new Unsplash key if one was typed. Adding or
 * removing an image and removing the key happen at once, without waiting for
 * Save.
 */
export function SignInPagePanel() {
  const { data } = useSuspenseQuery(loginAppearanceQueryOptions());
  return (
    <Section title={<Trans id="settings.sign-in.title">Sign-in page</Trans>}>
      <SignInPageCard saved={data} />
    </Section>
  );
}

function SignInPageCard({ saved }: { saved: AdminLoginAppearance }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateLoginAppearanceMutationOptions(queryClient));
  const { loginImages } = useInstanceBranding();
  const [replacingKey, setReplacingKey] = useState(false);
  const hasKey = saved.hasUnsplashKey;

  const form = useAppForm({
    defaultValues: {
      appearance: saved.appearance,
      unsplashAccessKey: "",
    } as SignInPageValues,
    validators: {
      // A keyword is kept while another source is chosen and its field is
      // hidden, and the server checks it all the same.
      onSubmit: ({ value }) =>
        value.appearance.background.source !== "unsplash" &&
        queryError(value.appearance.background.unsplash.query) !== undefined
          ? t({
              id: "settings.sign-in.unsplash.query.hidden_invalid",
              message:
                "The Unsplash keyword can't be used. Choose Unsplash to correct it.",
            })
          : undefined,
    },
    onSubmit: async ({ value }) => {
      // A key typed for Unsplash is sent only while Unsplash is chosen.
      const body = signInPageBody(
        value.appearance.background.source === "unsplash"
          ? value
          : { ...value, unsplashAccessKey: "" },
      );
      try {
        await update.mutateAsync(body);
        form.setFieldValue("unsplashAccessKey", "");
        setReplacingKey(false);
      } catch (error) {
        if (isCancellation(error)) return;
        // Anything else is the error toast's; only the key has a field to fix.
        if (
          error instanceof ApiError &&
          keyErrorCodes.includes(error.code ?? "") &&
          form.fieldInfo.unsplashAccessKey?.instance
        ) {
          applyServerError(form, error, {
            locations: {},
            codes: {
              unsplash_key_invalid: "unsplashAccessKey",
              unsplash_key_required: "unsplashAccessKey",
            },
          });
        }
      }
    },
  });

  const appearance = useStore(
    form.store,
    (state) => state.values.appearance,
  ) as LoginAppearance;
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const background = appearance.background;
  const setAppearance = (edit: (draft: LoginAppearance) => void) => {
    const next = structuredClone(form.getFieldValue("appearance"));
    edit(next);
    form.setFieldValue("appearance", next);
  };
  const showKeyField = !hasKey || replacingKey;

  return (
    <ConsoleCard wide contentClassName="@container">
      <div className="grid gap-8 @[50rem]:grid-cols-[minmax(0,1fr)_minmax(0,24rem)] @[50rem]:gap-10">
        {/* First in the DOM, so a narrow screen shows the effect above the
            settings; the wide layout moves it to the right column and keeps
            it in view below the console's 4rem header. */}
        <div className="@[50rem]:order-2">
          <div className="@[50rem]:sticky @[50rem]:top-[calc(var(--app-sticky-offset)+5.5rem)]">
            <LoginPreview
              appearance={appearance}
              images={loginImages.map((image) => image.url)}
              hasUnsplashKey={hasKey}
            />
          </div>
        </div>

        <div className="min-w-0 @[50rem]:order-1">
          <form.AppForm>
            <form.Form
              label={t({
                id: "settings.sign-in.form",
                message: "Sign-in page",
              })}
              className="flex flex-col gap-8"
            >
              <form.FormError />
              <FieldGroup
                legend={
                  <Trans id="settings.sign-in.background">Background</Trans>
                }
              >
                <SourceTiles
                  value={background.source}
                  isDisabled={submitting}
                  onChange={(source) =>
                    setAppearance((draft) => {
                      draft.background.source = source;
                    })
                  }
                />

                {background.source === "none" && (
                  <Description>
                    <Trans id="settings.sign-in.none.hint">
                      The page's own background, which follows light and dark
                      mode.
                    </Trans>
                  </Description>
                )}

                {background.source === "color" && (
                  <form.Field
                    name="appearance.background.color"
                    validators={{ onSubmit: ({ value }) => colorError(value) }}
                  >
                    {(field) => (
                      <ColorControls
                        value={field.state.value}
                        isDisabled={submitting}
                        onChange={(hex) => field.handleChange(hex)}
                      />
                    )}
                  </form.Field>
                )}

                {background.source === "gradient" && (
                  <form.Field name="appearance.background.gradient">
                    {(field) => (
                      <GradientControls
                        value={field.state.value}
                        isDisabled={submitting}
                        onChange={(gradient) => field.handleChange(gradient)}
                      />
                    )}
                  </form.Field>
                )}

                {background.source === "bing" && (
                  <>
                    <form.Field name="appearance.background.bing.market">
                      {(field) => (
                        <MarketSelect
                          value={field.state.value}
                          isDisabled={submitting}
                          onChange={(market) => field.handleChange(market)}
                        />
                      )}
                    </form.Field>
                    <form.AppField name="appearance.background.bing.showCaption">
                      {(field) => (
                        <field.SwitchField
                          label={
                            <Trans id="settings.sign-in.bing.caption">
                              Show the picture's title and copyright
                            </Trans>
                          }
                        />
                      )}
                    </form.AppField>
                  </>
                )}

                {background.source === "unsplash" && (
                  <>
                    {showKeyField ? (
                      <form.AppField
                        name="unsplashAccessKey"
                        validators={{
                          onSubmit: ({ value, fieldApi }) =>
                            keyError(value, {
                              source:
                                fieldApi.form.getFieldValue("appearance")
                                  .background.source,
                              hasSavedKey: hasKey,
                            }),
                        }}
                      >
                        {(field) => (
                          <field.FormField
                            className="max-w-sm"
                            label={
                              <Trans id="settings.sign-in.unsplash.key">
                                Access key
                              </Trans>
                            }
                            type="password"
                            autoComplete="off"
                            spellCheck={false}
                            isMonospace
                            variant="secondary"
                            description={
                              <Trans id="settings.sign-in.unsplash.key.hint">
                                From your application on{" "}
                                <Link
                                  href="https://unsplash.com/oauth/applications"
                                  target="_blank"
                                  rel="noreferrer"
                                  className="text-xs"
                                >
                                  unsplash.com/developers
                                </Link>
                                .
                              </Trans>
                            }
                          />
                        )}
                      </form.AppField>
                    ) : (
                      <SavedKeyRow
                        inUse={
                          saved.appearance.background.source === "unsplash"
                        }
                        isDisabled={submitting}
                        onReplace={() => setReplacingKey(true)}
                      />
                    )}
                    <form.AppField
                      name="appearance.background.unsplash.query"
                      validators={{
                        onBlur: ({ value }) => queryError(value),
                        onSubmit: ({ value }) => queryError(value),
                      }}
                    >
                      {(field) => (
                        <field.FormField
                          className="max-w-sm"
                          label={
                            <Trans id="settings.sign-in.unsplash.query">
                              Keyword
                            </Trans>
                          }
                          placeholder="mountains"
                          autoComplete="off"
                          variant="secondary"
                          description={
                            <Trans id="settings.sign-in.unsplash.query.hint">
                              Leave it empty for any photo.
                            </Trans>
                          }
                        />
                      )}
                    </form.AppField>
                    {/* Always offered: how many photos a batch holds is not
                        known before it is saved. */}
                    <RotationControls
                      order={background.unsplash.order}
                      isDisabled={submitting}
                      onOrder={(order) =>
                        setAppearance((draft) => {
                          draft.background.unsplash.order = order;
                        })
                      }
                    >
                      <form.Field
                        name="appearance.background.unsplash.intervalSeconds"
                        validators={{
                          onSubmit: ({ value }) => intervalError(value),
                        }}
                      >
                        {(field) => (
                          <IntervalField
                            value={field.state.value}
                            isDisabled={submitting}
                            errors={field.state.meta.errors}
                            onChange={(seconds) => field.handleChange(seconds)}
                          />
                        )}
                      </form.Field>
                    </RotationControls>
                  </>
                )}

                {background.source === "images" && (
                  <>
                    <LoginImagesControl images={loginImages} />
                    {loginImages.length > 1 && (
                      <RotationControls
                        order={background.images.order}
                        isDisabled={submitting}
                        onOrder={(order) =>
                          setAppearance((draft) => {
                            draft.background.images.order = order;
                          })
                        }
                      >
                        <form.Field
                          name="appearance.background.images.intervalSeconds"
                          validators={{
                            onSubmit: ({ value }) => intervalError(value),
                          }}
                        >
                          {(field) => (
                            <IntervalField
                              value={field.state.value}
                              isDisabled={submitting}
                              errors={field.state.meta.errors}
                              onChange={(seconds) =>
                                field.handleChange(seconds)
                              }
                            />
                          )}
                        </form.Field>
                      </RotationControls>
                    )}
                  </>
                )}
              </FieldGroup>

              <Separator />

              <div className="@container/surfaces">
                <div className="grid gap-8 @[26rem]/surfaces:grid-cols-2 @[26rem]/surfaces:gap-6">
                  <CardPositionControls
                    value={appearance.cardPosition}
                    isDisabled={submitting}
                    onChange={(position) =>
                      setAppearance((draft) => {
                        draft.cardPosition = position;
                      })
                    }
                  />
                  <ThemeControls
                    value={appearance.theme}
                    isDisabled={submitting}
                    onChange={(theme) =>
                      setAppearance((draft) => {
                        draft.theme = theme;
                      })
                    }
                  />
                </div>
              </div>

              <Separator />

              <div className="@container/surfaces">
                <div className="grid gap-8 @[26rem]/surfaces:grid-cols-2 @[26rem]/surfaces:gap-6">
                  <SurfaceControls
                    legend={
                      <Trans id="settings.sign-in.card">Sign-in card</Trans>
                    }
                    value={appearance.card}
                    isDisabled={submitting}
                    onChange={(card) =>
                      setAppearance((draft) => {
                        draft.card = card;
                      })
                    }
                  />
                  <SurfaceControls
                    legend={
                      <Trans id="settings.sign-in.toolbar">Toolbar</Trans>
                    }
                    value={appearance.capsules}
                    isDisabled={submitting}
                    onChange={(capsules) =>
                      setAppearance((draft) => {
                        draft.capsules = capsules;
                      })
                    }
                  />
                </div>
              </div>

              <div>
                <form.SubmitButton>
                  <Trans id="settings.sign-in.save">Save</Trans>
                </form.SubmitButton>
              </div>
            </form.Form>
          </form.AppForm>
        </div>
      </div>
    </ConsoleCard>
  );
}

/**
 * The saved key, which is never shown again: replace it with a new one, or
 * remove it. It cannot be removed while the saved background is Unsplash.
 */
function SavedKeyRow({
  inUse,
  isDisabled,
  onReplace,
}: {
  inUse: boolean;
  isDisabled: boolean;
  onReplace: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useMutation(removeUnsplashKeyMutationOptions(queryClient));
  const removeButton = (
    <Button
      size="sm"
      variant="danger-soft"
      isDisabled={inUse || isDisabled}
      isPending={remove.isPending}
      onPress={() => remove.mutate()}
    >
      <Trans id="settings.sign-in.unsplash.key.remove">Remove</Trans>
    </Button>
  );
  return (
    <div className="flex flex-col gap-2">
      <Label>
        <Trans id="settings.sign-in.unsplash.key">Access key</Trans>
      </Label>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span className="flex items-center gap-2 text-sm text-foreground">
          <KeyRound size={16} aria-hidden="true" className="text-muted" />
          <Trans id="settings.sign-in.unsplash.key.saved">
            Access key saved
          </Trans>
        </span>
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="secondary"
            isDisabled={isDisabled}
            onPress={onReplace}
          >
            <Trans id="settings.sign-in.unsplash.key.replace">Replace</Trans>
          </Button>
          {inUse ? (
            <Tooltip delay={0}>
              <Tooltip.Trigger>{removeButton}</Tooltip.Trigger>
              <Tooltip.Content>
                <Trans id="settings.sign-in.unsplash.key.in_use">
                  Switch the background to another source and save before
                  removing the key.
                </Trans>
              </Tooltip.Content>
            </Tooltip>
          ) : (
            removeButton
          )}
        </div>
      </div>
    </div>
  );
}
