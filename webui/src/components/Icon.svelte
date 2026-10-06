<script lang="ts">
  let { name, size = 18 }: { name: string; size?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
  {#if name === "overview"}<path d="M3 3h8v8H3zM14 3h7v5h-7zM14 11h7v10h-7zM3 14h8v7H3z" />
  {:else if name === "models"}<path d="m12 3 8.5 4.5v9L12 21l-8.5-4.5v-9L12 3Z"/><path d="m3.8 7.7 8.2 4.5 8.2-4.5M12 12.2V21" />
  {:else if name === "keys"}<circle cx="8" cy="15" r="4"/><path d="m11 12 8-8 2 2-2 2 2 2-3 3-2-2-3 3" />
  {:else if name === "proxies"}<circle cx="12" cy="12" r="9"/><path d="M3.5 9h17M3.5 15h17M12 3a15 15 0 0 1 0 18M12 3a15 15 0 0 0 0 18" />
  {:else if name === "playground"}<path d="m8 5-6 7 6 7M16 5l6 7-6 7M14 3l-4 18" />
  {:else if name === "logs"}<path d="M4 5h16M4 10h16M4 15h11M4 20h8"/><circle cx="19" cy="18" r="2" />
  {:else if name === "quota"}<path d="M12 13l4-4"/><path d="M3.5 17a9 9 0 1 1 17 0"/><path d="M12 13h.01" />
  {:else if name === "settings"}<circle cx="12" cy="12" r="3"/><path d="m19.4 15 .1.1 1.4 1.1-1.4 2.4-1.7-.7a8 8 0 0 1-1.7 1l-.3 1.8h-2.8l-.3-1.8a8 8 0 0 1-1.7-1l-1.7.7-1.4-2.4 1.4-1.1a7 7 0 0 1 0-2l-1.4-1.1 1.4-2.4 1.7.7a8 8 0 0 1 1.7-1l.3-1.8h2.8l.3 1.8a8 8 0 0 1 1.7 1l1.7-.7 1.4 2.4-1.4 1.1a7 7 0 0 1 0 2Z" transform="translate(-1 -1)" />
  {:else if name === "copy"}<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3" />
  {:else if name === "check"}<path d="m5 12 4 4L19 6" />
  {:else if name === "arrow"}<path d="M5 12h14m-6-6 6 6-6 6" />
  {:else if name === "sun"}<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4M2 12h2m16 0h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  {:else if name === "moon"}<path d="M20.9 13A9 9 0 0 1 11 3.1 9 9 0 1 0 20.9 13Z" />
  {:else}<path d="m13 2-3 8h7l-6 12 1-9H6l7-11Z" />{/if}
</svg>
