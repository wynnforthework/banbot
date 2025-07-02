<script lang="ts">
  import * as m from '$lib/paraglide/messages';
  import { getAccApi, postAccApi } from '$lib/netio';
  import { acc } from '$lib/dash/store';

  let currentTool = $state('download_history');
  let searchData = $state({
    source: 'order',
    startTime: '',
    endTime: '',
    downloadId: ''
  });
  let response = $state('');

  const menuItems = [
    {
      title: m.trade_history(),
      children: [
        { id: 'download_history', text: m.download_history() },
        { id: 'download_result', text: m.get_download_result() }
      ]
    }
  ];
  const sourceTypes = [
    { value: 'income', label: "income" },
    { value: 'order', label: "order" },
    { value: 'trade', label: "trade" },
  ];

  async function downloadHistory() {
    const data = {
      source: searchData.source,
      startTime: (searchData.startTime).toString(),
      endTime: searchData.endTime.toString()
    };
    const rsp = await postAccApi('/start_down_trade', data);
    response = JSON.stringify(rsp, null, 2);
  }

  async function getDownloadResult() {
    const data = {
      source: searchData.source,
      id: searchData.downloadId
    };
    const rsp = await getAccApi('/get_down_trade', data);
    response = JSON.stringify(rsp, null, 2);
  }

  function switchTool(tool: string) {
    currentTool = tool;
    response = '';
    searchData = {
      source: 'order',
      startTime: '',
      endTime: '',
      downloadId: ''
    };
  }
</script>

{#if $acc.env !== 'dry_run'}
<div class="flex gap-4 p-4 min-h-screen">
  <!-- Left Menu -->
  <div class="w-[260px]">
    <ul class="menu bg-base-200 rounded-box w-full">
      {#each menuItems as menuItem}
        <li>
          <details open>
            <summary>{menuItem.title}</summary>
            <ul>
              {#each menuItem.children as child}
                <li>
                  <a 
                    class={currentTool === child.id ? 'menu-active' : ''}
                    onclick={() => switchTool(child.id)}
                  >
                    {child.text}
                  </a>
                </li>
              {/each}
            </ul>
          </details>
        </li>
      {/each}
    </ul>
  </div>

  <!-- Right Content -->
  <div class="flex-1">
    <div class="card bg-base-100">
      <div class="card-body">
        {#if currentTool === 'download_history'}
          <h2 class="card-title text-lg mb-4">{m.download_history()}</h2>
          <div class="flex gap-4 items-center mb-6">
            <select class="select select-sm focus:outline-none w-36" bind:value={searchData.source}>
              {#each sourceTypes as type}
                <option value={type.value}>{type.label}</option>
              {/each}
            </select>
            <input 
              type="text"
              class="input input-sm focus:outline-none text-sm font-mono w-56"
              bind:value={searchData.startTime}
              placeholder={m.start_time()}
            />
            <input 
              type="text"
              class="input input-sm focus:outline-none text-sm font-mono w-56"
              bind:value={searchData.endTime}
              placeholder={m.end_time()}
            />
            <button 
              class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" 
              onclick={downloadHistory}
            >
              {m.download()}
            </button>
          </div>
        {:else if currentTool === 'download_result'}
          <h2 class="card-title text-lg mb-4">{m.get_download_result()}</h2>
          <div class="flex gap-4 items-center mb-6">
            <select class="select select-sm focus:outline-none w-36" bind:value={searchData.source}>
              {#each sourceTypes as type}
                <option value={type.value}>{type.label}</option>
              {/each}
            </select>
            <input 
              type="text" 
              class="input input-sm focus:outline-none text-sm font-mono"
              bind:value={searchData.downloadId}
              placeholder={m.download_id()}
            />
            <button 
              class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" 
              onclick={getDownloadResult}
            >
              {m.query()}
            </button>
          </div>
        {/if}

        {#if response}
          <div class="divider">{m.response()}</div>
          <pre class="bg-base-200 p-4 rounded-lg overflow-x-auto">
            {response}
          </pre>
        {/if}
      </div>
    </div>
  </div>
</div>
{:else}
  <div class="flex flex-col items-center justify-center min-h-[400px] text-center">
    <div class="text-base-content/60">
      <svg class="w-16 h-16 mx-auto mb-4 opacity-50" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"></path>
      </svg>
      <h3 class="text-lg font-medium mb-2">DryRun Mode</h3>
      <p class="text-sm text-base-content/50 max-w-md">
        Trading tools are not available in dry_run mode as they interact with real exchange data and operations.
      </p>
    </div>
  </div>
{/if}
