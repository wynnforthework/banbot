<script lang="ts">
  import { onMount } from 'svelte';
  import * as m from '$lib/paraglide/messages';
  import { getAccApi } from '$lib/netio';
  import { getDateStr } from '$lib/dateutil';

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

    if (res.length) {
      stats.startDate = getDateStr(res[0].time);
      stats.stopDate = getDateStr(res[res.length - 1].time);
    }

    if (searchData.account) {
      res = res.filter(it => it.info === searchData.account);
    }

    stats.num = `${res.length} (原始: ${orgNum})`;
    if (res.length) {
      stats.startDate = getDateStr(res[0].time);
      stats.stopDate = getDateStr(res[res.length - 1].time);
      stats.assets = calcAssets(res);
    }

    itemList = res;
  }

  onMount(() => {
    loadData();
  });
</script>

<div class="flex flex-col gap-6">
  <!-- Search Form -->
  <div class="card bg-base-100 shadow-xl">
    <div class="card-body">
      <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
        <!-- Income Type -->
        <div class="form-control">
          <label class="label">
            <span class="label-text">{m.income_type()}</span>
          </label>
          <select class="select select-bordered w-full" bind:value={searchData.intype}>
            {#each incomeTypes as type}
              <option value={type.value}>{type.label}</option>
            {/each}
          </select>
        </div>

        <!-- Account -->
        <div class="form-control">
          <label class="label">
            <span class="label-text">{m.account()}</span>
          </label>
          <input 
            type="text" 
            class="input input-bordered" 
            bind:value={searchData.account}
          />
        </div>

        <!-- Symbol -->
        <div class="form-control">
          <label class="label">
            <span class="label-text">{m.symbol()}</span>
          </label>
          <input 
            type="text" 
            class="input input-bordered" 
            bind:value={searchData.symbol}
          />
        </div>

        <!-- Start Time -->
        <div class="form-control">
          <label class="label">
            <span class="label-text">{m.start_time()}</span>
          </label>
          <input 
            type="text" 
            class="input input-bordered" 
            placeholder="20231012"
            bind:value={searchData.startTime}
          />
        </div>

        <!-- Limit -->
        <div class="form-control">
          <label class="label">
            <span class="label-text">{m.limit()}</span>
          </label>
          <input 
            type="number" 
            class="input input-bordered" 
            bind:value={searchData.limit}
          />
        </div>

        <!-- Search Button -->
        <div class="form-control justify-end">
          <button class="btn btn-primary mt-8" onclick={loadData}>
            {m.search()}
          </button>
        </div>
      </div>
    </div>
  </div>

  <!-- Statistics -->
  <div class="stats shadow bg-base-100">
    <div class="stat">
      <div class="stat-title">{m.start_time()}</div>
      <div class="stat-value text-sm">{stats.startDate}</div>
    </div>
    <div class="stat">
      <div class="stat-title">{m.end_time()}</div>
      <div class="stat-value text-sm">{stats.stopDate}</div>
    </div>
    <div class="stat">
      <div class="stat-title">{m.count()}</div>
      <div class="stat-value text-sm">{stats.num}</div>
    </div>
    <div class="stat">
      <div class="stat-title">{m.total()}</div>
      <div class="stat-value text-sm">{stats.assets}</div>
    </div>
  </div>

  <!-- Data Table -->
  <div class="overflow-x-auto">
    <table class="table table-zebra">
      <thead>
        <tr>
          <th>ID</th>
          <th>{m.symbol()}</th>
          <th>{m.amount()}</th>
          <th>{m.asset()}</th>
          <th>{m.timestamp()}</th>
          <th>{m.time()}</th>
          <th>{m.account()}</th>
          <th>{m.trade_id()}</th>
        </tr>
      </thead>
      <tbody>
        {#each itemList as item}
          <tr>
            <td>{item.tranId}</td>
            <td>{item.symbol}</td>
            <td>{item.income}</td>
            <td>{item.asset}</td>
            <td>{item.time}</td>
            <td>{getDateStr(item.time)}</td>
            <td>{item.info}</td>
            <td>{item.tradeId}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</div>
