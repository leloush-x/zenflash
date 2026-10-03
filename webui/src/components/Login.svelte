<script lang="ts">
  import { post } from "../lib";

  let { onSuccess }: { onSuccess: (username: string, csrf: string) => void } = $props();

  let username = $state(localStorage.getItem("zf.user") ?? "admin");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = "";
    try {
      const r = await post<any>("/api/auth/login", { username, password });
      if (r?.csrf_token) { localStorage.setItem("zf.user", r.username); onSuccess(r.username, r.csrf_token); }
      else error = r?.error?.message ?? "login failed";
    } catch (e) {
      error = String(e).includes("401") ? "invalid username or password" : String(e);
    } finally {
      busy = false;
    }
  }
</script>

<main class="mx-auto flex min-h-[70vh] w-full max-w-sm flex-col justify-center px-4">
  <form class="card fade-up flex flex-col gap-3 p-5" onsubmit={submit}>
    <div class="flex items-center gap-2.5">
      <span class="text-[17px] leading-none">⚡</span>
      <div class="eyebrow !mb-0">zenflash-llm · sign in</div>
    </div>
    <label class="flex flex-col gap-1.5 text-[13px]">
      <span class="dim">username</span>
      <input bind:value={username} autocomplete="username" required />
    </label>
    <label class="flex flex-col gap-1.5 text-[13px]">
      <span class="dim">password</span>
      <input type="password" bind:value={password} autocomplete="current-password" required />
    </label>
    {#if error}<p class="text-[12px] bad">{error}</p>{/if}
    <button class="btn-primary" disabled={busy}>{busy ? "signing in…" : "sign in"}</button>
  </form>
</main>
