import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Upload } from "lucide-react";
import { describe, expect, it, vi } from "vitest";
import { ImageDropTile } from "@/components/custom/ImageDropTile";

const png = () => new File(["x"], "a.png", { type: "image/png" });

function mount(
  props: Partial<React.ComponentProps<typeof ImageDropTile>> = {},
  onFiles = vi.fn(),
) {
  render(
    <ImageDropTile
      acceptedFileTypes={["image/png"]}
      ariaLabel="Upload"
      icon={Upload}
      label="Choose a file"
      onFiles={onFiles}
      {...props}
    />,
  );
  return onFiles;
}

const drop = (files: File[]) =>
  fireEvent.drop(screen.getByRole("group", { name: "Upload" }), {
    dataTransfer: { files, types: ["Files"] },
  });

describe("ImageDropTile", () => {
  it("hands over a file chosen through the labelled input", async () => {
    const onFiles = mount();
    const file = png();

    await userEvent.upload(screen.getByLabelText("Choose a file"), file);

    expect(onFiles).toHaveBeenCalledWith([file]);
  });

  it("limits the input to the accepted types and one file by default", () => {
    mount();

    const input = screen.getByLabelText("Choose a file");
    expect(input).toHaveAttribute("accept", "image/png");
    expect(input).not.toHaveAttribute("multiple");
  });

  it("hands over a dropped file", () => {
    const onFiles = mount();
    const file = png();

    drop([file]);

    expect(onFiles).toHaveBeenCalledWith([file]);
  });

  it("takes only the first dropped file unless it allows several", () => {
    const first = png();
    const second = png();
    const one = mount();
    drop([first, second]);
    expect(one).toHaveBeenCalledWith([first]);
  });

  it("takes every dropped file when it allows several", () => {
    const first = png();
    const second = png();
    const onFiles = mount({ allowsMultiple: true });
    drop([first, second]);
    expect(onFiles).toHaveBeenCalledWith([first, second]);
  });

  it("ignores a drop that carries no files", () => {
    const onFiles = mount();
    drop([]);
    expect(onFiles).not.toHaveBeenCalled();
  });

  it("blocks both ways in while disabled", () => {
    const onFiles = mount({ isDisabled: true });

    expect(screen.getByLabelText("Choose a file")).toBeDisabled();
    drop([png()]);

    expect(onFiles).not.toHaveBeenCalled();
  });

  it("blocks both ways in while pending", () => {
    const onFiles = mount({ isPending: true });

    expect(screen.getByLabelText("Choose a file")).toBeDisabled();
    drop([png()]);

    expect(onFiles).not.toHaveBeenCalled();
  });

  it("marks the tile while a file is dragged over it", () => {
    mount();
    const tile = screen.getByRole("group", { name: "Upload" });

    fireEvent.dragEnter(tile, { dataTransfer: { types: ["Files"] } });
    expect(tile).toHaveAttribute("data-drop-target", "true");
    fireEvent.dragLeave(tile, { dataTransfer: { types: ["Files"] } });
    expect(tile).not.toHaveAttribute("data-drop-target");
  });
});
