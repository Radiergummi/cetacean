/**
 * Returns the target of the first link in an RFC 8288 `Link` header whose
 * relation types include `relation`, or undefined when none does.
 *
 * Parameter names and relation types compare case-insensitively, `rel` may
 * list several space-separated types, and a quoted value may hold `,` or `;`.
 */
export function linkTarget(header: string | null, relation: string): string | undefined {
  if (!header) {
    return undefined;
  }

  const wanted = relation.toLowerCase();
  let position = 0;

  while (position < header.length) {
    const open = header.indexOf("<", position);

    if (open === -1) {
      return undefined;
    }

    const close = header.indexOf(">", open);

    if (close === -1) {
      return undefined;
    }

    const target = header.slice(open + 1, close);
    const { parameters, end } = readParameters(header, close + 1);
    position = end;

    const relationTypes = (parameters.get("rel") ?? "").toLowerCase().split(/\s+/);

    if (relationTypes.includes(wanted)) {
      return target;
    }
  }

  return undefined;
}

/**
 * Reads `; name=value` pairs from `start` up to the comma ending the link. The
 * first occurrence of a parameter wins, as RFC 8288 §3.3 requires of `rel`.
 */
function readParameters(header: string, start: number) {
  const parameters = new Map<string, string>();
  let position = start;

  while (position < header.length) {
    const character = header[position];

    if (character === ",") {
      return { parameters, end: position + 1 };
    }

    if (character !== ";") {
      position += 1;
      continue;
    }

    const nameMatch = /^\s*([^\s=;,]+)\s*/.exec(header.slice(position + 1));

    if (!nameMatch) {
      position += 1;
      continue;
    }

    const name = (nameMatch[1] ?? "").toLowerCase();
    position += 1 + nameMatch[0].length;

    let value = "";

    if (header[position] === "=") {
      position += 1;

      while (header[position] === " " || header[position] === "\t") {
        position += 1;
      }

      if (header[position] === '"') {
        position += 1;

        while (position < header.length && header[position] !== '"') {
          if (header[position] === "\\") {
            position += 1;
          }

          value += header[position] ?? "";
          position += 1;
        }

        position += 1;
      } else {
        value = /^[^\s;,]*/.exec(header.slice(position))?.[0] ?? "";
        position += value.length;
      }
    }

    if (!parameters.has(name)) {
      parameters.set(name, value);
    }
  }

  return { parameters, end: position };
}
