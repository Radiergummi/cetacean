"use client";

import { cn } from "@/lib/utils";
import type { ComponentProps } from "react";

function Label({ className, ...props }: ComponentProps<"label">) {
  return (
    // This is the primitive, not a use of it: `htmlFor` arrives through
    // `...props` from whichever field renders it, which the rule cannot see.
    // oxlint-disable-next-line jsx-a11y/label-has-associated-control
    <label
      data-slot="label"
      className={cn(
        "flex items-center gap-2 text-sm leading-none font-medium select-none " +
          "group-data-[disabled=true]:pointer-events-none group-data-[disabled=true]:opacity-50 " +
          "peer-disabled:cursor-not-allowed peer-disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}

export { Label };
