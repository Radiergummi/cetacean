import { parseCommand, joinCommand } from "./parseCommand";
import { describe, it, expect } from "vitest";

describe("parseCommand", () => {
  it("splits simple command", () => {
    expect(parseCommand("curl -f http://localhost/")).toEqual(["curl", "-f", "http://localhost/"]);
  });

  it("handles double quotes", () => {
    expect(parseCommand('/bin/sh -c "echo hello world"')).toEqual([
      "/bin/sh",
      "-c",
      "echo hello world",
    ]);
  });

  it("handles single quotes", () => {
    expect(parseCommand("echo 'hello world'")).toEqual(["echo", "hello world"]);
  });

  it("handles empty string", () => {
    expect(parseCommand("")).toEqual([]);
  });

  it("handles whitespace-only", () => {
    expect(parseCommand("   ")).toEqual([]);
  });

  it("preserves escaped quotes", () => {
    expect(parseCommand('echo "say \\"hello\\""')).toEqual(["echo", 'say "hello"']);
  });

  it("handles mixed quotes", () => {
    expect(parseCommand(`cmd --flag="value" --other='val'`)).toEqual([
      "cmd",
      "--flag=value",
      "--other=val",
    ]);
  });
});

describe("joinCommand", () => {
  it("joins simple args", () => {
    expect(joinCommand(["curl", "-f", "http://localhost/"])).toBe("curl -f http://localhost/");
  });

  it("quotes args with spaces", () => {
    expect(joinCommand(["/bin/sh", "-c", "echo hello world"])).toBe(
      '/bin/sh -c "echo hello world"',
    );
  });

  it("returns empty string for empty array", () => {
    expect(joinCommand([])).toBe("");
  });

  it("escapes inner double quotes", () => {
    expect(joinCommand(["echo", 'say "hello"'])).toBe('echo "say \\"hello\\""');
  });

  it("round-trips through parseCommand", () => {
    const args = ["/bin/sh", "-c", 'echo "hello world"'];
    expect(parseCommand(joinCommand(args))).toEqual(args);
  });
});
