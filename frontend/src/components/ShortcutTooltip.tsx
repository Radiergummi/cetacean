import { useState, useRef, useCallback, type ReactNode } from "react";

interface Props {
  keys: string[];
  label?: string;
  children: ReactNode;
}

export default function ShortcutTooltip({ keys, label, children }: Props) {
  const [visible, setVisible] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const show = useCallback(() => {
    timerRef.current = setTimeout(() => setVisible(true), 500);
  }, []);

  const hide = useCallback(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = null;
    setVisible(false);
  }, []);

  return (
    <div className="relative" onMouseEnter={show} onMouseLeave={hide}>
      {children}
      {visible && (
        <div className="absolute left-1/2 -translate-x-1/2 top-full mt-1.5 z-50 pointer-events-none">
          <div className="flex items-center gap-1.5 whitespace-nowrap rounded-md border bg-popover px-2 py-1 text-[11px] text-popover-foreground shadow-md">
            {label && <span>{label}</span>}
            {keys.map((key, i) => (
              <span key={i} className="flex items-center gap-1">
                {i > 0 && <span className="text-muted-foreground">then</span>}
                <kbd className="inline-flex items-center justify-center min-w-[18px] h-[18px] rounded border bg-muted px-1 font-mono text-[10px] font-medium uppercase">
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
