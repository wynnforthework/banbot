<script lang="ts">
  import { onMount } from 'svelte';
  import { getAccApi } from '$lib/netio';
  import * as m from '$lib/paraglide/messages';

  let content = $state('');

  async function loadData() {
    const rsp = await getAccApi('/config');
    content = rsp.data ?? 'no data';
  }

  onMount(() => {
    loadData();
    // 因在线更新配置有很多限制，大多数配置无法即刻生效，故暂不提供在线修改
  });
</script>

<div class="card bg-base-100">
  <div class="card-body p-4">
    <div class="flex justify-between items-center mb-3">
      <h2 class="text-xl font-semibold text-base-content">{m.config()}</h2>
    </div>
    
    <div class="bg-base-200/50 rounded-lg">
      <pre 
        class="p-4 font-mono text-sm w-full h-full whitespace-pre-wrap break-words"
      >{content}</pre>
    </div>
  </div>
</div>
