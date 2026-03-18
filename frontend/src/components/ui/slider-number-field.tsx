import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import { NumberField } from "@base-ui/react/number-field";
import { Minus, Plus } from "lucide-react";

interface SliderNumberFieldProps {
  value: number | undefined;
  onChange: (value: number | undefined) => void;
  min?: number;
  max?: number;
  step?: number;
  label: string;
}

export function SliderNumberField({
  value,
  onChange,
  min = 0,
  max,
  step,
  label,
}: SliderNumberFieldProps) {
  return (
    <div className="space-y-2">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <div className="flex items-center gap-3">
        <NumberField.Root
          value={value ?? null}
          onValueChange={(val) => onChange(val ?? undefined)}
          min={min}
          max={max}
          step={step}
        >
          <NumberField.Group className="flex items-center rounded-md border">
            <NumberField.Decrement className="flex size-8 items-center justify-center border-r text-muted-foreground hover:bg-accent disabled:opacity-50">
              <Minus className="size-3" />
            </NumberField.Decrement>
            <NumberField.Input className="w-20 bg-transparent px-2 py-1 text-center font-mono text-sm focus:outline-none" />
            <NumberField.Increment className="flex size-8 items-center justify-center border-l text-muted-foreground hover:bg-accent disabled:opacity-50">
              <Plus className="size-3" />
            </NumberField.Increment>
          </NumberField.Group>
        </NumberField.Root>
        {max !== undefined && (
          <Slider
            value={[value ?? min]}
            onValueChange={(val) => {
              const numbers = val as readonly number[];
              onChange(numbers[0]);
            }}
            min={min}
            max={max}
            step={step}
            className="flex-1"
          />
        )}
      </div>
    </div>
  );
}
