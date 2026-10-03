<script lang="ts">
  import { onMount } from "svelte";
  import { api, setCsrf, setOnUnauthorized } from "./lib";
  import Header from "./components/Header.svelte";
  import Overview from "./components/Overview.svelte";
  import Models from "./components/Models.svelte";
  import Keys from "./components/Keys.svelte";
  import Proxies from "./components/Proxies.svelte";
  import Playground from "./components/Playground.svelte";
  import Logs from "./components/Logs.svelte";
  import Config from "./components/Config.svelte";
  import Login from "./components/Login.svelte";

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
  let rtt = $state(0);
  let user = $state<string | null>(null);
  let checking = $state(true);

  function startStream() {
    const es = new EventSource("/api/events");
    let last = 0;
    es.addEventListener("tick", (e) => {
      const now = Date.now();
      if (last) rtt = now - last;
      last = now;
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

  const t = $derived(live?.metrics?.last_hour);
  const tLife = $derived(live?.metrics?.lifetime);
</script>

{#if checking}
  <main class="mx-auto w-full max-w-[1440px] px-4 py-5 sm:px-6 sm:py-6">
    <div class="skel h-[96px]"></div>
    <p class="faint mt-3 text-center text-xs">checking session…</p>
  </main>
{:else if !user}
  <Login onSuccess={onLogin} />
{:else}
  <Header {connected} live={live} {t} {tLife} {rtt} {tab} tabs={TABS} onTab={(id) => (tab = id)} onLogout={logout} />

  <main class="mx-auto w-full max-w-[1440px] px-4 py-5 sm:px-6 sm:py-6">
    {#if !live}
      <div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
        {#each Array(6) as _, i (i)}
          <div class="skel h-[96px]"></div>
        {/each}
      </div>
      <div class="skel mt-3 h-56"></div>
      <p class="faint mt-3 text-center text-xs">connecting to live stream…</p>
    {:else}
      {#key tab}
        <div class="fade-up">
          {#if tab === "overview"}<Overview {live} />{/if}
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
{/if}
