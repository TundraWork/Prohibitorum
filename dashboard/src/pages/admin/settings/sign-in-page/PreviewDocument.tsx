import {
  type ReactNode,
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";

export type PreviewTheme = "light" | "dark";

/**
 * Gives a frame's document the app's styles and the page's own body, once.
 * The sheets are copied from the console's document; a document the browser
 * replaces after loading is adopted again by the next call.
 */
function adopt(frame: HTMLIFrameElement): HTMLElement | null {
  const page = frame.contentDocument;
  if (!page) return null;
  if (page.documentElement.dataset.adopted === undefined) {
    page.documentElement.dataset.adopted = "";
    page.documentElement.lang = document.documentElement.lang;
    for (const sheet of document.head.querySelectorAll<
      HTMLLinkElement | HTMLStyleElement
    >('link[rel="stylesheet"], style')) {
      const copy = sheet.cloneNode(true) as typeof sheet;
      // A relative address must not depend on the frame's own base.
      if (copy instanceof HTMLLinkElement && sheet instanceof HTMLLinkElement)
        copy.href = sheet.href;
      page.head.appendChild(copy);
    }
    // The real page's body, so the background shows through as it does there.
    page.body.className = document.body.className;
  }
  return page.body;
}

/**
 * Draws its children into a frame of its own, laid out at `width` × `height`
 * and scaled to fit the box it sits in, centred. A frame has a viewport of its
 * own, so the page inside answers to its own width: the breakpoints that move
 * the card and the credit apply to the width chosen here, not to the
 * console's window. The frame also has its own `<html>`, which is where the
 * theme goes, so the preview shows either theme whatever the console shows.
 *
 * The frame is inert: it is a picture of the page, not a page to use.
 */
export function PreviewDocument({
  width,
  height,
  theme,
  title,
  onDocument,
  children,
}: {
  width: number;
  height: number;
  theme: PreviewTheme;
  title: string;
  /** Told the frame's document once it is ready, and `null` when it goes. */
  onDocument?: (document: Document | null) => void;
  children: ReactNode;
}) {
  const box = useRef<HTMLDivElement>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const [body, setBody] = useState<HTMLElement | null>(null);
  const [room, setRoom] = useState({
    width: width * 0.3,
    height: height * 0.3,
  });

  const take = useCallback(() => {
    if (frame.current) setBody(adopt(frame.current));
  }, []);
  useLayoutEffect(take, [take]);

  useLayoutEffect(() => {
    const element = box.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => {
      if (entry) {
        setRoom({
          width: entry.contentRect.width,
          height: entry.contentRect.height,
        });
      }
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  useLayoutEffect(() => {
    if (!body || !onDocument) return;
    onDocument(body.ownerDocument);
    return () => onDocument(null);
  }, [body, onDocument]);

  useLayoutEffect(() => {
    const root = body?.ownerDocument.documentElement;
    if (!root) return;
    root.dataset.theme = theme;
    root.classList.toggle("dark", theme === "dark");
  }, [body, theme]);

  const scale = Math.min(room.width / width, room.height / height);
  return (
    <div
      ref={box}
      className="relative aspect-[16/10] w-full overflow-hidden bg-muted"
    >
      <iframe
        ref={frame}
        data-sign-in-preview=""
        title={title}
        inert
        tabIndex={-1}
        onLoad={take}
        className="absolute top-0 border-0 bg-sidebar"
        style={{
          width,
          height,
          // A phone is drawn with rounded corners and an outline, sized
          // before the scale so they shrink with the page.
          ...(width < 800 && {
            borderRadius: 32,
            boxShadow:
              "0 0 0 3px var(--border), 0 12px 40px -12px rgb(0 0 0 / 0.35)",
          }),
          left: Math.max(0, (room.width - width * scale) / 2),
          transform: `scale(${scale})`,
          transformOrigin: "top left",
        }}
      />
      {body && createPortal(children, body)}
    </div>
  );
}
