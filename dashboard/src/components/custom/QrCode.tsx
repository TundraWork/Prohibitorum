import QRCode from "qrcode";
import { type ReactNode, useEffect, useRef, useState } from "react";

/**
 * `value` drawn as a QR code on a canvas, for a phone to scan: an
 * authenticator's setup URI, or the address that approves a pairing.
 *
 * The canvas stays mounted across failures, so a later `value` can be drawn
 * on it the moment it changes. Unmounting it, or skipping an empty value that
 * a first render passes while its data is on the way, would leave the ref
 * null and the canvas blank for good. A value that cannot be drawn hides the
 * canvas and shows `failure` in its place.
 */
export function QrCode({
  value,
  label,
  size = 240,
  failure,
}: {
  value: string;
  label: string;
  /** The drawn width and height in pixels, quiet zone included. */
  size?: number;
  failure?: ReactNode;
}) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let active = true;
    setFailed(false);
    const element = canvas.current;
    if (value === "" || !element) return;
    void QRCode.toCanvas(element, value, { width: size, margin: 1 }).catch(
      () => {
        if (active) setFailed(true);
      },
    );
    return () => {
      active = false;
    };
  }, [value, size]);

  return (
    <>
      {/* A white tile with its own quiet zone, so the code scans in either theme. */}
      <div
        className={
          failed
            ? "hidden"
            : "max-w-full self-center rounded-xl bg-white p-2.5 shadow-xs/5 ring-1 ring-border"
        }
      >
        <canvas
          ref={canvas}
          className="block max-w-full rounded-sm"
          role="img"
          aria-label={label}
        />
      </div>
      {failed && failure}
    </>
  );
}
