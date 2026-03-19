import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";

interface Props {
  keys: string[];
  children: ReactNode;
}

export default function ShortcutTooltip({ keys, children }: Props) {
  const [visible, setVisible] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const show = useCallback(() => {
    timerRef.current = setTimeout(() => setVisible(true), 500);
  }, []);

  const hide = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
    }

    timerRef.current = null;

    setVisible(false);
  }, []);

  useEffect(() => {
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, []);

  return (
    <div
      className="relative"
      onMouseEnter={show}
      onMouseLeave={hide}
    >
      {children}
      {visible && (
        <div className="pointer-events-none absolute top-full left-1/2 z-50 mt-1.5 hidden -translate-x-1/2 lg:block">
          <div className="flex items-center gap-1.5 rounded-md border bg-popover px-2 py-1 text-[11px] whitespace-nowrap text-popover-foreground shadow-md">
            {keys.map((key, index) => (
              <span
                key={index}
                className="flex items-center gap-1"
              >
                {index > 0 && <span className="text-muted-foreground">then</span>}
                <kbd className="inline-flex h-4.5 min-w-4.5 items-center justify-center rounded border bg-muted px-1 font-mono text-[10px] font-medium uppercase">
                  {key}
                </kbd>
              </span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
