export default function SectionHeader({ title }: { title: string }) {
  return (
    <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
      {title}
    </h2>
  );
}
