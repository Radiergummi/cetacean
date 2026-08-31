import { stripStackPrefix } from "../lib/parseStackLabels";
import { splitStackPrefix } from "../lib/searchConstants";

interface ResourceNameProps {
  name: string;
  /**
   * The com.docker.stack.namespace label, when the caller holds it: the stack
   * name for a resource in one, null for a resource in none. Supplying it
   * settles the split instead of guessing at the first underscore — an
   * underscore in an unstacked name is just an underscore, and a resource can
   * join a stack by label without being named for it.
   *
   * Leave it out only where no labels are in hand, such as search results and
   * chart series, where the guess is the best available answer.
   */
  stack?: string | null | undefined;
  direction?: "row" | "column" | "responsive" | undefined;
}

/**
 * Splits a name into its stack prefix and the rest, from the label when the
 * caller has it and from the first underscore when it does not. The prefix is
 * only ever the part the name actually carries, so a resource that joined its
 * stack by label alone renders under its own bare name.
 */
function splitName(name: string, stack: string | null | undefined) {
  if (stack === undefined) {
    return splitStackPrefix(name);
  }

  const rest = stripStackPrefix(name, stack ?? undefined);

  return { prefix: rest === name ? null : stack, name: rest };
}

/** Renders "stack_thing" as `<muted>stack/</muted><strong>thing</strong>` */
export default function ResourceName({ name, stack, direction = "row" }: ResourceNameProps) {
  const { prefix, name: rest } = splitName(name, stack);

  if (!prefix) {
    return <>{rest}</>;
  }

  if (direction === "column") {
    return (
      <span className="flex flex-col leading-tight">
        <span className="text-[0.5em] font-normal text-muted-foreground">{prefix}</span>
        <strong className="font-semibold">{rest}</strong>
      </span>
    );
  }

  if (direction === "responsive") {
    return (
      <span className="inline-flex flex-col items-baseline leading-tight md:flex-row md:items-baseline md:leading-normal">
        <span className="text-[0.5em] font-normal text-muted-foreground md:text-[1em]">
          <span className="md:hidden">{prefix}</span>
          <span className="hidden md:inline">{prefix}/</span>
        </span>
        <strong className="font-semibold">{rest}</strong>
      </span>
    );
  }

  return (
    <span className="inline-flex items-baseline">
      <span className="font-normal text-muted-foreground">{prefix}/</span>
      <strong className="font-semibold">{rest}</strong>
    </span>
  );
}
