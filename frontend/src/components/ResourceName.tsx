import { splitStackPrefix } from "../lib/searchConstants";

/** Renders "stack_thing" as `<muted>stack/</muted><strong>thing</strong>` */
export default function ResourceName({ name }: { name: string }) {
  const { prefix, name: rest } = splitStackPrefix(name);

  if (!prefix) {
    return <>{rest}</>;
  }

  return (
    <span className="inline-flex items-baseline">
      <span className="font-normal text-muted-foreground">{prefix}/</span>
      <strong className="font-semibold">{rest}</strong>
    </span>
  );
}
