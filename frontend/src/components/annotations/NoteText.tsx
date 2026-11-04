import { noteSegments } from "./annotationFields";

/**
 * A note's text, plain apart from its URLs, which open in a new tab.
 */
export default function NoteText({ text }: { text: string }) {
  return noteSegments(text).map(({ text: run, url }, index) =>
    url ? (
      <a
        key={index}
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        className="text-link underline-offset-2 hover:underline"
      >
        {run}
      </a>
    ) : (
      run
    ),
  );
}
