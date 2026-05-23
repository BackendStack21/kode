# AIEB — Agent Intelligence, Efficiency & Security Benchmark

Automated benchmark comparing **odek** vs **hermes** on four tiers of agentic tasks.

## Tiers

| Tier | Domain | Tasks | Max Score |
|------|--------|-------|-----------|
| 1 | Code Understanding | explain_function, find_bug, identify_architecture | 100 each |
| 2 | Tool Orchestration | find_exports, count_loc, find_todos | 100 each |
| 3 | Code Generation | write_function, add_test, refactor | 100 each |
| 4 | Agent Speed | fast_read, quick_math, multi_search | 100 each |

## Scoring

**v2.0 changes:**
- All required keywords must appear (no partial credit below 50%)
- LOC tolerance reduced from 10% → 3%
- merge_intervals: large input test + input mutation check
- verify_test_file: counts distinct assertions, not just test functions
- verify_refactor: requires dict-based rules validator with type validators
- Slow tasks (>120s) capped at 95
- Iteration-inefficient tasks penalized

## Usage

```bash
# Run odek only (default)
python3 benchmark/aieb.py

# Run hermes only
python3 benchmark/aieb.py --hermes

# Run both agents (comparison mode)
python3 benchmark/aieb.py --both

# Results saved to benchmark/results.json
```

Both agents use the same model (`deepseek-v4-flash`) on identical tasks.
