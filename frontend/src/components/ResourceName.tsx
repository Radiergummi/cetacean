import { splitStackPrefix } from "../lib/searchConstants";

/** Renders "stack_thing" as `<muted>stack/</muted><strong>thing</strong>` */
export default function ResourceName({
  name,
  direction = "row",
}: {
  name: string;
  direction?: "row" | "column";
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

  return (
    <span className="inline-flex items-baseline">
      <span className="font-normal text-muted-foreground">{prefix}/</span>
      <strong className="font-semibold">{rest}</strong>
    </span>
  );
}
