<script lang="ts">
  import { onMount } from 'svelte';
  import * as m from '$lib/paraglide/messages';
  import { getAccApi } from '$lib/netio';
  import { fmtDuration } from '$lib/dateutil';
  import type {OdGroup} from '$lib/order/types';
  import {localizeHref} from "$lib/paraglide/runtime";

  let tabName = $state('day');
  let dataList = $state<OdGroup[]>([]);
  let exitTags = $state<OdGroup[]>([]);
  let enterTags = $state<OdGroup[]>([]);
  let allPairs = $state<string[]>([]);
  let selectedPairs = $state<string[]>([]);
  
  let durations = $state({
    wins: 0,
    draws: 0,
    losses: 0
  });

  let search = $state({
    startTime: '',
    stopTime: ''
  });

  const tabLabels = [
    { name: 'day', title: m.by_day() },
    { name: 'week', title: m.by_week() },
    { name: 'month', title: m.by_month() },
    { name: 'symbol', title: m.by_pair() }
  ];

  function toUTCStamp(dateStr: string): number {
    if (!dateStr) return 0;
    const year = parseInt(dateStr.slice(0, 4));
    const month = parseInt(dateStr.slice(4, 6)) - 1;
    const day = parseInt(dateStr.slice(6, 8));
    return new Date(Date.UTC(year, month, day)).getTime();
  }

  async function loadData() {
    const data = {
      groupBy: tabName,
      pairs: selectedPairs,
      start: toUTCStamp(search.startTime),
      stop: toUTCStamp(search.stopTime),
      limit: 30
    };

    const rsp = await getAccApi('/performance', data);
    dataList.splice(0, dataList.length, ...(rsp.items ?? []));
    exitTags.splice(0, exitTags.length, ...(rsp.exits ?? []));
    durations = rsp.durations ?? { wins: 0, draws: 0, losses: 0 };
    enterTags.splice(0, enterTags.length, ...(rsp.enters ?? []));
  }

  async function loadPairs() {
    const data = {
      start: toUTCStamp(search.startTime),
      stop: toUTCStamp(search.stopTime)
    };
    const rsp = await getAccApi('/task_pairs', data);
    allPairs = rsp.pairs ?? [];
    if (selectedPairs.length === 0) {
      selectedPairs = [...allPairs];
    }
  }

  function clickTab(name: string) {
    tabName = name;
    loadData();
  }

  onMount(() => {
    loadData();
    loadPairs();
  });

</script>

{#snippet PerfTable(title: string, data: any[])}
<div class="card bg-base-100 shadow-md hover:shadow-lg transition-shadow rounded-lg border border-base-200">
  <div class="card-body p-4">
    <h2 class="card-title text-lg font-medium mb-3">{title}</h2>
    <div class="overflow-x-auto">
      <table class="table table-sm">
        <thead>
          <tr>
            <th class="bg-base-200/30 font-medium text-sm">{title}</th>
            <th class="bg-base-200/30 font-medium text-sm text-right">{m.win_total_num()}</th>
            <th class="bg-base-200/30 font-medium text-sm text-right">{m.total_profit()}</th>
            <th class="bg-base-200/30 font-medium text-sm text-right">{m.profit_rate()}</th>
            <th class="bg-base-200/30 font-medium text-sm">{m.actions()}</th>
          </tr>
        </thead>
        <tbody>
          {#each data as row}
            <tr class="hover:bg-base-200/50 transition-colors">
              <td class="text-sm font-mono">{row.key}</td>
              <td class="text-sm font-mono text-right">{row.winNum}/{row.closeNum}</td>
              <td class="text-sm font-mono text-right">{row.profitSum.toFixed(5)}</td>
              <td class="text-sm font-mono text-right">{(row.profitPct * 100).toFixed(1)}%</td>
              <td>
                <a href={localizeHref("/dash/kline?pair="+row.key)} class="btn btn-xs bg-primary/90 hover:bg-primary text-primary-content border-none">
                  {m.kline()}
                </a>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </div>
</div> 
{/snippet}

<!-- Tab Menu -->
<div class="tabs tabs-boxed bg-base-200/50 p-1 rounded-md m-4">
  {#each tabLabels as {name, title}}
    <button 
      class="tab tab-sm transition-all duration-200 px-4 {tabName === name ? 'tab-active bg-primary/90 text-primary-content' : 'hover:bg-base-300'}"
      onclick={() => clickTab(name)}
    >
      {title}
    </button>
  {/each}
</div>

<div class="flex flex-col gap-6 min-h-screen m-4 mt-0">
  <!-- 合并的控制面板 -->
  <div class="card bg-base-100 shadow-md hover:shadow-lg transition-shadow rounded-lg border border-base-200">
    <div class="card-body p-4">
      <!-- Pair Selection -->
      {#if tabName !== 'symbol' && allPairs.length > 0}
        <div class="mt-1">
          <h2 class="text-base font-medium mb-3">{m.pair()}</h2>
          <div class="flex flex-wrap gap-2">
            {#each allPairs as pair}
              <label class="label cursor-pointer bg-base-200/70 hover:bg-base-300 px-3 py-1.5 rounded-md transition-colors">
                <input 
                  type="checkbox" 
                  class="checkbox checkbox-primary checkbox-xs"
                  bind:group={selectedPairs}
                  value={pair}
                />
                <span class="label-text ml-1.5 text-sm">{pair}</span>
              </label>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Search Form -->
      <div class="flex flex-wrap gap-4 items-center">
        <label class="input input-sm focus:outline-none w-56 text-sm font-mono">
          {m.start_time()}
          <input
            type="text"
            class="grow"
            placeholder="20231012"
            bind:value={search.startTime}
          />
        </label>
        <label class="input input-sm focus:outline-none w-56 text-sm font-mono">
          {m.end_time()}
          <input
                  type="text"
                  class="grow"
                  placeholder="20231012"
                  bind:value={search.stopTime}
          />
        </label>
        
        <button class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" onclick={loadData}>
          {m.search()}
        </button>
      </div>
    </div>
  </div>

  <!-- Data Table using snippet -->
  {@render PerfTable(`${m.pair()}/${m.date()}`, dataList)}

  <!-- Duration Stats -->
  <div class="stats shadow-md bg-base-100 rounded-lg border border-base-200">
    <div class="stat px-4">
      <div class="stat-title text-sm text-base-content/70">{m.win_duration()}</div>
      <div class="stat-value text-success text-2xl mt-1.5">{fmtDuration(durations.wins)}</div>
    </div>
    <div class="stat px-4">
      <div class="stat-title text-sm text-base-content/70">{m.draw_duration()}</div>
      <div class="stat-value text-warning text-2xl mt-1.5">{fmtDuration(durations.draws)}</div>
    </div>
    <div class="stat px-4">
      <div class="stat-title text-sm text-base-content/70">{m.loss_duration()}</div>
      <div class="stat-value text-error text-2xl mt-1.5">{fmtDuration(durations.losses)}</div>
    </div>
  </div>

  <!-- Tag Stats Tables -->
  <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
    {@render PerfTable(m.enter_reason(), enterTags)}
    {@render PerfTable(m.exit_reason(), exitTags)}
  </div>
</div>
