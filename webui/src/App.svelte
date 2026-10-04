<script lang="ts">
  import { onMount } from "svelte";
  import { api, configToUpdate, put, setCsrf, setOnUnauthorized } from "./lib";
  import Header from "./components/Header.svelte";
  import Overview from "./components/Overview.svelte";
  import Models from "./components/Models.svelte";
  import Keys from "./components/Keys.svelte";
  import Proxies from "./components/Proxies.svelte";
  import Playground from "./components/Playground.svelte";
  import Logs from "./components/Logs.svelte";
  import Config from "./components/Config.svelte";
  import Login from "./components/Login.svelte";
  import QuickConnect from "./components/QuickConnect.svelte";

  const TABS: [string, string][] = [
    ["overview", "Overview"],
    ["models", "Models"],
    ["keys", "Keys"],
    ["proxies", "Proxies"],
    ["playground", "Playground"],
    ["logs", "Logs"],
    ["config", "Config"],
  ];

  let tab = $state(localStorage.getItem("zf.tab") ?? "overview");
  let live = $state<any>(null);
  let connected = $state(false);
  let user = $state<string | null>(null);
  let checking = $state(true);
  let hasKey = $state(false);
  let apiBase = $state("");
  let theme = $state(localStorage.getItem("zf.theme") ?? "mint");

  function startStream() {
    const es = new EventSource("/api/events");
    es.addEventListener("tick", (e) => {
      live = JSON.parse((e as MessageEvent).data);
      connected = true;
    });
    es.onerror = () => (connected = false);
    es.onopen = () => (connected = true);
    return () => es.close();
  }

  onMount(() => {
    setOnUnauthorized(() => {
      user = null;
      live = null;
      connected = false;
    });
    let stop: (() => void) | undefined;
    api("/api/auth/session")
      .then((s) => {
        if (s.authenticated) {
          user = s.username;
          setCsrf(s.csrf_token);
          api("/api/config").then((c) => {
            hasKey = (c.server_keys ?? []).length > 0;
            const address = String(c.effective?.listen ?? c.listen ?? "");
            const port = address.slice(address.lastIndexOf(":") + 1);
            const url = new URL(window.location.origin);
            if (/^\d+$/.test(port)) url.port = port;
            apiBase = `${url.origin}/v1`;
          }).catch(() => { apiBase = `${window.location.origin}/v1`; });
          stop = startStream();
        }
      })
      .catch(() => {})
      .finally(() => (checking = false));
    return () => stop?.();
  });

  function onLogin(u: string, csrf: string) {
    user = u;
    setCsrf(csrf);
    startStream();
  }

  function cycleTheme() {
    theme = theme === "mint" ? "violet" : theme === "violet" ? "amber" : "mint";
  }

  async function createServerKey() {
    const config = await api("/api/config");
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    const value = `zf_${Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("")}`;
    config.server_keys = [...(config.server_keys ?? []), { value }];
    const response = await put("/api/config", configToUpdate(config));
    if (!response?.config) throw new Error(response?.error?.message ?? "Could not save the API key");
    hasKey = true;
    return value;
  }

  async function logout() {
    try {
      await api("/api/auth/logout", { method: "POST" });
    } catch {}
    user = null;
    live = null;
    connected = false;
  }

  $effect(() => {
    tab;
    localStorage.setItem("zf.tab", tab);
  });
  $effect(() => {
    theme;
    localStorage.setItem("zf.theme", theme);
    document.documentElement.dataset.theme = theme;
  });
  const t = $derived(live?.metrics?.last_hour);
</script>

{#if checking}
  <main class="mx-auto w-full max-w-[1440px] px-4 py-5 sm:px-6 sm:py-6">
    <div class="skel h-[96px]"></div>
    <p class="faint mt-3 text-center text-xs">checking session…</p>
  </main>
{:else if !user}
  <Login onSuccess={onLogin} />
{:else}
  <div class="app-shell">
  <Header {connected} {live} {t} {tab} tabs={TABS} onTab={(id) => (tab = id)} onLogout={logout} {theme} onTheme={cycleTheme} />
  <main class="workspace">
    {#if !live}
      <div class="welcome-strip"><div><div class="eyebrow">ZENFLASH CONTROL PLANE</div><h1>Everything, in one place.</h1><p>Connecting to live runtime data…</p></div></div>
      <div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">{#each Array(6) as _, i (i)}<div class="skel h-[96px]"></div>{/each}</div>
    {:else}
      {#key tab}
        <div class="fade-up">
          {#if tab === "overview"}
            <div class="welcome-strip"><div><div class="eyebrow">ZENFLASH CONTROL PLANE</div><h1>Everything, in one place.</h1><p>Your models, routing and API activity at a glance.</p></div><button class="welcome-action" onclick={() => tab = "playground"}>Try a request <span>↗</span></button></div>
            <QuickConnect {hasKey} {apiBase} onCreateKey={createServerKey} onSettings={() => tab = "config"} />
            <Overview {live} />
          {/if}
          {#if tab === "models"}<Models />{/if}
          {#if tab === "keys"}<Keys {live} />{/if}
          {#if tab === "proxies"}<Proxies />{/if}
          {#if tab === "playground"}<Playground />{/if}
          {#if tab === "logs"}<Logs />{/if}
          {#if tab === "config"}<Config />{/if}
        </div>
      {/key}
    {/if}
  </main>
  <footer class="workspace-footer"><span>ZenFlash</span><span>OpenAI compatible · Anthropic API</span><span>Control plane</span></footer>
  </div>
{/if}
