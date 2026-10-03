<script lang="ts">
  import { onMount } from "svelte";
  import { num, ms } from "../lib";

  let {
    connected, live, t, tLife, rtt, tab, tabs, onTab, onLogout,
  }: {
    connected: boolean; live: any; t: any; tLife: any; rtt: number;
    tab: string; tabs: [string, string][]; onTab: (id: string) => void; onLogout: () => void;
  } = $props();

  let navEl: HTMLElement | undefined = $state();
  let ind = $state({ x: 0, w: 0, ready: false });

  function moveInd() {
    if (!navEl) return;
    const el = navEl.querySelector<HTMLElement>("[aria-current='page']");
    if (!el) return;
    ind = { x: el.offsetLeft, w: el.offsetWidth, ready: true };
  }

  $effect(() => {
    tab;
    queueMicrotask(moveInd);
  });

  onMount(() => {
    moveInd();
    const ro = new ResizeObserver(moveInd);
    if (navEl) ro.observe(navEl);
    return () => ro.disconnect();
  });

  const errRate = $derived(t && t.total > 0 ? ((t.errors / t.total) * 100).toFixed(1) : "0.0");
  const cache = $derived(live?.metrics?.usage?.lifetime?.tokens);
</script>

<header
  class="sticky top-0 z-40 border-b border-[color:var(--color-edge)] bg-[color:var(--color-bg)] sm:bg-[color:var(--color-bg)]/95 sm:backdrop-blur-md"
>
  <div class="mx-auto max-w-[1440px] px-4 sm:px-6">
    <div class="flex h-12 items-center gap-2.5 sm:h-14 sm:gap-3">
      <span
        class="flex-none select-none text-[17px] leading-none"
        role="img"
        aria-label="zenflash"
        title="zenflash-llm">⚡</span
      >

      <span class="pill {connected ? 'good' : 'bad'} ml-1">
        <span class="dot"></span>
        {connected ? "live" : "reconnecting"}
      </span>

      <span class="grow"></span>

      <div class="flex items-center gap-3 sm:gap-4">
        <div class="stat">
          <span class="stat-k">models</span>
          <span class="stat-v">{num(live?.resources?.models?.total)}</span>
        </div>
        <div class="vdiv"></div>
        <div class="stat">
          <span class="stat-k">requests</span>
          <span class="stat-v">{num(t?.total)}</span>
        </div>
        <div class="vdiv hidden sm:block"></div>
        <div class="stat hidden sm:block">
          <span class="stat-k">errors</span>
          <span class="stat-v {t && t.errors === 0 ? 'good' : t && t.errors > 0 ? 'warn' : ''}">
            {num(t?.errors)}<span class="ml-1 text-[10px] font-medium opacity-70">{errRate}%</span>
          </span>
        </div>
        <div class="vdiv hidden lg:block"></div>
        <div class="stat hidden lg:block">
          <span class="stat-k">p95</span>
          <span class="stat-v">{ms(t?.p95_ms)}</span>
        </div>
        <div class="vdiv hidden lg:block"></div>
        <div class="stat hidden lg:block">
          <span class="stat-k">tokens in/out</span>
          <span class="stat-v text-[13px]">{num(cache?.input_tokens)} / {num(cache?.output_tokens)}</span>
        </div>
      </div>

      <div class="vdiv hidden md:block"></div>
      <div class="hidden items-center gap-1.5 md:flex">
        <span class="pill faint hidden xl:inline-flex">sse {rtt}ms</span>
        <span class="pill faint">v{live?.version ?? "…"}</span>
        <button class="btn-ghost !min-h-0 !px-2.5 !py-1 text-[12px]" onclick={onLogout}>logout</button>
      </div>
    </div>

    <nav
      bind:this={navEl}
      class="tab-wrap no-scrollbar -mx-1 flex gap-0.5 overflow-x-auto px-1 pb-2"
      aria-label="sections"
    >
      {#if ind.ready}
        <span class="tab-ind" style="transform: translateX({ind.x}px); width: {ind.w}px"></span>
      {/if}
      {#each tabs as [id, label] (id)}
        <button
          class="tab"
          aria-current={tab === id ? "page" : undefined}
          onclick={() => onTab(id)}
        >
          {label}
        </button>
      {/each}
    </nav>
  </div>
</header>
