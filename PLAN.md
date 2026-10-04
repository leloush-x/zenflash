# Backend improvement plan

Ranked by expected benefit and safety. SAFE means the change can remain additive and covered locally. RISKY means it changes persistence, upstream behavior, or request compatibility and needs explicit compatibility tests.

1. **SAFE — Add deterministic tests for model metadata decode and cache fallback.** Prove context/output/reasoning are copied only when present, stale cache survives fetch failure, and cached snapshots are not mutated by reads.
2. **SAFE — Mark unsupported metadata as unknown in diagnostics.** Prove absent upstream fields remain absent/unknown in `/v1/models`; do not insert inferred numbers or effort levels.
3. **RISKY — Replace effort probing with upstream-declared effort metadata.** First establish that the authoritative upstream model-list schema actually publishes levels. Prove each returned level exactly matches fixture/source data and invalid explicit effort receives a protocol-correct 400. If upstream provides no levels, expose unknown and do not guess.
4. **RISKY — Move catalog/pricing cache from JSON to SQLite.** Define migration, atomic refresh, TTL and stale-while-revalidate semantics before implementation. Prove old JSON cache migrates, failed refresh preserves the last good rows, concurrent reads remain available, and restart reloads the same snapshot.
5. **SAFE — Remove verified unused backend helpers.** Require compiler/reference evidence and targeted tests; compatibility route aliases are not dead code by themselves.
6. **RISKY — Consolidate protocol conversion paths.** Only after parity tests for Chat, Responses, Anthropic Messages, System One, streaming and non-streaming flows.
