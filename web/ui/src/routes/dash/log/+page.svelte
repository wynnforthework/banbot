<script lang="ts">
  import { onMount } from 'svelte';
  import { getAccApi } from '$lib/netio';
  import * as m from '$lib/paraglide/messages';

  let content = $state('');

  async function loadData() {
    try {
      const rsp = await getAccApi('/log', { num: 3000 });
      content = rsp.data ?? 'no data';
    } catch (err) {
      console.error('Failed to load logs:', err);
    }
  }

  onMount(() => {
    loadData();
  });
</script>

<div class="card bg-base-100">
  <div class="card-body p-4">
    <div class="flex justify-between items-center mb-3">
      <h2 class="text-xl font-semibold text-base-content">{m.system_logs()}</h2>
      <button class="btn btn-sm btn-primary" onclick={loadData}>
        {m.refresh()}
      </button>
    </div>
    
    <div class="bg-base-200/50 rounded-lg p-4 h-[calc(100vh-12rem)]">
      <pre class="whitespace-pre-wrap font-mono text-sm text-base-content/80">{content}</pre>
    </div>
  </div>
</div>
