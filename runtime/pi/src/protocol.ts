import { once } from "node:events";

export const PROTOCOL_VERSION = 1;
export const MAX_RECORD_BYTES = 32 * 1024 * 1024;

// JSONL splits only on LF. U+2028/U+2029 inside a JSON string are ordinary data.
export class RecordReader {
  private pending = Buffer.alloc(0);
  push(chunk: Buffer): unknown[] {
    this.pending = Buffer.concat([this.pending, chunk]);
    const records: unknown[] = [];
    for (;;) {
      const end = this.pending.indexOf(10);
      if (end < 0) break;
      if (end > MAX_RECORD_BYTES) throw new Error("Protocol record exceeds limit");
      const line = this.pending.subarray(0, end).toString("utf8").trim();
      this.pending = this.pending.subarray(end + 1);
      if (line) records.push(JSON.parse(line));
    }
    if (this.pending.length > MAX_RECORD_BYTES) throw new Error("Protocol record exceeds limit");
    return records;
  }
  finish(): void {
    if (this.pending.length) throw new Error("Truncated protocol record");
  }
}

let writes = Promise.resolve();
export function send(value: unknown): Promise<void> {
  const line = JSON.stringify(value) + "\n";
  if (Buffer.byteLength(line) > MAX_RECORD_BYTES) return Promise.reject(new Error("Protocol output exceeds limit"));
  writes = writes.then(async () => {
    if (!process.stdout.write(line)) await once(process.stdout, "drain");
  });
  return writes;
}
