import { splitStackPrefix } from "../lib/searchConstants";

/** Renders "stack_thing" as `<muted>stack/</muted><strong>thing</strong>` */
export default function ResourceName({
  name,
  direction = "row",
}: {
  name: string;
  direction?: "row" | "column" | "responsive";
}) {
  const { prefix, name: rest } = splitStackPrefix(name);

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
      <span className="inline-flex items-baseline md:items-baseline flex-col md:flex-row leading-tight md:leading-normal">
        <span className="text-[0.5em] md:text-[1em] font-normal text-muted-foreground">
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
