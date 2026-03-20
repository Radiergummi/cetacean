/**
 * Quote-aware command string parser. Splits a string into an argument
 * array respecting single and double quotes, similar to shell parsing.
 */
export function parseCommand(input: string): string[] {
  const args: string[] = [];
  let current = "";
  let quote: string | null = null;
  let escape = false;

  for (const char of input) {
    if (escape) {
      current += char;
      escape = false;
      continue;
    }

    if (char === "\\" && quote === '"') {
      escape = true;
      continue;
    }

    if (char === quote) {
      quote = null;
      continue;
    }

    if (!quote && (char === '"' || char === "'")) {
      quote = char;
      continue;
    }

    if (!quote && char === " ") {
      if (current) {
        args.push(current);
        current = "";
      }
      continue;
    }

    current += char;
  }

  if (current) {
    args.push(current);
  }

  return args;
}

/**
 * Joins an argument array into a command string, quoting args that
 * contain spaces with double quotes.
 */
export function joinCommand(args: string[]): string {
  return args
    .map((arg) => {
      if (!arg.includes(" ") && !arg.includes('"')) {
        return arg;
      }

      return `"${arg.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
    })
    .join(" ");
}
