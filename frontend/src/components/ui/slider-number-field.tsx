import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import { NumberField } from "@base-ui/react/number-field";
import { Minus, Plus } from "lucide-react";
import { useState } from "react";

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
  // Track the slider value separately so that clearing the number input
  // (which sends undefined) doesn't snap the slider back to min.
  const [sliderValue, setSliderValue] = useState(value ?? min);

  function handleNumberChange(next: number | null) {
    const resolved = next ?? undefined;
    onChange(resolved);
    if (resolved !== undefined) {
      setSliderValue(resolved);
    }
  }

  function handleSliderChange(next: number | readonly number[]) {
    const resolved = Array.isArray(next) ? next[0] : next;
    onChange(resolved);
    setSliderValue(resolved);
  }

  return (
    <div className="space-y-2">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <div className="flex items-center gap-3">
        <NumberField.Root
          value={value ?? null}
          onValueChange={handleNumberChange}
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
            value={sliderValue}
            onValueChange={handleSliderChange}
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
