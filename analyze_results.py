import sys
import glob
import json
import os
from collections import defaultdict

models = defaultdict(
    lambda: {
        "total": 0,
        "json_valid": 0,
        "intent_valid": 0,
        "tokens_per_sec": [],
        "backend": "unknown",
    }
)

# Aggregate both root JSON reports and nested results reports
json_files = glob.glob("*.json") + glob.glob("results/**/*.json", recursive=True)
if len(sys.argv) > 1:
  json_files = glob.glob(sys.argv[1], recursive=True)

for path in json_files:
  try:
    with open(path, "r") as f:
      data = json.load(f)
      model = data.get("model_name", os.path.basename(path))
      backend = data.get("backend", "unknown")
      ctx = data.get("context_size", "unknown")
      key = f"{model} [{backend}] (ctx:{ctx})"
      for r in data.get("results", []):
        models[key]["total"] += 1
        models[key]["backend"] = backend
        if r.get("json_valid"):
          models[key]["json_valid"] += 1
        if r.get("intent_valid"):
          models[key]["intent_valid"] += 1
        tps = r.get("tokens_per_sec")
        if tps:
          models[key]["tokens_per_sec"].append(tps)
  except Exception as e:
    pass

rows = []
for k, v in models.items():
  tot = v["total"]
  if tot == 0:
    continue
  jv = (v["json_valid"] / tot) * 100
  iv = (v["intent_valid"] / tot) * 100
  avg_tps = (
      sum(v["tokens_per_sec"]) / len(v["tokens_per_sec"])
      if v["tokens_per_sec"]
      else 0
  )
  rows.append((iv, jv, avg_tps, k, tot))

rows.sort(key=lambda x: (x[0], x[1], x[2]), reverse=True)

print("Intent % | JSON % | Tokens/s | Prompts | Model, Backend & Context Size")
print("-" * 90)
for iv, jv, tps, k, tot in rows:
  print(f"{iv:6.1f}% | {jv:6.1f}% | {tps:8.2f} | {tot:7d} | {k}")
