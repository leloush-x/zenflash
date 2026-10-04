# Backend improvement plan

Ranked by expected benefit and safety. SAFE means the change can remain additive and covered locally. RISKY means it changes persistence, upstream behavior, or request compatibility and needs explicit compatibility tests.

1. **SAFE — Parse declared reasoning options and add decoder/cache tests.** Preserve the upstream `reasoning_options` values exactly, including any null/off value; absent source fields remain unknown. Prove limits and reasoning flags still decode unchanged.
2. **RISKY — Replace effort probes with upstream-declared effort metadata.** Prove the effort list in `/v1/models` matches `reasoning_options` fixtures exactly, stale refresh retains the last good value, and invalid explicit effort receives protocol-correct 400 errors for Chat, Responses, and Anthropic. Remove live probe traffic only after those checks pass.
3. **SAFE — Mark unsupported metadata as unknown in diagnostics.** Prove absent upstream fields remain absent/unknown in `/v1/models`; do not insert inferred numbers or effort levels.
4. **RISKY — Move catalog/pricing cache from JSON to SQLite.** Define migration, atomic refresh, TTL and stale-while-revalidate semantics before implementation. Prove old JSON cache migrates, failed refresh preserves the last good rows, concurrent reads remain available, and restart reloads the same snapshot.
5. **SAFE — Remove verified unused backend helpers.** Require compiler/reference evidence and targeted tests; compatibility route aliases are not dead code by themselves.
6. **RISKY — Consolidate protocol conversion paths.** Only after parity tests for Chat, Responses, Anthropic Messages, System One, streaming and non-streaming flows.
