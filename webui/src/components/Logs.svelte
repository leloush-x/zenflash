<script lang="ts">
  import { api, clock } from "../lib";

  let logs = $state<any[]>([]);
  let logFilter = $state("");
  let autoLogs = $state(true);
  let failed = $state(false);
  let lastSeq = 0;

  async function pullLogs() {
    try {
      const r = await api<any>(`/api/logs?after=${lastSeq}&limit=200`);
      if (r.events?.length) {
        lastSeq = r.events[r.events.length - 1].sequence;
        logs = [...r.events.slice().reverse(), ...logs].slice(0, 400);
      }
      failed = false;
    } catch {
      failed = true;
    }
  }

  $effect(() => {
    if (!autoLogs) return;
    pullLogs();
    const id = setInterval(() => { if (autoLogs) pullLogs(); }, 3000);
    return () => clearInterval(id);
  });

  const visibleLogs = $derived(
    logs.filter((l) => !logFilter || l.level === logFilter || JSON.stringify(l.fields ?? {}).includes(logFilter) || l.message?.includes(logFilter)),
  );
</script>

<section class="panel fade-up p-3 sm:p-4">
  <div class="mb-3 flex flex-wrap items-center gap-2">
    <div class="eyebrow !mb-0">gateway log ring</div>
    <span class="grow"></span>
    <select bind:value={logFilter} class="w-auto" aria-label="level filter">
      <option value="">all levels</option>
      <option value="debug">debug</option>
      <option value="info">info</option>
      <option value="warn">warn</option>
      <option value="error">error</option>
    </select>
    <label class="flex items-center gap-1.5 text-[12px]"><input type="checkbox" bind:checked={autoLogs} /> auto</label>
    <span class="pill faint">{visibleLogs.length} shown</span>
  </div>

  <div
    class="logbox overflow-y-auto rounded-xl border border-[color:var(--color-edge)] bg-[oklch(0.14_0.012_272/0.75)] p-1.5 font-mono text-[11.5px]"
  >
    {#each visibleLogs as l (l.sequence)}
      <div class="logline">
        <span class="ll-meta">
          <span class="ll-time">{clock(l.time)}</span>
          <span class="lv lv-{l.level === 'error' ? 'error' : l.level === 'warn' ? 'warn' : l.level === 'info' ? 'info' : 'debug'}">{l.level}</span>
          <span class="ll-comp">{l.component}</span>
        </span>
        <span class="ll-msg">
          {l.message}
          {#if l.fields && Object.keys(l.fields).length}
            <span class="ll-fields">{JSON.stringify(l.fields)}</span>
          {/if}
        </span>
      </div>
    {/each}
    {#if visibleLogs.length === 0}
      <div class="empty">
        {failed
          ? "log endpoint unreachable"
          : !autoLogs
            ? "auto-paused — tick auto to stream the ring"
            : "no log events yet"}
      </div>
    {/if}
  </div>
</section>
