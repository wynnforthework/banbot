<script lang="ts">
  import { onMount } from 'svelte';
  import * as m from '$lib/paraglide/messages';
  import { getAccApi } from '$lib/netio';
  import { fmtDuration } from '$lib/dateutil';

  interface PairPerf {
    key: string;
    closeNum: number;
    profitSum: number;
    profitPct: number;
  }

  interface TagStat {
    key: string;
    totalCost: number;
    closeNum: number;
    winNum: number;
    profitSum: number;
    profitPct: number;
  }

  let tabName = $state('day');
  let dataList = $state<TagStat[]>([]);
  let exitTags = $state<TagStat[]>([]);
  let enterTags = $state<TagStat[]>([]);
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
<div class="card bg-white shadow-lg">
  <div class="card-body">
    <h2 class="card-title text-primary mb-4">{title}</h2>
    <div class="overflow-x-auto">
      <table class="table table-zebra">
        <thead>
          <tr>
            <th>{title}</th>
            <th>{m.win_total_num()}</th>
            <th>{m.total_profit()}</th>
            <th>{m.profit_rate()}</th>
            <th>{m.actions()}</th>
          </tr>
        </thead>
        <tbody>
          {#each data as row}
            <tr>
              <td>{row.key}</td>
              <td>{row.winNum}/{row.closeNum}</td>
              <td>{row.profitSum.toFixed(5)}</td>
              <td>{(row.profitPct * 100).toFixed(1)}%</td>
              <td>
                <a href="/dash/kline?pair={row.key}" class="link link-primary">
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
<div class="tabs tabs-boxed my-4">
  {#each tabLabels as {name, title}}
    <button 
      class="tab {tabName === name ? 'tab-active' : ''}"
      onclick={() => clickTab(name)}
    >
      {title}
    </button>
  {/each}
</div>
<div class="flex flex-col gap-6 min-h-screen">
  <!-- 合并的控制面板 -->
  <div class="card bg-white shadow-lg">
    <div class="card-body">

      <!-- Pair Selection -->
      {#if tabName !== 'symbol'}
        <div class="mt-6">
          <h2 class="text-primary mb-4">{m.pair()}</h2>
          <div class="flex flex-wrap gap-4">
            {#each allPairs as pair}
              <label class="label cursor-pointer bg-base-200 px-4 py-2 rounded-lg">
                <input 
                  type="checkbox" 
                  class="checkbox checkbox-primary"
                  bind:group={selectedPairs}
                  value={pair}
                />
                <span class="label-text ml-2">{pair}</span>
              </label>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Search Form -->
      <div class="mt-6">
        <div class="flex flex-wrap gap-6 items-end">
          <div class="form-control">
            <label class="label">
              <span class="label-text">{m.start_time()}</span>
            </label>
            <input 
              type="text" 
              class="input input-bordered" 
              placeholder="20231012"
              bind:value={search.startTime}
            />
          </div>
          
          <div class="form-control">
            <label class="label">
              <span class="label-text">{m.end_time()}</span>
            </label>
            <input 
              type="text" 
              class="input input-bordered" 
              placeholder="20231012"
              bind:value={search.stopTime}
            />
          </div>
          
          <button class="btn btn-primary" onclick={loadData}>
            {m.search()}
          </button>
        </div>
      </div>
    </div>
  </div>

  <!-- Data Table using snippet -->
   {@render PerfTable(`${m.pair()}/${m.date()}`, dataList)}

  <!-- Duration Stats -->
  <div class="stats shadow bg-white">
    <div class="stat">
      <div class="stat-title text-primary">{m.win_duration()}</div>
      <div class="stat-value text-success">{fmtDuration(durations.wins)}</div>
    </div>
    <div class="stat">
      <div class="stat-title text-primary">{m.draw_duration()}</div>
      <div class="stat-value text-warning">{fmtDuration(durations.draws)}</div>
    </div>
    <div class="stat">
      <div class="stat-title text-primary">{m.loss_duration()}</div>
      <div class="stat-value text-error">{fmtDuration(durations.losses)}</div>
    </div>
  </div>

  <!-- Tag Stats Tables -->
  <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
    {@render PerfTable(m.enter_reason(), enterTags)}
    {@render PerfTable(m.exit_reason(), exitTags)}
  </div>
</div>
