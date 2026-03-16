export default function SectionHeader({ title }: { title: string }) {
  return (
    <h2 className="mb-3 text-sm font-medium tracking-wider text-muted-foreground uppercase">
      {title}
    </h2>
  );
}
