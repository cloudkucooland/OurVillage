Model	RawJSON %	Intent %	Tok/s	Judge Scores (P4 / P5 / P6)	Avg Score	Verdict
Huihui-Qwen3-8B-abliterated-v2	100.0%	100.0%	2.24	7 / — / 10	8.5 / 10	Best overall quality. Zero hallucinations on P4 and P6; cleanly added a new project from the P6 observation without fabricating completed actions.
bmo-qwen3.5-4b	100.0%	100.0%	1.96	2 / 10 / 10	7.3 / 10	Best zero-hallucination 4B. Scored 10/10 with zero hallucinations on both P5 and P6; only fault was being overly conservative ({"no_entry": true}) on P4.
qwen3-4b	100.0%	100.0%	3.97	9 / 4 / 6	6.3 / 10	Best speed/gameplay choice (~4 tok/s). Scored 9/10 on P4, and 100% raw JSON across all 7 prompts.
Qwen3-8B-hybrid-ratio-1.0	100.0%	100.0%	1.21	9 / 6 / 3	6.0 / 10	Strong on P4 (9/10), but hallucinated completing next_step on P6 and is ~3.3x slower than qwen3-4b.
hiiq-16k (Qwen3.5-9B)	57.1%	100.0%	1.49	8 / 5 / 5	6.0 / 10	Needs markdown fence stripping and tends to embellish observations into completed actions.
qwen3-4b_w8a8-HanzoHuang	100.0%	100.0%	3.70	6 / 6 / 5	5.7 / 10	Slightly behind standard qwen3-4b on grounding and perspective.
qwen3.5-4b-4k-HanzoHuang	85.7%	100.0%	2.35	4 / 3 / 10	5.7 / 10	Nailed P6 (10/10), but inverted perspective on P4 and fabricated actions on P5.
granite-42-3b-oliver0804	100.0%	100.0%	3.93	7 / 5 / 4	5.3 / 10	Fast and clean JSON, but invented a whole storytelling circle event on P6.
