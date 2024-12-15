<script lang="ts">
  import { onMount } from 'svelte';
  import { getAccApi, postAccApi } from '$lib/netio';
  import { alerts } from '$lib/stores/alerts';
  import * as m from '$lib/paraglide/messages';

  let content = $state('');

  async function loadData() {
    const rsp = await getAccApi('/config');
    content = rsp.data ?? 'no data';
  }

  async function reloadConfig() {
    try {
      const rsp = await postAccApi('/config', { data: content });
      if (rsp.status === 200) {
        alerts.addAlert('success', m.config_update_ok());
      } else {
        alerts.addAlert('error', JSON.stringify(rsp));
      }
      await loadData();
    } catch (err) {
      alerts.addAlert('error', (err as any).toString());
    }
  }

  onMount(() => {
    loadData();
  });
</script>

<div class="flex flex-col gap-4 flex-1">
  <!-- Header with reload button -->
  <div class="flex justify-between items-center">
    <h2 class="text-2xl font-bold">{m.config()}</h2>
    <button class="btn btn-primary" onclick={reloadConfig}>{m.apply_config()}</button>
  </div>

  <!-- Config content -->
  <div class="card bg-base-100 shadow-xl flex-1">
    <div class="card-body flex-1">
      <textarea class="textarea textarea-bordered w-full h-full font-mono" bind:value={content}></textarea>
    </div>
  </div>
</div>
