import { NumberField } from "@/components/ui/number-field";
import { bestDurationUnit, durationUnits } from "@/lib/duration";
import { useEffect, useState } from "react";

interface DurationInputProps {
  value: number;
  onChange: (nanoseconds: number) => void;
  disabled?: boolean;
}

export function DurationInput({ value, onChange, disabled }: DurationInputProps) {
  const [unit, setUnit] = useState(() => bestDurationUnit(value));
  const displayValue = value === 0 ? 0 : value / unit.factor;

  useEffect(() => {
    setUnit(bestDurationUnit(value));
  }, [value]);

  return (
    <div className="flex items-end gap-2">
      <div className="flex-1">
        <NumberField
          value={displayValue || undefined}
          onChange={(next) => {
            onChange((next ?? 0) * unit.factor);
          }}
          min={0}
          step={1}
          label=""
        />
      </div>
      <select
        value={unit.label}
        onChange={(event) => {
          const next =
            durationUnits.find(({ label }) => label === event.target.value) ?? durationUnits[0];
          setUnit(next);
          const currentNumber = value / unit.factor;
          onChange(currentNumber * next.factor);
        }}
        disabled={disabled}
        className="flex h-8 rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {durationUnits.map(({ label }) => (
          <option
            key={label}
            value={label}
          >
            {label}
          </option>
        ))}
      </select>
    </div>
  );
}
