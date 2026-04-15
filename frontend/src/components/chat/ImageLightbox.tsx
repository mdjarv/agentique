import { createPortal } from "react-dom";

export function ImageLightbox({ src, onClose }: { src: string | null; onClose: () => void }) {
  if (!src) return null;
  return createPortal(
    <dialog
      open
      className="fixed inset-0 z-50 bg-black/80 flex items-center justify-center cursor-pointer m-0 p-0 border-none max-w-none max-h-none w-screen h-screen"
      onClick={onClose}
      onKeyDown={(e) => {
        if (e.key === "Escape") onClose();
      }}
      aria-label="Image preview"
    >
      <img
        src={src}
        alt="Full-size preview"
        className="max-h-[90vh] max-w-[90vw] object-contain rounded-lg"
      />
    </dialog>,
    document.body,
  );
}
