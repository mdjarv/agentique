import { Popover as PopoverPrimitive } from "radix-ui";
import type * as React from "react";

import { useInModalLayer } from "~/components/ui/modal-layer";
import { cn } from "~/lib/utils";

function Popover({ modal, ...props }: React.ComponentProps<typeof PopoverPrimitive.Root>) {
  // Inside a Sheet or Dialog the popover has to own the scroll lock, or its
  // list cannot be scrolled by touch — see `modal-layer.tsx`. An explicit
  // `modal` from the caller still wins.
  const inModalLayer = useInModalLayer();
  return <PopoverPrimitive.Root data-slot="popover" modal={modal ?? inModalLayer} {...props} />;
}

function PopoverTrigger({ ...props }: React.ComponentProps<typeof PopoverPrimitive.Trigger>) {
  return <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />;
}

function PopoverContent({
  className,
  align = "start",
  sideOffset = 4,
  ...props
}: React.ComponentProps<typeof PopoverPrimitive.Content>) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Content
        data-slot="popover-content"
        align={align}
        sideOffset={sideOffset}
        className={cn(
          "z-50 w-72 origin-(--radix-popover-content-transform-origin) rounded-md border bg-popover p-4 text-popover-foreground shadow-md outline-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2",
          className,
        )}
        {...props}
      />
    </PopoverPrimitive.Portal>
  );
}

export { Popover, PopoverContent, PopoverTrigger };
