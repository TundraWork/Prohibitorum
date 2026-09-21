import {
  Button,
  Card,
  Description,
  Input,
  Label,
  Spinner,
  Tabs,
  TextField,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useState } from "react";
import { notificationQueue } from "@/components/custom/AppNotifications";
import {
  PageHeader,
  PageSkeleton,
  RewriteNotice,
} from "@/components/custom/PreviewLayout";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

export function Preview() {
  const { t } = useLingui();
  const [name, setName] = useState("Alex Morgan");
  return (
    <>
      <PageHeader
        eyebrow={
          <Trans id="preview.eyebrow">PHB-64 / M1 · Foundation preview</Trans>
        }
        title={<Trans id="preview.title">Interface preview</Trans>}
        description={
          <Trans id="preview.description">
            Review language, colors, and component states.
          </Trans>
        }
      />
      <RewriteNotice />
      <Tabs defaultSelectedKey="components" variant="secondary">
        <Tabs.ListContainer className="ml-2 w-fit max-w-full">
          <Tabs.List
            className="grid grid-flow-col auto-cols-fr"
            aria-label={t({ id: "preview.tabs", message: "Preview" })}
          >
            <Tabs.Tab className="whitespace-nowrap" id="components">
              <Trans id="components.title">Components</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab className="whitespace-nowrap" id="feedback">
              <Trans id="feedback.tab">Feedback</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
          </Tabs.List>
        </Tabs.ListContainer>
        <Tabs.Panel id="components">
          <Card>
            <Card.Header>
              <Card.Title>
                <Trans id="components.title">Components</Trans>
              </Card.Title>
            </Card.Header>
            <Card.Content>
              <TextField
                className="w-full max-w-md"
                value={name}
                onChange={setName}
              >
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
            </Card.Content>
            <Card.Footer className="flex flex-wrap gap-3">
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
            </Card.Footer>
          </Card>
        </Tabs.Panel>
        <Tabs.Panel id="feedback">
          <Card>
            <Card.Header>
              <Card.Title>
                <Trans id="feedback.title">State examples</Trans>
              </Card.Title>
              <Card.Description>
                <Trans id="feedback.description">
                  These visual examples do not send requests.
                </Trans>
              </Card.Description>
            </Card.Header>
            <Card.Content className="flex flex-col gap-4">
              <div className="flex flex-wrap gap-3">
                <Button isPending>
                  {({ isPending }) => (
                    <>
                      {isPending && <Spinner size="sm" color="current" />}
                      <Trans id="feedback.submitting">
                        Submitting (example)
                      </Trans>
                    </>
                  )}
                </Button>
              </div>
              <SurfaceAlert status="danger">
                <SurfaceAlert.Indicator />
                <SurfaceAlert.Content>
                  <SurfaceAlert.Title>
                    <Trans id="feedback.error">
                      Unable to save. Check your input and try again. (Example)
                    </Trans>
                  </SurfaceAlert.Title>
                </SurfaceAlert.Content>
              </SurfaceAlert>
              <p>
                <Trans id="feedback.skeleton">Loading skeleton example</Trans>
              </p>
              <div aria-hidden="true">
                <PageSkeleton />
              </div>
            </Card.Content>
          </Card>
        </Tabs.Panel>
      </Tabs>
      <footer className="text-sm text-muted">
        <span>
          <Trans id="preview.footer">Local preview · HeroUI components</Trans>
        </span>
      </footer>
    </>
  );
}
