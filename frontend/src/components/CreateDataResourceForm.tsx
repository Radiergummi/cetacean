import CreateResourceDialog from "./CreateResourceDialog";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { type ChangeEvent, useCallback, useState } from "react";

type InputMode = "text" | "file";

interface CreateDataResourceFormProps {
  resourceType: string;
  onCreate: (name: string, data: string) => Promise<{ id: string }>;
  basePath: string;
}

/**
 * Form for creating a name+data resource (config or secret).
 * Handles name input, text/file data toggle, and base64 encoding.
 */
export default function CreateDataResourceForm({
  resourceType,
  onCreate,
  basePath,
}: CreateDataResourceFormProps) {
  const [name, setName] = useState("");
  const [text, setText] = useState("");
  const [fileData, setFileData] = useState<string | null>(null);
  const [inputMode, setInputMode] = useState<InputMode>("text");

  const data = inputMode === "text" ? text : fileData;
  const canSubmit = name.trim().length > 0 && data != null && data.length > 0;

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];

    if (!file) {
      setFileData(null);
      return;
    }

    const reader = new FileReader();

    reader.onload = () => {
      const result = reader.result as string;
      const base64 = result.includes(",") ? result.split(",")[1] : result;
      setFileData(base64);
    };

    reader.readAsDataURL(file);
  }

  const lowerType = resourceType.toLowerCase();

  const handleSubmit = useCallback(async () => {
    const encoded = inputMode === "file" ? fileData! : btoa(text);
    const result = await onCreate(name.trim(), encoded);
    return `${basePath}/${result.id}`;
  }, [name, text, fileData, inputMode, onCreate, basePath]);

  function reset() {
    setName("");
    setText("");
    setFileData(null);
    setInputMode("text");
  }

  return (
    <CreateResourceDialog
      resourceType={resourceType}
      onSubmit={handleSubmit}
      canSubmit={canSubmit}
      onReset={reset}
    >
      <div className="flex flex-col gap-4 py-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor={`${lowerType}-name`}>Name</Label>
          <Input
            id={`${lowerType}-name`}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={`my-${lowerType}`}
            autoFocus
          />
        </div>

        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <Label>Data</Label>
            <div className="flex gap-1 text-xs">
              <button
                type="button"
                className={`rounded px-2 py-0.5 ${inputMode === "text" ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setInputMode("text")}
              >
                Text
              </button>
              <button
                type="button"
                className={`rounded px-2 py-0.5 ${inputMode === "file" ? "bg-muted font-medium" : "text-muted-foreground hover:text-foreground"}`}
                onClick={() => setInputMode("file")}
              >
                File
              </button>
            </div>
          </div>

          {inputMode === "text" ? (
            <textarea
              value={text}
              onChange={(event) => setText(event.target.value)}
              placeholder={`Paste ${lowerType} content…`}
              rows={8}
              className="w-full rounded-lg border border-input bg-transparent px-2.5 py-2 font-mono text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
            />
          ) : (
            <Input
              type="file"
              onChange={handleFileChange}
            />
          )}
        </div>
      </div>
    </CreateResourceDialog>
  );
}
