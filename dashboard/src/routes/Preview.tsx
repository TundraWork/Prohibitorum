import {
  Button,
  Description,
  Input,
  Label,
  Skeleton,
  Spinner,
  Tabs,
  TextField,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useState } from "react";
import { notificationQueue } from "@/components/custom/AppNotifications";

export function Preview() {
  const { t } = useLingui();
  const [name, setName] = useState("Alex Morgan");
  return (
    <>
      <div className="eyebrow">
        <Trans id="preview.eyebrow">PHB-64 / M1 · Foundation preview</Trans>
      </div>
      <h1>
        <Trans id="preview.title">Interface preview</Trans>
      </h1>
      <p className="intro">
        <Trans id="preview.description">
          Review language, colors, and component states.
        </Trans>
      </p>
      <div className="rewrite-notice">
        <Trans id="preview.notice">
          The frontend is being rebuilt. Sign-in and other features are
          temporarily unavailable.
        </Trans>
      </div>
      <Tabs defaultSelectedKey="components" variant="secondary">
        <Tabs.ListContainer>
          <Tabs.List aria-label={t({ id: "preview.tabs", message: "Preview" })}>
            <Tabs.Tab id="components">
              <Trans id="components.title">Components</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab id="feedback">
              <Trans id="feedback.tab">Feedback</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
          </Tabs.List>
        </Tabs.ListContainer>
        <Tabs.Panel id="components" className="preview-card">
          <h2>
            <Trans id="components.title">Components</Trans>
          </h2>
          <TextField className="preview-field" value={name} onChange={setName}>
            <Label>
              <Trans id="name.label">Display name</Trans>
            </Label>
            <Input />
            <Description>
              <Trans id="name.description">
                This is a local preview. Your input will not be submitted.
              </Trans>
            </Description>
          </TextField>
          <div className="actions">
            <Button
              onPress={() => {
                notificationQueue.add({
                  title: (
                    <Trans id="notification.message">
                      Preview notification. No data was submitted.
                    </Trans>
                  ),
                });
              }}
            >
              <Trans id="notification.show">Show notification</Trans>
            </Button>
            <Button variant="outline" isDisabled>
              <Trans id="button.disabled">Disabled button</Trans>
            </Button>
          </div>
        </Tabs.Panel>
        <Tabs.Panel id="feedback" className="preview-card">
          <h2>
            <Trans id="feedback.title">State examples</Trans>
          </h2>
          <p>
            <Trans id="feedback.description">
              These visual examples do not send requests.
            </Trans>
          </p>
          <div className="actions">
            <Button isDisabled>
              <span aria-hidden="true">
                <Spinner size="sm" color="current" />
              </span>
              <Trans id="feedback.submitting">Submitting (example)</Trans>
            </Button>
          </div>
          <div className="error-example">
            <Trans id="feedback.error">
              Unable to save. Check your input and try again. (Example)
            </Trans>
          </div>
          <p>
            <Trans id="feedback.skeleton">Loading skeleton example</Trans>
          </p>
          <div className="skeleton-example" aria-hidden="true">
            <Skeleton className="skeleton-line" />
            <Skeleton className="skeleton-line skeleton-medium" />
            <Skeleton className="skeleton-line skeleton-short" />
          </div>
        </Tabs.Panel>
      </Tabs>
      <footer className="preview-footer">
        <span>
          <Trans id="preview.footer">Local preview · HeroUI components</Trans>
        </span>
        <div
          className="swatches"
          role="img"
          aria-label={t({
            id: "theme.swatches",
            message: "Theme color samples",
          })}
        >
          <span className="swatch-accent" />
          <span className="swatch-soft" />
          <span className="swatch-surface" />
          <span className="swatch-background" />
        </div>
      </footer>
    </>
  );
}
