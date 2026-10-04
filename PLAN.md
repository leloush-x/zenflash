# Backend improvement plan

Ranked by expected benefit and safety. SAFE means the change can remain additive and covered locally. RISKY means it changes persistence, upstream behavior, or request compatibility and needs explicit compatibility tests.

1. **DONE — Parse declared reasoning options and preserve them through cache and `/v1/models`.** Tests cover exact values, null/off, and cache round trip.
2. **DONE — Validate explicit effort against the selected model/tier.** Unknown remains pass-through. Tests cover declared, undeclared, null/off, and protocol-specific field extraction.
3. **DONE — Remove guessed effort probes and their sidecar cache.** Replaced live candidate inference calls with upstream metadata.
4. **RISKY — Move catalog/pricing cache from JSON to SQLite.** Existing runtime uses atomic JSON snapshots and has no SQL dependency. Define migration, atomic refresh, TTL and stale-while-revalidate semantics first; prove migration and last-good fallback before switching.
5. **SAFE — Remove verified unused backend helpers.** No additional unused helpers were established; compatibility route aliases are not dead code by themselves.
6. **RISKY — Consolidate protocol conversion paths.** Only after parity tests for Chat, Responses, Anthropic Messages, System One, streaming and non-streaming flows.
