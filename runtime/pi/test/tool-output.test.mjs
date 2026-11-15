import assert from "node:assert/strict";
import { test } from "node:test";
import { compactToolJSON } from "../dist/tool-output.js";

test("tool JSON compaction preserves evidence, numeric lexemes and duplicate keys", () => {
  const input = '{\n  "id": 9007199254740993, "id": 2, "small": 1e-99,\n  "text": "a  b\\n\\t\\\"quoted\\\"", "nested": [ 1, 2 ]\n}';
  assert.equal(compactToolJSON(input), '{"id":9007199254740993,"id":2,"small":1e-99,"text":"a  b\\n\\t\\\"quoted\\\"","nested":[1,2]}');
  for (const text of ['{ broken  json }', '  ordinary  text\n', '"a  string"', '[output]  partial', '']) {
    assert.equal(compactToolJSON(text), text);
  }
});
