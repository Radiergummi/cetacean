import { Label } from "./label";
import { cn } from "@/lib/utils";
import { useId, type ReactNode } from "react";

interface FieldControlProps {
  id: string;
  "aria-describedby": string | undefined;
}

interface FieldProps {
  label: ReactNode;
  /** Hint rendered under the control, announced as part of it. */
  description?: ReactNode | undefined;
  className?: string | undefined;
  labelClassName?: string | undefined;
  children: (control: FieldControlProps) => ReactNode;
}

/**
 * A labelled form control.
 *
 * The editors wrote this as a `<label>` sitting beside its control with no
 * `htmlFor`, which associates nothing: the control was announced as unlabelled,
 * and clicking the label did not focus it. The hint underneath had the same
 * problem in reverse — visible to sighted readers, absent from what a screen
 * reader announced for the field.
 *
 * Both associations come from one generated id, so a caller cannot wire up half
 * of it. The control is a render prop rather than a plain child because the id
 * has to reach whatever element actually takes focus, which only the caller
 * knows.
 */
export function Field({ label, description, className, labelClassName, children }: FieldProps) {
  const id = useId();
  const descriptionId = description == null ? undefined : `${id}-description`;

  return (
    <div className={cn("flex flex-col gap-1.5", className)}>
      <Label
        htmlFor={id}
        className={cn("text-xs font-medium text-foreground", labelClassName)}
      >
        {label}
      </Label>

      {children({ id, "aria-describedby": descriptionId })}

      {description != null && (
        <p
          id={descriptionId}
          className="text-xs text-muted-foreground"
        >
          {description}
        </p>
      )}
    </div>
  );
}
