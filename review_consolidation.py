import glob
import json
import re

def check_json(text):
  text = text.strip()
  try:
    json.loads(text)
    return True
  except:
    pass
  match = re.search(r'```(?:json)?\s*([\s\S]*?)\s*```', text)
  if match:
    try:
      json.loads(match.group(1).strip())
      return True
    except:
      pass
  start = text.find('{')
  end = text.rfind('}')
  if start != -1 and end != -1 and end > start:
    try:
      json.loads(text[start:end+1])
      return True
    except:
      pass
  return False

print("=== Memory Consolidation & Synthesis & Judge Evaluation Review ===")
found = False

for path in glob.glob("*-4k.json") + glob.glob("results/**/*.json", recursive=True):
  try:
    with open(path, "r") as f:
      data = json.load(f)
      model = data.get("model_name", "unknown")
      ctx = data.get("context_size", "unknown")
      results = data.get("results", [])
      consolidation_results = [r for r in results if r.get("prompt_index", 0) >= 4]
      if not consolidation_results:
        continue
      found = True
      print(f"\nModel: {model} (Context Size: {ctx}) [File: {path}]")
      print("=" * 60)
      for r in consolidation_results:
        p_idx = r.get("prompt_index")
        agent_name = "Martha" if p_idx == 4 else "Eldon"
        output = r.get("output_text", "").strip()
        json_valid = check_json(output)
        judge_eval = r.get("judge_evaluation", "").strip()
        print(f"[{agent_name} Log Consolidation (Prompt Index {p_idx})]")
        print(f"  - Detected Valid JSON: {json_valid}")
        print(f"  - Output Text:\n{output}")
        if judge_eval:
          print(f"  - Judge Evaluation:\n{judge_eval}")
        print("-" * 60)
  except Exception as e:
    pass

if not found:
  print("No consolidation results found in JSON reports yet.")
