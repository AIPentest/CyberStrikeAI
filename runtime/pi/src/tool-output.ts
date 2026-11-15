// Compact only valid JSON containers. Retain the original numeric lexemes,
// key order, duplicate keys and string escapes instead of reserializing parsed
// values. Stored session messages and evidence keep their original text.
export function compactToolJSON(text: string): string {
  const start = text.trimStart()[0];
  if (start !== "{" && start !== "[") return text;
  try {
    JSON.parse(text);
  } catch {
    return text;
  }
  return text.replace(/"(?:[^"\\]|\\.)*"|\s+/g, part => part.startsWith('"') ? part : "");
}
