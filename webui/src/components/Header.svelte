<script lang="ts">
  import Icon from "./Icon.svelte";
  import { num, ms } from "../lib";

  let { connected, live, t, tab, tabs, onTab, onLogout, theme, onTheme }: {
    connected: boolean; live: any; t: any; tab: string; tabs: [string, string][];
    onTab: (id: string) => void; onLogout: () => void; theme: string; onTheme: () => void;
  } = $props();
  const icons: Record<string, string> = { overview: "overview", models: "models", quota: "quota", keys: "keys", proxies: "proxies", playground: "playground", logs: "logs", config: "settings" };
  const themeName = $derived(theme === "violet" ? "Violet" : theme === "amber" ? "Amber" : "Mint");
</script>

<aside class="side-rail">
  <a class="brand" href="#overview" onclick={(e) => { e.preventDefault(); onTab("overview"); }} aria-label="ZenFlash home">
    <span class="brand-mark"><Icon name="bolt" size={21}/></span>
    <span class="brand-copy"><strong>zenflash</strong><small>CONTROL PLANE</small></span>
  </a>
  <div class="rail-caption">WORKSPACE</div>
  <nav class="rail-nav" aria-label="Main navigation">
    {#each tabs as [id, label] (id)}
      <button class="rail-link" class:active={tab === id} aria-current={tab === id ? "page" : undefined} onclick={() => onTab(id)}>
        <Icon name={icons[id] ?? "overview"}/><span>{label}</span>{#if id === "models"}<span class="nav-count">{num(live?.resources?.models?.total)}</span>{/if}
      </button>
    {/each}
  </nav>
  <div class="rail-bottom">
    <div class="rail-health"><span class="status-dot" class:online={connected}></span><div><strong>{connected ? "System online" : "Reconnecting"}</strong><small>v{live?.version ?? "…"} · {ms(t?.p95_ms)} p95</small></div></div>
    <button class="theme-control" onclick={onTheme} title="Switch accent theme"><span class="theme-swatch"></span><span>Theme</span><strong>{themeName}</strong></button>
    <button class="account-control" onclick={onLogout} title="Sign out"><span class="avatar">Z</span><span class="account-name">WebUI session</span><span class="account-out">↗</span></button>
  </div>
</aside>

<header class="topbar">
  <div class="mobile-brand"><span class="brand-mark small"><Icon name="bolt" size={18}/></span><strong>zenflash</strong></div>
  <div class="breadcrumbs"><span>Workspace</span><b>/</b><strong>{tabs.find(([id]) => id === tab)?.[1] ?? "Overview"}</strong></div>
  <div class="topbar-right">
    <span class="connection-state"><span class="status-dot" class:online={connected}></span>{connected ? "Live" : "Connecting"}</span>
    <span class="topbar-divider"></span><span class="topbar-stat"><small>REQUESTS 1H</small><strong>{num(t?.total)}</strong></span>
    <span class="topbar-stat hide-small"><small>ERRORS</small><strong>{num(t?.errors)}</strong></span>
  </div>
</header>

<nav class="mobile-nav" aria-label="Mobile navigation">
  {#each tabs as [id, label] (id)}
    <button class="mobile-nav-link" class:active={tab === id} aria-current={tab === id ? "page" : undefined} onclick={() => onTab(id)}><Icon name={icons[id] ?? "overview"} size={19}/><span>{label}</span></button>
  {/each}
</nav>
