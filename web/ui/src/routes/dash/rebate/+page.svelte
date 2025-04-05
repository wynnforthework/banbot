<script lang="ts">
  import { onMount } from 'svelte';
  import * as m from '$lib/paraglide/messages';
  import { getAccApi } from '$lib/netio';
  import { fmtDateStr, curTZ } from '$lib/dateutil';

  interface Income {
    tranId: string;
    symbol: string;
    income: number;
    asset: string;
    time: number;
    info: string;
    tradeId: string;
  }

  let searchData = $state({
    intype: 'REFERRAL_KICKBACK',
    symbol: '',
    startTime: '',
    limit: '30',
    account: ''
  });

  let itemList = $state<Income[]>([]);
  let stats = $state({
    load: false,
    startDate: '',
    stopDate: '',
    num: '0',
    assets: ''
  });

  const incomeTypes = [
    { value: 'REALIZED_PNL', label: m.realized_pnl() },
    { value: 'FUNDING_FEE', label: m.funding_fee() },
    { value: 'COMMISSION', label: m.commission() },
    { value: 'REFERRAL_KICKBACK', label: m.referral_kickback() },
    { value: 'COMMISSION_REBATE', label: m.commission_rebate() }
  ];

  function calcAssets(items: Income[]): string {
    const data: Record<string, number> = {};
    items.forEach(it => {
      data[it.asset] = (data[it.asset] || 0) + it.income;
    });
    return Object.entries(data)
      .map(([k, v]) => `${k}: ${v.toFixed(8)}`)
      .join('   ');
  }

  function toUTCStamp(dateStr: string): number {
    if (!dateStr) return 0;
    const year = parseInt(dateStr.slice(0, 4));
    const month = parseInt(dateStr.slice(4, 6)) - 1;
    const day = parseInt(dateStr.slice(6, 8));
    return new Date(Date.UTC(year, month, day)).getTime();
  }

  async function loadData() {
    const data = {
      intype: searchData.intype,
      symbol: searchData.symbol,
      startTime: toUTCStamp(searchData.startTime),
      limit: parseInt(searchData.limit)
    };

    const rsp = await getAccApi('/incomes', data);
    let res = [...(rsp.data ?? [])];
    const orgNum = res.length;

    // Reset stats
    stats.startDate = '';
    stats.stopDate = '';
    stats.num = '0';
    stats.assets = '';
    stats.load = false;
    if (res.length) {
      stats.startDate = fmtDateStr(res[0].time);
      stats.stopDate = fmtDateStr(res[res.length - 1].time);
      stats.load = true;
    }

    if (searchData.account) {
      res = res.filter(it => it.info === searchData.account);
    }

    stats.num = `${res.length} (原始: ${orgNum})`;
    if (res.length) {
      stats.startDate = fmtDateStr(res[0].time);
      stats.stopDate = fmtDateStr(res[res.length - 1].time);
      stats.assets = calcAssets(res);
    }

    itemList = res;
  }

  onMount(() => {
    loadData();
  });
</script>

<div class="flex flex-col gap-4">
  <!-- Search Form -->
  <div class="card bg-base-100">
    <div class="card-body p-4">
      <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
        <!-- Income Type -->
        <fieldset class="fieldset">
          <label class="label" for="incomeType">{m.income_type()}</label>
          <select id="incomeType" class="select" bind:value={searchData.intype}>
            {#each incomeTypes as type}
              <option value={type.value}>{type.label}</option>
            {/each}
          </select>
        </fieldset>

        <!-- Account -->
        <fieldset class="fieldset">
          <label class="label" for="account">{m.account()}</label>
          <input 
            id="account"
            type="text" 
            class="input"
            bind:value={searchData.account}
          />
        </fieldset>

        <!-- Symbol -->
        <fieldset class="fieldset">
          <label class="label" for="symbol">{m.symbol()}</label>
          <input 
            id="symbol"
            type="text" 
            class="input"
            bind:value={searchData.symbol}
          />
        </fieldset>

        <!-- Start Time -->
        <fieldset class="fieldset">
          <label class="label" for="startTime">{m.start_time()}</label>
          <input 
            id="startTime"
            type="text" 
            class="input"
            placeholder="20231012"
            bind:value={searchData.startTime}
          />
        </fieldset>

        <!-- Limit -->
        <fieldset class="fieldset">
          <label class="label" for="limit">{m.limit()}</label>
          <input 
            id="limit"
            type="number" 
            class="input"
            bind:value={searchData.limit}
          />
        </fieldset>

        <!-- Search Button -->
        <div class="flex justify-end items-end">
          <button 
            class="btn btn-sm bg-primary hover:bg-primary-focus text-primary-content border-none shadow-sm mt-6"
            onclick={loadData}
          >
            {m.search()}
          </button>
        </div>
      </div>
    </div>
  </div>

  <!-- Statistics -->
  {#if stats.load}
    <div class="stats bg-base-100 rounded-xl border border-base-200 text-sm">
      <div class="stat px-4 py-2">
        <div class="stat-title text-xs text-base-content/60 font-medium">{m.start_time()}</div>
        <div class="stat-value text-base mt-1">{stats.startDate}</div>
      </div>
      <div class="stat px-4 py-2">
        <div class="stat-title text-xs text-base-content/60 font-medium">{m.end_time()}</div>
        <div class="stat-value text-base mt-1">{stats.stopDate}</div>
      </div>
      <div class="stat px-4 py-2">
        <div class="stat-title text-xs text-base-content/60 font-medium">{m.count()}</div>
        <div class="stat-value text-base mt-1">{stats.num}</div>
      </div>
      <div class="stat px-4 py-2">
        <div class="stat-title text-xs text-base-content/60 font-medium">{m.total()}</div>
        <div class="stat-value text-base mt-1">{stats.assets}</div>
      </div>
    </div>
  {/if}

  <!-- Data Table -->
  <div class="card bg-base-100">
    <div class="card-body p-4">
      <div class="overflow-x-auto">
        <table class="table table-zebra table-sm">
          <thead>
            <tr>
              <th class="bg-base-200/30 font-medium text-sm">ID</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.symbol()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.amount()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.asset()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.timestamp()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.time()}({curTZ()})</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.account()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.trade_id()}</th>
            </tr>
          </thead>
          <tbody>
            {#each itemList as item}
              <tr class="hover:bg-base-200/30 transition-colors text-sm">
                <td class="font-medium">{item.tranId}</td>
                <td>{item.symbol}</td>
                <td class="font-medium">{item.income}</td>
                <td>{item.asset}</td>
                <td class="font-mono text-xs">{item.time}</td>
                <td>{fmtDateStr(item.time)}</td>
                <td>{item.info}</td>
                <td class="font-mono text-xs">{item.tradeId}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  </div>
</div>
