import { annotationBody, parseTag, toLocalInput } from "./annotationFields";
import ResourcePicker from "./ResourcePicker";
import { api, ApiError } from "@/api/client";
import type { AnnotationScope } from "@/api/types";
import ResourceName from "@/components/ResourceName";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { X } from "lucide-react";
import { useId, useRef, useState, type KeyboardEvent } from "react";

type ResourceScope = Exclude<AnnotationScope, "cluster">;

const maximumTags = 10;

function scopeLabel(scope: AnnotationScope) {
  return scope === "cluster" ? "Cluster-wide" : <ResourceName name={scope.name} />;
}

/**
 * Writes one annotation. `scopes` are the fixed choices, the first chosen;
 * `pickResource` adds a search for any service, stack or node. The new note
 * arrives through the annotation event, so success only calls `onDone`.
 */
export default function AnnotationForm({
  scopes,
  pickResource = false,
  initial,
  onDone,
}: {
  scopes: AnnotationScope[];
  pickResource?: boolean | undefined;
  initial: { from: number; to?: number | undefined };
  onDone: () => void;
}) {
  const [text, setText] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [tagInput, setTagInput] = useState("");
  const [from, setFrom] = useState(() => toLocalInput(initial.from));
  // The inputs hold minutes, so a range inside one minute would arrive with
  // its end on its start; it starts as a point instead.
  const [to, setTo] = useState(() =>
    initial.to === undefined || Math.floor(initial.to / 60) <= Math.floor(initial.from / 60)
      ? ""
      : toLocalInput(initial.to),
  );
  const [choice, setChoice] = useState(0);
  const [picked, setPicked] = useState<ResourceScope | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const scopeName = useId();
  const pickerChoice = scopes.length;
  const scope = choice === pickerChoice ? picked : scopes[choice];

  /**
   * The tags with what is still typed added, or null after saying why not.
   */
  const withTyped = (): string[] | null => {
    if (tagInput.trim() === "") {
      return tags;
    }

    const tag = parseTag(tagInput);

    if (tag === null || (tags.length >= maximumTags && !tags.includes(tag))) {
      setError(
        tag === null
          ? "A tag takes lowercase letters, digits, dots, dashes and underscores, up to 40."
          : `A note takes at most ${maximumTags} tags.`,
      );

      return null;
    }

    return tags.includes(tag) ? tags : [...tags, tag];
  };

  const commitTag = () => {
    const next = withTyped();

    if (next) {
      setTags(next);
      setTagInput("");
      setError(null);
    }
  };

  const onTagKey = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Enter" || event.key === "," || event.key === " ") {
      event.preventDefault();
      commitTag();

      return;
    }

    if (event.key === "Backspace" && tagInput === "") {
      setTags((current) => current.slice(0, -1));
    }
  };

  const submit = async () => {
    if (submittingRef.current || !scope || text.trim() === "") {
      return;
    }

    const finalTags = withTyped();

    if (!finalTags) {
      return;
    }

    if (to !== "" && new Date(to) <= new Date(from)) {
      setError("The end must be after the start.");

      return;
    }

    submittingRef.current = true;
    setSubmitting(true);
    setError(null);

    try {
      await api.createAnnotation(annotationBody({ text, tags: finalTags, from, to, scope }));
      onDone();
    } catch (failure) {
      setError(
        failure instanceof ApiError
          ? failure.detail || failure.message
          : "The note could not be saved.",
      );
    } finally {
      submittingRef.current = false;
      setSubmitting(false);
    }
  };

  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <Field label="Note">
        {(control) => (
          <textarea
            {...control}
            value={text}
            onChange={(event) => setText(event.target.value)}
            maxLength={1000}
            rows={3}
            required
            className="w-full rounded-lg border border-input bg-transparent px-2.5 py-2 text-sm placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
          />
        )}
      </Field>

      <Field
        label="Tags"
        description="Enter, a comma or a space adds one."
      >
        {(control) => (
          <div className="flex flex-wrap items-center gap-1 rounded-lg border border-input px-1.5 py-1">
            {tags.map((tag, index) => (
              <span
                key={`${index}-${tag}`}
                className="flex items-center gap-0.5 rounded border px-1 text-xs text-muted-foreground"
              >
                #{tag}
                <button
                  type="button"
                  aria-label={`Remove ${tag}`}
                  onClick={() => setTags((current) => current.filter((_, at) => at !== index))}
                >
                  <X className="size-3" />
                </button>
              </span>
            ))}

            <input
              {...control}
              value={tagInput}
              onChange={(event) => setTagInput(event.target.value)}
              onKeyDown={onTagKey}
              onBlur={commitTag}
              className="min-w-24 flex-1 bg-transparent px-1 text-sm outline-none"
            />
          </div>
        )}
      </Field>

      <div className="grid grid-cols-2 gap-2">
        <Field label="From">
          {(control) => (
            <Input
              {...control}
              type="datetime-local"
              value={from}
              onChange={(event) => setFrom(event.target.value)}
              required
            />
          )}
        </Field>

        <Field
          label="Until"
          description="Optional; makes it a range."
        >
          {(control) => (
            <Input
              {...control}
              type="datetime-local"
              value={to}
              onChange={(event) => setTo(event.target.value)}
            />
          )}
        </Field>
      </div>

      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1.5 text-xs font-medium">About</legend>

        {scopes.map((option, index) => (
          <label
            key={option === "cluster" ? "cluster" : `${option.type}-${option.id}`}
            className="flex items-center gap-2 text-sm"
          >
            <input
              type="radio"
              name={scopeName}
              checked={choice === index}
              onChange={() => setChoice(index)}
            />
            {scopeLabel(option)}
          </label>
        ))}

        {pickResource && (
          <>
            {scopes.length > 0 && (
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="radio"
                  name={scopeName}
                  checked={choice === pickerChoice}
                  onChange={() => setChoice(pickerChoice)}
                />
                A service, stack or node
              </label>
            )}

            {choice === pickerChoice && (
              <ResourcePicker
                value={picked}
                onChange={setPicked}
              />
            )}
          </>
        )}
      </fieldset>

      {error && (
        <p
          role="alert"
          className="text-sm text-status-danger"
        >
          {error}
        </p>
      )}

      <div className="flex justify-end">
        <Button
          type="submit"
          size="sm"
          disabled={submitting || text.trim() === "" || !scope}
        >
          {submitting ? "Saving…" : "Add annotation"}
        </Button>
      </div>
    </form>
  );
}
