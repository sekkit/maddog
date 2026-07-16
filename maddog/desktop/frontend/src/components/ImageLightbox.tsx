import { useEffect, useRef, useState } from "react";
import { ExternalLink, Maximize2, X } from "lucide-react";
import { Tooltip } from "./Tooltip";

export function ImageLightbox({
  src,
  alt,
  onClose,
}: {
  src: string;
  alt: string;
  onClose: () => void;
}) {
  const [fit, setFit] = useState(true);
  const [broken, setBroken] = useState(false);
  const closeRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    closeRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      document.body.style.overflow = previousOverflow;
    };
  }, [onClose]);

  const openOriginal = () => window.open(src, "_blank", "noopener,noreferrer");

  return (
    <div className="image-lightbox" role="dialog" aria-modal="true" aria-label={alt || "Image preview"}>
      <button className="image-lightbox__backdrop" type="button" aria-label="Close image preview" onClick={onClose} />
      <div className="image-lightbox__toolbar">
        <Tooltip label={fit ? "View at original size" : "Fit image to window"}>
          <button className="workspace-iconbtn" type="button" aria-label={fit ? "View at original size" : "Fit image to window"} aria-pressed={!fit} onClick={() => setFit((value) => !value)}>
            <Maximize2 size={16} />
          </button>
        </Tooltip>
        <Tooltip label="Open original image">
          <button className="workspace-iconbtn" type="button" aria-label="Open original image" onClick={openOriginal}>
            <ExternalLink size={16} />
          </button>
        </Tooltip>
        <Tooltip label="Close image preview">
          <button ref={closeRef} className="workspace-iconbtn" type="button" aria-label="Close image preview" onClick={onClose}>
            <X size={17} />
          </button>
        </Tooltip>
      </div>
      <div className={`image-lightbox__stage${fit ? " image-lightbox__stage--fit" : " image-lightbox__stage--original"}`} onClick={(event) => event.stopPropagation()}>
        {broken ? (
          <div className="image-lightbox__error" role="status">Unable to load this image.</div>
        ) : (
          <img src={src} alt={alt} decoding="async" loading="eager" onError={() => setBroken(true)} />
        )}
      </div>
    </div>
  );
}
