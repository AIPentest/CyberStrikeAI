#!/usr/bin/env python3
"""Compare downloaded assessment metric cohorts; no model/network calls.

Usage: python3 scripts/assessment-evaluate.py --baseline /path/a --candidate /path/b
Input: one metrics JSON per run, as exported by /api/assessments/:id/metrics.
Rates describe system-recorded outcomes, not ground-truth vulnerability recall.
"""
import argparse
import json
from pathlib import Path

FIELDS = ("criteria_total", "criteria_satisfied", "findings", "confirmed_findings",
          "needs_retest", "tool_calls", "unique_actions", "suppressed_repeats",
          "model_calls", "model_retries", "tokens")


def summarize(directory):
    files = sorted(Path(directory).glob("*.json"))
    if not files:
        raise ValueError(f"No metric JSON files in {directory}")
    totals = dict.fromkeys(FIELDS, 0)
    completed = 0
    seen = set()
    settings = set()
    scenarios = set()
    for file in files:
        row = json.loads(file.read_text(encoding="utf-8"))
        if not isinstance(row.get("task_id"), str) or row["task_id"] in seen:
            raise ValueError(f"Missing or duplicate task_id: {file}")
        seen.add(row["task_id"])
        settings.add(json.dumps({k: row.get(k) for k in ("model_channel", "reviewer_channel", "worker_channels", "token_budget", "max_rounds")}, sort_keys=True))
        scenarios.add(row.get("name", ""))
        completed += row.get("status") == "completed"
        for key in FIELDS:
            value = row.get(key)
            if type(value) is not int or value < 0:
                raise ValueError(f"Invalid nonnegative integer {key}: {file}")
            totals[key] += value
        if row["criteria_satisfied"] > row["criteria_total"] or row["confirmed_findings"] + row["needs_retest"] > row["findings"] or row["unique_actions"] > row["tool_calls"]:
            raise ValueError(f"Inconsistent metric counters: {file}")
    return {"configurations": [json.loads(s) for s in sorted(settings)], "scenarios": sorted(scenarios), "runs": len(files), "completed_runs": completed, **totals,
            "completion_rate": completed / len(files),
            "criteria_satisfied_rate": (totals["criteria_satisfied"] / totals["criteria_total"]
                                        if totals["criteria_total"] else None),
            "tokens_per_run": totals["tokens"] / len(files)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--baseline", required=True)
    parser.add_argument("--candidate", required=True)
    parser.add_argument("--output")
    args = parser.parse_args()
    try:
        baseline, candidate = summarize(args.baseline), summarize(args.candidate)
    except (ValueError, OSError) as error:
        parser.error(str(error))
    output = {"baseline": baseline, "candidate": candidate,
              "same_configurations": baseline["configurations"] == candidate["configurations"],
              "same_scenarios": baseline["scenarios"] == candidate["scenarios"],
              "delta": {key: candidate[key] - baseline[key]
                        for key in (*FIELDS, "completion_rate", "tokens_per_run")},
              "interpretation": "Use identical authorized scenarios, model channels and budgets. "
                                "These are system-reviewed outcomes, not ground-truth accuracy. "
                                "Tokens include conservative charges when provider usage is missing."}
    text = json.dumps(output, ensure_ascii=False, indent=2)
    if args.output:
        Path(args.output).write_text(text + "\n", encoding="utf-8")
    print(text)


if __name__ == "__main__":
    main()
