import { type ChangeEvent, useCallback, useState } from "react";
import { api } from "../api/client";
import CreateResourceDialog from "./CreateResourceDialog";
import { Input } from "./ui/input";
import { Label } from "./ui/label";

type InputMode = "text" | "file";

export default function CreateConfigForm() {
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

  const handleSubmit = useCallback(async () => {
    const encoded = inputMode === "text" ? btoa(text) : fileData!;
    const response = await api.createConfig(name.trim(), encoded);
    return `/configs/${response.config.ID}`;
  }, [name, text, fileData, inputMode]);

  function reset() {
    setName("");
    setText("");
    setFileData(null);
    setInputMode("text");
  }

  return (
    <CreateResourceDialog
      resourceType="Config"
      onSubmit={handleSubmit}
      canSubmit={canSubmit}
      onReset={reset}
    >
      <div className="flex flex-col gap-4 py-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor="config-name">Name</Label>
          <Input
            id="config-name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="my-config"
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
              placeholder="Paste config content…"
              rows={8}
              className="w-full rounded-lg border border-input bg-transparent px-2.5 py-2 font-mono text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:border-ring focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
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
