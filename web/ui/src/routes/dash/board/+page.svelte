<script lang="ts">
  import { onMount } from 'svelte';
  import * as m from '$lib/paraglide/messages.js';
  import { getAccApi } from '$lib/netio';
  import { alerts } from '$lib/stores/alerts';
  import { acc, loadAccounts } from '$lib/dash/store';
  import { fmtDateStr, fmtDateStrTZ, getUTCStamp } from '@/lib/dateutil';

  let data = $state({
    cpuPct: 0,
    ramPct: 0,
    lastProcess: 0,
    allowTradeAt: 0,
    doneProfitPctMean: 0,
    doneProfitMean: 0,
    doneProfitPctSum: 0,
    doneProfitSum: 0,
    allProfitPctMean: 0,
    allProfitMean: 0,
    allProfitPctSum: 0,
    allProfitSum: 0,
    orderNum: 0,
    doneOrderNum: 0,
    firstOdTs: 0,
    lastOdTs: 0,
    avgDuration: 0,
    bestPair: '',
    bestProfitPct: 0,
    winNum: 0,
    lossNum: 0,
    profitFactor: 0,
    winRate: 0,
    expectancy: 0,
    expectancyRatio: 0,
    maxDrawdownPct: 0,
    maxDrawdownVal: 0,
    totalCost: 0,
    botStartMs: 0,
    balanceTotal: 0,
    balanceItems: [],
    runTfs: [],
    exchange: '',
    market: '',
    pairs: []
  });

  let delayHours = $state(3);
  let entryStatus = $state('');

  async function saveAllowEntry() {
    const secs = delayHours * 3600;
    const rsp = await getAccApi('/delay_entry', {
      method: 'POST',
      body: JSON.stringify({secs})
    });
    
    if(rsp.code === 200 && rsp.allowTradeAt) {
      data.allowTradeAt = rsp.allowTradeAt;
      alerts.addAlert('success', m.save_success());
      return;
    }
    alerts.addAlert('error', m.save_failed({msg: rsp.msg ?? ''}));
  }

  onMount(async () => {
    await loadAccounts(false)

    // 系统信息：CPU内存，上次处理时间
    let rsp = await getAccApi('/bot_info');
    if(rsp.code === 200) {
      Object.assign(data, rsp);
    }
    
    // 整体详细概况
    rsp = await getAccApi('/statistics');
    if(rsp.code === 200) {
      Object.assign(data, rsp);
    }
    
    // 显示余额情况
    rsp = await getAccApi('/balance');
    if(rsp.code === 200) {
      data.balanceTotal = rsp.total ?? 0;
      data.balanceItems = rsp.items ?? [];
    }

    const curMs = getUTCStamp();
    if(data.allowTradeAt <= curMs) {
      entryStatus = m.allow_entry();
    }else{
      const dateStr = fmtDateStr(data.allowTradeAt, 'MM-DD HH:mm');
      entryStatus = m.no_entry_until({date: dateStr});
    }
  });
</script>

<div class="p-4 space-y-6">
  <!-- Stats Row -->
  <div class="stats shadow w-full">
    <div class="stat">
      <div class="stat-title">{m.watch_pairs()}</div>
      <div class="stat-value">{data.pairs.length}</div>
    </div>
    
    <div class="stat">
      <div class="stat-title">{m.open_close_num()}</div>
      <div class="stat-value">{data.orderNum - data.doneOrderNum} / {data.doneOrderNum}</div>
    </div>
    
    <div class="stat">
      <div class="stat-title">{m.win_loss_rate()}</div>
      <div class="stat-value">
        {data.winNum} / {data.lossNum} / {(data.winRate * 100).toFixed(0)}%
      </div>
    </div>
    
    <div class="stat">
      <div class="stat-title">{m.profit_factor()}</div>
      <div class="stat-value">{data.profitFactor.toFixed(2)}</div>
    </div>
  </div>

  <!-- System Info -->
  <div class="card bg-base-100 shadow-xl">
    <div class="card-body">
      <h2 class="card-title">{m.system_info()}</h2>
      <div class="grid grid-cols-2 gap-4">
        <div>CPU: {data.cpuPct.toFixed(1)}%</div>
        <div>RAM: {data.ramPct.toFixed(1)}%</div>
        <div>{m.market()}: {data.exchange}.{data.market}</div>
        <div>{m.timeframes()}: {data.runTfs.join(', ')}</div>
        <div>{m.start_time()}: {fmtDateStrTZ(data.botStartMs)}</div>
        <div>{m.last_active()}: {fmtDateStrTZ(data.lastProcess)}</div>
      </div>
    </div>
  </div>

  <!-- Trading Stats -->
  <div class="card bg-base-100 shadow-xl">
    <div class="card-body">
      <h2 class="card-title">{m.trading_stats()}</h2>
      <div class="my-4">
        <span class="label-text">{m.entry_status()} : </span>
        <span class="label-text mr-4">{entryStatus}</span>
        <div class="join">
          <span class="join-item px-4 flex items-center bg-base-200">
            {m.delay_hours()}
          </span>
          <input type="number" class="input input-bordered join-item w-24" 
                 bind:value={delayHours}
                 min="0" max="9999" step="0.1"/>
          <button class="btn join-item" onclick={saveAllowEntry}>
            {m.save()}
          </button>
        </div>
      </div>
      <div class="overflow-x-auto">
        <table class="table">
          <tbody>
            <!-- All Orders -->
            <tr>
              <th colspan="4" class="text-center bg-base-200">{m.all_orders()}</th>
            </tr>
            <tr>
              <td>{m.avg_profit_rate()}</td>
              <td>{data.allProfitPctMean.toFixed(1)}%</td>
              <td>{m.avg_profit()}</td>
              <td>{data.allProfitMean.toFixed(5)}</td>
            </tr>
            <tr>
              <td>{m.total_profit_rate()}</td>
              <td>{data.allProfitPctSum.toFixed(1)}%</td>
              <td>{m.total_profit()}</td>
              <td>{data.allProfitSum.toFixed(5)}</td>
            </tr>
            
            <!-- Closed Orders -->
            <tr>
              <th colspan="4" class="text-center bg-base-200">{m.closed_orders()}</th>
            </tr>
            <tr>
              <td>{m.avg_profit_rate()}</td>
              <td>{data.doneProfitPctMean.toFixed(1)}%</td>
              <td>{m.avg_profit()}</td>
              <td>{data.doneProfitMean.toFixed(5)}</td>
            </tr>
            <tr>
              <td>{m.total_profit_rate()}</td>
              <td>{data.doneProfitPctSum.toFixed(1)}%</td>
              <td>{m.total_profit()}</td>
              <td>{data.doneProfitSum.toFixed(5)}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>

  <!-- Balance Info -->
  <div class="card bg-base-100 shadow-xl">
    <div class="card-body">
      <h2 class="card-title">{m.wallet_balance()}</h2>
      {#if data.balanceItems.length > 1}
        <div class="text-lg">{m.total()}: {data.balanceTotal.toFixed(4)}</div>
      {/if}
      <div class="flex flex-wrap gap-4">
        {#each data.balanceItems as item}
          <div class="badge badge-lg gap-2">
            <span class="font-bold">{(item as any).symbol}</span>
            <span class="text-primary">{(item as any).free.toFixed(6)}</span>
            {#if (item as any).used > 0}
              <span class="text-info">/ {(item as any).used.toFixed(6)}</span>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  </div>
</div> 