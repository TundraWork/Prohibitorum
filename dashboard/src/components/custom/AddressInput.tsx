import { Textarea, type TextareaProps } from "@/components/ui/textarea";

/**
 * A one-line box for an address that grows when the address is longer than the
 * box, so on a narrow screen the end of the address, the part that tells two
 * apart, is never hidden. It is monospace because the address has to match
 * another system's copy exactly, and a stray space or a look-alike letter has to
 * show.
 *
 * Enter submits the form as it does in a single-line input, and a line break
 * cannot be typed or pasted into the value.
 */
export function AddressInput({
  value,
  onValueChange,
  ...props
}: Omit<TextareaProps, "value" | "onChange" | "rows"> & {
  value: string;
  onValueChange: (value: string) => void;
}) {
  return (
    <Textarea
      className="**:data-[slot=textarea]:min-h-0! **:data-[slot=textarea]:resize-none **:data-[slot=textarea]:font-mono **:data-[slot=textarea]:text-base sm:**:data-[slot=textarea]:text-[13px] **:data-[slot=textarea]:leading-5 **:data-[slot=textarea]:[overflow-wrap:anywhere] max-sm:**:data-[slot=textarea]:py-[7px]"
      rows={1}
      inputMode="url"
      autoComplete="off"
      spellCheck={false}
      {...props}
      value={value}
      onKeyDown={(event) => {
        props.onKeyDown?.(event);
        // Enter confirms an IME candidate while composing; it is not a submit.
        if (event.key !== "Enter" || event.nativeEvent.isComposing) return;
        event.preventDefault();
        event.currentTarget.form?.requestSubmit();
      }}
      onChange={(event) =>
        onValueChange(event.target.value.replace(/[\r\n]/g, ""))
      }
    />
  );
}
