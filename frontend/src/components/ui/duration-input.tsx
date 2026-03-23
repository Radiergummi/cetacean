import { Input } from "@/components/ui/input";
import { useState } from "react";

const units = [
  { label: "seconds", factor: 1_000_000_000 },
  { label: "minutes", factor: 60_000_000_000 },
  { label: "hours", factor: 3_600_000_000_000 },
  { label: "days", factor: 86_400_000_000_000 },
] as const;

interface DurationInputProps {
  value: number;
  onChange: (nanoseconds: number) => void;
  disabled?: boolean;
}

function bestUnit(nanoseconds: number): (typeof units)[number] {
  for (let index = units.length - 1; index > 0; index--) {
    if (nanoseconds >= units[index].factor && nanoseconds % units[index].factor === 0) {
      return units[index];
    }
  }

  return units[0];
}

export function DurationInput({ value, onChange, disabled }: DurationInputProps) {
  const initial = bestUnit(value);
  const [unit, setUnit] = useState(initial);
  const displayValue = value === 0 ? 0 : value / unit.factor;

  return (
    <div className="flex gap-2">
      <Input
        type="number"
        min={0}
        value={displayValue}
        onChange={(event) => {
          const number = Number(event.target.value) || 0;
          onChange(number * unit.factor);
        }}
        disabled={disabled}
        className="flex-1"
      />
      <select
        value={unit.label}
        onChange={(event) => {
          const next = units.find(({ label }) => label === event.target.value) ?? units[0];
          setUnit(next);
          const currentNumber = value / unit.factor;
          onChange(currentNumber * next.factor);
        }}
        disabled={disabled}
        className="flex h-8 rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        {units.map(({ label }) => (
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
