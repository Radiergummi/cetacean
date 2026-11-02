import {
  annotationBody,
  annotationInputs,
  annotationPatch,
  type AnnotationInputs,
  parseTag,
  toLocalInput,
} from "./annotationFields";
import ResourcePicker from "./ResourcePicker";
import { api, ApiError } from "@/api/client";
import type { Annotation, AnnotationScope } from "@/api/types";
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
 * A note as it was read, with the version a write must match.
 */
export interface EditedNote {
  note: Annotation;
  etag: string | undefined;
}

type Target =
  | {
      scopes: AnnotationScope[];
      pickResource?: boolean | undefined;
      initial: { from: number; to?: number | undefined };
      editing?: undefined;
    }
  | { editing: EditedNote; scopes?: undefined; pickResource?: undefined; initial?: undefined };

/**
 * The form's starting values: the edited note's, or a new one at `initial`.
 * The inputs hold minutes, so a range inside one minute starts as a point.
 */
function startingInputs({ editing, initial }: Target) {
  if (editing) {
    return annotationInputs(editing.note);
  }

  const { from, to } = initial;

  return {
    text: "",
    tags: [],
    from: toLocalInput(from),
    to: to === undefined || Math.floor(to / 60) <= Math.floor(from / 60) ? "" : toLocalInput(to),
  };
}

/**
 * Writes one annotation. `scopes` are the fixed choices, the first chosen;
 * `pickResource` adds a search for any service, stack or node. With `editing`
 * it changes that note instead, its scope fixed. Notes arrive through the
 * annotation event, so success only calls `onDone`.
 */
export default function AnnotationForm({ onDone, ...target }: Target & { onDone: () => void }) {
  const { scopes = [], pickResource = false } = target;
  const [start] = useState(() => startingInputs(target));
  const [text, setText] = useState(start.text);
  const [tags, setTags] = useState<string[]>(start.tags);
  const [tagInput, setTagInput] = useState("");
  const [from, setFrom] = useState(start.from);
  const [to, setTo] = useState(start.to);
  const [edited, setEdited] = useState(target.editing);
  const [choice, setChoice] = useState(0);
  const [picked, setPicked] = useState<ResourceScope | null>(null);
  const [error, setError] = useState<{ message: string; field?: "tags" | "until" } | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const submittingRef = useRef(false);
  const scopeName = useId();
  const errorId = useId();
  const pickerChoice = scopes.length;
  const scope = edited ? "cluster" : choice === pickerChoice ? picked : scopes[choice];

  /**
   * A control's ARIA, naming the error when it is about this field.
   */
  const invalidAs = (field: "tags" | "until", describedBy: string | undefined) =>
    error?.field === field
      ? {
          "aria-invalid": true,
          "aria-describedby": [describedBy, errorId].filter(Boolean).join(" "),
        }
      : { "aria-describedby": describedBy };

  /**
   * The tags with what is still typed added, or null after saying why not.
   */
  const withTyped = (): string[] | null => {
    if (tagInput.trim() === "") {
      return tags;
    }

    const tag = parseTag(tagInput);

    if (tag === null || (tags.length >= maximumTags && !tags.includes(tag))) {
      setError({
        field: "tags",
        message:
          tag === null
            ? "A tag takes lowercase letters, digits, dots, dashes and underscores, up to 40."
            : `A note takes at most ${maximumTags} tags.`,
      });

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

  /**
   * Takes up the note's current version after a stale write: a field the user
   * changed keeps their input, any other shows the newer value.
   */
  const reload = async (opened: Annotation, inputs: AnnotationInputs) => {
    try {
      const { data, etag } = await api.annotation(opened.id);
      const was = annotationInputs(opened);
      const now = annotationInputs(data);

      if (inputs.text === was.text) {
        setText(now.text);
      }

      if (inputs.tags.join(",") === was.tags.join(",")) {
        setTags(now.tags);
      }

      if (inputs.from === was.from) {
        setFrom(now.from);
      }

      if (inputs.to === was.to) {
        setTo(now.to);
      }

      setEdited({ note: data, etag });
      setError({
        message:
          "This note changed since you opened it. Your edits are kept; save again to apply them.",
      });
    } catch (failure) {
      setError({
        message: failure instanceof ApiError ? failure.detail || failure.message : String(failure),
      });
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

    const shown = edited && annotationInputs(edited.note);

    // Times shown to the minute may meet; untouched, the server's seconds hold.
    if (
      (!shown || from !== shown.from || to !== shown.to) &&
      to !== "" &&
      new Date(to) <= new Date(from)
    ) {
      setError({ field: "until", message: "The end must be after the start." });

      return;
    }

    submittingRef.current = true;
    setSubmitting(true);
    setError(null);

    try {
      if (edited) {
        const patch = annotationPatch(edited.note, { text, tags: finalTags, from, to });

        if (Object.keys(patch).length > 0) {
          await api.updateAnnotation(edited.note.id, patch, edited.etag);
        }
      } else {
        await api.createAnnotation(annotationBody({ text, tags: finalTags, from, to, scope }));
      }

      onDone();
    } catch (failure) {
      if (edited && failure instanceof ApiError && failure.status === 412) {
        await reload(edited.note, { text, tags: finalTags, from, to });
      } else {
        setError({
          message:
            failure instanceof ApiError
              ? failure.detail || failure.message
              : "The note could not be saved.",
        });
      }
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
              id={control.id}
              {...invalidAs("tags", control["aria-describedby"])}
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
              id={control.id}
              {...invalidAs("until", control["aria-describedby"])}
              type="datetime-local"
              value={to}
              onChange={(event) => setTo(event.target.value)}
            />
          )}
        </Field>
      </div>

      {edited ? (
        <div className="flex flex-col gap-1.5">
          <p className="text-xs font-medium">About</p>
          <p className="flex flex-wrap gap-x-2 text-sm">
            {edited.note.resources?.length
              ? edited.note.resources.map(({ type, id, name }) => (
                  <ResourceName
                    key={`${type}-${id}`}
                    name={name}
                  />
                ))
              : "Cluster-wide"}
          </p>
        </div>
      ) : (
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
      )}

      {error && (
        <p
          id={errorId}
          role="alert"
          className="text-sm text-status-danger"
        >
          {error.message}
        </p>
      )}

      <div className="flex justify-end">
        <Button
          type="submit"
          size="sm"
          disabled={submitting || text.trim() === "" || !scope}
        >
          {submitting ? "Saving…" : edited ? "Save" : "Add annotation"}
        </Button>
      </div>
    </form>
  );
}
