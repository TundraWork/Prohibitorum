import { type ReactNode, useSyncExternalStore } from "react";
import {
  clampCount,
  getMockConfig,
  mockListMax,
  resetMockConfig,
  subscribeMockConfig,
  updateMockConfig,
} from "@/devtools/mock/model";

const controlClass =
  "shrink-0 rounded border border-slate-400/40 bg-transparent px-1.5 py-0.5";

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col">
      <h3 className="pb-1 text-xs font-medium uppercase tracking-wide opacity-60">
        {title}
      </h3>
      <div className="flex flex-col border-l-2 border-slate-400/40 pl-3">
        {children}
      </div>
    </section>
  );
}

/** Checkbox first, so the controls of a section share one left edge. */
function Toggle({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (next: boolean) => void;
}) {
  return (
    <label
      className={`flex items-center gap-2 py-1 ${
        disabled ? "cursor-not-allowed opacity-40" : "cursor-pointer"
      }`}
    >
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
        className="size-4 shrink-0 accent-emerald-500"
      />
      <span className="min-w-0">{label}</span>
    </label>
  );
}

function Count({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (next: number) => void;
}) {
  return (
    <label className="flex items-center gap-2 py-1">
      <input
        type="number"
        min={0}
        max={mockListMax}
        value={value}
        onChange={(event) => onChange(clampCount(Number(event.target.value)))}
        className={`w-14 text-left tabular-nums ${controlClass}`}
      />
      <span className="min-w-0">{label}</span>
    </label>
  );
}

function Text({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (next: string) => void;
}) {
  return (
    <label className="flex flex-col gap-1 py-1">
      <span>{label}</span>
      <input
        type="text"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className={`w-full ${controlClass}`}
      />
    </label>
  );
}

/**
 * The mock controls: whether reads and writes are answered from here, and the
 * account the console should believe it is serving. Answers land through the
 * query client, so a change repaints the pages that are already open.
 */
export function MockPanel() {
  const config = useSyncExternalStore(
    subscribeMockConfig,
    getMockConfig,
    getMockConfig,
  );

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-auto p-3 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <Toggle
            label="Mock API responses"
            checked={config.enabled}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.enabled = next;
              })
            }
          />
          <Toggle
            label="Override writes"
            checked={config.writes}
            disabled={!config.enabled}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.writes = next;
              })
            }
          />
        </div>
        <button
          type="button"
          onClick={resetMockConfig}
          className="shrink-0 text-xs underline opacity-60"
        >
          Reset
        </button>
      </div>
      <p className="text-xs opacity-60">
        Reads answer from this panel; with Override writes on, writes answer
        here too. A request with no fixture fails as <code>mock_unmocked</code>
        rather than reaching the server, so a passkey step-up cannot be faked.
        Mocking is off again after a reload.
      </p>

      <fieldset
        disabled={!config.enabled}
        className="flex min-h-0 flex-col gap-4 disabled:opacity-40"
      >
        <Section title="Session">
          <Toggle
            label="Signed in"
            checked={config.session.signedIn}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.session.signedIn = next;
              })
            }
          />
          <Toggle
            label="Avatar syncing"
            checked={config.session.avatarPending}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.session.avatarPending = next;
              })
            }
          />
          <Text
            label="Display name"
            value={config.session.displayName}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.session.displayName = next;
              })
            }
          />
          <Text
            label="Username"
            value={config.session.username}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.session.username = next;
              })
            }
          />
        </Section>

        <Section title="Sign-in methods">
          <Toggle
            label="Password set"
            checked={config.factors.passwordSet}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.factors.passwordSet = next;
              })
            }
          />
          <Toggle
            label="Authenticator enrolled"
            checked={config.factors.totpEnrolled}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.factors.totpEnrolled = next;
              })
            }
          />
          <Count
            label="Passkeys"
            value={config.factors.passkeys}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.factors.passkeys = next;
              })
            }
          />
          <Count
            label="Recovery codes"
            value={config.factors.recoveryCodes}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.factors.recoveryCodes = next;
              })
            }
          />
        </Section>

        <Section title="Lists">
          <Count
            label="Active sessions"
            value={config.lists.sessions}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.lists.sessions = next;
              })
            }
          />
          <Count
            label="Connected identities"
            value={config.lists.identities}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.lists.identities = next;
              })
            }
          />
          <Count
            label="Access tokens"
            value={config.lists.tokens}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.lists.tokens = next;
              })
            }
          />
          <Count
            label="Connected applications"
            value={config.lists.consentedApps}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.lists.consentedApps = next;
              })
            }
          />
          <Count
            label="Federation providers"
            value={config.lists.federationProviders}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.lists.federationProviders = next;
              })
            }
          />
          <Count
            label="Forward-auth apps"
            value={config.lists.forwardAuthApps}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.lists.forwardAuthApps = next;
              })
            }
          />
        </Section>

        <Section title="Step-up verification">
          <Toggle
            label="Window still fresh"
            checked={config.sudo.fresh}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.sudo.fresh = next;
              })
            }
          />
          <Toggle
            label="Allow passkey"
            checked={config.sudo.webauthn}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.sudo.webauthn = next;
              })
            }
          />
          <Toggle
            label="Allow password and code"
            checked={config.sudo.passwordTotp}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.sudo.passwordTotp = next;
              })
            }
          />
        </Section>

        <Section title="Instance">
          <Toggle
            label="Maintenance mode"
            checked={config.instance.maintenance}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.instance.maintenance = next;
              })
            }
          />
          <Toggle
            label="Initialized"
            checked={config.instance.bootstrapped}
            onChange={(next) =>
              updateMockConfig((draft) => {
                draft.instance.bootstrapped = next;
              })
            }
          />
        </Section>
      </fieldset>
    </div>
  );
}
