export default function SearchInput({ value, onChange, placeholder }: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
}) {
  return (
    <input
      type="text"
      value={value}
      onChange={e => onChange(e.target.value)}
      placeholder={placeholder || 'Search...'}
      className="w-full max-w-sm px-3 py-2 border rounded-md text-sm mb-4 focus:outline-none focus:ring-2 focus:ring-primary"
    />
  )
}
