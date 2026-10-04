<script lang="ts">
  import Icon from "./Icon.svelte";
  let { section, title, description, icon }: { section: string; title: string; description: string; icon: string } = $props();
</script>

<header class="page-heading">
  <span class="page-heading-icon"><Icon name={icon} size={19}/></span>
  <div class="page-heading-copy">
    <div class="page-heading-kicker">CONTROL PLANE <span>/</span> {section}</div>
    <h1>{title}</h1>
    <p>{description}</p>
  </div>
</header>
