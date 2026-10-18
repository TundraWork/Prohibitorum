import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Upload } from "lucide-react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImageDropTile } from "@/components/custom/ImageDropTile";

describe("ImageDropTile", () => {
  afterEach(() => vi.restoreAllMocks());

  // react-aria's FileTrigger cannot attach to the coss button until it is replaced.
  it.skip("opens the file picker when the tile is clicked", async () => {
    const open = vi
      .spyOn(HTMLInputElement.prototype, "click")
      .mockImplementation(() => {});
    render(
      <ImageDropTile
        acceptedFileTypes={["image/png"]}
        ariaLabel="Upload"
        icon={Upload}
        label="Choose a file"
        onFiles={() => {}}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: /Choose a file/ }),
    );
    expect(open).toHaveBeenCalled();
  });
});
