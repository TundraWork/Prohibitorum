import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import {
  CertificateExpiryCell,
  RedirectCell,
} from "@/components/custom/ListCells";
import { i18n } from "@/i18n";

beforeEach(() => {
  i18n.activate("en");
});

function mount(node: React.ReactNode) {
  return render(<I18nProvider i18n={i18n}>{node}</I18nProvider>);
}

describe("CertificateExpiryCell", () => {
  it("says an expired certificate is expired in words, not only in colour", () => {
    mount(<CertificateExpiryCell notAfter="2020-01-01T00:00:00Z" />);
    expect(
      screen.getByText(/2020/).closest("time"),
    ).toHaveAccessibleDescription("Expired");
  });

  it("describes a valid certificate as valid", () => {
    mount(<CertificateExpiryCell notAfter="2999-01-01T00:00:00Z" />);
    expect(
      screen.getByText(/2999/).closest("time"),
    ).toHaveAccessibleDescription("Valid until this date");
  });
});

describe("RedirectCell", () => {
  it("lets assistive technology read every address, not only the first host", () => {
    mount(
      <RedirectCell uris={["https://a.example/cb", "https://b.example/cb"]} />,
    );
    const trigger = screen.getByText("a.example").closest("[aria-describedby]");
    expect(trigger).toHaveAccessibleDescription(
      "2 addresses: a.example and b.example",
    );
  });

  it("reads the address list in Chinese under the zh locale", () => {
    i18n.activate("zh");
    mount(
      <RedirectCell uris={["https://a.example/cb", "https://b.example/cb"]} />,
    );
    const trigger = screen.getByText("a.example").closest("[aria-describedby]");
    expect(trigger).toHaveAccessibleDescription(/a\.example.*b\.example/);
    expect(trigger).not.toHaveAccessibleDescription(/ and /);
  });
});
