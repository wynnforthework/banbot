<script lang="ts">
  import { onMount } from 'svelte';
  import { getAccApi } from '$lib/netio';

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

<div class="card bg-base-100 shadow-xl">
  <div class="card-body">
    <div class="flex justify-between items-center mb-4">
      <h2 class="card-title">System Logs</h2>
      <button class="btn btn-primary btn-sm" onclick={loadData}>
        Refresh
      </button>
    </div>
    
    <div class="bg-base-200 rounded-lg p-4 h-[calc(100vh-12rem)] overflow-auto">
      <pre class="whitespace-pre-wrap font-mono text-sm">{content}</pre>
    </div>
  </div>
</div>
